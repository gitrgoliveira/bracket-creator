// Shared bracket rendering with SVG connector overlay.
// Connectors are drawn after layout via an effect that measures actual match
// card positions, so they always line up correctly regardless of card height.

const { useRef, useLayoutEffect: useLayoutEffectBC, useState: useStateBC, useEffect: useEffectBC } = React;

import { DAIHYOSEN_POSITION } from './pool_ids.jsx';

// TermBC: kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and this module doesn't matter.
function TermBC(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Local hikiwake check: bracket.jsx is tested in isolation, so we don't rely
// on window.isHikiwake here. See specs/openapi.yaml.
function isHikiwakeBC(v) { return v === "hikiwake"; }
function isKikenDecisionBC(v) { return v === "kiken" || v === "kiken-voluntary" || v === "kiken-injury"; }

// roundLabelFromEnd: the ONE mapping from "rounds still to come after this one"
// to a round NAME. 0 = Final, 1 = Semifinals, 2 = Quarterfinals, then the
// abbreviated bracket-size form. Every round name on every surface bottoms out
// here; nothing else may spell these strings.
function roundLabelFromEnd(fromEnd) {
  if (fromEnd === 0) return "Final";
  if (fromEnd === 1) return "Semifinals";
  if (fromEnd === 2) return "Quarterfinals";
  // mp-13y #8: abbreviated column header: R{N} where N is the bracket size
  // = 2^(fromEnd+1). Keeps column labels tight for wide brackets (R32, R128).
  return `R${2 ** (fromEnd + 1)}`;
}

function roundLabel(roundIdx, total) {
  return roundLabelFromEnd(total - 1 - roundIdx);
}

// bracketRoundLabel: THE single source of truth for what round a bracket MATCH
// belongs to, on every surface (bracket columns, viewer rows, admin score
// editor, watchlist, TV/display boards).
//
// mp-7f2w gave every real match an EFFECTIVE round (`displayRound`, counted from
// the final: 1 = Final, 2 = Semifinals, …) computed by walking the real feeder
// graph, so a structural bye COLLAPSES a round rather than showing an empty
// card. In a non-power-of-two draw that effective round can be nearer the final
// than the match's raw position in `bracket.rounds`, and the two disagree:
//
//   5-player knockout, backend rounds = 3
//     m-r1-0 (Alice v Bob)  backend round 0 → "Quarterfinals"
//                           displayRound 2  → "Semifinals"   ← its winner
//                                                              goes straight
//                                                              to the final
//
// The effective round is the true one: a match whose winner plays the final IS a
// semifinal, whatever array slot it occupies. So prefer `displayRound` and fall
// back to the raw position only for legacy brackets generated before the field
// existed (and for the pre-meta rendering path), where the two coincide anyway.
//
// Do NOT re-derive a round name from a raw round index at a call site: that is
// exactly the duplication that let the Overview rows say "Quarterfinals" while
// the bracket column above the same match said "Semifinals" (mp-u37s).
function bracketRoundLabel(m, roundIdx, total) {
  const dr = m && m.displayRound;
  if (typeof dr === "number" && dr > 0) return roundLabelFromEnd(dr - 1);
  return roundLabel(roundIdx, total);
}

// FEEDER_SLOT_RE reads (never writes) the engine's unresolved-feeder slot
// value, "Winner of r<depth>-m<index>" — the wire shape produced by
// engine.winnerOfPlaceholder and matched by helper.reservedWinnerOfRE, by
// admin_helpers.BRACKET_PLACEHOLDER_RE and by display_helpers'
// DISPLAY_PLACEHOLDER_RE. Those filters decide whether a match is
// playable/schedulable/announceable and they keep operating on the RAW value:
// nothing here rewrites a side, so a slot relabelled for humans is still the
// same unplayable placeholder to every predicate. The two capture groups are
// the only addition: they let a DISPLAY surface say which match will fill the
// slot. Do not use this regex as a filter — use the predicates named above (they live in admin_helpers.jsx / display_helpers.jsx / Go, not here).
const FEEDER_SLOT_RE = /^Winner of r(\d+)-m(\d+)$/;

// slotDisplayName: THE single mapping from a bracket SLOT VALUE to the text a
// human reads in that slot. Every surface that puts a bracket side in front of
// a person — public bracket, admin bracket, match details, the shiaijo "Later"
// queue — goes through this; no surface may print a raw slot value.
//
// "r3-m3" is a backend round/index pair, meaningless to a spectator AND not the
// number the bracket prints on its cards ("M1", "M2", … from matchNumById /
// state.BracketMatch.MatchNumber, which are ordered by effective round, not by
// the id). Printing it therefore pointed readers at the wrong card. So:
//   feeder resolved to a numbered match → "Winner of M<n>", which names a card
//                                         visible on the same screen;
//   feeder not resolvable (legacy bracket with no numbering, or a hidden
//   phantom feeder)                     → "TBD", the neutral label the empty
//                                         side already uses. Never the raw id.
// Anything that is NOT a feeder slot is returned untouched — in particular the
// pool-origin placeholders ("Pool A-1st", "Pool B-2nd"): those are already
// self-describing to a spectator (they name a pool and a finishing position, no
// internal index), so relabelling them would lose real information.
function slotDisplayName(name, feederMatchNum) {
  if (typeof name !== "string" || !FEEDER_SLOT_RE.test(name)) return name;
  return feederMatchNum > 0 ? `Winner of M${feederMatchNum}` : "TBD";
}

// makeSlotLabeller binds slotDisplayName to ONE bracket: it returns
// (slotValue, feederId) => display text, resolving each feeder slot to the
// match that will fill it.
//
// Two resolution paths, in order:
//  1. `feederId` — the caller's own state.BracketMatch.Feeders entry for that
//     side (mp-7f2w, [A, B] order). PREFERRED: the engine walked the REAL
//     feeder graph to build it, so it sees through phantom bye matches. In a
//     5-entrant draw the final's slot literally reads "Winner of r2-m0", but
//     r2-m0 is a dead bye match — the winner actually arrives from m-r1-0.
//  2. Positional parse of the slot itself, mirroring Go parseWinnerOf
//     (scoring.go): depth is 1-based from the final, so roundIndex =
//     rounds.length - depth and the index is the 0-based position in that
//     round. The fallback for brackets saved before the feeder metadata
//     existed, where there are no phantoms to see through anyway.
// Either way the NUMBER comes from matchNumById when the caller has a display
// model: the exact map that stamps "M<n>" on the cards, so the reference always
// resolves against something on screen. On a numbered bracket that map holds the
// server's match.matchNumber verbatim (buildDisplayModel consumes it instead of
// re-deriving it), so the raw-field fallback below is not a second numbering to
// keep in step — it serves callers that pass no model at all, e.g.
// makeSlotLabeller(rounds, null). An unresolvable feeder yields 0 and
// slotDisplayName degrades it to "TBD".
function makeSlotLabeller(rounds, matchNumById) {
  const rs = Array.isArray(rounds) ? rounds : [];
  const numOf = (m) => (m ? ((matchNumById && matchNumById[m.id]) || m.matchNumber || 0) : 0);
  const byId = (id) => {
    for (const round of rs) {
      const hit = (round || []).find((m) => m && m.id === id);
      if (hit) return hit;
    }
    return null;
  };
  return (name, feederId) => {
    const mm = typeof name === "string" ? FEEDER_SLOT_RE.exec(name) : null;
    if (!mm) return name;
    let num = feederId ? numOf(byId(feederId)) : 0;
    if (!num) num = numOf((rs[rs.length - Number(mm[1])] || [])[Number(mm[2])]);
    return slotDisplayName(name, num);
  };
}

// bracketSlotLabeller: makeSlotLabeller for callers that hold only the rounds
// (the match-details modal, the shiaijo queue). It derives the SAME numbering
// the tree draws by going through buildDisplayModel, so a slot's label and the
// card it points at can never disagree. BracketTree builds its labeller from
// the model it already computed instead of calling this (same result, one pass).
function bracketSlotLabeller(rounds) {
  return makeSlotLabeller(rounds, buildDisplayModel(rounds).matchNumById);
}

// sideA = top = Aka (Red), sideB = bottom = Shiro (White)
function sideLabel(side) {
  return side === "a" ? "AKA" : "SHIRO";
}

// enchoLabel renders the overtime marker for a match's encho block: "" when no
// overtime ran, "(E)" otherwise — always bare, never a count. mp-m4bn: the
// stepper records how many periods were fought (periodCount persists for the
// tournament log), but the result marking deliberately never carries the
// number: counted markers ("(E×3)") confuse readers of brackets and result
// sheets. Do not reintroduce the count here. Mirrors enchoLabel() in
// internal/export/suffix.go, pinned by the shared table in
// internal/export/testdata/encho_labels.json. The score editors' "· (E)
// Overtime ×N" eyebrow is different on purpose: a live readout of the stepper
// the operator is using, not a result marking.
function enchoLabel(encho) {
  return enchoOn(encho) ? "(E)" : "";
}

// enchoOn: THE single predicate for "did this result happen in encho" —
// a non-degenerate block with a positive periodCount. The (E) label and
// the default-win maru count both key on it, so a stray
// {periodCount: 0} block can never make one surface claim overtime
// while another denies it. Mirrors state.EnchoMetadata.On (Go).
const enchoOn = (encho) => (encho?.periodCount || 0) > 0;

// middleMark: the ONE mark the centre of a score may carry. The middle column
// of a score sheet can only ever read:
//   vs     not yet decided (callers render the plain "vs" middle)
//   X      a tie (hikiwake)
//   (E)    the match went to overtime
//   (DH)   a team encounter sent to a representative bout
// Mutually exclusive by rule: X means a tie and a match that went to encho
// cannot end tied (encho runs until someone scores), so X beats (E); and a
// daihyosen bout is one-point sudden death, so DH bouts do not have encho and
// (DH) beats (E). Everything else — Kiken, Fus., Ht — is a RESULT and belongs
// beside the competitor it names: see sideMarks. Mirrors MiddleMark in
// internal/export/suffix.go.
function middleMark(decision, encho) {
  if (isHikiwakeBC(decision)) return "X";
  if (decision === "daihyosen") return "(DH)";
  return enchoLabel(encho);
}

// joinSp: join a score fragment and a result mark with a space, skipping
// empties ("M" + "Ht" → "M Ht", "" + "Kiken" → "Kiken"). The JS twin of
// joinSp in internal/export/suffix.go.
const joinSp = (a, b) => [a, b].filter(Boolean).join(" ");

// placeMarks: resolve sideMarks onto the two display slots — the winner's
// mark rides the winning side, the loser's the other. When neither slot is
// known to have won, no marks are placed; each caller owns that fallback
// (score strings trail the marks, match cards drop them). The JS analogue
// of the winner-resolution half of SideMarksLR in internal/export/suffix.go.
const placeMarks = (marks, firstWins, secondWins) =>
  firstWins ? [marks.winner, marks.loser] : secondWins ? [marks.loser, marks.winner] : ["", ""];

// isDrawResult: a result is a draw when the recorded decision OR the
// client-derived score.type says hikiwake (quick-score paths set only
// score.type, so both sources count).
const isDrawResult = (decision, score) => isHikiwakeBC(decision) || isHikiwakeBC(score?.type);

// isDefaultWinBC: the decisions that award the match points without a
// technique. Mirrors domain.IsDefaultWinDecisionStr (Go).
const isDefaultWinBC = (d) => isKikenDecisionBC(d) || d === "fusenpai" || d === "fusensho";

// defaultWinMaru: the maru cells a default win awards — one "○" per point,
// per the FIK Regulations (Article 32 and the Score Board appendix p.15:
// "put one mark in case of Encho"): the two-point pair in regulation, the
// single deciding point in encho (sudden death). THE single JS source of
// the maru-count rule; mirrors domain.DefaultWinIppons (Go, same cells
// shape). The canonical record is the engine's RecordDecision fill via
// domain.DefaultWinIppons — displays only fall back to this for winners
// whose recorded cells are empty (byes, legacy data).
const defaultWinMaru = (encho) => (enchoOn(encho) ? ["○"] : ["○", "○"]);

// boutMiddle: THE single source for what a bout's middle can read —
// "vs" (plain, including unplayed/pending), "X" (tie), "(E)" (overtime),
// "(DH)" (rep bout). Nothing else is a valid middle value: a dash never is
// (operator ruling), and Ht/Kiken/Fus. are side results, never middles.
// SCOPE: this binds the surfaces that PROJECT A RESULT — viewer, display,
// scoreboard, match cards — and every one of them derives its
// middle from here. It does NOT bind the score editors' live-ENTRY
// separators (the plain "VS" in the individual/team encounter headers, the
// engi divider): those sit in the input zone and project no result, so they
// are exempt by mp-42g and carry their own note at each site. The team
// editor's per-BOUT rows do project a result, and go through
// renderTeamBoutMiddle → boutMiddle like every other bound surface.
function boutMiddle(decision, encho, score) {
  return (isDrawResult(decision, score) ? "X" : middleMark(decision, encho)) || "vs";
}

// matchMiddleMark: the SPECIAL middle marks only ("" when the middle is the
// plain "vs") — for surfaces that render the mark as a single centre chip
// (MatchCard meta strip, TV scoreboard header, lobby, OBS lower-third).
function matchMiddleMark(match) {
  if (!match) return "";
  const mid = boutMiddle(match.decision, match.encho, match.score);
  return mid === "vs" ? "" : mid;
}

// sideMarks: the per-side RESULT marks. winner goes in the winning side's
// score cell, loser in the losing side's — the mark names its competitor:
//   hantei   → winner "Ht"    (FIK 7-5 / 29-6: judges picked the winner)
//   kiken    → loser  "Kiken" (the competitor who withdrew)
//   fusenpai → loser  "Fus."  (the no-show)
// `fusensho` (the per-bout default WIN) is deliberately absent here: the
// viewer surfaces it via a separate bout badge. The Excel export has no
// badges, so its SideMarks (internal/export/suffix.go) folds fusensho in as
// a winner-side "Fus." — the one deliberate divergence between the mirrors.
function sideMarks(decision, decidedByHantei) {
  let winner = "", loser = "";
  if (isKikenDecisionBC(decision)) loser = "Kiken";
  else if (decision === "fusenpai") loser = "Fus.";
  if (decidedByHantei) winner = "Ht"; // nothing above sets winner (fusensho is a badge here)
  return { winner, loser };
}

// winnerSideLR: which DISPLAY side won, under the SHIRO-left convention every
// score string uses (sideB = Shiro = left, sideA = Aka = right). Returns
// "left" | "right" | null (no winner recorded, or drifted data). Accepts both
// object sides ({id, name}) and bare name strings.
function winnerSideLR(m) {
  if (!m || !m.winner) return null;
  const idOf = s => (s && typeof s === "object" ? s.id : null);
  const nameOf = s => (s && typeof s === "object" ? s.name : s);
  const wId = idOf(m.winner);
  const wName = nameOf(m.winner);
  // Prefer id equality: two different-dojo competitors may share a display
  // name, so a name match must NEVER override the ids that disambiguate them
  // (mirrors sideAWon in api_serializers.jsx). Fall back to name only when an
  // id is absent on the winner or on that side.
  const matchesSide = side => {
    const sId = idOf(side);
    return (wId && sId) ? wId === sId : (!!wName && wName === nameOf(side));
  };
  if (matchesSide(m.sideB)) return "left";
  if (matchesSide(m.sideA)) return "right";
  return null;
}

// Derive an ippon array from a Go-formatted scoreA/scoreB string.
// The backend formatScore() appends "(HN)" for outstanding hansoku, e.g.
// "MK(H1)": and inserts a SPACE separator between ippons and the suffix
// when both are present, e.g. "MK (H1)" (see engine/scoring.go:715-724).
// Splitting the raw string would inject "(", "H", "1", ")": plus a
// stray " " for the spaced shape: as bogus ippon letters. This helper
// strips the suffix AND the separator space first.
function ipponsFromScore(scoreStr) {
  if (!scoreStr) return [];
  return scoreStr.replace(/\s*\(H\d+\)$/, "").split("");
}

// Format ippons as a readable score string: ["M","K"] → "MK", [] → ""
// Returns something like "MM vs K", "M (E) –", "M X K", "X", "BYE".
//
// The string is the flat analogue of a paper score sheet row:
//   [left cell] [middle] [right cell]
// The MIDDLE carries exactly one mark — "X" (tie), "(E)" (overtime), "(DH)"
// (rep bout) — or the plain "vs"; see middleMark for the exclusivity rules.
// A cell with no points reads "–" (never a separator, so never ambiguous).
// RESULT marks (Kiken / Fus. / Ht) ride in the cell of the competitor they
// name ("M Ht (E) K", "– vs Kiken"), which needs `winnerSide`
// ("left" | "right", from winnerSideLR) — without it the marks fall back to
// trailing after the score, still readable but unattributed.
// Mirrors the Excel export (internal/export/suffix.go MiddleMark/SideMarks +
// builder cell writes; the sheet template's own middle cell text is "vs" and
// its empty score cells stay empty).
function formatIpponsScore(ipponsLeft, ipponsRight, score, decision, encho, decidedByHantei, winnerSide) {
  // decidedByHantei (positional) is the canonical flag. The `typeof` guard
  // lets callers that omit the arg safely get false without sending undefined.
  const hantei = typeof decidedByHantei === "boolean" ? decidedByHantei : false;
  if (score?.type === "bye") return "BYE";
  let aStr = (ipponsLeft || []).filter(x => x && x !== "•").join("");
  let bStr = (ipponsRight || []).filter(x => x && x !== "•").join("");
  // A default win (fusensho / fusenpai / any kiken) awards the match points
  // without a technique — one maru "○" per awarded point: the full
  // two-point win in regulation, exactly one deciding point in encho
  // (sudden death). The engine records them as maru ippons itself with the
  // same rule (domain.DefaultWinIppons), so scored data carries
  // the balls; results recorded before that fill (or imported) reach here
  // with the decision but an empty winner cell — mirror the engine so a
  // won match never reads as no-points.
  if (isDefaultWinBC(decision)) {
    const fill = defaultWinMaru(encho).join("");
    if (winnerSide === "left" && !aStr) aStr = fill;
    else if (winnerSide === "right" && !bStr) bStr = fill;
  }
  const isDraw = isDrawResult(decision, score);

  // A cell with no points reads "–" (or stays empty when the whole string is
  // empty); the plain middle reads "vs", so the dash is never a separator and
  // is unambiguous as "no points".
  const NONE = "–";

  if (isDraw) {
    // A tie's middle is X and NOTHING else: a match that went to encho cannot
    // have ended tied, and hantei picks a winner, so THIS STRING drops such
    // stale data rather than displaying a contradiction. (Scope: the score
    // string only. A hand-edited row carrying draw+hantei+winner would still
    // show Ht on the MatchCard and in Excel, whose sideMarks run
    // unconditionally; no API-legal state produces that combination —
    // validation rejects decidedByHantei on a draw.)
    if (!aStr && !bStr) return "X";
    // Scored equal draw (e.g. 1–1 M/K hikiwake): show the points around the
    // draw mark so the viewer sees what was struck AND that it was a tie.
    return `${aStr || NONE} X ${bStr || NONE}`;
  }

  const mid = middleMark(decision, encho);
  const sep = mid ? ` ${mid} ` : " vs ";
  const marks = sideMarks(decision, hantei);
  const [leftMark, rightMark] = placeMarks(marks, winnerSide === "left", winnerSide === "right");
  // Marks placeMarks could not attribute to a side (no winnerSide from the
  // caller) stay loose and trail the string so the result is never silently
  // dropped.
  const looseMarks = leftMark || rightMark ? "" : joinSp(marks.winner, marks.loser);

  // Numbers are NOT a valid display for ippon: the per-side waza-letter
  // arrays are the only source of an ippon score. There is deliberately no
  // winnerPts/loserPts fallback here (callers derive the arrays from
  // scoreA/scoreB via ipponsFromScore, so real data always has letters;
  // count-only data renders no score rather than invalid digits).
  if (!aStr && !bStr && !leftMark && !rightMark) {
    // Nothing to put in either cell: collapse to the bare middle mark plus
    // any loose result marks ("(E)", "Kiken").
    return joinSp(mid, looseMarks);
  }
  // A cell holds its letters and/or its result mark ("M Ht (E) K",
  // "Ht (E) –" for a 0-0 hantei); an empty cell reads "–".
  return joinSp(`${joinSp(aStr, leftMark) || NONE}${sep}${joinSp(bStr, rightMark) || NONE}`, looseMarks);
}

// engiFlagScore: derive an engi match's flag-count score string from
// FlagsA (sideA = Aka) / FlagsB (sideB = Shiro), in "Shiro–Aka" order to
// match formatIpponsScore's convention. Engi is the ONLY competition type
// where a completed match result is numeric; every other type (kendo,
// naginata) shows ippon LETTERS via formatIpponsScore, never digits. Returns
// null when the match carries no flag data (not an engi match), so callers
// fall through to teamIVScore / formatIpponsScore.
function engiFlagScore(m) {
  if (!m || (m.flagsA == null && m.flagsB == null)) return null;
  return `${m.flagsB || 0}–${m.flagsA || 0}`;
}

// teamIVScore: derive a team match's individual-victories aggregate ("shiroIV–akaIV")
// from persisted subResults. Mirrors Go engine.ComputeTeamSummary: skip the daihyosen
// sentinel and any malformed negative position (position <= DAIHYOSEN_POSITION); award IV to whichever match-level side won each bout (winner
// matches the match-level OR sub-level side name); empty winner = hikiwake (no IV).
// Orientation: sideB = Shiro (left), sideA = Aka (right): matches the (ipponsB, ipponsA)
// call order used everywhere. Returns null when there are no subResults (individual
// matches) so callers fall back to formatIpponsScore.
function teamIVScore(m) {
  const subs = m && m.subResults;
  if (!Array.isArray(subs) || subs.length === 0) return null;
  const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  let ivA = 0, ivB = 0;
  for (const sub of subs) {
    if (!sub || sub.position <= DAIHYOSEN_POSITION) continue; // skip the daihyosen sentinel (-1) and any malformed negative position
    const w = sub.winner;
    if (!w) continue;                        // hikiwake / undecided → no IV
    if (w === aName || w === sub.sideA) ivA++;
    else if (w === bName || w === sub.sideB) ivB++;
  }
  return `${ivB}–${ivA}`; // Shiro (B) – Aka (A)
}

// teamIVPWScore: the full team-match result, IV over PW on two lines
// ("IV shiroIV–akaIV\nPW shiroPW–akaPW") so the two totals stay readable in the
// narrow centre score cell. Score containers use white-space: pre-line to honour
// the newline; single-line contexts (ippon letters) are unaffected.
// PW (points won) is AUTHORITATIVE from the server: Go MatchResult.MarshalJSON
// attaches m.teamResult {shiroIV, akaIV, shiroPW, akaPW} for every team match, so
// the client never re-derives PW (single source of truth with the standings PW
// column, via state.TeamResultFrom). Orientation: shiro = sideB, aka = sideA.
// Legacy payloads that predate teamResult fall back to the client IV aggregate
// (teamIVScore), IV only, since PW is not recoverable without the server field.
// Returns null for non-team matches (no teamResult and no subResults).
function teamIVPWScore(m) {
  const tr = m && m.teamResult;
  if (tr && typeof tr === "object") {
    return `IV ${tr.shiroIV}–${tr.akaIV}\nPW ${tr.shiroPW}–${tr.akaPW}`;
  }
  const iv = teamIVScore(m); // legacy fallback: IV only, no server teamResult
  return iv == null ? null : `IV ${iv}`;
}

const PlayerLine = React.memo(({ player, isWinner, side, showDojo, score, isTBD, isEngi, slotLabel, feederId }) => {
  const isAka = side === "a";
  if (!player || isTBD) {
    return (
      <div className={`bc-side bc-side--empty bc-side--${side}`}>
        <span className={`bc-color-badge bc-color-badge--${isAka ? "aka" : "shiro"}`}>{isAka ? "AKA" : "SHIRO"}</span>
        <span className="bc-name bc-name--tbd">{isTBD ? "TBD" : "-"}</span>
      </div>
    );
  }
  // One call, on the whole name, before the engi pair split: a feeder slot has
  // no " - " so the split is a no-op on it either way, and doing it first keeps
  // the rule in one place rather than per-half. `slotLabel` carries this card's bracket (so the
  // slot resolves to "Winner of M<n>"); without one, slotDisplayName still
  // guarantees no raw id escapes — it degrades to "TBD".
  const shownName = slotLabel ? slotLabel(player.name, feederId) : slotDisplayName(player.name);
  // Engi pair: split the combined name so member 2 stacks under member 1
  // instead of truncating on narrow bracket cards.
  const [m1, m2] = isEngi && window.engiPairParts ? window.engiPairParts(shownName) : [shownName, ""];
  return (
    <div className={`bc-side bc-side--${side} ${isWinner ? "bc-side--winner" : ""}`}>
      <span className={`bc-color-badge bc-color-badge--${isAka ? "aka" : "shiro"}`}>{isAka ? "AKA" : "SHIRO"}</span>

      <div className="bc-name-wrap">
        <span className="bc-name">
          {isWinner ? <span className="bc-winner-tick" aria-label="Winner" title="Winner">✓</span> : null}
          {player.number ? <span className="num-prefix">{player.number}</span> : null}
          {m1}
        </span>
        {m2 ? <span className="bc-name">{m2}</span> : null}
        {/* Reserve the dojo line on every side when dojos are shown: a real
            player without a dojo, or a "Winner of…" placeholder, gets an
            invisible spacer line so all sides (and thus all bracket cards) keep
            a uniform height (mp-7f2w). When showDojo is off, no line is rendered
            anywhere, so cards stay uniformly short. */}
        {showDojo ? <span className="bc-dojo">{player.dojo || <span aria-hidden="true">{"\u00A0"}</span>}</span> : null}
      </div>
      {score != null ? <span className="bc-score">{score}</span> : null}
    </div>
  );
});
PlayerLine.displayName = "PlayerLine";

const MatchCard = React.memo(({ match, variant, showDojo, onClick, highlighted, matchRef, highlightPlayers, matchNum, isEngi, slotLabel }) => {
  const aWin = match.winner && match.sideA && match.winner.id === match.sideA.id;
  const bWin = match.winner && match.sideB && match.winner.id === match.sideB.id;
  const running = match.status === "running";
  // score.type === "bye" is CLIENT-ONLY: the sole producers are the sample-data
  // generators in data.jsx (advanceByes / simulateRounds), never a server
  // payload — Go's BracketMatch/Match carry scoreA/scoreB strings and no `score`
  // object, and api_serializers.normalizeMatch only ever synthesizes
  // type "ippon" or "hikiwake". Kept because it is the ONLY bye cue a MatchCard
  // has (the card builds its per-side scores from ippon arrays and never calls
  // matchScoreStr, so removing this leaves such a card completely unlabelled).
  // A server-fed structural bye in a bracket carrying mp-7f2w metadata is not
  // drawn as a MatchCard at all (a legacy bracket has no metadata, routes to
  // BracketTreeLegacy, and does draw every rounds[] entry as a card): it renders
  // as the bc-bye-slot placeholder in BracketTreeMeta below.
  const isBye = match.score?.type === "bye";

  const ipponsA = match.ipponsA || ipponsFromScore(match.scoreA);
  const ipponsB = match.ipponsB || ipponsFromScore(match.scoreB);
  const isDone = match.status === "completed";
  // Engi is the ONLY competition type where a per-side score is numeric (a
  // referee flag count); every other type shows the ippon letters joined
  // above. A match carries flagsA/flagsB only when it was scored via the
  // engi flag editor (see engiFlagScore in this file for the shared-cell
  // equivalent).
  const isEngiMatch = match.flagsA != null || match.flagsB != null;
  // Result marks (Kiken / Fus. / Ht) ride with the competitor they name, in
  // that side's score slot — the node's "results column". The meta strip
  // above carries only the middle marks (X / (E) / (DH)).
  const cardMarks = isDone ? sideMarks(match.decision, !!match.decidedByHantei) : { winner: "", loser: "" };
  const [aMark, bMark] = placeMarks(cardMarks, aWin, bWin);
  const aScore = isDone ? (joinSp(isEngiMatch ? String(match.flagsA || 0) : ipponsA.join(""), aMark) || null) : null;
  const bScore = isDone ? (joinSp(isEngiMatch ? String(match.flagsB || 0) : ipponsB.join(""), bMark) || null) : null;

  const aTBD = match.sideA && typeof match.sideA.id === "string" && match.sideA.id.startsWith("tbd-");
  const bTBD = match.sideB && typeof match.sideB.id === "string" && match.sideB.id.startsWith("tbd-");

  // mp-xhaa: highlight any watched player (Set of ids+names). Lazy window
  // lookup mirrors the prior pattern so bracket.jsx stays decoupled from
  // viewer.jsx module load order.
  const _isWatched = (typeof window !== "undefined" && window.isPlayerWatched) || (() => false);
  const playerHighlight = !!(highlightPlayers && (_isWatched(match.sideA, highlightPlayers) || _isWatched(match.sideB, highlightPlayers)));

  // Meta-strip middle mark: X | (E) | (DH), mutually exclusive (see
  // middleMark). The X chip keeps its dedicated span below for styling.
  const metaMid = matchMiddleMark(match);
  return (
    <button
      ref={matchRef}
      type="button"
      data-match-id={match.id}
      className={`bc-match bc-match--v${variant} ${running ? "bc-match--running" : ""} ${match.status === "completed" ? "bc-match--done" : ""} ${highlighted ? "bc-match--highlight" : ""} ${playerHighlight ? "bc-match--my-match" : ""}`}
      onClick={onClick}
      aria-label={matchNum != null ? `Match ${matchNum}` : `Match ${match.id}`}
    >
      <div className="bc-match-meta">
        {matchNum != null ? <span className="bc-match-num">M{matchNum}</span> : null}
        <span className="bc-court"><TermBC name="shiaijo">Shiaijo</TermBC> {match.court}</span>
        {match.scheduledAt ? <span className="bc-time">{match.scheduledAt}</span> : null}
        {running ? <span className="bc-running">● NOW</span> : null}
        {isBye ? <span className="bc-bye-tag">BYE</span> : null}
        {metaMid === "X" ? <span className="bc-draw">X</span> : null}
        {metaMid === "(E)" ? <span className="bc-encho"><TermBC name="encho">(E)</TermBC></span> : null}
        {metaMid === "(DH)" ? (
          <span className="bc-decision-chip"><TermBC name="daihyosen">(DH)</TermBC></span>
        ) : null}
      </div>
      {/* feeders is [A, B]: hand each side ITS feeder so an unresolved slot can
          be named after the match that will actually fill it (see makeSlotLabeller). */}
      <PlayerLine player={match.sideA} isWinner={aWin} side="a" showDojo={showDojo} score={aScore} isTBD={aTBD} isEngi={isEngi} slotLabel={slotLabel} feederId={(match.feeders || [])[0]} />
      <div className="bc-divider"></div>
      <PlayerLine player={match.sideB} isWinner={bWin} side="b" showDojo={showDojo} score={bScore} isTBD={bTBD} isEngi={isEngi} slotLabel={slotLabel} feederId={(match.feeders || [])[1]} />
    </button>
  );
});
MatchCard.displayName = "MatchCard";

// Anchor connectors to the midline of a card's two competitor sides rather than
// the card's geometric centre. A card stacks a meta header (time/court) above the
// two .bc-side rows, so its geometric centre sits inside the upper (Aka) row: a
// connector landing there reads as pointing at one competitor instead of the seam
// where the two feeders merge. The sides-block midline is the visual join point;
// the offset from geometric centre is uniform across cards, so the arms stay
// horizontal. Falls back to geometric centre if sides are absent.
function anchorY(el, rect, treeTop) {
  const sides = el.querySelectorAll(".bc-side");
  if (sides.length >= 2) {
    const first = sides[0].getBoundingClientRect();
    const last = sides[sides.length - 1].getBoundingClientRect();
    return (first.top + last.bottom) / 2 - treeTop;
  }
  return rect.top + rect.height / 2 - treeTop;
}

// Computes connector lines from the DOM positions of each match card,
// then draws them in an absolutely positioned SVG.
function BracketConnectors({ rounds, treeRef, refMap, version }) {
  const [paths, setPaths] = useStateBC([]);
  const [size, setSize] = useStateBC({ w: 0, h: 0 });

  useLayoutEffectBC(() => {
    const compute = () => {
      const tree = treeRef.current;
      if (!tree || !rounds) return;
      const treeRect = tree.getBoundingClientRect();
      const out = [];
      for (let r = 0; r < rounds.length - 1; r++) {
        for (let i = 0; i < rounds[r].length; i += 2) {
          const aId = rounds[r][i]?.id;
          const bId = rounds[r][i + 1]?.id;
          const a = refMap.current[aId];
          const b = refMap.current[bId];
          const next = refMap.current[rounds[r + 1][i / 2]?.id];
          if (!a || !b || !next) continue;
          const aR = a.getBoundingClientRect();
          const bR = b.getBoundingClientRect();
          const nR = next.getBoundingClientRect();
          const aMidY = anchorY(a, aR, treeRect.top);
          const bMidY = anchorY(b, bR, treeRect.top);
          const nMidY = anchorY(next, nR, treeRect.top);
          const aRight = aR.right - treeRect.left;
          const nLeft = nR.left - treeRect.left;
          const midX = (aRight + nLeft) / 2;
          out.push({ key: `${aId}-${bId}-h`, d: `M ${aRight} ${aMidY} L ${midX} ${aMidY} L ${midX} ${bMidY} L ${aRight} ${bMidY}` });
          out.push({ key: `${aId}-${bId}-v`, d: `M ${midX} ${(aMidY + bMidY) / 2} L ${nLeft} ${nMidY}` });
        }
      }
      setPaths(out);
      setSize({ w: tree.scrollWidth, h: tree.scrollHeight });
    };
    compute();
    const ro = new ResizeObserver(compute);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", compute);
    return () => { ro.disconnect(); window.removeEventListener("resize", compute); };
  }, [rounds, version]);

  return (
    <svg className="bc-connectors" width={size.w} height={size.h} style={{ position: "absolute", left: 0, top: 0, pointerEvents: "none" }}>
      {paths.map((p) => (
        <path key={p.key} d={p.d} fill="none" stroke="var(--line-strong, #c7cdd9)" strokeWidth="1.5" />
      ))}
    </svg>
  );
}

// buildDisplayModel decides how to render a bracket. When the engine has tagged
// matches with effective-round metadata (mp-7f2w: displayRound / hidden /
// feeders), it groups the REAL matches into effective-round columns identical to
// the Excel Tree sheet (structural byes skip a column, phantom bye matches are
// dropped) and exposes a feeder graph for connector drawing. Otherwise it falls
// back to the legacy balanced-rounds shape with positional (2i, 2i+1) feeders so
// brackets generated before this field existed render exactly as before.
// useAutoScrollToMatch smooth-scrolls the bracket so the given match is centred
// in the scroll container. Shared by both the effective-round (BracketTreeMeta)
// and legacy (BracketTreeLegacy) renderers so the centring math lives in one place.
function useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version) {
  useLayoutEffectBC(() => {
    if (!autoScrollMatchId) return;
    const realId = String(autoScrollMatchId).split("::")[0];
    let frame1 = 0, frame2 = 0;
    const run = () => {
      const el = refMap.current[realId];
      const scrollEl = scrollContainerRef?.current;
      if (!el || !scrollEl) return;
      const elRect = el.getBoundingClientRect();
      const scRect = scrollEl.getBoundingClientRect();
      const targetLeft = scrollEl.scrollLeft + (elRect.left - scRect.left) - (scRect.width / 2 - elRect.width / 2);
      const targetTop = scrollEl.scrollTop + (elRect.top - scRect.top) - (scRect.height / 2 - elRect.height / 2);
      scrollEl.scrollTo({ left: Math.max(0, targetLeft), top: Math.max(0, targetTop), behavior: "smooth" });
    };
    frame1 = requestAnimationFrame(() => { frame2 = requestAnimationFrame(run); });
    return () => { cancelAnimationFrame(frame1); cancelAnimationFrame(frame2); };
  }, [autoScrollMatchId, version]);
}

