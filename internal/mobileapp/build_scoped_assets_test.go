package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 3, F3: the modules are served from a build-scoped
// directory. The page (no-store) names every module under /dist/<hash>/, the
// content hash of the compiled front-end (buildAssetETag, never commit.txt,
// which two development builds of one commit share), and the modules are served
// there "immutable", so a load makes no conditional request for them (mp-6o1a:
// 69 per load on a slow link) and the page and its code are still always one
// build (S2 a). Every import is relative, so a module's imports resolve under
// the same prefix. A prefix that is not the current build's is a 404.
func newAssetRouter(t *testing.T, files fstest.MapFS) http.Handler {
	t.Helper()
	dir, err := os.MkdirTemp("", "assets-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	hub := NewHub()
	t.Cleanup(hub.Close)
	r, _, limiter := NewRouterWithHub(store, engine.New(store), resources.NewResources(nil, files), nil, hub, false)
	t.Cleanup(limiter.Close)
	return r
}

func getPath(r http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestBuildScopedAssets(t *testing.T) {
	files := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte(`<html><head>
<script type="module" src="/dist/a.js"></script>
<script type="module" src="/dist/b.jsx"></script>
<!-- The server rewrites "/dist/X.jsx" to the compiled "dist/X.js" -->
</head></html>`)},
		"web-mobile/dist/a.js": {Data: []byte(`import "./b.jsx";`)},
		"web-mobile/dist/b.js": {Data: []byte(`export const b = 1;`)},
	}
	r := newAssetRouter(t, files)
	prefix := strings.Trim(buildAssetETag(fstest.MapFS{
		"dist/a.js": files["web-mobile/dist/a.js"],
		"dist/b.js": files["web-mobile/dist/b.js"],
	}), `"`)
	require.Len(t, prefix, 16)

	t.Run("the page names every module under the build's prefix", func(t *testing.T) {
		w := getPath(r, "/")
		require.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, `src="/dist/`+prefix+`/a.js"`)
		assert.Contains(t, body, `src="/dist/`+prefix+`/b.jsx"`)
		assert.NotContains(t, body, `src="/dist/a.js"`)
		assert.Contains(t, body, `"/dist/X.jsx"`, "only script sources are rewritten, never the comments")
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	})

	t.Run("a module under the current prefix is immutable, and its .jsx import resolves there", func(t *testing.T) {
		w := getPath(r, "/dist/"+prefix+"/a.js")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, `import "./b.jsx";`, w.Body.String())
		assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
		assert.NotEmpty(t, w.Header().Get("ETag"))

		w = getPath(r, "/dist/"+prefix+"/b.jsx")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, `export const b = 1;`, w.Body.String())
		assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
	})

	t.Run("a prefix that is not the current build's is a 404, never the page", func(t *testing.T) {
		for _, path := range []string{"/dist/0123456789abcdef/a.js", "/dist/0123456789abcdef/b.jsx", "/dist/0123456789abcdef/gone"} {
			w := getPath(r, path)
			assert.Equal(t, http.StatusNotFound, w.Code, path)
			assert.NotContains(t, w.Body.String(), "<html", path)
		}
	})

	t.Run("an unprefixed module still loads, revalidated after five minutes", func(t *testing.T) {
		w := getPath(r, "/dist/a.js")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
		w = getPath(r, "/dist/b.jsx")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
	})
}

// With no hash (an unreadable dist tree) the page keeps its plain /dist/ paths:
// a prefix nothing could serve would leave the page without its code.
func TestBuildScopedAssets_NoHashNoPrefix(t *testing.T) {
	r := newAssetRouter(t, fstest.MapFS{
		"web-mobile/index.html": {Data: []byte(`<script type="module" src="/dist/a.js"></script>`)},
	})
	w := getPath(r, "/")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `src="/dist/a.js"`)
}
