package main

import (
	"embed"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
)

// Excel CLI web wrapper assets. List explicit patterns so the npm
// artefacts created by `cd web && npm install` (node_modules,
// package.json, package-lock.json, vitest.config.js) and the vitest
// spec files (web/tests/*.spec.js) stay OUT of the production binary —
// previously `all:web` recursed into them and the binary ballooned by
// ~36 MB while publicly serving every npm dependency at /static/.
// When adding new top-level files under web/, extend this list.
//
//go:embed web/index.html web/favicon.jpeg web/logo.jpeg
//go:embed web/css web/js
var webFiles embed.FS

// Mobile-app assets. The glob embeds all files present at build time.
// After `make go/build` runs esbuild, this includes css, js, dist and vendor.
// Explicit paths are not used here because dist/ and vendor/ are generated
// by esbuild and absent in a clean checkout — listing them would break
// `go test ./...` before a build has run. The trade-off: a local binary
// built after `npm install` will also embed node_modules/, making it
// larger. Production Docker builds and CI run in clean envs where
// node_modules/ is absent, so the artifact size is not affected there.
//
//go:embed web-mobile/*
var mobileWebFiles embed.FS

func main() {
	res := resources.NewResources(webFiles, mobileWebFiles)

	helper.WebFs = webFiles
	helper.MobileWebFs = mobileWebFiles

	cmd.ExecuteWithResources(res)
}
