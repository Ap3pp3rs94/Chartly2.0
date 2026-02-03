package profiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Validator performs schema-free profile validation focused on safety + structural sanity.
// This is NOT JSON Schema validation.
const ValidatorVersion = "profiles-validator/v0.1.1"

// Hard report-size controls.
const (
	MaxIssuesPerReport = 10000
	MaxPathLen         = 512
)
// type Severity string

const (
	SevInfo  Severity = "info"
	SevWarn  Severity = "warn"
	SevError Severity = "error"
)
type Issue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	DocPath  string   `json:"doc_path,omitempty"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
}
type Report struct {
	GeneratedAt      time.Time `json:"generated_at"`
	ValidatorVersion string    `json:"validator_version"`
	OptionsHash      string    `json:"options_hash"`
	OptionsSnapshot  VOptions  `json:"options_snapshot"`

	Env    string `json:"env,omitempty"`
	Tenant string `json:"tenant,omitempty"`

	CheckedDocs  int `json:"checked_docs"`
	CheckedNodes int `json:"checked_nodes"`

	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`

	Issues []Issue `json:"issues"`
}

func (r Report) HasErrors() bool { return r.Errors > 0 }

// VOptions controls validator bounds and pattern checks.
type VOptions struct {
	// Bounds
	MaxDepth       int `json:"max_depth"`
	MaxNodes       int `json:"max_nodes"`
	MaxStringBytes int `json:"max_string_bytes"`
	MaxArrayLen    int `json:"max_array_len"`
	MaxMapKeys     int `json:"max_map_keys"`

	// Toggles:
	// - If TogglesExplicit is false (default), all checks default ON.
	// - If TogglesExplicit is true, each bool is treated as an explicit enable (false disables).
	TogglesExplicit      bool `json:"toggles_explicit"`
	CheckEnvPlaceholders bool `json:"check_env_placeholders"`
	CheckTraversalKeys   bool `json:"check_traversal_keys"`
	CheckNullBytes       bool `json:"check_null_bytes"`
}
type Validator struct {
	opts VOptions

	reEnv1   *regexp.Regexp
	reEnv2   *regexp.Regexp
	reBadKey *regexp.Regexp
}

