package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"gopkg.in/yaml.v3"
)

// StampedFile names a competition file whose bytes move on a write that
// changes nothing, and the stamp fields Fingerprint zeroes in it. Every entry
// was measured through the real write doors (the mobileapp test
// backup_fingerprint_doors_test.go); a new stamp field joins its Fields.
type StampedFile struct {
	// Name is the file's name inside competitions/<id>/.
	Name string
	// Fields are the Go field names zeroed at every depth of the parsed file.
	Fields []string

	parse func(compID string, raw []byte) (any, error)
	// trim drops or rewrites the parsed file's stamp-only content in place
	// before Fields are zeroed, and answers whether anything is left.
	trim   func(v any) (keep bool)
	encode func(v any) ([]byte, error)
}

// FingerprintStampedFiles is the one list of stamp fields the fingerprint
// zeroes. Each file is read through the store's own parser and written back
// in the store's own format, so only the named fields are dropped.
var FingerprintStampedFiles = []StampedFile{
	{
		// A decision re-sent at a new stamp moves all three; a rescore that
		// changes nothing moves none of them.
		Name:   "pool-matches.csv",
		Fields: []string{"ModifiedAt", "GroupStamps", "ScoreVersion"},
		parse: func(_ string, raw []byte) (any, error) {
			results, err := parsePoolMatchesBytes(raw)
			return &results, err
		},
		encode: func(v any) ([]byte, error) { return encodePoolMatchesCSV(*v.(*[]MatchResult)) },
	},
	{
		Name:   "bracket.json",
		Fields: []string{"ModifiedAt", "GroupStamps", "ScoreVersion"},
		parse:  func(_ string, raw []byte) (any, error) { return parseBracketBytes(raw) },
		encode: func(v any) ([]byte, error) { return json.MarshalIndent(v.(*Bracket), "", "  ") },
	},
	{
		// The same status re-sent is recorded again at the server's time.
		Name:   competitorStatusFilename,
		Fields: []string{"RecordedAt"},
		parse: func(_ string, raw []byte) (any, error) {
			statuses, err := parseCompetitorStatusBytes(raw)
			return &statuses, err
		},
		encode: func(v any) ([]byte, error) {
			statuses := *v.(*map[string]domain.CompetitorStatus)
			file := competitorStatusFile{Statuses: make([]domain.CompetitorStatus, 0, len(statuses))}
			for _, k := range fingerprintSortedKeys(statuses) {
				file.Statuses = append(file.Statuses, statuses[k])
			}
			return yaml.Marshal(&file)
		},
	},
	{
		// A check-in sent again restamps the row's marker, and one taken back
		// leaves a "not_checked_in@<ms>" marker behind. Rows are parsed by the
		// zekken setting in config.md, which this table cannot see, so the
		// marker column is rewritten as text (unstampCheckInMarkers, the
		// marker's owner in participants.go) rather than through a Go field.
		Name: "participants.csv",
		parse: func(_ string, raw []byte) (any, error) {
			records, err := readParticipantsCSVRecords(raw)
			return &records, err
		},
		trim: func(v any) bool {
			unstampCheckInMarkers(*v.(*[][]string))
			return true
		},
		encode: func(v any) ([]byte, error) { return writeParticipantsCSVRecords(*v.(*[][]string)) },
	},
	{
		// A member saved under the name they already carry is stamped again.
		Name:   teamMembersFilename,
		Fields: []string{"ModifiedAt"},
		parse: func(_ string, raw []byte) (any, error) {
			members, err := parseSquadsBytes(raw)
			return &members, err
		},
		encode: func(v any) ([]byte, error) {
			return yaml.Marshal(&teamMembersFile{Members: *v.(*map[string][]domain.TeamMember)})
		},
	},
	{
		// A lineup saved unchanged restamps every position and adds a History
		// entry per position holding the value it already had. A real change
		// always changes a position too, so History is left out whole. A
		// removal with no live record appends a removal marker (IsRemovalMarker)
		// whose only payload is its arrival stamp; dropping removal markers
		// keeps no-op removals from moving the fingerprint.
		Name:   teamLineupFilename,
		Fields: []string{"Stamps", "History"},
		parse: func(_ string, raw []byte) (any, error) {
			lineups, err := decodeTeamLineupsBytes(raw)
			return &lineups, err
		},
		trim: func(v any) bool {
			lineups := *v.(*map[string]domain.TeamLineup)
			for k, l := range lineups {
				if l.IsRemovalMarker() {
					delete(lineups, k)
				}
			}
			return len(lineups) > 0
		},
		encode: func(v any) ([]byte, error) {
			lineups := *v.(*map[string]domain.TeamLineup)
			file := teamLineupFile{Lineups: make([]domain.TeamLineup, 0, len(lineups))}
			for _, k := range fingerprintSortedKeys(lineups) {
				file.Lineups = append(file.Lineups, lineups[k])
			}
			return yaml.Marshal(&file)
		},
	},
}

