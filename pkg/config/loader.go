package config

import (
"bytes"
"context"
"crypto/sha256"
"encoding/hex"
"encoding/json"
"errors"
"fmt"
"io"
"io/fs"
"os"
"path/filepath"
"regexp"
"sort"
"strings"
"time"
)

// Loader loads service configuration from a filesystem root with deterministic layering.
//
// Conventions:
//   <root>/<service>.json|yaml|yml
//   <root>/env/<env>/<service>.json|yaml|yml
//   <root>/tenants/<tenant>/<service>.json|yaml|yml
//
// Merge order (deterministic, later layers win):
//   base -> env -> tenant -> env-var overrides
//
// v0 YAML rule:
// - .yaml/.yml files are accepted ONLY if they contain valid JSON (JSON-as-YAML).
// - Otherwise, return ErrUnsupportedYAML.
//
// Env var overrides:
// - Use EnvPrefix (default: strings.ToUpper(service) + "_").
// - Use PathDelimiter (default "__") to express nested paths.
// - Example: GATEWAY_DB__HOST="localhost" => {"db":{"host":"localhost"}}
// - Values are parsed as JSON if possible; otherwise treated as strings.
//
// Observability:
// - OnWarn hook can be provided to surface non-fatal override skips (invalid segments, too deep, etc.).
type Options struct {
Service string // required (e.g. "gateway", "orchestrator")
Env     string // optional (e.g. "local", "dev", "prod")
Tenant  string // optional tenant id

// Optional explicit file override:
// If set, loader loads only this file (relative to root unless absolute path is used).
ExplicitPath string

// Env overrides
EnableEnvOverrides bool   // default true
EnvPrefix          string // default UPPER(service)+"_"
PathDelimiter      string // default "__"

// Safety bounds
MaxFiles     int   // default 8
MaxFileBytes int64 // default 2 MiB
MaxDepth     int   // default 32 (for merge + env insertion)
MaxEnvVars   int   // default 256 (matching prefix)

// Canonical JSON bound for Bundle.CanonicalJSON()
MaxCanonicalBytes int64 // default 4 MiB

// Optional warnings hook (nil-safe)
OnWarn func(code, detail string)
}

type Loader struct {
rootAbs string
opts    Options

reTenant *regexp.Regexp
reSeg    *regexp.Regexp
}

type Document struct {
Path     string         `json:"path"`      // rel path (slash)
Tier     string         `json:"tier"`      // base|env|tenant|explicit
LoadedAt time.Time      `json:"loaded_at"` // UTC
SHA256   string         `json:"sha256"`    // raw bytes hash
Data     map[string]any `json:"data"`      // parsed object
}

type Bundle struct {
Service string `json:"service"`
Env     string `json:"env,omitempty"`
Tenant  string `json:"tenant,omitempty"`

Docs     []Document     `json:"docs"`
Merged   map[string]any `json:"merged"`
LoadedAt time.Time      `json:"loaded_at"`

// Bound used by CanonicalJSON; set by Loader.Load.
MaxCanonicalBytes int64 `json:"-"`
}

var (
ErrInvalidRoot     = errors.New("config: invalid root")
ErrInvalidOptions  = errors.New("config: invalid options")
ErrPathEscape      = errors.New("config: path escapes root")
ErrNotFound        = errors.New("config: not found")
ErrTooManyFiles    = errors.New("config: too many files")
ErrFileTooLarge    = errors.New("config: file too large")
ErrUnsupportedExt  = errors.New("config: unsupported extension")
ErrInvalidJSON     = errors.New("config: invalid json")
ErrNotObject       = errors.New("config: top-level must be object")
ErrUnsupportedYAML = errors.New("config: yaml unsupported (v0 only supports json-as-yaml)")
ErrEnvOverride     = errors.New("config: env override invalid")
ErrDepthExceeded   = errors.New("config: max depth exceeded")
ErrCanonicalTooBig = errors.New("config: canonical json exceeds max bytes")
)

