package backup

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
)

var fixedHeaderTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Write writes a deterministic backup zip archive for the given metadata and file map.
func Write(w io.Writer, meta Meta, files map[string][]byte) error {
	if !validScope(meta.Scope) {
		return &Error{Code: CodeManifestInvalid, Detail: "invalid backup scope"}
	}
	if !validKind(meta.Kind) {
		return &Error{Code: CodeManifestInvalid, Detail: "invalid backup kind"}
	}
	if meta.Kind != KindBeforeRestore && len(meta.RestoreCreated) > 0 {
		return &Error{Code: CodeRestoreCreatedNotAllowed, Detail: "restoreCreated is only allowed for before-restore archives"}
	}
	if len(files)+1 > MaxEntries {
		return &Error{Code: CodeTooManyEntries, Detail: "archive exceeds maximum entry count"}
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	seenLower := make(map[string]string, len(files)+1)
	seenLower[ManifestName] = ManifestName

	var totalBytes int64
	fileEntries := make([]FileEntry, 0, len(paths))

	for _, p := range paths {
		if err := validatePathSafety(p); err != nil {
			return err
		}
		if err := validateNotExcluded(p); err != nil {
			return err
		}
		if p == ManifestName {
			return &Error{Code: CodeOutsideLayout, Path: p, Detail: "manifest.json cannot be supplied as a data file"}
		}
		if _, err := validatePathForScope(p, meta.Scope); err != nil {
			return err
		}
		lower := strings.ToLower(p)
		if prev, ok := seenLower[lower]; ok && prev != p {
			return &Error{Code: CodeCaseClash, Path: p, Detail: "entry path clashes in case with " + prev}
		}
		seenLower[lower] = p

		body := files[p]
		size := int64(len(body))
		if size > MaxFileBytes {
			return &Error{Code: CodeFileTooLarge, Path: p, Detail: "file exceeds maximum entry size"}
		}
		totalBytes += size
		if totalBytes > MaxTotalBytes {
			return &Error{Code: CodeTotalTooLarge, Path: p, Detail: "archive exceeds maximum total size"}
		}

		sum := sha256.Sum256(body)
		fileEntries = append(fileEntries, FileEntry{
			Path:   p,
			Size:   size,
			SHA256: hex.EncodeToString(sum[:]),
		})
	}

	comps := make([]CompetitionRef, len(meta.Competitions))
	copy(comps, meta.Competitions)
	sort.Slice(comps, func(i, j int) bool {
		return comps[i].ID < comps[j].ID
	})

	var restoreCreated *[]string
	if meta.Kind == KindBeforeRestore {
		rc := make([]string, len(meta.RestoreCreated))
		copy(rc, meta.RestoreCreated)
		sort.Strings(rc)
		restoreCreated = &rc
	}

	manifest := Manifest{
		Format:         FormatVersion,
		AppVersion:     version.GetVersion(),
		CreatedAt:      meta.CreatedAt.UTC(),
		Scope:          meta.Scope,
		Kind:           meta.Kind,
		Tournament:     meta.Tournament,
		Competitions:   comps,
		Files:          fileEntries,
		Fingerprint:    meta.Fingerprint,
		RestoreCreated: restoreCreated,
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestSize := int64(len(manifestBytes))
	if manifestSize > MaxFileBytes {
		return &Error{Code: CodeFileTooLarge, Path: ManifestName, Detail: "manifest exceeds maximum entry size"}
	}
	if totalBytes+manifestSize > MaxTotalBytes {
		return &Error{Code: CodeTotalTooLarge, Path: ManifestName, Detail: "archive exceeds maximum total size"}
	}

	methods := make([]uint16, len(paths))
	for i, p := range paths {
		m, err := chooseCompressionMethod(files[p])
		if err != nil {
			return err
		}
		methods[i] = m
	}
	manifestMethod, err := chooseCompressionMethod(manifestBytes)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	for i, p := range paths {
		if err := writeZipEntry(zw, p, files[p], methods[i]); err != nil {
			return err
		}
	}
	if err := writeZipEntry(zw, ManifestName, manifestBytes, manifestMethod); err != nil {
		return err
	}
	return zw.Close()
}

func chooseCompressionMethod(data []byte) (uint16, error) {
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return 0, err
	}
	if _, err := fw.Write(data); err != nil {
		return 0, err
	}
	if err := fw.Close(); err != nil {
		return 0, err
	}
	compressed := int64(buf.Len())
	uncompressed := int64(len(data))
	if compressed == 0 && uncompressed > 0 {
		return zip.Store, nil
	}
	if uncompressed > int64(MaxRatio)*compressed {
		return zip.Store, nil
	}
	return zip.Deflate, nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte, method uint16) error {
	hdr := &zip.FileHeader{
		Name:     name,
		Method:   method,
		Modified: fixedHeaderTime,
	}
	hdr.SetMode(0o644)

	ew, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err = ew.Write(data)
	return err
}
