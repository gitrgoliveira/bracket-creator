// Shared helpers and small presentational components for the scoring modals
// (ScoreEditorModal / TeamScoreEditorModal in admin_scoring_modal.jsx). Split
// out so the foundation can be reused and the modal file stays focused on the
// two stateful editors. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// Kendo best-of-3 cap. Mirrors the server-side `maxIpponsPerSide` in
// internal/mobileapp/validation.go: the bout ends when one side reaches
// 2 ippons, so 2-2 is an impossible scoreline. Used to gate the M/K/D/T/H
// buttons on both sides of every bout (individual + team sub-bout).
const MAX_IPPONS_PER_SIDE = 2;

// isBoutDecided: true once either side has reached the best-of-3 cap.
// The UI uses this to disable the add-ippon buttons on BOTH sides at
// that point: the bout would have ended at first-to-2, so neither side
// can legitimately add another ippon. Server enforces the same invariant
// in validateIpponCounts (rejects 2-2 with HTTP 400).
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

// ScoringShortcutHint: quiet, always-present reminder of the keyboard
// shortcuts the modal supports. Rendered below the prev/next nav so the
// affordance sits where operators look for navigation. Clarity over
// decoration: plain muted text, no animation. Styled inline (this region of
// styles.css is owned elsewhere).
function ScoringShortcutHint() {
  const kbd = {
    fontFamily: "var(--font-mono)", fontSize: 11, padding: "1px 5px",
    border: "1px solid var(--line)", borderRadius: 4, background: "var(--surface)",
    color: "var(--ink-3)", margin: "0 1px",
  };
  return (
    <div
      data-testid="scoring-modal-shortcut-hint"
      aria-hidden="true"
      style={{ marginTop: 6, fontSize: 12, color: "var(--ink-3)", textAlign: "center", display: "flex", gap: 4, justifyContent: "center", alignItems: "center", flexWrap: "wrap" }}
    >
      <kbd style={kbd}>←</kbd><kbd style={kbd}>→</kbd>
      <span>prev/next</span>
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>Esc</kbd>
      <span>close</span>
    </div>
  );
}

