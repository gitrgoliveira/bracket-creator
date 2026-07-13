// Package domain, glossary.go owns the canonical kendo-term dictionary
// used by the Preact <Term> tooltip component (web-mobile/js/glossary.jsx)
// AND by API error responses (handlers_*.go ResolveReasonHuman).
//
// The source of truth is specs/003-tournament-gap-closure/glossary.md.
// When entries are added or tooltip wording changes, update both this
// file AND the spec. A unit test (glossary_test.go) parses glossary.md
// and asserts the Go map covers every term in it, so a forgotten entry
// fails the build.
//
// The JS side reads the same data via web-mobile/js/glossary.js, which
// is generated from this file by `go generate ./internal/domain/...`
// (see the go:generate directive below). Running `make go/build`
// regenerates it; CI catches drift.
package domain

//go:generate go run ./internal/glossarygen

import (
	"regexp"
	"strings"
)

// Term is a single dictionary entry. ID is the lowercase romaji
// ("kiken", "ippon-shobu", "kachinuki-exhaustion"); it doubles as the
// map key and as the `name` prop of the JSX <Term> component. Kanji is
// the Japanese spelling (or empty for the few entries without one, e.g.
// "kachinuki-exhaustion"). Short is the one-word English gloss the
// /glossary page renders next to the term. Tooltip is the full
// volunteer-facing sentence (rendered inside the popover and the
// /glossary page). SeeAlso lists the IDs of cross-referenced terms; the
// JSX component uses it to render nested <Term> spans inside the
// tooltip so hovered cross-references reveal their own tooltips.
type Term struct {
	ID      string   `json:"id"`
	Kanji   string   `json:"kanji"`
	Short   string   `json:"short"`
	Tooltip string   `json:"tooltip"`
	SeeAlso []string `json:"seeAlso,omitempty"`
}

