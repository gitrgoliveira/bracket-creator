// Shared helpers and small presentational components for the scoring modals
// (ScoreEditorModal / TeamScoreEditorModal in admin_scoring_modal.jsx). Split
// out so the foundation can be reused and the modal file stays focused on the
// two stateful editors. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA, useMemo: useMemoA } = React;
const Icon = window.Icon;

import { DAIHYOSEN_POSITION, scoreRowMatchLabel } from './pool_ids.jsx';
import {
  writeDidNotLand, notLandedBanner,
  attemptScoreWrite, DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED, DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED, downstreamKnockoutReopenedNotice,
  courtBusyMessage,
} from './write_result.jsx';
import { sameCompetitor } from './competitor_identity.jsx';
import { sideWord } from './side_cell.jsx';
import { sideMarks } from './bracket.jsx';
import { NumberedName, numberFollowsName } from './numbered_name.jsx';
import { defaultWinDecisionBodyForSide, withdrawnSideKey } from './ineligible_match.jsx';
import { isTeamDefaultWinDecision } from './team_default_credit.jsx';
// BarredMatchNotice is a separate leaf (imports only ineligible_match.jsx +
// write_result.jsx): re-exported below, see that file's header for why.
import { BarredMatchNotice } from './barred_match_notice.jsx';

// Kendo best-of-3 cap. Mirrors the server-side `maxIpponsPerSide` in
// internal/mobileapp/validation.go: the bout ends when one side reaches
// 2 ippons, so 2-2 is an impossible scoreline. Used to gate the M/K/D/T/H
// buttons on both sides of every bout (individual + team sub-bout).
const MAX_IPPONS_PER_SIDE = 2;

