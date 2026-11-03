// Package backup reads and writes tournament backup zip archives.
package backup

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

const (
	FormatVersion = 1
	ManifestName  = "manifest.json"
	MaxEntries    = 20000     // entries in the zip, manifest included
	MaxFileBytes  = 32 << 20  // per entry, uncompressed
	MaxTotalBytes = 512 << 20 // all entries, uncompressed
	MaxRatio      = 200       // uncompressed:compressed, per entry
)

const (
	CodeNotAZip                  = "not_a_zip"
	CodeManifestMissing          = "manifest_missing"
	CodeManifestInvalid          = "manifest_invalid"
	CodeFormatTooNew             = "format_too_new"
	CodeUnsafePath               = "unsafe_path"
	CodeNotRegularFile           = "not_regular_file"
	CodeOutsideLayout            = "outside_layout"
	CodeDuplicatePath            = "duplicate_path"
	CodeCaseClash                = "case_clash"
	CodeTooManyEntries           = "too_many_entries"
	CodeFileTooLarge             = "file_too_large"
	CodeTotalTooLarge            = "total_too_large"
	CodeRatioTooHigh             = "ratio_too_high"
	CodeEntryUnreadable          = "entry_unreadable"
	CodeFileNotInManifest        = "file_not_in_manifest"
	CodeFileMissing              = "file_missing"
	CodeSizeMismatch             = "size_mismatch"
	CodeChecksumMismatch         = "checksum_mismatch"
	CodeBadCompetitionID         = "bad_competition_id"
	CodeCompetitionMismatch      = "competition_mismatch"
	CodeCompetitionCount         = "competition_count"
	CodeConfigMissing            = "config_missing"
	CodeTournamentMissing        = "tournament_missing"
	CodeRestoreCreatedNotAllowed = "restore_created_not_allowed"
	CodeFrontMatterInvalid       = "front_matter_invalid"
)

// Error is every refusal Write and Read make; the HTTP layer maps it to 400 archive_invalid with Code as the reason.
type Error struct {
	Code   string // one of the Code* constants
	Path   string // the entry or competition id concerned, when there is one
	Detail string // a short human sentence
}

func (e *Error) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("backup %s (%s): %s", e.Code, e.Path, e.Detail)
	}
	return fmt.Sprintf("backup %s: %s", e.Code, e.Detail)
}

func validScope(s Scope) bool {
	return s == ScopeTournament || s == ScopeCompetition
}

func validKind(k Kind) bool {
	return k == KindManual || k == KindScheduled || k == KindBeforeRestore
}

func hasWindowsDrivePrefix(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func validatePathSafety(p string) error {
	if p == "" {
		return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path is empty"}
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path contains control character"}
		}
	}
	if strings.ContainsRune(p, '\\') {
		return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path contains backslash"}
	}
	if hasWindowsDrivePrefix(p) {
		return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path has Windows drive prefix"}
	}
	if strings.HasPrefix(p, "/") {
		return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path must be relative"}
	}
	segments := strings.Split(p, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path contains invalid segment"}
		}
	}
	if path.Clean(p) != p {
		return &Error{Code: CodeUnsafePath, Path: p, Detail: "entry path is not clean"}
	}
	return nil
}

// IsTempFileName reports whether a base filename is a temporary or write-probe
// file excluded from snapshots and backup archives.
func IsTempFileName(name string) bool {
	return strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".tmp-") || strings.HasPrefix(name, ".write-check-")
}

func validateNotExcluded(p string) error {
	segments := strings.Split(p, "/")
	for _, seg := range segments {
		if seg == ".wal" || seg == "backups" || seg == ".restore" {
			return &Error{Code: CodeOutsideLayout, Path: p, Detail: "entry is in an excluded directory"}
		}
	}
	last := segments[len(segments)-1]
	if IsTempFileName(last) {
		return &Error{Code: CodeOutsideLayout, Path: p, Detail: "temporary file is not allowed in archive"}
	}
	return nil
}

func validatePathForScope(p string, scope Scope) (string, error) {
	segments := strings.Split(p, "/")
	switch scope {
	case ScopeTournament:
		if p == "tournament.md" {
			return "", nil
		}
		if (segments[0] == "branding" || segments[0] == "sponsors") && len(segments) >= 2 {
			return "", nil
		}
		if segments[0] == "competitions" && len(segments) >= 3 {
			return segments[1], nil
		}
		return "", &Error{Code: CodeOutsideLayout, Path: p, Detail: "entry path is outside tournament archive layout"}
	case ScopeCompetition:
		if segments[0] == "competitions" && len(segments) >= 3 {
			return segments[1], nil
		}
		return "", &Error{Code: CodeOutsideLayout, Path: p, Detail: "entry path is outside competition archive layout"}
	default:
		return "", &Error{Code: CodeOutsideLayout, Path: p, Detail: "entry path is outside archive layout"}
	}
}
