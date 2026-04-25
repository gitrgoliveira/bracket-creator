package main

import (
	"embed"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	res := resources.NewResources(webFiles)

	helper.WebFs = webFiles

	cmd.ExecuteWithResources(res)
}
