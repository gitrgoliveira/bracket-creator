package backup

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rawEntry struct {
	name             string
	body             []byte
	method           uint16
	hasMethod        bool
	mode             fs.FileMode
	hasMode          bool
	raw              bool
	compressedSize   uint64
	uncompressedSize uint64
	crc              uint32
}

func buildZip(t *testing.T, entries ...rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, e := range entries {
		method := zip.Deflate
		if e.hasMethod {
			method = e.method
		}
		mode := fs.FileMode(0o644)
		if e.hasMode {
			mode = e.mode
		}

		hdr := &zip.FileHeader{
			Name:     e.name,
			Method:   method,
			Modified: fixedTime,
		}
		hdr.SetMode(mode)

		if e.raw {
			hdr.CompressedSize64 = e.compressedSize
			hdr.UncompressedSize64 = e.uncompressedSize
			hdr.CRC32 = e.crc
			w, err := zw.CreateRaw(hdr)
			require.NoError(t, err)
			_, err = w.Write(e.body)
			require.NoError(t, err)
			continue
		}

		w, err := zw.CreateHeader(hdr)
		require.NoError(t, err)
		if len(e.body) > 0 {
			_, err = w.Write(e.body)
			require.NoError(t, err)
		}
	}

	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func deflateRawBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = fw.Write(plain)
	require.NoError(t, err)
	require.NoError(t, fw.Close())
	return buf.Bytes()
}

func validManifest(scope Scope, kind Kind, comps []CompetitionRef, files map[string][]byte) Manifest {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	fileEntries := make([]FileEntry, 0, len(paths))
	for _, p := range paths {
		sum := sha256.Sum256(files[p])
		fileEntries = append(fileEntries, FileEntry{
			Path:   p,
			Size:   int64(len(files[p])),
			SHA256: hex.EncodeToString(sum[:]),
		})
	}

	compSlice := make([]CompetitionRef, len(comps))
	copy(compSlice, comps)

	var restoreCreated *[]string
	if kind == KindBeforeRestore {
		empty := []string{}
		restoreCreated = &empty
	}

	return Manifest{
		Format:         FormatVersion,
		AppVersion:     version.GetVersion(),
		CreatedAt:      time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		Scope:          scope,
		Kind:           kind,
		Tournament:     TournamentRef{Name: "Test Tournament"},
		Competitions:   compSlice,
		Files:          fileEntries,
		Fingerprint:    "fp-valid",
		RestoreCreated: restoreCreated,
	}
}

func entriesFromFilesAndManifest(t *testing.T, files map[string][]byte, m Manifest) []rawEntry {
	t.Helper()
	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	return entriesFromFilesAndRawManifest(files, manifestBytes)
}

func entriesFromFilesAndRawManifest(files map[string][]byte, manifestBytes []byte) []rawEntry {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	entries := make([]rawEntry, 0, len(paths)+1)
	for _, p := range paths {
		entries = append(entries, rawEntry{
			name: p,
			body: files[p],
		})
	}
	entries = append(entries, rawEntry{
		name: ManifestName,
		body: manifestBytes,
	})
	return entries
}

var validIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func goodChecks() Checks {
	return Checks{
		ValidID: func(id string) error {
			if id == "" || len(id) > 64 || !validIDPattern.MatchString(id) {
				return fmt.Errorf("invalid competition id %q", id)
			}
			return nil
		},
		FrontMatter: func(b []byte) error {
			s := string(b)
			if !strings.HasPrefix(s, "---\n") {
				return errors.New("missing opening front matter delimiter")
			}
			if !strings.Contains(s[4:], "\n---") {
				return errors.New("missing closing front matter delimiter")
			}
			return nil
		},
	}
}

func assertBackupErrorCode(t *testing.T, err error, expectedCode string) *Error {
	t.Helper()
	require.Error(t, err)
	var be *Error
	require.ErrorAs(t, err, &be, "expected *backup.Error, got %T: %v", err, err)
	assert.Equal(t, expectedCode, be.Code, "unexpected error code (detail: %s, path: %s)", be.Detail, be.Path)
	return be
}

