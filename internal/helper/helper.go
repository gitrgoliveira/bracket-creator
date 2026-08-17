package helper

import (
	"bufio"
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var WebFs embed.FS
var MobileWebFs embed.FS

type RowStack struct {
	data []int
}

func (s *RowStack) Push(value int) {
	s.data = append(s.data, value)
}

func (s *RowStack) PushHighest(first int, second int) {
	if first > second {
		s.Push(first)
	} else {
		s.Push(second)
	}
}
func (s *RowStack) Pop() (int, error) {
	if len(s.data) == 0 {
		return 0, fmt.Errorf("pop on empty stack")
	}
	index := len(s.data) - 1
	value := s.data[index]
	s.data = s.data[:index]
	return value, nil
}

func (s *RowStack) Peek() (int, error) {
	if len(s.data) == 0 {
		return 0, fmt.Errorf("peek on empty stack")
	}
	return s.data[len(s.data)-1], nil
}

// RemoveDuplicates removes duplicate strings from the input slice and returns a new slice without duplicates.
//
// The function takes a parameter named input, which is a slice of strings. It represents the input slice from which duplicates and empty strings will be removed.
func RemoveDuplicates(input []string) []string {
	uniqueStrings := make(map[string]bool)
	result := make([]string, 0)

	for _, str := range input {
		if str != "" && !uniqueStrings[str] {
			uniqueStrings[str] = true
			result = append(result, str)
		} else {
			fmt.Printf("Warning: Duplicate found - %s\n", str)
		}
	}

	return result
}

// CheckDuplicateEntries scans the raw entry list (one CSV row per element)
// for duplicates and returns a list of entries that appear more than once.
// Empty strings are ignored. The returned slice preserves first-seen order
// of the offending entries; an empty result means the list is unique.
func CheckDuplicateEntries(input []string) []string {
	seen := make(map[string]int, len(input))
	order := make([]string, 0)
	for _, s := range input {
		if s == "" {
			continue
		}
		seen[s]++
		if seen[s] == 2 {
			order = append(order, s)
		}
	}
	return order
}

// ValidateCourts returns an error when n is outside the supported court range,
// [1, MaxCourts]. n < 1 is rejected so the caller does not have to guess what
// "0 courts" should mean; see MaxCourts for why the ceiling is 16.
func ValidateCourts(n int) error {
	if n < 1 {
		return fmt.Errorf("courts must be >= 1, got %d", n)
	}
	if n > MaxCourts {
		return fmt.Errorf("courts must be <= %d (the most any one competition can be allocated), got %d", MaxCourts, n)
	}
	return nil
}

// clampCourts bounds a court count to the supported range, [1, MaxCourts]. It
// is the coercing counterpart of ValidateCourts: the same range, for the paths
// that have no error channel to refuse through (the draw builder and the sheet
// writers, which are handed a count and must render something).
//
// The upper bound is not merely tidiness: CourtLabel indexes
// courtLabelAlphabet, so a count past MaxCourts is an out-of-range panic on the
// way to a pointless allocation. Every entry point validates long before here,
// so this changes no reachable behaviour -- it means no helper can be made to
// allocate or index off a number nothing checked.
//
// It lives beside ValidateCourts and CourtLabel because it states the same
// domain bound they do, and is called from the draw builder, the tree pager
// and the sheet writers alike.
func clampCourts(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxCourts {
		return MaxCourts
	}
	return n
}

// CourtLabel returns the letter label for a zero-based court index. It indexes
// courtLabelAlphabet, the same string MaxCourts is derived from, so an index at
// or past the cap panics rather than inventing a label the rest of the system
// does not accept.
func CourtLabel(i int) string {
	return string(courtLabelAlphabet[i])
}

// ShiaijoLabel is the operator-facing name of one shiaijo, e.g. "Shiaijo C".
//
// The single writer of that string. The Pool Matches and Elimination Matches
// column bands, the tree page titles and the workbook-parity reader that finds
// bands by the "Shiaijo " prefix all have to spell it identically or a band
// goes unrecognised, so the name is composed here rather than by each caller.
func ShiaijoLabel(name string) string {
	return "Shiaijo " + name
}

// CourtLabels is the default naming for a workbook laid out for n shiaijo:
// A, B, C ... It is what the CLI generators use, because `--courts 4` says how
// MANY shiaijo the draw runs on and never which ones the hall calls them.
//
// The live app must NOT use this. A competition carries its own court list and
// it need not start at A: sharing a 4-shiaijo venue by running one competition
// on A+B and another on C+D is the split the app itself recommends, and naming
// the second one's bands "Shiaijo A" and "Shiaijo B" hands its operators a
// sheet for courts that are not theirs. Pass comp.Courts instead.
func CourtLabels(n int) []string {
	return courtsPrefix(nil, clampCourts(n))
}

// courtNameAt is the name of band i, falling back to the positional letter when
// the caller supplied no name for it. The fallback keeps a short list (or a nil
// one) from producing an unnamed band rather than panicking mid-export.
func courtNameAt(courts []string, i int) string {
	if i >= 0 && i < len(courts) && courts[i] != "" {
		return courts[i]
	}
	return CourtLabel(i)
}

func OrderStringsAlphabetically(strings []*Node) []*Node {
	sort.Slice(strings, func(i, j int) bool {
		strA := strings[i]
		strB := strings[j]

		// Split the strings into prefix and suffix
		prefixA, suffixA := extractPrefixAndSuffix(strA.LeafVal)
		prefixB, suffixB := extractPrefixAndSuffix(strB.LeafVal)

		// Compare the prefixes
		if prefixA != prefixB {
			return prefixA < prefixB
		}

		// Compare the suffixes as numbers
		numA, _ := strconv.Atoi(suffixA)
		numB, _ := strconv.Atoi(suffixB)
		return numA < numB
	})

	return strings
}

// Helper function to extract prefix and suffix from a string
func extractPrefixAndSuffix(str string) (string, string) {
	lastIndex := len(str) - 1
	for i := lastIndex; i >= 0; i-- {
		if !isDigit(str[i]) {
			return str[:i+1], str[i+1:]
		}
	}
	return "", str
}

// Helper function to check if a character is a digit
func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func ReadEntriesFromFile(filePath string) ([]string, error) {
	// Validate file path to prevent directory traversal attacks
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}

	// #nosec G304 - file path is validated above
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing file: %v\n", err)
		}
	}()

	var entries []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entries = append(entries, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// ReadCSVFile reads a CSV file using encoding/csv, properly handling
// RFC 4180 quoting (fields with commas, double-quotes, or newlines).
func ReadCSVFile(filePath string) ([][]string, error) {
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}

	// #nosec G304 - file path is validated above
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing file: %v\n", err)
		}
	}()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	var records [][]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		allEmpty := true
		for _, f := range record {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if !allEmpty {
			records = append(records, record)
		}
	}

	return records, nil
}