function buildDisplayModel(rounds) {
  if (!rounds || rounds.length === 0) return { hasMeta: false, columns: rounds || [], feedersById: {} };
  const hasMeta = rounds.some((r) => r.some((m) => (m.displayRound || 0) > 0 || m.hidden));
  if (hasMeta) {
    let maxDR = 0;
    const real = [];
    rounds.forEach((r, backendRi) => r.forEach((m) => {
      if (!m.hidden && (m.displayRound || 0) > 0) { real.push({ ...m, roundIndex: backendRi }); if (m.displayRound > maxDR) maxDR = m.displayRound; }
    }));
    const columns = [];
    for (let dr = maxDR; dr >= 1; dr--) columns.push(real.filter((m) => m.displayRound === dr));
    // Structural-bye slots: for non-leaf matches whose feeder[i] is "" (meaning
    // one side had no upstream match and the player seeded directly), insert a
    // visible placeholder card in the upstream column so the tree layout
    // communicates the skip spatially: mirroring the Excel Tree sheet output.
    // Leaf-round matches (displayRound === maxDR) always have "" feeders for real
    // players; those don't need placeholders.
    const feedersById = {};
    real.forEach((m) => {
      const hasUpstream = m.displayRound < maxDR;
      const resolvedFeeders = [];
      (m.feeders || []).forEach((feederId, idx) => {
        if (feederId === "" && hasUpstream) {
          const playerObj = idx === 0 ? m.sideA : m.sideB;
          const playerName = typeof playerObj === "object" ? (playerObj?.name || "") : (playerObj || "");
          const slot = { id: `bye-${m.id}-${idx}`, isByeSlot: true, displayRound: m.displayRound + 1, playerName };
          feedersById[slot.id] = [];
          const colIdx = maxDR - slot.displayRound;
          if (colIdx >= 0 && colIdx < columns.length) columns[colIdx].push(slot);
          resolvedFeeders.push(slot.id);
        } else if (feederId !== "") {
          resolvedFeeders.push(feederId);
        }
      });
      feedersById[m.id] = resolvedFeeders;
    });
    // Match numbers: the "M1", "M2" stamped on the cards. A referee reads the
    // printed Excel sheet and the operator's screen side by side, so "M12" here
    // and "Match 12" there must name the same bout.
    //
    // The SERVED number is the answer whenever the bracket carries one: the
    // engine already computed it (engine.assignBracketMatchNumbers →
    // state.BracketMatch.MatchNumber, on the wire as matchNumber) and the whole
    // payload reaches us untouched (normalizeMatch spreads the match). Deriving
    // it a second time here only bought a second thing to drift from the sheet,
    // and it has drifted before (bc-draw: the app and the sheet named different
    // bouts "Match 12"). Excel's helper.AssignMatchNumbers is the other walk;
    // two implementations of one ordering is already one too many.
    //
    // Looked for on `real` — the matches that will actually carry a label —
    // rather than on every row: a number on a match this model drops cannot name
    // a card anyway. All-or-nothing, never per-match: a real match the engine
    // left unnumbered is better drawn with NO number than with a locally
    // invented one, which would either collide with a served number or point a
    // referee at the wrong bout.
    const matchNumById = {};
    const served = real.filter((m) => (m.matchNumber || 0) > 0);
    if (served.length > 0) {
      served.forEach((m) => { matchNumById[m.id] = m.matchNumber; });
    } else {
      // FALLBACK, still load-bearing: brackets persisted before MatchNumber
      // existed carry displayRound/hidden metadata but no numbering, and they
      // still have to render numbered cards. Same order as the Go walk: earliest
      // round first (highest displayRound), then LEFT TO RIGHT across the whole
      // tree, mirroring the Excel FillInMatches order.
      //
      // "Left to right" is the match's leftmost first-round slot, pos<<(roundIndex+1),
      // NOT the position alone: one effective round can hold matches from several
      // backend rounds at once (a shallow region's first bout shares a displayRound
      // with a deep region's second bout), and position means a different span in
      // each, so ordering on it interleaves them wrongly and the card number stops
      // matching the printed sheet. This is now the last place the Go/JS ordering
      // agreement is exercised at all, so the golden mirror
      // (__tests__/bracket_match_numbers.test.jsx) drives it with matchNumber
      // stripped out of the engine's own brackets.
      // id format: "m-r{ROUND}-{POS}": last segment is the 0-based within-round index.
      const posFromId = (id) => { const p = id.split("-"); return parseInt(p[p.length - 1], 10) || 0; };
      const leafSlotOf = (m) => posFromId(m.id) * Math.pow(2, (m.roundIndex || 0) + 1);
      const numbered = [...real].sort((a, b) => {
        const dr = b.displayRound - a.displayRound;
        return dr !== 0 ? dr : leafSlotOf(a) - leafSlotOf(b);
      });
      numbered.forEach((m, i) => { matchNumById[m.id] = i + 1; });
    }
    return { hasMeta: true, columns, feedersById, matchNumById };
  }
  // Legacy: columns = rounds. Connectors are positional (BracketConnectors
  // derives the 2i/2i+1 feeders from `rounds` itself), so no feeder graph is
  // produced here: feedersById stays empty to match the empty-input shape.
  return { hasMeta: false, columns: rounds, feedersById: {} };
}