func TestReadNilChecksReturnsPlainError(t *testing.T) {
	data := []byte("anything")
	_, err := Read(bytes.NewReader(data), int64(len(data)), Checks{
		ValidID:     nil,
		FrontMatter: goodChecks().FrontMatter,
	})
	require.Error(t, err)
	var be *Error
	assert.False(t, errors.As(err, &be))

	_, err = Read(bytes.NewReader(data), int64(len(data)), Checks{
		ValidID:     goodChecks().ValidID,
		FrontMatter: nil,
	})
	require.Error(t, err)
	assert.False(t, errors.As(err, &be))
}

func TestReadZipAndManifestValidation(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md":             []byte("---\nname: T\n---\n"),
		"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
	}
	baseComps := []CompetitionRef{{ID: "c1", Name: "C1"}}

	t.Run("not a zip", func(t *testing.T) {
		raw := []byte("definitely not a zip file")
		_, err := Read(bytes.NewReader(raw), int64(len(raw)), goodChecks())
		assertBackupErrorCode(t, err, CodeNotAZip)
	})

	t.Run("manifest missing", func(t *testing.T) {
		z := buildZip(t, rawEntry{
			name: "tournament.md",
			body: []byte("---\nname: T\n---\n"),
		})
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestMissing)
	})

	manifestCases := []struct {
		name         string
		manifestJSON string
		code         string
	}{
		{
			name:         "manifest not JSON",
			manifestJSON: "{not valid json",
			code:         CodeManifestInvalid,
		},
		{
			name:         "format 2 with unknown fields is format_too_new",
			manifestJSON: `{"format": 2, "futureField": "hello", "anotherNewThing": [1, 2, 3]}`,
			code:         CodeFormatTooNew,
		},
		{
			name:         "format 0 is manifest_invalid",
			manifestJSON: `{"format": 0}`,
			code:         CodeManifestInvalid,
		},
		{
			name:         "missing format field is manifest_invalid",
			manifestJSON: `{"appVersion": "1.0.0"}`,
			code:         CodeManifestInvalid,
		},
		{
			name: "unknown field at format 1",
			manifestJSON: `{
				"format": 1,
				"appVersion": "1.0.0",
				"createdAt": "2026-06-15T10:00:00Z",
				"scope": "tournament",
				"kind": "manual",
				"tournament": {"name": "T"},
				"competitions": [],
				"files": [],
				"fingerprint": "fp",
				"unknownKey": true
			}`,
			code: CodeManifestInvalid,
		},
		{
			name: "trailing data after JSON object",
			manifestJSON: `{
				"format": 1,
				"appVersion": "1.0.0",
				"createdAt": "2026-06-15T10:00:00Z",
				"scope": "tournament",
				"kind": "manual",
				"tournament": {"name": "T"},
				"competitions": [],
				"files": [],
				"fingerprint": "fp"
			} {"extra": 1}`,
			code: CodeManifestInvalid,
		},
		{
			name: "missing competitions key (nil slice)",
			manifestJSON: `{
				"format": 1,
				"appVersion": "1.0.0",
				"createdAt": "2026-06-15T10:00:00Z",
				"scope": "tournament",
				"kind": "manual",
				"tournament": {"name": "T"},
				"files": [],
				"fingerprint": "fp"
			}`,
			code: CodeManifestInvalid,
		},
		{
			name: "missing files key (nil slice)",
			manifestJSON: `{
				"format": 1,
				"appVersion": "1.0.0",
				"createdAt": "2026-06-15T10:00:00Z",
				"scope": "tournament",
				"kind": "manual",
				"tournament": {"name": "T"},
				"competitions": [],
				"fingerprint": "fp"
			}`,
			code: CodeManifestInvalid,
		},
	}

	for _, tc := range manifestCases {
		t.Run(tc.name, func(t *testing.T) {
			entries := entriesFromFilesAndRawManifest(baseFiles, []byte(tc.manifestJSON))
			z := buildZip(t, entries...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			assertBackupErrorCode(t, err, tc.code)
		})
	}

	t.Run("unknown scope in manifest", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.Scope = Scope("bogus")
		z := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)
	})

	t.Run("unknown kind in manifest", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.Kind = Kind("bogus")
		z := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)
	})

	t.Run("zero createdAt in manifest", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.CreatedAt = time.Time{}
		z := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)
	})

	t.Run("bad sha256 text in manifest", func(t *testing.T) {
		badHashes := []string{
			"short",
			strings.Repeat("A", 64), // uppercase not allowed
			strings.Repeat("g", 64), // non-hex char
		}
		for _, badHash := range badHashes {
			m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
			m.Files[0].SHA256 = badHash
			z := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m)...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			assertBackupErrorCode(t, err, CodeManifestInvalid)
		}
	})

	t.Run("invalid file entry fields in manifest", func(t *testing.T) {
		// Negative size
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.Files[0].Size = -1
		z := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)

		// Empty path in manifest files
		m2 := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m2.Files[0].Path = ""
		z2 := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m2)...)
		_, err = Read(bytes.NewReader(z2), int64(len(z2)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)

		// manifest.json listed in manifest files
		m3 := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m3.Files[0].Path = ManifestName
		z3 := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m3)...)
		_, err = Read(bytes.NewReader(z3), int64(len(z3)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)

		// Duplicate file entry in manifest
		m4 := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m4.Files = append(m4.Files, m4.Files[0])
		z4 := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m4)...)
		_, err = Read(bytes.NewReader(z4), int64(len(z4)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)

		// Duplicate competition IDs in manifest
		m5 := validManifest(ScopeTournament, KindManual, []CompetitionRef{
			{ID: "c1", Name: "C1"},
			{ID: "c1", Name: "C1 dup"},
		}, baseFiles)
		z5 := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m5)...)
		_, err = Read(bytes.NewReader(z5), int64(len(z5)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)

		// Duplicate restoreCreated IDs in manifest
		m6 := validManifest(ScopeTournament, KindBeforeRestore, baseComps, baseFiles)
		rcDup := []string{"c2", "c2"}
		m6.RestoreCreated = &rcDup
		z6 := buildZip(t, entriesFromFilesAndManifest(t, baseFiles, m6)...)
		_, err = Read(bytes.NewReader(z6), int64(len(z6)), goodChecks())
		assertBackupErrorCode(t, err, CodeManifestInvalid)
	})
}

