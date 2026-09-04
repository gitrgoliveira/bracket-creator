package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// TestThirdPlaceMatchFlag_Registration asserts --third-place-match is wired the
// same way every other bool flag on these two commands is: cmd.Flags(), no
// shorthand, default "false" (today's behaviour: no bronze block unless asked).
func TestThirdPlaceMatchFlag_Registration(t *testing.T) {
	t.Parallel()

	poolsFlag := newCreatePoolCmd().Flags().Lookup("third-place-match")
	require.NotNil(t, poolsFlag, "create-pools must register --third-place-match")
	assert.Equal(t, "false", poolsFlag.DefValue)
	assert.Empty(t, poolsFlag.Shorthand)

	playoffsFlag := newCreatePlayoffCmd().Flags().Lookup("third-place-match")
	require.NotNil(t, playoffsFlag, "create-playoffs must register --third-place-match")
	assert.Equal(t, "false", playoffsFlag.DefValue)
	assert.Empty(t, playoffsFlag.Shorthand)
}

// TestCreatePlayoffs_ThirdPlaceMatchFlag_ProducesBronzeBlock is the red-verify
// target for bc-3rdp's gap: --third-place-match on create-playoffs must add the
// "3rd Place" bronze block, and omitting it must not (today's default,
// preserved).
func TestCreatePlayoffs_ThirdPlaceMatchFlag_ProducesBronzeBlock(t *testing.T) {
	t.Parallel()

	const roster = "Alice,Dojo1\nBob,Dojo2\nCharlie,Dojo3\nDave,Dojo4\n"

	runAndFindThirdPlaceRow := func(t *testing.T, extraArgs ...string) int {
		t.Helper()
		dir := t.TempDir()
		input := filepath.Join(dir, "input.csv")
		require.NoError(t, os.WriteFile(input, []byte(roster), 0o600))
		output := filepath.Join(dir, "out.xlsx")

		cmd := newCreatePlayoffCmd()
		args := append([]string{"--file", input, "--output", output, "--determined", "--courts", "1"}, extraArgs...)
		cmd.SetArgs(args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		require.NoError(t, cmd.Execute())

		f, err := excelize.OpenFile(output)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		rows, err := f.GetRows(helper.SheetEliminationMatches)
		require.NoError(t, err)
		return bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	}

	t.Run("with flag: bronze block present", func(t *testing.T) {
		t.Parallel()
		row := runAndFindThirdPlaceRow(t, "--third-place-match")
		assert.GreaterOrEqual(t, row, 0, "--third-place-match must produce a '3rd Place' block")
	})

	t.Run("without flag: no bronze block (default preserved)", func(t *testing.T) {
		t.Parallel()
		row := runAndFindThirdPlaceRow(t)
		assert.Equal(t, -1, row, "omitting --third-place-match must not produce a '3rd Place' block")
	})
}

// TestCreatePools_ThirdPlaceMatchFlag_ProducesBronzeBlock mirrors the playoffs
// case above for create-pools.
func TestCreatePools_ThirdPlaceMatchFlag_ProducesBronzeBlock(t *testing.T) {
	t.Parallel()

	const roster = "Alice,Dojo1\nBob,Dojo2\nCharlie,Dojo3\nDave,Dojo4\nEve,Dojo5\nFrank,Dojo6\nGrace,Dojo7\nHeidi,Dojo8\n"

	runAndFindThirdPlaceRow := func(t *testing.T, extraArgs ...string) int {
		t.Helper()
		dir := t.TempDir()
		input := filepath.Join(dir, "input.csv")
		require.NoError(t, os.WriteFile(input, []byte(roster), 0o600))
		output := filepath.Join(dir, "out.xlsx")

		cmd := newCreatePoolCmd()
		args := append([]string{
			"--file", input, "--output", output, "--determined",
			"--courts", "1", "--players", "4", "--pool-winners", "2",
		}, extraArgs...)
		cmd.SetArgs(args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		require.NoError(t, cmd.Execute())

		f, err := excelize.OpenFile(output)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		rows, err := f.GetRows(helper.SheetEliminationMatches)
		require.NoError(t, err)
		return bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	}

	t.Run("with flag: bronze block present", func(t *testing.T) {
		t.Parallel()
		row := runAndFindThirdPlaceRow(t, "--third-place-match")
		assert.GreaterOrEqual(t, row, 0, "--third-place-match must produce a '3rd Place' block")
	})

	t.Run("without flag: no bronze block (default preserved)", func(t *testing.T) {
		t.Parallel()
		row := runAndFindThirdPlaceRow(t)
		assert.Equal(t, -1, row, "omitting --third-place-match must not produce a '3rd Place' block")
	})
}
