package errors

import (
	"bytes"
	"encoding/json"
	"sort"
)

// Code is a stable error code shared across all Chartly services.
// Once published, codes should be treated as API-stable.
// type Code string

// CodeMeta provides metadata useful for HTTP mapping, retry decisions, and documentation.
type CodeMeta struct {
	HTTPStatus  int    `json:"http_status"`
	Retryable   bool   `json:"retryable"`
	Kind        string `json:"kind"`        // client|server|security|dependency
	Description string `json:"description"` // human description
}

// ---- AUTH ----
const (
	AuthUnauthorized Code = "auth.unauthorized"
	AuthForbidden    Code = "auth.forbidden"
	AuthTokenInvalid Code = "auth.token_invalid"
	AuthTokenExpired Code = "auth.token_expired"
	AuthMFARequired  Code = "auth.mfa_required"
)

// ---- TENANCY ----
const (
	TenantMissing   Code = "tenancy.missing"
	TenantInvalid   Code = "tenancy.invalid"
	TenantForbidden Code = "tenancy.forbidden"
)

// ---- CONTRACTS / VALIDATION ----
const (
	ContractsInvalid        Code = "contracts.invalid"
	ContractsSchemaNotFound Code = "contracts.schema_not_found"
	ContractsRefNotAllowed  Code = "contracts.ref_not_allowed"
)

// ---- PROFILES ----
const (
	ProfilesInvalid     Code = "profiles.invalid"
	ProfilesNotFound    Code = "profiles.not_found"
	ProfilesUnsupported Code = "profiles.unsupported"
)

// ---- CONFIG ----
const (
	ConfigInvalid     Code = "config.invalid"
	ConfigNotFound    Code = "config.not_found"
	ConfigUnsupported Code = "config.unsupported"
)

// ---- QUEUE ----
const (
	QueueEmpty    Code = "queue.empty"
	QueueClosed   Code = "queue.closed"
	QueueTimeout  Code = "queue.timeout"
	QueueOversize Code = "queue.oversize"
	QueueConflict Code = "queue.conflict"
)

// ---- STORAGE ----
const (
	StorageNotFound    Code = "storage.not_found"
	StorageConflict    Code = "storage.conflict"
	StorageOversize    Code = "storage.oversize"
	StorageUnavailable Code = "storage.unavailable"
)

// ---- RATE LIMIT ----
const (
	RateLimitExceeded Code = "rate_limit.exceeded"
)

// ---- AUDIT ----
const (
	AuditRejected Code = "audit.rejected"
	AuditInvalid  Code = "audit.invalid"
)

// ---- INTERNAL ----
const (
	Internal        Code = "internal"
	InternalTimeout Code = "internal.timeout"
	DependencyDown  Code = "dependency.down"
)

// registry is intentionally unexported; use Meta/Known/List/ExportJSON.
var registry = map[Code]CodeMeta{
	// auth
	AuthUnauthorized: {HTTPStatus: 401, Retryable: false, Kind: "security", Description: "missing or invalid credentials"},
	AuthForbidden:    {HTTPStatus: 403, Retryable: false, Kind: "security", Description: "authenticated but not authorized"},
	AuthTokenInvalid: {HTTPStatus: 401, Retryable: false, Kind: "security", Description: "token invalid"},
	AuthTokenExpired: {HTTPStatus: 401, Retryable: false, Kind: "security", Description: "token expired"},
	AuthMFARequired:  {HTTPStatus: 403, Retryable: false, Kind: "security", Description: "mfa required"},

	// tenancy
	TenantMissing:   {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "tenant header missing"},
	TenantInvalid:   {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "tenant invalid"},
	TenantForbidden: {HTTPStatus: 403, Retryable: false, Kind: "security", Description: "tenant forbidden"},

	// contracts
	ContractsInvalid:        {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "payload failed contract validation"},
	ContractsSchemaNotFound: {HTTPStatus: 404, Retryable: false, Kind: "client", Description: "schema not found"},
	ContractsRefNotAllowed:  {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "schema $ref not allowed"},

	// profiles
	ProfilesInvalid:     {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "profile invalid"},
	ProfilesNotFound:    {HTTPStatus: 404, Retryable: false, Kind: "client", Description: "profile not found"},
	ProfilesUnsupported: {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "profile unsupported"},

	// config
	ConfigInvalid:     {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "config invalid"},
	ConfigNotFound:    {HTTPStatus: 404, Retryable: false, Kind: "client", Description: "config not found"},
	ConfigUnsupported: {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "config unsupported"},

	// queue
	QueueEmpty:    {HTTPStatus: 204, Retryable: true, Kind: "dependency", Description: "queue empty"},
	QueueClosed:   {HTTPStatus: 503, Retryable: true, Kind: "dependency", Description: "queue closed"},
	QueueTimeout:  {HTTPStatus: 504, Retryable: true, Kind: "dependency", Description: "queue timeout"},
	QueueOversize: {HTTPStatus: 413, Retryable: false, Kind: "client", Description: "message too large"},
	QueueConflict: {HTTPStatus: 409, Retryable: true, Kind: "dependency", Description: "message lease conflict"},

	// storage
	StorageNotFound:    {HTTPStatus: 404, Retryable: false, Kind: "client", Description: "object not found"},
	StorageConflict:    {HTTPStatus: 409, Retryable: true, Kind: "dependency", Description: "write conflict"},
	StorageOversize:    {HTTPStatus: 413, Retryable: false, Kind: "client", Description: "object too large"},
	StorageUnavailable: {HTTPStatus: 503, Retryable: true, Kind: "dependency", Description: "storage unavailable"},

	// rate limit
	RateLimitExceeded: {HTTPStatus: 429, Retryable: true, Kind: "client", Description: "rate limit exceeded"},

	// audit
	AuditRejected: {HTTPStatus: 403, Retryable: false, Kind: "security", Description: "audit policy rejected action"},
	AuditInvalid:  {HTTPStatus: 400, Retryable: false, Kind: "client", Description: "audit record invalid"},

	// internal
	Internal:        {HTTPStatus: 500, Retryable: true, Kind: "server", Description: "internal error"},
	InternalTimeout: {HTTPStatus: 504, Retryable: true, Kind: "server", Description: "internal timeout"},
	DependencyDown:  {HTTPStatus: 503, Retryable: true, Kind: "dependency", Description: "dependency unavailable"},
}

// Meta returns metadata for a code.
func Meta(code Code) (CodeMeta, bool) {
	m, ok := registry[code]
	return m, ok
}
func Known(code Code) bool {
	_, ok := registry[code]
	return ok
}

// List returns all known codes sorted.
func List() []Code {
	out := make([]Code, 0, len(registry))
	for k := range registry {
		out = append(out, k)

	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	// return out
}

// ExportJSON returns stable JSON of all codes + meta.
func ExportJSON() []byte {
	type row struct {
		Code Code     `json:"code"`
		Meta CodeMeta `json:"meta"`
	}
	codes := List()
	rows := make([]row, 0, len(codes))
	for _, c := range codes {
		rows = append(rows, row{Code: c, Meta: registry[c]})

	}
	b, err := json.Marshal(rows)
	if err != nil {
		return []byte("[]")

	} // Ensure newline-free stable bytes.
	var buf bytes.Buffer
	_, _ = buf.Write(b)
	return buf.Bytes()
}
