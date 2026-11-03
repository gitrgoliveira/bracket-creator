package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDeterminism(t *testing.T) {
	created := time.Date(2026, 5, 10, 14, 30, 0, 0, time.FixedZone("CEST", 2*3600))

	metaA := Meta{
		CreatedAt:  created,
		Scope:      ScopeTournament,
		Kind:       KindBeforeRestore,
		Tournament: TournamentRef{Name: "Spring Taikai"},
		Competitions: []CompetitionRef{
			{ID: "seniors", Name: "Seniors"},
			{ID: "juniors", Name: "Juniors"},
		},
		Fingerprint:    "fp-123",
		RestoreCreated: []string{"comp-b", "comp-a"},
	}
	filesA := map[string][]byte{
		"tournament.md":                     []byte("---\nname: Spring Taikai\n---\n"),
		"branding/logo.png":                 []byte("logo-bytes"),
		"sponsors/s1.png":                   []byte("sponsor-bytes"),
		"competitions/seniors/config.md":    []byte("---\nid: seniors\nname: Seniors\n---\n"),
		"competitions/juniors/config.md":    []byte("---\nid: juniors\nname: Juniors\n---\n"),
		"competitions/juniors/pools.csv":    []byte("pool,player\n"),
		"competitions/seniors/bracket.json": []byte("{}"),
	}

	var buf1, buf2 bytes.Buffer
	require.NoError(t, Write(&buf1, metaA, filesA))
	require.NoError(t, Write(&buf2, metaA, filesA))
	require.Equal(t, buf1.Bytes(), buf2.Bytes())

	// Build a second map and slices in a different order.
	filesB := make(map[string][]byte, len(filesA))
	keys := []string{
		"competitions/seniors/bracket.json",
		"competitions/juniors/pools.csv",
		"competitions/juniors/config.md",
		"competitions/seniors/config.md",
		"sponsors/s1.png",
		"branding/logo.png",
		"tournament.md",
	}
	for _, k := range keys {
		filesB[k] = filesA[k]
	}
	metaB := Meta{
		CreatedAt:  created.UTC(),
		Scope:      ScopeTournament,
		Kind:       KindBeforeRestore,
		Tournament: TournamentRef{Name: "Spring Taikai"},
		Competitions: []CompetitionRef{
			{ID: "juniors", Name: "Juniors"},
			{ID: "seniors", Name: "Seniors"},
		},
		Fingerprint:    "fp-123",
		RestoreCreated: []string{"comp-a", "comp-b"},
	}

	var buf3 bytes.Buffer
	require.NoError(t, Write(&buf3, metaB, filesB))
	require.Equal(t, buf1.Bytes(), buf3.Bytes())
}

func TestWriteLayoutAndHeaders(t *testing.T) {
	meta := Meta{
		CreatedAt:  time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		Scope:      ScopeTournament,
		Kind:       KindManual,
		Tournament: TournamentRef{Name: "Test Open"},
		Competitions: []CompetitionRef{
			{ID: "c2", Name: "Comp 2"},
			{ID: "c1", Name: "Comp 1"},
		},
		Fingerprint: "abc",
	}
	files := map[string][]byte{
		"tournament.md":                  []byte("---\nname: Test Open\n---\n"),
		"sponsors/b.png":                 []byte("b"),
		"branding/a.png":                 []byte("a"),
		"competitions/c2/config.md":      []byte("---\nid: c2\n---\n"),
		"competitions/c1/config.md":      []byte("---\nid: c1\n---\n"),
		"competitions/c1/overrides.json": []byte("{}"),
	}

	var buf bytes.Buffer
	require.NoError(t, Write(&buf, meta, files))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	expectedOrder := []string{
		"branding/a.png",
		"competitions/c1/config.md",
		"competitions/c1/overrides.json",
		"competitions/c2/config.md",
		"sponsors/b.png",
		"tournament.md",
		ManifestName,
	}
	actualOrder := make([]string, 0, len(zr.File))
	expectedModTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, zf := range zr.File {
		actualOrder = append(actualOrder, zf.Name)
		assert.Equal(t, expectedModTime, zf.Modified.UTC(), "entry %s modified time", zf.Name)
		assert.Equal(t, fs.FileMode(0o644), zf.Mode(), "entry %s mode", zf.Name)
	}
	assert.Equal(t, expectedOrder, actualOrder)
}