// computeMetaTops lays out an (uneven) effective-round bracket. It walks the
// feeder graph from the final: matches with no feeders ("seeded" entrants: real
// players or bye recipients) are stacked top-to-bottom in depth-first encounter
// order, and every parent is centred so its OWN connector anchor sits at the mean
// of its feeders' anchors. Returns a map of matchId → absolute top (px).
//
// `heights` is matchId → measured card height. `offsets` is matchId → anchor
// distance from the card top (the y the SVG connectors join at: the sides-block
// midline for match cards, the geometric centre for bye-slot cards). Centring on
// the mean of feeder ANCHORS rather than geometric centres keeps the elbow on each
// card's seam even when a tall, header-offset match card feeds a child alongside a
// shorter bye-slot card: otherwise the asymmetric offset shifts the merge ~6px
// off the seam (delta != 0). `offsets` defaults to h/2 for any id it omits, so a
// caller that passes only heights gets the prior geometric-centre-of-mass layout.
function computeMetaTops(columns, feedersById, heights, offsets = {}) {
  const GAP = 16;
  const DEFAULT_H = 110;
  const offsetOf = (id) => offsets[id] ?? (heights[id] || DEFAULT_H) / 2;
  const anchorOf = {};
  const inProgress = new Set();
  let cursor = 0;
  const visit = (id) => {
    if (anchorOf[id] != null) return anchorOf[id];
    // Cycle guard (mirrors the DisplayRound!=0 guard in the Go BFS): anchorOf is
    // only set post-order for parents, so a cyclic feeders graph would recurse
    // forever. The engine only emits acyclic trees, but a corrupt/hand-edited
    // bracket.json must not crash the renderer: break the cycle and return 0.
    if (inProgress.has(id)) return 0;
    inProgress.add(id);
    const fs = (feedersById[id] || []).filter(Boolean);
    const h = heights[id] || DEFAULT_H;
    if (fs.length === 0) {
      // Leaf: stacked in natural flow (top = cursor); its anchor is top + offset.
      const a = cursor + offsetOf(id);
      cursor += h + GAP;
      anchorOf[id] = a;
      inProgress.delete(id);
      return a;
    }
    const as = fs.map(visit);
    const a = as.reduce((x, y) => x + y, 0) / as.length;
    anchorOf[id] = a;
    inProgress.delete(id);
    return a;
  };
  const rootId = columns[columns.length - 1]?.[0]?.id;
  if (rootId) visit(rootId);
  // Defensive: the engine only sets displayRound>0 on matches reachable from the
  // final, so visit(rootId) already placed every match in `columns`. This loop
  // is a no-op for engine output and only fires on corrupt/hand-written metadata.
  columns.forEach((col) => col.forEach((m) => {
    if (anchorOf[m.id] == null) {
      const h = heights[m.id] || DEFAULT_H;
      anchorOf[m.id] = cursor + offsetOf(m.id);
      cursor += h + GAP;
    }
  }));
  const tops = {};
  columns.forEach((col) => col.forEach((m) => {
    tops[m.id] = anchorOf[m.id] - offsetOf(m.id);
  }));
  return tops;
}

