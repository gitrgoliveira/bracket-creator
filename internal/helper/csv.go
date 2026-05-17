package helper

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// ParseSeedsFile reads a CSV file mapping names to seed positions
func ParseSeedsFile(filePath string) ([]domain.SeedAssignment, error) {
	// cleanse the file path to mitigate G304
	cleanPath := filepath.Clean(filePath)
	file, err := os.Open(cleanPath) // #nosec G304
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing seeds file: %v\n", err)
		}
	}()

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
	dojoCol := -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "rank":
			rankCol = i
		case "name":
			nameCol = i
		case "dojo":
			dojoCol = i
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

		dojoStr := ""
		if dojoCol >= 0 && len(record) > dojoCol {
			dojoStr = strings.TrimSpace(record[dojoCol])
		}

		assignments = append(assignments, domain.SeedAssignment{
			Name:     nameStr,
			Dojo:     dojoStr,
			SeedRank: rank,
		})
	}

	if err := domain.ValidateAssignments(assignments); err != nil {
		return nil, err
	}

	return assignments, nil
}
