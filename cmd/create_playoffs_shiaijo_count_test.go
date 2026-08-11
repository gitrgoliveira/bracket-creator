package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlayoffOptionsRun_ShiaijoCount sweeps --courts on create-playoffs across
// 1..17. The tree gives each shiaijo its own block and the blocks merge in
// pairs, so a power of two (1, 2, 4, 8 or 16) is accepted and everything else
// is refused before any file is written.
func TestPlayoffOptionsRun_ShiaijoCount(t *testing.T) {
	for n := 1; n <= 17; n++ {
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.csv")
			require.NoError(t, os.WriteFile(input,
				[]byte("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\n"), 0o600))
			output := filepath.Join(dir, "out.xlsx")

			o := &playoffOptions{
				filePath:   input,
				outputPath: output,
				determined: true,
				courts:     n,
			}
			err := o.run(nil, nil)
			if legalShiaijoCount(n) {
				assert.NoErrorf(t, err, "%d courts must be accepted", n)
				return
			}
			require.Errorf(t, err, "%d courts must be rejected", n)
			assert.Contains(t, err.Error(), "shiaijo count must be a power of two")
			assert.Contains(t, err.Error(), ", or 1",
				"the message must always offer a single shiaijo")

			// The check runs before the workbook is opened, so a rejected run
			// leaves no half-written output behind.
			_, statErr := os.Stat(output)
			assert.Truef(t, os.IsNotExist(statErr),
				"a rejected court count must not create %s", output)
		})
	}
}

// TestPlayoffOptionsRun_RejectsEvenNonPowerOfTwo is the regression for the rule
// change: --courts 6 passed the retired "1 or an even number" rule.
func TestPlayoffOptionsRun_RejectsEvenNonPowerOfTwo(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.csv")
	require.NoError(t, os.WriteFile(input,
		[]byte("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\n"), 0o600))

	o := &playoffOptions{
		filePath:   input,
		outputPath: filepath.Join(dir, "out.xlsx"),
		determined: true,
		courts:     6,
	}
	err := o.run(nil, nil)
	require.Error(t, err, "6 is even but not a power of two")
	assert.Contains(t, err.Error(), "use 4 or 8, or 1")
}

// TestPlayoffOptionsRun_CourtCapBeforeShiaijoCount pins the order of the two
// court checks: 27 breaks both the A-Z label cap and the shiaijo-count rule,
// and the cap is the one an operator needs to hear about first.
func TestPlayoffOptionsRun_CourtCapBeforeShiaijoCount(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.csv")
	require.NoError(t, os.WriteFile(input, []byte("John Doe,Dojo1\nJane Smith,Dojo2\n"), 0o600))

	o := &playoffOptions{
		filePath:   input,
		outputPath: filepath.Join(dir, "out.xlsx"),
		determined: true,
		courts:     27,
	}
	err := o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "courts must be <= 26")
	assert.NotContains(t, err.Error(), "power of two")
}

// TestCreateHandler_ShiaijoCount covers the web form that drives the same
// generator as the CLI flags: it must refuse an illegal court count too,
// otherwise the browser path is a hole in the rule.
func TestCreateHandler_ShiaijoCount(t *testing.T) {
	post := func(t *testing.T, courts string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{
			"tournamentType": {"pools"},
			"playerList":     {"A,D1\nB,D2\nC,D3\nD,D4\nE,D5\nF,D6\n"},
			"courts":         {courts},
			"winnersPerPool": {"2"},
			"playersPerPool": {"3"},
			"poolSizeMode":   {"min"},
			"teamMatches":    {"0"},
			"determined":     {"on"},
		}
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.POST("/create", createTournamentHandler)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/create", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("3 courts rejected", func(t *testing.T) {
		w := post(t, "3")
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "use 2 or 4, or 1")
	})

	t.Run("6 courts rejected", func(t *testing.T) {
		// Even but not a power of two: accepted by the retired rule.
		w := post(t, "6")
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "use 4 or 8, or 1")
	})

	t.Run("2 courts accepted", func(t *testing.T) {
		w := post(t, "2")
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})

	t.Run("1 court accepted", func(t *testing.T) {
		w := post(t, "1")
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})
}