// BracketConnectorsMeta draws feeder→parent elbows for the effective-round
// layout (mp-7f2w). Unlike the legacy BracketConnectors it pairs by the explicit
// feeder graph, not binary (2i, 2i+1) positions, so uneven columns connect
// correctly. Bye-slot placeholder cards (isByeSlot) appear in the feeder graph
// and in refMap, so they DO receive connector lines from their parent match: 
// the elbow terminates at the bye card, mirroring the Excel Tree sheet.
function BracketConnectorsMeta({ columns, feedersById, treeRef, refMap, version, showDojo, variant }) {
  const [paths, setPaths] = useStateBC([]);
  const [size, setSize] = useStateBC({ w: 0, h: 0 });

  useLayoutEffectBC(() => {
    const compute = () => {
      const tree = treeRef.current;
      if (!tree) return;
      const treeRect = tree.getBoundingClientRect();
      const out = [];
      columns.forEach((col) => col.forEach((m) => {
        const fs = (feedersById[m.id] || []).filter(Boolean);
        if (fs.length === 0) return;
        const mEl = refMap.current[m.id];
        if (!mEl) return;
        const mR = mEl.getBoundingClientRect();
        const mLeft = mR.left - treeRect.left;
        const mMidY = anchorY(mEl, mR, treeRect.top);
        fs.forEach((fid) => {
          const fEl = refMap.current[fid];
          if (!fEl) return;
          const fR = fEl.getBoundingClientRect();
          const fRight = fR.right - treeRect.left;
          const fMidY = anchorY(fEl, fR, treeRect.top);
          const midX = (fRight + mLeft) / 2;
          out.push({ key: `${fid}->${m.id}`, d: `M ${fRight} ${fMidY} L ${midX} ${fMidY} L ${midX} ${mMidY} L ${mLeft} ${mMidY}` });
        });
      }));
      setPaths(out);
      setSize({ w: tree.scrollWidth, h: tree.scrollHeight });
    };
    compute();
    const ro = new ResizeObserver(compute);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", compute);
    return () => { ro.disconnect(); window.removeEventListener("resize", compute); };
    // showDojo/variant change card heights → feeder anchors move, so recompute.
  }, [columns, feedersById, version, showDojo, variant]);

  return (
    <svg className="bc-connectors" width={size.w} height={size.h} style={{ position: "absolute", left: 0, top: 0, pointerEvents: "none" }}>
      {paths.map((p) => (
        <path key={p.key} d={p.d} fill="none" stroke="var(--line-strong, #c7cdd9)" strokeWidth="1.5" />
      ))}
    </svg>
  );
}

