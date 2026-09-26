// match_shows_score.jsx: does a display show this match's recorded score?
//
// A match sent back to the queue KEEPS its score on the server (points,
// penalties, overtime, team bouts, engi flags; operator ruling 2026-09-26,
// bc-sbq), but while it waits in the queue every screen shows it as NOT
// STARTED: a plain "vs", empty slots, no marks. The kept score reappears once
// the match is started again. Every display surface asks this before it draws
// a score, a penalty mark, a middle mark or an IV/PW aggregate; the shared
// scoreboard components (IndividualScore, TeamScoreboard) ask it themselves,
// so every host inherits the rule. The score EDITORS are the one exception
// and never ask it: an editor opened on a queued match shows the kept marks,
// because that is where the operator removes a wrong one.
//
// A leaf with no imports, so every surface ES-imports it directly and gets the
// same answer; reading it off `window` behind a guard gave the sites
// different defaults when bracket.jsx had not loaded.
export function matchShowsScore(m) {
  return !!m && (m.status === "running" || m.status === "completed");
}
