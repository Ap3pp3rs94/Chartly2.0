package output

import (

	"context"

	"errors"

	"os"

	"path/filepath"

	"strings"

	"sync"

	"time"


	"github.com/Ap3pp3rs94/Chartly2.0/services/connector-hub/internal/streaming"
)

type LoggerFn func(level, msg string, fields map[string]any)

type streamKey string

type streamFile struct {

	f            *os.File

	bytesWritten int64

	createdAt    string

	sinceSync    int64
}

type RawWriter struct {

	baseDir           string

	maxBytesPerStream int64


	mu    sync.Mutex

	files map[streamKey]*streamFile


	logger LoggerFn
}

func NewRawWriter(baseDir string) *RawWriter {

	if strings.TrimSpace(baseDir) == "" {


		baseDir = "./data"

	}

	return &RawWriter{


		baseDir:           baseDir,


		maxBytesPerStream: 50 * 1024 * 1024,


		files:            make(map[streamKey]*streamFile),


		logger:           func(string, string, map[string]any) {},

	}
}

func (w *RawWriter) WithMaxBytes(n int64) *RawWriter {

	if n > 0 {


		w.maxBytesPerStream = n

	}

	return w
}

func (w *RawWriter) WithLogger(fn LoggerFn) *RawWriter {

	if fn != nil {


		w.logger = fn

	}

	return w
}

// Write writes raw chunks to a per-stream file.
// It matches streaming.StreamSink's method signature.
func (w *RawWriter) Write(ctx context.Context, meta streaming.Meta, chunk []byte) error {

	if ctx.Err() != nil {


		return ctx.Err()

	}

	if len(chunk) == 0 {


		return nil

	}


	k := makeKey(meta)


	w.mu.Lock()
	defer w.mu.Unlock()

	if ctx.Err() != nil {


		return ctx.Err()

	}

	if w.files == nil {


		w.files = make(map[streamKey]*streamFile)

	}

	sf := w.files[k]
	if sf == nil {


		if err := os.MkdirAll(w.baseDir, 0o755); err != nil {



			return err

		}


		path := w.filePath(meta)


		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {



			return err

		}


		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)

		if err != nil {



			return err

		}


		sf = &streamFile{


			f:         f,


			createdAt: time.Now().UTC().Format(time.RFC3339Nano),

		}


		w.files[k] = sf


		w.logger("info", "raw_writer_open", map[string]any{


			"event": "raw_writer_open",


			"path":  path,

		})

	}


	if w.maxBytesPerStream > 0 && sf.bytesWritten+int64(len(chunk)) > w.maxBytesPerStream {


		return errors.New("max bytes per stream exceeded")

	}


	// Write while holding lock to preserve ordering per stream.
	// This is conservative but safe for v0.
	n, err := sf.f.Write(chunk)
	sf.bytesWritten += int64(n)
	sf.sinceSync += int64(n)


	// periodic fsync every 1MB
	if sf.sinceSync >= 1024*1024 {


		_ = sf.f.Sync()


		sf.sinceSync = 0

	}

	return err
}

func (w *RawWriter) Close(ctx context.Context, meta streaming.Meta) error {

	if ctx.Err() != nil {


		return ctx.Err()

	}

	k := makeKey(meta)


	w.mu.Lock()
	sf := w.files[k]
	if sf == nil {


		w.mu.Unlock()


		return nil

	}

	delete(w.files, k)
	f := sf.f
	w.mu.Unlock()


	_ = f.Sync()
	return f.Close()
}

func makeKey(m streaming.Meta) streamKey {

	return streamKey(strings.ToLower(strings.TrimSpace(m.TenantID)) + "|" +


		strings.ToLower(strings.TrimSpace(m.SourceID)) + "|" +


		strings.ToLower(strings.TrimSpace(m.ConnectorID)))
}

func (w *RawWriter) filePath(m streaming.Meta) string {

	tenant := sanitizeSeg(m.TenantID, "tenant")

	source := sanitizeSeg(m.SourceID, "source")

	conn := sanitizeSeg(m.ConnectorID, "connector")

	date := time.Now().UTC().Format("2006-01-02")

	return filepath.Join(w.baseDir, tenant, source, conn, date+".log")
}

func sanitizeSeg(s, def string) string {

	s = strings.TrimSpace(s)

	if s == "" {


		return def

	}

	s = strings.ToLower(s)

	s = strings.ReplaceAll(s, "..", "")

	s = strings.ReplaceAll(s, "/", "_")

	s = strings.ReplaceAll(s, "\\", "_")

	s = strings.ReplaceAll(s, ":", "_")

	return s
}
