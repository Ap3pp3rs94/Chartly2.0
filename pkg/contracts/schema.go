package contracts

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
	"strconv"
	"strings"
	"time"
)

// Store loads and compiles JSON Schemas from a filesystem root.
//
// Supported $ref forms:
//   - "#/defs/Foo"              (in-document)
//   - "other.schema.json#/..."  (relative to schema root)
//
// Disallowed $ref forms:
//   - "http(s)://..."
//   - "file://..."
//   - any absolute path
//
// Security:
// - root is pinned (EvalSymlinks)
// and reads are prevented from escaping it.
// - bounded scanning and size caps prevent IO bombs.
// - ref resolution is bounded by depth, ref count, and compiled node count.

type StoreOptions struct {
	MaxFiles          int   // default 5000
	MaxFileBytes      int64 // default 2 MiB per schema
	MaxRefDepth       int   // default 64
	MaxRefs           int   // default 20000 (total resolved refs)
	MaxCanonicalBytes int64 // default 4 MiB compiled canonical JSON
	MaxCompiledNodes  int   // default 500000 nodes emitted during compilation
}
type Store struct {
	rootAbs string
	opts    StoreOptions
}

// SchemaDoc is a raw loaded schema document.
type SchemaDoc struct {
	Path     string         `json:"path"`      // normalized rel path (slashes)
	LoadedAt time.Time      `json:"loaded_at"` // UTC
	SHA256   string         `json:"sha256"`    // hash of raw bytes
	JSON     map[string]any `json:"json"`      // parsed JSON object
}
type LoadedDocDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// CompiledSchema is a fully inlined schema with all local $ref resolved (bounded).
type CompiledSchema struct {
	RootPath      string            `json:"root_path"`
	LoadedDocs    []LoadedDocDigest `json:"loaded_docs"`    // all schema files referenced (sorted)
	HashSHA256    string            `json:"hash_sha256"`    // hash of CanonicalJSON
	CanonicalJSON []byte            `json:"canonical_json"` // deterministic JSON bytes
	JSON          map[string]any    `json:"json"`           // compiled tree
}

var (
	ErrInvalidRoot       = errors.New("contracts: invalid schema root")
	ErrNotFound          = errors.New("contracts: schema not found")
	ErrPathEscape        = errors.New("contracts: path escapes root")
	ErrTooManyFiles      = errors.New("contracts: too many schema files")
	ErrFileTooLarge      = errors.New("contracts: schema too large")
	ErrInvalidJSON       = errors.New("contracts: invalid json")
	ErrNotJSONObject     = errors.New("contracts: schema must be a JSON object")
	ErrRefNotAllowed     = errors.New("contracts: $ref not allowed")
	ErrRefTooDeep        = errors.New("contracts: $ref depth exceeded")
	ErrTooManyRefs       = errors.New("contracts: too many $ref resolutions")
	ErrRefPointerInvalid = errors.New("contracts: invalid json pointer")
	ErrCanonicalTooLarge = errors.New("contracts: canonical json exceeds max bytes")
	ErrCompiledTooLarge  = errors.New("contracts: compiled schema exceeds max nodes")
)

// Strict JSON number token (no leading '+', no leading zeros except '0').
var jsonNumberToken = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func NewStore(root string, opts StoreOptions) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrInvalidRoot

	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)

	}
	absEval, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)

	}
	info, err := os.Stat(absEval)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: not a directory", ErrInvalidRoot)

	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 5000

	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = 2 * 1024 * 1024

	}
	if opts.MaxRefDepth <= 0 {
		opts.MaxRefDepth = 64

	}
	if opts.MaxRefs <= 0 {
		opts.MaxRefs = 20000

	}
	if opts.MaxCanonicalBytes <= 0 {
		opts.MaxCanonicalBytes = 4 * 1024 * 1024

	}
	if opts.MaxCompiledNodes <= 0 {
		opts.MaxCompiledNodes = 500000

	}
	return &Store{rootAbs: absEval, opts: opts}, nil
}