// applyFusenshoToggle: pure reducer for the per-bout Fusensho button in
// TeamScoreEditorModal. Implements three behaviours on top of the sub
// state {aPts, bPts, aFouls, bFouls, fusensho, _preFusensho?}:
//   1. Toggle-on from a clean state: snapshot {aPts,bPts,aFouls,bFouls}
//      into _preFusensho, then write the 2-0 default win.
//   2. Side-switch (fusensho is already on the other side): preserve
//      the original _preFusensho so a later untoggle restores the
//      genuine pre-fusensho score, not the intermediate 2-0.
//   3. Toggle-off (re-clicking the active side): restore from
//      _preFusensho and clear it. If no snapshot exists (e.g. modal
//      reopened from saved state: initSubs doesn't round-trip the
//      snapshot), just clear the flag.
// Manual pts/fouls edits clear _preFusensho separately (handled in
// the setPts/setFouls closures): once the operator hand-edits, the
// snapshot is stale.
function applyFusenshoToggle(prev, side) {
  if (prev.fusensho === side) {
    const snap = prev._preFusensho;
    if (snap) return { aPts: snap.aPts, bPts: snap.bPts, aFouls: snap.aFouls, bFouls: snap.bFouls, fusensho: "", _preFusensho: undefined };
    return { ...prev, fusensho: "", _preFusensho: undefined };
  }
  const snap = prev._preFusensho || { aPts: prev.aPts, bPts: prev.bPts, aFouls: prev.aFouls, bFouls: prev.bFouls };
  if (side === "a") return { aPts: ["○", "○"], bPts: [], aFouls: 0, bFouls: 0, fusensho: "a", _preFusensho: snap };
  return { aPts: [], bPts: ["○", "○"], aFouls: 0, bFouls: 0, fusensho: "b", _preFusensho: snap };
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
// scoreline that the server's validateIpponCounts would reject. The UI
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
// (e.g. the daihyosen pre-save). window.API.recordScore returns a
// discriminated { queued: true } result when a running write could only be
// enqueued (offline / retryable 5xx) instead of being confirmed by the
// server. A confirmed write returns the MatchResult object; a same-session
// out-of-order write returns { stale: true } (the server already holds an
// equal-or-newer state, so dependent reads are safe and we do NOT abort).
// Throws "score_not_synced" only on the queued case so the caller aborts
// rather than running its dependent request against stale server state.
function assertRunningWritePersisted(saveRes) {
  if (saveRes && saveRes.queued) throw new Error("score_not_synced");
}

// T093/T094: build the /decision POST body. Pure helper so we can pin the
// wire shape (decision/decisionBy/decisionReason/encho/force) against a
// moving server contract. `force` is the T103/T104 override flag used when
// the server replies decision_locked or max_encho_exceeded and the operator
// confirms the override.
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
// the 409 decision_locked / max_encho_exceeded retry-with-force loop.
function submitDecisionRequest(compId, matchId, kind, decisionPayload, enchoPeriodCount, password, opts = {}) {
  const body = buildDecisionBody(kind, decisionPayload, enchoPeriodCount, opts);
  return window.API.recordDecision(compId, matchId, body, resolveDecisionPassword(password));
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
//   - on 409 decision_locked / max_encho_exceeded, confirms then retries with
//     force (recursing into itself).
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
      // F5: if the write was only queued (offline / transient), do NOT close or
      // advance. Enter pending-write mode so the banner shows in the footer.
      // Save the submit closure so "Retry now" can re-invoke it directly.
      if (updated && updated.queued) {
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
        const winnerName = (updated?.winner || '').trim();
        const loserName = winnerName === (updated?.sideA || '') ? (updated?.sideB || '') : (updated?.sideA || '');
        const loser =
          (match.sideA?.name === loserName) ? match.sideA :
          (match.sideB?.name === loserName) ? match.sideB :
          { id: '', name: loserName };
        setWithdrawnPlayer(loser);
        setDecisionPromptKind('');
      } else if (!isComplete && onAfterDecision) {
        // Item 7: fusenpai (and any future non-kiken decision) advances to the
        // next match on the same court. The decision was already persisted via
        // /decision POST so we do NOT issue another score PUT: just advance.
        await onAfterDecision();
      } else {
        onClose();
      }
    } catch (e) {
      const msg = e?.message || 'Failed to record decision';
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
      } else if (!opts.force && /max_encho_exceeded/i.test(msg)) {
        // T104/CHK029: encho-cap override, same confirm-and-retry shape.
        const ok = mountedRef.current && await window.confirmDialog({
          message:
            'This decision would exceed the configured maximum encho periods.\n\n' +
            'Record it anyway?',
          confirmLabel: 'Record anyway',
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

// T104/CHK029: encho-period clamp + banner predicates. maxEnchoPeriods === 0
// (or nullish) means unlimited per the FIK default
// (state.CompetitionConfig.MaxEnchoPeriods). shouldShowEnchoMaxBanner
// surfaces the "Maximum encho periods reached" warning once the operator
// has incremented to the cap; the + button uses canIncrementEncho to gate
// further increments client-side (the server enforces the same cap on PUT
// /score → 409 max_encho_exceeded).
function shouldShowEnchoMaxBanner(enchoPeriodCount, maxEnchoPeriods) {
  if (!maxEnchoPeriods || maxEnchoPeriods <= 0) return false;
  return enchoPeriodCount >= maxEnchoPeriods;
}

function canIncrementEncho(enchoPeriodCount, maxEnchoPeriods) {
  if (!maxEnchoPeriods || maxEnchoPeriods <= 0) return true;
  return enchoPeriodCount < maxEnchoPeriods;
}

function nextEnchoPeriod(current, maxEnchoPeriods) {
  return canIncrementEncho(current, maxEnchoPeriods) ? current + 1 : current;
}

function prevEnchoPeriod(current) {
  return Math.max(1, current - 1);
}

// mp-4pc: once a daihyosen (rep bout, wire position -1) exists, the encho
// rides on that sub, not the top-level match (enchoBlock suppresses the
// match-level encho when hasDaihyosen). On re-open we must restore the
// period count from the sub: else a persisted decidedByHantei replays
// without encho and the backend rejects the next save
// ("requires encho with at least one period"). Exported for vitest.
function initialEnchoPeriodsForMatch(m) {
  const daihyosen = (m.subResults || []).find(s => s.position === -1);
  if (daihyosen) return daihyosen.encho?.periodCount || 0;
  return m.encho?.periodCount || 0;
}

// daihyosenEnchoFields: pure builder for the encho/decidedByHantei wire
// fields on the daihyosen representative bout (position -1). The backend
// invariant (validation.go validateSubBout) is: encho and hantei are valid
// ONLY on the daihyosen. Encho is OPTIONAL for hantei: a tied daihyosen may
// be taken straight to a judges' decision without overtime: so the two
// fields are emitted independently: encho whenever the counter is > 0, and
// decidedByHantei whenever it is armed on a tied scoreline. Returns the fields
// to merge into the entry (possibly empty). Exported for vitest.
function daihyosenEnchoFields({ enchoPeriodCount, daihyosenTied, daihyosenHantei }) {
  const fields = {};
  if (enchoPeriodCount > 0) fields.encho = { periodCount: enchoPeriodCount };
  if (daihyosenTied && daihyosenHantei) fields.decidedByHantei = true;
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
// counter is the existing −/×N/+ stepper plus the "Maximum encho
// periods reached" warning, preserved verbatim. Used by both
// ScoreEditorModal and TeamScoreEditorModal.
function EnchoControl({ enchoPeriodCount, setEnchoPeriodCount, maxEnchoPeriods }) {
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
          <span aria-hidden="true">⏱</span>
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
            onClick={() => setEnchoPeriodCount(c => prevEnchoPeriod(c))}
            disabled={enchoPeriodCount <= 1}
            aria-label="Decrease overtime period count"
          >−</button>
          <span className="encho-row__count">×{enchoPeriodCount}</span>
          <button
            type="button"
            className="btn btn--sm encho-row__btn"
            onClick={() => setEnchoPeriodCount(c => nextEnchoPeriod(c, maxEnchoPeriods))}
            disabled={!canIncrementEncho(enchoPeriodCount, maxEnchoPeriods)}
            aria-label="Increase overtime period count"
          >+</button>
        </div>
      )}
      {shouldShowEnchoMaxBanner(enchoPeriodCount, maxEnchoPeriods) && (
        <span role="alert" className="encho-row__max-banner">
          Maximum encho periods reached
        </span>
      )}
    </div>
  );
}

// Render the inline kiken/fusenpai prompt that replaces the score controls
// while open. Side picker uses radio inputs labelled "SHIRO (White)" / "AKA
// (Red)" to stay consistent with the score board legend; the value submitted
// to the backend is "shiro" or "aka" per DecisionRequest.Validate.
function DecisionPrompt({ kind, sideA, sideB, defaultSide, askReason, onCancel, onSubmit, submitting }) {
  const [side, setSide] = useStateA(defaultSide || "shiro");
  const [reason, setReason] = useStateA("");
  // Display rule (locked, glossary.md §Display rule): render the
  // romaji term ALONE: the popover (via <Term>) carries the gloss.
  // We keep "Decision" untouched (it's already plain English) and
  // wrap the kendo terms so a volunteer hovering/tapping the title
  // gets the full tooltip.
  const isKiken = window.isKikenDecision(kind);
  const kikenLabel = kind === "kiken-injury" ? "Kiken – Injury" : "Kiken – Voluntary";
  const title = isKiken
    ? React.createElement(TermAS, { name: kind }, kikenLabel)
    : kind === "fusenpai"
      ? React.createElement(TermAS, { name: kind }, "Fusenpai")
      : "Decision";

  const submit = (e) => {
    e?.preventDefault?.();
    if (submitting) return;
    onSubmit({ decisionBy: side, decisionReason: askReason ? reason.trim() : "" });
  };

  return (
    <form className="decision-prompt" onSubmit={submit} style={{ border: "1px solid var(--line, #ddd)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2, #fafafa)" }}>
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
        {askReason && (
          <label style={{ display: "flex", flexDirection: "column", gap: 4, marginTop: 4 }}>
            <span style={{ fontWeight: 600 }}>Reason (optional, ≤200 chars)</span>
            <input
              type="text"
              className="input"
              maxLength={200}
              value={reason}
              onInput={(e) => setReason(e.target.value)}
              placeholder="e.g. injury, no-show, doctor's stop"
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

  useEffectA(() => {
    return () => { mountedRef.current = false; };
  }, []);

  useEffectA(() => {
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(compID);
        if (cancelled) return;
        const all = window.compMatches ? window.compMatches(detail) : [];
        const wname = (withdrawnPlayer?.name || "").trim();
        const wid = withdrawnPlayer?.id || "";
        const matchesForPlayer = all.filter(m => {
          if (m.status !== "scheduled") return false;
          const aMatch = (wid && m.sideA?.id === wid) || (wname && m.sideA?.name === wname);
          const bMatch = (wid && m.sideB?.id === wid) || (wname && m.sideB?.name === wname);
          return aMatch || bMatch;
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
    // that's the side that gets the fusenpai (default loss). Pool matches:
    // sideA = Aka, sideB = Shiro. Same wire mapping in bracket matches.
    const wname = (withdrawnPlayer?.name || "").trim();
    const wid = withdrawnPlayer?.id || "";
    const isOnA = (wid && m.sideA?.id === wid) || (wname && m.sideA?.name === wname);
    const decisionBy = isOnA ? "aka" : "shiro";
    setBusyId(m.id);
    try {
      const updated = await window.API.recordDecision(m.compId || compID, m.id, {
        decision: "fusenpai",
        decisionBy,
        decisionReason: `auto: ${wname} withdrawn`,
      }, password);
      if (!mountedRef.current) return;
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

  const playerName = withdrawnPlayer?.name || "player";

  return (
    <div className="remaining-matches" style={{ border: "1px solid var(--line, #ddd)", borderRadius: 6, padding: 12, marginTop: 12, background: "var(--bg-2, #fafafa)" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
        <div style={{ fontSize: 13, fontWeight: 700 }}>Remaining matches for {playerName}</div>
        {onClose && <button type="button" className="btn btn--ghost btn--sm" onClick={onClose} style={{ padding: "2px 8px" }}>✕</button>}
      </div>
      {err && <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 6 }}>{err}</div>}
      {matches === null && <div style={{ fontSize: 12, color: "var(--ink-3)" }}>Loading…</div>}
      {matches !== null && matches.length === 0 && (
        <div style={{ fontSize: 12, color: "var(--ink-3)" }}>No remaining scheduled matches.</div>
      )}
      {matches && matches.length > 0 && (
        <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: 6 }}>
          {matches.map(m => {
            const wname = (withdrawnPlayer?.name || "").trim();
            const wid = withdrawnPlayer?.id || "";
            const isOnA = (wid && m.sideA?.id === wid) || (wname && m.sideA?.name === wname);
            const opponent = isOnA ? m.sideB : m.sideA;
            return (
              <li key={m.id} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, fontSize: 12 }}>
                <div>
                  <span style={{ fontWeight: 600 }}>{opponent?.name || "?"}</span>
                  <span style={{ color: "var(--ink-3)", marginLeft: 6 }}>
                    {m.phase === "pool" ? m.poolName : m.round}{m.court ? ` · Shiaijo ${m.court}` : ""}{m.scheduledAt ? ` · ${m.scheduledAt}` : ""}
                  </span>
                </div>
                <button type="button"
                  className="btn btn--sm"
                  onClick={() => award(m)}
                  disabled={busyId === m.id}
                  title="Record fusenpai: opponent receives the default win"
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

// Reusable foul counter: independent +/- buttons per side with clear labeling.
// The `+` button delegates to `onIncrement` which applies the
// applyFoulIncrement rule (auto-award H + reset at the 2-foul boundary);
// `setFouls` is kept for the `−` button (simple decrement). After the
// 2-foul auto-award the awarded H lives in the opponent's pts array, so
// the counter shows only "outstanding fouls not yet discharged."
function FoulCounter({ label, fouls, setFouls, onIncrement, color, disabled }) {
  // color is "shiro" or "aka": surface as data-testid so Playwright probes
  // (T023a) can target each side without depending on the className.
  // `disabled` freezes the `+` button when the bout is already decided: 
  // a 2nd-foul auto-award in that state would create an invalid 2-2.
  return (
    <div className={`foul-counter foul-counter--${color}`} data-testid={`scoring-modal-hansoku-${color}`}>
      <div className="foul-counter__label">{label} Fouls</div>
      <div className="foul-counter__controls">
        <button type="button" className="foul-counter__btn foul-counter__btn--dec" onClick={() => setFouls(Math.max(0, fouls - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 1 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
        </div>
        <button type="button" className="foul-counter__btn foul-counter__btn--inc" onClick={onIncrement} disabled={disabled}>+</button>
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
function LineupNameInput({ value, roster, onSelect, disabled, ariaLabel, color }) {
  const [query, setQuery] = useStateA("");
  const [open, setOpen] = useStateA(false);
  const [active, setActive] = useStateA(-1); // -1 = no explicit selection yet
  const ref = useRefA(null);
  const q = query.trim();
  const ql = q.toLowerCase();
  const matches = (roster || []).filter(n => !ql || n.toLowerCase().includes(ql)).slice(0, 12);
  const exact = (roster || []).some(n => n.toLowerCase() === ql);
  const canAddNew = q.length > 0 && !exact;
  const optionCount = matches.length + (canAddNew ? 1 : 0);

  window.useClickOutside(ref, () => { setOpen(false); setQuery(""); }, open);

  const commit = (name) => { onSelect(name); setOpen(false); setQuery(""); setActive(-1); };
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
      if (active >= 0 && active < matches.length) commit(matches[active]);
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
        />
        {value && !disabled && (
          <button type="button" className="lineup-name__clear" title="Clear player" aria-label="Clear player"
            onMouseDown={(e) => { e.preventDefault(); commit(""); }}>×</button>
        )}
      </div>
      {open && optionCount > 0 && (
        <div className="pmf__dropdown lineup-name__dropdown">
          {matches.map((n, i) => (
            <button type="button" key={n}
              className={`pmf__option ${i === active ? "pmf__option--active" : ""}`}
              onMouseDown={(e) => { e.preventDefault(); commit(n); }}>
              <span className="pmf__opt-name">{n}</span>
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
function ReasonPrompt({ label = "Reason for change", presets = [], onConfirm, onCancel, submitting = false }) {
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
      style={{ border: "1px solid var(--line, #ddd)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2, #fafafa)" }}
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

// ES exports: the modal file imports these and re-exports the test-facing
// subset, so `import { … } from './admin_scoring_modal.jsx'` keeps working.
export {
  MAX_IPPONS_PER_SIDE,
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
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  EnchoControl,
  DecisionPrompt,
  RemainingMatchesPanel,
  FoulCounter,
  LineupNameInput,
  ReasonPrompt,
  CORRECTION_PRESETS,
};
