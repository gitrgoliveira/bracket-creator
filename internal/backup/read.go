package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
)

// Checks provides caller-supplied validators so backup stays decoupled from internal/state.
type Checks struct {
	ValidID     func(id string) error
	FrontMatter func(b []byte) error
}

// Archive is a validated backup archive returned by Read.
type Archive struct {
	Manifest Manifest
	Files    map[string][]byte // every file except manifest.json, keyed by its zip path
}

// Read opens and validates a backup zip archive before returning its manifest and files.
func Read(r io.ReaderAt, size int64, checks Checks) (*Archive, error) {
	if checks.ValidID == nil || checks.FrontMatter == nil {
		return nil, errors.New("backup: Checks callbacks must not be nil")
	}
	if r == nil || size < 0 {
		return nil, &Error{Code: CodeNotAZip, Detail: "archive reader is invalid"}
	}

	zr, zipErr := zip.NewReader(r, size)
	if zipErr != nil && !errors.Is(zipErr, zip.ErrInsecurePath) {
		return nil, &Error{Code: CodeNotAZip, Detail: "input is not a valid zip archive"}
	}
	if zr == nil {
		return nil, &Error{Code: CodeUnsafePath, Detail: "archive contains an insecure path"}
	}

	if len(zr.File) > MaxEntries {
		return nil, &Error{Code: CodeTooManyEntries, Detail: "archive contains too many entries"}
	}

	manifestFile, err := validateZipHeaders(zr.File)
	if err != nil {
		return nil, err
	}
	if zipErr != nil {
		return nil, &Error{Code: CodeUnsafePath, Detail: "archive contains an insecure path"}
	}

	if manifestFile == nil {
		return nil, &Error{Code: CodeManifestMissing, Path: ManifestName, Detail: "manifest.json is missing from archive"}
	}

	var runningTotal int64
	manifestBytes, err := readZipEntry(manifestFile, &runningTotal)
	if err != nil {
		return nil, err
	}

	manifest, err := decodeAndValidateManifest(manifestBytes, checks)
	if err != nil {
		return nil, err
	}

	files, err := readAllDataEntries(zr.File, &runningTotal)
	if err != nil {
		return nil, err
	}

	if err := reconcileManifestAndFiles(zr.File, manifest.Files, files); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	if err := validateLayoutAndCompetitions(manifest.Scope, manifest.Kind, manifest.Competitions, paths); err != nil {
		return nil, err
	}

	if err := validateFrontMatter(manifest, files, checks); err != nil {
		return nil, err
	}

	return &Archive{
		Manifest: manifest,
		Files:    files,
	}, nil
}

func validateZipHeaders(entries []*zip.File) (*zip.File, error) {
	seenExact := make(map[string]bool, len(entries))
	seenLower := make(map[string]string, len(entries))
	var headerTotal uint64
	var manifestFile *zip.File

	for _, zf := range entries {
		name := zf.Name

		if strings.HasSuffix(name, "/") {
			trimmed := strings.TrimSuffix(name, "/")
			if err := validatePathSafety(trimmed); err != nil {
				return nil, &Error{Code: CodeUnsafePath, Path: name, Detail: "entry path is unsafe"}
			}
			return nil, &Error{Code: CodeNotRegularFile, Path: name, Detail: "directory entries are not allowed"}
		}

		if err := validatePathSafety(name); err != nil {
			return nil, err
		}

		mode := zf.Mode()
		if mode.IsDir() || mode&fs.ModeSymlink != 0 || mode&fs.ModeType != 0 {
			return nil, &Error{Code: CodeNotRegularFile, Path: name, Detail: "entry is not a regular file"}
		}

		if err := validateNotExcluded(name); err != nil {
			return nil, err
		}

		if seenExact[name] {
			return nil, &Error{Code: CodeDuplicatePath, Path: name, Detail: "duplicate entry path in archive"}
		}
		seenExact[name] = true

		lower := strings.ToLower(name)
		if prev, ok := seenLower[lower]; ok && prev != name {
			return nil, &Error{Code: CodeCaseClash, Path: name, Detail: "entry path clashes in case with " + prev}
		}
		seenLower[lower] = name

		if zf.UncompressedSize64 > MaxFileBytes {
			return nil, &Error{Code: CodeFileTooLarge, Path: name, Detail: "entry header size exceeds maximum file size"}
		}
		headerTotal += zf.UncompressedSize64
		if headerTotal > MaxTotalBytes {
			return nil, &Error{Code: CodeTotalTooLarge, Path: name, Detail: "archive header sizes exceed maximum total size"}
		}

		if zf.CompressedSize64 == 0 {
			if zf.UncompressedSize64 > 0 {
				return nil, &Error{Code: CodeRatioTooHigh, Path: name, Detail: "entry compression ratio exceeds maximum"}
			}
		} else if zf.CompressedSize64 <= MaxFileBytes && zf.UncompressedSize64 > MaxRatio*zf.CompressedSize64 {
			return nil, &Error{Code: CodeRatioTooHigh, Path: name, Detail: "entry compression ratio exceeds maximum"}
		}

		if name == ManifestName {
			manifestFile = zf
		}
	}

	return manifestFile, nil
}