func TestReadPathAndModeRefusals(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md": []byte("---\nname: T\n---\n"),
	}
	m := validManifest(ScopeTournament, KindManual, nil, baseFiles)

	unsafePaths := []string{
		"../evil",
		"competitions/c1/../../evil",
		"/abs",
		`a\b`,
		"a//b",
		"./a",
		"C:/x",
		"branding/a\x00b.png",
		"branding/a\x1fb.png",
		"branding/a\x7fb.png",
	}

	for _, p := range unsafePaths {
		t.Run("unsafe_path_"+p, func(t *testing.T) {
			entries := entriesFromFilesAndManifest(t, baseFiles, m)
			entries = append([]rawEntry{{name: p, body: []byte("x")}}, entries...)
			z := buildZip(t, entries...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			assertBackupErrorCode(t, err, CodeUnsafePath)
		})
	}

	t.Run("directory entry trailing slash", func(t *testing.T) {
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		entries = append([]rawEntry{{
			name:    "competitions/c1/",
			mode:    fs.ModeDir | 0o755,
			hasMode: true,
		}}, entries...)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeNotRegularFile)
	})

	t.Run("symlink entry", func(t *testing.T) {
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		entries = append([]rawEntry{{
			name:    "branding/link.png",
			body:    []byte("target"),
			mode:    fs.ModeSymlink | 0o777,
			hasMode: true,
		}}, entries...)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeNotRegularFile)
	})

	excludedPaths := []string{
		".wal/x",
		"competitions/c1/backups/x",
		"competitions/c1/.restore/x",
		"competitions/c1/pools.csv.tmp",
		"competitions/c1/pools.csv.tmp-123-456",
		"competitions/c1/.write-check-1",
	}
	for _, p := range excludedPaths {
		t.Run("excluded_"+p, func(t *testing.T) {
			files := map[string][]byte{
				"tournament.md":             []byte("---\nname: T\n---\n"),
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
				p:                           []byte("x"),
			}
			manifest := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
			z := buildZip(t, entriesFromFilesAndManifest(t, files, manifest)...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			assertBackupErrorCode(t, err, CodeOutsideLayout)
		})
	}
}