func TestWriteManifestFields(t *testing.T) {
	nonUTC := time.Date(2026, 7, 15, 18, 45, 12, 0, time.FixedZone("JST", 9*3600))
	body1 := []byte("---\nname: Kendo Cup\n---\n")
	body2 := []byte("---\nid: kyu\nname: Kyu\n---\n")

	meta := Meta{
		CreatedAt:  nonUTC,
		Scope:      ScopeTournament,
		Kind:       KindScheduled,
		Tournament: TournamentRef{Name: "Kendo Cup"},
		Competitions: []CompetitionRef{
			{ID: "dan", Name: "Dan"},
			{ID: "kyu", Name: "Kyu"},
		},
		Fingerprint: "fp-xyz",
	}
	files := map[string][]byte{
		"tournament.md":              body1,
		"competitions/kyu/config.md": body2,
		"competitions/dan/config.md": body2,
	}

	var buf bytes.Buffer
	require.NoError(t, Write(&buf, meta, files))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	manifestFile := zr.File[len(zr.File)-1]
	require.Equal(t, ManifestName, manifestFile.Name)

	rc, err := manifestFile.Open()
	require.NoError(t, err)
	rawBytes, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	var m Manifest
	require.NoError(t, json.Unmarshal(rawBytes, &m))

	assert.Equal(t, FormatVersion, m.Format)
	assert.Equal(t, version.GetVersion(), m.AppVersion)
	assert.Equal(t, nonUTC.UTC(), m.CreatedAt)
	assert.Equal(t, time.UTC, m.CreatedAt.Location())
	assert.Equal(t, ScopeTournament, m.Scope)
	assert.Equal(t, KindScheduled, m.Kind)
	assert.Equal(t, "Kendo Cup", m.Tournament.Name)
	assert.Equal(t, "fp-xyz", m.Fingerprint)
	assert.Nil(t, m.RestoreCreated)
	assert.NotContains(t, string(rawBytes), "restoreCreated")

	require.Equal(t, []CompetitionRef{
		{ID: "dan", Name: "Dan"},
		{ID: "kyu", Name: "Kyu"},
	}, m.Competitions)

	sum1 := sha256.Sum256(body1)
	sum2 := sha256.Sum256(body2)
	require.Equal(t, []FileEntry{
		{Path: "competitions/dan/config.md", Size: int64(len(body2)), SHA256: hex.EncodeToString(sum2[:])},
		{Path: "competitions/kyu/config.md", Size: int64(len(body2)), SHA256: hex.EncodeToString(sum2[:])},
		{Path: "tournament.md", Size: int64(len(body1)), SHA256: hex.EncodeToString(sum1[:])},
	}, m.Files)
}

func TestWriteRestoreCreatedHandling(t *testing.T) {
	baseFiles := map[string][]byte{
		"tournament.md": []byte("---\nname: T\n---\n"),
	}

	t.Run("before-restore with nil slice emits empty array in JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:      time.Now(),
			Scope:          ScopeTournament,
			Kind:           KindBeforeRestore,
			Tournament:     TournamentRef{Name: "T"},
			RestoreCreated: nil,
		}, baseFiles)
		require.NoError(t, err)

		rawManifest := readRawManifestFromZip(t, buf.Bytes())
		var rawMap map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rawManifest, &rawMap))
		rcRaw, ok := rawMap["restoreCreated"]
		require.True(t, ok, "restoreCreated key must be present")
		assert.Equal(t, "[]", strings.TrimSpace(string(rcRaw)))
		assert.Equal(t, "[]", strings.TrimSpace(string(rawMap["competitions"])))
	})

	t.Run("before-restore with ids sorts them", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:      time.Now(),
			Scope:          ScopeTournament,
			Kind:           KindBeforeRestore,
			Tournament:     TournamentRef{Name: "T"},
			RestoreCreated: []string{"z-comp", "a-comp", "m-comp"},
		}, baseFiles)
		require.NoError(t, err)

		rawManifest := readRawManifestFromZip(t, buf.Bytes())
		var m Manifest
		require.NoError(t, json.Unmarshal(rawManifest, &m))
		require.NotNil(t, m.RestoreCreated)
		assert.Equal(t, []string{"a-comp", "m-comp", "z-comp"}, *m.RestoreCreated)
	})

	t.Run("manual has no restoreCreated key", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:  time.Now(),
			Scope:      ScopeTournament,
			Kind:       KindManual,
			Tournament: TournamentRef{Name: "T"},
		}, baseFiles)
		require.NoError(t, err)

		rawManifest := readRawManifestFromZip(t, buf.Bytes())
		var rawMap map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rawManifest, &rawMap))
		_, ok := rawMap["restoreCreated"]
		assert.False(t, ok)
	})

	t.Run("manual with restoreCreated ids is refused", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:      time.Now(),
			Scope:          ScopeTournament,
			Kind:           KindManual,
			Tournament:     TournamentRef{Name: "T"},
			RestoreCreated: []string{"c1"},
		}, baseFiles)
		assertBackupErrorCode(t, err, CodeRestoreCreatedNotAllowed)
		assert.Zero(t, buf.Len())
	})
}

