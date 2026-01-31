package blob

// Artifact manager (deterministic, multi-tenant safe).
//
// This file provides a small orchestration layer over a pluggable Store interface,
// enabling consistent object-key generation and content-addressable storage patterns.
//
// ObjectKey format (deterministic):
//   <Prefix>/<tenantID>/sha256/<first2>/<next2>/<sha256>.bin
//
// Notes:
//   - Library-only: no HTTP handlers, no filesystem writes.
//   - No direct networking; all I/O is performed by the injected Store.
//   - Determinism: same bytes => same SHA256 => same object key.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrArtifact         = errors.New("artifact failed")
	ErrArtifactInvalid  = errors.New("artifact invalid")
	ErrArtifactTooLarge = errors.New("artifact too large")
	ErrArtifactConflict = errors.New("artifact conflict")
	ErrArtifactStore    = errors.New("artifact store error")
)

type Store interface {
	Put(ctx context.Context, tenantID, objectKey, contentType string, data []byte, meta map[string]string) error
	Get(ctx context.Context, tenantID, objectKey string) (contentType string, data []byte, meta map[string]string, err error)
	Head(ctx context.Context, tenantID, objectKey string) (contentType string, bytes int64, meta map[string]string, err error)
	Delete(ctx context.Context, tenantID, objectKey string) error
}

type ManagerOptions struct {
	Prefix         string
	MaxBytes       int64
	AllowOverwrite bool
}

type ArtifactRef struct {
	ObjectKey   string
	ContentType string
	Bytes       int64
	SHA256      string
	Meta        map[string]string
}

type Manager struct {
	store Store
	opts  ManagerOptions
}

func NewManager(store Store, opts ManagerOptions) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is nil", ErrArtifactInvalid)
	}
	o := normalizeManagerOptions(opts)
	return &Manager{store: store, opts: o}, nil
}

func (m *Manager) Put(ctx context.Context, tenantID string, contentType string, data []byte, meta map[string]string) (ArtifactRef, error) {
	tenantID = norm(tenantID)
	contentType = strings.TrimSpace(contentType)

	if tenantID == "" || contentType == "" {
		return ArtifactRef{}, fmt.Errorf("%w: tenantID/contentType required", ErrArtifactInvalid)
	}
	if data == nil {
		data = []byte{}
	}
	if m.opts.MaxBytes > 0 && int64(len(data)) > m.opts.MaxBytes {
		return ArtifactRef{}, fmt.Errorf("%w: max bytes exceeded", ErrArtifactTooLarge)
	}

	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	objKey := objectKeyFor(m.opts.Prefix, tenantID, shaHex)

	nmeta := normalizeMeta(meta)

	if !m.opts.AllowOverwrite {
		if _, _, _, err := m.store.Head(ctx, tenantID, objKey); err == nil {
			return ArtifactRef{}, fmt.Errorf("%w: %s", ErrArtifactConflict, objKey)
		}
		// If Head errors, proceed and let store decide.
	}

	if err := m.store.Put(ctx, tenantID, objKey, contentType, data, nmeta); err != nil {
		return ArtifactRef{}, fmt.Errorf("%w: %w: %v", ErrArtifact, ErrArtifactStore, err)
	}

	return ArtifactRef{
		ObjectKey:   objKey,
		ContentType: contentType,
		Bytes:       int64(len(data)),
		SHA256:      shaHex,
		Meta:        copyMeta(nmeta),
	}, nil
}

func (m *Manager) Get(ctx context.Context, tenantID, objectKey string) (ArtifactRef, []byte, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)

	if tenantID == "" || objectKey == "" {
		return ArtifactRef{}, nil, fmt.Errorf("%w: tenantID/objectKey required", ErrArtifactInvalid)
	}

	ct, data, meta, err := m.store.Get(ctx, tenantID, objectKey)
	if err != nil {
		return ArtifactRef{}, nil, fmt.Errorf("%w: %w: %v", ErrArtifact, ErrArtifactStore, err)
	}
	if data == nil {
		data = []byte{}
	}

	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	return ArtifactRef{
		ObjectKey:   objectKey,
		ContentType: strings.TrimSpace(ct),
		Bytes:       int64(len(data)),
		SHA256:      shaHex,
		Meta:        copyMeta(normalizeMeta(meta)),
	}, data, nil
}

func (m *Manager) Delete(ctx context.Context, tenantID, objectKey string) error {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)

	if tenantID == "" || objectKey == "" {
		return fmt.Errorf("%w: tenantID/objectKey required", ErrArtifactInvalid)
	}

	if err := m.store.Delete(ctx, tenantID, objectKey); err != nil {
		return fmt.Errorf("%w: %w: %v", ErrArtifact, ErrArtifactStore, err)
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Key derivation + normalization
////////////////////////////////////////////////////////////////////////////////

func objectKeyFor(prefix, tenantID, shaHex string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = "artifacts"
	}

	tenantID = norm(tenantID)
	shaHex = strings.ToLower(strings.TrimSpace(shaHex))

	a := "00"
	b := "00"
	if len(shaHex) >= 2 {
		a = shaHex[:2]
	}
	if len(shaHex) >= 4 {
		b = shaHex[2:4]
	}

	return prefix + "/" + tenantID + "/sha256/" + a + "/" + b + "/" + shaHex + ".bin"
}

func normalizeManagerOptions(opts ManagerOptions) ManagerOptions {
	o := opts
	o.Prefix = strings.TrimSpace(o.Prefix)
	if o.Prefix == "" {
		o.Prefix = "artifacts"
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 64 * 1024 * 1024
	}

	// Default allow overwrite = true
	o.AllowOverwrite = opts.AllowOverwrite
	return o
}

func normalizeMeta(meta map[string]string) map[string]string {
	if meta == nil || len(meta) == 0 {
		return map[string]string{}
	}

	keys := make([]string, 0, len(meta))
	tmp := make(map[string]string, len(meta))
	for k, v := range meta {
		nk := normCollapse(k)
		if nk == "" {
			continue
		}
		nv := normCollapse(v)
		tmp[nk] = nv
	}
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}

	return out
}

func copyMeta(meta map[string]string) map[string]string {
	if meta == nil {
		return map[string]string{}
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = meta[k]
	}
	return out
}

func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func normCollapse(s string) string {
	s = norm(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
