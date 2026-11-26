package mobileapp

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// gzipTestRouter is Recovery then the gzip middleware, the order the app's
// router has them in (gin.Default's Recovery runs first).
func gzipTestRouter(register func(r *gin.Engine)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(GzipMiddleware())
	register(r)
	return r
}

func gzipGet(r http.Handler, method, path, acceptEncoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// bigJSON is a JSON array comfortably over gzipMinBytes.
func bigJSON(t testing.TB, n int) []byte {
	t.Helper()
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("comp-%04d", i), "name": "Competition " + fmt.Sprint(i), "status": "running"}
	}
	b, err := json.Marshal(rows)
	require.NoError(t, err)
	return b
}

func gunzip(t testing.TB, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)
	out, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())
	return out
}

func varyTokens(h http.Header) []string {
	var out []string
	for _, v := range h.Values("Vary") {
		for _, tok := range strings.Split(v, ",") {
			out = append(out, strings.TrimSpace(tok))
		}
	}
	return out
}

func TestGzip_CompressesLargeJSONForAGzipCaller(t *testing.T) {
	body := bigJSON(t, 200)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/list", func(c *gin.Context) {
			c.Header("Content-Length", fmt.Sprint(len(body)))
			c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		})
	})
	w := gzipGet(r, http.MethodGet, "/api/list", "gzip, deflate, br")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Empty(t, w.Header().Get("Content-Length"), "Content-Length is dropped when compressing")
	assert.Contains(t, varyTokens(w.Header()), "Accept-Encoding")
	assert.Less(t, w.Body.Len(), len(body))
	assert.Equal(t, body, gunzip(t, w.Body.Bytes()))
}

func TestGzip_PlainForACallerThatDoesNotAcceptGzip(t *testing.T) {
	body := bigJSON(t, 200)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/list", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", body) })
	})
	for _, ae := range []string{"", "identity", "gzip;q=0", "gzip;q=0.000, *", "br"} {
		w := gzipGet(r, http.MethodGet, "/api/list", ae)
		assert.Empty(t, w.Header().Get("Content-Encoding"), "Accept-Encoding %q", ae)
		assert.Equal(t, body, w.Body.Bytes(), "Accept-Encoding %q", ae)
		assert.Contains(t, varyTokens(w.Header()), "Accept-Encoding", "Vary on the plain path too (%q)", ae)
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                  false,
		"gzip":              true,
		"GZIP":              true,
		"deflate, gzip":     true,
		"gzip;q=0.5":        true,
		"gzip; q=0":         false,
		"gzip;q=0.0":        false,
		"*":                 true,
		"*;q=0":             false,
		"gzip;q=0, *":       false,
		"br, *;q=0.1":       true,
		"identity, deflate": false,
	}
	for ae, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		if ae != "" {
			req.Header.Set("Accept-Encoding", ae)
		}
		assert.Equal(t, want, acceptsGzip(req), "Accept-Encoding %q", ae)
	}
}

// HEAD, 204 and 304 carry no body and no encoding.
func TestGzip_BodylessAnswersCarryNoBodyAndNoEncoding(t *testing.T) {
	body := bigJSON(t, 200)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.HEAD("/api/list", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", body) })
		r.GET("/api/none", func(c *gin.Context) {
			c.Header("Content-Type", "application/json")
			c.Status(http.StatusNoContent)
		})
		r.GET("/api/same", func(c *gin.Context) { c.Data(http.StatusNotModified, "application/json", body) })
	})

	for _, path := range []string{"/api/none", "/api/same"} {
		w := gzipGet(r, http.MethodGet, path, "gzip")
		assert.Empty(t, w.Header().Get("Content-Encoding"), path)
		assert.Zero(t, w.Body.Len(), path)
	}
	assert.Equal(t, http.StatusNoContent, gzipGet(r, http.MethodGet, "/api/none", "gzip").Code)
	assert.Equal(t, http.StatusNotModified, gzipGet(r, http.MethodGet, "/api/same", "gzip").Code)

	srv := httptest.NewServer(r)
	defer srv.Close()
	req, err := http.NewRequest(http.MethodHead, srv.URL+"/api/list", nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Content-Encoding"))
	assert.Empty(t, got)
}

// A handler panic still answers 500, whether it panicked before writing or
// after writing less than the middleware holds.
func TestGzip_HandlerPanicStillAnswers500(t *testing.T) {
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/boom", func(c *gin.Context) { panic("boom") })
		r.GET("/api/half", func(c *gin.Context) {
			c.Writer.Header().Set("Content-Type", "application/json")
			c.Writer.WriteHeader(http.StatusOK)
			_, err := c.Writer.Write([]byte(`{"partial":`))
			require.NoError(t, err)
			panic("boom")
		})
	})
	for _, path := range []string{"/api/boom", "/api/half"} {
		w := gzipGet(r, http.MethodGet, path, "gzip")
		assert.Equal(t, http.StatusInternalServerError, w.Code, path)
		assert.Empty(t, w.Header().Get("Content-Encoding"), path)
		assert.NotContains(t, w.Body.String(), "partial", path)
	}
}

