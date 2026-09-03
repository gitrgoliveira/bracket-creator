package mobileapp

import "io/fs"

// FrontendBundleMissing reports whether the compiled front-end bundle is
// absent from the embedded resources. web-mobile/dist is checked in with only
// a placeholder file when the front-end has not been built (make go/build
// runs esbuild), so dist/app.js, the entry point web-mobile/index.html loads,
// is the marker for a real build. A nil root counts as missing: tests wire an
// empty resources.Resources through the same router construction, and
// nothing can be served from it.
func FrontendBundleMissing(root fs.FS) bool {
	if root == nil {
		return true
	}
	sub, err := fs.Sub(root, mobileWebRoot)
	if err != nil {
		return true
	}
	_, err = fs.Stat(sub, "dist/app.js")
	return err != nil
}