// List returns all .json files under root as rel paths (sorted).
// Note: this is discovery; it does not guarantee each file is a valid schema object.
func (s *Store) List(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	var out []string
	count := 0

	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr

		}
		if err := ctx.Err(); err != nil {
			return err

		}
		if d.IsDir() {
			return nil

		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".json" {
			return nil

		}
		count++
		if count > s.opts.MaxFiles {
			return ErrTooManyFiles

		}
		absEval, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err

		}
		if !withinRoot(s.rootAbs, absEval) {
			return ErrPathEscape

		}
		out = append(out, relSlash(s.rootAbs, absEval))
		return nil
	}
	if err := filepath.WalkDir(s.rootAbs, walkFn); err != nil {
		return nil, err

	}
	sort.Strings(out)
	return out, nil
}

// Load loads and parses a schema JSON file (must be object).
func (s *Store) Load(ctx context.Context, relPath string) (*SchemaDoc, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	abs, rel, err := s.safeJoin(relPath)
	if err != nil {
		return nil, err

	}
	doc, err := s.readJSONDoc(ctx, abs)
	if err != nil {
		return nil, err

	}
	doc.Path = rel
	return doc, nil
}

// Compile loads a root schema and resolves all local $ref into a single inlined schema (bounded).
func (s *Store) Compile(ctx context.Context, relPath string) (*CompiledSchema, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	_, rootRel, err := s.safeJoin(relPath)
	if err != nil {
		return nil, err

	}
	loaded := map[string]*SchemaDoc{} // rel path -> doc
	loadedOrder := []string{}
	refCount := 0
	nodeBudget := s.opts.MaxCompiledNodes

	loadDoc := func(targetRel string) (*SchemaDoc, error) {
		if d, ok := loaded[targetRel]; ok {
			return d, nil

		}
		abs, rel, err := s.safeJoin(targetRel)
		if err != nil {
			return nil, err

		}
		d, err := s.readJSONDoc(ctx, abs)
		if err != nil {
			return nil, err

		}
		d.Path = rel
		loaded[rel] = d
		loadedOrder = append(loadedOrder, rel)
		return d, nil

	}
	rootDoc, err := loadDoc(rootRel)
	if err != nil {
		return nil, err

	}
	var resolveAny func(curDoc *SchemaDoc, node any, depth int) (any, error)
	resolveAny = func(curDoc *SchemaDoc, node any, depth int) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err

		}
		if depth > s.opts.MaxRefDepth {
			return nil, ErrRefTooDeep

		}
		if nodeBudget <= 0 {
			return nil, ErrCompiledTooLarge

		}
		nodeBudget--

		switch x := node.(type) {
		case map[string]any:
			// $ref handling:
			//   - If $ref exists with NO siblings => replace with resolved target
			//   - If $ref exists WITH siblings  => preserve via allOf:
			//     {"$ref":"X","title":"t"} -> {"allOf":[ <X>, {"title":"t"} ]}
			if refRaw, ok := x["$ref"]; ok {
				refStr, ok := refRaw.(string)
				if !ok {
					return nil, fmt.Errorf("%w: $ref must be string", ErrRefNotAllowed)

				}
				refCount++
				if refCount > s.opts.MaxRefs {
					return nil, ErrTooManyRefs

				}
				resolved, err := s.resolveRef(ctx, curDoc, refStr, loadDoc)
				if err != nil {
					return nil, err

				} // Build sibling constraints (excluding $ref)
				if len(x) == 1 {
					// Pure ref: recurse into resolved node
					return resolveAny(resolved.doc, resolved.node, depth+1)

				}
				sibs := make(map[string]any, len(x)-1)
				keys := make([]string, 0, len(x))
				for k := range x {
					if k == "$ref" {
						continue

					}
					keys = append(keys, k)

				}
				sort.Strings(keys)
				for _, k := range keys {
					vv, err := resolveAny(curDoc, x[k], depth)
					if err != nil {
						return nil, err

					}
					sibs[k] = vv

				} // allOf composition, then continue resolving within it
				allOf := []any{resolved.node, sibs}
				obj := map[string]any{"allOf": allOf}
				return resolveAny(curDoc, obj, depth+1)

			} // Deterministic key order
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)

			}
			sort.Strings(keys)
			out := make(map[string]any, len(keys))
			for _, k := range keys {
				vv, err := resolveAny(curDoc, x[k], depth)
				if err != nil {
					return nil, err

				}
				out[k] = vv

			}
			return out, nil

		case []any:
			out := make([]any, len(x))
			for i := 0; i < len(x); i++ {
				vv, err := resolveAny(curDoc, x[i], depth)
				if err != nil {
					return nil, err

				}
				out[i] = vv

			}
			return out, nil

		default:
			return node, nil

		}

	}
	compiledAny, err := resolveAny(rootDoc, rootDoc.JSON, 0)
	if err != nil {
		return nil, err

	}
	compiledMap, ok := compiledAny.(map[string]any)
	if !ok {
		return nil, ErrNotJSONObject

	}
	canon, err := canonicalJSON(compiledMap, ctx, s.opts.MaxRefDepth, s.opts.MaxCompiledNodes, s.opts.MaxCanonicalBytes)
	if err != nil {
		return nil, err

	}
	paths := append([]string(nil), loadedOrder...)
	sort.Strings(paths)
	docs := make([]LoadedDocDigest, 0, len(paths))
	for _, p := range paths {
		if d, ok := loaded[p]; ok && d != nil {
			docs = append(docs, LoadedDocDigest{Path: p, SHA256: d.SHA256})
		} else {
			docs = append(docs, LoadedDocDigest{Path: p, SHA256: ""})

		}

	}
	return &CompiledSchema{
		RootPath:      rootRel,
		LoadedDocs:    docs,
		HashSHA256:    sha256Hex(canon),
		CanonicalJSON: canon,
		JSON:          compiledMap,
	}, nil
}