func TestGzip_VaryMergesWithWhatTheHandlerSet(t *testing.T) {
	body := bigJSON(t, 200)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/judged", func(c *gin.Context) {
			c.Header("Vary", "X-Tournament-Password, "+revealKeyHeader)
			c.Data(http.StatusOK, "application/json", body)
		})
		r.GET("/api/named", func(c *gin.Context) {
			c.Header("Vary", "accept-encoding")
			c.Data(http.StatusOK, "application/json", body)
		})
		r.GET("/api/small", func(c *gin.Context) {
			c.Header("Vary", "X-Tournament-Password")
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	})

	w := gzipGet(r, http.MethodGet, "/api/judged", "gzip")
	assert.Equal(t, []string{"X-Tournament-Password", revealKeyHeader, "Accept-Encoding"}, varyTokens(w.Header()))
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))

	w = gzipGet(r, http.MethodGet, "/api/named", "gzip")
	assert.Equal(t, []string{"accept-encoding"}, varyTokens(w.Header()), "never named twice")

	w = gzipGet(r, http.MethodGet, "/api/small", "")
	assert.Equal(t, []string{"X-Tournament-Password", "Accept-Encoding"}, varyTokens(w.Header()))
}

// An answer that already carries a Content-Encoding (the shared builds'
// pre-compressed bytes) is left alone.
func TestGzip_AlreadyEncodedAnswerIsLeftAlone(t *testing.T) {
	var pre bytes.Buffer
	zw := gzip.NewWriter(&pre)
	_, err := zw.Write(bigJSON(t, 400))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/pre", func(c *gin.Context) {
			c.Header("Content-Encoding", "gzip")
			c.Data(http.StatusOK, "application/json", pre.Bytes())
		})
	})
	w := gzipGet(r, http.MethodGet, "/api/pre", "gzip")
	assert.Equal(t, []string{"gzip"}, w.Header().Values("Content-Encoding"))
	assert.Equal(t, pre.Bytes(), w.Body.Bytes(), "compressed once, never twice")
}

func TestGzip_UnderOneKiBIsSentAsItIs(t *testing.T) {
	small := bytes.Repeat([]byte("a"), gzipMinBytes-12)
	body := []byte(`{"v":"` + string(small) + `"}`)
	require.Less(t, len(body), gzipMinBytes)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/small", func(c *gin.Context) { c.Data(http.StatusCreated, "application/json", body) })
		r.GET("/api/status-only", func(c *gin.Context) { c.Status(http.StatusAccepted) })
	})
	w := gzipGet(r, http.MethodGet, "/api/small", "gzip")
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, body, w.Body.Bytes())
	assert.Contains(t, varyTokens(w.Header()), "Accept-Encoding")

	assert.Equal(t, http.StatusAccepted, gzipGet(r, http.MethodGet, "/api/status-only", "gzip").Code)
}

// The event stream is never wrapped: skipped by path, and any
// text/event-stream answer reaches the caller as it is flushed.
func TestGzip_EventStreamIsNeverWrapped(t *testing.T) {
	event := "event: match_updated\ndata: {}\n\n"
	var rec *httptest.ResponseRecorder
	var seenBeforeReturn string
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET(gzipStreamPath, func(c *gin.Context) {
			_, wrapped := c.Writer.(*gzipResponseWriter)
			assert.False(t, wrapped, "the stream path is skipped before wrapping")
			c.String(http.StatusOK, "ok")
		})
		r.GET("/api/other-stream", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Status(http.StatusOK)
			_, err := c.Writer.WriteString(event)
			require.NoError(t, err)
			c.Writer.Flush()
			seenBeforeReturn = rec.Body.String()
		})
	})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, gzipStreamPath, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(rec, req)
	assert.Empty(t, rec.Header().Get("Content-Encoding"))

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/other-stream", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(rec, req)
	assert.Equal(t, event, seenBeforeReturn, "the event went out at its flush, not held")
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.True(t, rec.Flushed)
}

func TestGzip_FlushAndHijackPassThrough(t *testing.T) {
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/hijack", func(c *gin.Context) {
			_, wrapped := c.Writer.(*gzipResponseWriter)
			assert.True(t, wrapped)
			conn, buf, err := c.Writer.Hijack()
			if !assert.NoError(t, err) {
				return
			}
			defer conn.Close()
			_, err = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nhi")
			assert.NoError(t, err)
			assert.NoError(t, buf.Flush())
		})
		r.GET("/api/flushed", func(c *gin.Context) {
			c.Header("Content-Type", "application/json")
			_, err := c.Writer.WriteString(`{"a":1}`)
			require.NoError(t, err)
			c.Writer.Flush()
			_, err = c.Writer.WriteString(strings.Repeat(" ", 2*gzipMinBytes))
			require.NoError(t, err)
		})
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/hijack")
	require.NoError(t, err)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "hi", string(got))

	w := gzipGet(r, http.MethodGet, "/api/flushed", "gzip")
	assert.True(t, w.Flushed)
	assert.Empty(t, w.Header().Get("Content-Encoding"), "an answer flushed under the threshold goes out as it is")
	assert.True(t, strings.HasPrefix(w.Body.String(), `{"a":1}`))
}

