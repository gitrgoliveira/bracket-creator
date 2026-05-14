// Competition shell + the sections it embeds (Overview, Settings, Bracket).
// LiveMatchPanel is the bracket-side detail panel for picking match winners.
// See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const compMatchStats = window.compMatchStats;
const hasBothSides = window.hasBothSides;
const normalizeDate = window.normalizeDate;
const isValidISODate = window.isValidISODate;
const decideNumericUpdate = window.decideNumericUpdate;
// Use the canonical error strings + numeric bounds (admin_helpers.jsx)
// so saveNow's inline asymmetric validation stays in lockstep with
// validateAndNormalizeDate's messages and predicate, and so the
// team-size input cap stays in lockstep with TEAM_POSITIONS in the
// scoring modal.
const DATE_ERR_INVALID_FORMAT = window.DATE_ERR_INVALID_FORMAT;
const DATE_ERR_YEAR_RANGE = window.DATE_ERR_YEAR_RANGE;
const MIN_YEAR = window.MIN_YEAR;
const MAX_YEAR = window.MAX_YEAR;
const MAX_TEAM_SIZE = window.MAX_TEAM_SIZE;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const AdminParticipants = window.AdminParticipants;
const AdminPools = window.AdminPools;
const AdminScoreEditor = window.AdminScoreEditor;
const AdminExport = window.AdminExport;

