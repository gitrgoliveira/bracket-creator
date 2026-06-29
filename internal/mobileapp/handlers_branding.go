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

// RegisterPublicBrandingHandlers wires the unauthenticated GET and HEAD routes
// that serve the tournament logo bytes. Returns 404 when no logo is configured.
// HEAD is registered explicitly because Gin does not auto-route HEAD to GET.
// The BrandingManager component uses HEAD to probe logo existence on mount.
func RegisterPublicBrandingHandlers(r *gin.RouterGroup, store *state.Store) {
	brandingLogoHandler := func(c *gin.Context) {
		// Prevent browsers/proxies from caching 404s , a newly uploaded logo
		// would otherwise stay "missing" until a hard refresh.
		c.Header("Cache-Control", "no-cache")
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil || t.Theme == nil || t.Theme.LogoPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "no logo configured"})
			return
		}
		name := t.Theme.LogoPath
		// Only allow the two known filenames , never serve arbitrary paths.
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
	}
	r.GET("/branding/logo", brandingLogoHandler)
	// HEAD is used by BrandingManager to probe logo existence without
	// fetching the full image payload. Gin does not auto-route HEAD to GET,
	// so we register it explicitly with the same handler (c.File/ServeContent
	// strips the body for HEAD requests automatically).
	r.HEAD("/branding/logo", brandingLogoHandler)
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
		if fh.Size > BrandingMaxFileBytes {
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
		// Use os.CreateTemp for a unique temp file so concurrent uploads
		// don't share the same ".tmp" path and corrupt each other's write.
		// Temp file is in brandingDir so the final Rename stays on the
		// same filesystem and remains atomic. // #nosec G304
		dst, err := os.CreateTemp(brandingDir, "logo-*.tmp")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		tmpPath := dst.Name()
		written, copyErr := io.Copy(dst, io.LimitReader(src, BrandingMaxFileBytes+1))
		cerr := dst.Close()
		if copyErr != nil || cerr != nil {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": errors.Join(copyErr, cerr).Error()})
			return
		}
		if written > BrandingMaxFileBytes {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "logo must be ≤1 MB"})
			return
		}
		// os.Rename is atomic on POSIX but fails on Windows when the
		// destination already exists. Remove the destination first and retry
		// once so repeated logo uploads work on all platforms.
		if err := os.Rename(tmpPath, fullPath); err != nil {
			if removeErr := os.Remove(fullPath); removeErr == nil {
				err = os.Rename(tmpPath, fullPath)
			}
			if err != nil {
				_ = os.Remove(tmpPath)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		// Update state before removing the other extension so GET never
		// fetches a file that has already been deleted.
		//
		// Use a sentinel so the error handler can distinguish "tournament
		// was never initialized" (file is orphaned, safe to remove) from
		// other transient failures (new bytes are in place; removing the
		// file would leave Theme.LogoPath pointing at a missing file).
		errNotInit := errors.New("tournament not initialized")
		_, err = store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
			if current == nil {
				return errNotInit
			}
			*desired = *current
			if desired.Theme == nil {
				desired.Theme = &state.Theme{}
			}
			desired.Theme.LogoPath = fileName
			return nil
		})
		if err != nil {
			if errors.Is(err, errNotInit) {
				_ = os.Remove(fullPath)
			}
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
			// Include WindowTitle in the check so deleting only the logo
			// doesn't silently clear a configured window title.
			if desired.Theme.PrimaryColor == "" && desired.Theme.AccentSoftColor == "" && desired.Theme.WindowTitle == "" {
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
