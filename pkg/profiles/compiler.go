package profiles

import (
"bytes"
"context"
"crypto/sha256"
"encoding/hex"
"encoding/json"
"fmt"
"regexp"
"sort"
"strconv"
"strings"
"time"
)

const CompilerVersion = "profiles-compiler/v0.1.1"

type CompileSeverity string

const (
CSevInfo  CompileSeverity = "info"
CSevWarn  CompileSeverity = "warn"
CSevError CompileSeverity = "error"
)

const (
compileMaxPathLen = 512
)

type CompileIssue struct {
Severity CompileSeverity `json:"severity"`
Code     string          `json:"code"`
DocPath  string          `json:"doc_path,omitempty"`
Path     string          `json:"path,omitempty"`
Message  string          `json:"message"`
}

type CompileReport struct {
GeneratedAt     time.Time `json:"generated_at"`
CompilerVersion string    `json:"compiler_version"`
OptionsHash     string    `json:"options_hash"`
OptionsSnapshot COptions  `json:"options_snapshot"`

Env    string `json:"env,omitempty"`
Tenant string `json:"tenant,omitempty"`

CheckedNodes int `json:"checked_nodes"`
Errors       int `json:"errors"`
Warnings     int `json:"warnings"`
Infos        int `json:"infos"`

Issues []CompileIssue `json:"issues"`
}

func (r CompileReport) HasErrors() bool { return r.Errors > 0 }

type DocDigest struct {
Path   string `json:"path"`
SHA256 string `json:"sha256"`
Tier   string `json:"tier,omitempty"`
}

type CompiledBundle struct {
Env    string `json:"env"`
Tenant string `json:"tenant"`

CompiledAt time.Time `json:"compiled_at"`
Version    string    `json:"version"`

DocDigests []DocDigest `json:"doc_digests"`

InputHash  string `json:"input_hash"`
OutputHash string `json:"output_hash"`

CanonicalJSON []byte         `json:"canonical_json"`
Data          map[string]any `json:"data"`
}

type COptions struct {
MaxDepth       int   `json:"max_depth"`
MaxNodes       int   `json:"max_nodes"`
MaxStringBytes int   `json:"max_string_bytes"`
MaxArrayLen    int   `json:"max_array_len"`
MaxMapKeys     int   `json:"max_map_keys"`
MaxOutputBytes int   `json:"max_output_bytes"`

MaxIssues int `json:"max_issues"`

TrimStrings    bool `json:"trim_strings"`
StripNullBytes bool `json:"strip_null_bytes"`

// If false (default), env placeholders are errors.
// If true, placeholders are warnings (caller may later expand safely elsewhere).
AllowEnvPlaceholders bool `json:"allow_env_placeholders"`
}

type Compiler struct {
opts COptions

reEnv1   *regexp.Regexp // ${VAR}
reEnv2   *regexp.Regexp // $(VAR)
reNumber *regexp.Regexp // JSON number token
}

func NewCompiler(opts COptions) *Compiler {
if opts.MaxDepth <= 0 {
opts.MaxDepth = 64

}if opts.MaxNodes <= 0 {
opts.MaxNodes = 250000

}if opts.MaxStringBytes <= 0 {
opts.MaxStringBytes = 16 * 1024

}if opts.MaxArrayLen <= 0 {
opts.MaxArrayLen = 50000

}if opts.MaxMapKeys <= 0 {
opts.MaxMapKeys = 50000

}if opts.MaxOutputBytes <= 0 {
opts.MaxOutputBytes = 4 * 1024 * 1024 // 4 MiB

}if opts.MaxIssues <= 0 {
opts.MaxIssues = 10000


}// Defaults: true
if !opts.TrimStrings {
// zero-value might mean "not specified"  default to true
opts.TrimStrings = true

}if !opts.StripNullBytes {
opts.StripNullBytes = true


}return &Compiler{
opts:     opts,
reEnv1:   regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`),
reEnv2:   regexp.MustCompile(`\$\([A-Za-z_][A-Za-z0-9_]*\)`),
reNumber: regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`),

}}