// A path outside /api is never wrapped.
func TestGzip_OnlyAPIPathsAreWrapped(t *testing.T) {
	body := bigJSON(t, 200)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/health", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", body) })
	})
	w := gzipGet(r, http.MethodGet, "/health", "gzip")
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Empty(t, w.Header().Get("Vary"))
}

// An unknown /api path with no NoRoute handler keeps the 404 gin set before
// the middleware ran, instead of the wrapper overwriting it with 200.
func TestGzip_UnknownAPIRouteKeepsGin404(t *testing.T) {
	r := gzipTestRouter(func(*gin.Engine) {})
	w := gzipGet(r, http.MethodGet, "/api/nothing-here", "gzip")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Through the app's own router: a JSON read is compressed, and backup zips
// and workbooks carry no Content-Encoding.
func TestGzip_AppRouter(t *testing.T) {
	rig := newBackupRig(t)
	for i := range 30 {
		require.NoError(t, rig.store.SaveCompetition(&state.Competition{
			ID: fmt.Sprintf("extra-%02d", i), Name: fmt.Sprintf("Extra competition %02d", i),
			Date: "01-06-2026", Courts: []string{"A"},
		}))
	}
	p1 := helper.Player{ID: "Alice", Name: "Alice", Dojo: "Dojo"}
	p2 := helper.Player{ID: "Bob", Name: "Bob", Dojo: "Dojo"}
	require.NoError(t, rig.store.SavePools("alpha", []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{p1, p2}, Matches: []helper.Match{{SideA: &p1, SideB: &p2}}},
	}))
	require.NoError(t, rig.store.SavePoolMatches("alpha", nil))

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Tournament-Password", backupTestPassword)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		rig.r.ServeHTTP(w, req)
		return w
	}

	w := get("/api/viewer/competitions")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Contains(t, varyTokens(w.Header()), "Accept-Encoding")
	assert.True(t, json.Valid(gunzip(t, w.Body.Bytes())))

	w = get("/api/tournament/backup")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, "PK", string(w.Body.Bytes()[:2]))

	w = get("/api/competitions/alpha/export-results")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "spreadsheetml")
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, "PK", string(w.Body.Bytes()[:2]))
}

// viewerListLike is a competitions-list-shaped JSON body of about size
// bytes.
func viewerListLike(b testing.TB, size int) []byte {
	b.Helper()
	type side struct {
		ID, Name, Dojo, Number string
	}
	type match struct {
		ID, Court, Status, Time string
		SideA, SideB            side
		IpponsA, IpponsB        string
	}
	var rows []match
	var out []byte
	for i := 0; len(out) < size; i++ {
		rows = append(rows, match{
			ID: fmt.Sprintf("Pool %c-%d", 'A'+i%16, i), Court: string(rune('A' + i%4)), Status: "scheduled",
			Time:    fmt.Sprintf("%02d:%02d", 9+i/60%8, i%60),
			SideA:   side{ID: fmt.Sprintf("%08x-aaaa-4bbb-8ccc-%012x", i, i), Name: fmt.Sprintf("Competitor %d", i), Dojo: "Dojo " + fmt.Sprint(i%40), Number: fmt.Sprintf("K%d", i)},
			SideB:   side{ID: fmt.Sprintf("%08x-dddd-4eee-8fff-%012x", i, i+1), Name: fmt.Sprintf("Competitor %d", i+1), Dojo: "Dojo " + fmt.Sprint((i+7)%40), Number: fmt.Sprintf("K%d", i+1)},
			IpponsA: "MK", IpponsB: "",
		})
		if i%100 == 0 {
			var err error
			out, err = json.Marshal(map[string]any{"competitions": []map[string]any{{"id": "kendo-open", "matches": rows}}})
			require.NoError(b, err)
		}
	}
	return out
}

// BenchmarkCompressViewerList records the cost of compressing one 300 KB
// competitions list through the middleware, and the bytes it sends.
func BenchmarkCompressViewerList(b *testing.B) {
	body := viewerListLike(b, 300*1024)
	r := gzipTestRouter(func(r *gin.Engine) {
		r.GET("/api/viewer/competitions", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", body) })
	})
	req := httptest.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	var sent int
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		sent = w.Body.Len()
	}
	b.ReportMetric(float64(len(body)), "plain-bytes")
	b.ReportMetric(float64(sent), "gzip-bytes")
}
