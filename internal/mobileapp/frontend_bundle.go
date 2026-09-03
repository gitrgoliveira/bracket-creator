package mobileapp

import "io/fs"

// frontendBundleMissing reports whether the compiled front-end bundle is
// absent from the embedded resources. web-mobile/dist is checked in with only
// a placeholder file when the front-end has not been built (make go/build
// runs esbuild), so the app.js entry point is the marker for a real build.
// A nil fsys counts as missing: tests wire an empty resources.Resources
// through the same router construction, and nothing can be served from it.
func frontendBundleMissing(fsys fs.FS) bool {
	if fsys == nil {
		return true
	}
	_, err := fs.Stat(fsys, "web-mobile/dist/app.js")
	return err != nil
}
