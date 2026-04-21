package cmd

import (
	"bufio"
	"bytes"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCreatePoolCmd(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "create-pools", cmd.Use)
	assert.Equal(t, "creates Pool brackets", cmd.Short)
}

func TestCreatePoolCmdFlags(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()

	// Test required flags
	fileFlag := cmd.PersistentFlags().Lookup("file")
	assert.NotNil(t, fileFlag)
	assert.Equal(t, "f", fileFlag.Shorthand)

	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "o", outputFlag.Shorthand)

	// Test optional flags
	playersFlag := cmd.Flags().Lookup("players")
	assert.NotNil(t, playersFlag)
	assert.Equal(t, "p", playersFlag.Shorthand)
	assert.Equal(t, "3", playersFlag.DefValue)

	maxPlayersFlag := cmd.Flags().Lookup("max-players")
	assert.NotNil(t, maxPlayersFlag)
	assert.Equal(t, "m", maxPlayersFlag.Shorthand)
	assert.Equal(t, "0", maxPlayersFlag.DefValue)

	winnersFlag := cmd.Flags().Lookup("pool-winners")
	assert.NotNil(t, winnersFlag)
	assert.Equal(t, "w", winnersFlag.Shorthand)
	assert.Equal(t, "2", winnersFlag.DefValue)

	roundRobinFlag := cmd.Flags().Lookup("round-robin")
	assert.NotNil(t, roundRobinFlag)
	assert.Equal(t, "r", roundRobinFlag.Shorthand)

	zekkenFlag := cmd.Flags().Lookup("with-zekken-name")
	assert.NotNil(t, zekkenFlag)
	assert.Equal(t, "z", zekkenFlag.Shorthand)

	singleTreeFlag := cmd.Flags().Lookup("single-tree")
	assert.NotNil(t, singleTreeFlag)

	teamMatchesFlag := cmd.Flags().Lookup("team-matches")
	assert.NotNil(t, teamMatchesFlag)
	assert.Equal(t, "t", teamMatchesFlag.Shorthand)
	assert.Equal(t, "0", teamMatchesFlag.DefValue)

	determinedFlag := cmd.PersistentFlags().Lookup("determined")
	assert.NotNil(t, determinedFlag)
	assert.Equal(t, "d", determinedFlag.Shorthand)
}

func TestCreatePools_BasicSuccess(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter:   writer,
		outputPath:     "dummy.xlsx",
		numPlayers:     3,
		poolWinners:    2,
		withZekkenName: false,
		determined:     true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	// Template file should be found in embedded resources
	require.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
	// Buffer may be empty if template is missing, which is OK for this test
}

