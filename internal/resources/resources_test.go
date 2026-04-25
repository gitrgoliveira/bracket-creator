package resources_test

import (
	"embed"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/stretchr/testify/assert"
)

//go:embed test_data/*
var testFiles embed.FS

func TestNewResources(t *testing.T) {
	res := resources.NewResources(testFiles)
	assert.NotNil(t, res)
}

func TestGetWebFS(t *testing.T) {
	res := resources.NewResources(testFiles)
	webFS := res.GetWebFS()
	assert.NotNil(t, webFS)

	file, err := webFS.Open("test_data/index.html")
	assert.NoError(t, err)
	if file != nil {
		file.Close()
	}
}
