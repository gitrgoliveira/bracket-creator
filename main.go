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

// Mobile-app assets. Single-level glob (not all:) so the underscore-
// prefixed __tests__/ directory is excluded — Go embed drops _- and
// .-prefixed entries by default. Don't switch to all:web-mobile
// without first relocating __tests__/.
//
//go:embed web-mobile/*
var mobileWebFiles embed.FS

func main() {
	res := resources.NewResources(webFiles, mobileWebFiles)

	helper.WebFs = webFiles
	helper.MobileWebFs = mobileWebFiles

	cmd.ExecuteWithResources(res)
}