func NewLoader(root string, opts Options) (*Loader, error) {
root = strings.TrimSpace(root)
if root == "" {
return nil, ErrInvalidRoot

}opts.Service = strings.TrimSpace(opts.Service)
if opts.Service == "" {
return nil, fmt.Errorf("%w: service required", ErrInvalidOptions)

}opts.Env = strings.TrimSpace(opts.Env)
opts.Tenant = strings.TrimSpace(opts.Tenant)
opts.ExplicitPath = strings.TrimSpace(opts.ExplicitPath)

if opts.MaxFiles <= 0 {
opts.MaxFiles = 8

}if opts.MaxFileBytes <= 0 {
opts.MaxFileBytes = 2 * 1024 * 1024

}if opts.MaxDepth <= 0 {
opts.MaxDepth = 32

}if opts.MaxEnvVars <= 0 {
opts.MaxEnvVars = 256

}if opts.MaxCanonicalBytes <= 0 {
opts.MaxCanonicalBytes = 4 * 1024 * 1024


}if opts.PathDelimiter == "" {
opts.PathDelimiter = "__"

}if opts.EnvPrefix == "" {
opts.EnvPrefix = strings.ToUpper(opts.Service) + "_"

}// default enable env overrides
if opts.EnableEnvOverrides == false {
// allow explicit false
} else {
opts.EnableEnvOverrides = true


}abs, err := filepath.Abs(root)
if err != nil {
return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)

}absEval, err := filepath.EvalSymlinks(abs)
if err != nil {
return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)

}info, err := os.Stat(absEval)
if err != nil || !info.IsDir() {
return nil, fmt.Errorf("%w: not a directory", ErrInvalidRoot)


}reTenant := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
reSeg := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

if opts.Tenant != "" && !reTenant.MatchString(opts.Tenant) {
return nil, fmt.Errorf("%w: invalid tenant %q", ErrInvalidOptions, opts.Tenant)


}return &Loader{
rootAbs:  absEval,
opts:     opts,
reTenant: reTenant,
reSeg:    reSeg,
}, nil
}

func (l *Loader) warn(code, detail string) {
if l != nil && l.opts.OnWarn != nil {
l.opts.OnWarn(strings.TrimSpace(code), strings.TrimSpace(detail))

}}

// LoadFile loads a single config document at relPath (relative to root).
func (l *Loader) LoadFile(ctx context.Context, relPath string) (*Document, error) {
if ctx == nil {
ctx = context.Background()

}abs, rel, err := l.safeJoin(relPath)
if err != nil {
return nil, err

}doc, err := l.readDoc(ctx, abs, "explicit")
if err != nil {
return nil, err

}doc.Path = rel
return doc, nil
}

// Load loads layered configuration and applies env-var overrides.
func (l *Loader) Load(ctx context.Context) (*Bundle, error) {
if ctx == nil {
ctx = context.Background()


}var docs []Document
merged := map[string]any{}

// If explicit path is provided, load only that file.
if l.opts.ExplicitPath != "" {
doc, err := l.loadAnyPath(ctx, l.opts.ExplicitPath, "explicit")
if err != nil {
return nil, err

}docs = append(docs, *doc)
merged = deepMergeDeterministic(merged, doc.Data, l.opts.MaxDepth)
} else {
tiers := l.computeTierPaths()
if len(tiers) > l.opts.MaxFiles {
return nil, ErrTooManyFiles

}for _, tp := range tiers {
doc, err := l.loadAnyPath(ctx, tp.path, tp.tier)
if err != nil {
if errors.Is(err, ErrNotFound) {
continue

}return nil, err

}docs = append(docs, *doc)
merged = deepMergeDeterministic(merged, doc.Data, l.opts.MaxDepth)

}

}// Apply env overrides last (strongest precedence).
if l.opts.EnableEnvOverrides {
envMap, err := l.envOverrides()
if err != nil {
return nil, err

}if envMap != nil && len(envMap) > 0 {
merged = deepMergeDeterministic(merged, envMap, l.opts.MaxDepth)

}

}// Deterministic docs ordering
sort.SliceStable(docs, func(i, j int) bool {
if docs[i].Tier != docs[j].Tier {
return tierRank(docs[i].Tier) < tierRank(docs[j].Tier)

}return docs[i].Path < docs[j].Path
})

return &Bundle{
Service:           l.opts.Service,
Env:               l.opts.Env,
Tenant:            l.opts.Tenant,
Docs:              docs,
Merged:            merged,
LoadedAt:          time.Now().UTC(),
MaxCanonicalBytes: l.opts.MaxCanonicalBytes,
}, nil
}

