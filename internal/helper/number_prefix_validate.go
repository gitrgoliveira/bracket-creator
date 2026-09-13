package helper

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateNumberPrefix trims s and checks it against MaxNumberPrefixLen. The
// count is RUNES, not bytes: the cap is stated in "characters" at every
// caller (the CLI flag help, the admin UI's maxLength, the printed
// Tags/Names-to-Print sheets), so a byte-length check would wrongly refuse a
// short multi-byte prefix like "ÖÖ" (2 characters, 4 UTF-8 bytes).
//
// This is the ONE owner of that check (PR #416 finding 4): cmd/shared.go's
// resolveNumberPrefix (CLI) and mobileapp's validateCompetitionLengths (web)
// both used to carry their own copy of the same rune-count comparison against
// the same cap. Returns the trimmed value on success. On failure the
// returned error names the cap but not the offending value or field, since
// each existing caller's own message is pinned by its own tests, callers
// reformat around this error (or ignore it and build their own) rather than
// surface it verbatim.
func ValidateNumberPrefix(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if utf8.RuneCountInString(trimmed) > MaxNumberPrefixLen {
		return "", fmt.Errorf("number prefix must be at most %d characters", MaxNumberPrefixLen)
	}
	return trimmed, nil
}