// ---- safe path join ----

func (s *Store) safeJoin(relPath string) (abs string, rel string, err error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", "", ErrNotFound

	}
	relClean := filepath.Clean(relPath)
	if filepath.IsAbs(relClean) {
		return "", "", ErrPathEscape

	}
	if relClean == ".." || strings.HasPrefix(relClean, ".."+string(os.PathSeparator)) {
		return "", "", ErrPathEscape

	}
	abs = filepath.Join(s.rootAbs, relClean)
	absEval, e := filepath.EvalSymlinks(abs)
	if e != nil {
		if errors.Is(e, fs.ErrNotExist) {
			return "", "", ErrNotFound

		}
		return "", "", e

	}
	if !withinRoot(s.rootAbs, absEval) {
		return "", "", ErrPathEscape

	}
	rel = relSlash(s.rootAbs, absEval)
	return absEval, rel, nil
}
func withinRoot(rootAbs, targetAbs string) bool {
	root := strings.ToLower(filepath.Clean(rootAbs))
	tgt := strings.ToLower(filepath.Clean(targetAbs))
	if tgt == root {
		return true

	}
	sep := strings.ToLower(string(os.PathSeparator))
	if !strings.HasSuffix(root, sep) {
		root += sep

	}
	return strings.HasPrefix(tgt, root)
}
func relSlash(rootAbs, abs string) string {
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		rel = abs

	}
	rel = filepath.Clean(rel)
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	return rel
}

// ---- JSON reading ----

func (s *Store) readJSONDoc(ctx context.Context, absPath string) (*SchemaDoc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err

	}
	fi, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound

		}
		return nil, err

	}
	if fi.Size() > s.opts.MaxFileBytes {
		return nil, ErrFileTooLarge

	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err

	}
	defer f.Close()
	lr := &io.LimitedReader{R: f, N: s.opts.MaxFileBytes + 1}
	raw, err := io.ReadAll(lr)
	if err != nil {
		return nil, err

	}
	if int64(len(raw)) > s.opts.MaxFileBytes {
		return nil, ErrFileTooLarge

	}
	sum := sha256.Sum256(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)

	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrNotJSONObject

	}
	return &SchemaDoc{
		Path:     "",
		LoadedAt: time.Now().UTC(),
		SHA256:   hex.EncodeToString(sum[:]),
		JSON:     m,
	}, nil
}

// ---- ref resolution ----

type resolvedRef struct {
	doc  *SchemaDoc
	node any
}

