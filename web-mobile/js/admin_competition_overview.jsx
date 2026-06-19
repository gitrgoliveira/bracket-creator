// admin_competition_overview.jsx — Overview + Fighting-Spirit awards sections
// of the competition admin. Split out of admin_competition.jsx (mp-hpe3) as
// cohesive section modules; loaded before admin_competition.js in index.html
// and consumed by the AdminCompetition shell via window.*.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const compMatchStats = window.compMatchStats;

function AdminCompOverview({ c, pools, poolMatches, bracket, onSection }) {
  // Prefer the props passed from the detail fetch, but fall back to whatever
  // is already on `c` when the detail hasn't loaded yet (or errored). Using ??
  // avoids overwriting non-null fields on `c` with undefined prop values.
  const statsSource = { ...c, pools: pools ?? c.pools, poolMatches: poolMatches ?? c.poolMatches, bracket: bracket ?? c.bracket };
  const { total, done, running: runningCount } = compMatchStats(statsSource);
  const pct = total ? Math.round((done / total) * 100) : 0;
  const effectiveBracket = bracket ?? c.bracket;
  // Honour the OS reduced-motion preference for the progress-bar animation.
  const prefersReducedMotion = typeof window !== "undefined" && window.matchMedia
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;
  return (
    <div>
      <div className="stats-strip">
        <div className="stat-box"><div className="v">{c.players.length}</div><div className="l">{c.kind === "team" ? "Teams" : "Participants"}</div></div>
        <div className="stat-box"><div className="v">{c.players.filter((p) => p.seed).length}</div><div className="l">Seeded</div></div>
        <div className="stat-box"><div className="v">{done}/{total}</div><div className="l">Matches done</div></div>
        <div className="stat-box"><div className="v" style={{ color: runningCount > 0 ? "var(--red)" : "inherit" }}>{runningCount}</div><div className="l">Now</div></div>
      </div>
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="card__head"><div className="card__title">Progress</div><div className="card__sub">{pct}%</div></div>
        {/* Animate via transform: scaleX (compositor-only) rather than width,
            which forced a layout/paint each frame. The inner bar is full-width
            and scaled from the left edge; the rounded track clips it so the
            visual is equivalent. Transition is dropped under
            prefers-reduced-motion. */}
        <div style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }} role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div style={{
            height: "100%",
            width: "100%",
            background: "var(--accent)",
            transformOrigin: "left center",
            transform: `scaleX(${pct / 100})`,
            transition: prefersReducedMotion ? "none" : "transform 300ms",
          }}></div>
        </div>
      </div>
      <div className="row">
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection("scores")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
          <div className="card__sub">Update or correct match results</div>
        </button>
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection(effectiveBracket ? "bracket" : "pools")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Results →</div>
          <div className="card__sub">Visual bracket / pool standings</div>
        </button>
      </div>
    </div>
  );
}

