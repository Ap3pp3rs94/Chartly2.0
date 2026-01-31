package cache

// Redis cache client (RESP2)  standard library only.
//
// This file implements a minimal Redis cache wrapper suitable for the storage service.
// It intentionally avoids third-party dependencies by speaking RESP2 over net.Conn.
//
// Design goals:
//   - Production-grade defensive behavior: strict parsing, timeouts, typed errors.
//   - Deterministic operation: no randomness, stable RESP encoding.
//   - Multi-tenant safety: all keys are namespaced by tenant_id + configured prefix.
//   - Simplicity: dial-per-operation by default; optional single-connection reuse guarded by a mutex.
//
// Supported commands:
//   - PING
//   - GET
//   - SET key value PX <ms>
//   - DEL key [key...]
//   - MGET key [key...]
//
// Notes on determinism:
//   - Redis itself is not "deterministic" as a system (time/eviction), but the wire encoding and
//     internal key derivation is stable and reproducible.
//   - Outputs that are maps are returned as map[string][]byte; callers should sort keys when iterating.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrCache         = errors.New("cache failed")
	ErrCacheConn     = errors.New("cache connection failed")
	ErrCacheProtocol = errors.New("cache protocol error")
	ErrCacheTimeout  = errors.New("cache timeout")
	ErrCacheInvalid  = errors.New("cache invalid input")
)

type Options struct {
	Addr         string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	DefaultTTL   time.Duration
	KeyPrefix    string

	// If true, reuse a single connection guarded by a mutex.
	// Default false (dial per op).
	ReuseConn bool
}

type RedisCache struct {
	opts Options

	mu   sync.Mutex
	conn net.Conn
	rw   *bufio.ReadWriter
}

func NewRedisCache(opts Options) *RedisCache {
	o := normalizeOptions(opts)
	return &RedisCache{opts: o}
}

func (c *RedisCache) Ping(ctx context.Context) error {
	_, err := c.do(ctx, []string{"PING"})
	return err
}

func (c *RedisCache) Get(ctx context.Context, tenantID, key string) ([]byte, bool, error) {
	tenantID = norm(tenantID)
	key = norm(key)

	if tenantID == "" || key == "" {
		return nil, false, fmt.Errorf("%w: %w: tenantID/key required", ErrCache, ErrCacheInvalid)
	}

	full := c.fullKey(tenantID, key)

	v, err := c.do(ctx, []string{"GET", full})
	if err != nil {
		return nil, false, err
	}

	if v.kind == respNil {
		return nil, false, nil
	}
	if v.kind != respBulk {
		return nil, false, fmt.Errorf("%w: %w: expected bulk reply", ErrCache, ErrCacheProtocol)
	}

	return v.bulk, true, nil
}

func (c *RedisCache) Set(ctx context.Context, tenantID, key string, value []byte, ttl time.Duration) error {
	tenantID = norm(tenantID)
	key = norm(key)

	if tenantID == "" || key == "" {
		return fmt.Errorf("%w: %w: tenantID/key required", ErrCache, ErrCacheInvalid)
	}

	if value == nil {
		value = []byte{}
	}

	if ttl <= 0 {
		ttl = c.opts.DefaultTTL
	}
	if ttl <= 0 {
		// Default to 30s if still unset.
		ttl = 30 * time.Second
	}

	ms := ttl.Milliseconds()
	if ms <= 0 {
		ms = 1
	}

	full := c.fullKey(tenantID, key)

	// SET key value PX <ms>
	_, err := c.do(ctx, []string{"SET", full, string(value), "PX", strconv.FormatInt(ms, 10)})
	return err
}

