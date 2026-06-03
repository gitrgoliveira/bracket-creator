package mobileapp

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// brandingDirName is the subdirectory under the store root for uploaded
// tournament logos. One file at a time: logo.png or logo.jpg.
const brandingDirName = "branding"

// validBrandingContentTypes maps http.DetectContentType sniff results to
// the canonical filename extension used on disk.
var validBrandingContentTypes = map[string]string{
	"image/png":  "logo.png",
	"image/jpeg": "logo.jpg",
}

// RegisterPublicBrandingHandlers wires the unauthenticated GET route that
// serves the tournament logo bytes. Returns 404 when no logo is configured.
func RegisterPublicBrandingHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/branding/logo", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil || t == nil || t.Theme == nil || t.Theme.LogoPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "no logo configured"})
			return
		}
		name := t.Theme.LogoPath
		// Only allow the two known filenames — never serve arbitrary paths.
		if name != "logo.png" && name != "logo.jpg" {
			c.JSON(http.StatusNotFound, gin.H{"error": "no logo configured"})
			return
		}
		path := filepath.Join(store.GetFolder(), brandingDirName, name)
		info, err := os.Lstat(path)
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "logo file not found"})
			return
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png":
			c.Header("Content-Type", "image/png")
		case ".jpg":
			c.Header("Content-Type", "image/jpeg")
		}
		// Logo filename changes on each upload (png vs jpg could flip), so
		// use no-cache to ensure browsers don't serve a stale type. The
		// admin upload flow is infrequent, so this is acceptable.
		c.Header("Cache-Control", "no-cache")
		c.File(path)
	})
}

// RegisterBrandingHandlers wires admin-gated mutation endpoints. The caller
// must use a group whose body cap is at least BrandingMaxBodyBytes (2 MB).
func RegisterBrandingHandlers(r *gin.RouterGroup, store *state.Store) {
	r.POST("/branding/logo", handleBrandingLogoUpload(store))
	r.DELETE("/branding/logo", handleBrandingLogoDelete(store))
}

func handleBrandingLogoUpload(store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		fh, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
			return
		}
		if fh.Size > SponsorMaxFileBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}

		src, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer func() { _ = src.Close() }()

		sniffBuf := make([]byte, 512)
		nRead, rerr := io.ReadFull(src, sniffBuf)
		if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) && !errors.Is(rerr, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": rerr.Error()})
			return
		}
		sniffed := http.DetectContentType(sniffBuf[:nRead])
		fileName, ok := validBrandingContentTypes[sniffed]
		if !ok {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "only PNG or JPEG accepted"})
			return
		}
		if _, err := src.Seek(0, io.SeekStart); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		brandingDir := filepath.Join(store.GetFolder(), brandingDirName)
		if err := os.MkdirAll(brandingDir, 0o700); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		fullPath := filepath.Join(brandingDir, fileName)
		// Write to a temp file then rename atomically so concurrent GET
		// requests never see a partial write. fileName is always "logo.png"
		// or "logo.jpg" — server-derived; no user-controlled input reaches
		// the OS call.
		tmpPath := fullPath + ".tmp"
		dst, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		written, copyErr := io.Copy(dst, io.LimitReader(src, SponsorMaxFileBytes+1))
		cerr := dst.Close()
		if copyErr != nil || cerr != nil {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": errors.Join(copyErr, cerr).Error()})
			return
		}
		if written > SponsorMaxFileBytes {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}
		if err := os.Rename(tmpPath, fullPath); err != nil {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Update state before removing the other extension so GET never
		// fetches a file that has already been deleted.
		_, err = store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
			if current == nil {
				return errors.New("tournament not initialized")
			}
			*desired = *current
			if desired.Theme == nil {
				desired.Theme = &state.Theme{}
			}
			desired.Theme.LogoPath = fileName
			return nil
		})
		if err != nil {
			_ = os.Remove(fullPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Remove the other extension only after state points to the new file.
		other := "logo.png"
		if fileName == "logo.png" {
			other = "logo.jpg"
		}
		_ = os.Remove(filepath.Join(brandingDir, other))

		c.JSON(http.StatusOK, gin.H{"logoPath": fileName})
	}
}

func handleBrandingLogoDelete(store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var removed string
		_, err := store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
			if current == nil || current.Theme == nil || current.Theme.LogoPath == "" {
				return errors.New("no logo configured")
			}
			*desired = *current
			removed = current.Theme.LogoPath
			desired.Theme.LogoPath = ""
			// Nil-out the Theme pointer when all fields are now empty so
			// tournament.md doesn't persist a bare "theme: {}" block.
			if desired.Theme.PrimaryColor == "" && desired.Theme.AccentSoftColor == "" {
				desired.Theme = nil
			}
			return nil
		})
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if removed == "logo.png" || removed == "logo.jpg" {
			_ = os.Remove(filepath.Join(store.GetFolder(), brandingDirName, removed))
		}
		c.JSON(http.StatusOK, gin.H{"removed": removed})
	}
}
