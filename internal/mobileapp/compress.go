package mobileapp

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipMinBytes is the size an /api answer must reach before it is
// compressed. Below it the gzip header and trailer cost more than they
// save, so a small answer goes out as it is.
const gzipMinBytes = 1024

// gzipStreamPath is the event stream. It is never wrapped: its events must
// reach the page as they are written, never held in a buffer.
const gzipStreamPath = "/api/events"

// gzipWriterPool keeps gzip writers at BestSpeed between answers, so a
// burst of refetches does not allocate one compressor per request.
var gzipWriterPool = sync.Pool{New: newPooledGzipWriter}

func newPooledGzipWriter() any {
	zw, err := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	if err != nil {
		// Unreachable: BestSpeed is a valid level.
		panic("mobileapp: gzip.BestSpeed rejected: " + err.Error())
	}
	return zw
}

// getGzipWriter takes a pooled writer and points it at w.
func getGzipWriter(w io.Writer) *gzip.Writer {
	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(w)
	return zw
}

// putGzipWriter returns a writer to the pool once its stream is closed.
func putGzipWriter(zw *gzip.Writer) {
	zw.Reset(io.Discard)
	gzipWriterPool.Put(zw)
}

// acceptsGzip reports whether the request's Accept-Encoding allows gzip: a
// "gzip" coding with a non-zero q, or, when gzip is not named, a "*" with a
// non-zero q.
func acceptsGzip(r *http.Request) bool {
	named, star := false, false
	gzipOK, starOK := false, false
	for _, v := range r.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(v, ",") {
			coding, params, _ := strings.Cut(part, ";")
			coding = strings.TrimSpace(coding)
			switch {
			case strings.EqualFold(coding, "gzip"):
				named = true
				gzipOK = gzipOK || !qualityZero(params)
			case coding == "*":
				star = true
				starOK = starOK || !qualityZero(params)
			}
		}
	}
	if named {
		return gzipOK
	}
	return star && starOK
}

// qualityZero reports whether a coding's parameters carry q=0.
func qualityZero(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return err == nil && q == 0
	}
	return false
}

// addVaryAcceptEncoding merges Accept-Encoding into the answer's Vary,
// keeping whatever the handler set there (the audience judge sets Vary on
// the public reads) and never naming it twice.
func addVaryAcceptEncoding(h http.Header) {
	existing := h.Values("Vary")
	for _, v := range existing {
		for _, tok := range strings.Split(v, ",") {
			t := strings.TrimSpace(tok)
			if t == "*" || strings.EqualFold(t, "Accept-Encoding") {
				return
			}
		}
	}
	if len(existing) == 0 {
		h.Set("Vary", "Accept-Encoding")
		return
	}
	h.Set("Vary", strings.Join(existing, ", ")+", Accept-Encoding")
}

// isJSONType reports whether a Content-Type names application/json.
func isJSONType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}

// GzipMiddleware compresses the JSON answers of /api/* for a caller that
// accepts gzip. It holds the first gzipMinBytes of an answer and then
// decides: compress when the type is application/json and no
// Content-Encoding is set, otherwise send the body as it is. The event
// stream is skipped by path, and a text/event-stream answer is never
// held. Every JSON answer names Accept-Encoding in Vary, compressed or not.
func GzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") || path == gzipStreamPath {
			c.Next()
			return
		}
		orig := c.Writer
		gw := &gzipResponseWriter{
			ResponseWriter: orig,
			accepts:        acceptsGzip(c.Request),
			head:           c.Request.Method == http.MethodHead,
			status:         orig.Status(),
			size:           -1,
		}
		c.Writer = gw
		completed := false
		defer func() {
			c.Writer = orig
			if !completed {
				// A panic is unwinding to Recovery, with the original writer
				// back in place. A complete JSON answer the handler had
				// already written is sent as it stands: a write that landed
				// stays answered 200, where a 500 would have the outbox retry
				// it. Anything else held (nothing written, or a body cut
				// short) is dropped, so Recovery's 500 reaches the caller.
				// An answer already past gzipMinBytes has sent its headers: its
				// stream is closed, so the caller reads what was written
				// whole, never a gzip body cut short. A writer mid-stream is
				// not returned to the pool.
				if gw.wroteHeader && !gw.decided && json.Valid(gw.buf) {
					if err := gw.decide(false); err != nil {
						slog.Debug("mobileapp: gzip answer", "err", err)
					}
				}
				if gw.zw != nil {
					if err := gw.zw.Close(); err != nil {
						slog.Debug("mobileapp: gzip close", "err", err)
					}
				}
				gw.zw = nil
				return
			}
			gw.finish()
		}()
		c.Next()
		completed = true
	}
}

