package excel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func TestNewStyleManager(t *testing.T) {
	file := excelize.NewFile()
	sm := NewStyleManager(file)

	require.NotNil(t, sm)
	assert.Same(t, file, sm.file)
}

func TestGetTextStyle(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetTextStyle()
	assert.Positive(t, styleID)
	assertStyleApplied(t, file, "Sheet1", "A1", styleID)
}

func TestGetPoolHeaderStyle(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetPoolHeaderStyle()
	assert.Positive(t, styleID)
	assertStyleApplied(t, file, "Sheet1", "A2", styleID)
}

func TestGetBorderStyleLeft(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetBorderStyleLeft()
	assert.Positive(t, styleID)
	assertStyleApplied(t, file, "Sheet1", "A3", styleID)
}

func TestGetBorderStyleBottom(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetBorderStyleBottom()
	assert.Positive(t, styleID)
	assertStyleApplied(t, file, "Sheet1", "A4", styleID)
}

func assertStyleApplied(t *testing.T, file *excelize.File, sheetName, cell string, styleID int) {
	t.Helper()
	require.NoError(t, file.SetCellStyle(sheetName, cell, cell, styleID))
	actualStyle, err := file.GetCellStyle(sheetName, cell)
	require.NoError(t, err)
	assert.Equal(t, styleID, actualStyle)
}