// FingerprintSkippedDirs are the competition subfolders the fingerprint leaves
// out: every score or decision write appends a line to history/, and most to
// events/, a write that changes nothing included, while every real change they
// log also changes pool-matches.csv or bracket.json.
var FingerprintSkippedDirs = []string{"history", "events"}

// Fingerprint returns a hex sha256 over the sorted paths and contents of a
// snapshot's files, with FingerprintSkippedDirs left out and the stamp fields
// of FingerprintStampedFiles zeroed. Two snapshots that differ only by writes
// that changed nothing give the same fingerprint. A stamped file its parser
// refuses, or that the store's format does not write back byte for byte, is
// hashed as its raw bytes.
func Fingerprint(files map[string][]byte) string {
	h := sha256.New()
	var n [8]byte
	frame := func(b []byte) {
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	for _, p := range fingerprintSortedKeys(files) {
		body := files[p]
		if compID, rest, ok := competitionFilePath(p); ok {
			if fingerprintSkipped(rest) {
				continue
			}
			if sf := stampedFileNamed(rest); sf != nil {
				var keep bool
				body, keep = sf.normalise(compID, body)
				if !keep {
					continue
				}
			}
		}
		frame([]byte(p))
		frame(body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (sf *StampedFile) normalise(compID string, raw []byte) ([]byte, bool) {
	v, err := sf.parse(compID, raw)
	if err != nil {
		return raw, true
	}
	// Only a file the store's own format writes back byte for byte is
	// normalised, so a lenient parse (a row skipped, a legacy column) never
	// hides a difference beyond the stamp fields.
	if same, err := sf.encode(v); err != nil || !bytes.Equal(same, raw) {
		return raw, true
	}
	if sf.trim != nil && !sf.trim(v) {
		return nil, false
	}
	names := make(map[string]bool, len(sf.Fields))
	for _, f := range sf.Fields {
		names[f] = true
	}
	zeroNamedFields(reflect.ValueOf(v), names)
	out, err := sf.encode(v)
	if err != nil {
		return raw, true
	}
	return out, true
}

// competitionFilePath splits "competitions/<id>/<rest>".
func competitionFilePath(p string) (compID, rest string, ok bool) {
	after, found := strings.CutPrefix(p, "competitions/")
	if !found {
		return "", "", false
	}
	compID, rest, ok = strings.Cut(after, "/")
	return compID, rest, ok && compID != "" && rest != ""
}

func fingerprintSkipped(rest string) bool {
	for _, d := range FingerprintSkippedDirs {
		if strings.HasPrefix(rest, d+"/") {
			return true
		}
	}
	return false
}

func stampedFileNamed(rest string) *StampedFile {
	for i := range FingerprintStampedFiles {
		if FingerprintStampedFiles[i].Name == rest {
			return &FingerprintStampedFiles[i]
		}
	}
	return nil
}

// zeroNamedFields zeroes every exported struct field whose name is in names,
// at any depth below v (through pointers, slices, arrays and map values).
func zeroNamedFields(v reflect.Value, names map[string]bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			zeroNamedFields(v.Elem(), names)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			f := v.Field(i)
			if names[field.Name] {
				if f.CanSet() {
					f.Set(reflect.Zero(f.Type()))
				}
				continue
			}
			zeroNamedFields(f, names)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			zeroNamedFields(v.Index(i), names)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			cp := reflect.New(iter.Value().Type()).Elem()
			cp.Set(iter.Value())
			zeroNamedFields(cp, names)
			v.SetMapIndex(iter.Key(), cp)
		}
	}
}

// encodePoolMatchesCSV writes results in pool-matches.csv's format, column for
// column as savePoolMatchesLocked does.
func encodePoolMatchesCSV(results []MatchResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(poolMatchHeader()); err != nil {
		return nil, err
	}
	for i := range results {
		row := make([]string, len(poolMatchColumns))
		for c, col := range poolMatchColumns {
			row[c] = col.put(&results[i])
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fingerprintSortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
