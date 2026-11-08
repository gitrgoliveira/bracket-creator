// Package backup reads and writes tournament backup zip archives.
package backup

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
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

// The data folder's reserved directories. Branding and sponsors hold uploaded
// images and travel in a tournament archive; the rest are the store's own
// working folders and never do. The tournament store aliases these names.
const (
	BrandingDirName = "branding"
	SponsorsDirName = "sponsors"
	BackupsDirName  = "backups"
	RestoreDirName  = ".restore"
	WALDirName      = ".wal"
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
	CodeVersionMismatch          = "version_mismatch"
)

// Error is every refusal Write and Read make, and CheckAppVersion's. The HTTP
// layer answers a Read refusal as 400 archive_invalid with Code as the reason
// and CheckAppVersion's as 400 backup_version_mismatch; a Write refusal is a
// server fault (500).
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

// releaseVersion matches the versions that can be put in order: a release
// tag (v2.4.0), or the git-describe form a build from main carries
// (v2.4.0-12-gabc1234: 12 commits after v2.4.0), either with -dirty after it.
// A branch name, a short sha or "dev" (get_build_info.sh) does not match.
var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(\d+)-g[0-9a-f]+)?(?:-dirty)?$`)

// versionOrder returns v's place in release order: major, minor, patch, then
// the commits since that release (0 for the release itself). ok is false for
// a version with no order. Not semver precedence, which would put
// v2.4.0-12-gabc1234 BEFORE v2.4.0; git-describe means 12 commits after it.
func versionOrder(v string) (order [4]int, ok bool) {
	m := releaseVersion.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return order, false
	}
	for i, s := range m[1:] {
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return order, false
		}
		order[i] = n
	}
	return order, true
}

// RestorableBy reports whether the app version running can restore an
// archive whose manifest names made: the version that made it or a later one
// (operator ruling 2026-11-08). A later version brings an older backup up to
// date with the upgrades the store already runs on what it loads; an earlier
// version never reads what a later one wrote. Two versions with no order (a
// branch name, a short sha, "dev") restore each other only when they are the
// same. The one comparison, used by CheckAppVersion and by the scheduled
// job's choice of the newest daily copy, so the two never disagree.
func RestorableBy(made, running string) bool {
	made, running = strings.TrimSpace(made), strings.TrimSpace(running)
	if made == running {
		return true
	}
	m, okMade := versionOrder(made)
	r, okRunning := versionOrder(running)
	if !okMade || !okRunning {
		return false
	}
	for i := range m {
		if m[i] != r[i] {
			return m[i] < r[i]
		}
	}
	return true
}

// CheckAppVersion refuses to restore an archive that the running version of
// the app cannot restore (RestorableBy): one a later version made, or one
// whose version cannot be put in order against it. running is the version of
// the app doing the restore (version.GetVersion). It is a restore check and
// NOT part of Read, because the scheduled backup job reads every kept copy,
// later versions' included. Detail is a whole sentence a person reads; the
// HTTP layer answers it as 400 backup_version_mismatch and the CLI prints it.
func CheckAppVersion(m Manifest, running string) *Error {
	if RestorableBy(m.AppVersion, running) {
		return nil
	}
	made := strings.TrimSpace(m.AppVersion)
	running = strings.TrimSpace(running)
	which := "This backup does not say which version made it"
	if made != "" {
		which = "This backup was made with version " + made
	}
	why := ""
	_, okMade := versionOrder(made)
	_, okRunning := versionOrder(running)
	if made != "" && (!okMade || !okRunning) {
		why = " These two versions cannot be compared."
	}
	return &Error{Code: CodeVersionMismatch, Path: ManifestName, Detail: fmt.Sprintf(
		"%s and this app is version %s.%s A backup can be restored only with the version that made it or a later one, so nothing was changed.",
		which, running, why)}
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

func isWorkingDirName(name string) bool {
	return name == WALDirName || name == BackupsDirName || name == RestoreDirName
}

// IsExcludedPath reports whether p, a slash path relative to the data folder,
// is a working folder of the store or lies inside one. The working folders sit
// at the top of the data folder and directly inside a competition folder
// (competitions/<id>/), never elsewhere: the id itself is not judged, so a
// competition named "backups" is an ordinary competition.
func IsExcludedPath(p string) bool {
	segments := strings.Split(p, "/")
	if isWorkingDirName(segments[0]) {
		return true
	}
	return segments[0] == "competitions" && len(segments) >= 3 && isWorkingDirName(segments[2])
}

func validateNotExcluded(p string) error {
	if IsExcludedPath(p) {
		return &Error{Code: CodeOutsideLayout, Path: p, Detail: "entry is in an excluded directory"}
	}
	segments := strings.Split(p, "/")
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
		if (segments[0] == BrandingDirName || segments[0] == SponsorsDirName) && len(segments) >= 2 {
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
