package main

import (
	"embed"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
)

//go:embed web/*
var webFiles embed.FS

//go:embed web-mobile/*
var mobileWebFiles embed.FS

func main() {
	res := resources.NewResources(webFiles, mobileWebFiles)

	helper.WebFs = webFiles
	helper.MobileWebFs = mobileWebFiles

	cmd.ExecuteWithResources(res)
}