// BracketTreeMeta renders the effective-round columns (mp-7f2w). Real match cards
// plus structural-bye placeholder cards (isByeSlot=true) are included; phantoms
// (hidden) are dropped. All cards are absolutely positioned at the
// feeder-graph-derived top so parents sit centred on their feeders
// (see computeMetaTops).
function BracketTreeMeta({ columns, feedersById, matchNumById, slotLabel, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef, highlightPlayers, isEngi }) {
  const treeRef = useRef(null);
  const refMap = useRef({});
  const [version, setVersion] = useStateBC(0);
  const [cardTops, setCardTops] = useStateBC(null);

  // Reset the measured layout only when the TOPOLOGY changes, not on every new
  // `columns` reference. Match ids and the feeder graph are frozen at generation
  // time, so a score update (which only mutates sideA/sideB/status/score) yields
  // the same signature and the measured tops are preserved: avoiding a reset-to-
  // null reflow flash on every in-progress-court update. Genuine height changes on
  // resolution are still caught by the measure effect's ResizeObserver below.
  const topoSig = React.useMemo(
    () => columns.map((c) => c.map((m) => m.id).join(",")).join("|"),
    [columns]
  );
  useEffectBC(() => { setCardTops(null); setVersion((v) => v + 1); }, [topoSig]);

  useLayoutEffectBC(() => {
    const measure = () => {
      const tree = treeRef.current;
      if (!tree || !columns || columns.length === 0) return;
      const heights = {};
      const offsets = {};
      for (const col of columns) {
        for (const m of col) {
          const el = refMap.current[m.id];
          if (!el) return;
          const rect = el.getBoundingClientRect();
          heights[m.id] = rect.height;
          // Anchor offset from the card top: the y the SVG connectors join at.
          // Mirrors anchorY(): sides-block midline for match cards, geometric
          // centre for bye-slot cards (no .bc-side). Passing this to computeMetaTops
          // centres parents on feeder ANCHORS, not geometric centres, so the elbow
          // lands on the seam even for a match-card + bye-slot feeder pair.
          const sides = el.querySelectorAll(".bc-side");
          if (sides.length >= 2) {
            const f = sides[0].getBoundingClientRect();
            const l = sides[sides.length - 1].getBoundingClientRect();
            offsets[m.id] = (f.top + l.bottom) / 2 - rect.top;
          } else {
            offsets[m.id] = rect.height / 2;
          }
        }
      }
      const tops = computeMetaTops(columns, feedersById, heights, offsets);
      // Every column is absolutely positioned, so the flow height of each
      // round-matches container is 0 and the tree would collapse. Derive the
      // overall content height from the lowest card bottom and pin it on the
      // containers so the tree (and its scroll area) size correctly.
      let height = 0;
      for (const col of columns) {
        for (const m of col) {
          const bottom = (tops[m.id] || 0) + (heights[m.id] || 0);
          if (bottom > height) height = bottom;
        }
      }
      setCardTops((prev) => {
        if (prev) {
          const same = Math.abs((prev.height ?? 0) - height) < 0.5 &&
            Object.keys(tops).length === Object.keys(prev.tops).length &&
            Object.keys(tops).every((k) => Math.abs((prev.tops[k] ?? 0) - tops[k]) < 0.5);
          if (same) return prev;
        }
        return { tops, height };
      });
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", measure);
    return () => { ro.disconnect(); window.removeEventListener("resize", measure); };
    // showDojo/variant affect card heights, so re-measure (and reposition) when
    // they change: not just on topology/version. The convergence guard keeps a
    // no-op recompute from causing a render.
  }, [columns, feedersById, version, showDojo, variant]);

  useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version);

  if (!columns || columns.length === 0) return null;
  const positioned = !!cardTops;
  // Every column is absolutely positioned, so override the legacy flex:1 (which
  // would zero the basis) and pin an explicit height; otherwise the flex row has
  // no non-abs column to establish its height and the tree collapses.
  const matchesStyle = positioned ? { flex: "none", height: `${cardTops.height}px`, minHeight: `${cardTops.height}px` } : undefined;
  return (
    <div className={`bc-tree bc-tree--v${variant}`} ref={treeRef}>
      <BracketConnectorsMeta columns={columns} feedersById={feedersById} treeRef={treeRef} refMap={refMap} version={version} showDojo={showDojo} variant={variant} />
      {columns.map((col, ci) => (
        // Column index is a stable key: bracket rounds never reorder.
        // oxlint-disable-next-line react/no-array-index-key
        <div key={ci} className="bc-round" style={{ "--round": ci }}>
          {/* Label the column from a match IN it, through the shared primitive,
              so a column header and the row/eyebrow labels for the same match can
              never diverge. Provably the same string as roundLabel(ci, length):
              columns run displayRound maxDR→1, so ci === maxDR - displayRound.
              The (ci, length) args are the fallback for an empty column, which
              buildDisplayModel never produces (the BFS assigns a contiguous
              1…maxDR, so every column holds at least one real match). */}
          <div className="bc-round-label">{bracketRoundLabel(col[0], ci, columns.length)}</div>
          <div className={`bc-round-matches${positioned ? " bc-round-matches--abs" : ""}`} style={matchesStyle}>
            {col.map((m, mi) => {
              const top = positioned ? cardTops.tops[m.id] : undefined;
              const wrapStyle = top != null
                ? { "--mi": mi, position: "absolute", top: `${top}px`, left: 0, right: 0 }
                : { "--mi": mi };
              const inner = m.isByeSlot ? (
                <div
                  className="bc-bye-slot"
                  aria-label={`${m.playerName || "Bye"}: advances without an opponent`}
                  ref={(el) => { if (el) refMap.current[m.id] = el; }}
                >
                  {/* The BYE tag is unconditional: a named slot without it is
                      just a grey box with a name in it, which reads as an
                      unexplained extra card rather than "this entrant advanced
                      unopposed". Name + tag share one flex row (see
                      .bc-bye-slot in styles.css): the name takes the free space
                      and ellipsises, the tag never shrinks, so a long name
                      truncates instead of pushing the marker out of the
                      fixed-width column. Rendered once in both cases: the
                      nameless slot is the tag alone, exactly as before. */}
                  {m.playerName ? <span className="bc-bye-slot__name">{m.playerName}</span> : null}
                  <span className="bc-bye-slot__tag">BYE</span>
                </div>
              ) : (
                <MatchCard
                  match={m}
                  variant={variant}
                  showDojo={showDojo}
                  highlighted={m.id === highlightedMatchId}
                  matchRef={(el) => { if (el) refMap.current[m.id] = el; }}
                  onClick={() => onMatchClick && onMatchClick(m, ci, mi, columns.length)}
                  highlightPlayers={highlightPlayers}
                  matchNum={matchNumById[m.id]}
                  isEngi={isEngi}
                  slotLabel={slotLabel}
                />
              );
              return (
                <div className="bc-match-wrap" key={m.id} style={wrapStyle}>
                  {inner}
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

// BracketTree switches between the effective-round renderer (when the engine
// supplied display metadata) and the legacy balanced-rounds renderer.
function BracketTree(props) {
  // Memoise on rounds identity: buildDisplayModel returns fresh arrays, and the
  // meta renderer's effects key on those: recomputing every render would reset
  // the measured layout in a loop.
  const model = React.useMemo(() => buildDisplayModel(props.rounds), [props.rounds]);
  // One labeller per bracket, built from the model that numbers the cards, so
  // "Winner of M3" always names the card drawn as "M3". Memoised alongside the
  // model: MatchCard is memo(), so a fresh function every render would defeat it.
  const slotLabel = React.useMemo(() => makeSlotLabeller(props.rounds, model.matchNumById), [props.rounds, model]);
  if (model.hasMeta) {
    return <BracketTreeMeta {...props} columns={model.columns} feedersById={model.feedersById} matchNumById={model.matchNumById} slotLabel={slotLabel} />;
  }
  return <BracketTreeLegacy {...props} slotLabel={slotLabel} />;
}

function BracketTreeLegacy({ rounds, slotLabel, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef, highlightPlayers, isEngi }) {
  const treeRef = useRef(null);
  const refMap = useRef({});
  const [version, setVersion] = useStateBC(0);
  // Measured absolute top (px) for each round≥1 card, keyed by match id. Round 0
  // flows naturally; every later card is then positioned at the exact midpoint of
  // its two real feeder centres. This is measured rather than derived from a fixed
  // slot pitch because card heights are not uniform within a bracket: a filled
  // name+dojo card (~118px) is taller than a TBD/placeholder card (~104px), so no
  // single pitch can centre every parent on its children.
  const [cardTops, setCardTops] = useStateBC(null);

  // On a bracket change, clear measured positions so the new rounds render in
  // natural flow for one frame (their match ids differ, so stale tops wouldn't
  // apply anyway) until the layout effect re-measures: avoids any stale-position
  // flash. The version bump re-runs the measure effect.
  useEffectBC(() => { setCardTops(null); setVersion((v) => v + 1); }, [rounds]);

  useLayoutEffectBC(() => {
    const measure = () => {
      const tree = treeRef.current;
      if (!tree || !rounds || rounds.length === 0) return;
      const rmEls = tree.querySelectorAll(".bc-round-matches");
      if (rmEls.length < rounds.length) return;
      const heights = {};
      const centers = []; // centers[r][i]: card centre relative to its round-matches top
      for (let r = 0; r < rounds.length; r++) {
        const rmTop = rmEls[r].getBoundingClientRect().top;
        if (r === 0) {
          const c0 = [];
          for (const m of rounds[0]) {
            const el = refMap.current[m.id];
            if (!el) return;
            const rect = el.getBoundingClientRect();
            heights[m.id] = rect.height;
            c0.push(rect.top - rmTop + rect.height / 2);
          }
          centers.push(c0);
        } else {
          // Heights still come from the DOM (card content is unaffected by the
          // absolute positioning, which keeps full width via left/right: 0).
          for (const m of rounds[r]) {
            const el = refMap.current[m.id];
            if (!el) return;
            heights[m.id] = el.getBoundingClientRect().height;
          }
          const prev = centers[r - 1];
          centers.push(rounds[r].map((_, i) => {
            const lo = prev[2 * i];
            const hi = prev[2 * i + 1] != null ? prev[2 * i + 1] : lo;
            return (lo + hi) / 2;
          }));
        }
      }
      const tops = {};
      for (let r = 1; r < rounds.length; r++) {
        rounds[r].forEach((m, i) => { tops[m.id] = centers[r][i] - heights[m.id] / 2; });
      }
      setCardTops((prev) => {
        if (prev) {
          const keys = Object.keys(tops);
          const prevKeys = Object.keys(prev);
          if (keys.length === prevKeys.length &&
              keys.every((k) => Math.abs((prev[k] ?? 0) - tops[k]) < 0.5)) {
            return prev; // unchanged: avoid a re-render loop
          }
        }
        return tops;
      });
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", measure);
    return () => { ro.disconnect(); window.removeEventListener("resize", measure); };
  }, [rounds, version]);

  useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version);

  if (!rounds) return null;
  // Round 0 flows naturally; rounds ≥ 1 are absolutely positioned at the measured
  // midpoint of their two feeder cards (see the layout effect above). cardTops is
  // null on the first paint, so every round renders in natural flow; the effect
  // then measures real centres and re-renders the later rounds into place, and
  // BracketConnectors' ResizeObserver redraws the SVG once the layout settles.
  return (
    <div className={`bc-tree bc-tree--v${variant}`} ref={treeRef}>
      <BracketConnectors rounds={rounds} treeRef={treeRef} refMap={refMap} version={version} />
      {rounds.map((round, ri) => {
        const positioned = ri > 0 && cardTops;
        return (
          // Round index is a stable key: bracket rounds never reorder.
          // oxlint-disable-next-line react/no-array-index-key
          <div key={ri} className="bc-round" style={{ "--round": ri }}>
            {/* Legacy (pre-mp-7f2w) brackets only: this renderer runs when no
                match carries displayRound AND none is hidden (see hasMeta), so
                bracketRoundLabel would degrade to this same call. Raw index is
                the only round there is. */}
            <div className="bc-round-label">{roundLabel(ri, rounds.length)}</div>
            <div className={`bc-round-matches${positioned ? " bc-round-matches--abs" : ""}`}>
              {round.map((m, mi) => {
                const top = positioned ? cardTops[m.id] : undefined;
                const wrapStyle = top != null
                  ? { "--mi": mi, position: "absolute", top: `${top}px`, left: 0, right: 0 }
                  : { "--mi": mi };
                return (
                <div className="bc-match-wrap" key={m.id} style={wrapStyle}>
                  <MatchCard
                    match={m}
                    variant={variant}
                    showDojo={showDojo}
                    highlighted={m.id === highlightedMatchId}
                    matchRef={(el) => { if (el) refMap.current[m.id] = el; }}
                    onClick={() => onMatchClick && onMatchClick(m, ri, mi, rounds.length)}
                    highlightPlayers={highlightPlayers}
                    isEngi={isEngi}
                    slotLabel={slotLabel}
                  />
                </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// matchScoreStr: unified score string for any completed match.
// Tries engiFlagScore first (engi matches → numeric "Shiro–Aka" flag count,
// the ONLY case with digits), then teamIVScore (team matches with
// subResults → "IV–IV"), then falls back to formatIpponsScore (every other
// competition type: ippon LETTERS, never numbers). Returns "" when no path
// produces a string (caller handles the ": " fallback).
//
// The ippon arrays are derived here, never passed in — a positional
// (B, A) parameter pair was the left/right-inversion trap this hoist
// removed. The derivation is load-bearing: bracket matches carry
// scoreA/scoreB strings rather than ipponsA/B arrays, the waza-letter
// arrays are the ONLY source of an ippon score string (numbers are never
// a valid ippon display, there is no numeric fallback), and
// ipponsFromScore strips Go formatScore's trailing "(HN)" hansoku suffix
// so it doesn't split into bogus ippon letters.
function matchScoreStr(m) {
  return engiFlagScore(m)
    || teamIVPWScore(m)
    || formatIpponsScore(
      m.ipponsB || ipponsFromScore(m.scoreB),
      m.ipponsA || ipponsFromScore(m.scoreA),
      m.score, m.decision, m.encho, m.decidedByHantei, winnerSideLR(m));
}

// matchStateCell: the centre score-cell content for a compact match row,
// shared so every list renders the SAME cue: completed → score string,
// anything else → boutMiddle (normally the plain "vs"); the row's
// .is-running highlight is the "now" signal, NOT a centre glyph, and the
// labelled "● NOW" badge elsewhere is a separate affordance.
function matchStateCell(m) {
  const mid = boutMiddle(m.decision, m.encho, m.score);
  if (m.status === "completed") return matchScoreStr(m) || mid;
  return mid;
}

// bronzeUnderFinalStyle: inline style that places the 3rd-place (bronze) card
// UNDER the final match card and makes it smaller than a full round card. The
// bronze section is a sibling of the bracket tree inside .bracket-canvas__inner,
// so it shares the tree's left origin. The final is always the LAST column, at
// (numCols - 1) column-steps from that origin; a column step is .bc-round's
// min-width (COL) + .bc-tree's gap (GAP). numCols comes from the same
// buildDisplayModel the tree renders from, so phantom bye columns are counted
// and the offset stays correct for any bracket size. The smaller card (CARD) is
// centred under the full-width final column.
function bronzeUnderFinalStyle(rounds) {
  // CARD (210) is the smallest width that still fits a typical winner name
  // without ellipsis truncation (measured live: "Haruto Watanabe" fits at 210,
  // truncates at 205), while staying visibly smaller than the 230px final it
  // sits under. COL/GAP mirror .bc-round min-width / .bc-tree gap.
  const COL = 230, GAP = 56, CARD = 210;
  const model = buildDisplayModel(rounds);
  const numCols = (model && model.hasMeta && Array.isArray(model.columns))
    ? model.columns.length
    : (Array.isArray(rounds) ? rounds.length : 1);
  const colOffset = Math.max(0, numCols - 1) * (COL + GAP);
  return { width: CARD, marginLeft: colOffset + (COL - CARD) / 2 };
}

window.BracketTree = BracketTree;
window.MatchCard = MatchCard;
window.bronzeUnderFinalStyle = bronzeUnderFinalStyle;
window.roundLabel = roundLabel;
// Exposed so every surface that labels a bracket MATCH (viewer rows, admin
// score editor, TV/display boards) uses the effective-round rule rather than
// re-deriving a name from the raw round index. See bracketRoundLabel above.
window.bracketRoundLabel = bracketRoundLabel;
// Exposed so the bracket winner-picker panel can label a selected match with
// the SAME number ("M1") and round the tree shows on its cards/columns.
window.buildDisplayModel = buildDisplayModel;
// Exposed so every surface that shows a bracket SIDE to a human (match details
// modal, shiaijo "Later" queue, feeder-resolution modal) renders slot values
// through the one rule instead of printing the raw "Winner of rX-mY" wire
// value. slotDisplayName is the no-bracket-context fallback ("TBD");
// bracketSlotLabeller resolves the slot to the card number the tree prints.
window.slotDisplayName = slotDisplayName;
window.bracketSlotLabeller = bracketSlotLabeller;
window.formatIpponsScore = formatIpponsScore;
window.teamIVScore = teamIVScore;
window.teamIVPWScore = teamIVPWScore;
window.engiFlagScore = engiFlagScore;
window.matchScoreStr = matchScoreStr;
window.matchStateCell = matchStateCell;
window.boutMiddle = boutMiddle;
window.defaultWinMaru = defaultWinMaru;
window.enchoOn = enchoOn;
window.matchMiddleMark = matchMiddleMark;
window.winnerSideLR = winnerSideLR;
window.sideLabel = sideLabel;
window.ipponsFromScore = ipponsFromScore;

export { formatIpponsScore, enchoLabel, boutMiddle, defaultWinMaru, matchMiddleMark, winnerSideLR, sideLabel, roundLabel, bracketRoundLabel, ipponsFromScore, teamIVScore, teamIVPWScore, engiFlagScore, matchScoreStr, matchStateCell, buildDisplayModel, computeMetaTops, bronzeUnderFinalStyle, PlayerLine, slotDisplayName, makeSlotLabeller, bracketSlotLabeller, MatchCard, BracketTree };
