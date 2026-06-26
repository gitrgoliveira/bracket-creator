package mobileapp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"gopkg.in/yaml.v3"
)

// ImportManifestComp describes one competition entry in manifest.yaml.
type ImportManifestComp struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Kind           string   `yaml:"kind"`   // "individual" or "team"
	Format         string   `yaml:"format"` // "mixed", "playoffs", "league", or "swiss"; omit/empty for default (playoffs)
	Courts         []string `yaml:"courts"`
	PoolSize       int      `yaml:"pool_size"`
	PoolSizeMode   string   `yaml:"pool_size_mode"` // "max" or "min"
	PoolWinners    int      `yaml:"pool_winners"`
	RoundRobin     bool     `yaml:"round_robin"`
	NumberPrefix   string   `yaml:"number_prefix"`
	WithZekkenName bool     `yaml:"with_zekken_name"`
	TeamSize       int      `yaml:"team_size"`
	Mirror         bool     `yaml:"mirror"`
	StartTime      string   `yaml:"start_time"`
	Date           string   `yaml:"date"`
	// SwissRounds — number of Swiss rounds to play when format=swiss
	// (FR-050a). Ignored for other formats.
	SwissRounds int `yaml:"swiss_rounds"`
	// File names relative to the uploaded set
	Participants string `yaml:"participants"`
	Seeds        string `yaml:"seeds"`
}

type ImportManifest struct {
	Competitions []ImportManifestComp `yaml:"competitions"`
}

type ImportResult struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ParticipantCount int    `json:"participantCount"`
	SeedCount        int    `json:"seedCount"`
	Error            string `json:"error,omitempty"`
}

func RegisterImportHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub, elevated ElevatedVerifier) {
	r.POST("/tournament/import", RequireElevatedPassword(elevated), func(c *gin.Context) {
		if err := c.Request.ParseMultipartForm(64 << 20); err != nil { // 64 MB limit
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to parse multipart form: " + err.Error()})
			return
		}

		form := c.Request.MultipartForm
		if form == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no multipart form data"})
			return
		}

		// Build a map of filename → file contents from all uploaded files.
		// Store by the original (possibly path-prefixed) filename only; findFile
		// handles base-name fallback so we don't need to double-index.
		fileMap := make(map[string][]byte)
		for _, headers := range form.File {
			for _, fh := range headers {
				data, err := readFormFile(fh)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot read %s: %v", fh.Filename, err)})
					return
				}
				fileMap[fh.Filename] = data
			}
		}

		// Find manifest.yaml or manifest.json.
		manifestData, manifestName := findManifest(fileMap)
		if manifestData == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no manifest.yaml or manifest.json found in uploaded files"})
			return
		}

		var manifest ImportManifest
		if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot parse %s: %v", manifestName, err)})
			return
		}

		if len(manifest.Competitions) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "manifest defines no competitions"})
			return
		}

		var results []ImportResult
		for _, entry := range manifest.Competitions {
			r := importCompetition(store, entry, fileMap)
			results = append(results, r)
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, gin.H{"results": results})
	})
}

