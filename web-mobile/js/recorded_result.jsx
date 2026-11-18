// recorded_result.jsx: the browser's copy of the server's result gate.
// Pinned to internal/engine/testdata/recorded_result_cases.json, which the Go
// test (internal/engine/recorded_result_test.go) and
// __tests__/recorded_result_table.test.jsx both read.
// Go owner: recordedResultIn (internal/engine/competition.go), over
// state.MatchResult.ResultRecorded and state.BracketMatch.ResultRecorded.
//
// A competition has a recorded result when any pool match is running or
// completed, or any knockout match (a round's or the bronze match) is running
// or completed with both sides named. A knockout match with an empty side is a
// bye the draw resolved when it was generated: it was never fought.
//
// The matches are read as the page holds them, which is the shape
// normalizeCompetitionDetail gives `poolMatches` and `bracket`: each side an
// {id, name} object, an empty slot being {id: "", name: ""}.

const RECORDED_STATUSES = new Set(["running", "completed"]);

function statusRecorded(m) {
    return !!m && RECORDED_STATUSES.has(m.status);
}

function sideNamed(side) {
    return !!side && !!side.name;
}

function bracketMatchRecorded(m) {
    return statusRecorded(m) && sideNamed(m.sideA) && sideNamed(m.sideB);
}

export function competitionHasRecordedResult(poolMatches, bracket) {
    if ((poolMatches || []).some(statusRecorded)) return true;
    if (!bracket) return false;
    if ((bracket.rounds || []).some(round => (round || []).some(bracketMatchRecorded))) return true;
    return bracketMatchRecorded(bracket.thirdPlaceMatch);
}
