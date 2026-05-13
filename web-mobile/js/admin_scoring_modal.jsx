// Score-entry modals used by the schedule and competition pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA } = React;

// Reusable foul counter: independent +/- buttons per side with clear labeling
function FoulCounter({ label, fouls, setFouls, color, hansokuPts }) {
  return (
    <div className={`foul-counter foul-counter--${color}`}>
      <div className="foul-counter__label">{label} Fouls</div>
      <div className="foul-counter__controls">
        <button className="foul-counter__btn foul-counter__btn--dec" onClick={() => setFouls(f => Math.max(0, f - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 2 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
          {hansokuPts > 0 && <span className="foul-counter__h">→ +{hansokuPts}H to opp.</span>}
        </div>
        <button className="foul-counter__btn foul-counter__btn--inc" onClick={() => setFouls(f => Math.min(4, f + 1))} disabled={fouls >= 4}>+</button>
      </div>
    </div>
  );
}

function ScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext }) {
  const m = match;
  const isComplete = m.status === "completed";
  const isTeam = m.compKind === "team";
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} />;

  const initialAPts = m.ipponsA?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? m.score.ippons || [] : []);
  const initialBPts = m.ipponsB?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? m.score.ippons || [] : []);

  // Use ?? not || so an explicit 0 isn't treated as "unset"
  const initialAFouls = m.hansokuA ?? m.score?.fouls?.a ?? 0;
  const initialBFouls = m.hansokuB ?? m.score?.fouls?.b ?? 0;
  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  const [aFouls, setAFouls] = useStateA(initialAFouls);
  const [bFouls, setBFouls] = useStateA(initialBFouls);
  const [submitting, setSubmitting] = useStateA(false);

  // Hansoku → ippon awarded to opponent on every 2nd foul
  const aHansokuPts = Math.floor(bFouls / 2);
  const bHansokuPts = Math.floor(aFouls / 2);
  const aTotal = aPts.filter((x) => x !== "•").length + aHansokuPts;
  const bTotal = bPts.filter((x) => x !== "•").length + bHansokuPts;

  const addPt = (side, letter) => {
    if (side === "a") setAPts((p) => p.length < 2 ? [...p, letter] : p);
    else setBPts((p) => p.length < 2 ? [...p, letter] : p);
  };
  const removePt = (side, idx) => {
    if (side === "a") setAPts((p) => p.filter((_, i) => i !== idx));
    else setBPts((p) => p.filter((_, i) => i !== idx));
  };

  const buildPatch = (targetStatus) => {
    const fouls = { a: aFouls, b: bFouls };
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0 };
    if (targetStatus === "running") return {
      status: "running", winner: null,
      ipponsA: aPts.filter(x => x !== "•"), ipponsB: bPts.filter(x => x !== "•"),
      hansokuA: aFouls, hansokuB: bFouls,
      score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls, live: true, corrected: isComplete },
    };
    if (isDrawToggled) return { winner: null, ipponsA: [], ipponsB: [], hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete } };
    // ippon
    const aLetters = aPts.filter(x => x !== "•");
    const bLetters = bPts.filter(x => x !== "•");
    const aFinal = [...aLetters, ...Array(aHansokuPts).fill("H")].slice(0, 2);
    const bFinal = [...bLetters, ...Array(bHansokuPts).fill("H")].slice(0, 2);
    const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
    if (!winnerSide) return { winner: null, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete } };
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const ippons = winnerSide === "a" ? aFinal : bFinal;
    return { winner, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "ippon", winnerPts: ippons.length, loserPts: (winnerSide === "a" ? bFinal : aFinal).length, ippons, fouls, corrected: isComplete } };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { setSubmitting(false); }
  };

  // isHikiwake accepts both the canonical "hikiwake" and legacy "hikewake"
  // for backward compatibility with state files written before normalization.
  const initialIsDrawToggled = window.isHikiwake(m.score?.type) || window.isHikiwake(m.decision);
  const [isDrawToggled, setIsDrawToggled] = useStateA(initialIsDrawToggled);

  // Arranged as [left, right] — left is always SHIRO (White), right is always AKA (Red)
  const sides = [
    { key: "b", name: m.sideB?.name, dojo: m.sideB?.dojo, pts: bPts, fouls: bFouls, setFouls: setBFouls, hansokuPts: bHansokuPts, color: "shiro", label: "SHIRO (White)" },
    { key: "a", name: m.sideA?.name, dojo: m.sideA?.dojo, pts: aPts, fouls: aFouls, setFouls: setAFouls, hansokuPts: aHansokuPts, color: "aka", label: "AKA (Red)" },
  ];

  const canFinish = isDrawToggled || aTotal > 0 || bTotal > 0;

  const isDirty =
    !window.arraysEqual(aPts, initialAPts) ||
    !window.arraysEqual(bPts, initialBPts) ||
    aFouls !== initialAFouls ||
    bFouls !== initialBFouls ||
    isDrawToggled !== initialIsDrawToggled;
  const handleDismiss = () => {
    if (submitting) return; // don't close while save is in-flight
    if (isDirty && !confirm("Discard unsaved scoring changes?")) return;
    onClose();
  };

  // Keyboard shortcuts:
  //   Shift+M/K/D/T/H  → award point to AKA (red, sideA)
  //   m/k/d/t/h        → award point to SHIRO (white, sideB)
  //   x / X            → toggle hikiwake (draw)
  //   ←/→              → previous / next match (skipped inside text-entry elements)
  //   Enter            → finish (or finish + start next when available)
  //   Esc              → close the modal (respects dirty-state confirm)
  // Scoring shortcuts (Enter/M/K/D/T/H/X) are skipped when any interactive
  // element (input, button, link, …) has focus so native activation still works.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, canFinish, isDrawToggled, handleDismiss, onPrev, onNext, onSubmit, onSubmitAndNext, buildPatch, addPt, doSubmit };

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
      if ("MKDTH".includes(upper) && k.length === 1) {
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
          setIsDrawToggled(false);
        } else {
          setIsDrawToggled(true);
          setAPts([]); setBPts([]);
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []); // listener registered once; reads fresh state via kbRef

  return (
    <div className="modal-backdrop" onClick={handleDismiss}>
      <div className="editor-modal editor-modal--lg" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 700 }}>
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
            </div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, letterSpacing: "-0.01em" }}>
              Shiaijo {m.court} · {m.scheduledAt || "Live"}
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
                        {["M", "K", "D", "T", "H"].map((cc) => (
                          <button key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={s.pts.length >= 2}>{cc}</button>
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
                              title="Mark as draw (hikiwake)"
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
                    color={s.color}
                    hansokuPts={s.hansokuPts}
                  />
                ))}
              </div>
          </div>

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

