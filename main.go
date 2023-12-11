package main

import (
	"embed"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

//go:embed web/*
var webFiles embed.FS

//go:embed template.xlsx
var templateFile embed.FS

func main() {
	helper.WebFs = webFiles
	helper.TemplateFile = templateFile
	cmd.Execute()
}
