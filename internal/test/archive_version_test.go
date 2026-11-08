package test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, body)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestArchiveWithAppVersion(t *testing.T) {
	in := zipOf(t, map[string]string{
		"manifest.json": `{"format":1,"appVersion":"v2.4.0","scope":"tournament"}`,
		"tournament.md": "---\nname: Cup\n---\n",
	})
	out, err := ArchiveWithAppVersion(in, "v2.3.0")
	require.NoError(t, err)

	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	require.NoError(t, err)
	got := map[string][]byte{}
	for _, f := range zr.File {
		body, err := readZipEntry(f)
		require.NoError(t, err)
		got[f.Name] = body
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(got["manifest.json"], &m))
	assert.Equal(t, "v2.3.0", m["appVersion"])
	assert.Equal(t, "tournament", m["scope"], "the rest of the manifest is kept")
	assert.Equal(t, "---\nname: Cup\n---\n", string(got["tournament.md"]))
}

func TestArchiveWithAppVersion_Refusals(t *testing.T) {
	_, err := ArchiveWithAppVersion([]byte("not a zip"), "v1")
	assert.Error(t, err)
	_, err = ArchiveWithAppVersion(zipOf(t, map[string]string{"manifest.json": "{not json"}), "v1")
	assert.ErrorContains(t, err, "manifest.json")
}
