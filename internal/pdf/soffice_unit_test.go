package pdf

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocateSoffice_EnvVar(t *testing.T) {
	t.Run("valid executable via LIBREOFFICE_PATH", func(t *testing.T) {
		// Create a temp file that looks executable.
		f, err := os.CreateTemp(t.TempDir(), "fake-soffice-*")
		require.NoError(t, err)
		require.NoError(t, f.Close())
		require.NoError(t, os.Chmod(f.Name(), 0o755))

		t.Setenv("LIBREOFFICE_PATH", f.Name())
		got, err := LocateSoffice()
		require.NoError(t, err)
		assert.Equal(t, f.Name(), got)
	})

	t.Run("non-existent path in LIBREOFFICE_PATH falls through", func(t *testing.T) {
		t.Setenv("LIBREOFFICE_PATH", "/nonexistent/soffice-fake")
		// Falls through to PATH lookup and candidates; on most CI boxes without
		// LibreOffice this returns ErrSofficeNotFound — that's the expected path.
		_, err := LocateSoffice()
		// Either succeeds (LibreOffice installed) or returns ErrSofficeNotFound.
		if err != nil {
			assert.ErrorIs(t, err, ErrSofficeNotFound)
		}
	})
}

func TestSofficeCandidates_NotEmpty(t *testing.T) {
	// sofficeCandidates must return at least one entry on every supported OS.
	got := sofficeCandidates()
	assert.NotEmpty(t, got)
	// All candidates must be absolute paths.
	for _, c := range got {
		assert.True(t, filepath.IsAbs(c), "candidate %q must be an absolute path", c)
	}
}

func TestIsExecutableFile(t *testing.T) {
	t.Run("returns false for non-existent path", func(t *testing.T) {
		assert.False(t, isExecutableFile("/nonexistent/path/to/binary"))
	})

	t.Run("returns false for a directory", func(t *testing.T) {
		assert.False(t, isExecutableFile(t.TempDir()))
	})

	if runtime.GOOS != "windows" {
		t.Run("returns true for chmod +x file", func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "exec-*")
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, os.Chmod(f.Name(), 0o755))
			assert.True(t, isExecutableFile(f.Name()))
		})

		t.Run("returns false for non-executable file", func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "noexec-*")
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, os.Chmod(f.Name(), 0o644))
			assert.False(t, isExecutableFile(f.Name()))
		})
	}
}

func TestSofficePath(t *testing.T) {
	// Construct a Converter directly (bypassing LocateSoffice) to unit-test
	// the SofficePath accessor without requiring LibreOffice to be installed.
	conv := &Converter{sofficePath: "/fake/soffice"}
	assert.Equal(t, "/fake/soffice", conv.SofficePath())
}

func TestProfileInstallURIPOSIX(t *testing.T) {
	if runtime.GOOS != "windows" {
		got := profileInstallURI("/tmp/lo-profile-xyz")
		assert.Equal(t, "file:///tmp/lo-profile-xyz", got)
	}
}

// TestLocateSoffice_NoPATH exercises the sofficeCandidates loop by stripping
// $PATH so exec.LookPath misses and the fallback candidate walk runs.
// On CI machines without LibreOffice the test expects ErrSofficeNotFound;
// on dev boxes with soffice in a well-known location it expects success.
func TestLocateSoffice_NoPATH(t *testing.T) {
	t.Setenv("LIBREOFFICE_PATH", "")
	t.Setenv("PATH", "")
	got, err := LocateSoffice()
	if err != nil {
		assert.ErrorIs(t, err, ErrSofficeNotFound)
	} else {
		assert.NotEmpty(t, got)
	}
}
