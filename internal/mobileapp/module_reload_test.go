package mobileapp

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 4, G6: in the deploy window a page served by the
// previous build names its modules under that build's directory, which the new
// server answers 404 (staticCacheControlScoped), so the page runs nothing. Every
// /dist/ module tag of the real page carries onerror="bcModuleFailed(this)",
// defined inline before the first module, which reloads the page once per build
// directory (sessionStorage), and the attribute survives the server's rewrite of
// each src to /dist/<hash>/. No Content-Security-Policy is set on the page, so
// the inline handler needs none.
func TestServedPageReloadsOnceWhenItsModulesCannotBeFetched(t *testing.T) {
	page, err := os.ReadFile("../../web-mobile/index.html")
	require.NoError(t, err)
	files := fstest.MapFS{
		"web-mobile/index.html": {Data: page},
		"web-mobile/dist/a.js":  {Data: []byte(`export const a = 1;`)},
	}
	r := newAssetRouter(t, files)
	w := getPath(r, "/")
	require.Equal(t, http.StatusOK, w.Code)
	served := w.Body.String()
	assert.Empty(t, w.Header().Get("Content-Security-Policy"), "no policy forbids the inline handler")

	tags := regexp.MustCompile(`<script type="module" src="/dist/[^"]+"[^>]*>`).FindAllString(served, -1)
	require.NotEmpty(t, tags, "the page names its modules under /dist/")
	for _, tag := range tags {
		assert.Contains(t, tag, `onerror="bcModuleFailed(this)"`, "every module tag reloads the page when it cannot be fetched: %s", tag)
		assert.NotContains(t, tag, `src="/dist/a.js"`, "and is named under the build's directory: %s", tag)
	}
	define := strings.Index(served, "window.bcModuleFailed = function")
	require.GreaterOrEqual(t, define, 0, "the handler is defined inline")
	assert.Less(t, define, strings.Index(served, tags[0]), "before the first module tag")
	assert.Contains(t, served, "sessionStorage", "once per build directory per tab")
}