func (c *Compiler) CompileBundle(ctx context.Context, b *Bundle) (*CompiledBundle, CompileReport) {
if ctx == nil {
ctx = context.Background()

}rep := c.newReport()
if b == nil {
rep.addIssue(CSevError, "bundle.nil", "", "", "bundle is nil")
return nil, finalizeCompileReport(rep)

}rep.Env = b.Env
rep.Tenant = b.Tenant

// Deterministic doc digests
digests := make([]DocDigest, 0, len(b.Docs))
for i := range b.Docs {
digests = append(digests, DocDigest{
Path:   b.Docs[i].Path,
SHA256: b.Docs[i].SHA256,
Tier:   b.Docs[i].Tier,
})

}sort.Slice(digests, func(i, j int) bool { return digests[i].Path < digests[j].Path })

// Canonicalize merged input (bounded)
inputCanon, inErr := canonicalJSON(
b.Merged,
c.opts.MaxDepth, c.opts.MaxNodes, c.opts.MaxStringBytes, c.opts.MaxArrayLen, c.opts.MaxMapKeys, c.opts.MaxOutputBytes,
ctx, &rep, c.reNumber,

)if inErr != nil {
rep.addIssue(CSevError, "input.canonicalize_failed", "", "$", inErr.Error())
return nil, finalizeCompileReport(rep)


}inputHash := sha256Hex(append(hashDocDigests(digests), inputCanon...))

// Normalize merged tree
nodeBudget := c.opts.MaxNodes
compiledAny, err := c.normalizeAny(ctx, "$", b.Merged, 0, &nodeBudget, &rep)
rep.CheckedNodes += (c.opts.MaxNodes - nodeBudget)
if err != nil {
rep.addIssue(CSevError, "compile.normalize_failed", "", "$", err.Error())
return nil, finalizeCompileReport(rep)


}compiledMap, ok := compiledAny.(map[string]any)
if !ok {
compiledMap = map[string]any{"value": compiledAny}
rep.addIssue(CSevWarn, "compile.coerced_root", "", "$", "root was not an object; coerced into {\"value\": ...}")


}// Canonicalize output
outCanon, outErr := canonicalJSON(
compiledMap,
c.opts.MaxDepth, c.opts.MaxNodes, c.opts.MaxStringBytes, c.opts.MaxArrayLen, c.opts.MaxMapKeys, c.opts.MaxOutputBytes,
ctx, &rep, c.reNumber,

)if outErr != nil {
rep.addIssue(CSevError, "output.canonicalize_failed", "", "$", outErr.Error())
return nil, finalizeCompileReport(rep)


}cb := &CompiledBundle{
Env:           b.Env,
Tenant:        b.Tenant,
CompiledAt:    time.Now().UTC(),
Version:       CompilerVersion,
DocDigests:    digests,
InputHash:     inputHash,
OutputHash:    sha256Hex(outCanon),
CanonicalJSON: outCanon,
Data:          compiledMap,

}return cb, finalizeCompileReport(rep)
}

// ---- report helpers ----

func (c *Compiler) newReport() CompileReport {
return CompileReport{
GeneratedAt:     time.Now().UTC(),
CompilerVersion: CompilerVersion,
OptionsHash:     stableOptionsHash(c.opts),
OptionsSnapshot: c.opts,
Issues:          make([]CompileIssue, 0, 64),

}}

// addIssue enforces cap and adds a single truncation marker once.
func (r *CompileReport) addIssue(sev CompileSeverity, code, docPath, path, msg string) {
if len(r.Issues) >= r.OptionsSnapshot.MaxIssues {
if len(r.Issues) == r.OptionsSnapshot.MaxIssues {
r.Issues = append(r.Issues, CompileIssue{
Severity: CSevWarn,
Code:     "report.truncated",
Message:  fmt.Sprintf("issue limit reached (%d); further issues not reported", r.OptionsSnapshot.MaxIssues),
})

}return

}if len(path) > compileMaxPathLen {
path = path[:compileMaxPathLen] + "..."

}r.Issues = append(r.Issues, CompileIssue{
Severity: sev,
Code:     code,
DocPath:  docPath,
Path:     path,
Message:  msg,
})
}

func finalizeCompileReport(r CompileReport) CompileReport {
sort.SliceStable(r.Issues, func(i, j int) bool {
a, b := r.Issues[i], r.Issues[j]
if a.Severity != b.Severity {
return cSevRank(a.Severity) < cSevRank(b.Severity)

}if a.Code != b.Code {
return a.Code < b.Code

}if a.DocPath != b.DocPath {
return a.DocPath < b.DocPath

}if a.Path != b.Path {
return a.Path < b.Path

}return a.Message < b.Message
})

r.Errors, r.Warnings, r.Infos = 0, 0, 0
for _, it := range r.Issues {
switch it.Severity {
case CSevError:
r.Errors++
case CSevWarn:
r.Warnings++
default:
r.Infos++

}
}return r
}

