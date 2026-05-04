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
	res := resources.NewResources(testFiles, testFiles)
	assert.NotNil(t, res)
}

func TestGetWebFS(t *testing.T) {
	res := resources.NewResources(testFiles, nil)
	webFS := res.GetWebFS()
	assert.NotNil(t, webFS)

	file, err := webFS.Open("test_data/index.html")
	assert.NoError(t, err)
	if file != nil {
		file.Close()
	}
}

func TestGetMobileWebFS(t *testing.T) {
	res := resources.NewResources(nil, testFiles)
	mobileFS := res.GetMobileWebFS()
	assert.NotNil(t, mobileFS)

	file, err := mobileFS.Open("test_data/index.html")
	assert.NoError(t, err)
	if file != nil {
		file.Close()
	}
}