func importCompetition(store *state.Store, entry ImportManifestComp, files map[string][]byte) ImportResult {
	// Pre-trim Name so the ImportResult returned to the client matches
	// the canonical record we save. Pre-fix: res.Name = entry.Name kept
	// any leading/trailing whitespace from the manifest, so the import
	// API reported "  Cup  " while the persisted record was "Cup" —
	// admin UI then showed two different names for the same competition.
	trimmedName := strings.TrimSpace(entry.Name)
	res := ImportResult{ID: entry.ID, Name: trimmedName}

	if entry.ID == "" {
		res.Error = "missing id"
		return res
	}
	if err := state.ValidateCompetitionID(entry.ID); err != nil {
		res.Error = err.Error()
		return res
	}
	// Cross-file guard symmetry with handlers_competition.go (POST + PUT)
	// and handlers_tournament.go. A manifest with `name: "   "` would
	// otherwise persist as Competition.Name = "" and render a blank
	// card in the admin UI. Per-competition result.Error so the rest
	// of the import batch isn't aborted by one malformed manifest row.
	if trimmedName == "" {
		res.Error = "competition name is required"
		return res
	}

	// Trim string fields so a YAML manifest with `name: "  Cup  "` or
	// `number_prefix: "  A  "` doesn't persist the padded values.
	// The POST/PUT handlers in handlers_competition.go trim
	// comp.Name + comp.NumberPrefix; importCompetition bypasses those
	// handlers (writes via store.SaveCompetitionChanged directly), so
	// it needs its own trim. Cross-file guard symmetry —
	// three sibling write paths, all three trim.
	comp := &state.Competition{
		ID:             entry.ID,
		Name:           trimmedName,
		Kind:           strings.TrimSpace(entry.Kind),
		Format:         strings.TrimSpace(entry.Format),
		Courts:         entry.Courts,
		PoolSize:       entry.PoolSize,
		PoolSizeMode:   strings.TrimSpace(entry.PoolSizeMode),
		PoolWinners:    entry.PoolWinners,
		RoundRobin:     entry.RoundRobin,
		NumberPrefix:   strings.TrimSpace(entry.NumberPrefix),
		WithZekkenName: entry.WithZekkenName,
		TeamSize:       entry.TeamSize,
		Mirror:         entry.Mirror,
		StartTime:      strings.TrimSpace(entry.StartTime),
		Date:           strings.TrimSpace(entry.Date),
		SwissRounds:    entry.SwissRounds,
		Status:         state.CompStatusSetup,
	}
	// Cross-file guard symmetry with handlers_competition.go (POST + PUT):
	// reject oversized string fields before they land on disk. Without
	// this an imported manifest could persist a 1MB Name where the REST
	// API would reject the same value at 200 chars.
	if err := validateCompetitionLengths(comp); err != nil {
		res.Error = err.Error()
		return res
	}

	// Cross-file guard symmetry with the POST /competitions and
	// PUT /competitions/:id handlers, which call validateCompetitionCourts
	// to reject empty / multi-character / >26-court manifests. Pre-fix the
	// import path bypassed this check and could land a Competition with
	// court labels that no other write path would accept — e.g. a manifest
	// row with 30 courts or court="AA" would persist via SaveCompetition
	// here while the same value via the REST API would 400. Empty courts
	// are permitted here — resolveCompetitionCourts (below, once the
	// tournament is loaded) inherits the tournament's courts, matching the
	// POST/PUT handlers. Per-row res.Error to match the other patterns.
	if err := validateCompetitionCourts(comp.Courts); err != nil {
		res.Error = "courts: " + err.Error()
		return res
	}

	// Reject non-canonical Date format (see validateDateDMY in
	// handlers_tournament.go). Per-row res.Error to match the existing
	// missing-id / invalid-id / save-error patterns — doesn't HTTP-fail
	// the batch.
	if err := validateDateDMY(comp.Date); err != nil {
		res.Error = err.Error()
		return res
	}

	// Cross-file guard symmetry with POST/PUT /competitions: reject a
	// competition date that falls outside the tournament's day range.
	// Load the tournament once per row; the cost is a file stat + cache
	// hit after the first row. Failures are soft (res.Error, not HTTP
	// abort) matching all other per-row validation patterns.
	importTourn, importTournErr := store.LoadTournament()
	if importTournErr != nil {
		res.Error = "load tournament: " + importTournErr.Error()
		return res
	}
	if err := validateCompetitionDateInTournament(comp, importTourn); err != nil {
		res.Error = err.Error()
		return res
	}

	// Guarantee >=1 court: a manifest row that omits courts inherits the
	// tournament's courts, identical to the POST /competitions and PUT
	// settings handlers (resolveCompetitionCourts). Keeps all three write
	// paths on one rule instead of a special-case "A" default for imports.
	comp.Courts = resolveCompetitionCourts(comp.Courts, importTourn)

	// Cross-file guard symmetry with POST /competitions and PUT
	// /competitions/:id (handlers_competition.go): reject unknown formats
	// (400) so a manifest cannot persist a Competition whose format would
	// be rejected via the REST API. PoolFormat is not in
	// ImportManifestComp (always ""), so only comp.Format needs checking
	// here. FR-050a: swiss is accepted but additionally requires
	// swissRounds >= 1 — validateSwissConfig enforces that below.
	if _, err := validateCompetitionFormat(comp.Format, ""); err != nil {
		res.Error = "format: " + err.Error()
		return res
	}
	if err := validateSwissConfig(comp); err != nil {
		res.Error = "swissRounds: " + err.Error()
		return res
	}

	if comp.PoolSize == 0 {
		comp.PoolSize = 4
	}
	if comp.PoolSizeMode == "" {
		comp.PoolSizeMode = "max"
	}

	// Parse participants AND seeds BEFORE saving the competition config.
	// Pre-fix, the config was saved first and the participants parse
	// happened after — so a malformed participants file left a half-
	// written competition on disk (config.md present, no participants.
	// csv). The ID-collision guard then made retries impossible: the
	// next attempt with the same manifest ID got "already exists" even
	// though the prior attempt failed. Parsing first means a parse
	// failure surfaces res.Error without ever touching disk; the user
	// can fix the file and retry the manifest cleanly.
	var parsedPlayers []domain.Player
	if entry.Participants != "" {
		data := findFile(files, entry.Participants)
		if data == nil {
			res.Error = fmt.Sprintf("participants file %q not found in upload", entry.Participants)
			return res
		}
		lines := csvLines(data)
		players, err := helper.CreatePlayers(lines, entry.WithZekkenName)
		if err != nil {
			res.Error = "parse participants: " + err.Error()
			return res
		}
		// Cross-file guard symmetry with POST /participants: reject
		// oversized fields before they land in participants.csv. The
		// REST API caps the same fields client-side at write time;
		// without this, the import path could persist values the API
		// would reject.
		for i, p := range players {
			if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Source, p.Metadata); err != nil {
				res.Error = fmt.Sprintf("participants[%d]: %s", i, err.Error())
				return res
			}
		}
		parsedPlayers = players
	}

	// Seeds parse: mirror the participants block's error-handling pattern.
	// Pre-fix this swallowed THREE shapes silently:
	//   (1) entry.Seeds named a file that wasn't in the upload — user
	//       got SeedCount=0 with no error and assumed the import worked.
	//   (2) parseSeedsBytes returned err != nil — currently unreachable
	//       (parseSeedsBytes never produces a non-nil err) but the dead
	//       branch documented "errors are OK to ignore" which would
	//       silently regress the moment parseSeedsBytes started surfacing
	//       parse failures.
	//   (3) The file was present but parsed to zero assignments — kept
	//       as a soft no-op (legitimate "header-only" / "no seeds yet"
	//       intent; symmetric with empty participants file → 0 players).
	// Only (1) and (2) become hard errors here; (3) stays soft.
	var parsedSeeds []domain.SeedAssignment
	if entry.Seeds != "" {
		data := findFile(files, entry.Seeds)
		if data == nil {
			res.Error = fmt.Sprintf("seeds file %q not found in upload", entry.Seeds)
			return res
		}
		assignments, err := parseSeedsBytes(data)
		if err != nil {
			res.Error = "parse seeds: " + err.Error()
			return res
		}
		for i, sa := range assignments {
			if err := validateMaxLen(fmt.Sprintf("seeds[%d].name", i), sa.Name, MaxLenSeedAssignmentName); err != nil {
				res.Error = err.Error()
				return res
			}
		}
		if len(assignments) > 0 {
			parsedSeeds = assignments
		}
	}

	// Cross-file guard symmetry with handlers_competition.go POST + PUT:
	// the uniqueness check + save run under WithCompetitionRenameLock
	// so two concurrent imports (or an import racing against a POST)
	// can't both land competitions with the same Name. Per-row error
	// lands in res.Error rather than aborting the batch — matches the
	// existing missing-id / invalid-id / save-error patterns.
	if err := store.WithCompetitionRenameLock(func() error {
		// ID-collision check (cross-file guard symmetry with the POST
		// /competitions handler and CreatePlayoff path). Without this,
		// a manifest row with an existing comp.ID but a different
		// comp.Name passes the name-uniqueness check (its name IS
		// unique) and then SaveCompetition silently overwrites the
		// existing record. POST is documented as CREATE, so an
		// existing ID is a 400-ish conflict. Retry-after-failure is
		// safe because the participants/seeds parse above happens
		// pre-save — a parse failure aborts the row before this guard
		// runs, so the disk stays clean and the retry's collision
		// check passes on a never-saved ID.
		if existing, _ := store.LoadCompetition(comp.ID); existing != nil {
			res.Error = fmt.Sprintf("competition ID %q already exists", comp.ID)
			return nil
		}
		if infraErr, uniqueErr := checkUniqueCompFields(store, comp.Name, comp.NumberPrefix, comp.ID); infraErr != nil {
			return infraErr
		} else if uniqueErr != nil {
			res.Error = uniqueErr.Error()
			return nil
		}
		if len(parsedPlayers) > 0 {
			// Mirror saveCompetitionWithPlayers semantics: when
			// participants land, the config records that fact so
			// later HasIDs-hinted loads parse correctly.
			comp.HasParticipantIDs = true
		}
		return store.SaveCompetition(comp)
	}); err != nil {
		res.Error = "save competition: " + err.Error()
		return res
	}
	// If the uniqueness check failed, res.Error was set inside the
	// closure — abort the row before participants/seeds save below.
	if res.Error != "" {
		return res
	}

	// Save participants — already parsed pre-save, so this is a pure
	// disk write that can only fail on I/O.
	//
	// Atomicity: SaveCompetition above has already written config.md
	// (the visible "this competition exists" marker, enforced by the
	// ID-collision check at the top of the lock). If SaveParticipants
	// or SaveSeeds below fails, the config is orphaned — and the
	// ID-collision check now blocks retries with the same manifest
	// (the row says "save participants: …" but the disk says "ID
	// already exists" on the next attempt). Roll back the config on
	// post-save failure so the row is fully reversed and the operator
	// can re-run the import after fixing the I/O issue.
	if len(parsedPlayers) > 0 {
		// helper.Player is a type alias for domain.Player (NFR-007); the
		// parser output flows straight into SaveParticipants.
		if err := store.SaveParticipants(entry.ID, parsedPlayers); err != nil {
			_ = store.DeleteCompetition(entry.ID) // best-effort rollback
			res.Error = "save participants: " + err.Error()
			return res
		}
		res.ParticipantCount = len(parsedPlayers)
	}

	// Save seeds — already parsed pre-save. Surface SaveSeeds I/O errors
	// to res.Error rather than swallowing them: pre-fix `_ = SaveSeeds(...)`
	// followed by `res.SeedCount = N` would claim N seeds imported even
	// when disk write failed (permission denied, no space, etc.), so the
	// admin UI showed a green "import successful" while seeds.csv was
	// empty / missing. Mirror the SaveParticipants pattern above —
	// errors abort the row with a clear message AND roll back so a
	// retry isn't blocked by the ID-collision check.
	if len(parsedSeeds) > 0 {
		if err := store.SaveSeeds(entry.ID, parsedSeeds); err != nil {
			_ = store.DeleteCompetition(entry.ID) // best-effort rollback
			res.Error = "save seeds: " + err.Error()
			return res
		}
		res.SeedCount = len(parsedSeeds)
	}

	return res
}