func cSevRank(s CompileSeverity) int {
switch s {
case CSevError:
return 1
case CSevWarn:
return 2
default:
return 3

}}

func stableOptionsHash(o COptions) string {
// json.Marshal for simple struct should not fail; if it does, return fixed sentinel hash.
b, err := json.Marshal(o)
if err != nil {
sum := sha256.Sum256([]byte("options_marshal_error"))
return hex.EncodeToString(sum[:])

}sum := sha256.Sum256(b)
return hex.EncodeToString(sum[:])
}

func sha256Hex(b []byte) string {
sum := sha256.Sum256(b)
return hex.EncodeToString(sum[:])
}

func hashDocDigests(d []DocDigest) []byte {
var buf bytes.Buffer
for _, x := range d {
buf.WriteString(x.Path)
buf.WriteByte(0)
buf.WriteString(x.SHA256)
buf.WriteByte(0)
buf.WriteString(x.Tier)
buf.WriteByte(0)

}return buf.Bytes()
}

// ---- normalization ----

func (c *Compiler) normalizeAny(ctx context.Context, path string, v any, depth int, nodeBudget *int, rep *CompileReport) (any, error) {
if err := ctx.Err(); err != nil {
return nil, err

}if depth > c.opts.MaxDepth {
rep.addIssue(CSevError, "limits.depth", "", path, fmt.Sprintf("max depth exceeded (%d)", c.opts.MaxDepth))
return nil, nil

}if *nodeBudget <= 0 {
rep.addIssue(CSevError, "limits.nodes", "", path, fmt.Sprintf("max nodes exceeded (%d)", c.opts.MaxNodes))
return nil, nil

}*nodeBudget--

switch x := v.(type) {
case map[string]any:
if len(x) > c.opts.MaxMapKeys {
rep.addIssue(CSevError, "limits.map_keys", "", path, fmt.Sprintf("map has too many keys (%d>%d)", len(x), c.opts.MaxMapKeys))
return map[string]any{}, nil

}keys := make([]string, 0, len(x))
for k := range x {
keys = append(keys, k)

}sort.Strings(keys)

out := make(map[string]any, len(keys))
for _, k := range keys {
childPath := cJoinPath(path, k)
nv, err := c.normalizeAny(ctx, childPath, x[k], depth+1, nodeBudget, rep)
if err != nil {
return nil, err

}out[k] = nv

}return out, nil

case []any:
if len(x) > c.opts.MaxArrayLen {
rep.addIssue(CSevError, "limits.array_len", "", path, fmt.Sprintf("array too long (%d>%d)", len(x), c.opts.MaxArrayLen))
return []any{}, nil

}out := make([]any, len(x))
for i := 0; i < len(x); i++ {
ip := cJoinIndexPath(path, i)
nv, err := c.normalizeAny(ctx, ip, x[i], depth+1, nodeBudget, rep)
if err != nil {
return nil, err

}out[i] = nv

}return out, nil

case string:
s := x
if c.opts.StripNullBytes && strings.IndexByte(s, 0x00) >= 0 {
s = strings.ReplaceAll(s, "\x00", "")
rep.addIssue(CSevWarn, "string.null_bytes_stripped", "", path, "null bytes stripped from string")

}if c.opts.TrimStrings {
s = strings.TrimSpace(s)

}if len(s) > c.opts.MaxStringBytes {
s = s[:c.opts.MaxStringBytes]
rep.addIssue(CSevError, "limits.string_bytes", "", path, fmt.Sprintf("string truncated to %d bytes", c.opts.MaxStringBytes))


}if c.reEnv1.MatchString(s) || c.reEnv2.MatchString(s) {
if c.opts.AllowEnvPlaceholders {
rep.addIssue(CSevWarn, "pattern.env_placeholder", "", path, "env placeholder detected (allowed)")
} else {
rep.addIssue(CSevError, "pattern.env_placeholder", "", path, "env placeholder detected (disallowed)")

}
}return s, nil

case json.Number:
// Preserve json.Number for deterministic downstream behavior; validate token syntax lightly.
s := strings.TrimSpace(x.String())
if !c.reNumber.MatchString(s) {
rep.addIssue(CSevWarn, "number.malformed", "", path, "json.Number has invalid JSON-number syntax; encoded as null")
return nil, nil

}return x, nil

case bool, nil:
return x, nil

case float64:
rep.addIssue(CSevWarn, "number.float64", "", path, "float64 encountered; prefer json.Number via decoder.UseNumber")
return x, nil

default:
rep.addIssue(CSevError, "type.unsupported", "", path, fmt.Sprintf("unsupported type %T", v))
return nil, nil

}}

