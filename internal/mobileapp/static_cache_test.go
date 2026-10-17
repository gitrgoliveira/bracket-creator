package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The compiled front-end under /dist/ is served with "Cache-Control: no-cache"
// plus a content-derived ETag (bc-evts stage 3 review 2, R7): every load
// revalidates every module, and the ETag turns each re-fetch into a 0-byte 304
// unless the bundle actually changed, so a reload never pairs the served page
// (which names its build, appBuildMeta) with the modules of another build.
//
// Before this, http.FileServer over embed.FS emitted no Cache-Control, no ETag
// and no Last-Modified (embed's zero modtime makes ServeContent omit it), so a
// browser had nothing to revalidate against: a page load transferred 1.17 MB
// across 69 files with NONE served from cache. Measured after: 20 KB with the
// ETag alone (all 69 revalidated, no bodies), then 0 across 0 requests once the
// max-age window was added.
func TestBuildAssetETag(t *testing.T) {
	fsA := fstest.MapFS{
		"dist/a.js": {Data: []byte("console.log(1)")},
		"dist/b.js": {Data: []byte("console.log(2)")},
	}

	t.Run("is derived from asset CONTENT, not the build version", func(t *testing.T) {
		// version.GetVersion() is an embedded string that on a development
		// branch is the branch name: it does not move when you rebuild, so
		// using it would serve a developer stale JavaScript after every
		// recompile. Changing a byte must change the validator.
		first := buildAssetETag(fsA)
		require.NotEmpty(t, first)

		changed := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(999)")}, // one byte differs
			"dist/b.js": {Data: []byte("console.log(2)")},
		})
		assert.NotEqual(t, first, changed, "a changed asset must invalidate the ETag")
	})

	t.Run("is stable for identical content", func(t *testing.T) {
		a := buildAssetETag(fsA)
		b := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(1)")},
			"dist/b.js": {Data: []byte("console.log(2)")},
		})
		assert.Equal(t, a, b, "restarting the same binary must not invalidate every client's cache")
	})

	t.Run("a file NAME change invalidates too", func(t *testing.T) {
		a := buildAssetETag(fsA)
		renamed := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(1)")},
			"dist/c.js": {Data: []byte("console.log(2)")}, // same bytes, new name
		})
		assert.NotEqual(t, a, renamed, "the path is hashed alongside the bytes")
	})

	t.Run("an unreadable tree yields no validator rather than a wrong one", func(t *testing.T) {
		// No dist/ directory at all: WalkDir errors, and we must fall back to
		// "uncacheable" rather than to a constant that would pin stale assets.
		assert.Empty(t, buildAssetETag(fstest.MapFS{"other/x.js": {Data: []byte("x")}}))
	})
}

func TestSetStaticCacheHeaders(t *testing.T) {
	etag := buildAssetETag(fstest.MapFS{"dist/a.js": {Data: []byte("x")}})
	require.NotEmpty(t, etag)

	t.Run("applies the caching policy to a dist asset", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		setStaticCacheHeaders(c, etag, "dist/a.js")
		// Both halves, and they compose: the client asks every time (no-cache:
		// stored, never reused unasked), and the ETag turns the answer into a
		// 0-byte 304 unless the bundle changed. No max-age window: within one a
		// reload paired the new page with the previous build's modules (R7).
		assert.Equal(t, "no-cache", c.Writer.Header().Get("Cache-Control"))
		assert.NotEmpty(t, c.Writer.Header().Get("ETag"))
	})

	t.Run("a revalidation of an unchanged asset is a 304 with no body", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/dist/a.js", nil)
		c.Request.Header.Set("If-None-Match", etag)
		setStaticCacheHeaders(c, etag, "dist/a.js")
		http.ServeContent(c.Writer, c.Request, "a.js", time.Time{}, strings.NewReader("x"))
		// gin's writer holds a status until something is written; a 304 writes none.
		assert.Equal(t, http.StatusNotModified, c.Writer.Status())
		assert.Empty(t, w.Body.String())
	})

	t.Run("leaves non-dist paths alone", func(t *testing.T) {
		// index.html carries server-rendered meta tags and is deliberately
		// no-store; uploaded images set their own policy in their handlers.
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		setStaticCacheHeaders(c, etag, "index.html")
		assert.Empty(t, c.Writer.Header().Get("Cache-Control"))
		assert.Empty(t, c.Writer.Header().Get("ETag"))
	})
}