func TestIsExcludedPath(t *testing.T) {
	cases := map[string]bool{
		".wal/x":                                 true,
		"backups/20261103T120000Z-scheduled.zip": true,
		".restore/tok/journal.json":              true,
		"competitions/c1/backups/x":              true,
		"competitions/c1/.restore/x":             true,
		"competitions/c1/.wal/x":                 true,
		"competitions/backups/config.md":         false,
		"competitions/backups/history/x.jsonl":   false,
		"competitions/.wal/config.md":            false,
		"competitions/c1/history/x.jsonl":        false,
		"branding/logo.png":                      false,
		"tournament.md":                          false,
	}
	for p, want := range cases {
		assert.Equal(t, want, IsExcludedPath(p), p)
	}
}

// A competition whose id is a working-folder name ("backups") is an ordinary
// competition: its files are archive content, not an excluded directory.
func TestReadCompetitionIDNamedLikeAWorkingFolder(t *testing.T) {
	files := map[string][]byte{
		"tournament.md":                  []byte("---\nname: T\n---\n"),
		"competitions/backups/config.md": []byte("---\nid: backups\n---\n"),
	}
	manifest := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "backups", Name: "Backups"}}, files)
	z := buildZip(t, entriesFromFilesAndManifest(t, files, manifest)...)
	_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
	require.NoError(t, err)
}

func TestReadDuplicateAndCaseClash(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md":             []byte("---\nname: T\n---\n"),
		"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
	}
	m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, baseFiles)

	t.Run("duplicate file entry", func(t *testing.T) {
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		entries = append([]rawEntry{{
			name: "competitions/c1/config.md",
			body: []byte("---\nid: c1\n---\n"),
		}}, entries...)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeDuplicatePath)
	})

	t.Run("two manifest.json entries", func(t *testing.T) {
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		manifestBytes, err := json.Marshal(m)
		require.NoError(t, err)
		entries = append(entries, rawEntry{
			name: ManifestName,
			body: manifestBytes,
		})
		z := buildZip(t, entries...)
		_, err = Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeDuplicatePath)
	})

	t.Run("case clash between entries", func(t *testing.T) {
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		entries = append([]rawEntry{{
			name: "competitions/c1/Config.md",
			body: []byte("---\nid: c1\n---\n"),
		}}, entries...)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeCaseClash)
	})
}