// CanonicalJSON returns deterministic JSON bytes for the merged config.
// Keys are sorted recursively and output is bounded by MaxCanonicalBytes (default set by Loader.Load).
func (b *Bundle) CanonicalJSON() ([]byte, error) {
if b == nil {
return nil, ErrInvalidOptions

}maxBytes := b.MaxCanonicalBytes
if maxBytes <= 0 {
maxBytes = 4 * 1024 * 1024

}return canonicalJSON(b.Merged, maxBytes)
}

type tierPath struct {
tier string
path string
}

func (l *Loader) computeTierPaths() []tierPath {
cands := []string{
l.opts.Service + ".json",
l.opts.Service + ".yaml",
l.opts.Service + ".yml",

}var out []tierPath
for _, c := range cands {
out = append(out, tierPath{tier: "base", path: c})

}if l.opts.Env != "" {
for _, c := range cands {
out = append(out, tierPath{tier: "env", path: filepath.Join("env", l.opts.Env, c)})

}
}if l.opts.Tenant != "" {
for _, c := range cands {
out = append(out, tierPath{tier: "tenant", path: filepath.Join("tenants", l.opts.Tenant, c)})

}
}return out
}

func tierRank(tier string) int {
switch tier {
case "base":
return 1
case "env":
return 2
case "tenant":
return 3
default:
return 9

}}

func (l *Loader) loadAnyPath(ctx context.Context, relOrAbs string, tier string) (*Document, error) {
relOrAbs = strings.TrimSpace(relOrAbs)
if relOrAbs == "" {
return nil, ErrNotFound

}if filepath.IsAbs(relOrAbs) {
absEval, err := filepath.EvalSymlinks(relOrAbs)
if err != nil {
if errors.Is(err, fs.ErrNotExist) {
return nil, ErrNotFound

}return nil, err

}if !withinRoot(l.rootAbs, absEval) {
return nil, ErrPathEscape

}doc, err := l.readDoc(ctx, absEval, tier)
if err != nil {
return nil, err

}doc.Path = relSlash(l.rootAbs, absEval)
return &doc, nil


}abs, rel, err := l.safeJoin(relOrAbs)
if err != nil {
return nil, err

}doc, err := l.readDoc(ctx, abs, tier)
if err != nil {
return nil, err

}doc.Path = rel
return &doc, nil
}

func (l *Loader) safeJoin(relPath string) (abs string, rel string, err error) {
relPath = strings.TrimSpace(relPath)
if relPath == "" {
return "", "", ErrNotFound

}relClean := filepath.Clean(relPath)
if filepath.IsAbs(relClean) {
return "", "", ErrPathEscape

}if relClean == ".." || strings.HasPrefix(relClean, ".."+string(os.PathSeparator)) {
return "", "", ErrPathEscape


}abs = filepath.Join(l.rootAbs, relClean)
absEval, e := filepath.EvalSymlinks(abs)
if e != nil {
if errors.Is(e, fs.ErrNotExist) {
return "", "", ErrNotFound

}return "", "", e

}if !withinRoot(l.rootAbs, absEval) {
return "", "", ErrPathEscape

}rel = relSlash(l.rootAbs, absEval)
return absEval, rel, nil
}

func withinRoot(rootAbs, targetAbs string) bool {
root := strings.ToLower(filepath.Clean(rootAbs))
tgt := strings.ToLower(filepath.Clean(targetAbs))
if tgt == root {
return true

}sep := strings.ToLower(string(os.PathSeparator))
if !strings.HasSuffix(root, sep) {
root += sep

}return strings.HasPrefix(tgt, root)
}