const TEAM_POSITIONS = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];

function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext }) {
  const m = match;
  const isComplete = m.status === "completed";
  const positions = TEAM_POSITIONS.slice(0, teamSize);
  const [submitting, setSubmitting] = useStateA(false);

  const existingSub = m.subResults || [];
  const initSubs = positions.map((_, idx) => {
    const existing = existingSub.find(s => s.position === idx + 1);
    return {
      aPts: existing ? (existing.ipponsA || []).filter(x => x !== "•") : [],
      bPts: existing ? (existing.ipponsB || []).filter(x => x !== "•") : [],
      aFouls: existing ? existing.hansokuA || 0 : 0,
      bFouls: existing ? existing.hansokuB || 0 : 0,
    };
  });
  const [subs, setSubs] = useStateA(initSubs);

  const updateSub = (idx, fn) => setSubs(prev => prev.map((s, i) => i === idx ? fn(s) : s));

  const subTotals = subs.map(s => {
    const aH = Math.floor(s.bFouls / 2);
    const bH = Math.floor(s.aFouls / 2);
    const aT = s.aPts.length + aH;
    const bT = s.bPts.length + bH;
    const winner = aT > bT ? "a" : bT > aT ? "b" : null;
    return { aTotal: aT, bTotal: bT, aHansoku: aH, bHansoku: bH, winner };
  });

  const ivA = subTotals.filter(s => s.winner === "a").length;
  const ivB = subTotals.filter(s => s.winner === "b").length;
  const pwA = subTotals.reduce((sum, s) => sum + s.aTotal, 0);
  const pwB = subTotals.reduce((sum, s) => sum + s.bTotal, 0);
  const teamWinner = ivA > ivB ? "a" : ivB > ivA ? "b" : pwA > pwB ? "a" : pwB > pwA ? "b" : null;

  const buildPatch = (targetStatus) => {
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
    const subResults = subs.map((s, idx) => {
      const t = subTotals[idx];
      const aAll = [...s.aPts, ...Array(t.aHansoku).fill("H")].slice(0, 2);
      const bAll = [...s.bPts, ...Array(t.bHansoku).fill("H")].slice(0, 2);
      const w = t.winner === "a" ? m.sideA : t.winner === "b" ? m.sideB : null;
      return {
        position: idx + 1,
        sideA: typeof m.sideA === "object" ? m.sideA?.name : m.sideA,
        sideB: typeof m.sideB === "object" ? m.sideB?.name : m.sideB,
        ipponsA: aAll,
        ipponsB: bAll,
        hansokuA: s.aFouls,
        hansokuB: s.bFouls,
        winner: w ? (typeof w === "object" ? w.name : w) : "",
        decision: t.winner === null ? "hikiwake" : "",
      };
    });
    const winner = teamWinner === "a" ? m.sideA : teamWinner === "b" ? m.sideB : null;
    return {
      winner,
      status: targetStatus === "running" ? "running" : "completed",
      ipponsA: [],
      ipponsB: [],
      score: { type: teamWinner ? "ippon" : "hikiwake", winnerPts: teamWinner === "a" ? ivA : ivB, loserPts: teamWinner === "a" ? ivB : ivA, fouls: { a: 0, b: 0 }, corrected: isComplete },
      subResults,
    };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { setSubmitting(false); }
  };

  // Match ScoreEditorModal: don't dismiss while a save is in-flight,
  // otherwise unmounting mid-submit causes a setState after unmount.
  const handleDismiss = () => {
    if (submitting) return;
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
    <div className="modal-backdrop" onClick={handleDismiss}>
      <div className="editor-modal editor-modal--team" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 700 }}>
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
            </div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, letterSpacing: "-0.01em" }}>
              Shiaijo {m.court} · {m.scheduledAt || "Live"}
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

          {/* Individual match rows */}
          {positions.map((pos, idx) => {
            const s = subs[idx];
            const t = subTotals[idx];

            // Each row: [left side, center score, right side] — left=SHIRO, right=AKA
            const rowSides = [
              { key: "b", pts: s.bPts, fouls: s.bFouls, hansokuPts: t.bHansoku,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, bPts: pts })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, bFouls: f })),
                color: "shiro", label: "SHIRO" },
              { key: "a", pts: s.aPts, fouls: s.aFouls, hansokuPts: t.aHansoku,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, aPts: pts })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, aFouls: f })),
                color: "aka", label: "AKA" },
            ];

            const scoreDisplay = (() => {
              if (t.winner === null && t.aTotal === 0 && t.bTotal === 0) return <span style={{ color: "var(--ink-3)" }}>–</span>;
              if (t.winner === null) return <span className="tsm-draw">X</span>;
              return <span>{`${t.bTotal}–${t.aTotal}`}</span>;
            })();

            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos">Match {pos}</div>
                <div className="team-sub-match__row">
                  {rowSides.map((rs, rsIdx) => (
                    <React.Fragment key={rs.key}>
                      <div className={`team-sub-match__side ${rsIdx === 1 ? "team-sub-match__side--right" : ""}`}>
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
                          {["M", "K", "D", "T", "H"].map(cc => (
                            <button key={cc} className={`ipt-btn ipt-btn--sm ${cc === "H" ? "ipt-btn--h" : ""}`}
                              onClick={() => rs.setPts(rs.pts.length < 2 ? [...rs.pts, cc] : rs.pts)}
                              disabled={rs.pts.length >= 2}>{cc}</button>
                          ))}
                        </div>
                        {/* Independent foul counter */}
                        <div className="tsm-fouls">
                          <span className="tsm-fouls__label">{rs.label} Fouls</span>
                          <div className="tsm-fouls__controls">
                            <button className="tsm-fouls__btn" onClick={() => rs.setFouls(f => Math.max(0, f - 1))} disabled={rs.fouls === 0}>−</button>
                            <span className={`tsm-fouls__count ${rs.fouls >= 2 ? "tsm-fouls__count--warn" : ""}`}>{rs.fouls}</span>
                            <button className="tsm-fouls__btn" onClick={() => rs.setFouls(f => Math.min(4, f + 1))} disabled={rs.fouls >= 4}>+</button>
                          </div>
                          {rs.hansokuPts > 0 && <span className="tsm-fouls__h">→ +{rs.hansokuPts}H</span>}
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

          {/* Team summary */}
          <div className="team-summary">
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
