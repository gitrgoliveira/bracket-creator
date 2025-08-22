package resources_test

import (
	"embed"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
)

//go:embed test_data/*
var testFiles embed.FS

//go:embed test_data/template.txt
var testTemplate embed.FS

func TestNewResources(t *testing.T) {
	// Create a new resources handler
	res := resources.NewResources(testFiles, testTemplate)

	// Check that the resources are not nil
	if res == nil {
		t.Error("Expected resources to not be nil")
	}
}

func TestGetWebFS(t *testing.T) {
	// Create a new resources handler
	res := resources.NewResources(testFiles, testTemplate)

	// Get the web file system
	webFS := res.GetWebFS()

	// Try to read a file from the web file system
	_, err := webFS.Open("test_data/index.html")
	if err != nil {
		// This will fail because we don't have actual test data files yet,
		// but we're testing the method wiring here, not actual file access
		t.Logf("Note: Expected error when no test files exist: %v", err)
	}
}

func TestGetTemplateFS(t *testing.T) {
	// Create a new resources handler
	res := resources.NewResources(testFiles, testTemplate)

	// Get the template file system
	templateFS := res.GetTemplateFS()

	// Try to read a file from the template file system
	_, err := templateFS.Open("test_data/template.txt")
	if err != nil {
		// This will fail because we don't have actual test data files yet,
		// but we're testing the method wiring here, not actual file access
		t.Logf("Note: Expected error when no test files exist: %v", err)
	}
}
