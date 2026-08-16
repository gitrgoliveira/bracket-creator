package mobileapp

import (
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The compiled front-end under /dist/ is served with "Cache-Control: public,
// max-age=300" plus a content-derived ETag. The two compose: inside the window
// a returning client makes NO request at all, and once it lapses the ETag turns
// the re-fetch into a 0-byte 304 unless the bundle actually changed.
//
// Before this, http.FileServer over embed.FS emitted no Cache-Control, no ETag
// and no Last-Modified (embed's zero modtime makes ServeContent omit it), so a
// browser had nothing to revalidate against: a page load transferred 1.17 MB
// across 69 files with NONE served from cache. Measured after: 20 KB with the
// ETag alone (all 69 revalidated, no bodies), then 0 across 0 requests once the
// max-age window was added.
func TestBuildAssetETag(t *testing.T) {
	// Package state, restored for whatever runs next; see resetAssetETag.
	t.Cleanup(resetAssetETag)
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
	t.Cleanup(resetAssetETag)
	fsys := fstest.MapFS{"dist/a.js": {Data: []byte("x")}}

	t.Run("applies the caching policy to a dist asset", func(t *testing.T) {
		resetAssetETag()
		c, _ := gin.CreateTestContext(newRecorder())
		setStaticCacheHeaders(c, fsys, "dist/a.js")
		// Both halves, and they compose: inside the window the client makes no
		// request at all; after it the ETag turns a re-fetch into a 0-byte 304.
		assert.Equal(t, "public, max-age=300", c.Writer.Header().Get("Cache-Control"))
		assert.NotEmpty(t, c.Writer.Header().Get("ETag"))
	})

	t.Run("the window is bounded so an upgrade cannot stay invisible", func(t *testing.T) {
		// An asset cached under max-age is reused WITHOUT asking, so this bound
		// is how long a mid-event upgrade can go unseen by a loaded client.
		assert.LessOrEqual(t, staticAssetMaxAge, 15*time.Minute,
			"a long window would hide a server upgrade from clients already running")
		assert.Positive(t, staticAssetMaxAge,
			"zero would restore one conditional request per asset per load")
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
//
// It mutates PACKAGE state, so every test that calls it must also register it
// with t.Cleanup: the last subtest here deliberately leaves an empty ETag
// behind a consumed sync.Once, and setStaticCacheHeaders short-circuits on an
// empty validator. Left in place, that state leaks into whatever router test
// runs next in this package, which would then assert on Cache-Control/ETag
// against a memo it never set — passing or failing on file ordering alone.
func resetAssetETag() {
	assetETagOnce = sync.Once{}
	assetETag = ""
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