func readZipEntry(zf *zip.File, runningTotal *int64) ([]byte, error) {
	if zf.UncompressedSize64 > MaxFileBytes {
		return nil, &Error{Code: CodeFileTooLarge, Path: zf.Name, Detail: "entry header size exceeds maximum file size"}
	}
	expectedSize := int64(zf.UncompressedSize64)

	rc, err := zf.Open()
	if err != nil {
		return nil, &Error{Code: CodeEntryUnreadable, Path: zf.Name, Detail: "cannot open zip entry"}
	}

	data, readErr := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
	closeErr := rc.Close()

	n := int64(len(data))
	if n > MaxFileBytes {
		return nil, &Error{Code: CodeFileTooLarge, Path: zf.Name, Detail: "entry uncompressed size exceeds maximum file size"}
	}

	*runningTotal += n
	if *runningTotal > MaxTotalBytes {
		return nil, &Error{Code: CodeTotalTooLarge, Path: zf.Name, Detail: "archive uncompressed size exceeds maximum total size"}
	}

	if readErr != nil && !errors.Is(readErr, zip.ErrFormat) {
		return nil, &Error{Code: CodeEntryUnreadable, Path: zf.Name, Detail: "cannot read zip entry"}
	}
	if n != expectedSize {
		return nil, &Error{Code: CodeSizeMismatch, Path: zf.Name, Detail: "entry size does not match header"}
	}
	if readErr != nil || closeErr != nil {
		return nil, &Error{Code: CodeEntryUnreadable, Path: zf.Name, Detail: "cannot read zip entry"}
	}

	return data, nil
}

func decodeAndValidateManifest(manifestBytes []byte, checks Checks) (Manifest, error) {
	var probe struct {
		Format *int `json:"format"`
	}
	if err := json.Unmarshal(manifestBytes, &probe); err != nil || probe.Format == nil {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "manifest is not valid JSON"}
	}
	if *probe.Format > FormatVersion {
		return Manifest{}, &Error{Code: CodeFormatTooNew, Path: ManifestName, Detail: "archive format version is newer than supported"}
	}
	if *probe.Format < 1 {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "archive format version is invalid"}
	}

	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(manifestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "manifest JSON does not match schema"}
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "manifest contains trailing data"}
	}

	if !validScope(manifest.Scope) {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "invalid manifest scope"}
	}
	if !validKind(manifest.Kind) {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "invalid manifest kind"}
	}
	if manifest.CreatedAt.IsZero() {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "manifest createdAt is missing"}
	}
	if manifest.Competitions == nil || manifest.Files == nil {
		return Manifest{}, &Error{Code: CodeManifestInvalid, Path: ManifestName, Detail: "manifest competitions and files arrays are required"}
	}
	if manifest.RestoreCreated != nil && manifest.Kind != KindBeforeRestore {
		return Manifest{}, &Error{Code: CodeRestoreCreatedNotAllowed, Path: ManifestName, Detail: "restoreCreated is only allowed for before-restore archives"}
	}

	seenFiles := make(map[string]bool, len(manifest.Files))
	for _, fe := range manifest.Files {
		if fe.Path == "" || fe.Path == ManifestName || seenFiles[fe.Path] {
			return Manifest{}, &Error{Code: CodeManifestInvalid, Path: fe.Path, Detail: "invalid or duplicate file path in manifest"}
		}
		seenFiles[fe.Path] = true
		if fe.Size < 0 || !isValidLowerHexSHA256(fe.SHA256) {
			return Manifest{}, &Error{Code: CodeManifestInvalid, Path: fe.Path, Detail: "invalid size or sha256 in manifest"}
		}
	}

	var restoreCreated []string
	if manifest.RestoreCreated != nil {
		restoreCreated = *manifest.RestoreCreated
	}
	if err := validateManifestIDs(manifest.Competitions, restoreCreated, checks.ValidID); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func isValidLowerHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func readAllDataEntries(entries []*zip.File, runningTotal *int64) (map[string][]byte, error) {
	files := make(map[string][]byte, len(entries))
	for _, zf := range entries {
		if zf.Name == ManifestName {
			continue
		}
		data, err := readZipEntry(zf, runningTotal)
		if err != nil {
			return nil, err
		}
		files[zf.Name] = data
	}
	return files, nil
}

func reconcileManifestAndFiles(entries []*zip.File, manifestFiles []FileEntry, files map[string][]byte) error {
	manifestByPath := make(map[string]FileEntry, len(manifestFiles))
	for _, fe := range manifestFiles {
		manifestByPath[fe.Path] = fe
	}

	for _, zf := range entries {
		if zf.Name == ManifestName {
			continue
		}
		if _, ok := manifestByPath[zf.Name]; !ok {
			return &Error{Code: CodeFileNotInManifest, Path: zf.Name, Detail: "archive entry is not listed in manifest"}
		}
	}

	for _, fe := range manifestFiles {
		data, ok := files[fe.Path]
		if !ok {
			return &Error{Code: CodeFileMissing, Path: fe.Path, Detail: "file listed in manifest is missing from archive"}
		}
		if int64(len(data)) != fe.Size {
			return &Error{Code: CodeSizeMismatch, Path: fe.Path, Detail: "file size does not match manifest"}
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != fe.SHA256 {
			return &Error{Code: CodeChecksumMismatch, Path: fe.Path, Detail: "file SHA-256 checksum does not match manifest"}
		}
	}

	return nil
}

// safeSegmentID is the check Write holds an id to, since it has no caller
// supplied Checks: an id becomes a path segment, so it must be non-empty and
// free of separators and dot segments. Read applies the caller's stricter
// ValidID on top.
func safeSegmentID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return errors.New("competition id must be a single path segment")
	}
	return nil
}

