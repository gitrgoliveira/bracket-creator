// admin_competition_overview.jsx — Overview + Fighting-Spirit awards sections
// of the competition admin. Split out of admin_competition.jsx (mp-hpe3) as
// cohesive section modules; loaded only via the admin_competition.js entry's
// imports (no standalone <script> tag) and consumed by the AdminCompetition
// shell via window.*.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const compMatchStats = window.compMatchStats;

// ---------------------------------------------------------------------------
// Pure helper: competitionNextSteps
// ---------------------------------------------------------------------------
// Returns an ordered array of step descriptors for the "Next steps" checklist
// shown in the setup state. Each step is a plain object so the function is
// unit-testable without mounting any component.
//
// Shape:
//   { id: string, label: string, detail: string,
//     state: 'done'|'active'|'todo', section: string|null }
//
// Rules:
//   - "Create competition" is always done (the comp already exists).
//   - "Add participants" is done when players >= 2, else active.
//   - "Review seeds & settings" is always emitted as a non-done step: there is
//     no reliable "seeds reviewed" signal, so it never auto-completes (its
//     detail line reflects the current seed count, but its state stays
//     todo/active and is resolved by the active-step pass below). Its CTA
//     navigates to the "settings" section (format, courts, duration); seeds
//     are accessible from the seeding card on the "participants" page.
//   - Team comps get an extra "Set team lineups" step (→ "lineups") inserted
//     AFTER "Review seeds & settings" and before "Generate the draw". This is
//     the checklist's logical setup order; the sidebar lists lineups after
//     participants but before settings, which differs from this ordering.
//   - "Generate the draw" is always non-done (the button is in the page-head).
//   - The FIRST non-done step is promoted to "active"; the rest stay "todo".
//
// Exported on window.competitionNextSteps AND in the ES export block below.
function competitionNextSteps(c) {
  const players = (c && c.players) || [];
  const numPlayers = players.length;
  const isTeam = c && c.kind === "team";
  const hasEnoughPlayers = numPlayers >= 2;
  const seededCount = players.filter(p => p && p.seed).length;

  const steps = [];

  // Step 1 — always done.
  steps.push({
    id: "create",
    label: "Create competition",
    detail: `${c && c.name ? `"${c.name}" created` : "Competition created"}`,
    state: "done",
    section: null,
    cta: null,
  });

  // Step 2 — participants.
  const participantDetail = hasEnoughPlayers
    ? `${numPlayers} ${isTeam ? "teams" : "participants"} added${seededCount > 0 ? `, ${seededCount} seeded` : ""}`
    : `${numPlayers} ${isTeam ? "team" : "participant"}${numPlayers !== 1 ? "s" : ""} added — need at least 2`;
  steps.push({
    id: "participants",
    label: `Add ${isTeam ? "teams" : "participants"}`,
    detail: participantDetail,
    state: hasEnoughPlayers ? "done" : "active",
    section: "participants",
    // `cta` is the button label shown only when this step is active. Authored
    // here (not in JSX) so the copy is centralised and unit-testable; null for
    // steps with no navigable action (the generate step lives in the header).
    cta: `Add ${isTeam ? "teams" : "participants"} →`,
  });

  // Step 3 — seeds & settings (becomes active once participants done).
  steps.push({
    id: "settings",
    label: "Review seeds & settings",
    detail: seededCount > 0
      ? `${seededCount} seed${seededCount !== 1 ? "s" : ""} assigned`
      : "Optionally assign seeds and adjust format",
    state: "todo",
    section: "settings",
    cta: "Review settings →",
  });

  // Step 3b (team only) — lineups, after settings.
  if (isTeam) {
    steps.push({
      id: "lineups",
      label: "Set team lineups",
      detail: "Assign positions (Senpo, Jiho, …) before the draw",
      state: "todo",
      section: "lineups",
      cta: "Set lineups →",
    });
  }

  // Final step — generate draw (button is in the page-head; no section nav).
  steps.push({
    id: "generate",
    label: "Generate the draw",
    detail: hasEnoughPlayers
      ? "Use the \"Generate draw\" button in the header above"
      : "Add at least 2 participants first",
    state: "todo",
    section: null,
    cta: null,
  });

  // Resolve active/todo: the first non-done step is active; the rest are todo.
  let foundActive = false;
  for (const step of steps) {
    if (step.state === "done") continue;
    if (!foundActive) {
      step.state = "active";
      foundActive = true;
    }
    // else stays "todo"
  }

  return steps;
}

