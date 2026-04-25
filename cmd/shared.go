package cmd

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// openOutputFile opens (or creates) the file at outputPath for appending and
// returns the file and a buffered writer over it.  The caller must defer
// both Close and Flush.
func openOutputFile(outputPath string) (*os.File, *bufio.Writer, error) {
	f, err := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) // #nosec G304 — path is user-supplied CLI argument
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open output file: %w", err)
	}
	return f, bufio.NewWriter(f), nil
}

// processEntries validates the entry list (rejecting duplicates), optionally
// shuffles, and converts raw CSV entry strings into Player objects. Duplicate
// entries are returned as a hard error so the caller (CLI or web handler)
// can surface them to the user instead of silently dropping rows.
func processEntries(entries []string, determined bool, withZekkenName bool) ([]helper.Player, error) {
	if dups := helper.CheckDuplicateEntries(entries); len(dups) > 0 {
		return nil, fmt.Errorf("duplicate participant entries found: %v", dups)
	}
	// Drop empty strings (blank lines) without warning — duplicates have
	// already been rejected above.
	entries = helper.RemoveDuplicates(entries)
	if !determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}
	players, err := helper.CreatePlayers(entries, withZekkenName)
	if err != nil {
		return nil, err
	}
	return players, nil
}