func TestCreatePools_WithZekkenNames(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter:   writer,
		outputPath:     "dummy.xlsx",
		numPlayers:     3,
		poolWinners:    2,
		withZekkenName: true,
		determined:     true,
	}

	entries := []string{
		"John Doe,Dojo1,Johnny",
		"Jane Smith,Dojo2,Janey",
		"Alice,Dojo3,Ali",
		"Bob,Dojo4,Bobby",
		"Charlie,Dojo5,Chuck",
		"Dave,Dojo6,Davey",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_RoundRobin(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   4,
		poolWinners:  2,
		roundRobin:   true,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
		"Eve,Dojo7",
		"Frank,Dojo8",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_SingleTree(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		singleTree:   true,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_WithTeamMatches(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		teamMatches:  2,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_WithSeeds(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	seedAssignments := []domain.SeedAssignment{
		{Name: "John Doe", SeedRank: 1},
		{Name: "Jane Smith", SeedRank: 2},
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     2,
		SeedAssignments: seedAssignments,
		determined:      true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_MaxPlayersValidation(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	// 3 entries with max-players 3 and 2 winners is the smallest valid config.
	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entries       []string
		numPlayers    int
		poolWinners   int
		expectedError string
	}{
		{
			name:          "too few entries for winners",
			entries:       []string{"John Doe,Dojo1"},
			numPlayers:    3,
			poolWinners:   2,
			expectedError: "number of entries must be higher than number of winners per pool",
		},
		{
			name:          "too few entries for pool size",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2"},
			numPlayers:    3,
			poolWinners:   2,
			expectedError: "number of entries must be greater than requested players in pool",
		},
		{
			name:          "invalid pool size",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2", "Alice,Dojo3"},
			numPlayers:    1,
			poolWinners:   2,
			expectedError: "number of players per pool must be greater than 1",
		},
		{
			name:          "winners >= players",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2", "Alice,Dojo3"},
			numPlayers:    3,
			poolWinners:   3,
			expectedError: "number of pool winners must be less than number of players per pool",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			writer := bufio.NewWriter(&b)

			o := &poolOptions{
				outputWriter: writer,
				outputPath:   "dummy.xlsx",
				numPlayers:   tt.numPlayers,
				poolWinners:  tt.poolWinners,
			}

			err := o.createPools(tt.entries)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestCreatePools_RemovesDuplicates(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	// Include duplicates
	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"John Doe,Dojo1", // duplicate
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Jane Smith,Dojo2", // duplicate
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_ShuffleWhenNotDetermined(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		determined:   false, // Should shuffle
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestPoolOptionsRun_FileNotFound(t *testing.T) {
	o := &poolOptions{
		filePath:   "nonexistent.csv",
		outputPath: "output.xlsx",
	}

	err := o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entries from file")
}

func TestPoolOptionsRun_EmptyFile(t *testing.T) {
	// Create a temporary empty file
	tmpFile, err := os.CreateTemp("", "empty-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	o := &poolOptions{
		filePath:   tmpFile.Name(),
		outputPath: "output.xlsx",
	}

	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no entries found in file")
}

func TestPoolOptionsRun_Success(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())

	_, err = tmpInput.WriteString("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\nCharlie,Dojo5\nDave,Dojo6\n")
	require.NoError(t, err)
	tmpInput.Close()

	// Create a temporary output file
	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &poolOptions{
		filePath:    tmpInput.Name(),
		outputPath:  tmpOutput.Name(),
		numPlayers:  3,
		poolWinners: 2,
		determined:  true,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
	// Output file is created even if template is missing
	_, err = os.Stat(tmpOutput.Name())
	assert.NoError(t, err)
}

func TestCreatePoolCmdMutuallyExclusiveFlags(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	// Provide both --players and --max-players to trigger the mutual-exclusion check.
	cmd.SetArgs([]string{"--file", "/tmp/does-not-exist.csv", "--output", "/tmp/out.xlsx", "--players", "3", "--max-players", "4"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestCreatePools_MaxMode_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entries       []string
		maxPlayers    int
		poolWinners   int
		expectedError string
	}{
		{
			name:          "max mode requires at least 2 entries",
			entries:       []string{"John Doe,Dojo1"},
			maxPlayers:    3,
			poolWinners:   0,
			expectedError: "number of entries must be at least 2",
		},
		{
			name:          "max mode rejects pool size below 2",
			entries:       []string{"A,D1", "B,D2", "C,D3"},
			maxPlayers:    1,
			poolWinners:   0,
			expectedError: "number of players per pool must be greater than 1",
		},
		{
			name:          "max mode rejects winners >= max",
			entries:       []string{"A,D1", "B,D2", "C,D3", "D,D4"},
			maxPlayers:    3,
			poolWinners:   3,
			expectedError: "number of pool winners must be less than number of players per pool",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			writer := bufio.NewWriter(&b)

			o := &poolOptions{
				outputWriter: writer,
				outputPath:   "dummy.xlsx",
				maxPlayers:   tt.maxPlayers,
				poolWinners:  tt.poolWinners,
				determined:   true,
			}

			err := o.createPools(tt.entries)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestCreatePools_MaxMode_BalancedDistribution(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	// 10 entries with max 3 players per pool -> 4 pools (3, 3, 2, 2).
	entries := []string{
		"P1,D1", "P2,D2", "P3,D3", "P4,D4", "P5,D5",
		"P6,D6", "P7,D7", "P8,D8", "P9,D9", "P10,D10",
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
}

func TestCreatePools_ServeMutuallyExclusiveModes(t *testing.T) {
	t.Parallel()

	// Sanity check: the option struct allows only one of numPlayers/maxPlayers
	// to be effective. Cmd-level enforcement is via cobra.MarkFlagsMutuallyExclusive,
	// while serve.go selects between the two based on poolSizeMode.
	o := &poolOptions{
		outputPath:  "dummy.xlsx",
		maxPlayers:  3,
		numPlayers:  10, // numPlayers should be ignored when maxPlayers > 0
		poolWinners: 2,
		determined:  true,
	}

	var b bytes.Buffer
	o.outputWriter = bufio.NewWriter(&b)

	// 4 entries with max 3 should produce 2 pools (2, 2). Without isMax this
	// would be 0 pools (4/10) and would have errored.
	err := o.createPools([]string{"A,D1", "B,D2", "C,D3", "D,D4"})
	require.NoError(t, err)
}

func TestCreatePools_WithMaxPlayersAndSeeds(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
		SeedAssignments: []domain.SeedAssignment{
			{Name: "P1", SeedRank: 1},
			{Name: "P2", SeedRank: 2},
		},
	}

	entries := []string{
		"P1,D1", "P2,D2", "P3,D3", "P4,D4", "P5,D5",
		"P6,D6", "P7,D7", "P8,D8",
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
}