func TestWriteCompressionMethodSelection(t *testing.T) {
	zeros := make([]byte, 1<<20) // 1 MiB of zeros, compresses > 200:1 with deflate
	ordinary := []byte("---\nname: Ordinary Competition Config\nid: c1\nformat: mixed\n---\n")

	meta := Meta{
		CreatedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Scope:        ScopeTournament,
		Kind:         KindManual,
		Tournament:   TournamentRef{Name: "Cup"},
		Competitions: []CompetitionRef{{ID: "c1", Name: "C1"}},
	}
	files := map[string][]byte{
		"tournament.md":             []byte("---\nname: Cup\n---\n"),
		"competitions/c1/config.md": ordinary,
		"branding/compressible.bin": zeros,
	}

	var buf bytes.Buffer
	require.NoError(t, Write(&buf, meta, files))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	methods := make(map[string]uint16, len(zr.File))
	for _, zf := range zr.File {
		methods[zf.Name] = zf.Method
	}
	assert.Equal(t, zip.Store, methods["branding/compressible.bin"])
	assert.Equal(t, zip.Deflate, methods["competitions/c1/config.md"])

	// Verify the written archive passes Read.
	arch, err := Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()), goodChecks())
	require.NoError(t, err)
	assert.Equal(t, zeros, arch.Files["branding/compressible.bin"])
}