// AssignPoolsToCourts distributes numPools pools across numCourts courts using
// contiguous blocks that match the tree sheet grouping. The first court gets
// ceil(numPools/numCourts) pools, subsequent courts get the remainder.
//
// Every court gets at least one pool WHEN numCourts <= numPools, which is what
// makes a draw's blocks all non-empty (EffectiveDrawCourts is the clamp that
// guarantees the precondition, and every draw goes through it). Asked for more
// courts than pools it does not error, it leaves the trailing courts empty; the
// error return is kept for callers that already handle one and for future
// validation.
func AssignPoolsToCourts(numPools, numCourts int) ([]int, error) {
	if numCourts < 1 {
		numCourts = 1
	}
	if numPools == 0 {
		return []int{}, nil
	}
	base := numPools / numCourts
	extra := numPools % numCourts
	result := make([]int, numPools)
	pool := 0
	for court := 0; court < numCourts; court++ {
		count := base
		if court < extra {
			count++
		}
		for j := 0; j < count; j++ {
			result[pool] = court
			pool++
		}
	}
	return result, nil
}

// SubtreeCourtIndex returns the zero-based court index for tree subtree idx
// when numSubtrees are spread across numCourts. Mirrors the grouping used by
// PoolBoundsForSubtree so that court labels are always consistent.
//
// numSubtrees is an EXACT multiple of numCourts (R8: SubdivideRegions emits
// numCourts x {1,2,4} pages, one court's pages consecutively), so for every
// draw this repo builds the division is exact and idx/pagesPerCourt never
// reaches numCourts.
//
// The one remaining non-multiple case is --single-tree, which prints ONE page
// for the whole draw; pagesPerCourt then floors to 0, is raised to 1, and the
// single page reports court 0. TreePageTitle names every shiaijo on that page
// rather than pretending it is court A's.
//
// The final clamp is for callers OUTSIDE that invariant: RenderTreePages is
// exported and takes the page list as an argument, so nothing in the type
// system stops a 5-page, 2-court call, and the quotient then addresses a court
// the competition does not have. courtNameAt answers that by inventing a
// positional letter, so a 2-shiaijo draw titled its last page "Shiaijo C" -- a
// court no operator can be standing at. Folding it onto the last real court
// instead names one that at least exists. This is not the clamp that once hid
// R8 violations by absorbing leftover pages: that fault is fixed at its source
// (SubdivideRegions emits the exact multiple), which is what makes this
// unreachable for every internal caller rather than load-bearing for any.
func SubtreeCourtIndex(numSubtrees, numCourts, idx int) int {
	// Every current caller clamps its court count, but this is the one place a
	// zero would actually divide, so enforce the invariant here (like
	// PoolBoundsForSubtree does) rather than trusting call sites to pre-clamp.
	numCourts = clampCourts(numCourts)
	pagesPerCourt := numSubtrees / numCourts
	if pagesPerCourt < 1 {
		pagesPerCourt = 1
	}
	courtIdx := idx / pagesPerCourt
	if courtIdx >= numCourts {
		return numCourts - 1
	}
	return courtIdx
}

