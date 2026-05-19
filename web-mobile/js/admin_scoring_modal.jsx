// Score-entry modals used by the schedule and competition pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// Kendo best-of-3 cap. Mirrors the server-side `maxIpponsPerSide` in
// internal/mobileapp/validation.go — the bout ends when one side reaches
// 2 ippons, so 2-2 is an impossible scoreline. Used to gate the M/K/D/T/H
// buttons on both sides of every bout (individual + team sub-bout).
const MAX_IPPONS_PER_SIDE = 2;

// isBoutDecided — true once either side has reached the best-of-3 cap.
// The UI uses this to disable the add-ippon buttons on BOTH sides at
// that point: the bout would have ended at first-to-2, so neither side
// can legitimately add another ippon. Server enforces the same invariant
// in validateIpponCounts (rejects 2-2 with HTTP 400).
function isBoutDecided(aPts, bPts) {
  return (aPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE
      || (bPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE;
}

// getIpponButtons — returns the ordered array of scoring button labels for a
// bout. Naginata adds "S" (Sune, shin strike) between "T" and "H".
function getIpponButtons(isNaginata) {
  return isNaginata ? ["M", "K", "D", "T", "S", "H"] : ["M", "K", "D", "T", "H"];
}

// getValidPointKeys — returns the string of valid single-character keyboard
// shortcuts for scoring. Keyboard handler checks `validKeys.includes(upper)`.
function getValidPointKeys(isNaginata) {
  return isNaginata ? "MKDTSH" : "MKDTH";
}

// applyFusenshoToggle — pure reducer for the per-bout Fusensho button in
// TeamScoreEditorModal. Implements three behaviours on top of the sub
// state {aPts, bPts, aFouls, bFouls, fusensho, _preFusensho?}:
//   1. Toggle-on from a clean state: snapshot {aPts,bPts,aFouls,bFouls}
//      into _preFusensho, then write the 2-0 default win.
//   2. Side-switch (fusensho is already on the other side): preserve
//      the original _preFusensho so a later untoggle restores the
//      genuine pre-fusensho score, not the intermediate 2-0.
//   3. Toggle-off (re-clicking the active side): restore from
//      _preFusensho and clear it. If no snapshot exists (e.g. modal
//      reopened from saved state — initSubs doesn't round-trip the
//      snapshot), just clear the flag.
// Manual pts/fouls edits clear _preFusensho separately (handled in
// the setPts/setFouls closures) — once the operator hand-edits, the
// snapshot is stale.
function applyFusenshoToggle(prev, side) {
  if (prev.fusensho === side) {
    const snap = prev._preFusensho;
    if (snap) return { aPts: snap.aPts, bPts: snap.bPts, aFouls: snap.aFouls, bFouls: snap.bFouls, fusensho: "", _preFusensho: undefined };
    return { ...prev, fusensho: "", _preFusensho: undefined };
  }
  const snap = prev._preFusensho || { aPts: prev.aPts, bPts: prev.bPts, aFouls: prev.aFouls, bFouls: prev.bFouls };
  if (side === "a") return { aPts: ["M", "M"], bPts: [], aFouls: 0, bFouls: 0, fusensho: "a", _preFusensho: snap };
  return { aPts: [], bPts: ["M", "M"], aFouls: 0, bFouls: 0, fusensho: "b", _preFusensho: snap };
}

// applyFoulIncrement — pure helper modelling a single `+` press on a
// side's foul counter. Per FIK rules (and internal/domain/glossary.go):
// "Two hansoku awarded to a competitor give the opponent one free point."
// The 2nd foul auto-awards an "H" ippon to the opponent and resets this
// side's counter to 0. The counter is "outstanding fouls not yet
// discharged into an H" — discharged Hs live in the opponent's pts array.
//
// Bout-decided guard: if EITHER side is already at maxIppons the bout is
// over — the counter still resets to 0 on the 2nd foul but no new H is
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

// reconcileFoulsAtOpen — pure helper for the reopen/correction flow.
// Pre-fix builds stored hansoku as a cumulative raw count (0..N) alongside
// the already-discharged "H" entries in the opponent's pts array. The new
// counter is "outstanding fouls not yet discharged" (0 or 1). Naively
// taking `rawFouls % 2` strips full pairs — but if the opponent's pts is
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

// nextFoulOnDecrement — pure helper for the `−` button. Returns the new
// foul value (a NUMBER, not a React-style functional updater), suitable
// for setters like the team sub-match `rs.setFouls(value)` shape that
// doesn't accept fn-updaters. Extracted so the team-modal `−` regression
// (a fn-updater silently storing as state) is unit-testable.
function nextFoulOnDecrement(currentFouls) {
  return Math.max(0, currentFouls - 1);
}

// Term — kendo-glossary tooltip wrapper. Read lazily off window so the
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
// Falls back to null — the icon is purely decorative; no content to preserve.
function GlossaryHintAS({ name }) {
  if (typeof window !== 'undefined' && window.GlossaryHint) {
    return React.createElement(window.GlossaryHint, { name });
  }
  return null;
}

// T093–T098: shared helpers for the decision (kiken/fusenpai/fusensho) flow.
//
// Resolve the password to use for the /decision POST. The modal historically
// took no password prop (the parent does the recordScore call). Rather than
// re-thread the AdminApp props tree in this slice we accept an explicit prop
// AND fall back to a window-scoped session value if the caller hasn't wired
// it yet. The orchestrator marks the prop wiring as a separate follow-up.
function resolveDecisionPassword(propPassword) {
  if (propPassword) return propPassword;
  if (typeof window !== "undefined" && window.adminPassword) return window.adminPassword;
  return "";
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

// EnchoControl — collapsed by default to a small "⏱ Overtime" pill so
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
  // romaji term ALONE — the popover (via <Term>) carries the gloss.
  // We keep "Decision" untouched (it's already plain English) and
  // wrap the kendo terms so a volunteer hovering/tapping the title
  // gets the full tooltip.
  const title = (kind === "kiken" || kind === "fusenpai")
    ? React.createElement(TermAS, { name: kind }, kind === "kiken" ? "Kiken" : "Fusenpai")
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
        <div style={{ fontWeight: 600 }}>{kind === "kiken" ? "Which side withdrew?" : "Which side did not show up?"}</div>
        <div style={{ display: "flex", gap: 12 }}>
          <label style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <input type="radio" name="decision-side" value="shiro" checked={side === "shiro"} onChange={() => setSide("shiro")} />
            <span><TermAS name="shiro">SHIRO</TermAS> (White){sideB?.name ? ` — ${sideB.name}` : ""}</span>
          </label>
          <label style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <input type="radio" name="decision-side" value="aka" checked={side === "aka"} onChange={() => setSide("aka")} />
            <span><TermAS name="aka">AKA</TermAS> (Red){sideA?.name ? ` — ${sideA.name}` : ""}</span>
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
// calls /decision with decision=fusenpai and decisionBy=<the withdrawn side>
// — note: that's the side the WITHDRAWN player occupies in THAT match, not
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
    // Figure out which side the withdrawn player occupies in THIS match —
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
        {onClose && <button className="btn btn--ghost btn--sm" onClick={onClose} style={{ padding: "2px 8px" }}>✕</button>}
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
                <button
                  className="btn btn--sm"
                  onClick={() => award(m)}
                  disabled={busyId === m.id}
                  title="Record fusenpai — opponent receives the default win"
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
  // color is "shiro" or "aka" — surface as data-testid so Playwright probes
  // (T023a) can target each side without depending on the className.
  // `disabled` freezes the `+` button when the bout is already decided —
  // a 2nd-foul auto-award in that state would create an invalid 2-2.
  return (
    <div className={`foul-counter foul-counter--${color}`} data-testid={`scoring-modal-hansoku-${color}`}>
      <div className="foul-counter__label">{label} Fouls</div>
      <div className="foul-counter__controls">
        <button className="foul-counter__btn foul-counter__btn--dec" onClick={() => setFouls(f => Math.max(0, f - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 1 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
        </div>
        <button className="foul-counter__btn foul-counter__btn--inc" onClick={onIncrement} disabled={disabled}>+</button>
      </div>
    </div>
  );
}

function ScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext, password }) {
  const m = match;
  const isComplete = m.status === "completed";
  const isTeam = m.compKind === "team";
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} password={password} />;

  const seedAPts = m.ipponsA?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? m.score.ippons || [] : []);
  const seedBPts = m.ipponsB?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? m.score.ippons || [] : []);

  // Use ?? not || so an explicit 0 isn't treated as "unset".
  // reconcileFoulsAtOpen turns the pre-fix cumulative raw count into the
  // post-fix "outstanding fouls" semantics AND tops up the opponent's pts
  // with any missing discharged H ippons (legacy/imported data that has
  // hansokuA >= 2 without matching H's in ipponsB would otherwise silently
  // lose points on resubmit). A's fouls discharge into B's pts; B's into A's.
  const rawAFouls = m.hansokuA ?? m.score?.fouls?.a ?? 0;
  const rawBFouls = m.hansokuB ?? m.score?.fouls?.b ?? 0;
  const reconA = reconcileFoulsAtOpen(rawAFouls, seedBPts);
  const reconB = reconcileFoulsAtOpen(rawBFouls, seedAPts);
  const initialAPts = reconB.opponentPts;
  const initialBPts = reconA.opponentPts;
  const initialAFouls = reconA.outstandingFouls;
  const initialBFouls = reconB.outstandingFouls;
  // FR-033: encho (overtime) counter rides alongside the score. Initialized
  // from the existing match.encho?.periodCount so re-opens of completed
  // matches retain the toggle. Slice 1 ships the operator-visible toggle and
  // round-trips the count via toBackendMatchResult; Slice 3 (T093+) layers
  // the decision/kiken UI on top.
  const initialEnchoPeriods = m.encho?.periodCount || 0;
  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  const [aFouls, setAFouls] = useStateA(initialAFouls);
  const [bFouls, setBFouls] = useStateA(initialBFouls);
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  const [submitting, setSubmitting] = useStateA(false);
  // T104/CHK029: MaxEnchoPeriods cap from the competition config.
  // Fetched once on open so the warning banner can fire before the
  // operator submits (the server validates the same cap on PUT /score).
  const [maxEnchoPeriods, setMaxEnchoPeriods] = useStateA(0);
  // Naginata competitions add an extra "S" (Sune) ippon button.
  // Fetched from the competition config alongside maxEnchoPeriods.
  const [isNaginata, setIsNaginata] = useStateA(false);
  // T093–T098: decision (kiken/fusenpai) prompt state. promptKind is
  // "" | "kiken" | "fusenpai"; when non-empty the inline prompt replaces the
  // bottom controls. After the POST /decision succeeds, withdrawnPlayer holds
  // the side that lost so the "Remaining matches" panel can render below.
  const [decisionPromptKind, setDecisionPromptKind] = useStateA("");
  const [decisionSubmitting, setDecisionSubmitting] = useStateA(false);
  const [decisionErr, setDecisionErr] = useStateA("");
  const [withdrawnPlayer, setWithdrawnPlayer] = useStateA(null);
  // doSubmit's setSubmitting(false) in finally fires post-await; if the
  // parent unmounts the modal during the in-flight save (e.g.
  // AdminScoreEditor unmounts), gate the setState. handleDismiss
  // already no-ops UI dismissal while submitting=true, so this covers
  // only external/parent-driven unmount.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  useEffectA(() => {
    if (!m.compId) return;
    let cancelled = false;
    window.API.fetchCompetitionDetails(m.compId).then(d => {
      if (!cancelled && d?.config?.maxEnchoPeriods > 0) setMaxEnchoPeriods(d.config.maxEnchoPeriods);
      if (!cancelled) setIsNaginata(!!d?.config?.naginata);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [m.compId]);

  // T093/T094: shared decision-submit path for kiken & fusenpai.
  // - decisionBy is "shiro" or "aka" per the server contract.
  // - encho rides along when the operator has marked overtime so the server
  //   can attach the periodCount metadata to the resulting MatchResult.
  // - On success we close the modal (matching the Save button contract) UNLESS
  //   the decision is kiken — in that case we keep the modal open and surface
  //   the RemainingMatchesPanel so the operator can chain default-win awards.
  // - T103/CHK024: when the server replies 409 decision_locked (the
  //   prior kiken on this match can't be safely overwritten because a
  //   subsequent match for either side has started), prompt the
  //   operator to confirm and re-send with force=true.
  const submitDecision = async (kind, { decisionBy, decisionReason }, opts = {}) => {
    setDecisionSubmitting(true);
    setDecisionErr("");
    try {
      const body = buildDecisionBody(kind, { decisionBy, decisionReason }, enchoPeriodCount, opts);
      const updated = await window.API.recordDecision(m.compId, m.id, body, resolveDecisionPassword(password));
      if (!mountedRef.current) return;
      if (kind === "kiken") {
        // The loser is the side != Winner. SideA/SideB on MatchResult are
        // names; resolve back to {id, name} via the original match for the
        // remaining-matches lookup.
        const winnerName = (updated?.winner || "").trim();
        const loserName = winnerName === (updated?.sideA || "") ? (updated?.sideB || "") : (updated?.sideA || "");
        // m.sideA / m.sideB are normalized player objects with id+name.
        const loser =
          (m.sideA?.name === loserName) ? m.sideA :
          (m.sideB?.name === loserName) ? m.sideB :
          { id: "", name: loserName };
        setWithdrawnPlayer(loser);
        setDecisionPromptKind("");
      } else {
        // fusenpai (and any future kinds that don't need a follow-up panel)
        // collapses straight back to the parent's onClose/onScored flow.
        onClose();
      }
    } catch (e) {
      const msg = e?.message || "Failed to record decision";
      // T103: the server returns the literal "decision_locked" string
      // as the error body when the kiken-undo would invalidate a
      // downstream match. Re-prompt the operator and retry with force.
      if (!opts.force && /decision_locked/i.test(msg)) {
        if (mountedRef.current && confirm(
          "A subsequent match for one of these competitors has already started.\n\n" +
          "Overwriting the prior decision now may make those downstream results inconsistent. Proceed anyway?"
        )) {
          await submitDecision(kind, { decisionBy, decisionReason }, { force: true });
          return;
        }
        if (mountedRef.current) setDecisionErr("Override cancelled.");
      } else if (!opts.force && /max_encho_exceeded/i.test(msg)) {
        // T104/CHK029: the server caps encho periods per competition.
        // Same confirm-and-retry-with-force shape as decision_locked.
        if (mountedRef.current && confirm(
          "This decision would exceed the configured maximum encho periods.\n\n" +
          "Record it anyway?"
        )) {
          await submitDecision(kind, { decisionBy, decisionReason }, { force: true });
          return;
        }
        if (mountedRef.current) setDecisionErr("Override cancelled.");
      } else if (mountedRef.current) {
        setDecisionErr(msg);
      }
    } finally {
      if (mountedRef.current) setDecisionSubmitting(false);
    }
  };

  // Hansoku Hs are now physically present in the opponent's pts array
  // (folded in at the 2-foul boundary by applyFoulIncrement). The counter
  // is "outstanding fouls" — no derived addends needed.
  const aTotal = aPts.filter((x) => x !== "•").length;
  const bTotal = bPts.filter((x) => x !== "•").length;

  const addPt = (side, letter) => {
    if (side === "a") setAPts((p) => p.length < 2 ? [...p, letter] : p);
    else setBPts((p) => p.length < 2 ? [...p, letter] : p);
  };
  const removePt = (side, idx) => {
    if (side === "a") setAPts((p) => p.filter((_, i) => i !== idx));
    else setBPts((p) => p.filter((_, i) => i !== idx));
  };

  // FR-033: when the operator has marked overtime, attach the encho block
  // to non-"reset" patches. periodCount=0 means "no overtime"; emitting the
  // field as undefined keeps the wire payload clean (omitempty server-side).
  const enchoBlock = () => enchoPeriodCount > 0 ? { encho: { periodCount: enchoPeriodCount } } : {};

  const buildPatch = (targetStatus) => {
    const fouls = { a: aFouls, b: bFouls };
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0 };
    if (targetStatus === "running") return {
      status: "running", winner: null,
      ipponsA: aPts.filter(x => x !== "•"), ipponsB: bPts.filter(x => x !== "•"),
      hansokuA: aFouls, hansokuB: bFouls,
      score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls, live: true, corrected: isComplete },
      ...enchoBlock(),
    };
    if (isDrawToggled) return { winner: null, ipponsA: [], ipponsB: [], hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock() };
    // ippon. Hansoku Hs are already physically present in the pts arrays
    // (folded in by applyFoulIncrement at the 2-foul boundary), so no
    // additional H fold is needed here.
    const aLetters = aPts.filter(x => x !== "•");
    const bLetters = bPts.filter(x => x !== "•");
    const aFinal = aLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = bLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
    if (!winnerSide) return { winner: null, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock() };
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const ippons = winnerSide === "a" ? aFinal : bFinal;
    return { winner, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "ippon", winnerPts: ippons.length, loserPts: (winnerSide === "a" ? bFinal : aFinal).length, ippons, fouls, corrected: isComplete }, ...enchoBlock() };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { if (mountedRef.current) setSubmitting(false); }
  };

  // Draw detection: check both the score.type (when present) and the
  // top-level decision string. Either being "hikiwake" means draw.
  const initialIsDrawToggled = window.isHikiwake(m.score?.type) || window.isHikiwake(m.decision);
  const [isDrawToggled, setIsDrawToggled] = useStateA(initialIsDrawToggled);

  // Arranged as [left, right] — left is always SHIRO (White), right is always AKA (Red).
  // onIncrement applies the FIK 2-foul auto-award rule via applyFoulIncrement:
  // every 2nd foul on this side discharges into a hansoku ippon ("H") for
  // the OPPONENT and resets this side's counter to 0.
  const sides = [
    {
      key: "b", name: m.sideB?.name, dojo: m.sideB?.dojo, pts: bPts, fouls: bFouls, setFouls: setBFouls,
      onIncrement: () => {
        const r = applyFoulIncrement(bFouls, aPts, bPts);
        setBFouls(r.fouls);
        setAPts(r.opponentPts);
      },
      color: "shiro", label: "SHIRO (White)",
    },
    {
      key: "a", name: m.sideA?.name, dojo: m.sideA?.dojo, pts: aPts, fouls: aFouls, setFouls: setAFouls,
      onIncrement: () => {
        const r = applyFoulIncrement(aFouls, bPts, aPts);
        setAFouls(r.fouls);
        setBPts(r.opponentPts);
      },
      color: "aka", label: "AKA (Red)",
    },
  ];

  // Bout is decided once either side reaches 2 ippons — disable add-ippon
  // buttons on BOTH sides (mirrors validateIpponCounts on the server).
  const boutDecided = isBoutDecided(aPts, bPts);

  const canFinish = isDrawToggled || aTotal > 0 || bTotal > 0;

  const isDirty =
    !window.arraysEqual(aPts, initialAPts) ||
    !window.arraysEqual(bPts, initialBPts) ||
    aFouls !== initialAFouls ||
    bFouls !== initialBFouls ||
    isDrawToggled !== initialIsDrawToggled ||
    enchoPeriodCount !== initialEnchoPeriods;
  const handleDismiss = () => {
    // Don't close while any save/decision request is in flight — letting
    // the modal unmount would orphan the pending fetch and lose the
    // setState landing.
    if (submitting || decisionSubmitting) return;
    if (isDirty && !confirm("Discard unsaved scoring changes?")) return;
    onClose();
  };

  // Keyboard shortcuts:
  //   Shift+M/K/D/T/H  → award point to AKA (red, sideA)
  //   m/k/d/t/h        → award point to SHIRO (white, sideB)
  //   Shift+S / s      → award Sune to AKA / SHIRO (naginata competitions only)
  //   x / X            → toggle hikiwake (draw)
  //   ←/→              → previous / next match (skipped inside text-entry elements)
  //   Enter            → finish (or finish + start next when available)
  //   Esc              → close the modal (respects dirty-state confirm)
  // Scoring shortcuts (Enter/M/K/D/T/H/X) are skipped when any interactive
  // element (input, button, link, …) has focus so native activation still works.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, canFinish, isDrawToggled, aTotal, bTotal, handleDismiss, onPrev, onNext, onSubmit, onSubmitAndNext, buildPatch, addPt, doSubmit, isNaginata };

  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;

      // Esc routes through handleDismiss so the dirty-state confirm still fires
      if (ev.key === "Escape") { ev.preventDefault(); s.handleDismiss(); return; }

      // Navigation blocked only inside text-entry elements (preserves cursor movement)
      if (!window.isTextEntry(ev.target)) {
        if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
        if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }
      }

      // Scoring shortcuts blocked when any interactive element has focus
      if (window.isInteractiveTarget(ev.target)) return;

      if (ev.key === "Enter" && s.canFinish) {
        ev.preventDefault();
        const patch = s.buildPatch("completed");
        if (s.onSubmitAndNext) s.doSubmit(() => s.onSubmitAndNext(patch));
        else s.doSubmit(() => s.onSubmit(patch));
        return;
      }

      const k = ev.key;
      const upper = k.toUpperCase();
      const validKeys = getValidPointKeys(s.isNaginata);
      if (validKeys.includes(upper) && k.length === 1) {
        ev.preventDefault();
        // Pressing a point key exits draw mode first
        if (s.isDrawToggled) setIsDrawToggled(false);
        // Shift held → AKA (red); no Shift → SHIRO (white). ev.shiftKey is used
        // instead of uppercase detection to avoid Caps Lock misrouting.
        s.addPt(ev.shiftKey ? "a" : "b", upper);
        return;
      }
      if (k === "x" || k === "X") {
        ev.preventDefault();
        if (s.isDrawToggled) {
          // Cancel-draw is always allowed (mirrors the active-state X button).
          setIsDrawToggled(false);
        } else if (s.aTotal === 0 && s.bTotal === 0) {
          // Toggle-on guarded: any existing score would be silently wiped by
          // the setAPts([])/setBPts([]) below. The clickable X button has
          // disabled={aTotal > 0 || bTotal > 0} for the same reason.
          setIsDrawToggled(true);
          setAPts([]); setBPts([]);
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []); // listener registered once; reads fresh state via kbRef

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className="editor-modal editor-modal--lg editor-modal--compact" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div className="editor-modal__eyebrow">
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
              {enchoPeriodCount > 0 && <span className="editor-modal__eyebrow-encho">· (E) Overtime ×{enchoPeriodCount}</span>}
            </div>
            <div className="editor-modal__title">
              <TermAS name="shiaijo">Shiaijo</TermAS> {m.court} · {m.scheduledAt || "Live"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`viewer__admin-pill ${m.status === "running" ? "sched-row--live" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● LIVE" : "PRE-MATCH"}
            </div>
            <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>
          </div>
        </div>

        <div className="editor-modal__body">
          {/* FR-033 encho toggle: collapses to a small "⏱ Overtime" pill
              when no overtime is active. Click the pill (or set the
              counter through the existing flow) to mount the full
              counter UI. Saves ~32px of vertical space pre-overtime. */}
          <EnchoControl
            enchoPeriodCount={enchoPeriodCount}
            setEnchoPeriodCount={setEnchoPeriodCount}
            maxEnchoPeriods={maxEnchoPeriods}
          />
          <div className="scoring-board">
              {/* Score slots + point buttons */}
              <div className="sb-match">
                {sides.map((s, idx) => (
                  <React.Fragment key={s.key}>
                    <div className={`sb-side sb-side--${s.color}`}>
                      <div className="sb-name">{s.name}</div>
                      <div className="sb-dojo">{s.label}</div>
                      <div className="sb-slots">
                        {[0, 1].map((i) => (
                          <button key={i} className={`sb-slot ${s.pts[i] ? "sb-slot--filled" : ""}`} onClick={() => removePt(s.key, i)} title="Click to remove">
                            {s.pts[i] || "·"}
                          </button>
                        ))}
                      </div>
                      <div className="sb-points-grid">
                        {getIpponButtons(isNaginata).map((cc) => (
                          <button key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={boutDecided}>{cc}</button>
                        ))}
                      </div>
                    </div>
                    {idx === 0 && (
                      <div className="sb-center">
                        {isDrawToggled ? (
                          <button className="sb-draw-toggle sb-draw-toggle--active" onClick={() => { setIsDrawToggled(false); }} title="Cancel draw" aria-label="Cancel draw (hikiwake)">X</button>
                        ) : (
                          <>
                            {(aTotal > 0 || bTotal > 0) && <div className="sb-vs">{`${bTotal}–${aTotal}`}</div>}
                            <button
                              className="sb-draw-toggle"
                              onClick={() => { setIsDrawToggled(true); setAPts([]); setBPts([]); }}
                              disabled={aTotal > 0 || bTotal > 0}
                              title={aTotal > 0 || bTotal > 0 ? "Clear scores before marking a draw" : "Mark as draw (hikiwake)"}
                              aria-label="Mark as draw (hikiwake)"
                            >{aTotal === 0 && bTotal === 0 ? "vs" : "X"}</button>
                          </>
                        )}
                      </div>
                    )}
                  </React.Fragment>
                ))}
              </div>

              {/* Independent foul counters */}
              <div className="sb-fouls">
                {sides.map((s) => (
                  <FoulCounter
                    key={s.key}
                    label={s.label}
                    fouls={s.fouls}
                    setFouls={s.setFouls}
                    onIncrement={s.onIncrement}
                    color={s.color}
                    disabled={boutDecided}
                  />
                ))}
              </div>
          </div>

          {/* T093–T098: decision (kiken/fusenpai) controls + remaining-matches
              follow-up. Sits between the scoring board and the footer so the
              flow is: enter score OR record a decision → either way the modal
              closes (or surfaces the remaining-matches list for kiken). */}
          {!withdrawnPlayer && !decisionPromptKind && (
            <div className="decision-controls" style={{ display: "flex", gap: 8, marginTop: 12, fontSize: 12, alignItems: "center" }}>
              <span style={{ color: "var(--ink-3)", fontWeight: 600 }}>Decision:</span>
              <div className="decision-btn-group">
                <button data-testid="scoring-modal-kiken-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("kiken"); }} disabled={submitting || decisionSubmitting}>
                  Kiken
                </button>
                <GlossaryHintAS name="kiken" />
              </div>
              <div className="decision-btn-group">
                <button data-testid="scoring-modal-fusenpai-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("fusenpai"); }} disabled={submitting || decisionSubmitting}>
                  Fusenpai
                </button>
                <GlossaryHintAS name="fusenpai" />
              </div>
              {/* Per-bout fusensho is a sub-match concept — implemented inside
                  TeamScoreEditorModal. This placeholder explains the affordance
                  to operators who open the individual-match editor. */}
              <div className="decision-btn-group">
                <button type="button" className="btn btn--sm" disabled title="Fusensho is recorded per-bout inside the team-match editor">
                  Fusensho (team only)
                </button>
                <GlossaryHintAS name="fusensho" />
              </div>
            </div>
          )}
          {decisionErr && (
            <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginTop: 6 }}>{decisionErr}</div>
          )}
          {decisionPromptKind && (
            <DecisionPrompt
              kind={decisionPromptKind}
              sideA={m.sideA}
              sideB={m.sideB}
              defaultSide="shiro"
              askReason={decisionPromptKind === "kiken"}
              submitting={decisionSubmitting}
              onCancel={() => { setDecisionPromptKind(""); setDecisionErr(""); }}
              onSubmit={({ decisionBy, decisionReason }) => submitDecision(decisionPromptKind, { decisionBy, decisionReason })}
            />
          )}
          {withdrawnPlayer && (
            <RemainingMatchesPanel
              compID={m.compId}
              password={resolveDecisionPassword(password)}
              withdrawnPlayer={withdrawnPlayer}
              onAwarded={() => { /* stay open; operator decides when to close */ }}
              onClose={() => { setWithdrawnPlayer(null); onClose(); }}
            />
          )}

        </div>

        {/* Sticky navigation + action footer */}
        <div className="editor-modal__foot editor-modal__foot--nav">
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={prevMatch.sideA?.name + " vs " + prevMatch.sideB?.name}>← Prev</button>
            ) : <span />}

            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>
                  ▶ Start Match
                </button>
              )}
              <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>
              {onSubmitAndNext ? (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmitAndNext(buildPatch("completed")))} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmit(buildPatch("completed")))} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : "Finish"}
                </button>
              )}
            </div>

            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={nextMatch.sideA?.name + " vs " + nextMatch.sideB?.name}>Next →</button>
            ) : <span />}
          </div>
        </div>
      </div>
    </div>
  );
}

// Built from MAX_TEAM_SIZE (admin_helpers.jsx) so the scoring UI's
// position count stays in lockstep with the team-size input caps in
// admin_competition.jsx and admin_setup.jsx. Bumping MAX_TEAM_SIZE
// flows automatically to all three sites.
const TEAM_POSITIONS = Array.from({ length: window.MAX_TEAM_SIZE }, (_, i) => String(i + 1));

// T131 helper: human-friendly position label for the team-match scoring
// modal. 5-person teams use the canonical FIK names; non-5 sizes use the
// position number. Kept inline here so the team modal doesn't have a
// hard import dependency on admin_lineup.jsx (the two files are loaded
// independently and admin_lineup.jsx may not be present in older
// builds). The mapping mirrors POS_LABELS_5 in admin_lineup.jsx.
const POS_LABELS_BY_INDEX_5 = ["Senpo", "Jiho", "Chuken", "Fukusho", "Taisho"];
const POS_TERM_BY_LABEL_5 = { Senpo: "senpo", Jiho: "jiho", Chuken: "chuken", Fukusho: "fukusho", Taisho: "taisho" };
function positionLabelFor(teamSize, index, sub) {
  if (sub && sub.position && typeof sub.position === "string" && sub.position.length > 0 && /[a-z]/i.test(sub.position)) {
    // Backend may emit a name string in Position for non-5 sizes once
    // domain.Position is wire-stable. Use it verbatim when present.
    return sub.position;
  }
  if (teamSize === 5 && index >= 0 && index < 5) return POS_LABELS_BY_INDEX_5[index];
  return `Match ${index + 1}`;
}

// renderPositionLabel — wrap a known FIK position label in <Term> so
// the team scoring modal's bout headings carry the gloss. Falls back to
// the plain string for non-FIK labels ("Match 3", etc.).
function renderPositionLabel(label) {
  const termId = POS_TERM_BY_LABEL_5[label];
  if (termId) return React.createElement(TermAS, { name: termId }, label);
  return label;
}

function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext, password }) {
  const m = match;
  const isComplete = m.status === "completed";
  const positions = TEAM_POSITIONS.slice(0, teamSize);
  // FR-033: encho counter for team matches (overtime period count rides
  // alongside the score on the wire — same shape as ScoreEditorModal).
  const initialEnchoPeriods = m.encho?.periodCount || 0;
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  const [submitting, setSubmitting] = useStateA(false);
  // T093–T098: decision state — same shape as the individual editor. See the
  // ScoreEditorModal copy for the contract.
  const [decisionPromptKind, setDecisionPromptKind] = useStateA("");
  const [decisionSubmitting, setDecisionSubmitting] = useStateA(false);
  const [decisionErr, setDecisionErr] = useStateA("");
  const [withdrawnPlayer, setWithdrawnPlayer] = useStateA(null);
  // T131: lineup data so each bout cell can show the assigned player
  // name + canonical position label. Falls back gracefully when the
  // lineup hasn't been submitted yet (404 → null).
  const [lineupA, setLineupA] = useStateA(null);
  const [lineupB, setLineupB] = useStateA(null);
  // T136 / T141: competition lookup so we can branch on teamMatchType
  // ("kachinuki" vs "fixed") and gate the daihyosen affordance on the
  // knockout-format precondition. Falls back to compKind/teamSize when
  // the fetch fails so the existing fixed-grid flow still works.
  const [compMeta, setCompMeta] = useStateA(null);
  // T141: error banner mapping for the daihyosen POST. Server returns
  // 400 not_tied / 400 pool_match / 409 insufficient_eligibility — see
  // handlers_daihyosen.go for the canonical strings.
  const [daihyosenErr, setDaihyosenErr] = useStateA("");
  const [daihyosenBusy, setDaihyosenBusy] = useStateA(false);
  // Same teardown-race guard as ScoreEditorModal — covers external/
  // parent-driven unmount during in-flight save.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Fetch lineup + competition data on mount. Both endpoints are
  // read-only and idempotent; failures degrade gracefully (the modal
  // still functions, just without position labels / kachinuki mode).
  useEffectA(() => {
    let cancelled = false;
    if (!m.compId) return;
    // Competition detail for teamMatchType + format. We don't need the
    // full payload — just the config fields. fetchCompetitionDetails
    // already exists and is cheap.
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(m.compId);
        if (cancelled) return;
        setCompMeta(detail || null);
      } catch (e) {
        // Soft-fail: kachinuki/daihyosen UI just won't render.
        console.warn("Competition fetch for team modal failed:", e);
      }
    })();
    // Lineups for both teams. compMatches injects m.round as a string
    // label ("Round 2", "Quarterfinals", ...) — for bracket matches we
    // extract the numeric index from "Round N" when possible. Pool
    // matches don't have a per-round lineup in the current model, so
    // we fall back to round 0 (matches the first set of bouts).
    // TODO(T131): plumb a numeric round through compMatches so this
    // lookup is exact for every phase / label.
    let round = 0;
    if (typeof m.round === "string") {
      const mr = /^Round\s+(\d+)$/.exec(m.round);
      if (mr) round = parseInt(mr[1], 10) - 1;
    } else if (typeof m.round === "number") {
      round = m.round;
    }
    const sideAId = m.sideA?.id || (typeof m.sideA === "string" ? m.sideA : "");
    const sideBId = m.sideB?.id || (typeof m.sideB === "string" ? m.sideB : "");
    if (sideAId) {
      window.API.fetchTeamLineup(m.compId, sideAId, round).then(l => {
        if (!cancelled) setLineupA(l);
      }).catch(() => { /* 404 / network: ignore */ });
    }
    if (sideBId) {
      window.API.fetchTeamLineup(m.compId, sideBId, round).then(l => {
        if (!cancelled) setLineupB(l);
      }).catch(() => { /* 404 / network: ignore */ });
    }
    return () => { cancelled = true; };
  }, [m.compId, m.id]);

  // T136: kachinuki branch. Match-level teamMatchType (added by
  // viewer.compMatches in a sibling slice) is preferred; competition
  // fetch is the fallback. Default "fixed" preserves the legacy N×1
  // grid behaviour.
  const teamMatchType = m.teamMatchType || compMeta?.config?.teamMatchType || "fixed";
  const isKachinuki = teamMatchType === "kachinuki";
  // Compact "Instrument Panel" mode fits the modal on one viewport page
  // for ≤5-person teams. Kachinuki renders only the current bout
  // (see visiblePositions: positions.slice(kachinukiIdx, kachinukiIdx+1)),
  // so it always fits even with a 9-person roster. Larger fixed-format
  // teams keep the roomier layout and use .team-bouts-scroll for
  // independent bout-list scrolling.
  const useCompact = teamSize <= 5 || isKachinuki;
  // T141: daihyosen is knockout-only — pool matches resolve ties via
  // the standings tiebreak, not a representative bout. Format comes
  // from match-level compFormat (when set by compMatches) or the comp
  // fetch fallback. Phase === "bracket" is the in-modal signal.
  const compFormat = m.compFormat || compMeta?.config?.format || "";
  const maxEnchoPeriods = compMeta?.config?.maxEnchoPeriods || 0;
  const isNaginataTeam = !!compMeta?.config?.naginata;
  const isKnockoutPhase = m.phase === "bracket" || compFormat === "playoffs" || compFormat === "mixed";

  // Mirror of submitDecision in ScoreEditorModal — kept inline rather than
  // hoisted to a shared hook because the two modals own different "after
  // success" semantics (the individual modal doesn't have per-bout state to
  // preserve, but the wiring is identical otherwise). T103/CHK024:
  // 409 decision_locked triggers a confirm-and-retry-with-force loop.
  const submitDecision = async (kind, { decisionBy, decisionReason }, opts = {}) => {
    setDecisionSubmitting(true);
    setDecisionErr("");
    try {
      const body = buildDecisionBody(kind, { decisionBy, decisionReason }, enchoPeriodCount, opts);
      const updated = await window.API.recordDecision(m.compId, m.id, body, resolveDecisionPassword(password));
      if (!mountedRef.current) return;
      if (kind === "kiken") {
        const winnerName = (updated?.winner || "").trim();
        const loserName = winnerName === (updated?.sideA || "") ? (updated?.sideB || "") : (updated?.sideA || "");
        const loser =
          (m.sideA?.name === loserName) ? m.sideA :
          (m.sideB?.name === loserName) ? m.sideB :
          { id: "", name: loserName };
        setWithdrawnPlayer(loser);
        setDecisionPromptKind("");
      } else {
        onClose();
      }
    } catch (e) {
      const msg = e?.message || "Failed to record decision";
      if (!opts.force && /decision_locked/i.test(msg)) {
        if (mountedRef.current && confirm(
          "A subsequent match for one of these teams has already started.\n\n" +
          "Overwriting the prior decision now may make those downstream results inconsistent. Proceed anyway?"
        )) {
          await submitDecision(kind, { decisionBy, decisionReason }, { force: true });
          return;
        }
        if (mountedRef.current) setDecisionErr("Override cancelled.");
      } else if (!opts.force && /max_encho_exceeded/i.test(msg)) {
        // T104/CHK029: encho-cap override, same shape as decision_locked.
        if (mountedRef.current && confirm(
          "This decision would exceed the configured maximum encho periods.\n\n" +
          "Record it anyway?"
        )) {
          await submitDecision(kind, { decisionBy, decisionReason }, { force: true });
          return;
        }
        if (mountedRef.current) setDecisionErr("Override cancelled.");
      } else if (mountedRef.current) {
        setDecisionErr(msg);
      }
    } finally {
      if (mountedRef.current) setDecisionSubmitting(false);
    }
  };

  const existingSub = m.subResults || [];
  // T096/FR-031: round-trip per-bout fusensho. SubMatchResult.decision is
  // the canonical signal — when "fusensho", figure out which side it
  // belongs to via the recorded winner so the UI re-opens with the
  // affordance shown as active.
  const sideAName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const sideBName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  const initSubs = positions.map((_, idx) => {
    const existing = existingSub.find(s => s.position === idx + 1);
    let fusensho = "";
    if (existing?.decision === "fusensho") {
      if (existing.winner === sideAName) fusensho = "a";
      else if (existing.winner === sideBName) fusensho = "b";
    }
    // reconcileFoulsAtOpen mirrors ScoreEditorModal: pre-fix builds
    // stored the cumulative raw foul count alongside the already-awarded
    // H in the opponent's ippon array. The counter now means "outstanding
    // fouls not yet discharged" and any missing discharged H's in the
    // opponent's pts are topped up (defensive against legacy/imported data).
    const rawAFouls = existing ? existing.hansokuA || 0 : 0;
    const rawBFouls = existing ? existing.hansokuB || 0 : 0;
    const seedAPts = existing ? (existing.ipponsA || []).filter(x => x && x !== "•") : [];
    const seedBPts = existing ? (existing.ipponsB || []).filter(x => x && x !== "•") : [];
    const reconA = reconcileFoulsAtOpen(rawAFouls, seedBPts);
    const reconB = reconcileFoulsAtOpen(rawBFouls, seedAPts);
    return {
      aPts: reconB.opponentPts,
      bPts: reconA.opponentPts,
      aFouls: reconA.outstandingFouls,
      bFouls: reconB.outstandingFouls,
      fusensho,
    };
  });
  const [subs, setSubs] = useStateA(initSubs);

  const updateSub = (idx, fn) => setSubs(prev => prev.map((s, i) => i === idx ? fn(s) : s));

  // T096/FR-031: per-bout Fusensho — award a 2-0 default win to the
  // present side. Re-clicking the active side undoes the fusensho and
  // restores the score that existed before fusensho was applied (the
  // operator's intent on the active button is "undo this"). Clicking
  // the OTHER side while fusensho is active is a side-switch; the
  // original pre-fusensho snapshot is preserved so a later untoggle
  // still restores the genuine prior state, not the intermediate 2-0.
  const setFusenshoFor = (idx, side) => updateSub(idx, prev => applyFusenshoToggle(prev, side));

  // Hansoku Hs are already in the pts arrays (folded in by
  // applyFoulIncrement at the 2-foul boundary), so totals are just the
  // pts length. No separate hansoku tally is needed in the live view.
  const subTotals = subs.map(s => {
    const aT = s.aPts.length;
    const bT = s.bPts.length;
    const winner = aT > bT ? "a" : bT > aT ? "b" : null;
    return { aTotal: aT, bTotal: bT, winner };
  });

  const ivA = subTotals.filter(s => s.winner === "a").length;
  const ivB = subTotals.filter(s => s.winner === "b").length;
  const pwA = subTotals.reduce((sum, s) => sum + s.aTotal, 0);
  const pwB = subTotals.reduce((sum, s) => sum + s.bTotal, 0);
  const teamWinner = ivA > ivB ? "a" : ivB > ivA ? "b" : pwA > pwB ? "a" : pwB > pwA ? "b" : null;

  const enchoBlock = () => enchoPeriodCount > 0 ? { encho: { periodCount: enchoPeriodCount } } : {};

  const buildPatch = (targetStatus) => {
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
    const subResults = subs.map((s, idx) => {
      const t = subTotals[idx];
      // Hansoku Hs already in pts arrays via applyFoulIncrement — no fold.
      const aAll = s.aPts.slice(0, MAX_IPPONS_PER_SIDE);
      const bAll = s.bPts.slice(0, MAX_IPPONS_PER_SIDE);
      const w = t.winner === "a" ? m.sideA : t.winner === "b" ? m.sideB : null;
      // T096/FR-031: per-bout fusensho overrides the default hikiwake/fought
      // mapping. The bout was awarded as a default win — backend tally treats
      // it as a victory for IV but the canonical decision string makes it
      // distinguishable from a fought 2-0 in reports and audit trails.
      let decision = "";
      if (s.fusensho) decision = "fusensho";
      else if (t.winner === null) decision = "hikiwake";
      return {
        position: idx + 1,
        sideA: typeof m.sideA === "object" ? m.sideA?.name : m.sideA,
        sideB: typeof m.sideB === "object" ? m.sideB?.name : m.sideB,
        ipponsA: aAll,
        ipponsB: bAll,
        hansokuA: s.aFouls,
        hansokuB: s.bFouls,
        winner: w ? (typeof w === "object" ? w.name : w) : "",
        decision,
      };
    });
    const winner = teamWinner === "a" ? m.sideA : teamWinner === "b" ? m.sideB : null;
    // When transitioning to "running" (▶ Start), teamWinner is typically
    // null (0–0). Don't emit score.type: "hikiwake" — toBackendMatchResult
    // maps score.type to decision, which would persist a draw decision on
    // a live match. Send live: true with no completed-state semantics so
    // the backend leaves decision empty until the match actually finishes.
    if (targetStatus === "running") {
      return {
        winner: null,
        status: "running",
        ipponsA: [],
        ipponsB: [],
        score: { type: "ippon", winnerPts: 0, loserPts: 0, fouls: { a: 0, b: 0 }, live: true, corrected: isComplete },
        subResults,
        ...enchoBlock(),
      };
    }
    return {
      winner,
      status: "completed",
      ipponsA: [],
      ipponsB: [],
      score: { type: teamWinner ? "ippon" : "hikiwake", winnerPts: teamWinner === "a" ? ivA : ivB, loserPts: teamWinner === "a" ? ivB : ivA, fouls: { a: 0, b: 0 }, corrected: isComplete },
      subResults,
      ...enchoBlock(),
    };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { if (mountedRef.current) setSubmitting(false); }
  };

  // Mirrors ScoreEditorModal.isDirty: structural compare of current subs
  // to the initial snapshot. Used by handleDismiss below to prompt before
  // discarding multi-sub-match edits. Team scoring typically has 3–9 sub
  // entries; the JSON serialize approach is fine for that size and keeps
  // the comparison robust against array identity drift from setSubs.
  // Encho toggle is included so an operator-only encho change still
  // triggers the discard confirm.
  const isDirty = JSON.stringify(subs) !== JSON.stringify(initSubs) || enchoPeriodCount !== initialEnchoPeriods;

  // Match ScoreEditorModal's dismiss contract: never close mid-submit
  // (setState-after-unmount), AND confirm-then-discard when the user has
  // unsaved sub-match edits. The earlier version only checked submitting,
  // so an accidental backdrop/Esc silently lost up to 9 sub-match scores.
  const handleDismiss = () => {
    // Same contract as ScoreEditorModal: never close while a save,
    // decision, or daihyosen request is mid-flight.
    if (submitting || decisionSubmitting || daihyosenBusy) return;
    if (isDirty && !confirm("Discard unsaved scoring changes?")) return;
    onClose();
  };

  // Esc-to-close, matching ScoreEditorModal. The full keyboard-shortcut
  // surface (M/K/D/T/H, ←/→, Enter) isn't wired here — team scoring is
  // many sub-matches and would need different bindings — but Esc is
  // table-stakes UX.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, handleDismiss, onPrev, onNext };
  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      if (ev.key === "Escape") { ev.preventDefault(); s.handleDismiss(); return; }
      if (window.isTextEntry(ev.target)) return;
      if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
      if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // left = SHIRO (White), right = AKA (Red)
  const teamSides = [
    { key: "b", name: m.sideB?.name || m.sideB, label: "SHIRO (White)", color: "shiro", iv: ivB, pw: pwB },
    { key: "a", name: m.sideA?.name || m.sideA, label: "AKA (Red)", color: "aka", iv: ivA, pw: pwA },
  ];

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className={`editor-modal editor-modal--team ${useCompact ? "editor-modal--compact" : ""}`} onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div className="editor-modal__eyebrow">
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
              {enchoPeriodCount > 0 && <span className="editor-modal__eyebrow-encho">· (E) Overtime ×{enchoPeriodCount}</span>}
            </div>
            <div className="editor-modal__title">
              <TermAS name="shiaijo">Shiaijo</TermAS> {m.court} · {m.scheduledAt || "Live"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`viewer__admin-pill ${m.status === "running" ? "sched-row--live" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● LIVE" : "PRE-MATCH"}
            </div>
            <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>
          </div>
        </div>

        <div className="editor-modal__body">
          {/* FR-033 encho toggle: see ScoreEditorModal for the contract.
              EnchoControl collapses to a pill when no overtime is active. */}
          <EnchoControl
            enchoPeriodCount={enchoPeriodCount}
            setEnchoPeriodCount={setEnchoPeriodCount}
            maxEnchoPeriods={maxEnchoPeriods}
          />
          {/* Team header */}
          <div className="sb-match" style={{ marginBottom: 16 }}>
            {teamSides.map((s, idx) => (
              <React.Fragment key={s.key}>
                <div className={`sb-side sb-side--${s.color}`}>
                  <div className="sb-name">{s.name}</div>
                  <div className="sb-dojo">{s.label}</div>
                </div>
                {idx === 0 && (
                  <div className="sb-center">
                    <div className="sb-vs">VS</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>

          {/* Individual match rows. T136: in kachinuki mode only the
              CURRENT bout is rendered (positions.slice(kachinukiIdx,
              kachinukiIdx+1)) — the last row that has any data, or row 0
              if nothing has been scored yet. The server appends new bouts
              via engine.MaybeAdvanceKachinuki after each score record, so
              the operator re-opens the modal to score the next bout.
              The .team-bouts-scroll wrapper gives the roomy (non-compact)
              layout an independent scroll region for the bout list so the
              team header / summary / decision / footer stay anchored. */}
          <div className="team-bouts-scroll">
          {(() => {
            // T136: kachinuki "current bout" index — last row that has
            // any data, or 0 if nothing scored yet.
            let kachinukiIdx = 0;
            for (let i = subs.length - 1; i >= 0; i--) {
              if (subs[i].aPts.length > 0 || subs[i].bPts.length > 0 || subs[i].aFouls > 0 || subs[i].bFouls > 0) {
                kachinukiIdx = i;
                break;
              }
            }
            // T136: "kachinuki-exhaustion" sentinel — surface the end
            // banner instead of more bout rows when the backend has
            // already decided the match.
            const exhausted = isKachinuki && (m.decision === "kachinuki-exhaustion" || (m.subResults || []).some(s => s.decision === "kachinuki-exhaustion"));
            const visiblePositions = isKachinuki ? positions.slice(kachinukiIdx, kachinukiIdx + 1) : positions;
            return [
              isKachinuki && (
                <div key="kachinuki-banner" style={{ background: "var(--bg-2, #fafafa)", border: "1px solid var(--accent, #ddd)", borderRadius: 4, padding: "8px 12px", marginBottom: 12, fontSize: 12, display: "flex", flexDirection: "column", gap: 4 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span style={{ fontWeight: 700 }}><TermAS name="kachinuki">Kachinuki</TermAS> (winner-stays)</span>
                    <span style={{ color: "var(--ink-3)" }}>
                      {exhausted
                        ? "One team exhausted — match ended."
                        : "Score only the current bout. The server appends the next bout automatically; reopen the match to score it."}
                    </span>
                  </div>
                  {/* TODO(T136): inline auto-refresh after each score so
                      operators don't have to close+reopen the modal —
                      requires hooking the onSubmit response (current
                      flow forwards through parent + closes the modal). */}
                </div>
              ),
              ...visiblePositions
            ];
          })().filter(Boolean).map((pos, _displayIdx) => {
            // Kachinuki returns a banner element as the first item; pass
            // it through unchanged. Other items are position strings —
            // map them back to their canonical index in `positions`.
            if (React.isValidElement(pos)) return pos;
            const idx = positions.indexOf(pos);
            const s = subs[idx];
            const t = subTotals[idx];
            // T131: pull the per-side player + position label. existingSub
            // (from the match) and lineup data are both consulted so the
            // bout cell shows e.g. "Match 1 (Senpo) — A. Tanaka vs B. Sato".
            const existingSubAtIdx = (m.subResults || []).find(sr => sr.position === idx + 1);
            const posLabel = positionLabelFor(teamSize, idx, existingSubAtIdx);
            // Resolve the player name occupying this position on each
            // side: lineup data first (canonical when present), then the
            // SubMatchResult.SideA/SideB strings from a prior score.
            //
            // 5-person teams use named position keys (senpo, jiho, ...);
            // other sizes use the numeric string "1".."N". Try both
            // shapes so this stays size-agnostic.
            const posKey5 = (teamSize === 5 && idx < 5) ? POS_LABELS_BY_INDEX_5[idx].toLowerCase() : null;
            const posKeyN = String(positions[idx]);
            const pickFromLineup = (lineup) => {
              if (!lineup?.positions) return "";
              if (posKey5 && lineup.positions[posKey5]) return lineup.positions[posKey5];
              if (lineup.positions[posKeyN]) return lineup.positions[posKeyN];
              return "";
            };
            const playerAName = pickFromLineup(lineupA) || existingSubAtIdx?.sideA || "";
            const playerBName = pickFromLineup(lineupB) || existingSubAtIdx?.sideB || "";

            // Each row: [left side, center score, right side] — left=SHIRO, right=AKA
            // T096/FR-031: manual pts/fouls edits clear the per-bout fusensho
            // flag AND discard the _preFusensho snapshot so the bout becomes
            // a regular fought score once the operator intervenes. Re-applying
            // via the Fusensho button captures a fresh snapshot from the
            // current (manually-edited) state.
            // onIncrement applies the FIK 2-foul rule via applyFoulIncrement:
            // the 2nd foul auto-awards an H to the OPPONENT and resets this
            // side's foul counter. The auto-award also invalidates the
            // _preFusensho snapshot — once an H lands in the slot the prior
            // pre-fusensho state is stale.
            const rowSides = [
              {
                key: "b", pts: s.bPts, fouls: s.bFouls,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, bPts: pts, fusensho: "", _preFusensho: undefined })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, bFouls: f, fusensho: "", _preFusensho: undefined })),
                onIncrement: () => updateSub(idx, prev => {
                  const r = applyFoulIncrement(prev.bFouls, prev.aPts, prev.bPts);
                  return { ...prev, bFouls: r.fouls, aPts: r.opponentPts, fusensho: "", _preFusensho: undefined };
                }),
                color: "shiro", label: "SHIRO",
              },
              {
                key: "a", pts: s.aPts, fouls: s.aFouls,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, aPts: pts, fusensho: "", _preFusensho: undefined })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, aFouls: f, fusensho: "", _preFusensho: undefined })),
                onIncrement: () => updateSub(idx, prev => {
                  const r = applyFoulIncrement(prev.aFouls, prev.bPts, prev.aPts);
                  return { ...prev, aFouls: r.fouls, bPts: r.opponentPts, fusensho: "", _preFusensho: undefined };
                }),
                color: "aka", label: "AKA",
              },
            ];

            // Sub-bout is decided once either side reaches 2 ippons — disable
            // add-ippon buttons on BOTH sides of THIS sub-bout only (other
            // sub-bouts in the team match remain independent).
            const subBoutDecided = isBoutDecided(s.aPts, s.bPts);

            const scoreDisplay = (() => {
              if (t.winner === null && t.aTotal === 0 && t.bTotal === 0) return <span style={{ color: "var(--ink-3)" }}>–</span>;
              if (t.winner === null) return <span className="tsm-draw">X</span>;
              return <span>{`${t.bTotal}–${t.aTotal}`}</span>;
            })();

            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos">
                  {/* T131: position label + assigned player names from
                      lineup data. The position label (Senpo/Jiho/etc.
                      for 5-person teams, "Match N" otherwise) comes from
                      positionLabelFor; player names are joined from the
                      team lineups when available. */}
                  <span style={{ fontWeight: 700 }}>{renderPositionLabel(posLabel)}</span>
                  {(playerAName || playerBName) && (
                    <span style={{ display: "block", fontSize: 11, color: "var(--ink-3)", fontWeight: 500, marginTop: 2 }}>
                      {playerBName || "?"} (SHIRO) vs {playerAName || "?"} (AKA)
                    </span>
                  )}
                </div>
                <div className="team-sub-match__row">
                  {rowSides.map((rs, rsIdx) => (
                    <React.Fragment key={rs.key}>
                      <div className={`team-sub-match__side ${rsIdx === 1 ? "team-sub-match__side--right" : ""}`}>
                        {/* Row 1: side label + point slots + M/K/D/T/H buttons.
                            In compact mode these align on one horizontal
                            channel-strip; in roomy mode the wrapper is
                            display:contents so the legacy column stack is
                            preserved without rewriting CSS. */}
                        <div className="tsm-row-1">
                          <div className="tsm-side-label">{rs.label}</div>
                          {/* Point slots */}
                          <div className="team-sub-match__pts">
                            {[0, 1].map(i => (
                              <button key={i} className={`editor-side__pt ${rs.pts[i] ? "editor-side__pt--filled" : ""}`}
                                onClick={() => rs.setPts(rs.pts.filter((_, j) => j !== i))} title="Click to remove">
                                {rs.pts[i] || "·"}
                              </button>
                            ))}
                          </div>
                          {/* Point buttons incl. H */}
                          <div className="team-sub-match__btns">
                            {getIpponButtons(isNaginataTeam).map(cc => (
                              <button key={cc} className={`ipt-btn ipt-btn--sm ${cc === "H" ? "ipt-btn--h" : ""}`}
                                onClick={() => rs.setPts(rs.pts.length < MAX_IPPONS_PER_SIDE ? [...rs.pts, cc] : rs.pts)}
                                disabled={subBoutDecided}>{cc}</button>
                            ))}
                          </div>
                        </div>
                        {/* Row 2: foul stepper + per-bout Fusensho button.
                            Independent foul counter. The `+` button calls
                            onIncrement which applies the FIK 2-foul rule via
                            applyFoulIncrement (auto-award H to opponent, reset
                            counter to 0). The discharged H is physically in
                            the opponent's pts array — no derived display.
                            T096/FR-031: per-bout Fusensho — awards the bout
                            2-0 to this side. Re-clicking the active side
                            undoes the fusensho; manual pts/fouls edits while
                            active clear the flag and discard the snapshot. */}
                        <div className="tsm-row-2">
                          <div className="tsm-fouls" data-testid={`scoring-modal-hansoku-${rs.color}`}>
                            <span className="tsm-fouls__label">{rs.label} Fouls</span>
                            <div className="tsm-fouls__controls">
                              <button className="tsm-fouls__btn" onClick={() => rs.setFouls(nextFoulOnDecrement(rs.fouls))} disabled={rs.fouls === 0}>−</button>
                              <span className={`tsm-fouls__count ${rs.fouls >= 1 ? "tsm-fouls__count--warn" : ""}`}>{rs.fouls}</span>
                              <button className="tsm-fouls__btn" onClick={rs.onIncrement} disabled={subBoutDecided}>+</button>
                            </div>
                          </div>
                          <div className="tsm-fusensho">
                            <button
                              data-testid="scoring-modal-fusensho-button"
                              type="button"
                              className={`btn btn--sm ${s.fusensho === rs.key ? "btn--primary" : ""}`}
                              onClick={() => setFusenshoFor(idx, rs.key)}
                              title={s.fusensho === rs.key
                                ? `Click to undo fusensho — restores the previous score`
                                : `Mark bout as fusensho — default win 2-0 to ${rs.label}`}
                            >
                              {s.fusensho === rs.key
                                ? <>✓ <TermAS name="fusensho">Fusensho</TermAS></>
                                : <TermAS name="fusensho">Fusensho</TermAS>}
                            </button>
                          </div>
                        </div>
                      </div>
                      {rsIdx === 0 && (
                        <div className={`team-sub-match__score ${t.winner === "b" ? "team-sub-match__score--a-win" : t.winner === "a" ? "team-sub-match__score--b-win" : ""}`}>
                          {scoreDisplay}
                        </div>
                      )}
                    </React.Fragment>
                  ))}
                </div>
              </div>
            );
          })}
          </div>

          {/* Team summary — T138: sticky to the top of the modal body so
              the IV/PW totals stay visible as the operator scrolls through
              many bout rows (especially relevant on small screens / when
              every sub-match has been scored). zIndex: 5 keeps it under
              the modal head (10) but above the bout cells. */}
          <div className="team-summary" style={{ position: "sticky", top: 0, background: "var(--bg, white)", zIndex: 5, borderBottom: "1px solid var(--line, #ddd)", paddingBottom: 8 }}>
            {teamSides.map((ts, idx) => (
              <React.Fragment key={ts.key}>
                <div className="team-summary__side">
                  <div className="team-summary__label">{ts.label}</div>
                  <div className="team-summary__stats">IV: {ts.iv} · PW: {ts.pw}</div>
                </div>
                {idx === 0 && (
                  <div className="team-summary__result">
                    {teamWinner === "a" ? "AKA WIN" : teamWinner === "b" ? "SHIRO WIN" : "DRAW"}
                    <div style={{ fontSize: 14, opacity: 0.6, marginTop: 4 }}>RESULT</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>

          {/* T141: daihyosen (representative bout) affordance. Visible
              when the match is in the knockout stage AND all positions
              have been scored AND IV/PW tie. Click POSTs to /daihyosen;
              server validates and appends a new SubMatchResult with
              decision="daihyosen" that the operator then scores via the
              regular bout flow. Error responses are mapped to user-
              visible strings per the contract in handlers_daihyosen.go. */}
          {(() => {
            const allComplete = subTotals.every(t => t.aTotal > 0 || t.bTotal > 0 || t.winner !== null);
            const tied = ivA === ivB && pwA === pwB && (ivA + pwA + ivB + pwB) > 0;
            if (!isKnockoutPhase || !allComplete || !tied) return null;
            const onDaihyosen = async () => {
              setDaihyosenErr("");
              setDaihyosenBusy(true);
              try {
                await window.API.recordDaihyosen(m.compId, m.id, resolveDecisionPassword(password));
                if (!mountedRef.current) return;
                // Reload the modal data via parent: closing + reopening
                // is the cleanest cross-cutting refresh path. The parent
                // listens for SSE match_updated and will push the new
                // bout when re-opened.
                onClose();
              } catch (e) {
                if (!mountedRef.current) return;
                const msg = String(e?.message || "");
                let userMsg = msg;
                if (msg === "not_tied") userMsg = "Daihyosen requires a tied result on IV and PW";
                else if (msg === "pool_match") userMsg = "Daihyosen is only for knockout matches";
                else if (msg === "insufficient_eligibility") userMsg = "Not enough eligible competitors for a representative bout";
                setDaihyosenErr(userMsg);
              } finally {
                if (mountedRef.current) setDaihyosenBusy(false);
              }
            };
            return (
              <div className="daihyosen-controls" style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 12, padding: 12, border: "1px dashed var(--accent, #888)", borderRadius: 6, background: "var(--bg-2, #fafafa)" }}>
                <div style={{ fontSize: 12, fontWeight: 700 }}>Match tied on IV and PW</div>
                <div style={{ fontSize: 11, color: "var(--ink-3)" }}>Add a representative bout (<TermAS name="daihyosen">daihyosen</TermAS>) to break the tie. Each side picks one eligible competitor; the bout is scored like any other sub-match.</div>
                <div>
                  <button data-testid="scoring-modal-daihyosen-button" type="button" className="btn btn--primary btn--sm" onClick={onDaihyosen} disabled={daihyosenBusy}>
                    {daihyosenBusy ? "Adding…" : <>Add <TermAS name="daihyosen">daihyosen</TermAS></>}
                  </button>
                </div>
                {daihyosenErr && (
                  <div style={{ color: "var(--danger, #c00)", fontSize: 12 }}>{daihyosenErr}</div>
                )}
              </div>
            );
          })()}

          {/* T093–T098: decision (kiken/fusenpai) controls for the overall
              team match. Per-bout Fusensho lives on each sub-match row
              (see the row-level "Fusensho" button per side, T096). */}
          {!withdrawnPlayer && !decisionPromptKind && (
            <div className="decision-controls" style={{ display: "flex", gap: 8, marginTop: 12, fontSize: 12, alignItems: "center" }}>
              <span style={{ color: "var(--ink-3)", fontWeight: 600 }}>Team decision:</span>
              <button data-testid="scoring-modal-kiken-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("kiken"); }} disabled={submitting || decisionSubmitting}>
                <TermAS name="kiken">Kiken</TermAS>
              </button>
              <button data-testid="scoring-modal-fusenpai-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("fusenpai"); }} disabled={submitting || decisionSubmitting}>
                <TermAS name="fusenpai">Fusenpai</TermAS>
              </button>
              <span style={{ color: "var(--ink-3)", fontSize: 11, marginLeft: 4 }}>
                (<TermAS name="fusensho">Fusensho</TermAS> is per-bout — use the "Fusensho" button on each row above.)
              </span>
            </div>
          )}
          {decisionErr && (
            <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginTop: 6 }}>{decisionErr}</div>
          )}
          {decisionPromptKind && (
            <DecisionPrompt
              kind={decisionPromptKind}
              sideA={{ name: m.sideA?.name || m.sideA }}
              sideB={{ name: m.sideB?.name || m.sideB }}
              defaultSide="shiro"
              askReason={decisionPromptKind === "kiken"}
              submitting={decisionSubmitting}
              onCancel={() => { setDecisionPromptKind(""); setDecisionErr(""); }}
              onSubmit={({ decisionBy, decisionReason }) => submitDecision(decisionPromptKind, { decisionBy, decisionReason })}
            />
          )}
          {withdrawnPlayer && (
            <RemainingMatchesPanel
              compID={m.compId}
              password={resolveDecisionPassword(password)}
              withdrawnPlayer={withdrawnPlayer}
              onAwarded={() => { /* stay open; operator decides when to close */ }}
              onClose={() => { setWithdrawnPlayer(null); onClose(); }}
            />
          )}

        </div>

        <div className="editor-modal__foot editor-modal__foot--nav">
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>▶ Start</button>
              )}
              <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>
              {onSubmitAndNext ? (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmitAndNext(buildPatch("completed")))} disabled={submitting}>
                  {submitting ? "Saving…" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmit(buildPatch("completed")))} disabled={submitting}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : "Finish"}
                </button>
              )}
            </div>
            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting}>Next →</button>
            ) : <span />}
          </div>
        </div>
      </div>
    </div>
  );
}

window.ScoreEditorModal = ScoreEditorModal;

// ES exports for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
export {
  resolveDecisionPassword,
  buildDecisionBody,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  DecisionPrompt,
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  applyFusenshoToggle,
  getIpponButtons,
  getValidPointKeys,
};
