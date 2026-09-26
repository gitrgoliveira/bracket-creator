// ineligible_match.jsx: a scheduled match that cannot be fought because a
// competitor in it is barred.
//
// A competitor who withdrew (kiken, fusenpai, or kiken-injury until they are
// reinstated) is "prohibited from participating in following shiai" (FIK
// Art. 31), so each match they still have ends as a default win for the
// opponent. The server knows who is barred and stamps it on every SCHEDULED
// match as the read-only `ineligibleSides` ({a?: decision, b?: decision},
// keyed by side, valued by the decision that barred that side; never stored).
// This module is the one reader of that stamp, so every surface that picks
// the next match, labels a row, or offers the default win asks the same
// question the same way.
//
// The default win is recorded as a match-level `fusensho` naming the barred
// side (decisionBy), through the existing /decision route. fusensho is the
// default win recorded on someone else's withdrawal: it writes no eligibility
// status, so the withdrawal that barred the competitor stays the one record
// of it, and reinstating or clearing that withdrawal still works.
//
// A leaf that imports only competitor_identity.jsx (itself a leaf with no
// imports), for the winner-attribution fallback withdrawnSideKey needs below:
// keep it to that, so every surface can take the rule without taking a real
// dependency graph with it.

import { sameCompetitor } from './competitor_identity.jsx';

// Side A is Aka and side B is Shiro, on pool and bracket matches alike -- the
// ONE owner of this two-value mapping, in both directions (bc-cse). It used
// to be hand-rolled at four call sites: admin_scoring_shared.jsx's
// withdrawnKeyOf, team_default_credit.jsx's creditedSideKey, this constant,
// and barred_chip.jsx's barredNameMark (whose per-side "aka"/"shiro" render
// context is the same vocabulary decisionBy uses, so sideKeyForDecisionBy
// answers that question too). All four now go through sideKeyForDecisionBy
// or withdrawnSideKey below.
const DECISION_BY = { a: "aka", b: "shiro" };

// sideKeyForDecisionBy: the read direction of the DECISION_BY mapping above
// ("aka"/"shiro" -> "a"/"b"), "" for anything else.
export function sideKeyForDecisionBy(decisionBy) {
    return decisionBy === DECISION_BY.a ? "a" : decisionBy === DECISION_BY.b ? "b" : "";
}

// withdrawnSideKey(m): the side ("a" = Aka/sideA, "b" = Shiro/sideB) a
// match's default-win ruling names as WITHDRAWN/BARRED -- decisionBy first,
// falling back for a LEGACY row recorded before decisionBy was captured to
// the match's own winner attribution (mirrors Go's state.DefaultWinCreditSide,
// which falls back to domain.AttributeWinnerSide the same way): the side
// that did NOT win is the one that was barred.
export function withdrawnSideKey(m) {
    if (!m) return "";
    const byDecision = sideKeyForDecisionBy(m.decisionBy);
    if (byDecision) return byDecision;
    if (m.winner && m.sideA && sameCompetitor(m.winner, m.sideA)) return "b";
    if (m.winner && m.sideB && sameCompetitor(m.winner, m.sideB)) return "a";
    return "";
}

// The competitor a decision just recorded on `match` bars from their other
// matches: the side a withdrawal (any kiken) or a no-show names. Null for any
// other decision, and for a write that did not come back (a queued write
// answers { queued: true }). The side comes from the caller's own copy of the
// match, because the /decision response is the stored match, whose sides are
// bare name strings. A court list shows that competitor's other matches
// barred only after it refreshes, so a surface that moves on straight after
// the decision skips them itself, through involvesCompetitor below.
// window.isKikenDecision is api_serializers.jsx's, read at call time so this
// leaf takes no import for it.
export function sideBarredByDecision(result, match) {
    const d = result && result.decision;
    if (!d || !(window.isKikenDecision(d) || d === "fusenpai")) return null;
    const key = withdrawnSideKey(result);
    return key === "a" ? match.sideA : key === "b" ? match.sideB : null;
}

// Whether `side` fights in match `m`. False when there is no side.
export function involvesCompetitor(m, side) {
    return !!side && (sameCompetitor(m.sideA, side) || sameCompetitor(m.sideB, side));
}

function nameOf(side) {
    return side?.name || "";
}

// The barred sides of a match, by side key. Empty for anything but a
// scheduled match: a match already running or decided is not awaiting a
// default win, whatever its competitors' status is now.
export function barredSides(m) {
    if (m.status !== "scheduled") return { a: "", b: "" };
    const s = m.ineligibleSides || {};
    return { a: s.a || "", b: s.b || "" };
}