func (s *Store) resolveRef(ctx context.Context, curDoc *SchemaDoc, ref string, loadDoc func(string) (*SchemaDoc, error)) (resolvedRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return resolvedRef{}, fmt.Errorf("%w: empty ref", ErrRefNotAllowed)

	}
	lref := strings.ToLower(ref)
	if strings.Contains(lref, "://") || strings.HasPrefix(lref, "file:") {
		return resolvedRef{}, ErrRefNotAllowed

	}
	filePart := ""
	ptrPart := ""
	if strings.HasPrefix(ref, "#") {
		ptrPart = ref
	} else {
		parts := strings.SplitN(ref, "#", 2)
		filePart = parts[0]
		if len(parts) == 2 {
			ptrPart = "#" + parts[1]

		}

	}
	targetDoc := curDoc
	if strings.TrimSpace(filePart) != "" {
		if filepath.IsAbs(filePart) || strings.HasPrefix(filePart, "..") {
			return resolvedRef{}, ErrRefNotAllowed

		}
		d, err := loadDoc(filePart)
		if err != nil {
			return resolvedRef{}, err

		}
		targetDoc = d

	}
	node := any(targetDoc.JSON)
	if ptrPart != "" && ptrPart != "#" {
		n, err := jsonPointerGet(node, ptrPart)
		if err != nil {
			return resolvedRef{}, err

		}
		node = n

	}
	return resolvedRef{doc: targetDoc, node: node}, nil
}
func jsonPointerGet(root any, ptr string) (any, error) {
	if ptr == "" || ptr == "#" {
		return root, nil

	}
	if !strings.HasPrefix(ptr, "#/") {
		return nil, ErrRefPointerInvalid

	}
	path := strings.Split(ptr[2:], "/")
	cur := root

	for _, seg := range path {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[seg]
			if !ok {
				return nil, fmt.Errorf("%w: missing key %q", ErrRefPointerInvalid, seg)

			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, fmt.Errorf("%w: bad index %q", ErrRefPointerInvalid, seg)

			}
			cur = x[i]
		default:
			return nil, fmt.Errorf("%w: cannot traverse %T", ErrRefPointerInvalid, cur)

		}
	}
	return cur, nil
}

// ---- deterministic canonical JSON for hashing ----

func canonicalJSON(root any, ctx context.Context, maxDepth, maxNodes int, maxOutBytes int64) ([]byte, error) {
	var buf bytes.Buffer
	nodeBudget := maxNodes

	write := func(b []byte) error {
		if maxOutBytes > 0 && int64(buf.Len()+len(b)) > maxOutBytes {
			return ErrCanonicalTooLarge

		}
		_, _ = buf.Write(b)
		return nil
	}
	var enc func(path string, v any, depth int) error
	enc = func(path string, v any, depth int) error {
		if err := ctx.Err(); err != nil {
			return err

		}
		if depth > maxDepth || nodeBudget <= 0 {
			return write([]byte("null"))

		}
		nodeBudget--

		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)

			}
			sort.Strings(keys)
			if err := write([]byte("{")); err != nil {
				return err
			}
			for i, k := range keys {
				if i > 0 {
					if err := write([]byte(",")); err != nil {
						return err
					}
				}
				ks, err := json.Marshal(k)
				if err != nil {
					return err
				}
				if err := write(ks); err != nil {
					return err
				}
				if err := write([]byte(":")); err != nil {
					return err
				}
				if err := enc(path+"."+k, x[k], depth+1); err != nil {
					return err
				}

			}
			return write([]byte("}"))
		case []any:
			if err := write([]byte("[")); err != nil {
				return err
			}
			for i := 0; i < len(x); i++ {
				if i > 0 {
					if err := write([]byte(",")); err != nil {
						return err
					}
				}
				if err := enc(fmt.Sprintf("%s[%d]", path, i), x[i], depth+1); err != nil {
					return err
				}

			}
			return write([]byte("]"))
		case string:
			b, err := json.Marshal(x)
			if err != nil {
				return err
			}
			return write(b)
		case json.Number:
			s := strings.TrimSpace(x.String())
			if !jsonNumberToken.MatchString(s) {
				return write([]byte("null"))

			}
			return write([]byte(s))
		case bool:
			if x {
				return write([]byte("true"))
			}
			return write([]byte("false"))
		case nil:
			return write([]byte("null"))
		default:
			return write([]byte("null"))

		}

	}
	if err := enc("$", root, 0); err != nil {
		return nil, err

	}
	return buf.Bytes(), nil
}
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
