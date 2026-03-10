package tests

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// TestMain ensures helper.TemplateFile is populated for tests.
func TestMain(m *testing.M) {
	templatePath := filepath.Join("..", "template.xlsx")
	wd, err := os.Getwd()
	if err == nil && filepath.Base(wd) != "tests" {
		templatePath = "template.xlsx"
	}

	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		panic("failed to read template.xlsx: " + err.Error())
	}

	helper.TemplateFile = fstest.MapFS{
		"template.xlsx": &fstest.MapFile{
			Data:    templateData,
			Mode:    0644,
			ModTime: time.Now(),
		},
	}

	code := m.Run()
	os.Exit(code)
}