// FightingSpiritAwardsEditor: free-text form for adding/removing/saving
// optional fighting-spirit (敢闘賞) awards for a competition. Each award has
// a title, recipient name, and optional dojo. Save calls
// API.updateCompetitionAwards (elevated-gated PUT /api/competitions/:id/awards).
// v1 = free-text only; no competitor picker (deferred).
function FightingSpiritAwardsEditor({ c, password, showToast }) {
  // Each row carries a stable client-only `_key` so React keys survive
  // mid-list removals (index keys cause inputs to "jump" / reuse the wrong
  // DOM node when a middle row is deleted). `_key` is stripped before save.
  const keyCounter = useRefA(0);
  const withKey = (a) => ({ ...a, _key: keyCounter.current++ });
  const [awards, setAwards] = useStateA(() => (c.fightingSpiritAwards || []).map(withKey));
  const [saving, setSaving] = useStateA(false);
  // `dirty` guards the prop-sync effect: the parent refetches the competition
  // on unrelated SSE events (match updates, status changes), which would
  // otherwise clobber the operator's typed-but-unsaved edits. We only re-sync
  // from props when there are no pending edits — except on a competition
  // SWITCH, where we always reset to the new comp's list.
  const [dirty, setDirty] = useStateA(false);
  const lastCompId = useRefA(c.id);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Sync from parent. Reset unconditionally when switching competitions;
  // otherwise skip while the user has unsaved edits (see `dirty` above).
  useEffectA(() => {
    const switched = lastCompId.current !== c.id;
    lastCompId.current = c.id;
    if (switched || !dirty) {
      setAwards((c.fightingSpiritAwards || []).map(withKey));
      if (switched) setDirty(false);
    }
  }, [c.id, c.fightingSpiritAwards, dirty]);

  const addRow = () => { setDirty(true); setAwards(prev => [...prev, withKey({ title: "Fighting Spirit", recipientName: "", recipientDojo: "" })]); };
  const removeRow = (idx) => { setDirty(true); setAwards(prev => prev.filter((_, i) => i !== idx)); };
  const updateField = (idx, field, val) => { setDirty(true); setAwards(prev => prev.map((a, i) => i === idx ? { ...a, [field]: val } : a)); };

  const save = async () => {
    const admin = window.promptAdminPassword ? await window.promptAdminPassword() : null;
    if (admin === null) return;
    setSaving(true);
    try {
      // Strip the client-only `_key` before sending to the API.
      const payload = awards.map(({ _key, ...rest }) => rest);
      await window.API.updateCompetitionAwards(c.id, payload, password, admin);
      // Edits are now persisted; clear dirty so the next prop refresh
      // (the save triggers an SSE refetch) re-syncs to the saved state.
      if (mountedRef.current) { setDirty(false); showToast("Fighting Spirit awards saved.", "success"); }
    } catch (e) {
      if (mountedRef.current) showToast(e.message || "Failed to save awards", "error");
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  };

  return (
    <div className="row">
      <div className="card" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div className="card__title">🔥 Fighting Spirit Awards <span style={{ fontWeight: 400, fontSize: 12, color: "var(--ink-3)" }}>(optional)</span></div>
        <div className="card__sub" style={{ marginTop: -4 }}>Record individual honourees independent of the placement podium. Shown to viewers on the Awards tab.</div>
      {awards.length === 0 && (
        <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>No awards yet.</div>
      )}
      {awards.map((a, idx) => (
        <div key={a._key} style={{ display: "flex", gap: 6, alignItems: "flex-start", flexWrap: "wrap" }}>
          <input
            className="input"
            placeholder="Title (e.g. Fighting Spirit)"
            value={a.title || ""}
            onChange={e => updateField(idx, "title", e.target.value)}
            style={{ flex: "1 1 140px", minWidth: 100 }}
            data-testid={`fs-award-title-${idx}`}
          />
          <input
            className="input"
            placeholder="Recipient name"
            value={a.recipientName || ""}
            onChange={e => updateField(idx, "recipientName", e.target.value)}
            style={{ flex: "2 1 180px", minWidth: 120 }}
            data-testid={`fs-award-name-${idx}`}
          />
          <input
            className="input"
            placeholder="Dojo (optional)"
            value={a.recipientDojo || ""}
            onChange={e => updateField(idx, "recipientDojo", e.target.value)}
            style={{ flex: "1 1 120px", minWidth: 90 }}
            data-testid={`fs-award-dojo-${idx}`}
          />
          <button
            className="btn btn--sm btn--ghost"
            onClick={() => removeRow(idx)}
            aria-label="Remove award"
            data-testid={`fs-award-remove-${idx}`}
          >✕</button>
        </div>
      ))}
      <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
        <button className="btn btn--sm btn--ghost" onClick={addRow} data-testid="fs-award-add">+ Add award</button>
        <button className="btn btn--sm btn--primary" onClick={save} disabled={saving} data-testid="fs-award-save">
          {saving && <span className="spinner" />}
          {saving ? "Saving…" : "Save awards"}
        </button>
      </div>
      </div>
    </div>
  );
}


window.AdminCompOverview = AdminCompOverview;
window.FightingSpiritAwardsEditor = FightingSpiritAwardsEditor;