func (c *RedisCache) Del(ctx context.Context, tenantID string, keys ...string) (int, error) {
	tenantID = norm(tenantID)
	if tenantID == "" {
		return 0, fmt.Errorf("%w: %w: tenantID required", ErrCache, ErrCacheInvalid)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	args := make([]string, 0, 1+len(keys))
	args = append(args, "DEL")
	for _, k := range keys {
		k = norm(k)
		if k == "" {
			continue
		}
		args = append(args, c.fullKey(tenantID, k))
	}

	if len(args) == 1 {
		return 0, nil
	}

	v, err := c.do(ctx, args)
	if err != nil {
		return 0, err
	}
	if v.kind != respInt {
		return 0, fmt.Errorf("%w: %w: expected int reply", ErrCache, ErrCacheProtocol)
	}

	return int(v.i), nil
}

// GetMulti returns a map of key->value for keys that exist.
// Map iteration order is not deterministic; callers should sort keys when iterating.
func (c *RedisCache) GetMulti(ctx context.Context, tenantID string, keys []string) (map[string][]byte, error) {
	tenantID = norm(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("%w: %w: tenantID required", ErrCache, ErrCacheInvalid)
	}
	if len(keys) == 0 {
		return map[string][]byte{}, nil
	}

	// Deterministic command construction: operate on a sorted copy of keys.
	cp := make([]string, 0, len(keys))
	for _, k := range keys {
		k = norm(k)
		if k == "" {
			continue
		}
		cp = append(cp, k)
	}
	sort.Strings(cp)

	args := make([]string, 0, 1+len(cp))
	args = append(args, "MGET")
	for _, k := range cp {
		args = append(args, c.fullKey(tenantID, k))
	}

	v, err := c.do(ctx, args)
	if err != nil {
		return nil, err
	}
	if v.kind != respArray {
		return nil, fmt.Errorf("%w: %w: expected array reply", ErrCache, ErrCacheProtocol)
	}

	out := make(map[string][]byte, len(cp))
	if len(v.arr) != len(cp) {
		return nil, fmt.Errorf("%w: %w: mget length mismatch", ErrCache, ErrCacheProtocol)
	}

	for i := range cp {
		it := v.arr[i]
		if it.kind == respNil {
			continue
		}
		if it.kind != respBulk {
			return nil, fmt.Errorf("%w: %w: expected bulk element", ErrCache, ErrCacheProtocol)
		}
		out[cp[i]] = it.bulk
	}

	return out, nil
}

////////////////////////////////////////////////////////////////////////////////
// Core operation + connection management
////////////////////////////////////////////////////////////////////////////////

func (c *RedisCache) do(ctx context.Context, args []string) (respValue, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 {
		return respValue{}, fmt.Errorf("%w: %w: empty command", ErrCache, ErrCacheInvalid)
	}

	if c.opts.ReuseConn {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.doWithConnLocked(ctx, args)
	}

	conn, rw, err := c.dial(ctx)
	if err != nil {
		return respValue{}, err
	}
	defer func() { _ = conn.Close() }()

	return c.sendAndRead(ctx, conn, rw, args)
}

func (c *RedisCache) doWithConnLocked(ctx context.Context, args []string) (respValue, error) {
	if c.conn == nil || c.rw == nil {
		conn, rw, err := c.dial(ctx)
		if err != nil {
			return respValue{}, err
		}
		c.conn = conn
		c.rw = rw
	}

	v, err := c.sendAndRead(ctx, c.conn, c.rw, args)
	if err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.rw = nil
	}

	return v, err
}

func (c *RedisCache) dial(ctx context.Context) (net.Conn, *bufio.ReadWriter, error) {
	addr := c.opts.Addr
	d := net.Dialer{Timeout: c.opts.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w: dial %s: %v", ErrCache, ErrCacheConn, addr, err)
	}

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// AUTH if needed
	if strings.TrimSpace(c.opts.Password) != "" {
		if _, err := c.sendAndRead(ctx, conn, rw, []string{"AUTH", c.opts.Password}); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}

	// SELECT DB if needed
	if c.opts.DB != 0 {
		if _, err := c.sendAndRead(ctx, conn, rw, []string{"SELECT", strconv.Itoa(c.opts.DB)}); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}

	return conn, rw, nil
}

func (c *RedisCache) sendAndRead(ctx context.Context, conn net.Conn, rw *bufio.ReadWriter, args []string) (respValue, error) {
	if err := applyDeadlines(ctx, conn, c.opts.ReadTimeout, c.opts.WriteTimeout); err != nil {
		return respValue{}, err
	}

	if err := writeArray(rw.Writer, args); err != nil {
		return respValue{}, fmt.Errorf("%w: %w: write: %v", ErrCache, ErrCacheConn, err)
	}
	if err := rw.Flush(); err != nil {
		return respValue{}, fmt.Errorf("%w: %w: flush: %v", ErrCache, ErrCacheConn, err)
	}

	v, err := readValue(rw.Reader)
	if err != nil {
		return respValue{}, err
	}

	if v.kind == respErr {
		return respValue{}, fmt.Errorf("%w: %w: redis error: %s", ErrCache, ErrCacheProtocol, v.s)
	}

	return v, nil
}

func applyDeadlines(ctx context.Context, conn net.Conn, rt, wt time.Duration) error {
	// Use deterministic timeouts (fixed durations). We must use time.Now() for deadlines on sockets.
	now := time.Now()
	if wt <= 0 {
		wt = 2 * time.Second
	}
	if rt <= 0 {
		rt = 2 * time.Second
	}
	_ = conn.SetWriteDeadline(now.Add(wt))
	_ = conn.SetReadDeadline(now.Add(rt))

	// Best-effort: if ctx has deadline sooner, apply it.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	return nil
}

////////////////////////////////////////////////////////////////////////////////
// RESP2 encoding/decoding (deterministic)
////////////////////////////////////////////////////////////////////////////////