// Glossary is the full term map keyed by lowercase romaji ID. The
// entries here mirror specs/003-tournament-gap-closure/glossary.md
// verbatim, tooltips are copied as-is so the spec and code never
// drift. Don't paraphrase.
var Glossary = map[string]Term{
	// --- Tier 1: every operator sees these during scoring ---

	"hikiwake": {
		ID:      "hikiwake",
		Kanji:   "引き分け",
		Short:   "Draw",
		Tooltip: "The match ended tied, both competitors finished with equal points and no winner is declared. Common in pool matches; knockout matches go to overtime (encho) instead.",
		SeeAlso: []string{"encho"},
	},
	"encho": {
		ID:      "encho",
		Kanji:   "延長",
		Short:   "Overtime",
		Tooltip: "Overtime, when a knockout match is tied at the end of regulation, an extra period is played until someone scores. Scoring in Encho follows ippon-shobu rules: first to one point wins.",
		SeeAlso: []string{"ippon-shobu", "ippon"},
	},
	"kiken": {
		ID:      "kiken",
		Kanji:   "棄権",
		Short:   "Withdrawal",
		Tooltip: "The competitor withdraws. Two sub-types: Voluntary (FIK Art. 31), permanent disqualification from the competition; Injury (FIK Art. 30), can be reinstated if a doctor and shinpan permit it. The opponent wins 2–0 by default.",
	},
	"kiken-voluntary": {
		ID:      "kiken-voluntary",
		Kanji:   "棄権",
		Short:   "Voluntary withdrawal",
		Tooltip: "FIK Article 31, the competitor voluntarily withdraws. They forfeit this match (opponent wins 2–0) and are permanently barred from all later matches in this competition.",
	},
	"kiken-injury": {
		ID:      "kiken-injury",
		Kanji:   "棄権",
		Short:   "Injury withdrawal",
		Tooltip: "FIK Article 30, the competitor withdraws due to injury or illness. They forfeit this match (opponent wins 2–0) and are blocked from later matches, but can be reinstated if a doctor and shinpan-in approve.",
	},
	"fusenpai": {
		ID:      "fusenpai",
		Kanji:   "不戦敗",
		Short:   "No-show forfeit",
		Tooltip: "The competitor didn't appear when called to court. The opponent wins 2–0 by default. (Literal meaning: no-fight loss.)",
	},
	"fusensho": {
		ID:      "fusensho",
		Kanji:   "不戦勝",
		Short:   "Bye-win",
		Tooltip: "In a team match, when one team is short a player for a bout, the present opponent wins that bout 2–0 without fighting. (Literal meaning: no-fight win.)",
	},
	"daihyosen": {
		ID:      "daihyosen",
		Kanji:   "代表選",
		Short:   "Representative bout",
		Tooltip: "Representative tiebreaker bout for teams. When teams finish level, each side sends one player to fight a single-point bout (ippon-shobu). In a tied team pool or league it decides the order in the standings; in a tied team knockout match it decides the winner (who advances).",
		SeeAlso: []string{"ippon-shobu", "ippon"},
	},
	"hansoku": {
		ID:      "hansoku",
		Kanji:   "反則",
		Short:   "Penalty",
		Tooltip: "A foul. Two hansoku awarded to a competitor give the opponent one free point.",
	},
	"shiro": {
		ID:      "shiro",
		Kanji:   "白",
		Short:   "White",
		Tooltip: "The competitor wearing a white ribbon at the back. Always shown on the left in this app.",
	},
	"aka": {
		ID:      "aka",
		Kanji:   "赤",
		Short:   "Red",
		Tooltip: "The competitor wearing a red ribbon at the back. Always shown on the right in this app.",
	},
	"shiaijo": {
		ID:      "shiaijo",
		Kanji:   "試合場",
		Short:   "Court",
		Tooltip: "The marked floor area where matches are fought. Labelled A, B, C, etc.",
	},
	"ippon": {
		ID:      "ippon",
		Kanji:   "一本",
		Short:   "Point",
		Tooltip: "A valid point, a clean strike with correct form, posture, and spirit. First competitor to two ippon wins the match (or one in overtime).",
	},

	// --- Tier 2: team competitions and overtime contexts ---

	"ippon-shobu": {
		ID:      "ippon-shobu",
		Kanji:   "一本勝負",
		Short:   "Single-point match",
		Tooltip: "First competitor to score one ippon wins. Used in overtime (encho) and tiebreaker (daihyosen) bouts.",
		SeeAlso: []string{"ippon", "encho", "daihyosen"},
	},
	"kachinuki": {
		ID:      "kachinuki",
		Kanji:   "勝ち抜き",
		Short:   "Winner stays on",
		Tooltip: "A team format where the winner of each bout stays on to face the next opponent. A draw retires both. The match ends when one team has no more players left.",
	},
	"senpo": {
		ID:      "senpo",
		Kanji:   "先鋒",
		Short:   "First player",
		Tooltip: "The team's first player, fights bout 1 in a 5-person team. Senpo must be filled; this position cannot be left vacant, even when a teammate is withdrawn.",
	},
	"jiho": {
		ID:      "jiho",
		Kanji:   "次鋒",
		Short:   "Second player",
		Tooltip: "The team's second player, fights bout 2. If a team is short one player, FIK rules require that the Jiho position is the one left vacant.",
	},
	"chuken": {
		ID:      "chuken",
		Kanji:   "中堅",
		Short:   "Middle player",
		Tooltip: "The team's middle player, fights bout 3 in a 5-person team.",
	},
	"fukusho": {
		ID:      "fukusho",
		Kanji:   "副将",
		Short:   "Vice-captain",
		Tooltip: "The team's vice-captain, fights bout 4. If a team is short two players, FIK rules require jiho and Fukusho to be the two vacant positions.",
		SeeAlso: []string{"jiho"},
	},
	"taisho": {
		ID:      "taisho",
		Kanji:   "大将",
		Short:   "Captain",
		Tooltip: "The team's captain, fights the final bout (bout 5). Like senpo, Taisho must be filled; this position cannot be left vacant.",
		SeeAlso: []string{"senpo"},
	},

	// --- Tier 3: registration and setup (less critical for floor volunteers) ---

	"zekken": {
		ID:      "zekken",
		Kanji:   "ゼッケン",
		Short:   "Name tag (nafuda)",
		Tooltip: "The name tag affixed to the centre of the tare (waist protector). Also called nafuda. The display can show this name (instead of the registered name) so spectators see what's physically visible.",
	},
	"dan": {
		ID:      "dan",
		Kanji:   "段",
		Short:   "Rank",
		Tooltip: "A competitor's grade, shodan (1st), nidan (2nd), sandan (3rd), up to 8th. Optional in the entry form.",
	},
	"dojo": {
		ID:      "dojo",
		Kanji:   "道場",
		Short:   "Club or school",
		Tooltip: "The training school or club a competitor represents. The pool generator avoids placing two competitors from the same dojo in the same pool when it can.",
	},
	"waza": {
		ID:      "waza",
		Kanji:   "技",
		Short:   "Technique",
		Tooltip: "The specific type of strike scored, for example, men, kote, or do. Recorded with each ippon for the match record.",
		SeeAlso: []string{"ippon"},
	},
	"kachinuki-exhaustion": {
		ID:      "kachinuki-exhaustion",
		Kanji:   "",
		Short:   "Team exhausted",
		Tooltip: "A kachinuki match ended because one team has no remaining players to send. The opposing team wins.",
		SeeAlso: []string{"kachinuki"},
	},

	// --- Naginata ---

	"sune": {
		ID:      "sune",
		Kanji:   "すね",
		Short:   "Shin strike",
		Tooltip: "A strike to the shin. Scores a point in Naginata competitions. Not a valid scoring technique in Kendo.",
	},
}

