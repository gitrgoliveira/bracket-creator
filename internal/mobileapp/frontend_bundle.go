package mobileapp

import (
	"io/fs"
	"path"
)

// FrontendBundleMissing reports whether the compiled front-end bundle is
// absent from the embedded resources. web-mobile/dist and web-mobile/vendor
// are both checked in with only a placeholder file when the front-end has
// not been built (make go/build runs esbuild): dist/app.js is the entry
// module and vendor/preact.min.js the vendored runtime web-mobile/index.html
// loads first, so either one missing means a checkout that has not run make
// go/build. A nil root counts as missing: tests wire an empty
// resources.Resources through the same router construction, and nothing can
// be served from it. fs.Sub cannot fail on this constant path, so no Sub
// call is needed here.
func FrontendBundleMissing(root fs.FS) bool {
	if root == nil {
		return true
	}
	if _, err := fs.Stat(root, path.Join(mobileWebRoot, "dist/app.js")); err != nil {
		return true
	}
	if _, err := fs.Stat(root, path.Join(mobileWebRoot, "vendor/preact.min.js")); err != nil {
		return true
	}
	return false
}
