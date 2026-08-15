package mobileapp

import (
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The compiled front-end under /dist/ is served with a content-derived ETag and
// Cache-Control: no-cache, so a returning client revalidates and gets a 0-byte
// 304 unless the bundle actually changed.
//
// Before this, http.FileServer over embed.FS emitted no Cache-Control, no ETag
// and no Last-Modified (embed's zero modtime makes ServeContent omit it), so a
// browser had nothing to revalidate against: a page load transferred 1.17 MB
// across 69 files with NONE served from cache. Measured after: 20 KB, all 69
// revalidated with no body.
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
		resetAssetETag()
		first := buildAssetETag(fsA)
		require.NotEmpty(t, first)

		resetAssetETag()
		changed := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(999)")}, // one byte differs
			"dist/b.js": {Data: []byte("console.log(2)")},
		})
		assert.NotEqual(t, first, changed, "a changed asset must invalidate the ETag")
	})

	t.Run("is stable for identical content", func(t *testing.T) {
		resetAssetETag()
		a := buildAssetETag(fsA)
		resetAssetETag()
		b := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(1)")},
			"dist/b.js": {Data: []byte("console.log(2)")},
		})
		assert.Equal(t, a, b, "restarting the same binary must not invalidate every client's cache")
	})

	t.Run("a file NAME change invalidates too", func(t *testing.T) {
		resetAssetETag()
		a := buildAssetETag(fsA)
		resetAssetETag()
		renamed := buildAssetETag(fstest.MapFS{
			"dist/a.js": {Data: []byte("console.log(1)")},
			"dist/c.js": {Data: []byte("console.log(2)")}, // same bytes, new name
		})
		assert.NotEqual(t, a, renamed, "the path is hashed alongside the bytes")
	})

	t.Run("an unreadable tree yields no validator rather than a wrong one", func(t *testing.T) {
		resetAssetETag()
		// No dist/ directory at all: WalkDir errors, and we must fall back to
		// "uncacheable" rather than to a constant that would pin stale assets.
		assert.Empty(t, buildAssetETag(fstest.MapFS{"other/x.js": {Data: []byte("x")}}))
	})
}

func TestSetStaticCacheHeaders(t *testing.T) {
	fsys := fstest.MapFS{"dist/a.js": {Data: []byte("x")}}

	t.Run("applies the revalidation policy to a dist asset", func(t *testing.T) {
		resetAssetETag()
		c, _ := gin.CreateTestContext(newRecorder())
		setStaticCacheHeaders(c, fsys, "dist/a.js")
		assert.Equal(t, "no-cache", c.Writer.Header().Get("Cache-Control"))
		assert.NotEmpty(t, c.Writer.Header().Get("ETag"))
	})

	t.Run("leaves non-dist paths alone", func(t *testing.T) {
		// index.html carries server-rendered meta tags and is deliberately
		// no-store; uploaded images set their own policy in their handlers.
		resetAssetETag()
		c, _ := gin.CreateTestContext(newRecorder())
		setStaticCacheHeaders(c, fsys, "index.html")
		assert.Empty(t, c.Writer.Header().Get("Cache-Control"))
		assert.Empty(t, c.Writer.Header().Get("ETag"))
	})
}

// resetAssetETag clears the once-per-process memo so each case hashes afresh.
func resetAssetETag() {
	assetETagOnce = sync.Once{}
	assetETag = ""
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
