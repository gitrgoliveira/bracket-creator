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
	Format         string   `yaml:"format"` // "pools" or "playoffs"
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

func RegisterImportHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
	r.POST("/tournament/import", func(c *gin.Context) {
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
	res := ImportResult{ID: entry.ID, Name: entry.Name}

	if entry.ID == "" {
		res.Error = "missing id"
		return res
	}
	if err := state.ValidateCompetitionID(entry.ID); err != nil {
		res.Error = err.Error()
		return res
	}

	comp := &state.Competition{
		ID:             entry.ID,
		Name:           entry.Name,
		Kind:           entry.Kind,
		Format:         entry.Format,
		Courts:         entry.Courts,
		PoolSize:       entry.PoolSize,
		PoolSizeMode:   entry.PoolSizeMode,
		PoolWinners:    entry.PoolWinners,
		RoundRobin:     entry.RoundRobin,
		NumberPrefix:   entry.NumberPrefix,
		WithZekkenName: entry.WithZekkenName,
		TeamSize:       entry.TeamSize,
		Mirror:         entry.Mirror,
		StartTime:      entry.StartTime,
		Date:           entry.Date,
		Status:         "setup",
	}
	if len(comp.Courts) == 0 {
		comp.Courts = []string{"A"}
	}
	if comp.PoolSize == 0 {
		comp.PoolSize = 4
	}
	if comp.PoolSizeMode == "" {
		comp.PoolSizeMode = "max"
	}

	if err := store.SaveCompetition(comp); err != nil {
		res.Error = "save competition: " + err.Error()
		return res
	}

	// Save participants if file is provided.
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
		if err := store.SaveParticipants(entry.ID, players); err != nil {
			res.Error = "save participants: " + err.Error()
			return res
		}
		res.ParticipantCount = len(players)
	}

	// Save seeds if file is provided.
	if entry.Seeds != "" {
		data := findFile(files, entry.Seeds)
		if data != nil {
			assignments, err := parseSeedsBytes(data)
			if err == nil && len(assignments) > 0 {
				_ = store.SaveSeeds(entry.ID, assignments)
				res.SeedCount = len(assignments)
			}
		}
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