// gzipResponseWriter wraps gin's writer for one answer. Until it decides,
// it records the status and holds the body; after, it writes through a
// pooled gzip writer or straight to the wrapped writer.
type gzipResponseWriter struct {
	gin.ResponseWriter

	accepts bool
	head    bool

	status      int
	size        int // bytes the handler wrote, -1 before anything
	wroteHeader bool

	buf      []byte
	decided  bool
	hijacked bool
	zw       *gzip.Writer
}

// WriteHeader records the status until the header is written. A later call
// is ignored, as gin's own writer does, so Status keeps the code sent.
func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader || w.decided {
		return
	}
	if code > 0 {
		w.status = code
	}
}

func (w *gzipResponseWriter) WriteHeaderNow() {
	w.wroteHeader = true
	if w.size < 0 {
		w.size = 0
	}
}

func (w *gzipResponseWriter) Status() int { return w.status }

func (w *gzipResponseWriter) Size() int { return w.size }

func (w *gzipResponseWriter) Written() bool { return w.size != -1 }

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	w.size += len(p)
	if w.decided {
		return w.writeOut(p)
	}
	if !w.mayCompress() {
		if err := w.decide(false); err != nil {
			return 0, err
		}
		return w.writeOut(p)
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) >= gzipMinBytes {
		if err := w.decide(true); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush sends what is held: an answer flushed before gzipMinBytes goes out
// as it is.
func (w *gzipResponseWriter) Flush() {
	if !w.decided {
		if err := w.decide(len(w.buf) >= gzipMinBytes && w.mayCompress()); err != nil {
			slog.Debug("mobileapp: gzip flush", "err", err)
			return
		}
	}
	if w.zw != nil {
		if err := w.zw.Flush(); err != nil {
			slog.Debug("mobileapp: gzip flush", "err", err)
			return
		}
	}
	w.ResponseWriter.Flush()
}

// Hijack hands the connection over; nothing held is sent after it.
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	w.decided = true
	w.buf = nil
	return w.ResponseWriter.Hijack()
}

// mayCompress reports whether the answer, as its headers stand, is one
// the middleware compresses once it is large enough.
func (w *gzipResponseWriter) mayCompress() bool {
	if !w.accepts || w.head || !bodyAllowed(w.status) {
		return false
	}
	h := w.Header()
	return h.Get("Content-Encoding") == "" && isJSONType(h.Get("Content-Type"))
}

// bodyAllowed mirrors net/http: 1xx, 204 and 304 carry no body.
func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

// decide sends the headers and what is held, compressed or not.
func (w *gzipResponseWriter) decide(compress bool) error {
	w.decided = true
	h := w.Header()
	if isJSONType(h.Get("Content-Type")) {
		addVaryAcceptEncoding(h)
	}
	if compress {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
	}
	w.ResponseWriter.WriteHeader(w.status)
	if w.wroteHeader {
		w.ResponseWriter.WriteHeaderNow()
	}
	if compress {
		w.zw = getGzipWriter(w.ResponseWriter)
	}
	held := w.buf
	w.buf = nil
	if len(held) == 0 {
		return nil
	}
	_, err := w.writeOut(held)
	return err
}

func (w *gzipResponseWriter) writeOut(p []byte) (int, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	if w.zw != nil {
		return w.zw.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// finish runs once the handlers return: an answer still held (under
// gzipMinBytes, or none written) goes out as it is, and a gzip stream is
// closed and its writer pooled.
func (w *gzipResponseWriter) finish() {
	if w.hijacked {
		return
	}
	if !w.decided {
		if err := w.decide(false); err != nil {
			slog.Debug("mobileapp: gzip answer", "err", err)
		}
	}
	if w.zw == nil {
		return
	}
	if err := w.zw.Close(); err != nil {
		slog.Debug("mobileapp: gzip close", "err", err)
	}
	putGzipWriter(w.zw)
	w.zw = nil
}