func relSlash(rootAbs, abs string) string {
rel, err := filepath.Rel(rootAbs, abs)
if err != nil {
rel = abs

}rel = filepath.Clean(rel)
rel = filepath.ToSlash(rel)
rel = strings.TrimPrefix(rel, "./")
return rel
}

func (l *Loader) readDoc(ctx context.Context, absPath string, tier string) (Document, error) {
if err := ctx.Err(); err != nil {
return Document{}, err


}fi, err := os.Stat(absPath)
if err != nil {
if errors.Is(err, fs.ErrNotExist) {
return Document{}, ErrNotFound

}return Document{}, err

}if fi.Size() > l.opts.MaxFileBytes {
return Document{}, ErrFileTooLarge


}f, err := os.Open(absPath)
if err != nil {
return Document{}, err

}defer f.Close()

lr := &io.LimitedReader{R: f, N: l.opts.MaxFileBytes + 1}
raw := make([]byte, 0, minInt64(l.opts.MaxFileBytes, 64*1024))
buf := make([]byte, 32*1024)

for {
if err := ctx.Err(); err != nil {
return Document{}, err

}n, rerr := lr.Read(buf)
if n > 0 {
raw = append(raw, buf[:n]...)
if int64(len(raw)) > l.opts.MaxFileBytes {
return Document{}, ErrFileTooLarge

}
}if rerr != nil {
if rerr == io.EOF {
break

}return Document{}, rerr

}

}sum := sha256.Sum256(raw)
sha := hex.EncodeToString(sum[:])

ext := strings.ToLower(filepath.Ext(absPath))
var obj map[string]any

switch ext {
case ".json":
if err := decodeStrictJSON(raw, &obj); err != nil {
return Document{}, err

}case ".yaml", ".yml":
trimmed := bytesTrimBOM(raw)
if err := decodeStrictJSON(trimmed, &obj); err != nil {
return Document{}, ErrUnsupportedYAML

}default:
return Document{}, ErrUnsupportedExt


}return Document{
Path:     "",
Tier:     tier,
LoadedAt: time.Now().UTC(),
SHA256:   sha,
Data:     obj,
}, nil
}

func decodeStrictJSON(b []byte, out *map[string]any) error {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)

	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("%w: trailing tokens", ErrInvalidJSON)
	} else if err != io.EOF {
		return fmt.Errorf("%w: trailing tokens", ErrInvalidJSON)
	}

	m, ok := v.(map[string]any)
	if !ok {
		return ErrNotObject

	}*out = m
	return nil
}

func bytesTrimBOM(b []byte) []byte {
if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
return b[3:]

}return b
}

// ---- deterministic merge ----

func deepMergeDeterministic(dst, src map[string]any, maxDepth int) map[string]any {
return deepMergeDeterministicDepth(dst, src, 0, maxDepth)
}

func deepMergeDeterministicDepth(dst, src map[string]any, depth int, maxDepth int) map[string]any {
if maxDepth > 0 && depth > maxDepth {
return src

}if dst == nil {
dst = map[string]any{}

}if src == nil {
return dst


}out := make(map[string]any, len(dst))
for k, v := range dst {
out[k] = v


}keys := make([]string, 0, len(src))
for k := range src {
keys = append(keys, k)

}sort.Strings(keys)

for _, k := range keys {
sv := src[k]
if dv, ok := out[k]; ok {
dm, dok := dv.(map[string]any)
sm, sok := sv.(map[string]any)
if dok && sok {
out[k] = deepMergeDeterministicDepth(dm, sm, depth+1, maxDepth)
continue

}
}out[k] = sv

}return out
}

// ---- env overrides ----