// ---- canonical JSON encoder (deterministic) ----
//
// Deterministic JSON bytes (sorted object keys). Bounded output bytes.
// When depth/nodes exceeded, subtree becomes null and recursion stops for that subtree.

func canonicalJSON(root any, maxDepth, maxNodes, maxStringBytes, maxArrayLen, maxMapKeys int, maxOutBytes int, ctx context.Context, rep *CompileReport, reNumber *regexp.Regexp) ([]byte, error) {
var buf bytes.Buffer
nodeBudget := maxNodes

write := func(b []byte) error {
if maxOutBytes > 0 && buf.Len()+len(b) > maxOutBytes {
return fmt.Errorf("canonical json exceeds max output bytes (%d)", maxOutBytes)

}_, _ = buf.Write(b)
return nil


}var enc func(path string, v any, depth int) error
enc = func(path string, v any, depth int) error {
if err := ctx.Err(); err != nil {
return err


}// Depth/node guard: replace subtree with null and STOP descending.
if depth > maxDepth {
rep.addIssue(CSevError, "limits.depth", "", path, fmt.Sprintf("max depth exceeded (%d) during encoding", maxDepth))
return write([]byte("null"))

}if nodeBudget <= 0 {
rep.addIssue(CSevError, "limits.nodes", "", path, fmt.Sprintf("max nodes exceeded (%d) during encoding", maxNodes))
return write([]byte("null"))

}nodeBudget--

switch x := v.(type) {
case map[string]any:
if len(x) > maxMapKeys {
rep.addIssue(CSevError, "limits.map_keys", "", path, fmt.Sprintf("map has too many keys (%d>%d) during encoding", len(x), maxMapKeys))
return write([]byte("{}"))

}keys := make([]string, 0, len(x))
for k := range x {
keys = append(keys, k)

}sort.Strings(keys)

if err := write([]byte("{")); err != nil {
return err

}for i, k := range keys {
if i > 0 {
if err := write([]byte(",")); err != nil {
return err

}
}ks, err := json.Marshal(k)
if err != nil {
return err

}if err := write(ks); err != nil {
return err

}if err := write([]byte(":")); err != nil {
return err

}if err := enc(cJoinPath(path, k), x[k], depth+1); err != nil {
return err

}
}return write([]byte("}"))

case []any:
if len(x) > maxArrayLen {
rep.addIssue(CSevError, "limits.array_len", "", path, fmt.Sprintf("array too long (%d>%d) during encoding", len(x), maxArrayLen))
return write([]byte("[]"))

}if err := write([]byte("[")); err != nil {
return err

}for i := 0; i < len(x); i++ {
if i > 0 {
if err := write([]byte(",")); err != nil {
return err

}
}if err := enc(cJoinIndexPath(path, i), x[i], depth+1); err != nil {
return err

}
}return write([]byte("]"))

case string:
s := x
if len(s) > maxStringBytes {
s = s[:maxStringBytes]
rep.addIssue(CSevWarn, "limits.string_bytes", "", path, "string truncated during encoding")

}b, err := json.Marshal(s)
if err != nil {
return err

}return write(b)

case json.Number:
s := strings.TrimSpace(x.String())
if !reNumber.MatchString(s) {
rep.addIssue(CSevWarn, "number.malformed", "", path, "invalid json.Number syntax; encoded as null")
return write([]byte("null"))

}return write([]byte(s))

case bool:
if x {
return write([]byte("true"))

}return write([]byte("false"))

case nil:
return write([]byte("null"))

case float64:
s := strconv.FormatFloat(x, 'g', -1, 64)
if s == "NaN" || s == "+Inf" || s == "-Inf" || s == "Inf" {
rep.addIssue(CSevWarn, "number.non_finite", "", path, "non-finite float encoded as null")
return write([]byte("null"))

}return write([]byte(s))

default:
rep.addIssue(CSevWarn, "type.unsupported_encode", "", path, fmt.Sprintf("unsupported type %T encoded as null", v))
return write([]byte("null"))

}

}if err := enc("$", root, 0); err != nil {
return nil, err

}return buf.Bytes(), nil
}

func cJoinPath(base, key string) string {
out := base + "." + key
if len(out) > compileMaxPathLen {
return out[:compileMaxPathLen] + "..."

}return out
}

func cJoinIndexPath(base string, idx int) string {
out := fmt.Sprintf("%s[%d]", base, idx)
if len(out) > compileMaxPathLen {
return out[:compileMaxPathLen] + "..."

}return out
}
