package idempotency

import (
"bytes"
"crypto/sha256"
"encoding/hex"
"encoding/json"
"errors"
"fmt"
"sort"
"strconv"
"strings"
)

const (
KeyVersion = "v1"

MaxTenantLen = 64
MaxScopeLen  = 32
MaxKeyLen    = 256

MaxParts = 32
MaxBytes = 32 * 1024 // 32 KiB input cap for hashing
)

var (
ErrInvalidKey   = errors.New("idempotency: invalid key")
ErrInputTooBig  = errors.New("idempotency: input too big")
ErrInvalidScope = errors.New("idempotency: invalid scope")
)

// KeyParts is the parsed representation.
type KeyParts struct {
Version string `json:"version"`
Tenant  string `json:"tenant"`
Scope   string `json:"scope"`
Hash    string `json:"hash"` // lowercase hex sha256
}

// BuildKey computes a deterministic key for a tenant+scope from ordered parts.
// Parts are encoded deterministically and hashed with SHA-256.
func BuildKey(tenant, scope string, parts ...any) (string, error) {
tenant = normalizeTenant(tenant)
scope, err := normalizeScope(scope)
if err != nil {
return "", err


}if len(parts) > MaxParts {
return "", ErrInputTooBig


}b, err := encodeDeterministic(parts)
if err != nil {
return "", err

}if len(b) > MaxBytes {
return "", ErrInputTooBig


}sum := sha256.Sum256(b)
hash := hex.EncodeToString(sum[:])
key := fmt.Sprintf("%s:%s:%s:%s", KeyVersion, tenant, scope, hash)
if len(key) > MaxKeyLen {
return "", ErrInvalidKey

}return key, nil
}

// BuildKeyFromMap computes a deterministic key from a map by sorting keys.
// Useful when you have named inputs rather than ordered parts.
func BuildKeyFromMap(tenant, scope string, m map[string]any) (string, error) {
if m == nil {
return BuildKey(tenant, scope, nil)

}keys := make([]string, 0, len(m))
for k := range m {
keys = append(keys, strings.ToLower(strings.TrimSpace(k)))

}sort.Strings(keys)
parts := make([]any, 0, len(keys)*2)
for _, k := range keys {
if k == "" {
continue

}parts = append(parts, k)
parts = append(parts, m[k])

}return BuildKey(tenant, scope, parts...)
}

// ParseKey parses "v1:<tenant>:<scope>:<sha256hex>".
func ParseKey(key string) (KeyParts, error) {
key = strings.TrimSpace(key)
if key == "" || len(key) > MaxKeyLen {
return KeyParts{}, ErrInvalidKey

}parts := strings.Split(key, ":")
if len(parts) != 4 {
return KeyParts{}, ErrInvalidKey

}v := parts[0]
tenant := parts[1]
scope := parts[2]
hash := parts[3]

if v != KeyVersion {
return KeyParts{}, ErrInvalidKey

}if err := validateTenant(tenant); err != nil {
return KeyParts{}, err

}nscope, err := normalizeScope(scope)
if err != nil {
return KeyParts{}, err

}if hash == "" || len(hash) != 64 || !isLowerHex(hash) {
return KeyParts{}, ErrInvalidKey

}return KeyParts{Version: v, Tenant: tenant, Scope: nscope, Hash: hash}, nil
}

// ValidateKey checks format and returns nil if valid.
func ValidateKey(key string) error {
_, err := ParseKey(key)
return err
}

// ---- normalization/validation ----

func normalizeTenant(t string) string {
t = strings.ToLower(strings.TrimSpace(t))
if t == "" {
return "local"

}if len(t) > MaxTenantLen {
t = t[:MaxTenantLen]

}// allow [a-z0-9_-]
out := make([]rune, 0, len(t))
for _, r := range t {
if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
out = append(out, r)

}
}if len(out) == 0 {
return "local"

}return string(out)
}

func validateTenant(t string) error {
if t == "" || len(t) > MaxTenantLen {
return ErrInvalidKey

}for _, r := range t {
if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
continue

}return ErrInvalidKey

}return nil
}

func normalizeScope(s string) (string, error) {
s = strings.ToLower(strings.TrimSpace(s))
if s == "" || len(s) > MaxScopeLen {
return "", ErrInvalidScope

}for _, r := range s {
if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
continue

}return "", ErrInvalidScope

}return s, nil
}

func isLowerHex(s string) bool {
for _, r := range s {
if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
continue

}return false

}return true
}

// ---- deterministic encoder ----
//
// We avoid json.Marshal(map) nondeterminism by writing canonical JSON-like bytes:
// - maps: keys sorted; values recursively encoded
// - slices: order preserved
// - strings: JSON-escaped
// - numbers: json.Number as token; floats via strconv; ints via decimal
// - bool/null supported
//
// This encoder is intended for hashing, not for user-facing serialization.

func encodeDeterministic(parts []any) ([]byte, error) {
var buf bytes.Buffer
if err := encAny(&buf, parts); err != nil {
return nil, err

}return buf.Bytes(), nil
}

func encAny(buf *bytes.Buffer, v any) error {
switch x := v.(type) {
case nil:
buf.WriteString("null")
return nil
case bool:
if x {
buf.WriteString("true")
} else {
buf.WriteString("false")

}return nil
case string:
b, _ := json.Marshal(x)
buf.Write(b)
return nil
case []byte:
// encode bytes as base16 string to be deterministic
buf.WriteByte('"')
buf.WriteString(hex.EncodeToString(x))
buf.WriteByte('"')
return nil
case int:
buf.WriteString(strconv.FormatInt(int64(x), 10))
return nil
case int64:
buf.WriteString(strconv.FormatInt(x, 10))
return nil
case uint:
buf.WriteString(strconv.FormatUint(uint64(x), 10))
return nil
case uint64:
buf.WriteString(strconv.FormatUint(x, 10))
return nil
case float64:
buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
return nil
case json.Number:
s := strings.TrimSpace(x.String())
if s == "" {
buf.WriteString("null")
return nil

}buf.WriteString(s)
return nil
case []any:
buf.WriteByte('[')
for i := 0; i < len(x); i++ {
if i > 0 {
buf.WriteByte(',')

}if err := encAny(buf, x[i]); err != nil {
return err

}
}buf.WriteByte(']')
return nil
case map[string]any:
keys := make([]string, 0, len(x))
for k := range x {
keys = append(keys, strings.ToLower(strings.TrimSpace(k)))

}sort.Strings(keys)
buf.WriteByte('{')
first := true
for _, k := range keys {
if k == "" {
continue

}if !first {
buf.WriteByte(',')

}first = false
kb, _ := json.Marshal(k)
buf.Write(kb)
buf.WriteByte(':')
if err := encAny(buf, x[k]); err != nil {
return err

}
}buf.WriteByte('}')
return nil
case map[string]string:
keys := make([]string, 0, len(x))
for k := range x {
keys = append(keys, strings.ToLower(strings.TrimSpace(k)))

}sort.Strings(keys)
buf.WriteByte('{')
for i, k := range keys {
if i > 0 {
buf.WriteByte(',')

}kb, _ := json.Marshal(k)
vb, _ := json.Marshal(x[k])
buf.Write(kb)
buf.WriteByte(':')
buf.Write(vb)

}buf.WriteByte('}')
return nil
default:
// Fallback: JSON marshal (best-effort). If it fails, error.
b, err := json.Marshal(x)
if err != nil {
return err

}buf.Write(b)
return nil

}}