// ---------------------------------------------------------------------------
// Pure helper: overviewViewMode
// ---------------------------------------------------------------------------
// Maps a CompetitionStatus wire value to the overview's render mode. The wire
// values (internal/state/models.go:441-446) are:
//   "setup" → "draw-ready" → "pools" / "playoffs" (the two RUNNING phases) →
//   "completed", plus "invalid". There is NO literal "running" status — an
//   active competition reports its phase ("pools" or "playoffs"). Anything that
//   is not setup/draw-ready/completed (including "pools", "playoffs", "invalid",
//   or any future phase) maps to "running", so an active competition keeps the
//   progress + scores/results widgets instead of mis-rendering the "completed"
//   card. Extracted as a pure function so this mapping is unit-tested and the
//   regression (treating only the non-existent "running" string as active)
//   cannot silently return. Exported on window + ES.
function overviewViewMode(status) {
  if (!status || status === "setup") return "setup";
  if (status === "draw-ready") return "draw-ready";
  if (status === "completed") return "completed";
  return "running";
}

// ---------------------------------------------------------------------------
// Pure helper: overviewResultsSection
// ---------------------------------------------------------------------------
// The section that holds standings/results for a given competition format.
// Swiss competitions have no pools/bracket section — their results live in the
// dedicated "swiss" section — so a Results/podium CTA that defaulted to
// pools/bracket would strand the operator on an empty/hidden view. Route swiss
// to "swiss"; otherwise prefer the bracket when one exists, else pools.
// Pure + exported so the swiss routing is unit-tested (Copilot review #312).
function overviewResultsSection(format, hasBracket) {
  if (format === "swiss") return "swiss";
  return hasBracket ? "bracket" : "pools";
}