func TestWriteRefusals(t *testing.T) {
	validMeta := Meta{
		CreatedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Scope:        ScopeTournament,
		Kind:         KindManual,
		Tournament:   TournamentRef{Name: "Cup"},
		Competitions: []CompetitionRef{{ID: "c1", Name: "C1"}},
	}

	cases := []struct {
		name  string
		meta  Meta
		files map[string][]byte
		code  string
	}{
		{
			name:  "relative parent traversal",
			meta:  validMeta,
			files: map[string][]byte{"../x": []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "absolute path",
			meta:  validMeta,
			files: map[string][]byte{"/abs": []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "backslash path",
			meta:  validMeta,
			files: map[string][]byte{`a\b`: []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "windows drive path",
			meta:  validMeta,
			files: map[string][]byte{"C:/x": []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "empty segment",
			meta:  validMeta,
			files: map[string][]byte{"branding//x.png": []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "nul byte in path",
			meta:  validMeta,
			files: map[string][]byte{"branding/a\x00b.png": []byte("x")},
			code:  CodeUnsafePath,
		},
		{
			name:  "excluded wal path",
			meta:  validMeta,
			files: map[string][]byte{".wal/x": []byte("x")},
			code:  CodeOutsideLayout,
		},
		{
			name:  "excluded tmp suffix",
			meta:  validMeta,
			files: map[string][]byte{"x.tmp": []byte("x")},
			code:  CodeOutsideLayout,
		},
		{
			name:  "excluded competition tmp suffix",
			meta:  validMeta,
			files: map[string][]byte{"competitions/c1/pools.csv.tmp": []byte("x")},
			code:  CodeOutsideLayout,
		},
		{
			name:  "manifest.json as file key",
			meta:  validMeta,
			files: map[string][]byte{"manifest.json": []byte("{}")},
			code:  CodeOutsideLayout,
		},
		{
			name:  "outside tournament layout",
			meta:  validMeta,
			files: map[string][]byte{"other/file.txt": []byte("x")},
			code:  CodeOutsideLayout,
		},
		{
			name: "tournament.md in competition scope",
			meta: Meta{
				CreatedAt: validMeta.CreatedAt,
				Scope:     ScopeCompetition,
				Kind:      KindManual,
			},
			files: map[string][]byte{"tournament.md": []byte("---\n---\n")},
			code:  CodeOutsideLayout,
		},
		{
			name: "unknown scope",
			meta: Meta{
				CreatedAt: validMeta.CreatedAt,
				Scope:     Scope("invalid"),
				Kind:      KindManual,
			},
			files: map[string][]byte{"tournament.md": []byte("---\n---\n")},
			code:  CodeManifestInvalid,
		},
		{
			name: "unknown kind",
			meta: Meta{
				CreatedAt: validMeta.CreatedAt,
				Scope:     ScopeTournament,
				Kind:      Kind("invalid"),
			},
			files: map[string][]byte{"tournament.md": []byte("---\n---\n")},
			code:  CodeManifestInvalid,
		},
		{
			name: "zero creation time",
			meta: Meta{
				Scope: ScopeTournament,
				Kind:  KindManual,
			},
			files: map[string][]byte{"tournament.md": []byte("---\n---\n")},
			code:  CodeManifestInvalid,
		},
		{
			name: "case clash",
			meta: validMeta,
			files: map[string][]byte{
				"tournament.md":             []byte("---\nname: Cup\n---\n"),
				"competitions/c1/config.md": []byte("a"),
				"competitions/c1/Config.md": []byte("b"),
			},
			code: CodeCaseClash,
		},
		{
			name: "tournament archive without tournament.md",
			meta: validMeta,
			files: map[string][]byte{
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			},
			code: CodeTournamentMissing,
		},
		{
			name: "competition folder not in meta.Competitions",
			meta: validMeta,
			files: map[string][]byte{
				"tournament.md":             []byte("---\nname: Cup\n---\n"),
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
				"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
			},
			code: CodeCompetitionMismatch,
		},
		{
			name: "meta.Competitions entry with no folder in files",
			meta: Meta{
				CreatedAt:  validMeta.CreatedAt,
				Scope:      ScopeTournament,
				Kind:       KindManual,
				Tournament: TournamentRef{Name: "Cup"},
				Competitions: []CompetitionRef{
					{ID: "c1", Name: "C1"},
					{ID: "c2", Name: "C2"},
				},
			},
			files: map[string][]byte{
				"tournament.md":             []byte("---\nname: Cup\n---\n"),
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			},
			code: CodeCompetitionMismatch,
		},
		{
			name: "competition folder without config.md",
			meta: validMeta,
			files: map[string][]byte{
				"tournament.md":                    []byte("---\nname: Cup\n---\n"),
				"competitions/c1/participants.csv": []byte("ID,Name,Dojo\n"),
			},
			code: CodeConfigMissing,
		},
		{
			name: "competition archive with zero competitions when manual",
			meta: Meta{
				CreatedAt:  validMeta.CreatedAt,
				Scope:      ScopeCompetition,
				Kind:       KindManual,
				Tournament: TournamentRef{Name: "Cup"},
			},
			files: nil,
			code:  CodeCompetitionCount,
		},
		{
			name: "competition archive with two competitions",
			meta: Meta{
				CreatedAt:  validMeta.CreatedAt,
				Scope:      ScopeCompetition,
				Kind:       KindManual,
				Tournament: TournamentRef{Name: "Cup"},
				Competitions: []CompetitionRef{
					{ID: "c1", Name: "C1"},
					{ID: "c2", Name: "C2"},
				},
			},
			files: map[string][]byte{
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
				"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
			},
			code: CodeCompetitionCount,
		},
		{
			name: "duplicate competition id in meta.Competitions",
			meta: Meta{
				CreatedAt:  validMeta.CreatedAt,
				Scope:      ScopeTournament,
				Kind:       KindManual,
				Tournament: TournamentRef{Name: "Cup"},
				Competitions: []CompetitionRef{
					{ID: "c1", Name: "C1"},
					{ID: "c1", Name: "C1 dup"},
				},
			},
			files: map[string][]byte{
				"tournament.md":             []byte("---\nname: Cup\n---\n"),
				"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			},
			code: CodeManifestInvalid,
		},
		{
			name: "duplicate id in meta.RestoreCreated",
			meta: Meta{
				CreatedAt:      validMeta.CreatedAt,
				Scope:          ScopeTournament,
				Kind:           KindBeforeRestore,
				Tournament:     TournamentRef{Name: "Cup"},
				RestoreCreated: []string{"c2", "c2"},
			},
			files: map[string][]byte{
				"tournament.md": []byte("---\nname: Cup\n---\n"),
			},
			code: CodeManifestInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Write(&buf, tc.meta, tc.files)
			assertBackupErrorCode(t, err, tc.code)
			assert.Zero(t, buf.Len(), "Write must not write bytes when validation fails")
		})
	}

	t.Run("file over MaxFileBytes", func(t *testing.T) {
		var buf bytes.Buffer
		oversized := make([]byte, MaxFileBytes+1)
		err := Write(&buf, validMeta, map[string][]byte{
			"tournament.md":             []byte("---\nname: Cup\n---\n"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			"branding/huge.bin":         oversized,
		})
		assertBackupErrorCode(t, err, CodeFileTooLarge)
		assert.Zero(t, buf.Len())
	})
}

func TestWriteLayoutAndCompetitionRefusals(t *testing.T) {
	created := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	t.Run("tournament archive without tournament.md is tournament_missing", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:    created,
			Scope:        ScopeTournament,
			Kind:         KindManual,
			Tournament:   TournamentRef{Name: "Cup"},
			Competitions: []CompetitionRef{{ID: "c1", Name: "C1"}},
		}, map[string][]byte{
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
		})
		be := assertBackupErrorCode(t, err, CodeTournamentMissing)
		assert.Equal(t, "tournament.md", be.Path)
		assert.Zero(t, buf.Len())
	})

	t.Run("meta.Competitions mismatch with competition folders is competition_mismatch", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:    created,
			Scope:        ScopeTournament,
			Kind:         KindManual,
			Tournament:   TournamentRef{Name: "Cup"},
			Competitions: []CompetitionRef{{ID: "c1", Name: "C1"}},
		}, map[string][]byte{
			"tournament.md":             []byte("---\nname: Cup\n---\n"),
			"competitions/c1/config.md": []byte("---\nid: c1\n---\n"),
			"competitions/c2/config.md": []byte("---\nid: c2\n---\n"),
		})
		be := assertBackupErrorCode(t, err, CodeCompetitionMismatch)
		assert.Equal(t, "c2", be.Path)
		assert.Zero(t, buf.Len())
	})

	t.Run("competition without config.md is config_missing", func(t *testing.T) {
		var buf bytes.Buffer
		err := Write(&buf, Meta{
			CreatedAt:    created,
			Scope:        ScopeTournament,
			Kind:         KindManual,
			Tournament:   TournamentRef{Name: "Cup"},
			Competitions: []CompetitionRef{{ID: "c1", Name: "C1"}},
		}, map[string][]byte{
			"tournament.md":                    []byte("---\nname: Cup\n---\n"),
			"competitions/c1/participants.csv": []byte("ID,Name,Dojo\n"),
		})
		be := assertBackupErrorCode(t, err, CodeConfigMissing)
		assert.Equal(t, "competitions/c1/config.md", be.Path)
		assert.Zero(t, buf.Len())
	})
}