// Lookup returns the Term for an ID (case-insensitive) and a found
// boolean. Centralised so callers don't need to remember the lowercase
// convention or import the raw map.
func Lookup(id string) (Term, bool) {
	t, ok := Glossary[strings.ToLower(strings.TrimSpace(id))]
	return t, ok
}

// reasonHumanPatterns maps the canonical engine-emitted reason
// fragments to humanised wording.
//
// The shape we map is the format `internal/engine` writes when it
// returns IneligibleCompetitorError / AlreadyIneligibleError. The
// reason field is a short phrase that names the canonical decision
// (kiken / fusenpai / fusensho / daihyosen / kachinuki-exhaustion)
// followed by `at <matchID>`. Volunteers reading the error in the UI
// shouldn't have to look up what "kiken at m_12" means, the human
// gloss is rendered alongside (the JSON shape now carries both
// `reason` and `reasonHuman`).
var reasonHumanPatterns = []struct {
	re      *regexp.Regexp
	rewrite func(matches []string) string
}{
	// "kiken at <matchID>" → "withdrew from match <matchID>"
	{
		re: regexp.MustCompile(`^kiken at (\S+)$`),
		rewrite: func(m []string) string {
			return "withdrew from match " + m[1]
		},
	},
	// "kiken-voluntary at <matchID>" → "withdrew voluntarily from match <matchID>"
	{
		re: regexp.MustCompile(`^kiken-voluntary at (\S+)$`),
		rewrite: func(m []string) string {
			return "withdrew voluntarily from match " + m[1]
		},
	},
	// "kiken-injury at <matchID>" → "withdrew due to injury from match <matchID>"
	{
		re: regexp.MustCompile(`^kiken-injury at (\S+)$`),
		rewrite: func(m []string) string {
			return "withdrew due to injury from match " + m[1]
		},
	},
	// "fusenpai at <matchID>" → "no-show forfeit at match <matchID>"
	{
		re: regexp.MustCompile(`^fusenpai at (\S+)$`),
		rewrite: func(m []string) string {
			return "no-show forfeit at match " + m[1]
		},
	},
	// "fusensho at <matchID>" → "bye-win at match <matchID>"
	{
		re: regexp.MustCompile(`^fusensho at (\S+)$`),
		rewrite: func(m []string) string {
			return "bye-win at match " + m[1]
		},
	},
	// "daihyosen at <matchID>" → "representative bout at match <matchID>"
	{
		re: regexp.MustCompile(`^daihyosen at (\S+)$`),
		rewrite: func(m []string) string {
			return "representative bout at match " + m[1]
		},
	},
	// "kachinuki-exhaustion at <matchID>" → "team exhausted at match <matchID>"
	{
		re: regexp.MustCompile(`^kachinuki-exhaustion at (\S+)$`),
		rewrite: func(m []string) string {
			return "team exhausted at match " + m[1]
		},
	},
}

// ResolveReasonHuman returns the human-friendly gloss for an
// engine-emitted reason string, or "" when no pattern matches.
//
// Callers wire this into error responses alongside the raw `reason`
// field so UIs can show "withdrew from match m_12" instead of "kiken
// at m_12". Returning "" rather than echoing the input lets the caller
// decide whether to omit the field or fall back to the raw reason,
// the JSON omitempty tag on ReasonHuman handles the typical case.
func ResolveReasonHuman(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	for _, p := range reasonHumanPatterns {
		if m := p.re.FindStringSubmatch(reason); m != nil {
			return p.rewrite(m)
		}
	}
	// Fallback: a bare kendo term (no "at <matchID>" suffix) is also
	// translated when we recognise it. Engine paths that signal "kiken"
	// without a match context (e.g. team-level decisions) benefit.
	if t, ok := Lookup(reason); ok && t.Short != "" {
		return strings.ToLower(t.Short)
	}
	return ""
}
