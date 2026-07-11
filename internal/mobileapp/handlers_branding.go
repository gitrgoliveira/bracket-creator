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
// that serve the tournament logo bytes. When no custom logo is configured, GET
// redirects to the bundled default (/logo.jpeg) so image consumers never see a
// 404, while HEAD returns 404 so BrandingManager can probe whether a custom
// logo is set. HEAD is registered explicitly because Gin does not auto-route
// HEAD to GET.
func RegisterPublicBrandingHandlers(r *gin.RouterGroup, store *state.Store) {
	// serveLogo serves the custom logo when one is configured; otherwise it
	// calls onMissing, which differs by method (GET redirects to the default,
	// HEAD 404s for the existence probe).
	serveLogo := func(c *gin.Context, onMissing func(*gin.Context)) {
		// Prevent browsers/proxies from caching the miss/redirect: a newly
		// uploaded logo would otherwise stay "missing" until a hard refresh.
		c.Header("Cache-Control", "no-cache")
		t, err := store.LoadTournament()
		if err != nil {
			internalError(c, err)
			return
		}
		if t == nil || t.Theme == nil || t.Theme.LogoPath == "" {
			onMissing(c)
			return
		}
		name := t.Theme.LogoPath
		// Only allow the two known filenames, never serve arbitrary paths.
		if name != "logo.png" && name != "logo.jpg" {
			onMissing(c)
			return
		}
		path := filepath.Join(store.GetFolder(), brandingDirName, name)
		info, err := os.Lstat(path)
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			onMissing(c)
			return
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png":
			c.Header("Content-Type", "image/png")
		case ".jpg":
			c.Header("Content-Type", "image/jpeg")
		}
		c.File(path)
	}
	// GET: fall back to the bundled default logo so the topbar/auth <img>
	// never logs a console 404 (it would otherwise swap to /logo.jpeg via its
	// onError handler anyway). Redirecting keeps that outcome without the noise.
	r.GET("/branding/logo", func(c *gin.Context) {
		serveLogo(c, func(c *gin.Context) { c.Redirect(http.StatusFound, "/logo.jpeg") })
	})
	// HEAD is used by BrandingManager to probe custom-logo existence without
	// fetching the payload, so it must keep returning 404 when none is set.
	// Gin does not auto-route HEAD to GET, so register it explicitly.
	r.HEAD("/branding/logo", func(c *gin.Context) {
		serveLogo(c, func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"error": "no logo configured"}) })
	})
}

// RegisterBrandingHandlers wires admin-gated mutation endpoints. The caller
// must use a group whose body cap is at least BrandingMaxBodyBytes (2 MB).
//
// hub broadcasts EventTournamentUpdated after a successful logo upload/delete
// so open viewer and display surfaces re-fetch and re-apply branding live,
// matching PUT /tournament (same live-update gap as sponsors, mp-scf).
func RegisterBrandingHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
	r.POST("/branding/logo", handleBrandingLogoUpload(store, hub))
	r.DELETE("/branding/logo", handleBrandingLogoDelete(store, hub))
}

func handleBrandingLogoUpload(store *state.Store, hub *Hub) gin.HandlerFunc {
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
			internalError(c, err)
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
			internalError(c, err)
			return
		}

		brandingDir := filepath.Join(store.GetFolder(), brandingDirName)
		if err := os.MkdirAll(brandingDir, 0o700); err != nil {
			internalError(c, err)
			return
		}
		fullPath := filepath.Join(brandingDir, fileName)
		// Use os.CreateTemp for a unique temp file so concurrent uploads
		// don't share the same ".tmp" path and corrupt each other's write.
		// Temp file is in brandingDir so the final Rename stays on the
		// same filesystem and remains atomic. // #nosec G304
		dst, err := os.CreateTemp(brandingDir, "logo-*.tmp")
		if err != nil {
			internalError(c, err)
			return
		}
		tmpPath := dst.Name()
		written, copyErr := io.Copy(dst, io.LimitReader(src, BrandingMaxFileBytes+1))
		cerr := dst.Close()
		if copyErr != nil || cerr != nil {
			_ = os.Remove(tmpPath)
			internalError(c, errors.Join(copyErr, cerr))
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
				internalError(c, err)
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
			internalError(c, err)
			return
		}

		// Remove the other extension only after state points to the new file.
		other := "logo.png"
		if fileName == "logo.png" {
			other = "logo.jpg"
		}
		_ = os.Remove(filepath.Join(brandingDir, other))

		if hub != nil {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, gin.H{"logoPath": fileName})
	}
}

func handleBrandingLogoDelete(store *state.Store, hub *Hub) gin.HandlerFunc {
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
		if hub != nil {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, gin.H{"removed": removed})
	}
}