const LiveMatchPanel = React.memo(({ match, compId, courts, onMoveCourt, onRecord, onOverride }) => {
  const [mode, setMode] = useStateA("tap");
  const [aPoints, setAPoints] = useStateA([]);
  const [bPoints, setBPoints] = useStateA([]);
  useEffectA(() => {
    setAPoints(match.score?.type === "ippon" && match.winner?.id === match.sideA?.id ? match.score.ippons || [] : []);
    setBPoints(match.score?.type === "ippon" && match.winner?.id === match.sideB?.id ? match.score.ippons || [] : []);
    // Include score/winner/status in deps so an SSE update for the same
    // match (e.g. an off-panel correction) doesn't leave the scoreboard
    // view showing stale points.
  }, [match.id, match.status, match.winner?.id, match.score?.type, match.score?.ippons?.join(",")]);
  const a = match.sideA, b = match.sideB;
  const isComplete = match.status === "completed";
  return (
    <div className="live-panel">
      <div className="live-panel__head">
        <div className="live-panel__title">Match · {match.id.slice(-6)}</div>
        <div className="live-panel__court">
          {onMoveCourt && courts && courts.length ? (
            <>
              <CourtPicker
                value={match.court}
                courts={courts}
                onChange={(cc) => onMoveCourt(compId, match.id, cc)}
                btnClassName="live-panel__court-btn"
                label="SHIAIJO "
              />
              <span> · {match.scheduledAt || "TBA"}</span>
            </>
          ) : (
            <span>SHIAIJO {match.court} · {match.scheduledAt || "TBA"}</span>
          )}
        </div>
      </div>
      <div className="mode-tabs">
        <button className={mode === "tap" ? "is-active" : ""} onClick={() => setMode("tap")}>Tap winner</button>
        <button className={mode === "card" ? "is-active" : ""} onClick={() => setMode("card")}>Match card</button>
        <button className={mode === "scoreboard" ? "is-active" : ""} onClick={() => setMode("scoreboard")}>Scoreboard</button>
      </div>
      {mode === "tap" && (<>
        {/* Layout convention: SHIRO (White, sideB) on the LEFT, AKA (Red, sideA) on the RIGHT. */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginBottom: 10 }}>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === b.id ? "var(--accent)" : "var(--line)", background: match.winner?.id === b.id ? "var(--accent)" : "var(--surface)", color: match.winner?.id === b.id ? "white" : "inherit" }} onClick={() => onRecord("b", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>SHIRO (WHITE)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{b.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{b.dojo}</div>
          </button>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === a.id ? "var(--red)" : "var(--line)", background: match.winner?.id === a.id ? "var(--red)" : "var(--surface)", color: match.winner?.id === a.id ? "white" : "inherit" }} onClick={() => onRecord("a", "ippon")}>
            {/* Label tinted red when unselected (button is on white), inherits white when selected (button background is red) */}
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em", color: match.winner?.id === a.id ? "inherit" : "var(--red)" }}>AKA (RED)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{a.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{a.dojo}</div>
          </button>
        </div>
        <div className="field__hint" style={{ textAlign: "center" }}>Tap the winner. Use Match card or Scoreboard for detail.</div>
      </>)}
      {mode === "card" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div><div className="score-side__dojo">{b.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--primary" onClick={() => onRecord("b", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("b", "hantei")}>Hantei</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div><div className="score-side__dojo">{a.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--danger" onClick={() => onRecord("a", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("a", "hantei")}>Hantei</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--shiro ${bPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{bPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{["M", "K", "D", "T"].map((cc) => (<button key={cc} className="ipt-btn" onClick={() => setBPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setBPoints([])}>↺</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--aka ${aPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{aPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{["M", "K", "D", "T"].map((cc) => (<button key={cc} className="ipt-btn ipt-btn--aka" onClick={() => setAPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setAPoints([])}>↺</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (() => {
        // Submit is only valid when one side strictly leads. Tied counts
        // (e.g. 1–1) would otherwise silently get attributed to SHIRO via
        // `aWins ? "a" : "b"`. For draws, use the full editor's hikiwake toggle.
        const aWins = aPoints.length > bPoints.length;
        const bWins = bPoints.length > aPoints.length;
        const hasWinner = aWins || bWins;
        const isTied = !hasWinner && (aPoints.length > 0 || bPoints.length > 0);
        return (
          <div className="live-panel__actions">
            <button
              className="btn btn--primary btn--full"
              disabled={!hasWinner}
              onClick={() => onRecord(aWins ? "a" : "b", "ippon", aWins ? aPoints[0] : bPoints[0])}
            >Submit result</button>
            {isTied && (
              <div className="field__hint" style={{ textAlign: "center", marginTop: 6 }}>
                Tied — open the full score editor to record a draw (hikiwake).
              </div>
            )}
          </div>
        );
      })()}
      {isComplete && (
        <div style={{ marginTop: 12, padding: 10, background: "#ecfdf5", border: "1px solid #a7f3d0", borderRadius: 8, fontSize: 12.5, color: "#065f46" }}>
          ✓ Recorded — {match.winner?.name} advances
        </div>
      )}
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px dashed var(--line)" }}>
        <button className="btn btn--sm btn--full" onClick={() => {
          // prompt() returns "   " for whitespace-only input — which is
          // truthy under `if (name)` and would persist a whitespace key
          // as `m.Winner` on the backend (and then mismatch the canonical
          // SideA / SideB names downstream). Trim defensively and only
          // override when there's a real value to record.
          const raw = prompt("Enter the name of the winner to override:", match.winner?.name || match.sideA?.name);
          const name = raw?.trim();
          if (name) onOverride(name);
        }}>Force winner (manual override)</button>
      </div>
    </div>
  );
});
LiveMatchPanel.displayName = "LiveMatchPanel";

function AdminCompOverview({ c, pools, poolMatches, bracket, onSection }) {
  // Prefer the props passed from the detail fetch, but fall back to whatever
  // is already on `c` when the detail hasn't loaded yet (or errored). Using ??
  // avoids overwriting non-null fields on `c` with undefined prop values.
  const statsSource = { ...c, pools: pools ?? c.pools, poolMatches: poolMatches ?? c.poolMatches, bracket: bracket ?? c.bracket };
  const { total, done, live } = compMatchStats(statsSource);
  const pct = total ? Math.round((done / total) * 100) : 0;
  const effectiveBracket = bracket ?? c.bracket;
  return (
    <div>
      <div className="stats-strip">
        <div className="stat-box"><div className="v">{c.players.length}</div><div className="l">{c.kind === "team" ? "Teams" : "Participants"}</div></div>
        <div className="stat-box"><div className="v">{c.players.filter((p) => p.seed).length}</div><div className="l">Seeded</div></div>
        <div className="stat-box"><div className="v">{done}/{total}</div><div className="l">Matches done</div></div>
        <div className="stat-box"><div className="v" style={{ color: live > 0 ? "var(--red)" : "inherit" }}>{live}</div><div className="l">Live now</div></div>
      </div>
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="card__head"><div className="card__title">Progress</div><div className="card__sub">{pct}%</div></div>
        <div style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }}>
          <div style={{ height: "100%", width: pct + "%", background: "var(--accent)", transition: "width 300ms" }}></div>
        </div>
      </div>
      <div className="row">
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection("scores")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
          <div className="card__sub">Update or correct match results</div>
        </button>
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection(effectiveBracket ? "bracket" : "pools")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Live results →</div>
          <div className="card__sub">Visual bracket / pool standings</div>
        </button>
      </div>
    </div>
  );
}

function AdminSettings({ c, tournament, onUpdate, onBack, password, showToast }) {
  const [lastSaved, setLastSaved] = useStateA(null);
  const [saveErr, setSaveErr] = useStateA(null);
  const [deleting, setDeleting] = useStateA(false);
  const [local, setLocal] = useStateA({ ...c });
  const debounceRef = useRefA(null);

  useEffectA(() => {
    setLocal(prev => {
      if (debounceRef.current) return prev;
      return { ...prev, ...c };
    });
  }, [c.id, c.name, c.date, c.startTime, c.poolSize, c.poolWinners, c.poolSizeMode, c.courts, c.roundRobin, c.withZekkenName]);

  const saveNow = (next) => {
    // Date validation here is intentionally NOT routed through
    // validateAndNormalizeDate (admin_helpers.jsx) because saveNow has a
    // unique asymmetry that the shared helper can't express cleanly:
    //   - shape-invalid + dateChanged   → block (operator changed to junk)
    //   - shape-invalid + !dateChanged  → ALLOW (preserve legacy/imported
    //                                     bad data so other fields can be
    //                                     edited without first fixing the
    //                                     date)
    //   - shape-valid + year-out-of-range (regardless of dateChanged)
    //                                  → block (a well-formed date with
    //                                     impossible year is a typo, not
    //                                     legacy data)
    // The other two date-validation sites (admin_setup.jsx handleSave +
    // create) don't have this asymmetry and delegate to the shared helper.
    const norm = normalizeDate(next.date);
    const dateIsValid = !!norm && /^\d{4}-\d{2}-\d{2}$/.test(norm);
    const dateChanged = next.date !== c.date;

    if (dateChanged && !dateIsValid) {
      setSaveErr(DATE_ERR_INVALID_FORMAT);
      return;
    }
    if (dateIsValid) {
      const year = parseInt(norm.substring(0, 4));
      if (year < MIN_YEAR || year > MAX_YEAR) {
        setSaveErr(DATE_ERR_YEAR_RANGE);
        return;
      }
    }

    // Trim before comparing AND before sending. The backend trims
    // `comp.Name` on save, so without normalizing here the JS-side
    // uniqueness check would compare "  Men's Cup  " against the
    // canonical "Men's Cup" and miss — landing two competitions with the
    // same effective name. Send the trimmed value so the value the user
    // sees in the input matches what the server stores.
    const trimmedName = (next.name || "").trim();
    if (trimmedName.toLowerCase() !== c.name.toLowerCase()) {
      const exists = (tournament.competitions || []).some(cc => cc.id !== c.id && cc.name.toLowerCase() === trimmedName.toLowerCase());
      if (exists) {
        setSaveErr(`Competition name "${trimmedName}" is already in use.`);
        return;
      }
    }

    // Normalize on save when valid (auto-cleans DD-MM-YYYY → ISO on any
    // save where the date round-trips cleanly). Otherwise preserve the
    // raw existing value so we don't clobber legacy data with `null`.
    const finalNext = { ...c, ...next, name: trimmedName, date: dateIsValid ? norm : next.date };
    Promise.resolve(onUpdate(finalNext)).then(() => {
      const now = new Date();
      setLastSaved(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
      setSaveErr(null);
    }).catch((e) => {
      setSaveErr(e?.message || "Save failed");
      showToast(e?.message || "Save failed", "error");
    });
  };

  const saveLater = (next) => {
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      debounceRef.current = null;
      saveNow(next);
    }, 400);
  };

  // Cancel any pending debounced save on unmount so the timer can't fire
  // saveNow() (and trigger state updates / API calls) after teardown.
  useEffectA(() => () => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  }, []);

  const update = (k, v) => {
    const next = { ...local, [k]: v };
    setLocal(next);
    saveLater(next);
  };

  const updateNow = (k, v) => {
    const next = { ...local, [k]: v };
    setLocal(next);
    saveNow(next);
  };

  // Number-input variant of `update`. Stores NaN in local state for empty
  // input so the render side can keep the display empty (see
  // decideNumericUpdate's contract). Skips saveLater when the parsed value
  // isn't a positive integer ≥ min — and cancels any pending debounced save
  // so the backend doesn't receive a stale good value while the user sees
  // an empty/invalid input. Without the cancel, a saveLater from an earlier
  // keystroke ("12") would still fire after the user cleared the input,
  // leaving server state mismatched with what's on screen until SSE refresh.
  const updateNumber = (k, raw, min = 1) => {
    const { value, shouldSave } = decideNumericUpdate(raw, min);
    const next = { ...local, [k]: value };
    setLocal(next);
    if (shouldSave) {
      saveLater(next);
    } else if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  };

  const toggleCourt = (cc) => {
    const nextCourts = local.courts.includes(cc) ? local.courts.filter((x) => x !== cc) : [...local.courts, cc].sort();
    if (nextCourts.length) updateNow("courts", nextCourts);
  };

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{
          fontSize: 12.5,
          padding: "4px 8px",
          borderRadius: 4,
          background: saveErr ? "var(--red-soft)" : lastSaved ? "var(--accent-soft)" : "transparent",
          color: saveErr ? "var(--red)" : "var(--accent)",
          fontWeight: 600,
          transition: "all 300ms"
        }}>
          {saveErr ? `⚠ ${saveErr}` : lastSaved ? `✓ Saved at ${lastSaved}` : ""}
        </div>
      </div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={local.name} onChange={(e) => update("name", e.target.value)} /></div>
        <div className="field">
          <label className="field__label">Date</label>
          {/* Picker bounds match the validation range (MIN_YEAR/MAX_YEAR */}
          {/* in admin_helpers.jsx) so a typed date can't pass validation */}
          {/* but be unreachable via the picker — and vice versa. */}
          <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={local.date} onChange={(e) => update("date", e.target.value)} />
          <div className="field__hint">Pick the competition day.</div>
        </div>
        <div className="field"><label className="field__label">Start time</label><input className="input" type="time" value={local.startTime} onChange={(e) => update("startTime", e.target.value)} /></div>
      </div>
      {local.kind === "team" && (
        <div className="field">
          <label className="field__label">Team size</label>
          {/* Cap is MAX_TEAM_SIZE (admin_helpers.jsx). TEAM_POSITIONS in */}
          {/* admin_scoring_modal.jsx is built from the same constant, so */}
          {/* this input can't allow a value the scoring UI doesn't render. */}
          {/* Render NaN as "" so clearing the input stays empty instead of */}
          {/* collapsing to "0"; updateNumber gates the debounced save so a */}
          {/* cleared/invalid value never lands on the backend as 0. */}
          <input
            className="input"
            type="number"
            min="1"
            max={MAX_TEAM_SIZE}
            value={Number.isFinite(local.teamSize) ? local.teamSize : ""}
            onChange={(e) => updateNumber("teamSize", e.target.value, 1)}
          />
        </div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo (courts)</label>
        <div className="radio-group">
          {tournament.courts.map((cc) => (
            <button key={cc} className={`radio-pill ${local.courts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)}>Shiaijo (court) {cc}</button>
          ))}
        </div>
        <div className="field__hint">Concurrency = number of shiaijo (courts) assigned. Schedule prevents double-booking with other competitions.</div>
      </div>
      {local.format === "pools" && (
        <>
          <div className="field">
            <label className="field__label">Pool size is a</label>
            <div className="radio-group">
              <button className={`radio-pill ${local.poolSizeMode === "max" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "max")}>maximum</button>
              <button className={`radio-pill ${local.poolSizeMode === "min" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "min")}>minimum</button>
            </div>
          </div>
          <div className="row">
            {/* Same NaN-as-"" + gated-save pattern as Team size above. */}
            {/* min=3 for poolSize matches the backend's pool-size lower */}
            {/* bound (3 players minimum to run a round-robin). */}
            <div className="field"><label className="field__label">Players per pool</label><input
              className="input"
              type="number"
              min="3"
              value={Number.isFinite(local.poolSize) ? local.poolSize : ""}
              onChange={(e) => updateNumber("poolSize", e.target.value, 3)}
            /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input
              className="input"
              type="number"
              min="1"
              value={Number.isFinite(local.poolWinners) ? local.poolWinners : ""}
              onChange={(e) => updateNumber("poolWinners", e.target.value, 1)}
            /></div>
          </div>
        </>
      )}
      <div className="field">
        <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={local.numberPrefix || ""} onChange={(e) => update("numberPrefix", e.target.value.substring(0, 3))} style={{ maxWidth: 80 }} />
        <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => updateNow("roundRobin", e.target.checked)} /> Round-robin in pools</label>
<div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => updateNow("withZekkenName", e.target.checked)} disabled={local.kind === "team"} /> Use Zekken display name</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : "When enabled, participant CSV uses three columns: Name, Zekken, Dojo."}</div>
        </div>
      </div>
      <div style={{ marginTop: 24, padding: 16, borderTop: "1px solid var(--line)" }}>
        <button className="btn btn--danger btn--ghost" disabled={deleting} onClick={async () => {
          const started = local.status && local.status !== "setup" && local.status !== "pending";
          const msg = started
            ? `"${local.name}" has already started. Deleting it will remove ALL matches and results. This cannot be undone. Continue?`
            : `Are you sure you want to delete "${local.name}"? This action cannot be undone.`;
          if (confirm(msg)) {
            setDeleting(true);
            try {
              const ok = await window.API.deleteCompetition(local.id, password);
              if (ok) onBack();
              else showToast("Failed to delete competition.", "error");
            } catch (e) {
              console.error("Delete competition failed:", e);
              showToast(e.message, "error");
            } finally {
              setDeleting(false);
            }
          }
        }}>
          {deleting && <span className="spinner" />}
          {deleting ? "Deleting…" : "Delete competition"}
        </button>
        <div className="field__hint" style={{ marginTop: 4 }}>Deleting a started competition will remove all matches and results.</div>
      </div>
    </div>
  );
}

function AdminBracket({ c, t, bracket, onMoveCourt, tweaks, password, showToast }) {
  const [selected, setSelected] = useStateA(null);
  const scrollRef = useRefA(null);
  const [autoScrollId, setAutoScrollId] = useStateA(null);

  // Recenter on the running match whenever it changes (initial bracket
  // load, or one match finishing and the next starting). Empty deps would
  // miss the case where `bracket` is still null on first mount and only
  // populates via the detail fetch / SSE.
  const runningMatchId = (bracket?.rounds || []).flatMap(r => r).find(m => m.status === "running")?.id || null;
  useEffectA(() => {
    if (runningMatchId) setAutoScrollId(runningMatchId + "::" + Date.now());
  }, [runningMatchId]);

  if (!bracket || !bracket.rounds) {
    return <div className="empty"><div className="icon">⚙</div><h3>Bracket not generated yet</h3><div>Start the competition to build the bracket.</div></div>;
  }
  const select = (m, ri, mi) => setSelected({ matchId: m.id, ri, mi });
  // Look up the selected match by ID rather than [ri][mi] index. The
  // index can go stale if an SSE-driven bracket rebuild (playoff
  // regeneration, source-comp promotion) reorders entries between the
  // user's click and the next render/action; the ID is the only stable
  // handle we set in `selected`. Returns null when the match has been
  // removed entirely from the bracket.
  const findSelectedMatch = () => {
    if (!selected || !bracket?.rounds) return null;
    for (const round of bracket.rounds) {
      for (const m of (round || [])) {
        if (m && m.id === selected.matchId) return m;
      }
    }
    return null;
  };
  const recordWinner = (winnerSide, _mode = "ippon", ipponLetter = null) => {
    const m = findSelectedMatch();
    if (!m) return;
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    if (!winner) return;

    const result = {
      winner: winner,
      status: "completed",
      ipponsA: winnerSide === "a" ? [ipponLetter || "M"] : [],
      ipponsB: winnerSide === "b" ? [ipponLetter || "M"] : [],
      score: { type: "ippon", winnerPts: 1, loserPts: 0, ippons: [ipponLetter || "M"], fouls: { a: 0, b: 0 } },
    };

    // Don't call onUpdate(c) on success — AdminApp's onUpdate is the
    // competition-config PUT, which would overwrite server state with
    // the (now-stale) c prop. SSE + patchCompetitionData in AdminApp
    // already refreshes the bracket after a recordScore.
    window.API.recordScore(c.id, m.id, result, password, m)
      .catch(err => showToast(err.message, "error"));
  };

  const overrideWinner = (winnerName) => {
    if (!selected) return;
    // Same reason as recordWinner: rely on SSE to refresh, don't
    // route the success path through the config-PUT callback.
    window.API.overrideBracketWinner(c.id, selected.matchId, winnerName, password)
      .catch(err => showToast(err.message, "error"));
  };
  const selectedMatch = findSelectedMatch();
  return (
    <div className="row" style={{ gridTemplateColumns: "1fr 360px", alignItems: "start" }}>
      <div>
        <div className="bracket-canvas" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={select}
              highlightedMatchId={selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
            />
          </div>
        </div>
      </div>
      <div>
        {hasBothSides(selectedMatch) ? (
          <LiveMatchPanel
            match={selectedMatch}
            compId={c.id}
            courts={t?.courts || []}
            onMoveCourt={onMoveCourt}
            onRecord={recordWinner}
            onOverride={overrideWinner}
          />
        ) : selectedMatch ? (
          <div className="empty"><h3>Match not ready</h3><div style={{ fontSize: 13 }}>Waiting for upstream winners.</div></div>
        ) : (
          <div className="empty"><div className="icon">👆</div><h3>Pick a match</h3><div style={{ fontSize: 13 }}>Click any match in the bracket to record results.</div></div>
        )}
      </div>
    </div>
  );
}

function AdminCompetition({ tournament, competition, pools, poolMatches, standings, bracket, reservedSlots, section, onSection, onBack, onOpenCompetition, onUpdate, onCreatePlayoff, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password, showToast }) {
  const c = competition;
  const t = tournament;
  const [starting, setStarting] = useStateA(false);

  // Use the shared isValidISODate (admin_helpers.jsx) which delegates to
  // normalizeDate for semantic validity — rejects "2026-13-32" / Feb 31 /
  // Feb 29 in non-leap years. Without this, the Start button would enable
  // for shape-valid-but-impossible dates that AdminSettings.saveNow's
  // stricter check would reject — letting the operator start a competition
  // with a date that can't be saved back.
  const isDateValid = isValidISODate;

  const start = async () => {
    showToast(`Starting ${c.name}…`);

    setStarting(true);
    try {
      const updated = await window.API.startCompetition(c.id, password);
      // Directly update the local state without calling updateCompetition (PUT)
      // updated is a CompetitionDetail (config, pools, bracket…); extract the flat config for the list
      const flatComp = updated.config || updated;
      const comps = (t.competitions || []).map(cc => cc.id === c.id ? { ...cc, ...flatComp } : cc);
      onUpdate({ ...t, competitions: comps });
      showToast(`${c.name} started`);
      onSection("scores");
    } catch (e) {
      console.error("Start competition failed:", e);
      showToast(e.message, "error");
    } finally {
      setStarting(false);
    }
  };

  const sections = [
    {
      sec: "Preparation", items: [
        { id: "overview", label: "Overview" },
        { id: "participants", label: "Participants & seeds" },
        { id: "settings", label: "Settings" },
      ]
    },
    {
      sec: "Run", items: [
        pools ? { id: "pools", label: "Pools — live" } : null,
        bracket ? { id: "bracket", label: "Bracket — live" } : null,
        { id: "scores", label: "Scores — edit" },
      ].filter(Boolean)
    },
    {
      sec: "Output", items: [
        { id: "export", label: "Export & print" },
      ]
    },
  ];

  const currentItem = sections.flatMap(m => m.items).find(i => i.id === section);

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <Breadcrumbs items={[
          { label: "Dashboard", onClick: onBack },
          { label: c.name, onClick: section === "overview" ? null : () => onSection("overview") },
          currentItem && section !== "overview" ? { label: currentItem.label } : null
        ].filter(Boolean)} />
        <div className="page-head">
          <div>
            <div className="page-head__eyebrow">{t.name} ›</div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{c.name}</h1>
              <StatusBadge status={c.status} />
            </div>
            <div className="page-head__sub">
              {window.competitionKindLabel(c)} · {c.players.length} {c.kind === "team" ? "teams" : "players"} ·
              {c.date && ` ${formatDate(c.date)} at `} {c.startTime} · {c.courts.join(", ")}
            </div>
          </div>
          <div className="page-head__actions" style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
            {c.status === "setup" && c.players.length >= 2 && (
              <>
                <button className="btn btn--primary" onClick={start} disabled={!isDateValid(c.date) || starting}>
                  {starting && <span className="spinner" />}
                  {starting ? "Starting…" : "Start competition →"}
                </button>
                {!isDateValid(c.date) && (
                  <div style={{ color: "var(--red)", fontSize: 11, fontWeight: 600 }}>
                    ⚠ Cannot start: invalid date in Settings tab (e.g. "{c.date}")
                  </div>
                )}
              </>
            )}
            {c.format === "pools" && c.status !== "setup" && onCreatePlayoff && (() => {
              const playoffName = c.name + " - Playoffs";
              const hasPlayoff = (t.competitions || []).some(cc => cc.name === playoffName);
              return hasPlayoff
                ? <div style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>Playoff bracket already created</div>
                : <button className="btn btn--primary" onClick={() => onCreatePlayoff(c.id)}>Create playoff bracket →</button>;
            })()}
          </div>
        </div>

        <div className="workspace">
          <div className="side-nav">
            {sections.map((sec) => (
              <div key={sec.sec}>
                <div className="side-nav__sec">{sec.sec}</div>
                {sec.items.map((it) => (
                  <button key={it.id} className={section === it.id ? "is-active" : ""} onClick={() => onSection(it.id)}>{it.label}</button>
                ))}
              </div>
            ))}
            <div>
              <div className="side-nav__sec">Other competitions</div>
              {t.competitions.filter((cc) => cc.id !== c.id).map((cc) => (
                <button key={cc.id} onClick={() => onOpenCompetition(cc.id)}>{cc.name}</button>
              ))}
            </div>
          </div>
          <div>
            {section === "overview" && <AdminCompOverview c={c} pools={pools} poolMatches={poolMatches} bracket={bracket} onSection={onSection} />}
            {section === "participants" && <AdminParticipants c={c} tournament={t} reservedSlots={reservedSlots || []} onUpdate={onUpdate} password={password} showToast={showToast} onSection={onSection} />}
            {section === "settings" && <AdminSettings c={c} tournament={t} onUpdate={onUpdate} onBack={onBack} password={password} showToast={showToast} />}
            {section === "pools" && <AdminPools c={c} pools={pools} standings={standings} tweaks={tweaks} onEditScore={onEditScore} password={password} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onMoveCourt={onMoveCourt} tweaks={tweaks} password={password} showToast={showToast} />}
            {section === "scores" && <AdminScoreEditor c={c} t={t} onEditScore={onEditScore} onMoveCourt={onMoveCourt} restrictToCompId={c.id} />}
            {section === "export" && <AdminExport c={c} t={t} password={password} />}
          </div>
        </div>
      </div>
    </div>
  );
}

window.AdminCompetition = AdminCompetition;
