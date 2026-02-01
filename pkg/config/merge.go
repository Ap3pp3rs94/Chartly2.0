package config

import (
"fmt"
"sort"
"strings"
)

// Deterministic deep merge utilities for config trees (map[string]any).
//
// Semantics (best practice):
// - Later layers win: src overrides dst.
// - map + map => recursive merge.
// - array policy is configurable, default Replace (recommended).
// - scalar => replace.
//
// This file intentionally uses unique symbol names to avoid collisions with loader.go helpers.

type ArrayPolicy string

const (
ArrayReplace ArrayPolicy = "replace"
ArrayConcat  ArrayPolicy = "concat" // optional; bounded
)

type MergeOptions struct {
// MaxDepth bounds recursion. When exceeded, src subtree replaces dst subtree and a warning is recorded.
// Default: 32
MaxDepth int

// MaxNodes bounds total visited nodes (map entries + array items + scalar leaves).
// Default: 250000
MaxNodes int

// MaxWarnings bounds report growth.
// Default: 64
MaxWarnings int

// ArrayPolicy controls how arrays are handled.
// Default: replace
ArrayPolicy ArrayPolicy

// MaxConcatLen bounds ArrayConcat result length.
// Default: 50000
MaxConcatLen int
}

type MergeWarning struct {
Code string `json:"code"`
Path string `json:"path,omitempty"` // json-ish path: $.a.b[0]
Msg  string `json:"msg"`
}

type MergeReport struct {
Warnings []MergeWarning `json:"warnings,omitempty"`
Nodes    int           `json:"nodes"`
DepthHit int           `json:"depth_hit"` // number of times depth cap triggered
}

func (r MergeReport) HasWarnings() bool { return len(r.Warnings) > 0 }

func (r *MergeReport) warn(opts MergeOptions, code, path, msg string) {
if opts.MaxWarnings > 0 && len(r.Warnings) >= opts.MaxWarnings {
return

}r.Warnings = append(r.Warnings, MergeWarning{
Code: strings.TrimSpace(code),
Path: strings.TrimSpace(path),
Msg:  strings.TrimSpace(msg),
})
}

func defaultMergeOptions(opts MergeOptions) MergeOptions {
if opts.MaxDepth <= 0 {
opts.MaxDepth = 32

}if opts.MaxNodes <= 0 {
opts.MaxNodes = 250000

}if opts.MaxWarnings <= 0 {
opts.MaxWarnings = 64

}if opts.ArrayPolicy == "" {
opts.ArrayPolicy = ArrayReplace

}if opts.MaxConcatLen <= 0 {
opts.MaxConcatLen = 50000

}return opts
}

// Merge merges src into dst deterministically and returns a new map.
func Merge(dst, src map[string]any, opts MergeOptions) (map[string]any, MergeReport) {
opts = defaultMergeOptions(opts)
rep := MergeReport{Warnings: make([]MergeWarning, 0, 8)}

out := make(map[string]any, len(dst))
for k, v := range dst {
out[k] = v


}nodeBudget := opts.MaxNodes
merged := mergeMap(out, src, "$", 0, &nodeBudget, opts, &rep)
rep.Nodes = opts.MaxNodes - nodeBudget
return merged, rep
}

// MergeMany folds layers in order: layers[0] then layers[1] then ...
// Later layers win.
func MergeMany(layers []map[string]any, opts MergeOptions) (map[string]any, MergeReport) {
opts = defaultMergeOptions(opts)
rep := MergeReport{Warnings: make([]MergeWarning, 0, 16)}

out := map[string]any{}
nodeBudget := opts.MaxNodes

for i := 0; i < len(layers); i++ {
if layers[i] == nil {
continue

}out = mergeMap(out, layers[i], "$", 0, &nodeBudget, opts, &rep)


}rep.Nodes = opts.MaxNodes - nodeBudget
return out, rep
}

func mergeMap(dst, src map[string]any, path string, depth int, nodeBudget *int, opts MergeOptions, rep *MergeReport) map[string]any {
if *nodeBudget <= 0 {
rep.warn(opts, "limits.nodes", path, fmt.Sprintf("max nodes exceeded (%d)", opts.MaxNodes))
return src

}*nodeBudget--

if depth >= opts.MaxDepth {
rep.DepthHit++
rep.warn(opts, "limits.depth", path, fmt.Sprintf("max depth exceeded (%d); subtree replaced", opts.MaxDepth))
return src


}if dst == nil {
dst = map[string]any{}

}if src == nil {
return dst


}keys := make([]string, 0, len(src))
for k := range src {
keys = append(keys, k)

}sort.Strings(keys)

// copy-on-write for determinism and isolation
out := make(map[string]any, len(dst)+len(src))
for k, v := range dst {
out[k] = v


}for _, k := range keys {
sv := src[k]
dv, exists := out[k]
if !exists {
out[k] = sv
continue

}out[k] = mergeValue(dv, sv, joinPath(path, k), depth+1, nodeBudget, opts, rep)


}return out
}

func mergeValue(dst any, src any, path string, depth int, nodeBudget *int, opts MergeOptions, rep *MergeReport) any {
if *nodeBudget <= 0 {
rep.warn(opts, "limits.nodes", path, fmt.Sprintf("max nodes exceeded (%d)", opts.MaxNodes))
return src

}*nodeBudget--

// Depth guard: replace
if depth >= opts.MaxDepth {
rep.DepthHit++
rep.warn(opts, "limits.depth", path, fmt.Sprintf("max depth exceeded (%d); value replaced", opts.MaxDepth))
return src


}// map + map => recurse
dm, dok := dst.(map[string]any)
sm, sok := src.(map[string]any)
if dok && sok {
return mergeMap(dm, sm, path, depth, nodeBudget, opts, rep)


}// arrays
da, daok := dst.([]any)
sa, saok := src.([]any)
if daok && saok {
switch opts.ArrayPolicy {
case ArrayConcat:
// bounded concat
max := opts.MaxConcatLen
if max <= 0 {
max = 50000

}out := make([]any, 0, minInt(len(da)+len(sa), max))
out = append(out, da...)
if len(out) > max {
out = out[:max]

}remain := max - len(out)
if remain > 0 {
if len(sa) > remain {
out = append(out, sa[:remain]...)
rep.warn(opts, "array.concat_truncated", path, "array concat truncated to max length")
} else {
out = append(out, sa...)

}} else {
rep.warn(opts, "array.concat_truncated", path, "array concat truncated to max length")

}return out
default:
// replace
return src

}

}// Type conflict: replace with warning (optional)
if fmt.Sprintf("%T", dst) != fmt.Sprintf("%T", src) {
rep.warn(opts, "type.replace", path, fmt.Sprintf("type changed %T -> %T (replaced)", dst, src))


}return src
}

func joinPath(base, key string) string {
if base == "" || base == "$" {
return "$." + key

}return base + "." + key
}

func minInt(a, b int) int {
if a < b {
return a

}return b
}