func TestReadCapsAndIntegrity(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md": []byte("---\nname: T\n---\n"),
	}
	baseManifest := validManifest(ScopeTournament, KindManual, nil, baseFiles)
	manifestBytes, err := json.MarshalIndent(baseManifest, "", "  ")
	require.NoError(t, err)

	t.Run("more than MaxEntries is too_many_entries", func(t *testing.T) {
		entries := make([]rawEntry, 0, MaxEntries+1)
		for i := 0; i < MaxEntries; i++ {
			entries = append(entries, rawEntry{
				name:      fmt.Sprintf("branding/f-%d.txt", i),
				method:    zip.Store,
				hasMethod: true,
			})
		}
		entries = append(entries, rawEntry{
			name:      ManifestName,
			body:      manifestBytes,
			method:    zip.Store,
			hasMethod: true,
		})
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeTooManyEntries)
	})

	t.Run("header UncompressedSize64 over MaxFileBytes is file_too_large", func(t *testing.T) {
		plain := []byte("small")
		comp := deflateRawBytes(t, plain)
		entries := []rawEntry{
			{
				name:             "branding/huge.bin",
				body:             comp,
				method:           zip.Deflate,
				hasMethod:        true,
				raw:              true,
				compressedSize:   uint64(len(comp)),
				uncompressedSize: MaxFileBytes + 1,
				crc:              crc32.ChecksumIEEE(plain),
			},
			{
				name: ManifestName,
				body: manifestBytes,
			},
		}
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeFileTooLarge)
	})

	t.Run("headers sum over MaxTotalBytes is total_too_large", func(t *testing.T) {
		// 17 entries each claiming MaxFileBytes (32 MiB) = 544 MiB > 512 MiB,
		// with compressedSize set to 1 MiB so ratio (32:1) is well under 200:1.
		entries := make([]rawEntry, 0, 18)
		fakeBody := make([]byte, 1<<20)
		for i := 0; i < 17; i++ {
			entries = append(entries, rawEntry{
				name:             fmt.Sprintf("branding/part-%02d.bin", i),
				body:             fakeBody,
				method:           zip.Deflate,
				hasMethod:        true,
				raw:              true,
				compressedSize:   uint64(len(fakeBody)),
				uncompressedSize: MaxFileBytes,
				crc:              0,
			})
		}
		entries = append(entries, rawEntry{
			name: ManifestName,
			body: manifestBytes,
		})
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeTotalTooLarge)
	})

	t.Run("1 MiB zeros deflated by hand is ratio_too_high", func(t *testing.T) {
		zeros := make([]byte, 1<<20)
		files := map[string][]byte{
			"tournament.md":      []byte("---\nname: T\n---\n"),
			"branding/zeros.bin": zeros,
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		mBytes, err := json.MarshalIndent(m, "", "  ")
		require.NoError(t, err)

		entries := []rawEntry{
			{
				name:      "branding/zeros.bin",
				body:      zeros,
				method:    zip.Deflate,
				hasMethod: true,
			},
			{
				name: "tournament.md",
				body: files["tournament.md"],
			},
			{
				name: ManifestName,
				body: mBytes,
			},
		}
		z := buildZip(t, entries...)
		_, err = Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeRatioTooHigh)
	})

	t.Run("header that understates size is refused while reading", func(t *testing.T) {
		realData := bytes.Repeat([]byte("abcd1234"), 64) // 512 bytes
		comp := deflateRawBytes(t, realData)
		files := map[string][]byte{
			"tournament.md":    []byte("---\nname: T\n---\n"),
			"branding/lie.bin": realData[:16],
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		mBytes, err := json.MarshalIndent(m, "", "  ")
		require.NoError(t, err)

		entries := []rawEntry{
			{
				name:             "branding/lie.bin",
				body:             comp,
				method:           zip.Deflate,
				hasMethod:        true,
				raw:              true,
				compressedSize:   uint64(len(comp)),
				uncompressedSize: 16, // lies: stream actually decompresses to 512 bytes
				crc:              crc32.ChecksumIEEE(realData),
			},
			{
				name: "tournament.md",
				body: files["tournament.md"],
			},
			{
				name: ManifestName,
				body: mBytes,
			},
		}
		z := buildZip(t, entries...)
		arch, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		require.Nil(t, arch)
		require.Error(t, err)
		var be *Error
		require.ErrorAs(t, err, &be)
		assert.Contains(t, []string{CodeSizeMismatch, CodeFileTooLarge, CodeEntryUnreadable}, be.Code)
	})

	t.Run("bad CRC32 is entry_unreadable", func(t *testing.T) {
		body := []byte("---\nname: T\n---\n")
		comp := deflateRawBytes(t, body)
		entries := []rawEntry{
			{
				name:             "tournament.md",
				body:             comp,
				method:           zip.Deflate,
				hasMethod:        true,
				raw:              true,
				compressedSize:   uint64(len(comp)),
				uncompressedSize: uint64(len(body)),
				crc:              crc32.ChecksumIEEE(body) ^ 0xffffffff,
			},
			{
				name: ManifestName,
				body: manifestBytes,
			},
		}
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeEntryUnreadable)
	})
}