func TestWriteAndReadRoundTrip(t *testing.T) {
	t.Run("tournament archive with two competitions", func(t *testing.T) {
		created := time.Date(2026, 9, 1, 10, 15, 0, 0, time.UTC)
		meta := Meta{
			CreatedAt:  created,
			Scope:      ScopeTournament,
			Kind:       KindManual,
			Tournament: TournamentRef{Name: "Autumn Taikai"},
			Competitions: []CompetitionRef{
				{ID: "c2", Name: "Team Match"},
				{ID: "c1", Name: "Individual"},
			},
			Fingerprint: "fp-roundtrip",
		}
		files := map[string][]byte{
			"tournament.md":                    []byte("---\nname: Autumn Taikai\n---\n"),
			"branding/logo.png":                []byte("\x89PNG-logo"),
			"sponsors/a.png":                   []byte("\x89PNG-sponsor"),
			"competitions/c1/config.md":        []byte("---\nid: c1\nname: Individual\n---\n"),
			"competitions/c1/participants.csv": []byte("id,name,dojo\n1,Alice,Dojo A\n"),
			"competitions/c1/history/m1.jsonl": []byte("{\"seq\":1}\n"),
			"competitions/c1/events/e.jsonl":   []byte("{\"type\":\"start\"}\n"),
			"competitions/c2/config.md":        []byte("---\nid: c2\nname: Team Match\n---\n"),
			"competitions/c2/participants.csv": []byte("id,name,dojo\n2,Bob,Dojo B\n"),
			"competitions/c2/history/m1.jsonl": []byte("{\"seq\":1}\n"),
			"competitions/c2/events/e.jsonl":   []byte("{\"type\":\"start\"}\n"),
		}

		var buf bytes.Buffer
		require.NoError(t, Write(&buf, meta, files))

		arch, err := Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()), goodChecks())
		require.NoError(t, err)

		assert.Equal(t, files, arch.Files)
		assert.NotContains(t, arch.Files, ManifestName)
		assert.Equal(t, FormatVersion, arch.Manifest.Format)
		assert.Equal(t, version.GetVersion(), arch.Manifest.AppVersion)
		assert.Equal(t, created, arch.Manifest.CreatedAt)
		assert.Equal(t, ScopeTournament, arch.Manifest.Scope)
		assert.Equal(t, KindManual, arch.Manifest.Kind)
		assert.Equal(t, TournamentRef{Name: "Autumn Taikai"}, arch.Manifest.Tournament)
		assert.Equal(t, []CompetitionRef{
			{ID: "c1", Name: "Individual"},
			{ID: "c2", Name: "Team Match"},
		}, arch.Manifest.Competitions)
		assert.Equal(t, "fp-roundtrip", arch.Manifest.Fingerprint)
		assert.Nil(t, arch.Manifest.RestoreCreated)
	})

	t.Run("before-restore competition archive with no competitions", func(t *testing.T) {
		created := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
		meta := Meta{
			CreatedAt:      created,
			Scope:          ScopeCompetition,
			Kind:           KindBeforeRestore,
			Tournament:     TournamentRef{Name: "Autumn Taikai"},
			Competitions:   nil,
			Fingerprint:    "fp-empty-safety",
			RestoreCreated: []string{"c-new"},
		}

		var buf bytes.Buffer
		require.NoError(t, Write(&buf, meta, nil))

		arch, err := Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()), goodChecks())
		require.NoError(t, err)

		assert.Empty(t, arch.Files)
		assert.Empty(t, arch.Manifest.Competitions)
		require.NotNil(t, arch.Manifest.RestoreCreated)
		assert.Equal(t, []string{"c-new"}, *arch.Manifest.RestoreCreated)
	})
}

func TestErrorStringFormatting(t *testing.T) {
	e1 := &Error{Code: CodeUnsafePath, Path: "../x", Detail: "traversal not allowed"}
	assert.Contains(t, e1.Error(), CodeUnsafePath)
	assert.Contains(t, e1.Error(), "../x")
	assert.Contains(t, e1.Error(), "traversal not allowed")

	e2 := &Error{Code: CodeNotAZip, Detail: "bad magic"}
	assert.Contains(t, e2.Error(), CodeNotAZip)
	assert.Contains(t, e2.Error(), "bad magic")
}

func readRawManifestFromZip(t *testing.T, zipBytes []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	for _, zf := range zr.File {
		if zf.Name == ManifestName {
			rc, err := zf.Open()
			require.NoError(t, err)
			data, err := io.ReadAll(rc)
			require.NoError(t, err)
			require.NoError(t, rc.Close())
			return data
		}
	}
	t.Fatalf("manifest.json not found in zip")
	return nil
}
