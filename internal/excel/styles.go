package excel

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// StyleManager handles Excel cell styles
type StyleManager struct {
	file *excelize.File
}

// NewStyleManager creates a new style manager
func NewStyleManager(file *excelize.File) *StyleManager {
	return &StyleManager{file: file}
}

// GetTextStyle returns a style for text cells
func (s *StyleManager) GetTextStyle() (int, error) {
	style, err := s.file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri",
			Bold: true,
			Size: 10,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating text style: %w", err)
	}
	return style, nil
}

// GetPoolHeaderStyle returns a style for pool headers
func (s *StyleManager) GetPoolHeaderStyle() (int, error) {
	style, err := s.file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri",
			Bold: true,
			Size: 14,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#C0C0C0"},
			Pattern: 1,
		},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating pool header style: %w", err)
	}
	return style, nil
}

// GetBorderStyleLeft returns a style for left borders
func (s *StyleManager) GetBorderStyleLeft() (int, error) {
	style, err := s.file.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "left", Color: "000000", Style: 1},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating left border style: %w", err)
	}
	return style, nil
}

// GetBorderStyleBottom returns a style for bottom borders
func (s *StyleManager) GetBorderStyleBottom() (int, error) {
	style, err := s.file.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "bottom", Color: "000000", Style: 1},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating bottom border style: %w", err)
	}
	return style, nil
}

// Additional style methods can be added as needed
