// Package pdf converts bracket XLSX workbooks into grouped, print-ready PDFs.
//
// The pipeline mirrors square_prep/lc2026/xlsx_to_pdf.py: each XLSX is rendered
// to a full PDF by LibreOffice (soffice) headless, the per-sheet bookmarks
// LibreOffice emits are read to map sheet names to page ranges, and the wanted
// sheets are extracted and merged into grouped output PDFs.
//
// LibreOffice (soffice) is a runtime dependency. It is NOT bundled. Callers
// detect availability with LocateSoffice and surface an actionable error when
// it is absent rather than failing deep in the pipeline.
package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ErrSofficeNotFound is returned when no LibreOffice binary can be located.
// Callers map this to an actionable user-facing message (HTTP 503 in the web
// app) telling the operator to install LibreOffice or pull the -pdf image.
var ErrSofficeNotFound = errors.New("LibreOffice (soffice) not found")

// convertTimeout bounds a single soffice invocation. The Python reference uses
// 120s for XLSX conversion; we apply the same ceiling to every soffice call.
const convertTimeout = 120 * time.Second

// sofficeMu serializes all soffice invocations process-wide. LibreOffice
// headless shares a single user profile directory and is not safe to run
// concurrently; serializing also bounds the CPU/memory cost of conversion,
// which is the documented intent (admin-only, queued exports).
var sofficeMu sync.Mutex

// Converter runs soffice headless conversions. The zero value is not usable;
// construct with NewConverter.
type Converter struct {
	// sofficePath is the resolved absolute path to the soffice binary.
	sofficePath string
}

// NewConverter locates LibreOffice and returns a Converter. It returns
// ErrSofficeNotFound (wrapped) when no binary is available, so callers can
// branch on errors.Is(err, ErrSofficeNotFound).
func NewConverter() (*Converter, error) {
	path, err := LocateSoffice()
	if err != nil {
		return nil, err
	}
	return &Converter{sofficePath: path}, nil
}

// SofficePath returns the resolved soffice binary path.
func (c *Converter) SofficePath() string { return c.sofficePath }

// sofficeCandidates returns the ordered list of locations to probe for the
// soffice binary, after $LIBREOFFICE_PATH and a plain $PATH lookup.
func sofficeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/LibreOffice.app/Contents/MacOS/soffice",
			"/opt/homebrew/bin/soffice",
			"/usr/local/bin/soffice",
			"/usr/bin/soffice",
		}
	default:
		return []string{
			"/usr/bin/soffice",
			"/usr/local/bin/soffice",
			"/opt/libreoffice/program/soffice",
		}
	}
}

// LocateSoffice resolves the LibreOffice binary using, in order:
//  1. $LIBREOFFICE_PATH (if set and executable)
//  2. "soffice" on $PATH
//  3. a platform-specific list of well-known install locations
//
// It returns ErrSofficeNotFound (wrapped) when none resolve.
func LocateSoffice() (string, error) {
	if p := os.Getenv("LIBREOFFICE_PATH"); p != "" {
		if isExecutableFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("soffice"); err == nil {
		return p, nil
	}
	for _, cand := range sofficeCandidates() {
		if isExecutableFile(cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%w: set $LIBREOFFICE_PATH, add soffice to PATH, or install LibreOffice (brew install --cask libreoffice)", ErrSofficeNotFound)
}

// isExecutableFile reports whether path is an existing regular file with at
// least one executable bit set.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path) // #nosec G304 G703 -- path is from a fixed candidate list or $LIBREOFFICE_PATH/$PATH lookup, not request data.
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// ConvertToPDF renders srcPath (an XLSX or HTML file) to a PDF in outDir using
// soffice headless, and returns the resulting PDF path. Calls are serialized
// process-wide and bounded by convertTimeout. A dedicated, isolated user
// profile is passed via -env:UserInstallation so a concurrent desktop
// LibreOffice (or another server) does not collide on the shared profile.
func (c *Converter) ConvertToPDF(ctx context.Context, srcPath, outDir string) (string, error) {
	sofficeMu.Lock()
	defer sofficeMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()

	// Isolated, ephemeral profile so concurrent/host LibreOffice instances
	// don't fight over ~/.config/libreoffice.
	profileDir, err := os.MkdirTemp("", "lo-profile-*")
	if err != nil {
		return "", fmt.Errorf("create soffice profile dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(profileDir) }()

	// #nosec G204 -- c.sofficePath is resolved by LocateSoffice (fixed candidates / env), and srcPath/outDir are internally-generated temp paths, not request data.
	cmd := exec.CommandContext(ctx, c.sofficePath,
		"--headless",
		"--nologo",
		"--nofirststartwizard",
		fmt.Sprintf("-env:UserInstallation=file://%s", profileDir),
		"--convert-to", "pdf",
		srcPath,
		"--outdir", outDir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("soffice timed out after %s converting %s", convertTimeout, filepath.Base(srcPath))
		}
		return "", fmt.Errorf("soffice convert %s: %w (output: %s)", filepath.Base(srcPath), err, string(out))
	}

	// LibreOffice names the output <stem>.pdf in outDir.
	stem := stemWithoutExt(filepath.Base(srcPath))
	pdfPath := filepath.Join(outDir, stem+".pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		return "", fmt.Errorf("soffice produced no PDF for %s (expected %s; output: %s)", filepath.Base(srcPath), pdfPath, string(out))
	}
	return pdfPath, nil
}

// stemWithoutExt returns the filename with its final extension removed.
func stemWithoutExt(name string) string {
	ext := filepath.Ext(name)
	return name[:len(name)-len(ext)]
}
