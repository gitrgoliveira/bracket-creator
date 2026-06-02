package mobileapp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
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

// Sentinel errors surfaced by the POST/DELETE transforms so the handler
// can map them to specific status codes without inspecting strings.
var (
	errSponsorTournamentNotInit = errors.New("tournament not initialized")
	errSponsorCapReached        = errors.New("sponsor cap reached")
	errSponsorNotFound          = errors.New("sponsor not found")
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
		// Lstat (not Stat) so a symlink planted in the sponsors dir is
		// rejected rather than followed. The filename regex prevents
		// directory traversal, but a separate write path (operator,
		// backup-restore, future feature) could drop a symlink into the
		// dir; refusing to serve symlinks closes that gap regardless.
		info, err := os.Lstat(path)
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "sponsor logo not found"})
			return
		}
		ext := strings.ToLower(filepath.Ext(name))
		// Random-token filenames are unique per upload, so the bytes a
		// given URL refers to never change. immutable is correct here,
		// not aspirational; see mp-c38 plan.
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Header("ETag", `"`+strings.TrimSuffix(name, ext)+`"`)
		// Set Content-Type explicitly from extension; we control the
		// names (regex-validated) so this is safe and avoids depending
		// on the platform's MIME database via http.ServeFile.
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
	r.POST("/sponsors", handleSponsorUpload(store))
	r.DELETE("/sponsors/:index", handleSponsorDelete(store))
}

func handleSponsorUpload(store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		candidate := state.Sponsor{
			Name: strings.TrimSpace(c.PostForm("name")),
			Link: strings.TrimSpace(c.PostForm("link")),
		}
		if err := state.ValidateSponsor(candidate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
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

		// Sniff first 512 bytes per http.DetectContentType contract.
		// Don't trust the Content-Type header. multipart.File is
		// guaranteed by the net/textproto contract to be a ReadSeeker,
		// so the Seek below cannot fail in practice.
		sniffBuf := make([]byte, 512)
		nRead, rerr := io.ReadFull(src, sniffBuf)
		// ErrUnexpectedEOF on a short part is fine: DetectContentType
		// sniffs whatever bytes are present. Any other read error is a
		// real I/O failure and we cannot recover.
		if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) && !errors.Is(rerr, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": rerr.Error()})
			return
		}
		sniffed := http.DetectContentType(sniffBuf[:nRead])
		ext, ok := validSponsorContentTypes[sniffed]
		if !ok {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "only PNG or JPEG accepted"})
			return
		}
		if _, err := src.(io.Seeker).Seek(0, io.SeekStart); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Server-generated random filename: 16 hex chars + sniffed ext.
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
		fullPath := filepath.Join(sponsorsDir, fileName)
		// fileName is server-generated (16 random hex chars + sniffed
		// image extension); fullPath joins under sponsorsDir which is
		// itself derived from store.GetFolder. No user-controlled input
		// reaches the OS call.
		// #nosec G304 -- filename is server-generated, not from user input
		dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// LimitReader+1 catches an envelope that lied about its size.
		written, copyErr := io.Copy(dst, io.LimitReader(src, SponsorMaxFileBytes+1))
		cerr := dst.Close()
		if copyErr != nil || cerr != nil {
			_ = os.Remove(fullPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": errors.Join(copyErr, cerr).Error()})
			return
		}
		if written > SponsorMaxFileBytes {
			_ = os.Remove(fullPath)
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}

		// Append the sponsor entry atomically under the store lock.
		// UpdateTournamentChanged serialises load+modify+save so two
		// concurrent uploads can't exceed the cap or clobber each
		// other's appended entry. The transform copies all fields from
		// current; we replace Sponsors with a fresh slice to avoid
		// aliasing current's backing array.
		candidate.File = fileName
		_, err = store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
			if current == nil {
				return errSponsorTournamentNotInit
			}
			*desired = *current
			if len(current.Sponsors) >= state.MaxSponsors {
				return errSponsorCapReached
			}
			sponsors := make([]state.Sponsor, len(current.Sponsors)+1)
			copy(sponsors, current.Sponsors)
			sponsors[len(current.Sponsors)] = candidate
			desired.Sponsors = sponsors
			return nil
		})
		if err != nil {
			_ = os.Remove(fullPath)
			switch {
			case errors.Is(err, errSponsorTournamentNotInit):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, errSponsorCapReached):
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "maximum " + strconv.Itoa(state.MaxSponsors) + " sponsors per tournament",
				})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}
		c.JSON(http.StatusCreated, candidate)
	}
}

func handleSponsorDelete(store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		idx, err := strconv.Atoi(c.Param("index"))
		if err != nil || idx < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
			return
		}
		var removed state.Sponsor
		_, err = store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
			if current == nil || idx >= len(current.Sponsors) {
				return errSponsorNotFound
			}
			*desired = *current
			sponsors := make([]state.Sponsor, 0, len(current.Sponsors)-1)
			sponsors = append(sponsors, current.Sponsors[:idx]...)
			sponsors = append(sponsors, current.Sponsors[idx+1:]...)
			removed = current.Sponsors[idx]
			desired.Sponsors = sponsors
			return nil
		})
		if err != nil {
			if errors.Is(err, errSponsorNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "sponsor not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Best-effort unlink. Random filenames make collisions effectively
		// impossible at the 1–6 scale, so a missing file (ENOENT from a
		// concurrent delete) is harmless and silently ignored.
		_ = os.Remove(filepath.Join(store.GetFolder(), sponsorsDirName, removed.File))
		c.JSON(http.StatusOK, gin.H{"removed": removed})
	}
}
