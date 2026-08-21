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

// ParseSeedsFile reads a CSV file mapping names to seed positions and REJECTS a
// set that is not a valid seeding (duplicate ranks, or ranks that do not run
// 1..N without a gap).
//
// Use this wherever the seed list is about to be USED: a --seeds file handed to
// the CLI is a complete input, and a draw must never be built from a seeding the
// operator has not finished. Use ReadSeedsFileRaw where the seed list is about
// to be SHOWN, because refusing to display an invalid set is how the operator
// stops being told about it.
func ParseSeedsFile(filePath string) ([]domain.SeedAssignment, error) {
	assignments, err := ReadSeedsFileRaw(filePath)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateAssignments(assignments); err != nil {
		return nil, err
	}
	return assignments, nil
}

// ReadSeedsFileRaw reads the same file and returns exactly what it holds,
// WITHOUT judging whether the set is a usable seeding.
//
// This exists because validating on read made an invalid set INVISIBLE. The
// seeding panel persists each rank as it is typed, so an operator who enters
// seed 4 before seeds 1 to 3 leaves a gapped file behind; the participants list
// then read it, got an error, discarded it, and rendered "0 seeded" with no
// warning at all. The operator was not told their seeds were broken, and the
// next edit wrote the empty view back over them.
//
// So display reads raw and the UI warns about what it sees (it already knows how
// to say "seed gap detected: rank 1, 2, 3 are missing"), while every path that
// CONSUMES seeds keeps using ParseSeedsFile and refuses.
func ReadSeedsFileRaw(filePath string) ([]domain.SeedAssignment, error) {
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

	return assignments, nil
}
