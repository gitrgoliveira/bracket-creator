// Package main, glossarygen emits web-mobile/{js,dist}/glossary_data.js
// from internal/domain/glossary.go (the Go-side dictionary). Invoked
// via `go generate ./internal/domain/...` (the //go:generate directive
// in glossary.go) and from `make go/build` so the JS bundle always
// reflects the latest Go source. The emitted file is checked into
// source control (so JS-only developers don't need a Go toolchain to
// run vitest); CI verifies the working tree stays clean after
// regeneration.
//
// Two-target write: the same bytes go to BOTH
//   - web-mobile/js/glossary_data.js   (vitest/source resolution)
//   - web-mobile/dist/glossary_data.js (compiled-bundle relative import)
//
// so the compiled `dist/glossary.js` (built from glossary.jsx) can do
// `import { GLOSSARY } from './glossary_data.js'` and resolve at both
// test-time AND browser-time without a server-side rewrite hack.
//
// Output shape (web-mobile/js/glossary_data.js):
//
//	// AUTO-GENERATED, do not edit by hand.
//	// Source: internal/domain/glossary.go. Regenerate via:
//	//   go generate ./internal/domain/...
//	// or `make go/build`.
//	export const GLOSSARY = { kiken: {...}, ... };
//
// The exported map mirrors domain.Glossary one-to-one. Property names
// match the Go JSON tags (id, kanji, short, tooltip, seeAlso) so the
// Preact <Term> component can consume both shapes interchangeably.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

func main() {
	out, err := generate()
	if err != nil {
		log.Fatalf("glossarygen: %v", err)
	}
	// Write to BOTH paths so the same file is reachable from vitest
	// (which resolves bare `.js` imports against the source tree) AND
	// the browser bundle (which loads `/dist/glossary.js` and needs a
	// sibling `glossary_data.js` in `/dist/`). The contents are
	// byte-identical; the duplication is harmless and avoids a
	// browser/server import-rewrite hack.
	for _, dest := range destPaths() {
		// #nosec G301 -- build-time generator, host filesystem perms apply
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			log.Fatalf("glossarygen: mkdir %s: %v", dest, err)
		}
		// #nosec G306 -- generated source file, world-readable by convention
		if err := os.WriteFile(dest, out, 0o600); err != nil {
			log.Fatalf("glossarygen: write %s: %v", dest, err)
		}
		fmt.Printf("glossarygen: wrote %d entries → %s\n", len(domain.Glossary), dest)
	}
}

// generate is the pure function under test (or under reuse if/when we
// ever want a different output target). Returns the file bytes.
func generate() ([]byte, error) {
	// Sort keys for deterministic output, without this, map-range
	// iteration order would change the file on every regenerate and
	// CI would chase a phantom diff.
	keys := make([]string, 0, len(domain.Glossary))
	for k := range domain.Glossary {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString("// AUTO-GENERATED, do not edit by hand.\n")
	buf.WriteString("// Source: internal/domain/glossary.go.\n")
	buf.WriteString("// Regenerate via `go generate ./internal/domain/...` or `make go/build`.\n")
	buf.WriteString("//\n")
	buf.WriteString("// The exported GLOSSARY map is consumed by web-mobile/js/glossary.jsx\n")
	buf.WriteString("// (the <Term> tooltip component and /glossary page).\n")
	buf.WriteString("\n")
	buf.WriteString("export const GLOSSARY = ")

	// Emit ordered entries by writing the JSON manually rather than
	// relying on encoding/json's alphabetical key ordering, the
	// latter would still sort keys but we want a comment-rich, diff-
	// stable layout (one entry per line, lower-cased keys preserved).
	buf.WriteString("{\n")
	for i, k := range keys {
		entry := domain.Glossary[k]
		marshalled, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("marshal %q: %w", k, err)
		}
		// JSON-encode the key too so any future keys with non-ident chars
		// (none today, but cheap to future-proof) stay valid JS.
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key %q: %w", k, err)
		}
		buf.WriteString("  ")
		buf.Write(keyJSON)
		buf.WriteString(": ")
		buf.Write(marshalled)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("};\n")
	buf.WriteString("\n")
	buf.WriteString("// Convenience lookup so callers can normalise case at the call site\n")
	buf.WriteString("// without re-implementing the lowercase convention everywhere.\n")
	buf.WriteString("export function lookupTerm(id) {\n")
	buf.WriteString("  if (typeof id !== 'string') return null;\n")
	buf.WriteString("  return GLOSSARY[id.trim().toLowerCase()] || null;\n")
	buf.WriteString("}\n")
	buf.WriteString("\n")
	buf.WriteString("if (typeof window !== 'undefined') {\n")
	buf.WriteString("  window.GLOSSARY = GLOSSARY;\n")
	buf.WriteString("  window.lookupTerm = lookupTerm;\n")
	buf.WriteString("}\n")

	return buf.Bytes(), nil
}

// destPaths resolves to the two write targets:
//   - <repo>/web-mobile/js/glossary_data.js      (source-tree, vitest)
//   - <repo>/web-mobile/dist/glossary_data.js    (browser, served from
//     the same dir as the compiled dist/glossary.js so the relative
//     `./glossary_data.js` import in the compiled bundle resolves)
//
// Computed off the caller's file location so the generator runs
// correctly from `go generate` regardless of the working directory.
func destPaths() []string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("glossarygen: runtime.Caller failed")
	}
	// file = .../internal/domain/internal/glossarygen/main.go
	// repo = .../
	repo := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return []string{
		filepath.Join(repo, "web-mobile", "js", "glossary_data.js"),
		filepath.Join(repo, "web-mobile", "dist", "glossary_data.js"),
	}
}