// TreePageTitle is the shiaijo title a rendered tree page carries. Normally
// that is the one court whose region the page prints. When the whole draw is
// forced onto fewer pages than there are courts (--single-tree), the page
// carries every court's bracket, so it names the whole range instead of
// claiming a single shiaijo it does not own.
func TreePageTitle(numSubtrees int, courts []string, idx int) string {
	numCourts := clampCourts(len(courts))
	if numSubtrees > 0 && numSubtrees < numCourts {
		return ShiaijoLabel(courtNameAt(courts, 0)) + "-" + courtNameAt(courts, numCourts-1)
	}
	return ShiaijoLabel(courtNameAt(courts, SubtreeCourtIndex(numSubtrees, numCourts, idx)))
}

// ReorderPoolsForCourts deinterleaves pools so that when divided into
// contiguous court blocks, each block has balanced pool sizes and seeds are
// spread across courts. Original pool i goes to court block (i % numCourts).
// Pool names are re-assigned alphabetically after reordering.
func ReorderPoolsForCourts(pools []Pool, numCourts int) []Pool {
	if numCourts <= 1 || len(pools) <= numCourts {
		return pools
	}

	// Group pools by their round-robin court: pool i → group (i % numCourts)
	groups := make([][]Pool, numCourts)
	for i, p := range pools {
		court := i % numCourts
		groups[court] = append(groups[court], p)
	}

	// Concatenate groups: all court-0 pools first, then court-1, etc.
	result := make([]Pool, 0, len(pools))
	for _, group := range groups {
		result = append(result, group...)
	}

	// Re-assign pool names in the new order
	for i := range result {
		char := string(rune('A' + i%26))
		if i > 25 {
			char = char + char
		}
		result[i].PoolName = fmt.Sprintf("Pool %s", char)
	}

	return result
}