func validateManifestIDs(comps []CompetitionRef, restoreCreated []string, validID func(string) error) error {
	seenComps := make(map[string]bool, len(comps))
	for _, c := range comps {
		if seenComps[c.ID] {
			return &Error{Code: CodeManifestInvalid, Path: c.ID, Detail: "duplicate competition id in manifest"}
		}
		seenComps[c.ID] = true
		if err := validID(c.ID); err != nil {
			return &Error{Code: CodeBadCompetitionID, Path: c.ID, Detail: err.Error()}
		}
	}
	seenRestore := make(map[string]bool, len(restoreCreated))
	for _, id := range restoreCreated {
		if seenRestore[id] {
			return &Error{Code: CodeManifestInvalid, Path: id, Detail: "duplicate id in restoreCreated"}
		}
		seenRestore[id] = true
		if seenComps[id] {
			return &Error{Code: CodeManifestInvalid, Path: id, Detail: "competition id appears in both competitions and restoreCreated"}
		}
		if err := validID(id); err != nil {
			return &Error{Code: CodeBadCompetitionID, Path: id, Detail: err.Error()}
		}
	}
	return nil
}

func validateLayoutAndCompetitions(scope Scope, kind Kind, comps []CompetitionRef, paths []string) error {
	folderIDs := make(map[string]bool)
	hasConfigMD := make(map[string]bool)
	hasTournamentMD := false

	for _, p := range paths {
		compID, err := validatePathForScope(p, scope)
		if err != nil {
			return err
		}
		if p == "tournament.md" {
			hasTournamentMD = true
		}
		if compID != "" {
			folderIDs[compID] = true
			if p == "competitions/"+compID+"/config.md" {
				hasConfigMD[compID] = true
			}
		}
	}

	if scope == ScopeTournament && !hasTournamentMD {
		return &Error{Code: CodeTournamentMissing, Path: "tournament.md", Detail: "tournament.md is required for tournament scope"}
	}

	manifestCompIDs := make(map[string]bool, len(comps))
	for _, c := range comps {
		manifestCompIDs[c.ID] = true
	}

	var mismatchIDs []string
	for id := range folderIDs {
		if !manifestCompIDs[id] {
			mismatchIDs = append(mismatchIDs, id)
		}
	}
	for id := range manifestCompIDs {
		if !folderIDs[id] {
			mismatchIDs = append(mismatchIDs, id)
		}
	}
	if len(mismatchIDs) > 0 {
		sort.Strings(mismatchIDs)
		return &Error{
			Code:   CodeCompetitionMismatch,
			Path:   mismatchIDs[0],
			Detail: "competition folders in archive do not match manifest competitions",
		}
	}

	sortedCompIDs := make([]string, 0, len(manifestCompIDs))
	for id := range manifestCompIDs {
		sortedCompIDs = append(sortedCompIDs, id)
	}
	sort.Strings(sortedCompIDs)

	for _, id := range sortedCompIDs {
		if !hasConfigMD[id] {
			return &Error{
				Code:   CodeConfigMissing,
				Path:   "competitions/" + id + "/config.md",
				Detail: "competition folder is missing config.md",
			}
		}
	}

	if scope == ScopeCompetition {
		if kind == KindBeforeRestore {
			if len(comps) > 1 {
				return &Error{Code: CodeCompetitionCount, Detail: "before-restore competition archive may hold at most one competition"}
			}
		} else if len(comps) != 1 {
			return &Error{Code: CodeCompetitionCount, Detail: "competition archive must hold exactly one competition"}
		}
	}

	return nil
}

func validateFrontMatter(manifest Manifest, files map[string][]byte, checks Checks) error {
	if data, ok := files["tournament.md"]; ok {
		if err := checks.FrontMatter(data); err != nil {
			return &Error{Code: CodeFrontMatterInvalid, Path: "tournament.md", Detail: err.Error()}
		}
	}

	compIDs := make([]string, 0, len(manifest.Competitions))
	for _, c := range manifest.Competitions {
		compIDs = append(compIDs, c.ID)
	}
	sort.Strings(compIDs)

	for _, id := range compIDs {
		cfgPath := "competitions/" + id + "/config.md"
		if err := checks.FrontMatter(files[cfgPath]); err != nil {
			return &Error{Code: CodeFrontMatterInvalid, Path: cfgPath, Detail: err.Error()}
		}
	}

	return nil
}
