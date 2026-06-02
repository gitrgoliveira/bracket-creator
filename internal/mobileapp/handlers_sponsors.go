package mobileapp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// sponsorsDirName is the subdirectory under the store root that holds
// uploaded sponsor logo files. Created lazily on first upload.
const sponsorsDirName = "sponsors"

// sponsorFilePattern enforces the server-generated filename shape on the
// public GET route: 16 lowercase hex chars + .png/.jpg/.jpeg. Any other
// shape (path traversal, absent suffix, uppercase, longer/shorter) is
// rejected before touching the filesystem.
var sponsorFilePattern = regexp.MustCompile(`^[a-f0-9]{16}\.(png|jpe?g)$`)

// validSponsorContentTypes is the allowlist for the http.DetectContentType
// sniff result on POST. Header Content-Type is not trusted.
var validSponsorContentTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
}

// maxSponsorNameLen and maxSponsorLinkLen bound the optional metadata
// fields to keep YAML compact and prevent abuse.
const (
	maxSponsorNameLen = 80
	maxSponsorLinkLen = 500
)

// RegisterPublicSponsorHandlers wires the unauthenticated GET route that
// serves sponsor logo bytes. Public because the logos render on viewer
// and TV/lobby surfaces that have no admin credentials.
func RegisterPublicSponsorHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/sponsors/:file", func(c *gin.Context) {
		name := c.Param("file")
		if !sponsorFilePattern.MatchString(name) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sponsor filename"})
			return
		}
		path := filepath.Join(store.GetFolder(), sponsorsDirName, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			c.JSON(http.StatusNotFound, gin.H{"error": "sponsor logo not found"})
			return
		}
		// Content-hashed filenames are unique per upload, so the bytes a
		// given URL refers to never change. immutable is correct here, not
		// aspirational; see mp-c38 plan.
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Header("ETag", `"`+strings.TrimSuffix(name, filepath.Ext(name))+`"`)
		// Pick content-type from filename extension; we control the names
		// so this is safe (no header-trust loop).
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png":
			c.Header("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			c.Header("Content-Type", "image/jpeg")
		}
		c.File(path)
	})
}

// RegisterSponsorHandlers wires admin-gated mutation endpoints onto the
// supplied admin router group (which already carries body-cap + auth
// middleware). The caller must use a group whose body cap is at least
// SponsorMaxBodyBytes (2 MB) — the in-handler file size check at
// SponsorMaxFileBytes still applies separately.
func RegisterSponsorHandlers(r *gin.RouterGroup, store *state.Store) {
	r.POST("/sponsors", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not initialized"})
			return
		}
		if len(t.Sponsors) >= state.MaxSponsors {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "maximum " + strconv.Itoa(state.MaxSponsors) + " sponsors per tournament",
			})
			return
		}

		name := strings.TrimSpace(c.PostForm("name"))
		if name == "" || len([]rune(name)) > maxSponsorNameLen {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required (1–80 chars)"})
			return
		}

		link := strings.TrimSpace(c.PostForm("link"))
		if link != "" {
			if len(link) > maxSponsorLinkLen {
				c.JSON(http.StatusBadRequest, gin.H{"error": "link must be ≤500 chars"})
				return
			}
			u, perr := url.Parse(link)
			if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "link must be a valid http(s) URL"})
				return
			}
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
			return
		}
		if fileHeader.Size > SponsorMaxFileBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}

		src, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer func() { _ = src.Close() }()

		// Sniff the first 512 bytes per http.DetectContentType contract.
		// Don't trust the Content-Type header; clients lie.
		sniffBuf := make([]byte, 512)
		nRead, _ := io.ReadFull(src, sniffBuf)
		sniffed := http.DetectContentType(sniffBuf[:nRead])
		ext, ok := validSponsorContentTypes[sniffed]
		if !ok {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "only PNG or JPEG accepted"})
			return
		}
		// Rewind so we can copy the full file. fileHeader.Open returns a
		// multipart.File which is always io.Seeker — but defend against
		// future refactors with an explicit assertion.
		seeker, ok := src.(io.Seeker)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal: file not seekable"})
			return
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Server-generated random filename: 16 hex chars + sniffed-ext.
		// Each upload gets a unique URL — see mp-c38 plan §2.
		nameBytes := make([]byte, 8)
		if _, err := rand.Read(nameBytes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		fileName := hex.EncodeToString(nameBytes) + ext

		sponsorsDir := filepath.Join(store.GetFolder(), sponsorsDirName)
		if err := os.MkdirAll(sponsorsDir, 0o700); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// fileName is server-generated (16 random hex chars + sniffed image
		// extension); the path joins under sponsorsDir which is itself
		// derived from store.GetFolder. No user-controlled input reaches
		// the OS call.
		// #nosec G304 -- filename is server-generated, not from user input
		dst, err := os.OpenFile(filepath.Join(sponsorsDir, fileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Cap the file-write side at SponsorMaxFileBytes+1 so an envelope
		// that lied about size can still be caught mid-stream.
		written, copyErr := io.Copy(dst, io.LimitReader(src, SponsorMaxFileBytes+1))
		cerr := dst.Close()
		if copyErr != nil || cerr != nil {
			_ = os.Remove(filepath.Join(sponsorsDir, fileName))
			c.JSON(http.StatusInternalServerError, gin.H{"error": errors.Join(copyErr, cerr).Error()})
			return
		}
		if written > SponsorMaxFileBytes {
			_ = os.Remove(filepath.Join(sponsorsDir, fileName))
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}

		// Persist the sponsor entry. Re-load under the store lock via
		// SaveTournament — the brief window between LoadTournament above
		// and Save below is bounded by the per-store mutex in Save.
		sponsor := state.Sponsor{Name: name, File: fileName, Link: link}
		t.Sponsors = append(t.Sponsors, sponsor)
		if err := store.SaveTournament(t); err != nil {
			_ = os.Remove(filepath.Join(sponsorsDir, fileName))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, sponsor)
	})

	r.DELETE("/sponsors/:index", func(c *gin.Context) {
		idx, err := strconv.Atoi(c.Param("index"))
		if err != nil || idx < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
			return
		}
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil || idx >= len(t.Sponsors) {
			c.JSON(http.StatusNotFound, gin.H{"error": "sponsor not found"})
			return
		}
		removed := t.Sponsors[idx]
		t.Sponsors = append(t.Sponsors[:idx], t.Sponsors[idx+1:]...)
		if err := store.SaveTournament(t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Unlink the file unconditionally — random filenames mean cross-
		// references are effectively impossible at the 1–6 scale (mp-c38).
		// Best-effort: log on failure but don't roll back the YAML write.
		_ = os.Remove(filepath.Join(store.GetFolder(), sponsorsDirName, removed.File))
		c.JSON(http.StatusOK, gin.H{"removed": removed})
	})
}
