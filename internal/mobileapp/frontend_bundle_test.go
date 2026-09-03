package mobileapp

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestFrontendBundleMissing(t *testing.T) {
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		missing bool
	}{
		{
			name:    "built bundle present",
			fsys:    fstest.MapFS{"web-mobile/dist/app.js": &fstest.MapFile{Data: []byte("// app")}},
			missing: false,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.missing, frontendBundleMissing(tt.fsys))
		})
	}
}

func TestFrontendBundleMissing_NilFS(t *testing.T) {
	// cmd's TestMain wires resources.NewResources(nil, nil), so the router is
	// built over a nil fs.FS; the check must answer "missing" rather than panic.
	assert.True(t, frontendBundleMissing(nil))
}
