// AUTO-GENERATED — do not edit by hand.
// Source: internal/domain/glossary.go.
// Regenerate via `go generate ./internal/domain/...` or `make go/build`.
//
// The exported GLOSSARY map is consumed by web-mobile/js/glossary.jsx
// (the <Term> tooltip component and /glossary page).

export const GLOSSARY = {
  "aka": {"id":"aka","kanji":"赤","short":"Red","tooltip":"The competitor wearing a red ribbon at the back. Always shown on the right in this app."},
  "chuken": {"id":"chuken","kanji":"中堅","short":"Middle player","tooltip":"The team's middle player — fights bout 3 in a 5-person team."},
  "daihyosen": {"id":"daihyosen","kanji":"代表選","short":"Representative bout","tooltip":"Tiebreaker for team knockout matches. When two teams finish equal on both individual wins and points, each picks one player to fight a single-point bout (ippon-shobu) to decide the match.","seeAlso":["ippon-shobu","ippon"]},
  "dan": {"id":"dan","kanji":"段","short":"Rank","tooltip":"A competitor's grade — shodan (1st), nidan (2nd), sandan (3rd), up to 8th. Optional in the entry form."},
  "encho": {"id":"encho","kanji":"延長","short":"Overtime","tooltip":"Overtime — when a knockout match is tied at the end of regulation, an extra period is played until someone scores. Scoring in Encho follows ippon-shobu rules: first to one point wins.","seeAlso":["ippon-shobu","ippon"]},
  "fukusho": {"id":"fukusho","kanji":"副将","short":"Vice-captain","tooltip":"The team's vice-captain — fights bout 4. If a team is short two players, FIK rules require jiho and Fukusho to be the two vacant positions.","seeAlso":["jiho"]},
  "fusenpai": {"id":"fusenpai","kanji":"不戦敗","short":"No-show forfeit","tooltip":"The competitor didn't appear when called to court. The opponent wins 2–0 by default. (Literal meaning: no-fight loss.)"},
  "fusensho": {"id":"fusensho","kanji":"不戦勝","short":"Bye-win","tooltip":"In a team match, when one team is short a player for a bout, the present opponent wins that bout 2–0 without fighting. (Literal meaning: no-fight win.)"},
  "hansoku": {"id":"hansoku","kanji":"反則","short":"Penalty","tooltip":"A foul. Two hansoku awarded to a competitor give the opponent one free point."},
  "hikiwake": {"id":"hikiwake","kanji":"引き分け","short":"Draw","tooltip":"The match ended tied — both competitors finished with equal points and no winner is declared. Common in pool matches; knockout matches go to overtime (encho) instead.","seeAlso":["encho"]},
  "ippon": {"id":"ippon","kanji":"一本","short":"Point","tooltip":"A valid point — a clean strike with correct form, posture, and spirit. First competitor to two ippon wins the match (or one in overtime)."},
  "ippon-shobu": {"id":"ippon-shobu","kanji":"一本勝負","short":"Single-point match","tooltip":"First competitor to score one ippon wins. Used in overtime (encho) and tiebreaker (daihyosen) bouts.","seeAlso":["ippon","encho","daihyosen"]},
  "jiho": {"id":"jiho","kanji":"次鋒","short":"Second player","tooltip":"The team's second player — fights bout 2. If a team is short one player, FIK rules require that the Jiho position is the one left vacant."},
  "kachinuki": {"id":"kachinuki","kanji":"勝ち抜き","short":"Winner-stays-on","tooltip":"A team format where the winner of each bout stays on to face the next opponent. A draw retires both. The match ends when one team has no more players left."},
  "kachinuki-exhaustion": {"id":"kachinuki-exhaustion","kanji":"","short":"Team exhausted","tooltip":"A kachinuki match ended because one team has no remaining players to send. The opposing team wins.","seeAlso":["kachinuki"]},
  "kiken": {"id":"kiken","kanji":"棄権","short":"Withdrawal","tooltip":"The competitor withdraws. Two sub-types: Voluntary (FIK Art. 31) — permanent disqualification from the competition; Injury (FIK Art. 30) — can be reinstated if a doctor and shinpan permit it. The opponent wins 2–0 by default."},
  "kiken-injury": {"id":"kiken-injury","kanji":"棄権","short":"Injury withdrawal","tooltip":"FIK Article 30 — the competitor withdraws due to injury or illness. They forfeit this match (opponent wins 2–0) and are blocked from later matches, but can be reinstated if a doctor and shinpan-in approve."},
  "kiken-voluntary": {"id":"kiken-voluntary","kanji":"棄権","short":"Voluntary withdrawal","tooltip":"FIK Article 31 — the competitor voluntarily withdraws. They forfeit this match (opponent wins 2–0) and are permanently barred from all later matches in this competition."},
  "senpo": {"id":"senpo","kanji":"先鋒","short":"First player","tooltip":"The team's first player — fights bout 1 in a 5-person team. Senpo must be filled; this position cannot be left vacant, even when a teammate is withdrawn."},
  "shiaijo": {"id":"shiaijo","kanji":"試合場","short":"Court","tooltip":"The marked floor area where matches are fought. Labelled A, B, C, etc."},
  "shiro": {"id":"shiro","kanji":"白","short":"White","tooltip":"The competitor wearing a white ribbon at the back. Always shown on the left in this app."},
  "sune": {"id":"sune","kanji":"すね","short":"Shin strike","tooltip":"A strike to the shin. Scores a point in Naginata competitions. Not a valid scoring technique in Kendo."},
  "taisho": {"id":"taisho","kanji":"大将","short":"Captain","tooltip":"The team's captain — fights the final bout (bout 5). Like senpo, Taisho must be filled; this position cannot be left vacant.","seeAlso":["senpo"]},
  "waza": {"id":"waza","kanji":"技","short":"Technique","tooltip":"The specific type of strike scored — for example, men, kote, or do. Recorded with each ippon for the match record.","seeAlso":["ippon"]},
  "zekken": {"id":"zekken","kanji":"ゼッケン","short":"Name tag (nafuda)","tooltip":"The name tag affixed to the centre of the tare (waist protector). Also called nafuda. The display can show this name (instead of the registered name) so spectators see what's physically visible."}
};

// Convenience lookup so callers can normalise case at the call site
// without re-implementing the lowercase convention everywhere.
export function lookupTerm(id) {
  if (typeof id !== 'string') return null;
  return GLOSSARY[id.trim().toLowerCase()] || null;
}

if (typeof window !== 'undefined') {
  window.GLOSSARY = GLOSSARY;
  window.lookupTerm = lookupTerm;
}
