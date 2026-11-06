package engine

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpectedParticipantsCapMatchesTheBrowser(t *testing.T) {
	data, err := os.ReadFile("../../web-mobile/js/competition_put_body.jsx")
	require.NoError(t, err)

	re := regexp.MustCompile(`MAX_EXPECTED_PARTICIPANTS\s*=\s*(\d+)`)
	matches := re.FindSubmatch(data)
	require.NotNil(t, matches, "MAX_EXPECTED_PARTICIPANTS constant not found in competition_put_body.jsx")

	capVal, err := strconv.Atoi(string(matches[1]))
	require.NoError(t, err)

	require.Equal(t, MaxEstimateParticipants, capVal, "competition_put_body.jsx MAX_EXPECTED_PARTICIPANTS must match engine.MaxEstimateParticipants")
}
