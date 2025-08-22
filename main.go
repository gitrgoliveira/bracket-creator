package main

import (
	"embed"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/gitrgoliveira/bracket-creator/internal/helper" // Keep for compatibility during transition
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
)

//go:embed web/*
var webFiles embed.FS

//go:embed template.xlsx
var templateFile embed.FS

func main() {
	// Create resource handler
	res := resources.NewResources(webFiles, templateFile)

	// For compatibility during transition
	helper.WebFs = webFiles
	helper.TemplateFile = templateFile

	// Execute commands with resources
	cmd.ExecuteWithResources(res)
}