type respKind int

const (
	respSimple respKind = iota
	respErr
	respInt
	respBulk
	respArray
	respNil
)

type respValue struct {
	kind respKind
	s    string
	i    int64
	bulk []byte
	arr  []respValue
}

func writeArray(w *bufio.Writer, args []string) error {
	// *<n>\r\n $<len>\r\n<arg>\r\n ...
	if _, err := w.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, a := range args {
		b := []byte(a)
		if _, err := w.WriteString("$" + strconv.Itoa(len(b)) + "\r\n"); err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if _, err := w.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func readValue(r *bufio.Reader) (respValue, error) {
	p, err := r.ReadByte()
	if err != nil {
		return respValue{}, wrapIO(err)
	}

	switch p {
	case '+':
		s, err := readLine(r)
		if err != nil {
			return respValue{}, err
		}
		return respValue{kind: respSimple, s: s}, nil

	case '-':
		s, err := readLine(r)
		if err != nil {
			return respValue{}, err
		}
		return respValue{kind: respErr, s: s}, nil

	case ':':
		s, err := readLine(r)
		if err != nil {
			return respValue{}, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return respValue{}, fmt.Errorf("%w: %w: bad int", ErrCache, ErrCacheProtocol)
		}
		return respValue{kind: respInt, i: n}, nil

	case '$':
		s, err := readLine(r)
		if err != nil {
			return respValue{}, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return respValue{}, fmt.Errorf("%w: %w: bad bulk len", ErrCache, ErrCacheProtocol)
		}
		if n == -1 {
			return respValue{kind: respNil}, nil
		}
		if n < 0 || n > (64*1024*1024) {
			return respValue{}, fmt.Errorf("%w: %w: bulk too large", ErrCache, ErrCacheProtocol)
		}

		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return respValue{}, wrapIO(err)
		}
		if len(buf) < 2 || buf[len(buf)-2] != '\r' || buf[len(buf)-1] != '\n' {
			return respValue{}, fmt.Errorf("%w: %w: bulk missing crlf", ErrCache, ErrCacheProtocol)
		}
		return respValue{kind: respBulk, bulk: append([]byte{}, buf[:len(buf)-2]...)}, nil

	case '*':
		s, err := readLine(r)
		if err != nil {
			return respValue{}, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return respValue{}, fmt.Errorf("%w: %w: bad array len", ErrCache, ErrCacheProtocol)
		}
		if n == -1 {
			return respValue{kind: respNil}, nil
		}
		if n < 0 || n > 1_000_000 {
			return respValue{}, fmt.Errorf("%w: %w: array too large", ErrCache, ErrCacheProtocol)
		}

		arr := make([]respValue, 0, n)
		for i := int64(0); i < n; i++ {
			v, err := readValue(r)
			if err != nil {
				return respValue{}, err
			}
			arr = append(arr, v)
		}
		return respValue{kind: respArray, arr: arr}, nil

	default:
		return respValue{}, fmt.Errorf("%w: %w: unknown prefix byte", ErrCache, ErrCacheProtocol)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", wrapIO(err)
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", fmt.Errorf("%w: %w: expected crlf", ErrCache, ErrCacheProtocol)
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func wrapIO(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w: deadline exceeded", ErrCache, ErrCacheTimeout)
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return fmt.Errorf("%w: %w: timeout", ErrCache, ErrCacheTimeout)
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %w: eof", ErrCache, ErrCacheConn)
	}
	return fmt.Errorf("%w: %w: %v", ErrCache, ErrCacheConn, err)
}

////////////////////////////////////////////////////////////////////////////////
// Key derivation + defaults
////////////////////////////////////////////////////////////////////////////////

func (c *RedisCache) fullKey(tenantID, key string) string {
	// Stable tenant namespacing: <prefix>:<tenant>:<key>
	prefix := strings.TrimSuffix(c.opts.KeyPrefix, ":")
	return prefix + ":" + tenantID + ":" + key
}

func normalizeOptions(opts Options) Options {
	o := opts

	if strings.TrimSpace(o.Addr) == "" {
		o.Addr = "127.0.0.1:6379"
	}
	if o.DB < 0 {
		o.DB = 0
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = 2 * time.Second
	}
	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 2 * time.Second
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 2 * time.Second
	}
	if o.DefaultTTL <= 0 {
		o.DefaultTTL = 30 * time.Second
	}
	if strings.TrimSpace(o.KeyPrefix) == "" {
		o.KeyPrefix = "chartly:cache"
	} else {
		o.KeyPrefix = strings.TrimSpace(o.KeyPrefix)
		o.KeyPrefix = strings.ReplaceAll(o.KeyPrefix, "\x00", "")
	}

	return o
}

func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}