func TestReadManifestVsZipReconciliation(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md":             []byte("---\nname: T\n---\n"),
		"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
	}
	baseComps := []CompetitionRef{{ID: "c1", Name: "C1"}}

	t.Run("extra file in zip not in manifest is file_not_in_manifest", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		entries = append([]rawEntry{{
			name: "branding/extra.png",
			body: []byte("extra"),
		}}, entries...)
		z := buildZip(t, entries...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeFileNotInManifest)
		assert.Equal(t, "branding/extra.png", be.Path)
	})

	t.Run("file listed in manifest missing from zip is file_missing", func(t *testing.T) {
		filesWithExtra := map[string][]byte{
			"tournament.md":             baseFiles["tournament.md"],
			"competitions/c1/config.md": baseFiles["competitions/c1/config.md"],
			"branding/missing.png":      []byte("missing"),
		}
		m := validManifest(ScopeTournament, KindManual, baseComps, filesWithExtra)
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		z := buildZip(t, entries...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeFileMissing)
		assert.Equal(t, "branding/missing.png", be.Path)
	})

	t.Run("wrong size in manifest is size_mismatch", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.Files[0].Size += 5
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeSizeMismatch)
	})

	t.Run("wrong sha256 in manifest is checksum_mismatch", func(t *testing.T) {
		m := validManifest(ScopeTournament, KindManual, baseComps, baseFiles)
		m.Files[0].SHA256 = strings.Repeat("0", 64)
		entries := entriesFromFilesAndManifest(t, baseFiles, m)
		z := buildZip(t, entries...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeChecksumMismatch)
	})
}

func TestReadLayoutAndCompetitionRules(t *testing.T) {
	t.Run("tournament archive without tournament.md is tournament_missing", func(t *testing.T) {
		files := map[string][]byte{
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeTournamentMissing)
	})

	t.Run("competition archive holding tournament.md is outside_layout", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":             []byte("---\nname: T\n---\n"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		}
		m := validManifest(ScopeCompetition, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeOutsideLayout)
	})

	t.Run("competition archive holding branding/x is outside_layout", func(t *testing.T) {
		files := map[string][]byte{
			"branding/x.png":            []byte("png"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		}
		m := validManifest(ScopeCompetition, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeOutsideLayout)
	})

	t.Run("competition-scope manual archive with zero competitions is competition_count", func(t *testing.T) {
		files := map[string][]byte{}
		m := validManifest(ScopeCompetition, KindManual, nil, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeCompetitionCount)
	})

	t.Run("competition archive with two competitions is competition_count", func(t *testing.T) {
		files := map[string][]byte{
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
		}
		comps := []CompetitionRef{{ID: "c1", Name: "C1"}, {ID: "c2", Name: "C2"}}
		m := validManifest(ScopeCompetition, KindManual, comps, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeCompetitionCount)
	})

	t.Run("before-restore competition archive with two competitions is competition_count", func(t *testing.T) {
		files := map[string][]byte{
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
		}
		comps := []CompetitionRef{{ID: "c1", Name: "C1"}, {ID: "c2", Name: "C2"}}
		m := validManifest(ScopeCompetition, KindBeforeRestore, comps, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeCompetitionCount)
	})

	t.Run("before-restore competition archive with zero competitions and only manifest.json is accepted", func(t *testing.T) {
		files := map[string][]byte{}
		m := validManifest(ScopeCompetition, KindBeforeRestore, nil, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		arch, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		require.NoError(t, err)
		assert.Empty(t, arch.Files)
		assert.Empty(t, arch.Manifest.Competitions)
	})

	t.Run("tournament archive with zero competitions is accepted", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md": []byte("---\nname: T\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		arch, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		require.NoError(t, err)
		assert.Len(t, arch.Files, 1)
		assert.Empty(t, arch.Manifest.Competitions)
	})

	t.Run("folder in zip not in manifest is competition_mismatch", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":             []byte("---\nname: T\n---\n"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeCompetitionMismatch)
		assert.Equal(t, "c2", be.Path)
	})

	t.Run("manifest competition with no folder is competition_mismatch", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":             []byte("---\nname: T\n---\n"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		}
		comps := []CompetitionRef{{ID: "c1", Name: "C1"}, {ID: "c2", Name: "C2"}}
		m := validManifest(ScopeTournament, KindManual, comps, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeCompetitionMismatch)
		assert.Equal(t, "c2", be.Path)
	})

	t.Run("competition folder without config.md is config_missing", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":                    []byte("---\nname: T\n---\n"),
			"competitions/c1/participants.csv": []byte("id,name,dojo\n"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeConfigMissing)
	})

	t.Run("competitions/c1 as a file is outside_layout", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":   []byte("---\nname: T\n---\n"),
			"competitions/c1": []byte("file instead of folder"),
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeOutsideLayout)
	})

	t.Run("other/x in tournament archive is outside_layout", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md": []byte("---\nname: T\n---\n"),
			"other/x":       []byte("hello"),
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeOutsideLayout)
	})
}

