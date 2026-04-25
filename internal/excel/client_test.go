package excel_test

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestNewClient(t *testing.T) {
	client, err := excel.NewClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()
}

func TestSaveFile(t *testing.T) {
	file, err := os.CreateTemp("", "test-excel-*.xlsx")
	if err != nil {
		t.Skip("Skipping test due to failure to create temp file")
	}
	tempFileName := file.Name()
	file.Close()
	os.Remove(tempFileName)
	defer os.Remove(tempFileName)

	excelFile := excelize.NewFile()
	defer excelFile.Close()

	client := &excel.Client{}
	client.SetFileForTest(excelFile)

	err = client.SaveFile(tempFileName)
	require.NoError(t, err)

	_, err = os.Stat(tempFileName)
	require.NoError(t, err)

	err = client.SaveFile("/nonexistent/directory/file.xlsx")
	assert.Error(t, err)
}

func TestClose(t *testing.T) {
	excelFile := excelize.NewFile()

	client := &excel.Client{}
	client.SetFileForTest(excelFile)

	err := client.Close()
	require.NoError(t, err)

	// Second close must fail (file is nil after first close).
	err = client.Close()
	assert.Error(t, err)
}