// isBoutDecided: true once either side has reached the best-of-3 cap.
// The UI uses this to disable the add-ippon buttons on BOTH sides at
// that point: the bout would have ended at first-to-2, so neither side
// can legitimately add another ippon. Server enforces the same invariant
// in validateIppons (rejects 2-2 with HTTP 400).
function isBoutDecided(aPts, bPts) {
  return (aPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE
      || (bPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE;
}

// getIpponButtons: returns the ordered array of scoring button labels for a
// bout. Naginata adds "S" (Sune, shin strike) between "T" and "H".
function getIpponButtons(isNaginata) {
  return isNaginata ? ["M", "K", "D", "T", "S", "H"] : ["M", "K", "D", "T", "H"];
}

// getValidPointKeys: returns the string of valid single-character keyboard
// shortcuts for scoring. Keyboard handler checks `validKeys.includes(upper)`.
function getValidPointKeys(isNaginata) {
  return isNaginata ? "MKDTSH" : "MKDTH";
}

// IPPON_LETTER_LEGEND: the M/K/D/T/H (+S for naginata) ippon-type letters
// and their kendo meaning. The strike-target names mirror the kendo glossary
// (internal/domain/glossary.go datapoint "datapoint"/"sune"/"hansoku"). H is
// the hansoku-derived free point ("Two hansoku … give the opponent one free
// point", glossary "hansoku"). The viewer glossary doesn't define these
// single letters as terms, so the wording is authored here for the operator.
const IPPON_LETTER_LEGEND = [
  ["M", "Men: head strike"],
  ["K", "Kote: wrist strike"],
  ["D", "Do: torso strike"],
  ["T", "Tsuki: throat thrust"],
  ["S", "Sune: shin strike (naginata)"],
  ["H", "Hansoku point: opponent's 2nd foul"],
];

// IpponLegend: compact, always-present key mapping each scoring letter to its
// kendo meaning, so the admin scoring modal isn't dependent on the viewer-only
// glossary. The "S" (Sune) row only shows for naginata competitions. Styled
// inline with DESIGN tokens (this region of styles.css is owned elsewhere).
function IpponLegend({ isNaginata }) {
  const rows = IPPON_LETTER_LEGEND.filter(([letter]) => letter !== "S" || isNaginata);
  return (
    <details
      data-testid="scoring-modal-ippon-legend"
      style={{ marginTop: 10, fontSize: 12, color: "var(--ink-3)" }}
    >
      <summary style={{ cursor: "pointer", fontWeight: 600, color: "var(--ink-2)", userSelect: "none" }}>
        <span aria-hidden="true" style={{
          display: "inline-block", width: 16, height: 16, lineHeight: "16px",
          textAlign: "center", borderRadius: 999, border: "1px solid var(--line)",
          color: "var(--ink-2)", fontWeight: 700, marginRight: 6,
        }}>?</span>
        Ippon-type key
      </summary>
      <dl style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 10px", margin: "8px 0 0", padding: 0 }}>
        {rows.map(([letter, meaning]) => (
          <React.Fragment key={letter}>
            <dt style={{
              fontFamily: "var(--font-mono)", fontWeight: 700, color: "var(--ink-1)",
              margin: 0, textAlign: "center",
            }}>{letter}</dt>
            <dd style={{ margin: 0, color: "var(--ink-2)" }}>{meaning}</dd>
          </React.Fragment>
        ))}
      </dl>
    </details>
  );
}

// ScoringShortcutHint: quiet reminder of the keyboard shortcuts the editor
// supports. Rendered below the prev/next nav so the affordance sits where
// operators look for navigation. Clarity over decoration: plain muted text, no
// animation. The container's layout lives in the `.scoring-shortcut-hint`
// rule in styles.css, NOT inline: an inline display:flex outranked the
// coarse-pointer `display: none` and showed the hint on the iPad (bc-kbhn).
// pointKeys: the valid ippon letters ("MKDTH" / "MKDTSH") when this editor
// supports keyboard scoring — individual matches and kachinuki bouts — else
// "" (fixed-order team bouts score by tap only). Listed so the shortcut is
// discoverable instead of hidden in the code.
// hasNav / canClose: whether ←/→ and Esc actually do something on this host.
// A key that does nothing is never listed (the inline court console wires
// neither), and with nothing to list the hint renders nothing at all.
function ScoringShortcutHint({ pointKeys = "", hasNav = false, canClose = false }) {
  const kbd = {
    fontFamily: "var(--font-mono)", fontSize: 11, padding: "1px 5px",
    border: "1px solid var(--line)", borderRadius: 4, background: "var(--surface)",
    color: "var(--ink-3)", margin: "0 1px",
  };
  const keys = pointKeys ? [...pointKeys] : [];
  const groups = [];
  if (keys.length > 0) {
    groups.push(
      <React.Fragment key="pts">
        {keys.map((k) => <kbd key={k} style={kbd}>{k}</kbd>)}
        <span>Shiro</span>
        <span aria-hidden="true">·</span>
        <kbd style={kbd}>⇧</kbd><span>Aka</span>
      </React.Fragment>,
    );
  }
  if (hasNav) {
    groups.push(
      <React.Fragment key="nav">
        <kbd style={kbd}>←</kbd><kbd style={kbd}>→</kbd>
        <span>prev/next</span>
      </React.Fragment>,
    );
  }
  if (canClose) {
    groups.push(
      <React.Fragment key="close">
        <kbd style={kbd}>Esc</kbd>
        <span>close</span>
      </React.Fragment>,
    );
  }
  if (groups.length === 0) return null;
  return (
    <div
      className="scoring-shortcut-hint"
      data-testid="scoring-modal-shortcut-hint"
      aria-hidden="true"
    >
      {groups.map((g, i) => (
        <React.Fragment key={g.key}>
          {i > 0 && <span aria-hidden="true">·</span>}
          {g}
        </React.Fragment>
      ))}
    </div>
  );
}

// applyFusenshoToggle: pure reducer for the per-bout Fusensho button in
// TeamScoreEditorModal. Implements three behaviours on top of a sub-bout
// object {aPts, bPts, aFouls, bFouls, fusensho, _preFusensho?, ...}:
//   1. Toggle-on from a clean state: snapshot {aPts,bPts,aFouls,bFouls}
//      into _preFusensho, then write the default win.
//   2. Side-switch (fusensho is already on the other side): preserve
//      the original _preFusensho so a later untoggle restores the
//      genuine pre-fusensho score, not the intermediate default win.
//   3. Toggle-off (re-clicking the active side): restore from
//      _preFusensho and clear it. If no snapshot exists (e.g. modal
//      reopened from saved state: initSubs doesn't round-trip the
//      snapshot), just clear the flag.
// EVERY branch spreads `...prev` so per-sub fields this reducer does NOT
// own survive the toggle — notably the kachinuki `encho` marker (mp-gmcg)
// and manually typed side names. A bespoke object literal silently dropped
// them, which erased the (E) audit mark and inflated an encho default win
// from one maru to two. draw and fusensho are mutually exclusive, so a set
// draw is cleared when fusensho is applied.
// Manual pts/fouls edits clear _preFusensho separately (handled in
// the setPts/setFouls closures): once the operator hand-edits, the
// snapshot is stale.
function applyFusenshoToggle(prev, side) {
  if (prev.fusensho === side) {
    const snap = prev._preFusensho;
    if (snap) return { ...prev, aPts: snap.aPts, bPts: snap.bPts, aFouls: snap.aFouls, bFouls: snap.bFouls, fusensho: "", _preFusensho: undefined };
    return { ...prev, fusensho: "", _preFusensho: undefined };
  }
  const snap = prev._preFusensho || { aPts: prev.aPts, bPts: prev.bPts, aFouls: prev.aFouls, bFouls: prev.bFouls };
  // The maru cells come from the shared count rule (defaultWinMaru in
  // bracket.jsx): one maru per point, so two in regulation but ONE in encho.
  // Pass THIS bout's encho period — a per-bout fusensho can land on a pairing
  // already fighting on in overtime — and let the shared rule decide; a zero or
  // absent period reads as regulation there, so no local branch is needed.
  const maru = window.defaultWinMaru ? window.defaultWinMaru({ periodCount: prev.encho }) : ["○", "○"];
  const base = { ...prev, aFouls: 0, bFouls: 0, _preFusensho: snap, ...(prev.draw ? { draw: false } : {}) };
  if (side === "a") return { ...base, aPts: maru, bPts: [], fusensho: "a" };
  return { ...base, aPts: [], bPts: maru, fusensho: "b" };
}

// applyFoulIncrement: pure helper modelling a single `+` press on a
// side's foul counter. Per FIK rules (and internal/domain/glossary.go):
// "Two hansoku awarded to a competitor give the opponent one free point."
// The 2nd foul auto-awards an "H" ippon to the opponent and resets this
// side's counter to 0. The counter is "outstanding fouls not yet
// discharged into an H": discharged Hs live in the opponent's pts array.
//
// Bout-decided guard: if EITHER side is already at maxIppons the bout is
// over: the counter still resets to 0 on the 2nd foul but no new H is
// awarded. This prevents an auto-award from creating an invalid 2-2
// scoreline that the server's validateIppons would reject. The UI
// also disables the `+` button via isBoutDecided as a defense in depth.
// To undo a previously awarded H, the operator removes it from the
// opponent's slot directly.
function applyFoulIncrement(fouls, opponentPts, thisSidePts = [], maxIppons = MAX_IPPONS_PER_SIDE) {
  const next = fouls + 1;
  if (next < 2) return { fouls: next, opponentPts };
  if (opponentPts.length >= maxIppons || thisSidePts.length >= maxIppons) {
    return { fouls: 0, opponentPts };
  }
  return { fouls: 0, opponentPts: [...opponentPts, "H"] };
}

// reconcileFoulsAtOpen: pure helper for the reopen/correction flow.
// Pre-fix builds stored hansoku as a cumulative raw count (0..N) alongside
// the already-discharged "H" entries in the opponent's pts array. The new
// counter is "outstanding fouls not yet discharged" (0 or 1). Naively
// taking `rawFouls % 2` strips full pairs: but if the opponent's pts is
// MISSING the expected H entries (older data, partial save, imported
// match), the strip silently loses points. This helper tops up the
// opponent's pts with the missing H's (capped at maxIppons) before
// returning the outstanding remainder. Idempotent: when the H's are
// already present it leaves opponentPts unchanged.
function reconcileFoulsAtOpen(rawFouls, opponentPts, maxIppons = MAX_IPPONS_PER_SIDE) {
  const safe = Math.max(0, rawFouls);
  const expectedH = Math.floor(safe / 2);
  const haveH = opponentPts.filter(x => x === "H").length;
  const missing = Math.max(0, expectedH - haveH);
  const topUp = Math.min(missing, Math.max(0, maxIppons - opponentPts.length));
  const newOpp = topUp > 0 ? [...opponentPts, ...Array(topUp).fill("H")] : opponentPts;
  return { outstandingFouls: safe % 2, opponentPts: newOpp };
}

// nextFoulOnDecrement: pure helper for the `−` button. Returns the new
// foul value (a NUMBER, not a React-style functional updater), suitable
// for setters like the team sub-match `rs.setFouls(value)` shape that
// doesn't accept fn-updaters. Extracted so the team-modal `−` regression
// (a fn-updater silently storing as state) is unit-testable.
function nextFoulOnDecrement(currentFouls) {
  return Math.max(0, currentFouls - 1);
}

// Term: kendo-glossary tooltip wrapper. Read lazily off window so the
// load order between glossary.js and this module doesn't matter (both
// are type="module" scripts and may execute in any order). Falls back
// to a plain pass-through when window.Term isn't available yet (e.g.
// vitest harness, or pre-mount of the glossary module).
function TermAS(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Lazily loaded from window for the same load-order reason as TermAS above.
// Falls back to null: the icon is purely decorative; no content to preserve.
// No align prop: the tooltip now self-positions (Term in glossary.jsx
// measures its room on open and flips leftwards itself), so no call site
// needs to say where in a wrapping row its hint landed.
function GlossaryHintAS({ name }) {
  if (typeof window !== 'undefined' && window.GlossaryHint) {
    return React.createElement(window.GlossaryHint, { name });
  }
  return null;
}

// T093–T098: shared helpers for the decision (kiken/fusenpai/fusensho) flow.
//
// Resolve the password for /decision POST. The helper only uses the prop
// (no window fallback); callers must pass the password explicitly. Returns ""
// as a safe sentinel that the server will reject with 401, surfacing any
// misconfiguration where the prop was not provided.
function resolveDecisionPassword(propPassword) {
  return propPassword || "";
}

// Guard for actions with a HARD prerequisite on server-side persistence
// (e.g. the daihyosen pre-save). window.API.recordScore hands back one of four
// shapes, and only two of them mean the dependent request may proceed:
//
//   MatchResult      confirmed by the server. Proceed.
//   { stale: true }  a SAME-SESSION out-of-order write. The server already
//                    holds this operator's own equal-or-newer state, so a
//                    dependent read sees their own later intent. Proceed.
//   { queued: true } never reached the server (offline / retryable 5xx). ABORT.
//   { applied:false} reached the server and the timestamp guard DROPPED it
//                    because a DIFFERENT writer's newer result won (bc-lww1).
//                    ABORT.
//
// The last two are exactly what writeDidNotLand owns, so this asks it rather
// than re-spelling the test — this was the SIXTH site of that question and the
// one the original five-site conversion missed, which is also the reason the
// rule now lives in a leaf module instead of at each caller.
//
// The stale/superseded split is the subtle part, and is why this cannot simply
// abort on "not a MatchResult": `stale` is the operator's OWN newer state, so
// a dependent read sees their later intent and is safe, whereas `applied:false`
// is a different writer's state this operator has never seen, so a dependent
// request built on it acts on a scoreline that was never on their screen.
//
// Throws "score_not_synced" so the caller aborts rather than running its
// dependent request against server state it did not produce.
function assertRunningWritePersisted(saveRes) {
  if (writeDidNotLand(saveRes)) throw new Error("score_not_synced");
}

// T093/T094: build the /decision POST body. Pure helper so we can pin the
// wire shape (decision/decisionBy/decisionReason/encho/force) against a
// moving server contract. `force` is the T103 override flag used when the
// server replies decision_locked and the operator confirms the override.
function buildDecisionBody(kind, { decisionBy, decisionReason }, enchoPeriodCount, opts = {}) {
  const body = { decision: kind, decisionBy };
  if (decisionReason) body.decisionReason = decisionReason;
  if (enchoPeriodCount > 0) body.encho = { periodCount: enchoPeriodCount };
  if (opts.force) body.force = true;
  return body;
}

// mp-os3: shared decision-submit path used by both ScoreEditorModal and
// TeamScoreEditorModal. Wraps buildDecisionBody + recordDecision and
// resolves the password from the explicit prop. Extracted so the regression
// test pins the production call site (rather than re-implementing the chain
// inside the test, which was how the original gap slipped through). Returns
// the promise from window.API.recordDecision so callers can await + handle
// the 409 decision_locked retry-with-force loop.
//
// bc-cse: routed through attemptScoreWrite (write_result.jsx), the SAME
// confirm-and-retry loop admin.jsx's editMatchScore gives the score path, so
// a kiken/fusenpai/daihyosen decision that corrects a completed knockout
// match whose later round already played gets the identical
// downstream_knockout_played 409 experience instead of a raw error token
// with no way forward. The recordScore collaborator here deliberately
// declares only 4 parameters (compId, matchId, result, password) so the
// `match` argument attemptScoreWrite forwards is dropped rather than reaching
// window.API.recordDecision, whose call-site contract (pinned in
// admin_scoring_modal.test.jsx) is exactly 4 arguments. `result` is the
// already-built decision body: attemptScoreWrite's retry spreads
// `forceDownstreamReopen: true` onto it exactly as it does the score body,
// since both are plain objects the server reads the same field off.
function submitDecisionRequest(compId, matchId, kind, decisionPayload, enchoPeriodCount, password, opts = {}) {
  const body = buildDecisionBody(kind, decisionPayload, enchoPeriodCount, opts);
  return attemptScoreWrite({
    recordScore: (cId, mId, result, pwd) => window.API.recordDecision(cId, mId, result, pwd),
    confirmDialog: window.confirmDialog,
    compId, matchId, result: body, password: resolveDecisionPassword(password),
  });
}

// makeSubmitDecision builds the kiken/fusenpai decision submit handler shared by
// ScoreEditorModal and TeamScoreEditorModal. The two modals had byte-identical
// copies of this (the only difference was the "competitors"/"teams" wording in
// the decision_locked confirm), so it lives here once. The returned closure:
//   - POSTs the decision, then on a kiken result resolves the loser side and
//     opens the withdrawn-player panel for default-win chaining (kiken keeps
//     the modal open: DO NOT route kiken through onAfterDecision, the
//     operator must work through RemainingMatchesPanel first);
//   - for fusenpai / other non-kiken decisions: calls onAfterDecision when
//     provided and the match is not a correction (item 7: starts next match),
//     else falls back to onClose;
//   - on 409 decision_locked, confirms then retries with force (recursing
//     into itself).
// Call it fresh each render so it captures the current enchoPeriodCount/password.
function makeSubmitDecision({
  match,
  enchoPeriodCount,
  password,
  mountedRef,
  setDecisionSubmitting,
  setDecisionErr,
  setWithdrawnPlayer,
  setDecisionPromptKind,
  onClose,
  // item 7: optional zero-arg callback invoked after a non-kiken decision
  // succeeds and the match is not a correction. The shiaijo page wires this
  // to startMatch(next) so the operator advances without an extra tap.
  // Kiken always keeps the modal open for RemainingMatchesPanel chaining.
  onAfterDecision,
  isComplete,       // item 7: corrections (isComplete=true) must not auto-advance
  entityLabel = 'competitors',
  // F5: optional pending-write handles threaded in from ScoreEditorModal so
  // a queued (offline) decision write shows the sticky "Not saved yet" banner.
  // Not provided by TeamScoreEditorModal (which has its own pending state path).
  setPendingWrite,
  pendingFnRef,
}) {
  const submit = async (kind, { decisionBy, decisionReason }, opts = {}) => {
    setDecisionSubmitting(true);
    setDecisionErr('');
    // F5: clear any prior pending-write state when the operator retries.
    if (setPendingWrite && mountedRef.current) setPendingWrite(false);
    try {
      const updated = await submitDecisionRequest(
        match.compId, match.id, kind, { decisionBy, decisionReason }, enchoPeriodCount, password, opts,
      );
      if (!mountedRef.current) return;
      // A decision that did not land must not advance ANYTHING below this
      // line: not the kiken hand-off to RemainingMatchesPanel, not
      // onAfterDecision (which the shiaijo page wires to a LOCAL bracket
      // advance), not the close. This asks the owner predicate rather than
      // `updated.queued` so BOTH not-landed shapes take the same exit --
      // queued (F5: offline/transient, will land on reconnect) and
      // `applied:false` (bc-lww1: the server refused it, so it never will).
      // The score path already guards this way; a decision that advanced a
      // bracket on a winner the server discarded is the same failure.
      //
      // LIVE since mp-jnvl: the /decision write now carries a real
      // modifiedAt (recordDecision, api_client.jsx), so the server can
      // genuinely answer with applied:false for either reason (superseded or
      // clock_skew) -- this is no longer a defence against a shape it could
      // not yet send.
      //
      // F5: enter pending-write mode so the banner shows in the footer, and
      // save the submit closure so "Retry now" can re-invoke it directly.
      if (writeDidNotLand(updated)) {
        if (setPendingWrite) {
          setPendingWrite(true);
          if (pendingFnRef) pendingFnRef.current = () => submit(kind, { decisionBy, decisionReason }, opts);
        }
        return;
      }
      if (window.isKikenDecision(kind)) {
        // Kiken keeps the modal open so the operator can walk through
        // RemainingMatchesPanel and award default wins to each remaining
        // scheduled match for the withdrawn player. Do NOT advance yet.
        //
        // bc-pnum: decisionBy ("aka"/"shiro") already names the withdrawn
        // SIDE unambiguously and matches the server's own attribution
        // exactly (scoring_tx.go: aka=sideA, shiro=sideB) -- no name
        // comparison needed. Re-deriving the loser from the /decision
        // response's plain winner/sideA/sideB NAME strings (the previous
        // approach) goes wrong for a same-name/different-dojo pair: both
        // sides' names are then identical, so a name compare always
        // resolves to the SAME side regardless of who actually withdrew.
        const loser = decisionBy === 'aka'
          ? (match.sideA || { id: '', name: '' })
          : (match.sideB || { id: '', name: '' });
        setWithdrawnPlayer(loser);
        setDecisionPromptKind('');
      } else if (!isComplete && onAfterDecision) {
        // Item 7: fusenpai (and any future non-kiken decision) advances to the
        // next match on the same court. The decision was already persisted via
        // /decision POST so we do NOT issue another score PUT: just advance.
        // Pass the resolved result (winner/status) so an offline host can also
        // advance the LOCAL bracket for a decision-completed bout (mp-y3nk),
        // matching the score path's maybeAdvanceLocal.
        await onAfterDecision(updated);
      } else {
        onClose();
      }
    } catch (e) {
      const msg = e?.message || 'Failed to record decision';
      // bc-cse: the operator declined attemptScoreWrite's downstream-knockout
      // confirm dialog (submitDecisionRequest above). Declining left the
      // match and the later result it depends on unchanged -- the SAME
      // cancellation copy the score path's editMatchScore shows (admin.jsx),
      // not the raw refusal message the thrown error still carries.
      if (e && e.downstreamKnockoutPlayedCancelled) {
        if (mountedRef.current) setDecisionErr(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED);
        return;
      }
      // T103: server returns "decision_locked" when a kiken-undo would
      // invalidate a downstream match: confirm and retry with force.
      if (!opts.force && /decision_locked/i.test(msg)) {
        const ok = mountedRef.current && await window.confirmDialog({
          message:
            `A subsequent match for one of these ${entityLabel} has already started.\n\n` +
            'Overwriting the prior decision now may make those downstream results inconsistent. Proceed anyway?',
          confirmLabel: 'Proceed anyway',
          danger: true,
        });
        if (!mountedRef.current) return;
        if (ok) { await submit(kind, { decisionBy, decisionReason }, { force: true }); return; }
        setDecisionErr('Override cancelled.');
      } else if (mountedRef.current) {
        setDecisionErr(msg);
      }
    } finally {
      if (mountedRef.current) setDecisionSubmitting(false);
    }
  };
  return submit;
}

// mp-m4bn: encho periods are UNBOUNDED. How many overtime periods a match
// runs, and how a still-tied match is finally resolved (hantei for an
// individual bout, daihyosen for a team encounter), is the shimpan's call,
// not the software's. The operator records what was fought, so the stepper
// only guards the lower bound; there is no maximum to clamp against.
function nextEnchoPeriod(current) {
  return current + 1;
}

function prevEnchoPeriod(current) {
  return Math.max(1, current - 1);
}

// mp-4pc: once a daihyosen (rep bout, wire position DAIHYOSEN_POSITION) exists, the encho
// rides on that sub, not the top-level match (enchoBlock suppresses the
// match-level encho when hasDaihyosen). On re-open we must restore the
// period count from the sub: else a persisted decidedByHantei replays
// without encho and the backend rejects the next save
// ("requires encho with at least one period"). Exported for vitest.
function initialEnchoPeriodsForMatch(m) {
  const daihyosen = (m.subResults || []).find(s => s.position === DAIHYOSEN_POSITION);
  if (daihyosen) return daihyosen.encho?.periodCount || 0;
  return m.encho?.periodCount || 0;
}

// daihyosenEnchoFields: pure builder for the encho/decidedByHantei wire
// fields on the daihyosen representative bout (position DAIHYOSEN_POSITION). The backend
// invariant (validation.go validateSubBout) is: encho and hantei are valid
// ONLY on the daihyosen. Encho is OPTIONAL for hantei: a tied daihyosen may
// be taken straight to a judges' decision without overtime: so the two
// fields are emitted independently: encho whenever the counter is > 0, and
// decidedByHantei explicitly (operator ruling "all results must be recorded
// into storage"): true when armed on a tied scoreline, otherwise an EXPLICIT
// false. That boolean is a LOCAL signal, not a wire field: toBackendMatchResult
// (api_serializers.jsx) consumes it and states the verdict as the "Ht" mark in
// the winner's ippon list instead - true places the mark, false leaves the
// arrays markless - so the flag itself never reaches the server.
//
// What the server reads as verdict-SILENCE is therefore the ippons, and the
// discriminator is nil versus empty: a writer that omits the arrays entirely
// (stale snapshots, quick-score) said nothing and has its stored verdict
// preserved, while an explicit [] says "zero points" and withdraws a 0-0
// verdict rather than being read as silence (engine preserveSubHantei).
// Emitting the false is what drives that stripping, so a caller that HAS a
// say still must state it.
//
// This builder itself always states the boolean when it runs, never omits
// it: that is safe because of where it is called from - the team editor
// adopts whatever verdict the server holds (its adopt effect), so a call
// into this function can never state an authoritative false about something
// it has not displayed. But `buildPatch` (admin_scoring_team.jsx) now has a
// SILENT branch (`daihyosenSilent`) that skips this builder entirely for a
// daihyosen row that is untouched this session AND nothing about it is known
// locally - that omission, not a call into this function, is how genuine
// silence reaches the wire today. This builder is still the ONLY caller that
// states the boolean, and it is still the case that when it runs, it commits
// to a boolean rather than a third "unknown" state - that part of the
// adoption argument still holds; it is simply no longer unconditional, since
// `buildPatch` may choose not to call it at all.
//
// There used to be a `hanteiKnown` parameter for exactly the case where an
// editor mounted before the verdict existed kept a mount-frozen armed flag,
// so it would have erased another device's judges' decision on the next
// unrelated autosave, and passing false made the write SILENT instead.
// Freezing bought that safety by letting the editor show something other
// than the stored result, which breaks the rule that a match has one result
// and every surface shows the same one. Adoption fixes the display and
// removes THAT blind spot (a mounted-and-stale editor), which is why the
// parameter was removed from this function; the untouched-and-never-yet-seen
// case is instead handled one layer up, by `buildPatch` choosing not to call
// this builder at all. Returns the fields to merge into the entry.
// Exported for vitest.
function daihyosenEnchoFields({ enchoPeriodCount, daihyosenTied, daihyosenHantei }) {
  const fields = { decidedByHantei: !!(daihyosenTied && daihyosenHantei) };
  if (enchoPeriodCount > 0) fields.encho = { periodCount: enchoPeriodCount };
  return fields;
}

// Pure decision for the draw-toggle action (button and keyboard shortcut).
// Returns:
//   {action: "enter"}  : set draw=true, clear pts (only when no scores exist)
//   {action: "cancel"}: set draw=false (always allowed when in draw mode)
//   {action: "noop"}   : blocked: scores exist while not in draw mode (button disabled)
// Exported for vitest.
function decideDrawToggle({ isDrawToggled, aTotal, bTotal }) {
  if (isDrawToggled) return { action: "cancel" };
  if (aTotal === 0 && bTotal === 0) return { action: "enter" };
  return { action: "noop" };
}

// shouldBlockScoringKeys: pure predicate consumed by the onKeyDown handler.
// Returns true when scoring keys (M/K/D/T/H/S and x/X draw toggle) must be
// suppressed. Currently this happens when hantei is armed: the backend
// requires a tied scoreline at that point, so any score mutation would
// produce a 400 on submit.
function shouldBlockScoringKeys({ decidedByHantei }) {
  return !!decidedByHantei;
}

// EnchoControl: collapsed by default to a small "⏱ Overtime" pill so
// it occupies <24px of vertical space in the live scoring modal. The
// full counter UI mounts only when overtime is active (enchoPeriodCount
// > 0) OR the operator clicks the pill (local showCounter state). The
// counter is the −/×N/+ stepper, unbounded above (see nextEnchoPeriod).
// Used by both ScoreEditorModal and TeamScoreEditorModal.
function EnchoControl({ enchoPeriodCount, setEnchoPeriodCount }) {
  const [showCounter, setShowCounter] = useStateA(enchoPeriodCount > 0);
  const expanded = showCounter || enchoPeriodCount > 0;
  if (!expanded) {
    return (
      <div className="encho-row encho-row--collapsed">
        <button
          type="button"
          className="encho-pill"
          data-testid="scoring-modal-encho-pill"
          onClick={() => setShowCounter(true)}
          aria-label="Show overtime (encho) controls"
        >
          <span aria-hidden="true" className="encho-pill__icon">{Icon ? <Icon name="timer" size={14} /> : "⏱"}</span>
          <TermAS name="encho">Overtime</TermAS>
        </button>
      </div>
    );
  }
  return (
    <div className="encho-row encho-row--expanded">
      <label className="encho-row__label">
        <input
          data-testid="scoring-modal-encho-checkbox"
          type="checkbox"
          checked={enchoPeriodCount > 0}
          onChange={(e) => {
            const next = e.target.checked ? Math.max(1, enchoPeriodCount) : 0;
            setEnchoPeriodCount(next);
            if (!e.target.checked) setShowCounter(false);
          }}
        />
        <TermAS name="encho">Encho</TermAS> started (overtime)
      </label>
      {enchoPeriodCount > 0 && (
        <div className="encho-row__stepper">
          <button
            type="button"
            className="btn btn--sm encho-row__btn"
            onClick={() => setEnchoPeriodCount(prevEnchoPeriod)}
            disabled={enchoPeriodCount <= 1}
            aria-label="Decrease overtime period count"
          >−</button>
          <span className="encho-row__count">×{enchoPeriodCount}</span>
          <button
            type="button"
            className="btn btn--sm encho-row__btn"
            onClick={() => setEnchoPeriodCount(nextEnchoPeriod)}
            aria-label="Increase overtime period count"
          >+</button>
        </div>
      )}
    </div>
  );
}

// Render the inline kiken/fusenpai prompt that replaces the score controls
// while open. Side picker uses radio inputs labelled "SHIRO (White)" / "AKA
// (Red)" to stay consistent with the score board legend; the value submitted
// to the backend is "shiro" or "aka" per DecisionRequest.Validate. The reason
// is always optional, a reopened match included: a match can be reopened
// without any reason, and ending it again asks for none (operator ruling
// 2026-09-25).
function DecisionPrompt({ kind, sideA, sideB, defaultSide, askReason, onCancel, onSubmit, submitting }) {
  const [side, setSide] = useStateA(defaultSide || "shiro");
  const [reason, setReason] = useStateA("");
  const showReason = askReason;
  // Display rule (locked, glossary.md §Display rule): render the
  // romaji term ALONE: the popover (via <Term>) carries the gloss.
  // We keep "Decision" untouched (it's already plain English) and
  // wrap the kendo terms so a volunteer hovering/tapping the title
  // gets the full tooltip.
  const isKiken = window.isKikenDecision(kind);
  const title = isKiken || kind === "fusenpai"
    ? React.createElement(TermAS, { name: kind }, withdrawalLabel(kind))
    : "Decision";

  const submit = (e) => {
    e?.preventDefault?.();
    if (submitting) return;
    onSubmit({ decisionBy: side, decisionReason: showReason ? reason.trim() : "" });
  };

  return (
    <form className="decision-prompt" onSubmit={submit} style={{ border: "1px solid var(--line)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2)" }}>
      <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 8 }}>{title}</div>
      <div style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12 }}>
        <div style={{ fontWeight: 600 }}>{isKiken ? "Which side withdrew?" : "Which side did not show up?"}</div>
        <div style={{ display: "flex", gap: 12 }}>
          <label style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <input type="radio" name="decision-side" value="shiro" checked={side === "shiro"} onChange={() => setSide("shiro")} />
            <span><TermAS name="shiro">SHIRO</TermAS> (White){sideB?.name ? `: ${sideB.name}` : ""}</span>
          </label>
          <label style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <input type="radio" name="decision-side" value="aka" checked={side === "aka"} onChange={() => setSide("aka")} />
            <span><TermAS name="aka">AKA</TermAS> (Red){sideA?.name ? `: ${sideA.name}` : ""}</span>
          </label>
        </div>
        {showReason && (
          <label style={{ display: "flex", flexDirection: "column", gap: 4, marginTop: 4 }}>
            <span style={{ fontWeight: 600 }}>
              Reason (optional, ≤200 chars)
            </span>
            <input
              type="text"
              className="input"
              maxLength={200}
              value={reason}
              onInput={(e) => setReason(e.target.value)}
              placeholder="e.g. injury, no-show, doctor's stop"
              data-testid="decision-reason"
            />
          </label>
        )}
      </div>
      <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 10 }}>
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={submitting}>Cancel</button>
        <button type="submit" className="btn btn--primary btn--sm" disabled={submitting}>
          {submitting ? "Saving…" : "Record"}
        </button>
      </div>
    </form>
  );
}