func TestReadCallerChecks(t *testing.T) {
	t.Run("invalid competition id in manifest is bad_competition_id", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":              []byte("---\nname: T\n---\n"),
			"competitions/c 1/config.md": []byte("---\nid: c 1\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c 1", Name: "Bad ID"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeBadCompetitionID)
		assert.Equal(t, "c 1", be.Path)
	})

	t.Run("invalid restoreCreated id is bad_competition_id", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md": []byte("---\nname: T\n---\n"),
		}
		m := validManifest(ScopeTournament, KindBeforeRestore, nil, files)
		badRC := []string{"../x"}
		m.RestoreCreated = &badRC
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeBadCompetitionID)
		assert.Equal(t, "../x", be.Path)
	})

	t.Run("restoreCreated present on manual archive is restore_created_not_allowed", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md": []byte("---\nname: T\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, nil, files)
		emptyRC := []string{}
		m.RestoreCreated = &emptyRC
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
		assertBackupErrorCode(t, err, CodeRestoreCreatedNotAllowed)
	})

	t.Run("FrontMatter refusing config.md is front_matter_invalid with path", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":             []byte("---\nname: T\n---\n"),
			"competitions/c1/config.md": []byte("not front matter"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeFrontMatterInvalid)
		assert.Equal(t, "competitions/c1/config.md", be.Path)
	})

	t.Run("FrontMatter refusing tournament.md is front_matter_invalid with path", func(t *testing.T) {
		files := map[string][]byte{
			"tournament.md":             []byte("not front matter"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		}
		m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
		z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
		be := assertBackupErrorCode(t, func() error {
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			return err
		}(), CodeFrontMatterInvalid)
		assert.Equal(t, "tournament.md", be.Path)
	})
}

func TestReadRejectTempAndWriteCheckPaths(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md":             []byte("---\nname: T\n---\n"),
		"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
	}
	m := validManifest(ScopeTournament, KindManual, []CompetitionRef{{ID: "c1", Name: "C1"}}, baseFiles)

	for _, p := range []string{
		"competitions/c1/pools.csv.tmp-123-456",
		"competitions/c1/.write-check-1",
	} {
		t.Run("reject_"+p, func(t *testing.T) {
			entries := entriesFromFilesAndManifest(t, baseFiles, m)
			entries = append([]rawEntry{{name: p, body: []byte("temp")}}, entries...)
			z := buildZip(t, entries...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			be := assertBackupErrorCode(t, err, CodeOutsideLayout)
			assert.Equal(t, p, be.Path)
		})
	}
}

func TestReadRejectControlCharacterPaths(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md": []byte("---\nname: T\n---\n"),
	}
	m := validManifest(ScopeTournament, KindManual, nil, baseFiles)

	for _, p := range []string{
		"branding/a\x00b.png",
		"branding/a\x1fb.png",
		"branding/a\x7fb.png",
	} {
		t.Run("reject_control_"+p, func(t *testing.T) {
			entries := entriesFromFilesAndManifest(t, baseFiles, m)
			entries = append([]rawEntry{{name: p, body: []byte("x")}}, entries...)
			z := buildZip(t, entries...)
			_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
			be := assertBackupErrorCode(t, err, CodeUnsafePath)
			assert.Equal(t, p, be.Path)
		})
	}
}

func TestReadManifestOverlapValidation(t *testing.T) {
	files := map[string][]byte{
		"tournament.md":             []byte("---\nname: T\n---\n"),
		"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
	}
	m := validManifest(ScopeTournament, KindBeforeRestore, []CompetitionRef{{ID: "c1", Name: "C1"}}, files)
	rc := []string{"c1"}
	m.RestoreCreated = &rc
	z := buildZip(t, entriesFromFilesAndManifest(t, files, m)...)
	_, err := Read(bytes.NewReader(z), int64(len(z)), goodChecks())
	be := assertBackupErrorCode(t, err, CodeManifestInvalid)
	assert.Equal(t, "c1", be.Path)
}
