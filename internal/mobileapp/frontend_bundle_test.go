package mobileapp

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestFrontendBundleMissing(t *testing.T) {
	tests := []struct {
		name    string
		fsys    fs.FS
		missing bool
	}{
		{
			name: "both dist and vendor present",
			fsys: fstest.MapFS{
				"web-mobile/dist/app.js":          &fstest.MapFile{Data: []byte("// app")},
				"web-mobile/vendor/preact.min.js": &fstest.MapFile{Data: []byte("// preact")},
			},
			missing: false,
		},
		{
			name: "dist only, vendor still a placeholder",
			fsys: fstest.MapFS{
				"web-mobile/dist/app.js": &fstest.MapFile{Data: []byte("// app")},
			},
			missing: true,
		},
		{
			name: "vendor only, dist still a placeholder",
			fsys: fstest.MapFS{
				"web-mobile/vendor/preact.min.js": &fstest.MapFile{Data: []byte("// preact")},
			},
			missing: true,
		},
		{
			name:    "placeholder only, as on a go install checkout",
			fsys:    fstest.MapFS{"web-mobile/dist/keep": &fstest.MapFile{Data: []byte("")}},
			missing: true,
		},
		{
			name:    "no web-mobile directory at all",
			fsys:    fstest.MapFS{},
			missing: true,
		},
		{
			// cmd's TestMain wires resources.NewResources(nil, nil), so the
			// router is built over a nil fs.FS; the check must answer
			// "missing" rather than panic.
			name:    "nil fs.FS",
			fsys:    nil,
			missing: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.missing, FrontendBundleMissing(tt.fsys))
		})
	}
}