func readFormFile(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func findManifest(files map[string][]byte) ([]byte, string) {
	for _, name := range []string{"manifest.yaml", "manifest.yml", "manifest.json"} {
		if data := findFile(files, name); data != nil {
			return data, name
		}
	}
	return nil, ""
}

func findFile(files map[string][]byte, name string) []byte {
	if data, ok := files[name]; ok {
		return data
	}
	base := baseName(name)
	if data, ok := files[base]; ok {
		return data
	}
	return nil
}

func baseName(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func csvLines(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseSeedsBytes(data []byte) ([]domain.SeedAssignment, error) {
	var assignments []domain.SeedAssignment
	for i, line := range csvLines(data) {
		parts := strings.Split(line, ",")
		if i == 0 && len(parts) >= 2 && strings.ToLower(strings.TrimSpace(parts[0])) == "rank" {
			continue // skip header
		}
		if len(parts) < 2 {
			continue
		}
		rank := 0
		name := ""
		// Support both "rank,name" and "name,rank" formats
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &rank); err == nil {
			name = strings.TrimSpace(parts[1])
		} else if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &rank); err == nil {
			name = strings.TrimSpace(parts[0])
		}
		if rank > 0 && name != "" {
			assignments = append(assignments, domain.SeedAssignment{Name: name, SeedRank: rank})
		}
	}
	return assignments, nil
}
