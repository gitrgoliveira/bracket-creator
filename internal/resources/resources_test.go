package resources_test

import (
	"embed"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/stretchr/testify/assert"
)

//go:embed test_data/*
var testFiles embed.FS

//go:embed test_data/template.txt
var testTemplate embed.FS

func TestNewResources(t *testing.T) {
	res := resources.NewResources(testFiles, testTemplate)
	assert.NotNil(t, res)
}

func TestGetWebFS(t *testing.T) {
	res := resources.NewResources(testFiles, testTemplate)
	webFS := res.GetWebFS()
	assert.NotNil(t, webFS)

	file, err := webFS.Open("test_data/index.html")
	assert.NoError(t, err)
	if file != nil {
		file.Close()
	}
}

func TestGetTemplateFS(t *testing.T) {
	res := resources.NewResources(testFiles, testTemplate)
	templateFS := res.GetTemplateFS()
	assert.NotNil(t, templateFS)

	file, err := templateFS.Open("test_data/template.txt")
	assert.NoError(t, err)
	if file != nil {
		file.Close()
	}
}
