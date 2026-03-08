package helper

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// ParseSeedsFile reads a CSV file mapping names to seed positions
func ParseSeedsFile(filePath string) ([]domain.SeedAssignment, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("seeds file is empty")
		}
		return nil, err
	}

	if len(header) < 2 {
		return nil, errors.New("invalid CSV format, expected at least 2 columns")
	}

	rankCol := -1
	nameCol := -1
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), "Rank") {
			rankCol = i
		} else if strings.EqualFold(strings.TrimSpace(h), "Name") {
			nameCol = i
		}
	}

	if rankCol == -1 || nameCol == -1 {
		return nil, errors.New("missing Rank or Name headers in CSV")
	}

	var assignments []domain.SeedAssignment

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) <= rankCol || len(record) <= nameCol {
			continue // skip malformed lines
		}

		rankStr := strings.TrimSpace(record[rankCol])
		nameStr := strings.TrimSpace(record[nameCol])

		if rankStr == "" || nameStr == "" {
			continue
		}

		rank, err := strconv.Atoi(rankStr)
		if err != nil {
			continue
		}

		assignments = append(assignments, domain.SeedAssignment{
			Name:     nameStr,
			SeedRank: rank,
		})
	}

	if err := domain.ValidateAssignments(assignments); err != nil {
		return nil, err
	}

	return assignments, nil
}