// T098: "Remaining matches for [player]" panel. After a kiken decision lands,
// look up every scheduled match where the just-withdrawn player still appears
// and offer a one-click "Award default win to opponent" for each. The button
// calls /decision with decision=fusenpai and decisionBy=<the withdrawn side>:
// note: that's the side the WITHDRAWN player occupies in THAT match, not
// the side they had in the originating match (sides can flip across matches).
function RemainingMatchesPanel({ compID, password, withdrawnPlayer, onAwarded, onClose }) {
  const [matches, setMatches] = useStateA(null);
  const [err, setErr] = useStateA("");
  const [busyId, setBusyId] = useStateA("");
  const mountedRef = useRefA(true);
  const playerName = withdrawnPlayer?.name || "player";

  useEffectA(() => {
    return () => { mountedRef.current = false; };
  }, []);

  useEffectA(() => {
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(compID);
        if (cancelled) return;
        // compMatchesForCompetition, NOT compMatches(detail): the detail
        // response keeps the competition's identity under `config` and its
        // match data as siblings, and compMatches needs one object with both.
        // Passing `detail` straight in returned [] on every format, so this
        // panel listed no matches to award after a kiken (mp-dej2).
        const all = window.compMatchesForCompetition
          ? window.compMatchesForCompetition(detail.config || detail, detail)
          : [];
        const matchesForPlayer = all.filter(m => {
          if (m.status !== "scheduled") return false;
          return sameCompetitor(m.sideA, withdrawnPlayer) || sameCompetitor(m.sideB, withdrawnPlayer);
        });
        setMatches(matchesForPlayer);
      } catch (e) {
        if (!cancelled) setErr(e?.message || "Failed to load matches");
      }
    })();
    return () => { cancelled = true; };
  }, [compID, withdrawnPlayer?.id, withdrawnPlayer?.name]);

  const award = async (m) => {
    // Figure out which side the withdrawn player occupies in THIS match:
    // that's the side that gets the default loss. Pool matches: sideA = Aka,
    // sideB = Shiro. Same wire mapping in bracket matches.
    const isOnA = sameCompetitor(m.sideA, withdrawnPlayer);
    const barredKey = isOnA ? "a" : "b";
    setBusyId(m.id);
    // Clear any previous verdict before this attempt. Without it the panel's
    // error is sticky: a refusal on match A stays on screen while the operator
    // successfully awards match B, so the panel reports a failure that belongs
    // to a match no longer in the list. The catch arm below had the same
    // defect, so this covers both rather than only the new branch.
    setErr("");
    try {
      // bc-rawm: fusensho, not fusenpai. This panel exists BECAUSE the
      // operator just recorded a withdrawal for this competitor, so every
      // OTHER scheduled match of theirs is against an already-ineligible
      // competitor: fusenpai there is refused with 409 already_ineligible
      // (the concurrent-kiken guard). fusensho is the per-bout default win:
      // it writes no ineligibility of its own, so it never trips that guard
      // (engine.TestRecordDecision_FusenshoSkipsConcurrentCheck). decisionBy
      // is unchanged: it still names the WITHDRAWN competitor's side, the
      // side that gets the default loss, exactly as fusenpai's did.
      // bc-cse: the body itself has ONE owner (ineligible_match.jsx), keyed
      // directly by the barred side this panel already knows, so a default
      // win's wire shape cannot drift between this panel and the other
      // surfaces that build it off the server's `ineligibleSides` stamp.
      const updated = await window.API.recordDecision(
        m.compId || compID, m.id, defaultWinDecisionBodyForSide(barredKey, withdrawnPlayer), password,
      );
      if (!mountedRef.current) return;
      // A default win the server REFUSED must not leave the list: dropping it
      // here would hide the one match the operator still has to resolve, and
      // nothing else on this panel would ever mention it again (bc-lww1).
      // notLandedBanner, not writeDidNotLand, is the right ask: a QUEUED
      // award lands on reconnect, so an offline court must keep walking the
      // list exactly as it does today (notLandedBanner is null for a queued
      // write, same as writeWasSuperseded's old narrower check was). LIVE
      // since mp-jnvl stamped the /decision write (see recordDecision,
      // api_client.jsx): the server can genuinely answer with applied:false
      // now, so this is no longer a defence against a shape it could not yet
      // send. bc-cse: notLandedBanner over writeWasSuperseded alone -- that
      // predicate is TRUE for both the superseded AND the clock_skew
      // refusal, so it always showed the superseded copy even when the
      // clock, not a newer result, was the reason.
      const banner = notLandedBanner(updated);
      if (banner) {
        setErr(`Not saved: ${banner.reason}. ${banner.advice}`);
        return;
      }
      // Drop the awarded match from the list so the operator can keep walking.
      setMatches(prev => (prev || []).filter(x => x.id !== m.id));
      if (typeof onAwarded === "function") onAwarded(updated);
    } catch (e) {
      if (!mountedRef.current) return;
      setErr(e?.message || "Failed to award default win");
    } finally {
      if (mountedRef.current) setBusyId("");
    }
  };

  return (
    <div className="remaining-matches" style={{ border: "1px solid var(--line)", borderRadius: 6, padding: 12, marginTop: 12, background: "var(--bg-2)" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
        <div style={{ fontSize: 13, fontWeight: 700 }}>Remaining matches for {playerName}</div>
        {onClose && <button type="button" className="btn btn--ghost btn--sm" onClick={onClose} style={{ padding: "2px 8px" }}>✕</button>}
      </div>
      {err && <div style={{ color: "var(--danger)", fontSize: 12, marginBottom: 6 }}>{err}</div>}
      {matches === null && <div style={{ fontSize: 12, color: "var(--ink-3)" }}>Loading…</div>}
      {matches !== null && matches.length === 0 && (
        <div style={{ fontSize: 12, color: "var(--ink-3)" }}>No remaining scheduled matches.</div>
      )}
      {matches && matches.length > 0 && (
        <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: 6 }}>
          {matches.map(m => {
            const isOnA = sameCompetitor(m.sideA, withdrawnPlayer);
            const opponent = isOnA ? m.sideB : m.sideA;
            return (
              <li key={m.id} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, fontSize: 12 }}>
                <div>
                  <span style={{ fontWeight: 600 }}>{opponent?.name || "?"}</span>
                  <span style={{ color: "var(--ink-3)", marginLeft: 6 }}>
                    {/* window.poolLabel, not a raw m.poolName: these rows come
                        straight off window.compMatches, so a Swiss match carries
                        the synthetic "Swiss-R1" engine name and a league match
                        carries a pool name it does not have. poolLabel is the one
                        owner of that translation (mp-dej2); the sibling panels in
                        admin_schedule_page / admin_scoring_engi / admin_scoring_team
                        already use it. */}
                    {m.phase === "pool" ? window.poolLabel(m) : m.round}{m.court ? ` · Shiaijo ${m.court}` : ""}{m.scheduledAt ? ` · ${m.scheduledAt}` : ""}
                  </span>
                </div>
                <button type="button"
                  className="btn btn--sm"
                  onClick={() => award(m)}
                  disabled={busyId === m.id}
                  title="Record fusensho: opponent receives the default win"
                >
                  {busyId === m.id ? "Saving…" : "Award default win to opponent"}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

// BarredMatchNotice lives in its own leaf (barred_match_notice.jsx): it
// imports only ineligible_match.jsx and write_result.jsx, so a surface that
// does not otherwise need this module's graph (which pulls in bracket.jsx
// for sideMarks) can import it directly without that graph riding along --
// see that file's header. Re-exported here so the editors' existing
// `from './admin_scoring_shared.jsx'` import lists need no change.

// sideColorName: the human-readable side name for a "shiro"/"aka" colour key.
// Named for the colour it takes so it cannot be confused with admin_helpers.jsx's
// sideName(side), which takes a side object and returns a participant name.
// Used in aria-labels only: the foul counter's two buttons below and the
// individual editor's slot grid. The sr-only side labels come from SideLabel
// (side_cell.jsx), never from here. (It named the header badges until bc-sccl
// removed them; a reader tracing the badge rule from here would look for
// markup that no longer exists.)
function sideColorName(color) {
  // Delegates: side_cell.jsx owns the word, so the two can never disagree.
  return sideWord(color);
}

// Reusable foul counter: independent +/- buttons per side with clear labeling.
// The `+` button delegates to `onIncrement` which applies the
// applyFoulIncrement rule (auto-award H + reset at the 2-foul boundary);
// `setFouls` is kept for the `−` button (simple decrement). After the
// 2-foul auto-award the awarded H lives in the opponent's pts array, so
// the counter shows only "outstanding fouls not yet discharged."
function FoulCounter({ fouls, setFouls, onIncrement, color, disabled }) {
  // color is "shiro" or "aka": surface as data-testid so Playwright probes
  // (T023a) can target each side without depending on the className.
  // `disabled` freezes the `+` button when the bout is already decided:
  // a 2nd-foul auto-award in that state would create an invalid 2-2.
  // In the individual editor the two counters sit BELOW the board in
  // .sb-fouls, left = Shiro, right = Aka (the app-wide column convention),
  // with the Aka box tinted. The visible label names no side (it is
  // conveyed by position and tint alone, matching the bout rows above), so
  // "Fouls" stays unprefixed; the aria-labels below carry the side for
  // assistive tech.
  return (
    <div className={`foul-counter foul-counter--${color}`} data-testid={`scoring-modal-hansoku-${color}`}>
      <div className="foul-counter__label">Fouls</div>
      <div className="foul-counter__controls">
        <button type="button" className="foul-counter__btn foul-counter__btn--dec" aria-label={`Remove a ${sideColorName(color)} foul`} onClick={() => setFouls(Math.max(0, fouls - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 1 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
        </div>
        <button type="button" className="foul-counter__btn foul-counter__btn--inc" aria-label={`Add a ${sideColorName(color)} foul`} onClick={onIncrement} disabled={disabled}>+</button>
      </div>
    </div>
  );
}

// LineupNameInput: typeable player picker/writer for a team lineup slot.
// Type to filter the team roster (matches show in a dropdown); click one or
// press Enter to pick. Unlike a plain picker, a name not on the roster can be
// ADDED as-is via the "+ Add …" row: the lineup stores a free name string,
// which is what an operator needs when entering a late substitute while bouts
// are running. The × clears the slot. Keyboard: ↑/↓ move, Enter picks, Esc closes.
//
// roster entries are either plain name strings (admin_schedule_lineup.jsx's
// suggestion list) or squad-member objects `{ id, index, name, label }`
// (admin_scoring_team.jsx's rosterForSide, bc-dnst: the row's list must show
// every numbered slot the team can field, blank ones included, so the
// operator can pick a number and name it). Both shapes are normalised to
// `{ name, label, isObject }` once up front; isObject is what a plain
// string entry never gets, so it alone decides whether the dropdown row
// renders a number chip. Picking an object entry threads the WHOLE entry
// through onSelect as a second argument (`onSelect(name, entry)`); a plain
// string match, the "+ Add" row, and a typed-query commit call onSelect
// with the name alone, exactly as before.
// `clearable` (bc-dnst): show the clear button even with an empty value, for a
// host whose position is occupied by a picked squad slot that has no name yet
// (the Up Next lineup panel); without it a nameless placement had no way out.
function LineupNameInput({ value, roster, onSelect, disabled, ariaLabel, color, clearable }) {
  const [query, setQuery] = useStateA("");
  const [open, setOpen] = useStateA(false);
  const [active, setActive] = useStateA(-1); // -1 = no explicit selection yet
  const ref = useRefA(null);
  // Guards against double-commit when click-outside fires first and the blur
  // event arrives immediately after (mousedown precedes blur in browser order).
  const skipBlurRef = useRefA(false);
  const q = query.trim();
  const ql = q.toLowerCase();
  // `raw` keeps the ORIGINAL roster item (unmodified: no isObject marker,
  // no spread copy) so a caller picking an object entry gets back exactly
  // what it put into `roster`, never an internal-shape lookalike. Memoised
  // (bc-rvfx) on `roster` alone: this component is mounted once per lineup
  // row and its own `query` state changes on every keystroke, which used to
  // re-map the whole roster on every one of those renders even though
  // `roster` itself had not changed.
  const entries = useMemoA(() => (roster || []).map(r =>
    typeof r === "string"
      ? { name: r, label: "", isObject: false, raw: r }
      : { name: r?.name || "", label: r?.label || "", isObject: true, raw: r }
  ), [roster]);
  // matches/exact still recompute on every keystroke (ql is one of their own
  // dependencies), but no longer on a re-render this component causes for
  // itself where neither `entries` nor `ql` changed -- arrow-key navigation
  // (`active`) and open/close (`open`) are the common case, and previously
  // re-filtered and re-capped the whole roster on each of those too.
  const { matches, exact } = useMemoA(() => {
    const matches = entries.filter(e => {
      if (!ql) return true;
      const nameHit = (e.name || "").toLowerCase().includes(ql);
      const labelHit = (e.label || "").toLowerCase().includes(ql);
      return nameHit || labelHit;
    });
    // Capped only while FILTERING. With no query the operator is BROWSING this
    // team's own numbered slots, and every one has to be reachable: the list
    // grew from "the team's named members" to every seeded slot (team size plus
    // two reserves, bc-dnst) plus any assigned-name tail, so a flat cap of 12
    // silently hid the highest slots on an 11-person team -- exactly the reserve
    // slots the +2 exists to expose, and the operator could only reach them by
    // guessing that typing narrows the list. The dropdown scrolls, so showing
    // the whole of a team's own roster costs nothing; a QUERY can still match
    // the long legacy tail, which is what the cap is for.
    if (ql) matches.splice(12);
    // Scans `entries`, not the capped `matches`: an exact match past the cap
    // must still count as "already on the roster", or the "+ Add" row would
    // offer to add a name that is already there, just scrolled out of view.
    const exact = entries.some(e => (e.name || "").toLowerCase() === ql);
    return { matches, exact };
  }, [entries, ql]);
  const canAddNew = q.length > 0 && !exact;
  const optionCount = matches.length + (canAddNew ? 1 : 0);

  // On click-outside: commit a typed but un-submitted name rather than
  // discarding it. Without this, tabbing quickly between slots loses names.
  // Note: option onMouseDown uses preventDefault so the outside mousedown only
  // fires when clicking a genuinely external target (q is already "" after commit).
  window.useClickOutside(ref, () => {
    if (q) {
      skipBlurRef.current = true;
      commit(q);
    } else {
      setOpen(false);
      setQuery("");
    }
  }, open);

  // commit's optional second argument is the matched roster entry (when the
  // commit came from picking one). It is called with ONE argument (never a
  // trailing explicit `undefined`) for the "+ Add" row, a typed-query
  // commit, and the clear button -- calling onSelect(name, undefined)
  // there instead would change every existing single-arg
  // `onSelect(name)`/`toHaveBeenCalledWith(name)` consumer's call shape
  // (Vitest's toHaveBeenCalledWith does not ignore a trailing explicit
  // undefined), so this stays a real arity difference, not a value one.
  const commit = (name, entry) => {
    if (entry !== undefined) onSelect(name, entry);
    else onSelect(name);
    setOpen(false); setQuery(""); setActive(-1);
  };
  // A string-origin entry commits exactly like a plain string always did
  // (ONE argument); an object entry threads its ORIGINAL raw value back as
  // onSelect's second argument.
  const commitEntry = (entry) => (
    entry.isObject ? commit(entry.name || "", entry.raw) : commit(entry.name)
  );
  const onKeyDown = (e) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      // First ArrowDown opens the list highlighting the FIRST option (index 0);
      // subsequent presses move down. Without the open-guard the first row is
      // skipped (jumps straight to index 1).
      if (!open) { setOpen(true); setActive(0); }
      else setActive(a => Math.min(a + 1, Math.max(0, optionCount - 1)));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(a => Math.max(a - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      // Commit only on an explicit choice: a navigated list row, the add-row,
      // or a typed query. A bare focus + Enter (active === -1, empty query)
      // must NOT overwrite the current slot.
      if (active >= 0 && active < matches.length) commitEntry(matches[active]);
      else if (active === matches.length && canAddNew) commit(q);
      else if (q) commit(q);
    } else if (e.key === "Escape") { e.preventDefault(); setOpen(false); setQuery(""); }
  };

  return (
    <div className={`pmf lineup-name lineup-name--${color}${!value ? " lineup-name--empty" : ""}`} ref={ref}>
      <div className="pmf__bar lineup-name__bar">
        <input
          className="pmf__input"
          placeholder={value || "Add player…"}
          aria-label={ariaLabel}
          disabled={disabled}
          value={open ? query : (value || "")}
          onChange={(e) => { setQuery(e.target.value); setOpen(true); setActive(-1); }}
          onFocus={() => { setOpen(true); setQuery(""); setActive(-1); }}
          onKeyDown={onKeyDown}
          onBlur={(e) => {
            // Do not close if focus moved to something inside the wrapper
            // (e.g. a dropdown option button).
            if (ref.current && ref.current.contains(e.relatedTarget)) return;
            // Skip if the click-outside path already committed (it fires on
            // mousedown, before blur; it sets skipBlurRef to prevent a double
            // onSelect call).
            if (skipBlurRef.current) { skipBlurRef.current = false; return; }
            // Tab-away with a typed name: commit it so the operator does not
            // lose a batch-entry value.
            if (q) commit(q);
            else if (open) { setOpen(false); setQuery(""); }
          }}
        />
        {(value || clearable) && !disabled && (
          <button type="button" className="lineup-name__clear" title="Clear player" aria-label="Clear player"
            onMouseDown={(e) => { e.preventDefault(); commit(""); }}>×</button>
        )}
      </div>
      {open && optionCount > 0 && (
        <div className="pmf__dropdown lineup-name__dropdown">
          {matches.map((entry, i) => (
            <button type="button" key={entry.isObject ? (entry.raw?.id || entry.raw?.index) : entry.name}
              className={`pmf__option ${i === active ? "pmf__option--active" : ""}`}
              onMouseDown={(e) => { e.preventDefault(); commitEntry(entry); }}>
              {entry.isObject ? (
                <>
                  <span className="pmf__opt-label">{entry.label}</span>
                  <span className={`pmf__opt-name${entry.name ? "" : " pmf__opt-name--blank"}`}>{entry.name || "no name yet"}</span>
                </>
              ) : (
                <span className="pmf__opt-name">{entry.name}</span>
              )}
            </button>
          ))}
          {canAddNew && (
            <button type="button"
              className={`pmf__option lineup-name__add ${active === matches.length ? "pmf__option--active" : ""}`}
              onMouseDown={(e) => { e.preventDefault(); commit(q); }}>
              <span className="pmf__opt-name">+ Add “{q}”</span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ReasonPrompt: inline form for collecting a mandatory audit justification
// when an operator corrects a completed match or edits a lineup mid-match.
//
// Renders a preset <select> + optional free-text note; calls onConfirm with
// the combined "<category>: <note>" string (or just "<category>" when note is
// empty). Calls onCancel to dismiss without submitting.
//
// presets: array of string labels (e.g. ["Scoring error","Wrong competitor",…])
// Registered as window.ReasonPrompt for use from other modules.
// Module-level empty default keeps the prop reference stable across renders
// (a `= []` inline default allocates a fresh array every call).
const REASON_PROMPT_NO_PRESETS = [];
function ReasonPrompt({ label = "Reason for change", presets = REASON_PROMPT_NO_PRESETS, onConfirm, onCancel, submitting = false }) {
  const [category, setCategory] = useStateA(presets[0] || "");
  const [note, setNote] = useStateA("");
  const built = note.trim() ? `${category}: ${note.trim()}` : category;
  const canConfirm = !!category && !submitting;
  const submit = (e) => {
    e.preventDefault();
    if (!canConfirm) return;
    onConfirm(built);
  };
  return (
    <form
      className="reason-prompt"
      onSubmit={submit}
      style={{ border: "1px solid var(--line)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2)" }}
    >
      <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>{label}</div>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <select
          className="input"
          style={{ flex: "0 0 auto", minWidth: 160 }}
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          disabled={submitting}
          aria-label="Reason category"
        >
          {presets.map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <input
          className="input"
          style={{ flex: "1 1 160px", minWidth: 100 }}
          type="text"
          placeholder="Optional note…"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          disabled={submitting}
          aria-label="Reason note"
        />
      </div>
      <div style={{ display: "flex", gap: 8, marginTop: 8, justifyContent: "flex-end" }}>
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={submitting}>Cancel</button>
        <button type="submit" className="btn btn--sm btn--primary" disabled={!canConfirm}>
          {submitting ? "Saving…" : "Confirm"}
        </button>
      </div>
    </form>
  );
}

window.ReasonPrompt = ReasonPrompt;

const CORRECTION_PRESETS = ["Scoring error", "Wrong competitor", "Data entry", "Other"];

// Presets for the reason collected by Clear withdrawal and reopen. The honest
// first answer for clearing a withdrawal is that it was entered by mistake, so
// it leads and is the default; the correction vocabulary follows, DERIVED from
// CORRECTION_PRESETS rather than restated so the two cannot drift.
const WITHDRAWAL_REOPEN_PRESETS = ["Withdrawal recorded by mistake", ...CORRECTION_PRESETS];

// bc-cse: the sibling set for clearing a match-level FUSENSHO (a default win
// awarded because the OTHER competitor was barred elsewhere), which is not a
// withdrawal on THIS match at all, so "Withdrawal recorded by mistake" is the
// wrong first answer for it -- derived the same way, first entry swapped.
const DEFAULT_WIN_REOPEN_PRESETS = ["Default win recorded by mistake", ...CORRECTION_PRESETS];

// withdrawalLabel: the operator's name for a match-level withdrawal decision,
// the ONE copy of it: DecisionPrompt's title and the Recorded line in both
// editors read it. Any kiken that is not the injury kind reads as voluntary,
// the legacy bare "kiken" included, which the server loads as voluntary too.
function withdrawalLabel(decision) {
  if (decision === "fusenpai") return "Fusenpai";
  // bc-rawm: a match-level fusensho, the shape RemainingMatchesPanel.award
  // writes for a scheduled match against an already-withdrawn competitor
  // (see the file header there). Short label for the same "fact" slots
  // kiken/fusenpai use (e.g. admin_scoring_team.jsx's team-summary-decision);
  // the fuller "Default win (fusensho) for <winner>" sentence is composed in
  // RecordedWithdrawal, which needs the winner's name this function does not
  // have.
  if (decision === "fusensho") return "Default win (fusensho)";
  return decision === "kiken-injury" ? "Kiken – Injury" : "Kiken – Voluntary";
}

// withdrawnSideOf: the side a recorded withdrawal names as the one that
// withdrew (or did not appear), or null when the match cannot say.
// decisionBy is authoritative (aka = sideA, shiro = sideB, the server's own
// mapping in RecordDecisionTx). A ruling without it falls back to the side the
// recorded winner is NOT, attributed by sameCompetitor (id first), never by a
// bare name comparison.
function withdrawnSideOf(m) {
  const key = withdrawnKeyOf(m);
  if (key === "a") return m.sideA || null;
  if (key === "b") return m.sideB || null;
  return null;
}

// withdrawnKeyOf: the same answer as withdrawnSideOf, as the editors' side key
// ("a" = Aka/sideA, "b" = Shiro/sideB), or "" when the match cannot say.
function withdrawnKeyOf(m) {
  return withdrawnSideKey(m);
}

// withdrawalInForce: the one statement of "a recorded withdrawal is in force
// on this correction": the match is completed and a withdrawal (any kiken, or
// fusenpai) decided it. Both editors ask it: it gates RecordedWithdrawal, and
// the individual editor locks the winner's default-win maru while it holds
// (Save correction keeps the withdrawal and its maru, engine
// keptWithdrawalScoreline, so the maru is not the operator's to edit). It stops
// holding the moment the withdrawal is cleared, because the reopen puts the
// match back to running.
function withdrawalInForce(m) {
  // bc-rawm: widened to the default-win class. A match-level fusensho is
  // RemainingMatchesPanel.award's shape for a SCHEDULED match against a
  // competitor already withdrawn elsewhere (fusenpai is refused there with
  // 409 already_ineligible, which is why that panel writes fusensho
  // instead): same default-win outcome as a kiken/fusenpai on this match, so
  // it belongs in the same "a recorded withdrawal decided this" class as
  // kiken/fusenpai, not a separate one. So this asks the match-level
  // default-win class, whose one JS owner is isTeamDefaultWinDecision
  // (team_default_credit.jsx), rather than ORing a fusensho arm onto a
  // narrower kiken/fusenpai check.
  return m.status === "completed" && isTeamDefaultWinDecision(m.decision);
}

// useMatchReopen: the one client of POST .../reopen and its court-busy remedy
// POST .../requeue-blocker-and-reopen, for every editor that reopens a match:
// the kachinuki Reopen (one tap, no reason) and Clear withdrawal and reopen in
// the individual and team editors (with the reason collected first). It was
// the kachinuki editor's own state machine; lifting it here is what lets the
// individual editor offer the same door without a second copy of it.
//
// Three outcomes, each of which the returned state carries:
//   - success: the editor STAYS OPEN and follows the match to running in
//     place, for both doors (the kachinuki Reopen and Clear withdrawal and
//     reopen): `notice`, which names any later match the reopen also
//     reopened, is only readable in an editor that is still there. busy
//     clears on the success response and `landed` is set, so the button says
//     the reopen landed rather than "Reopening…" even when the match_updated
//     broadcast that flips the prop never arrives, while a double tap still
//     cannot post a second reopen the server would 409 as "not completed".
//     The effect below clears `landed` once the match is running;
//   - court_busy: `conflict` names the match holding the court, and
//     requeueBlocker() is the remedy (it carries the same reason and, once
//     given, the same downstream confirmation);
//   - a later knockout match with its own result (downstream_knockout_played):
//     attemptScoreWrite, the score path's confirm-and-retry loop, asks the
//     operator with the same dialog a correction gets and retries with the
//     force flag (operator ruling 2026-09-24: "The operator just needs to be
//     aware of the consequences"). What that reopened is named in `notice`;
//     declining leaves everything as it was and says so in `err`.
// Every other refusal is the server's sentence, shown verbatim in `err`.
function useMatchReopen({ match, password, isComplete }) {
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  const [busy, setBusy] = useStateA(false);
  // The server answered success; the match prop may not have flipped yet.
  const [landed, setLanded] = useStateA(false);
  const [err, setErr] = useStateA("");
  // { court, matchId, compId, message, label, reason, force }: the match
  // ALREADY running on this court, plus what the refused reopen carried.
  // `label` is the server's operator-facing name for it, when sent -- it
  // names no COMPETITOR (bc-cse), so ReopenFeedback prefers the fetched
  // blockerLabel below over it. `message` is the server's raw refusal
  // sentence, kept on the object but no longer rendered (see ReopenFeedback:
  // it named an internal id and told the operator to finish that match,
  // contradicting this panel's own button).
  const [conflict, setConflict] = useStateA(null);
  // Best-effort "Match 2 · Shiro vs Aka" for the blocking match. The operator
  // is about to wipe that match's score, so naming its competitors (not just
  // the server's opaque id) is a safety property, not decoration.
  const [blockerLabel, setBlockerLabel] = useStateA("");
  const [notice, setNotice] = useStateA("");
  // Set once the operator has confirmed the downstream reopen, so the
  // court-busy remedy that may follow does not ask them a second time.
  const confirmedRef = useRefA(false);

  // Once the match reopens (status flips to running), a stale error or
  // conflict describes a completed-state action that no longer applies. When it
  // completes AGAIN (a new result or decision), the reopen notice is history:
  // it described that reopen, not the match now on screen. Keyed on the value,
  // so the notice set while the match is still completed survives the flip.
  useEffectA(() => {
    if (!isComplete) { setErr(""); setConflict(null); setBusy(false); setLanded(false); }
    else setNotice("");
  }, [isComplete]);

  const applyFailure = (e, reason) => {
    if (e && e.downstreamKnockoutPlayedCancelled) {
      setConflict(null);
      setErr(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED);
      return;
    }
    const msg = String(e?.message || "Failed to reopen match");
    if (e?.code === "court_busy" && e?.matchId) {
      setErr("");
      setConflict({
        court: e.court || match.court || "",
        matchId: e.matchId,
        compId: e.compId || match.compId,
        message: msg,
        // The server's own operator label for the blocking match
        // (api_client.jsx's reopenFailureError carries it through when
        // present). bc-cse: it names no competitor ("Pool A · Match 2"), so
        // ReopenFeedback prefers the best-effort FETCHED blockerLabel over
        // it and falls back to this only when that fetch has not resolved;
        // either way the heading never falls back to printing the raw
        // matchId.
        label: e.label || "",
        reason,
        force: confirmedRef.current,
      });
      return;
    }
    setConflict(null);
    setErr(msg);
  };

  const run = (call, reason, force) => attemptScoreWrite({
    recordScore: (_compId, _matchId, result, pw) => call(pw, { reason, force: !!result.forceDownstreamReopen }),
    confirmDialog: async (opts) => {
      const ok = await window.confirmDialog(opts);
      if (ok) confirmedRef.current = true;
      return ok;
    },
    compId: match.compId,
    matchId: match.id,
    result: force ? { forceDownstreamReopen: true } : {},
    password: resolveDecisionPassword(password),
    match,
  });

  const finish = (res) => {
    const reopened = downstreamKnockoutReopenedNotice(res && res.downstreamReopened);
    if (reopened) setNotice(reopened);
    setBusy(false);
    setLanded(true);
  };

  const reopen = async (reason = "") => {
    if (busy || landed) return;
    setErr("");
    setConflict(null);
    setNotice("");
    confirmedRef.current = false;
    setBusy(true);
    try {
      const res = await run((pw, opts) => window.API.reopenMatch(match.compId, match.id, pw, opts), reason, false);
      if (!mountedRef.current) return;
      finish(res);
    } catch (e) {
      if (!mountedRef.current) return;
      applyFailure(e, reason);
      setBusy(false);
    }
  };

  // The remedy: send the blocking match back to the queue and reopen this one,
  // in ONE server call under one court lock (mp-gmcg review A4). DESTRUCTIVE:
  // revert-to-queue clears that match's partial score, which is why the panel
  // spells the consequence out before this can be tapped.
  const requeueBlocker = async () => {
    const c = conflict;
    if (!c || busy || landed) return;
    setErr("");
    setBusy(true);
    try {
      const res = await run(
        (pw, opts) => window.API.requeueBlockerAndReopen(match.compId, match.id, c.compId, c.matchId, pw, opts),
        c.reason, c.force,
      );
      if (!mountedRef.current) return;
      setConflict(null);
      finish(res);
    } catch (e) {
      if (!mountedRef.current) return;
      // One atomic call, so one failure path: a DIFFERENT match that has since
      // taken the court is offered the remedy again; anything else is shown.
      applyFailure(e, c.reason);
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  useEffectA(() => {
    setBlockerLabel("");
    if (!conflict?.matchId || !conflict?.compId) return;
    if (typeof window.compMatchesForCompetition !== "function" || typeof window.API?.fetchCompetitionDetails !== "function") return;
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(conflict.compId);
        if (cancelled || !mountedRef.current) return;
        // compMatchesForCompetition (viewer_utils.jsx) owns the recombination
        // of the details payload's config with its matches (mp-dej2).
        const hit = (window.compMatchesForCompetition
          ? window.compMatchesForCompetition(detail.config || detail, detail)
          : []).find(x => x.id === conflict.matchId);
        if (!hit) return;
        const shiro = hit.sideB?.name || hit.sideB || "";
        const aka = hit.sideA?.name || hit.sideA || "";
        // bc-rawm: a POOL match has no matchNumber (bracket matches alone are
        // numbered), so it fell through to the bare "Shiro vs Aka" -- correct
        // but silent about which pool, unlike every bracket blocker, which
        // names its round via matchNumber's "Match N". window.poolLabel is
        // the one owner of that pool-name translation (mp-dej2; the same
        // helper RemainingMatchesPanel above uses), so a pool blocker now
        // leads with it exactly as a bracket blocker leads with its number.
        const lead = hit.phase === "pool" && typeof window.poolLabel === "function"
          ? window.poolLabel(hit)
          : (hit.matchNumber ? `Match ${hit.matchNumber}` : "");
        const label = [
          lead,
          shiro && aka ? `${shiro} vs ${aka}` : "",
        ].filter(Boolean).join(" · ");
        if (label) setBlockerLabel(label);
      } catch { /* best effort: the panel falls back to the server's own label, or "another match" */ }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conflict?.compId, conflict?.matchId]);

  return { busy, landed, err, conflict, blockerLabel, notice, reopen, requeueBlocker, dismissConflict: () => setConflict(null) };
}

// ReopenFeedback: what a reopen made through useMatchReopen came back with,
// rendered ONCE per editor, outside the control that started the reopen
// (testIdPrefix names the door). It must outlive that control: Clear
// withdrawal and reopen (RecordedWithdrawal) unmounts the moment the match is
// running again, which is exactly when its `notice` has something to say, so
// a copy inside it could never be seen. The court-busy panel's
// warning is NOT optional chrome: the remedy WIPES the blocking match's partial
// score, and the operator tapping it is looking at their own match, not that
// one, so the consequence is stated in full above the button.
function ReopenFeedback({ ctl, testIdPrefix }) {
  const c = ctl.conflict;
  return (
    <>
      {ctl.notice && (
        <div role="status" data-testid={`${testIdPrefix}-notice`} style={{ fontSize: 12, color: "var(--ink-2)", marginBottom: 6 }}>{ctl.notice}</div>
      )}
      {ctl.err && (
        <div data-testid={`${testIdPrefix}-error`} style={{ color: "var(--danger)", fontSize: 12, marginBottom: 6 }}>{ctl.err}</div>
      )}
      {c && (
        <div className="alert alert--error reopen-conflict" data-testid={`${testIdPrefix}-conflict`}>
          {/* bc-cse: the best-effort FETCHED blockerLabel wins over the
              server's own `label` -- the server always sends one, but it is
              "Pool A · Match 2" with no names, never the "Match 2 · Shiro vs
              Aka" this panel is about to wipe the score of; that safety
              property only exists in the fetched label. Only when the fetch
              cannot resolve it (still loading, or a DIFFERENT match has since
              taken the court) does the server's label stand in, and only
              "another match" when neither is available. Reuses
              courtBusyMessage (write_result.jsx) rather than repeating its
              sentence here. The raw c.message line that used to sit below
              this heading is gone -- it named the internal id and told the
              operator to "finish that match before reopening", the opposite
              of the remedy this panel's own button offers. */}
          <div className="reopen-conflict__head">
            {courtBusyMessage({ court: c.court, label: ctl.blockerLabel || c.label || "another match" })}
          </div>
          <div className="reopen-conflict__warn" data-testid={`${testIdPrefix}-conflict-warning`}>
            Sending it back to the queue clears any score already entered for it. Finishing that
            match instead keeps its score.
          </div>
          <div className="reopen-conflict__actions">
            <button
              type="button"
              className="btn btn--sm btn--danger"
              data-testid={`${testIdPrefix}-requeue-button`}
              onClick={ctl.requeueBlocker}
              disabled={ctl.busy}
              title="Clears that match's score, returns it to the queue, then reopens this one"
            >
              {ctl.busy ? "Working…" : "Clear its score, queue it, and reopen"}
            </button>
            <button
              type="button"
              className="btn btn--sm btn--ghost"
              data-testid={`${testIdPrefix}-conflict-dismiss`}
              onClick={ctl.dismissConflict}
              disabled={ctl.busy}
            >
              Leave it running
            </button>
          </div>
        </div>
      )}
    </>
  );
}

// RecordedWithdrawal: what a correction of a withdrawal-decided match shows
// about the withdrawal, and the one way to remove it, identical in the
// individual and team editors (operator ruling 2026-09-24: "Everything should
// be able to be fixed, in case of a wrong entry").
//
// Clearing a withdrawal is a REOPEN, not a score write: a withdrawal means the
// opponent received the default score, so without it the match was never
// decided. The match goes back to running with what was fought kept and the
// withdrawn side eligible again, and the operator scores the rest and
// finishes it normally (engine.ReopenMatch). The reason is asked for BEFORE
// the reopen posts and rides it, with the consequence spelled out above it
// (operator ruling 2026-09-24: the operator "just needs to be aware of the
// consequences"). On a single bout (singleBout: the individual editor, which
// also scores a team's -DH-/-TB- rep bout) the consequence names the one
// thing the reopen cannot keep, the winner's points, which the recorded
// withdrawal had already replaced with the default win.
//
// A kachinuki encounter a withdrawal decided renders this too, in place of
// its plain one-tap Reopen, so a withdrawal has one control and one
// consequence text in every editor; the plain Reopen stays for every other
// kachinuki result. Switching the withdrawal to the other side stays with the
// editor's own withdrawal controls. What the reopen came back with (the
// notice, an error, the court-busy remedy) is rendered by the editor through
// ReopenFeedback, never here: this unmounts as soon as the match is running.
//
// A POOL match of a pools-then-knockout competition adds one line: finishing
// the reopened match may change who qualifies from its pool, and the save
// that finishes it shows which knockout matches that affects before anything
// is saved (the server's qualifierChange refusal, confirmed through
// attemptScoreWrite like any other).
//
// bc-rawm: widened to the match-level DEFAULT-WIN class alongside kiken and
// fusenpai (withdrawalInForce above), for a match RemainingMatchesPanel.award
// closed with a fusensho because the competitor was already ineligible from
// an earlier withdrawal. Reads and the reopen remedy are otherwise identical;
// only the copy differs (the Recorded line names the winner, and "Clear
// default win and reopen" replaces "Clear withdrawal and reopen").
function RecordedWithdrawal({ match, ctl, disabled = false, singleBout = false }) {
  const [asking, setAsking] = useStateA(false);
  const withdrawnKey = withdrawnKeyOf(match);
  const withdrawn = withdrawnSideOf(match);
  const who = withdrawn?.name || "";
  const winner = withdrawnKey === "a" ? match.sideB : withdrawnKey === "b" ? match.sideA : null;
  const winnerName = winner?.name || "";
  const isDefaultWin = match.decision === "fusensho";
  const what = match.decision === "fusenpai" ? "did not appear" : "withdrew";
  const feedsKnockout = match.phase === "pool" && match.compFormat === "mixed";

  // bc-cse: "Everything should be able to be fixed... the operator just needs
  // to be aware of the consequences" (operator ruling 2026-09-24) extends past
  // THIS match: if the withdrawn competitor has LATER matches RemainingMatchesPanel
  // already closed with a default win (fusensho, naming this competitor's side
  // as the one that withdrew), clearing THIS withdrawal does not touch those --
  // they keep their own recorded result and must be reopened separately to be
  // fought. Fetched the same way RemainingMatchesPanel finds them (fetch +
  // compMatchesForCompetition), only once the operator asks to clear (asking),
  // never eagerly. Best-effort: a fetch failure just omits the list rather than
  // blocking the reopen the operator came here to do.
  const [laterDefaultWins, setLaterDefaultWins] = useStateA(null);
  useEffectA(() => {
    if (!asking || !withdrawn || !match.compId) { setLaterDefaultWins(null); return; }
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(match.compId);
        if (cancelled) return;
        const all = window.compMatchesForCompetition
          ? window.compMatchesForCompetition(detail.config || detail, detail)
          : [];
        const hits = all.filter(x => (
          x.id !== match.id &&
          x.status === "completed" &&
          x.decision === "fusensho" &&
          ((x.decisionBy === "aka" && sameCompetitor(x.sideA, withdrawn)) ||
            (x.decisionBy === "shiro" && sameCompetitor(x.sideB, withdrawn)))
        ));
        if (!cancelled) setLaterDefaultWins(hits);
      } catch (_e) {
        if (!cancelled) setLaterDefaultWins([]);
      }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [asking, match.compId, match.id, withdrawn?.id, withdrawn?.name]);

  // bc-cse: whether the FUSENSHO's barred competitor (`withdrawn`, the side
  // THIS match's decisionBy names -- barred elsewhere, not by anything on
  // this match) is STILL barred, whether that withdrawal is reinstateable,
  // or whether they are eligible again already (reinstated, or their
  // earlier withdrawal cleared), so the confirm can offer the right remedy
  // instead of guessing. isDefaultWin-only: a kiken/fusenpai clear on this
  // match is an ordinary withdrawal, not another competitor's barring, so
  // it never needs this. window.API.fetchCompetitorStatuses always exists
  // in the app (api_client.jsx); best-effort is only about the FETCH, same
  // pattern as laterDefaultWins above -- unknown (fetch failed, or no
  // matching row) reads as "still barred, not reinstateable", the more
  // conservative of the wrong guesses.
  const [withdrawnStatus, setWithdrawnStatus] = useStateA(null);
  useEffectA(() => {
    if (!asking || !isDefaultWin || !withdrawn?.id || !match.compId) {
      setWithdrawnStatus(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const statuses = await window.API.fetchCompetitorStatuses(match.compId);
        if (cancelled) return;
        setWithdrawnStatus((statuses || []).find(s => s.playerId === withdrawn.id) || null);
      } catch (_e) {
        if (!cancelled) setWithdrawnStatus(null);
      }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [asking, isDefaultWin, match.compId, withdrawn?.id]);
  const canReinstate = !!(withdrawnStatus && withdrawnStatus.eligible === false && withdrawnStatus.reinstateable);
  // bc-cse: the barred competitor's own status record now reads eligible --
  // reinstated, or their earlier withdrawal cleared elsewhere -- so the
  // server reopens THIS match straight to running rather than the queue
  // (see the fusensho branch below).
  const isEligibleAgain = !!(withdrawnStatus && withdrawnStatus.eligible === true);

  return (
    <div className="decision-recorded" data-testid="recorded-withdrawal" style={{ marginTop: 10, fontSize: 13 }}>
      <div>
        {/* bc-rawm: a match-level fusensho (RemainingMatchesPanel.award) names
            the WINNER first -- "Default win (fusensho) for <winner>" -- so the
            operator sees who benefits from the same reading the wire's
            decisionBy already carries, then names who had withdrawn as a
            second sentence. withdrawalLabel cannot build this on its own: it
            takes only the decision string, not the winner's name. */}
        <span>
          {isDefaultWin
            ? `Recorded: Default win (fusensho) for ${winnerName || "the opponent"}. ${who || "The withdrawn competitor"} had withdrawn.`
            : `Recorded: ${withdrawalLabel(match.decision)}${who ? `, ${who} ${what}` : ""}.`}
        </span>
        {!asking && (
          <>
            {" "}
            <button
              type="button"
              className="btn btn--sm"
              data-testid="clear-withdrawal-reopen"
              onClick={() => setAsking(true)}
              disabled={disabled || ctl.busy || ctl.landed}
            >
              {/* bc-cse: "Clear default win" -- no "and reopen" -- because a
                  fusensho reopen no longer always lands running: the server
                  returns the match to SCHEDULED when the barred competitor
                  is still barred (BarredMatchNotice shows again), and only
                  to running once they no longer are, so this button cannot
                  promise "and reopen" for either outcome uniformly. */}
              {ctl.busy ? "Reopening…" : ctl.landed ? "Reopened" : isDefaultWin ? "Clear default win" : "Clear withdrawal and reopen"}
            </button>
          </>
        )}
      </div>
      {asking && (
        <>
          {isDefaultWin ? (
            // bc-cse: a fusensho names the OTHER competitor as barred, not
            // this match's own withdrawal, so clearing it does not simply
            // "reopen to running" -- the barred competitor is almost always
            // still barred (whatever withdrew them elsewhere is still on
            // record), so that is the default the copy states; only when a
            // live check finds them reinstateable does it also offer that
            // route. When the live check instead finds them ELIGIBLE
            // AGAIN (reinstated, or their earlier withdrawal cleared), the
            // server reopens this match straight to running, so the copy
            // says that instead of promising the queue.
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              {isEligibleAgain
                ? `${who || "The barred competitor"} can fight again, so the match reopens in progress. Score it and finish it as usual.`
                : <>
                  The match goes back to the queue. {who || "The barred competitor"} is still withdrawn, so
                  record the default win again{canReinstate ? `, or reinstate ${who || "them"} first to fight it` : ""}.
                </>}
            </p>
          ) : singleBout && match.decision === "fusenpai" ? (
            // A no-show fought nothing: there are no points to keep or lose,
            // so the reopen only removes the default win.
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running, the default win given
              to {winnerName || "the other side"} is removed, and {who || "the side marked absent"} can
              compete again. Then score the match and finish it.
            </p>
          ) : singleBout ? (
            // A single bout (an individual match, or a team -DH-/-TB- rep
            // bout) loses the WINNER's points on a reopen: recording the
            // kiken replaced them with the default win (recordDecisionTx)
            // and the reopen drops that verdict, so only what the withdrawing
            // side struck is still there to keep (engine singleBoutFightOf).
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running and {who || "the withdrawn side"} can
              compete again. {winnerName || "The winner"}&apos;s points were replaced by the default
              win when the withdrawal was recorded, so enter them again; {who ? `${who}'s` : "the withdrawn side's"} points
              are kept. Then score the rest and finish it.
            </p>
          ) : (
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running with what was fought kept,{" "}
              {who || "the withdrawn side"} can compete again, and you score the rest and finish it.
            </p>
          )}
          {feedsKnockout && (
            // A pool match of a pools-then-knockout competition feeds the
            // knockout through its standings, so the result it is finished
            // with may seat someone else there. The reopen itself moves
            // nobody; the finishing save is what is checked, and it names
            // any knockout match already fought before anything is saved.
            <p data-testid="clear-withdrawal-qualifier-note" style={{ margin: "6px 0 0" }}>
              Finishing it may change who qualifies from {match.poolName || "its pool"}. If that moves
              someone who has already fought in the knockout, you will be shown which knockout
              matches it affects before anything is saved.
            </p>
          )}
          {laterDefaultWins && laterDefaultWins.length > 0 && (
            <div data-testid="clear-withdrawal-default-win-consequences" style={{ margin: "6px 0 0" }}>
              {/* bc-cse: named by scoreRowMatchLabel first -- a pairing alone
                  cannot be found in the scores list, which is where the
                  operator has to go to reopen it -- with the pairing appended
                  the same way ReopenFeedback's own fetched blockerLabel joins
                  a lead onto a pairing ("Pool A · Match 2 · Shiro vs Aka"),
                  so the two operator lines that name a match this way agree.
                  Falls back to the bare pairing when the match carries no
                  number at all (see scoreRowMatchLabel's own doc). */}
              {laterDefaultWins.map((x) => {
                const label = scoreRowMatchLabel(x);
                const pairing = `${x.sideB?.name || "Shiro"} vs ${x.sideA?.name || "Aka"}`;
                return (
                  <p key={x.id} data-testid={`clear-withdrawal-default-win-${x.id}`} style={{ margin: "4px 0 0" }}>
                    {label ? `${label} · ${pairing}` : pairing} keeps its default win; reopen it to fight it.
                  </p>
                );
              })}
            </div>
          )}
          <ReasonPrompt
            label={isDefaultWin ? "Why is this default win being cleared?" : "Why is this withdrawal being cleared?"}
            presets={isDefaultWin ? DEFAULT_WIN_REOPEN_PRESETS : WITHDRAWAL_REOPEN_PRESETS}
            submitting={ctl.busy}
            onConfirm={(r) => { setAsking(false); ctl.reopen(r); }}
            onCancel={() => setAsking(false)}
          />
        </>
      )}
    </div>
  );
}

// WithdrawalMarkedName: a side's name in an editor header, carrying the RESULT
// mark (Kiken / Fus.) of a recorded withdrawal beside the competitor it names
// while that withdrawal is in force, so a correction shows the recorded
// decision before any edit (bc-kcsh). WHICH mark is sideMarks (bracket.jsx),
// the owner the score strings and the bracket card already read
// ("M Kiken vs ○○"); WHICH side is withdrawnKeyOf, the answer the Recorded line
// under the board gives, so the two cannot disagree. The mark sits on the
// INNER side of the name, across it from the number (numberFollowsName), and
// never in the centre: the middle is a closed set, and Kiken/Fus. name one
// competitor. Both editors render their header names through here, the
// individual board and the team encounter header alike.
function WithdrawalMarkedName({ match, sideKey, side, name, number }) {
  // bc-cse: take BOTH marks once and place the right one on the right name,
  // rather than always reading .loser -- for a match-level fusensho the
  // withdrawal names the BARRED (losing) side, and sideMarks' fusensho arm
  // puts its "Fus." on .winner, not .loser, so reading .loser alone showed
  // NO mark at all on either header while the bracket, list rows, TV
  // headline and export all marked the winner "Fus.".
  const inForce = withdrawalInForce(match);
  const withdrawnKey = inForce ? withdrawnKeyOf(match) : "";
  let mark = "";
  if (inForce && withdrawnKey) {
    const marks = sideMarks(match.decision, false);
    mark = sideKey === withdrawnKey ? marks.loser : marks.winner;
  }
  if (!mark) return <NumberedName side={side} name={name} number={number} />;
  const markEl = <span className="sb-result-mark" data-testid={`withdrawal-mark-${side}`}>{mark}</span>;
  return numberFollowsName(side)
    ? <>{markEl}{" "}<NumberedName side={side} name={name} number={number} /></>
    : <><NumberedName side={side} name={name} number={number} />{" "}{markEl}</>;
}

// A match has ONE result and every surface asking for it shows the same one,
// and the score editors are such surfaces: while one is open, the viewer card,
// the bracket, the TV board, the lobby and the export are all already showing
// whatever the server holds, so an editor showing something else is a
// divergence whatever its reason. Every editor field is local state seeded at
// MOUNT, so following the server takes a deliberate effect per channel — and
// each channel written by hand is a channel a later reader has to NOTICE is
// missing. Three were written by hand, in three shapes, and the team editor's
// numbered bouts were the one nobody noticed (bc-tsub). This is that rule as
// ONE primitive, so a new channel either calls it or is visibly absent.
//
// `signature` must change exactly when the SERVER's value for this channel
// moves — key on the values the effect WRITES, not the fields it reads: those
// two lists drift (the individual editor's did), and a signature built from
// the seeds cannot, since whatever the derivation starts reading is
// automatically part of the key. NEVER key on the match object: SSE re-creates
// it on every broadcast, so an object-keyed effect fights the operator's every
// tap.
//
// `keepLocalEdits` picks the policy, and the two are deliberately different:
//
//   false — adopt unconditionally. Right for a channel that is ONE indivisible
//     value (the hantei verdict, the encho count): keying on the VALUE already
//     means a local arm/pick/cancel stands, because that does not move the
//     server's value. Also right for a channel whose `apply` MERGES rather than
//     replaces — the team editor's bout board does exactly that, keeping the
//     rows the operator touched and taking the server's for the rest, which is
//     strictly better than the all-or-nothing gate below and is why that
//     channel does NOT set this flag.
//
//   true — an operator with UNSAVED work keeps ALL of it, and the server's
//     change is dropped for as long as they are dirty. Use this only where the
//     channel cannot be merged, i.e. where local and server are rival readings
//     of the SAME indivisible thing: the individual editor's scoreline is one
//     fight, so there is no per-row seam to merge along. Prefer a merging
//     `apply` wherever the channel has independent parts, because this policy
//     necessarily discards the newer reading of the parts the operator never
//     touched — and the next full-snapshot write then sends the stale ones
//     back out over it.
//
// The dirty flag read is the PREVIOUS render's, which is the subtle bit this
// hook exists to own: `isDirty` measures local state against what the server
// holds NOW, so on the very render a server change lands it reads true for an
// editor nobody touched — the exact case being corrected. Callers pass the
// current value and the hook remembers it; the tracking effect is registered
// AFTER the adopt effect, so the adopt always sees the value from the render
// BEFORE the change. It self-corrects: adopting makes the next render clean.
//
// THE FULL SET, because "visibly absent" is only true of a set a reader can
// enumerate. There are THREE editor bodies behind ONE entry point, and every
// host mounts the entry point, never a body:
//
//   ScoreEditorModal      admin_scoring_individual.jsx — the entry point AND
//                         the individual editor; dispatches the other two
//   EngiScoreEditorModal  admin_scoring_engi.jsx       — flag counts
//   TeamScoreEditorModal  admin_scoring_team.jsx       — the bout board
//
// Four hosts mount ScoreEditorModal: the shiaijo inline scorer, the admin
// schedule editor, the pools tab and the competition bracket. Which body runs
// is decided by the competition, not the host, so a channel missing from a body
// is missing on ALL FOUR surfaces — which is why the count that matters here is
// three, not four.
//
// All three call this hook. If you add a fourth body, or a new piece of local
// state to one of these three, it needs a channel or an explicit reason.
function useAdoptFromServer({ signature, apply, keepLocalEdits = false, isDirty = false }) {
  // Hold `apply` in a ref so the adopt effect can depend on `signature` ALONE:
  // `apply` is a fresh closure every render and would otherwise re-run the
  // adopt on every render and stomp the operator.
  //
  // Assigned in an effect, NOT during render. A render React starts and then
  // discards (concurrent rendering; StrictMode's double invoke) must not leave
  // the ref holding a closure over props that were never committed — the adopt
  // would then seed from a board nobody was shown. Effects run in declaration
  // order after commit, so this one always refreshes the ref BEFORE the adopt
  // below reads it, and both see the committed render.
  const applyRef = useRefA(apply);
  useEffectA(() => { applyRef.current = apply; });
  const wasDirtyRef = useRefA(false);
  useEffectA(() => {
    if (keepLocalEdits && wasDirtyRef.current) return;
    applyRef.current();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature]);
  // Only the keepLocalEdits policy reads this, but the hook call itself must be
  // unconditional (rules of hooks), so the CHEAP part is the branch inside.
  useEffectA(() => { if (keepLocalEdits) wasDirtyRef.current = !!isDirty; });
}

// ES exports: the modal file imports these and re-exports the test-facing
// subset, so `import { … } from './admin_scoring_modal.jsx'` keeps working.
export {
  MAX_IPPONS_PER_SIDE,
  sideColorName,
  isBoutDecided,
  getIpponButtons,
  getValidPointKeys,
  IpponLegend,
  ScoringShortcutHint,
  applyFusenshoToggle,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  TermAS,
  GlossaryHintAS,
  resolveDecisionPassword,
  assertRunningWritePersisted,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  useAdoptFromServer,
  EnchoControl,
  DecisionPrompt,
  RemainingMatchesPanel,
  BarredMatchNotice,
  FoulCounter,
  LineupNameInput,
  ReasonPrompt,
  CORRECTION_PRESETS,
  WITHDRAWAL_REOPEN_PRESETS,
  withdrawalLabel,
  withdrawnSideOf,
  withdrawnKeyOf,
  withdrawalInForce,
  useMatchReopen,
  ReopenFeedback,
  RecordedWithdrawal,
  WithdrawalMarkedName,
};
