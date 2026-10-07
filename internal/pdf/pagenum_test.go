package pdf

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStampPageNumbersPreservesPageCount(t *testing.T) {
	conv := requireSoffice(t)
	xlsx := findExampleXLSX(t)
	tmp := t.TempDir()

	pdfPath, err := conv.ConvertToPDF(context.Background(), xlsx, tmp)
	require.NoError(t, err)

	before, err := PageCount(context.Background(), pdfPath)
	require.NoError(t, err)
	require.Positive(t, before)

	stamped := filepath.Join(tmp, "stamped.pdf")
	require.NoError(t, StampPageNumbers(context.Background(), pdfPath, stamped))
	require.FileExists(t, stamped)

	after, err := PageCount(context.Background(), stamped)
	require.NoError(t, err)
	assert.Equal(t, before, after, "stamping must not add or drop pages")
}
