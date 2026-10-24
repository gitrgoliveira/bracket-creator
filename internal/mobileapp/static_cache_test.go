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

// The compiled front-end is served from a build-scoped directory,
// /dist/<content hash>/, as immutable (bc-evts stage 3 review 3, F3; see
// build_scoped_assets_test.go), with a content-derived ETag: a reload never
// pairs the served page (which names its build, appBuildMeta, and its module
// directory) with the modules of another build, and makes no request for a
// module it already holds.
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

	t.Run("a module under the build's own directory is immutable", func(t *testing.T) {
		// Stage 3 review 3, F3: the page names its modules under
		// /dist/<content hash>/, so a module there never changes and a load
		// makes no conditional request for it (mp-6o1a), while the page and
		// its code are still one build (S2 a, the page is no-store).
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		setStaticCacheHeaders(c, etag, "dist/a.js", true)
		assert.Equal(t, "public, max-age=31536000, immutable", c.Writer.Header().Get("Cache-Control"))
		assert.NotEmpty(t, c.Writer.Header().Get("ETag"))
	})

	t.Run("a plain dist path keeps the five-minute window", func(t *testing.T) {
		// Only a page whose content hash could not be made names these.
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		setStaticCacheHeaders(c, etag, "dist/a.js", false)
		assert.Equal(t, "public, max-age=300", c.Writer.Header().Get("Cache-Control"))
		assert.NotEmpty(t, c.Writer.Header().Get("ETag"))
	})

	t.Run("a revalidation of an unchanged asset is a 304 with no body", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/dist/a.js", nil)
		c.Request.Header.Set("If-None-Match", etag)
		setStaticCacheHeaders(c, etag, "dist/a.js", false)
		http.ServeContent(c.Writer, c.Request, "a.js", time.Time{}, strings.NewReader("x"))
		// gin's writer holds a status until something is written; a 304 writes none.
		assert.Equal(t, http.StatusNotModified, c.Writer.Status())
		assert.Empty(t, w.Body.String())
	})

	t.Run("leaves non-dist paths alone", func(t *testing.T) {
		// index.html carries server-rendered meta tags and is deliberately
		// no-store; uploaded images set their own policy in their handlers.
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		setStaticCacheHeaders(c, etag, "index.html", true)
		assert.Empty(t, c.Writer.Header().Get("Cache-Control"))
		assert.Empty(t, c.Writer.Header().Get("ETag"))
	})
}