func NewValidator(opts VOptions) *Validator {
	// Default bounds
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 64

	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 250000

	}
	if opts.MaxStringBytes <= 0 {
		opts.MaxStringBytes = 16 * 1024

	}
	if opts.MaxArrayLen <= 0 {
		opts.MaxArrayLen = 50000

	}
	if opts.MaxMapKeys <= 0 {
		opts.MaxMapKeys = 50000

	} // Toggle defaults
	if !opts.TogglesExplicit {
		opts.CheckEnvPlaceholders = true
		opts.CheckTraversalKeys = true
		opts.CheckNullBytes = true

	}
	return &Validator{
		opts:     opts,
		reEnv1:   regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`),
		reEnv2:   regexp.MustCompile(`\$\([A-Za-z_][A-Za-z0-9_]*\)`),
		reBadKey: regexp.MustCompile(`(^\.\.?$)|(\.\.)|([\\])|(^/)|(:\/\/)`),
	}
}
func (v *Validator) ValidateBundle(ctx context.Context, b *Bundle) Report {
	if ctx == nil {
		ctx = context.Background()

	}
	r := v.newReport()
if b == nil {
		r.addIssue(Issue{Severity: SevError, Code: "bundle.nil", Message: "bundle is nil"})
return finalizeReport(r)

	}
	r.Env = b.Env
	r.Tenant = b.Tenant

	// Validate each doc; merge issues via addIssue (cap enforced at bundle level).
	for i := range b.Docs {
		if err := ctx.Err(); err != nil {
			r.addIssue(Issue{Severity: SevError, Code: "ctx.canceled", Message: err.Error()})
return finalizeReport(r)

		}
		dr := v.ValidateDocument(ctx, &b.Docs[i])
r.CheckedDocs += 1
		r.CheckedNodes += dr.CheckedNodes

		for _, it := range dr.Issues {
			r.addIssue(it)

		}

	} // Validate merged as synthetic doc.
	md := &Document{
		Path:     "merged",
		Tier:     "merged",
		LoadedAt: b.LoadedAt,
		SHA256:   "",
		Data:     b.Merged,
	}
	mr := v.ValidateDocument(ctx, md)
r.CheckedDocs += 1
	r.CheckedNodes += mr.CheckedNodes
	for _, it := range mr.Issues {
		r.addIssue(it)

	}
	return finalizeReport(r)
}
func (v *Validator) ValidateDocument(ctx context.Context, d *Document) Report {
	if ctx == nil {
		ctx = context.Background()

	}
	r := v.newReport()
if d == nil {
		r.addIssue(Issue{Severity: SevError, Code: "doc.nil", Message: "document is nil"})
return finalizeReport(r)

	}
	r.CheckedDocs = 1

	if d.Data == nil {
		r.addIssue(Issue{Severity: SevInfo, Code: "doc.empty", DocPath: d.Path, Message: "document data is empty"})
return finalizeReport(r)

	}
	nodeBudget := v.opts.MaxNodes

	var walk func(path string, val any, depth int) // error walk = func(path string, val any, depth int) error {
		if err := ctx.Err(); err != nil {
			return err

		}
		if depth > v.opts.MaxDepth {
			r.addIssue(Issue{
				Severity: SevError, Code: "limits.depth",
				DocPath: d.Path, Path: path,
				Message: fmt.Sprintf("max depth exceeded (%d)", v.opts.MaxDepth),
			})
// return nil

		}
		if nodeBudget <= 0 {
			r.addIssue(Issue{
				Severity: SevError, Code: "limits.nodes",
				DocPath: d.Path, Path: path,
				Message: fmt.Sprintf("max nodes exceeded (%d)", v.opts.MaxNodes),
			})
// return nil

		}
		nodeBudget--
		r.CheckedNodes++

		switch x := val.(type) {
		case map[string]any:
			if len(x) > v.opts.MaxMapKeys {
				r.addIssue(Issue{
					Severity: SevError, Code: "limits.map_keys",
					DocPath: d.Path, Path: path,
					Message: fmt.Sprintf("map has too many keys (%d>%d)", len(x), v.opts.MaxMapKeys),
				})
// return nil

			}
			keys := make([]string, 0, len(x))
for k := range x {
				keys = append(keys, k)

			}
			sort.Strings(keys)
for _, k := range keys {
				childPath := joinPath(path, k)
if v.opts.CheckTraversalKeys && v.reBadKey.MatchString(k) {
					r.addIssue(Issue{
						Severity: SevWarn, Code: "pattern.suspicious_key",
						DocPath: d.Path, Path: childPath,
						Message: fmt.Sprintf("suspicious key name %q", k),
					})

				}
				if err := walk(childPath, x[k], depth+1); err != nil {
					return err

				}
			}
			return nil

		case []any:
			if len(x) > v.opts.MaxArrayLen {
				r.addIssue(Issue{
					Severity: SevError, Code: "limits.array_len",
					DocPath: d.Path, Path: path,
					Message: fmt.Sprintf("array too long (%d>%d)", len(x), v.opts.MaxArrayLen),
				})
// return nil

			}
			for i := 0; i < len(x); i++ {
				ip := joinIndexPath(path, i)
if err := walk(ip, x[i], depth+1); err != nil {
					return err

				}
			}
			return nil

		case string:
			if v.opts.CheckNullBytes && strings.IndexByte(x, 0x00) >= 0 {
				r.addIssue(Issue{
					Severity: SevError, Code: "string.null_byte",
					DocPath: d.Path, Path: path,
					Message: "string contains null byte",
				})

			}
			if len(x) > v.opts.MaxStringBytes {
				r.addIssue(Issue{
					Severity: SevError, Code: "limits.string_bytes",
					DocPath: d.Path, Path: path,
					Message: fmt.Sprintf("string too large (%d>%d)", len(x), v.opts.MaxStringBytes),
				})
// return nil

			}
			if v.opts.CheckEnvPlaceholders {
				if v.reEnv1.MatchString(x) || v.reEnv2.MatchString(x) {
					r.addIssue(Issue{
						Severity: SevWarn, Code: "pattern.env_placeholder",
						DocPath: d.Path, Path: path,
						Message: "string contains env-style placeholder; expansion is not supported here",
					})

				}
			}
			return nil

		case json.Number:
			if len(x.String()) > 64 {
				r.addIssue(Issue{
					Severity: SevWarn, Code: "number.huge",
					DocPath: d.Path, Path: path,
					Message: "number has very large textual representation",
				})

			}
			return nil

		case bool, nil:
			return nil

		case float64:
			r.addIssue(Issue{
				Severity: SevWarn, Code: "number.float64",
				DocPath: d.Path, Path: path,
				Message: "float64 encountered (expected json.Number); consider decoding with UseNumber",
			})
// return nil

		default:
			r.addIssue(Issue{
				Severity: SevError, Code: "type.unsupported",
				DocPath: d.Path, Path: path,
				Message: fmt.Sprintf("unsupported value type %T", val),
			})
// return nil

		}

	}
	if err := walk("$", d.Data, 0); err != nil {
		r.addIssue(Issue{Severity: SevError, Code: "ctx.canceled", DocPath: d.Path, Message: err.Error()})

	}
	return finalizeReport(r)
}
func (v *Validator) newReport() Report {
	return Report{
		GeneratedAt:      time.Now().UTC(),
		ValidatorVersion: ValidatorVersion,
		OptionsHash:      hashOptions(v.opts),
		OptionsSnapshot:  v.opts,
		Issues:           make([]Issue, 0, 32),
	}
}

// addIssue enforces cap and adds a single truncation marker once.
func (r *Report) addIssue(it Issue) {
	if len(r.Issues) >= MaxIssuesPerReport {
		if len(r.Issues) == MaxIssuesPerReport {
			r.Issues = append(r.Issues, Issue{
				Severity: SevWarn,
				Code:     "report.truncated",
				Message:  fmt.Sprintf("issue limit reached (%d); further violations not reported", MaxIssuesPerReport),
			})

		}
		return

	}
	if len(it.Path) > MaxPathLen {
		it.Path = it.Path[:MaxPathLen] + "..."

	}
	r.Issues = append(r.Issues, it)
}
func hashOptions(o VOptions) string {
	b, err := json.Marshal(o)
if err != nil {
		// Fallback: hash the fmt string (still deterministic enough for diagnostics)
fb := []byte(fmt.Sprintf("%+v", o))
sum := sha256.Sum256(fb)
return hex.EncodeToString(sum[:])

	}
	sum := sha256.Sum256(b)
return hex.EncodeToString(sum[:])
}
func joinPath(base, key string) string {
	var out string
	if base == "" || base == "$" {
		out = "$." + key
	} else {
		out = base + "." + key

	}
	if len(out) > MaxPathLen {
		return out[:MaxPathLen] + "..."

	}
	return out
}
func joinIndexPath(base string, idx int) string {
	out := fmt.Sprintf("%s[%d]", base, idx)
if len(out) > MaxPathLen {
		return out[:MaxPathLen] + "..."

	}
	return out
}
func finalizeReport(r Report) Report {
	sort.SliceStable(r.Issues, func(i, j int) bool {
		a, b := r.Issues[i], r.Issues[j]
		if a.Severity != b.Severity {
			return sevRank(a.Severity) < sevRank(b.Severity)

		}
		if a.Code != b.Code {
			return a.Code < b.Code

		}
		if a.DocPath != b.DocPath {
			return a.DocPath < b.DocPath

		}
		if a.Path != b.Path {
			return a.Path < b.Path

		}
		return a.Message < b.Message
	})
r.Errors, r.Warnings, r.Infos = 0, 0, 0
	for _, it := range r.Issues {
		switch it.Severity {
		case SevError:
			r.Errors++
		case SevWarn:
			r.Warnings++
		default:
			r.Infos++

		}
	}
	return r
}
func sevRank(s Severity) int {
	switch s {
	case SevError:
		return 1
	case SevWarn:
		return 2
	default:
		return 3

	}
}