func (l *Loader) envOverrides() (map[string]any, error) {
prefix := l.opts.EnvPrefix
if prefix == "" {
return nil, nil

}del := l.opts.PathDelimiter
if del == "" {
del = "__"


}out := map[string]any{}
matched := 0

for _, kv := range os.Environ() {
parts := strings.SplitN(kv, "=", 2)
if len(parts) != 2 {
continue

}k := parts[0]
if !strings.HasPrefix(k, prefix) {
continue

}matched++
if matched > l.opts.MaxEnvVars {
return nil, fmt.Errorf("%w: too many env vars for prefix %q", ErrEnvOverride, prefix)


}rest := strings.TrimSpace(strings.TrimPrefix(k, prefix))
if rest == "" {
l.warn("env.skip.empty_key", k)
continue


}rawSegs := strings.Split(rest, del)
segs := make([]string, 0, len(rawSegs))
bad := false
for _, s := range rawSegs {
s = strings.ToLower(strings.TrimSpace(s))
if s == "" {
l.warn("env.skip.empty_segment", k)
continue

}if !l.reSeg.MatchString(s) {
l.warn("env.skip.invalid_segment", fmt.Sprintf("%s segment=%q", k, s))
bad = true
break

}segs = append(segs, s)

}if bad || len(segs) == 0 {
continue

}if len(segs) > l.opts.MaxDepth {
l.warn("env.skip.too_deep", k)
continue


}val := parseEnvValue(parts[1])
if err := setPath(out, segs, val, l.opts.MaxDepth); err != nil {
l.warn("env.skip.setpath_error", fmt.Sprintf("%s err=%v", k, err))
continue

}

}if len(out) == 0 {
return nil, nil

}return out, nil
}

func parseEnvValue(s string) any {
s = strings.TrimSpace(s)
if s == "" {
return ""

}var v any
dec := json.NewDecoder(strings.NewReader(s))
dec.UseNumber()
if err := dec.Decode(&v); err == nil && !dec.More() {
return v

}return s
}

func setPath(root map[string]any, segs []string, val any, maxDepth int) error {
if maxDepth > 0 && len(segs) > maxDepth {
return ErrDepthExceeded

}cur := root
for i := 0; i < len(segs); i++ {
k := segs[i]
if i == len(segs)-1 {
cur[k] = val
return nil

}nxt, ok := cur[k]
if ok {
if m, ok := nxt.(map[string]any); ok {
cur = m
continue

}
}m := map[string]any{}
cur[k] = m
cur = m

}return nil
}

// ---- canonical json ----

func canonicalJSON(root map[string]any, maxBytes int64) ([]byte, error) {
	var buf bytes.Buffer
	reNum := regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

	write := func(b []byte) error {
		if maxBytes > 0 && int64(buf.Len()+len(b)) > maxBytes {
			return ErrCanonicalTooBig

}_, _ = buf.Write(b)
return nil


}var enc func(any) error
enc = func(v any) error {
switch x := v.(type) {
case nil:
return write([]byte("null"))
case bool:
if x {
return write([]byte("true"))

}return write([]byte("false"))
case string:
b, err := json.Marshal(x)
if err != nil {
return write([]byte(`""`))

}return write(b)
	case json.Number:
		// emit as token; if empty/invalid, null
		s := strings.TrimSpace(x.String())
		if s == "" {
			return write([]byte("null"))

		}
		if !reNum.MatchString(s) {
			return write([]byte("null"))
		}
		return write([]byte(s))
case float64:
// should not happen from UseNumber, but handle
b, err := json.Marshal(x)
if err != nil {
return write([]byte("null"))

}return write(b)
case []any:
if err := write([]byte("[")); err != nil {
return err

}for i := 0; i < len(x); i++ {
if i > 0 {
if err := write([]byte(",")); err != nil {
return err

}
}if err := enc(x[i]); err != nil {
return err

}
}return write([]byte("]"))
case map[string]any:
keys := make([]string, 0, len(x))
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
}kb, _ := json.Marshal(k)
if err := write(kb); err != nil {
return err

}if err := write([]byte(":")); err != nil {
return err

}if err := enc(x[k]); err != nil {
return err

}
}return write([]byte("}"))
default:
// fallback: marshal; not fully deterministic for nested maps, but acceptable for non-map types.
b, err := json.Marshal(x)
if err != nil {
return write([]byte("null"))

}return write(b)

}

}// Root is always object for merged config.
if err := enc(root); err != nil {
return nil, err

}return buf.Bytes(), nil
}

func minInt64(a, b int64) int64 {
if a < b {
return a

}return b
}