// True when the match cannot be fought as scheduled. Every picker of the next
// match skips these; opening one directly is still allowed, because that is
// how the operator records its default win.
export function isBarredMatch(m) {
    const { a, b } = barredSides(m);
    return !!(a || b);
}

// The default win this match awaits, or null when no side is barred or both
// are (neither can be awarded the match then).
export function awaitedDefaultWin(m) {
    const { a, b } = barredSides(m);
    if (!a === !b) return null;
    const barredKey = a ? "a" : "b";
    const decision = a || b;
    return {
        barredKey,
        decision,
        barred: barredKey === "a" ? m.sideA : m.sideB,
        opponent: barredKey === "a" ? m.sideB : m.sideA,
        reinstateable: decision === "kiken-injury",
    };
}

// The /decision body for a default win, keyed directly by which side is
// barred and who they are. defaultWinDecisionBody (below) is its only
// caller, deriving the barred side from the `ineligibleSides` stamp via
// awaitedDefaultWin.
export function defaultWinDecisionBodyForSide(barredKey, barredSide) {
    return {
        decision: "fusensho",
        decisionBy: DECISION_BY[barredKey],
        decisionReason: `auto: ${nameOf(barredSide)} withdrawn`,
    };
}

// The /decision body that records the awaited default win, or null.
export function defaultWinDecisionBody(m) {
    const w = awaitedDefaultWin(m);
    if (!w) return null;
    return defaultWinDecisionBodyForSide(w.barredKey, w.barred);
}

// The operator's note for a barred match: who cannot fight, and what to do.
export function barredNote(m) {
    const { a, b } = barredSides(m);
    if (a && b) {
        // bc-cse: a pool/league match can be recorded as drawn (the guide's
        // rule: "If both competitors withdraw, neither receives a win or
        // points"), so its note stays the plain fact; a knockout bracket
        // needs a winner, so nothing here resolves it and the note says what
        // the operator must fix instead. bothBarredDrawable is the ONE gate
        // this text and BarredMatchNotice's action offer must agree on.
        return bothBarredDrawable(m)
            ? "Both withdrew earlier: neither can fight this match."
            : "Neither can fight: correct the earlier withdrawal or the draw.";
    }
    const w = awaitedDefaultWin(m);
    if (!w) return "";
    const name = nameOf(w.barred);
    if (w.reinstateable) return `${name} withdrew injured: reinstate them or record the default win.`;
    if (w.decision === "fusenpai") return `${name} did not appear earlier: record the default win.`;
    return `${name} withdrew: record the default win.`;
}

// The label of the operator's one-tap action.
export function defaultWinActionLabel(m) {
    const w = awaitedDefaultWin(m);
    return w ? `Record default win for ${nameOf(w.opponent)}` : "";
}

// bothBarredDrawable(m): true when a both-barred match's format allows
// recording it as drawn -- a pool/league match, never a knockout one (the
// bracket needs a winner, so a draw resolves nothing there). The server
// accepts that draw for exactly these ids (engine.IsPoolMatchID: a pool or
// league id starts "Pool "; Swiss and bracket ids do not), so the offer and
// the acceptance cannot disagree.
export function bothBarredDrawable(m) {
    return m.id.startsWith("Pool ");
}

// bothBarredDrawAction(m): the one-tap "record as drawn" action for a
// SCHEDULED pool/league match whose BOTH sides are barred, or null when
// this match does not qualify (not both barred, or a knockout bracket
// match). Bundles the gate, the button label, and the /decision body
// together -- like awaitedDefaultWin does for the single-sided case --
// so BarredMatchNotice never re-derives any of the three. The server
// accepts this hikiwake ONLY for a scheduled pool/league match with both
// sides barred (FIK guide: "If both competitors withdraw, neither
// receives a win or points").
export function bothBarredDrawAction(m) {
    const { a, b } = barredSides(m);
    if (!a || !b || !bothBarredDrawable(m)) return null;
    return {
        label: "Record as drawn (neither can fight)",
        body: {
            decision: "hikiwake",
            decisionReason: `auto: ${nameOf(m.sideA)} and ${nameOf(m.sideB)} withdrawn`,
        },
    };
}

// The spectator-facing chip beside a barred competitor's name.
export const BARRED_CHIP = "Withdrawn";