// ---------------------------------------------------------------------------
// AdminCompOverview — status-aware overview component
// ---------------------------------------------------------------------------
function AdminCompOverview({ c, tournament, pools, poolMatches, bracket, onSection, password }) {
  // Prefer the props passed from the detail fetch, but fall back to whatever
  // is already on `c` when the detail hasn't loaded yet (or errored). Using ??
  // avoids overwriting non-null fields on `c` with undefined prop values.
  const statsSource = {
    ...c,
    pools: pools ?? c.pools,
    poolMatches: poolMatches ?? c.poolMatches,
    bracket: bracket ?? c.bracket,
  };
  const { total, done, running: runningCount } = compMatchStats(statsSource);
  const effectiveBracket = bracket ?? c.bracket;

  // Honour the OS reduced-motion preference for the progress-bar animation.
  const prefersReducedMotion = typeof window !== "undefined" && window.matchMedia
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;

  // ---------------------------------------------------------------------------
  // Status derivation (single source of truth, derived once)
  // ---------------------------------------------------------------------------
  // CompetitionStatus wire values (internal/state/models.go:441-446):
  //   "setup" → "draw-ready" → "pools"/"playoffs" (running phases) → "completed",
  //   plus "invalid". There is NO literal "running" status — an active
  //   competition is in its phase status ("pools" or "playoffs"). isRunning is
  //   therefore the catch-all for any status that is not setup/draw-ready/
  //   completed, which also lets an "invalid" competition keep the progress +
  //   scores/results widgets (matching the pre-redesign always-on behaviour)
  //   rather than mis-rendering the "Competition complete" card.
  // viewMode ∈ { setup, draw-ready, running, completed }. The render helpers
  // branch on isSetup/isDrawReady/isRunning and treat the remaining case
  // (completed) as the trailing else, so no separate isCompleted flag is needed.
  const viewMode = overviewViewMode(c.status);
  const isSetup = viewMode === "setup";
  const isDrawReady = viewMode === "draw-ready";
  const isRunning = viewMode === "running";

  // ---------------------------------------------------------------------------
  // Schedule estimate fetch — same AbortController pattern as AdminSettings.
  // Never blocks render: estimate starts as null and fills in asynchronously.
  // ---------------------------------------------------------------------------
  const [estimate, setEstimate] = useStateA(null);
  const [estimateLoading, setEstimateLoading] = useStateA(false);
  const [estimateErr, setEstimateErr] = useStateA(null);

  useEffectA(() => {
    if (!c.id) return;
    const controller = new AbortController();
    setEstimateLoading(true);
    setEstimateErr(null);
    window.API.estimateCompetitionSchedule(c.id, password, controller.signal)
      .then(res => {
        setEstimate(res);
        setEstimateLoading(false);
      })
      .catch(e => {
        if (!controller.signal.aborted) {
          setEstimateErr(e.message || "Failed to estimate");
          setEstimateLoading(false);
        }
      });
    return () => controller.abort();
    // Re-fetch when config, participant count, or tournament-level timing changes.
    // When check-in is enabled the backend counts only checked-in participants, so
    // toggling check-in changes the effective count even if roster size stays fixed.
    // Tournament ceremony/timing fields mirror AdminSettings' estimate effect deps.
  }, [c.id, c.format, c.kind, c.poolMatchDuration, c.playoffMatchDuration, c.courts,
    c.teamSize, c.poolSize, c.poolSizeMode, c.poolWinners, c.roundRobin, c.poolFormat,
    c.swissRounds, c.checkInEnabled, password,
    (c.players || []).length,
    c.checkInEnabled ? (c.players || []).filter(p => p && p.checkedIn).length : 0,
    tournament?.openingBlock, tournament?.lunchBlock, tournament?.closingBlock,
    tournament?.clockToElapsedMultiplier, tournament?.slowestCourtBufferPct]);

  // ---------------------------------------------------------------------------
  // Derived stat values
  // ---------------------------------------------------------------------------
  const participantLabel = c.kind === "team" ? "Teams" : "Participants";
  const numPlayers = (c.players || []).length;
  const seededCount = (c.players || []).filter(p => p && p.seed).length;
  const numCourts = (typeof window !== "undefined" && window.courtCount)
    ? window.courtCount(c.courts)
    : (Array.isArray(c.courts) ? c.courts.length : 0) || 1;

  const formatCompMinutes = (typeof window !== "undefined" && window.formatCompMinutes)
    ? window.formatCompMinutes
    : (m) => (Number.isFinite(m) && m > 0 ? `${m}m` : null);

  const estDuration = formatCompMinutes(estimate && estimate.totalDurationMinutes) || "—";

  // draw-ready: pool count vs bracket size
  const poolCount = (Array.isArray(pools) && pools.length > 0) ? pools.length : (c.pools ? c.pools.length : null);
  const bracketRounds = effectiveBracket && effectiveBracket.rounds ? effectiveBracket.rounds.length : null;

  const pct = total ? Math.round((done / total) * 100) : 0;

  // Stat values that are short numbers use the default 22px display size; long
  // or textual values ("2 pools", "3h 10m") step down to 16px via .v--sm so
  // they don't crowd the box. The em-dash placeholder keeps the display size.
  const vClass = (val) => (val === "—" ? "v" : "v v--sm");

  // Standings/results target for this competition's format (see helper above).
  const resultsSection = overviewResultsSection(c.format, !!effectiveBracket);

  // ---------------------------------------------------------------------------
  // Render helpers
  // ---------------------------------------------------------------------------

  // Stats strip — 4 boxes, varies by state.
  function renderStatsStrip() {
    if (isSetup) {
      return (
        <div className="stats-strip">
          <div className="stat-box">
            <div className="v">{numPlayers}</div>
            <div className="l">{participantLabel}</div>
          </div>
          <div className="stat-box">
            <div className="v">{seededCount}</div>
            <div className="l">Seeded</div>
          </div>
          <div className="stat-box">
            <div className="v">{numCourts}</div>
            <div className="l">{numCourts === 1 ? "Court" : "Courts"}</div>
          </div>
          <div className="stat-box">
            <div className={vClass(estDuration)}>{estDuration}</div>
            <div className="l">Est. duration{estimateLoading ? " …" : ""}</div>
          </div>
        </div>
      );
    }

    if (isDrawReady) {
      const drawSizeVal = c.format === "playoffs" || (!poolCount && bracketRounds)
        ? (bracketRounds ? `${Math.pow(2, bracketRounds - 1) * 2} max` : "—")
        : (poolCount ? `${poolCount} pool${poolCount !== 1 ? "s" : ""}` : "—");
      const drawSizeLabel = c.format === "playoffs" ? "Bracket size" : "Pools";
      return (
        <div className="stats-strip">
          <div className="stat-box">
            <div className="v">{numPlayers}</div>
            <div className="l">{participantLabel}</div>
          </div>
          <div className="stat-box">
            <div className="v">{seededCount}</div>
            <div className="l">Seeded</div>
          </div>
          <div className="stat-box">
            <div className="v v--sm">{drawSizeVal}</div>
            <div className="l">{drawSizeLabel}</div>
          </div>
          <div className="stat-box">
            <div className={vClass(estDuration)}>{estDuration}</div>
            <div className="l">Est. duration{estimateLoading ? " …" : ""}</div>
          </div>
        </div>
      );
    }

    if (isRunning) {
      return (
        <div className="stats-strip">
          <div className="stat-box">
            <div className="v">{numPlayers}</div>
            <div className="l">{participantLabel}</div>
          </div>
          <div className="stat-box">
            <div className="v">{done}/{total}</div>
            <div className="l">Matches done</div>
          </div>
          <div className="stat-box">
            <div className="v" style={{ color: runningCount > 0 ? "var(--red)" : "inherit" }}>
              {runningCount}
            </div>
            <div className="l">Now</div>
          </div>
          <div className="stat-box">
            <div className={vClass(estDuration)}>{estDuration}</div>
            <div className="l">Est. duration{estimateLoading ? " …" : ""}</div>
          </div>
        </div>
      );
    }

    // completed
    const totalDuration = formatCompMinutes(estimate && estimate.totalDurationMinutes) || "—";
    return (
      <div className="stats-strip">
        <div className="stat-box">
          <div className="v">{numPlayers}</div>
          <div className="l">{participantLabel}</div>
        </div>
        <div className="stat-box">
          <div className="v">{done}</div>
          <div className="l">Matches played</div>
        </div>
        <div className="stat-box">
          <button
            type="button"
            className="stat-box__link"
            onClick={() => onSection(resultsSection)}
          >
            <div className="v v--sm" style={{ color: "var(--accent)" }}>View podium →</div>
            <div className="l">Standings</div>
          </button>
        </div>
        <div className="stat-box">
          <div className={vClass(totalDuration)}>{totalDuration}</div>
          <div className="l">Total duration</div>
        </div>
      </div>
    );
  }

  // Primary content — branched by state.
  function renderPrimaryContent() {
    // --- setup ---
    if (isSetup) {
      const steps = competitionNextSteps(c);
      return (
        <div className="card" style={{ marginBottom: 16 }} data-testid="next-steps-card">
          <div className="card__head">
            <div className="card__title">Next steps</div>
          </div>
          <div className="comp-steps">
            {steps.map(step => (
              <div
                key={step.id}
                className={`comp-step comp-step--${step.state}`}
                data-testid={`step-${step.id}`}
              >
                <div className="comp-step__icon" aria-hidden="true">
                  {step.state === "done" ? "✓" : step.state === "active" ? "●" : "○"}
                </div>
                <div className="comp-step__body">
                  <div className="comp-step__label">{step.label}</div>
                  <div className="comp-step__detail">{step.detail}</div>
                  {step.state === "active" && step.section && step.cta && (
                    <button
                      type="button"
                      className="btn btn--primary comp-step__cta"
                      onClick={() => onSection(step.section)}
                      data-testid={`step-${step.id}-cta`}
                    >
                      {step.cta}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
          {numPlayers < 2 && (
            <div className="comp-steps__hint" data-testid="participants-hint">
              Need at least 2 {c.kind === "team" ? "teams" : "participants"} to generate a draw.
            </div>
          )}
        </div>
      );
    }

    // --- draw-ready ---
    if (isDrawReady) {
      // Swiss rounds are generated dynamically on start; there is no static
      // pools/bracket preview to show. Only non-swiss formats expose a preview
      // section in the nav at this stage.
      const isSwiss = c.format === "swiss";
      const previewSection = c.format === "playoffs" ? "bracket" : "pools";
      const previewLabel = c.format === "playoffs" ? "bracket" : "pools";
      return (
        <div className="card" style={{ marginBottom: 16 }} data-testid="draw-ready-card">
          <div className="card__head">
            <div className="card__title">Draw ready</div>
            <div className="card__sub">{numPlayers} {participantLabel} · {seededCount > 0 ? `${seededCount} seeded` : "no seeds"}</div>
          </div>
          <div style={{ fontSize: 13.5, color: "var(--ink-2)", marginBottom: 14 }}>
            {isSwiss
              ? <>Participants are set. Use the <strong>Start competition →</strong> button in the header to begin the first round.</>
              : <>The draw has been generated. Preview it below, then use the <strong>Start competition →</strong> button in the header to begin.</>}
          </div>
          <div className="row">
            {!isSwiss && (
              <button
                type="button"
                className="card card--sm card--action"
                style={{ textAlign: "left" }}
                onClick={() => onSection(previewSection)}
                data-testid="draw-ready-preview-btn"
              >
                <div className="card__title" style={{ marginBottom: 4 }}>
                  Preview {previewLabel} →
                </div>
                <div className="card__sub">Check placements before starting</div>
              </button>
            )}
            <div className="card card--sm" style={{ background: "var(--bg)" }}>
              <div className="card__title" style={{ marginBottom: 4, color: "var(--ink-2)" }}>To start</div>
              <div className="card__sub">
                Use the <strong>Start competition →</strong> button above. To change the draw, use <strong>Discard draw</strong> first.
              </div>
            </div>
          </div>
        </div>
      );
    }

    // --- running ---
    if (isRunning) {
      return (
        <div>
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__head">
              <div className="card__title">Progress</div>
              <div className="card__sub">{pct}%</div>
            </div>
            {/* Animate via transform: scaleX (compositor-only) rather than width,
                which forced a layout/paint each frame. The inner bar is full-width
                and scaled from the left edge; the rounded track clips it so the
                visual is equivalent. Transition is dropped under
                prefers-reduced-motion. */}
            <div
              style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }}
              role="progressbar"
              aria-valuenow={pct}
              aria-valuemin={0}
              aria-valuemax={100}
            >
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
            <button
              type="button"
              className="card card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection("scores")}
            >
              <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
              <div className="card__sub">Update or correct match results</div>
            </button>
            <button
              type="button"
              className="card card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection(resultsSection)}
            >
              <div className="card__title" style={{ marginBottom: 6 }}>Results →</div>
              <div className="card__sub">Visual bracket / pool standings</div>
            </button>
          </div>
        </div>
      );
    }

    // --- completed ---
    return (
      <div>
        <div className="card" style={{ marginBottom: 16 }} data-testid="completed-card">
          <div className="card__head">
            <div className="card__title">Competition complete</div>
          </div>
          <div style={{ fontSize: 13.5, color: "var(--ink-2)", marginBottom: 14 }}>
            All matches have been played. View final standings or export results.
          </div>
          <div className="row">
            <button
              type="button"
              className="card card--sm card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection(resultsSection)}
              data-testid="completed-results-btn"
            >
              <div className="card__title" style={{ marginBottom: 4 }}>Results →</div>
              <div className="card__sub">View final standings &amp; podium</div>
            </button>
            <button
              type="button"
              className="card card--sm card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection("export")}
              data-testid="completed-export-btn"
            >
              <div className="card__title" style={{ marginBottom: 4 }}>Export →</div>
              <div className="card__sub">Print brackets &amp; certificates</div>
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Footer: schedule estimate strip — shown whenever estimate resolves.
  function renderEstimateFooter() {
    if (!estimate && !estimateLoading && !estimateErr) return null;
    const total = formatCompMinutes(estimate && estimate.totalDurationMinutes);
    const perCourt = estimate ? (estimate.perCourtMinutes || []).map(m => formatCompMinutes(m) || "0m") : [];
    const ceremony = formatCompMinutes(estimate && estimate.ceremonyMinutes);
    return (
      <div style={{
        padding: "10px 12px",
        borderRadius: 6,
        background: "var(--accent-soft)",
        border: "1px solid var(--accent)",
        marginTop: 16,
      }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: "var(--ink-2)", marginBottom: 4 }}>
          Schedule estimate
          {estimateLoading && <span className="spinner" style={{ marginLeft: 6, verticalAlign: "middle" }} />}
        </div>
        {estimateErr && (
          <div style={{ fontSize: 12, color: "var(--red)" }}>{estimateErr}</div>
        )}
        {estimate && !estimateErr && (() => {
          if (!total) {
            return (
              <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
                No estimate yet — add participants and configure duration to see a projection.
              </div>
            );
          }
          return (
            <div style={{ fontSize: 12.5, color: "var(--ink)" }}>
              <div><strong>Total:</strong> {total}</div>
              {perCourt.length > 1 && (
                <div style={{ marginTop: 2 }}>
                  <strong>Per court:</strong>{" "}
                  {perCourt.map((t, i) => `Court ${(c.courts && c.courts[i]) || String.fromCharCode(65 + i)}: ${t}`).join(" · ")}
                </div>
              )}
              {perCourt.length === 1 && (
                <div style={{ marginTop: 2 }}>
                  <strong>Per court:</strong> {perCourt[0]}
                </div>
              )}
              {ceremony && (
                <div style={{ marginTop: 2 }}>
                  <strong>Ceremony blocks:</strong> {ceremony}
                </div>
              )}
            </div>
          );
        })()}
      </div>
    );
  }

  return (
    <div>
      {renderStatsStrip()}
      {renderPrimaryContent()}
      {renderEstimateFooter()}
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
    const admin = await window.promptAdminPassword();
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
          <button type="button"
            className="btn btn--sm btn--ghost"
            onClick={() => removeRow(idx)}
            aria-label="Remove award"
            data-testid={`fs-award-remove-${idx}`}
          >✕</button>
        </div>
      ))}
      <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
        <button type="button" className="btn btn--sm btn--ghost" onClick={addRow} data-testid="fs-award-add">+ Add award</button>
        <button type="button" className="btn btn--sm btn--primary" onClick={save} disabled={saving} data-testid="fs-award-save">
          {saving && <span className="spinner" />}
          {saving ? "Saving…" : "Save awards"}
        </button>
      </div>
      </div>
    </div>
  );
}

if (typeof window !== "undefined") {
  window.AdminCompOverview = AdminCompOverview;
  window.FightingSpiritAwardsEditor = FightingSpiritAwardsEditor;
  window.competitionNextSteps = competitionNextSteps;
  window.overviewViewMode = overviewViewMode;
  window.overviewResultsSection = overviewResultsSection;
}

export { competitionNextSteps, overviewViewMode, overviewResultsSection };
