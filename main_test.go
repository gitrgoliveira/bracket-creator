package main

import (
	"io/fs"
	"strings"
	"testing"
)

// TestEmbed_ExcludesNpmAndTestArtefacts pins the main.go embed patterns
// against the regression where `//go:embed all:web` recursively
// embedded `web/node_modules/` (36 MB) and `web/tests/*.spec.js` into
// the production binary, then publicly served them at /static/. If a
// future change widens the patterns, this test fails immediately
// instead of waiting for someone to notice the binary growing.
//
// Run from `go test ./...` (root package). `make go/test` covers this
// because the Makefile target globs include the repo root.
func TestEmbed_ExcludesNpmAndTestArtefacts(t *testing.T) {
	forbidden := []string{
		"node_modules",
		"/tests/",
		"package.json",
		"package-lock.json",
		"vitest.config.js",
	}

	err := fs.WalkDir(webFiles, "web", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, f := range forbidden {
			if strings.Contains(path, f) {
				t.Errorf("embed.FS contains forbidden entry %q (matches %q) — production binary leak", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded web FS: %v", err)
	}
}
