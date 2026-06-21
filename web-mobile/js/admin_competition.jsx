// admin_competition.jsx — thin entry for the competition admin shell.
// The AdminCompetition shell lives here; its sections were split into sibling
// modules (mp-hpe3): admin_competition_overview/settings/bracket/swiss.jsx.
// This file evaluates every section module (via the imports/re-exports below,
// which set their window.* component globals) and re-exports the full pure-helper
// surface so existing `import { … } from './admin_competition.jsx'` test sites
// keep resolving. This entry is the SOLE loader of the section modules — they
// have no <script> tag of their own (avoids a duplicate module-eval; see
// index.html) and are pulled in only by the imports here.

// Evaluate section modules so their window.* globals are set before the alias
// block below reads them. Settings/bracket/swiss are pulled in via their
// helper re-exports; overview has no pure helper, so it needs a side-effect import.
import './admin_competition_overview.jsx';
export { formatCompMinutes } from './admin_competition_settings.jsx';
export { buildRunningIpponResult, loadScoreboardPoints } from './admin_competition_bracket.jsx';
export {
  swissRoundIDPrefix,
  filterSwissRoundMatches,
  isSwissRoundComplete,
  canGenerateNextSwissRound,
  isSwissCompetitionComplete,
} from './admin_competition_swiss.jsx';

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const isValidDate = window.isValidDate;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const AdminParticipants = window.AdminParticipants;
const AdminPools = window.AdminPools;
const AdminScoreEditor = window.AdminScoreEditor;
const AdminExport = window.AdminExport;
// Section components — produced by the sibling modules above, aliased here for
// the AdminCompetition render tree.
const AdminCompOverview = window.AdminCompOverview;
const FightingSpiritAwardsEditor = window.FightingSpiritAwardsEditor;
const AdminSettings = window.AdminSettings;
const AdminBracket = window.AdminBracket;
const AdminSwissRounds = window.AdminSwissRounds;

function AdminCompetition({ tournament, competition, pools, poolMatches, standings, bracket, section, onSection, onBack, onOpenCompetition, onUpdate, onRefreshCompetition, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password, showToast }) {
  const c = competition;
  const t = tournament;
  const [starting, setStarting] = useStateA(false);
  const [generating, setGenerating] = useStateA(false);
  const [discarding, setDiscarding] = useStateA(false);
  // localStatus lets AdminSettings report an invalidation immediately so the
  // page-header StatusBadge flips without waiting for the SSE refresh.
  // Cleared automatically when the prop status changes (SSE arrives).
  const [localStatus, setLocalStatus] = useStateA(null);
  useEffectA(() => { setLocalStatus(null); }, [c.id, c.status]);
  // start() awaits a multi-second backend call (pool generation + bracket
  // build). If the user clicks Back during that window AdminCompetition
  // unmounts and setStarting(false) in finally targets a dead component.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Use the shared isValidDate (admin_helpers.jsx) which delegates to
  // normalizeDate for semantic validity — rejects "32-13-2026" / Feb 31 /
  // Feb 29 in non-leap years. Without this, the Start button would enable
  // for shape-valid-but-impossible dates that AdminSettings.saveNow's
  // stricter check would reject — letting the operator start a competition
  // with a date that can't be saved back.
  const isDateValid = isValidDate;

  // mp-w7x: surface how many participants will be excluded from the draw
  // because they have not checked in. Mirror the engine's rule
  // (filterCheckedIn in internal/engine/competition.go): only applies when the
  // competition has check-in tracking enabled (c.checkInEnabled) — consistent
  // with the rest of the UI, which masks checkedIn behind that flag. Within an
  // enabled competition, opt-in semantics apply: only exclude when at least one
  // participant is checked in. Empty roster (e.g. a playoffs-from-source
  // competition resolved server-side) yields 0.
  const drawPlayers = c.players || [];
  const anyCheckedIn = drawPlayers.some(p => p.checkedIn);
  const excludedFromDraw = (c.checkInEnabled && anyCheckedIn) ? drawPlayers.filter(p => !p.checkedIn).length : 0;

  // Confirm before a draw/start that would drop non-checked-in participants.
  // Returns false when the operator cancels.
  const confirmCheckInExclusion = async () => {
    if (excludedFromDraw === 0) return true;
    const plural = excludedFromDraw === 1 ? "" : "s";
    const verb = excludedFromDraw === 1 ? "is" : "are";
    return await window.confirmDialog({
      message:
        `${excludedFromDraw} participant${plural} ${verb} not checked in and will be excluded from the draw.\n\n` +
        `Only checked-in participants will be drawn. Continue?`,
      danger: true,
    });
  };

  const generateDraw = async () => {
    if (!(await confirmCheckInExclusion())) return;
    setGenerating(true);
    try {
      await window.API.generateDraw(c.id, password);
      if (!mountedRef.current) return;
      // Refresh immediately so the UI reflects draw-ready status without
      // waiting for SSE (which may be slow or temporarily disconnected).
      onRefreshCompetition?.();
      showToast(`Draw generated for ${c.name}`);
      onSection(c.format === "playoffs" ? "bracket" : c.format === "swiss" ? "overview" : "pools");
    } catch (e) {
      console.error("Generate draw failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  const discardDraw = async () => {
    if (!(await window.confirmDialog({ message: `Discard the generated draw for "${c.name}"? The pools/bracket will be removed and you can regenerate.`, confirmLabel: "Discard draw", danger: true }))) return;
    const admin = await window.promptAdminPassword();
    if (admin === null) return;
    setDiscarding(true);
    try {
      await window.API.discardDraw(c.id, password, admin);
      if (!mountedRef.current) return;
      onRefreshCompetition?.();
      showToast(`Draw discarded for ${c.name}`);
      // After discard the status reverts to "setup", so draw-only/preview nav
      // items (pools, bracket, swiss) disappear. If the operator is on one of
      // those sections, fall back to participants — a sensible landing point
      // after discarding. Otherwise leave them exactly where they are.
      // (This fallback set mirrors the draw-only items gated on isDrawReady /
      // draw artifacts in the sections array above.)
      if (["pools", "bracket", "swiss"].includes(section)) {
        onSection("participants");
      }
    } catch (e) {
      console.error("Discard draw failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setDiscarding(false);
    }
  };

  const regenerateDraw = async () => {
    if (!(await confirmCheckInExclusion())) return;
    // Regenerate discards the existing draw first (DELETE /draw is gated),
    // so collect the elevated password up front before any work begins.
    const admin = await window.promptAdminPassword();
    if (admin === null) return;
    setGenerating(true);
    try {
      await window.API.discardDraw(c.id, password, admin);
      if (!mountedRef.current) return;
      // Refresh after discard so the UI reflects setup status immediately;
      // if generateDraw then fails the UI is consistent with the server.
      onRefreshCompetition?.();
      await window.API.generateDraw(c.id, password);
      if (!mountedRef.current) return;
      onRefreshCompetition?.();
      showToast(`Draw regenerated for ${c.name}`);
      onSection(c.format === "playoffs" ? "bracket" : c.format === "swiss" ? "overview" : "pools");
    } catch (e) {
      console.error("Regenerate draw failed:", e);
      if (mountedRef.current) {
        onRefreshCompetition?.();
        showToast(e.message, "error");
      }
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  const start = async () => {
    // draw-ready → running does not regenerate the draw; skip the confirmation
    // there to avoid showing stale exclusion counts against an already-fixed draw.
    if (c.status !== "draw-ready" && !(await confirmCheckInExclusion())) return;
    showToast(`Starting ${c.name}…`);

    setStarting(true);
    try {
      await window.API.startCompetition(c.id, password);
      // Don't attempt a local-state refresh here. Pre-fix this called
      // onUpdate({ ...t, competitions: comps }), but onUpdate at this
      // level is wired (in AdminApp's render — see the AdminCompetition
      // <Component onUpdate={...}> binding) to
      // (next) => updateCompetition(c.id, next), which fires
      // PUT /api/competitions/:id with `next` as the body.
      // Passing a tournament-shaped object would have Gin binding it as
      // state.Competition and silently overwriting the just-started
      // competition's Name/Date/etc. with tournament-level values.
      //
      // The backend broadcasts `competition_started` (and optionally
      // `competition_completed` when the zero-match auto-complete
      // path fires); both are in AdminApp's REFRESHABLE_EVENTS set, so
      // the SSE handler refetches the tournament + competitions list
      // and local state catches up within a roundtrip. Trade a tiny
      // perceived latency for not corrupting the record.
      if (!mountedRef.current) return;
      showToast(`${c.name} started`);
      onSection("scores");
    } catch (e) {
      console.error("Start competition failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setStarting(false);
    }
  };

  const isDrawReady = c.status === "draw-ready";
  const sections = [
    {
      sec: "Preparation", items: [
        { id: "overview", label: "Overview" },
        { id: "participants", label: "Participants & seeds" },
        // T136 nav: Lineups is a team-only surface — hide it for
        // individual competitions so the sidebar stays uncluttered.
        c.kind === "team" ? { id: "lineups", label: "Lineups" } : null,
        { id: "settings", label: "Settings" },
        { id: "awards", label: "Fighting Spirit" },
      ].filter(Boolean)
    },
    {
      sec: "Run", items: [
        // Show pools/bracket in nav when draw is ready (preview) or running.
        // Use .length checks: the state store returns [] / {rounds:[]} (never null)
        // when files are absent, so plain truthiness would always show the items.
        (pools?.length || (isDrawReady && c.format !== "playoffs" && c.format !== "swiss")) ? { id: "pools", label: (c.format === "league" ? "League" : "Pools") + (isDrawReady ? " (preview)" : "") } : null,
        // T191 (FR-050d): Swiss competitions surface a dedicated round
        // management panel for the "Generate next round" workflow.
        c.format === "swiss" && !isDrawReady ? { id: "swiss", label: "Swiss rounds" } : null,
        (bracket?.rounds?.length || (isDrawReady && c.format === "playoffs")) ? { id: "bracket", label: isDrawReady ? "Bracket (preview)" : "Bracket" } : null,
        !isDrawReady ? { id: "scores", label: "Scores" } : null,
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
              <StatusBadge status={localStatus ?? c.status} format={c.format} />
            </div>
            <div className="page-head__sub">
              {window.competitionKindLabel(c)} · {c.players.length} {c.kind === "team" ? "teams" : "players"} ·
              {c.date && ` ${formatDate(c.date)} at `} {c.startTime} · {c.courts.join(", ")}
            </div>
          </div>
          <div className="page-head__actions" style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
            {(!c.status || c.status === "setup") && c.players.length >= 2 && (
              <>
                <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                  <button type="button" className="btn btn--primary" onClick={generateDraw} disabled={!isDateValid(c.date) || generating || starting}>
                    {generating && <span className="spinner" />}
                    {generating ? "Generating…" : "Generate draw"}
                  </button>
                  <button type="button" className="btn btn--ghost" onClick={start} disabled={!isDateValid(c.date) || starting || generating}>
                    {starting && <span className="spinner" />}
                    {starting ? "Starting…" : "Start competition →"}
                  </button>
                </div>
                {!isDateValid(c.date) && (
                  <div style={{ color: "var(--red)", fontSize: 11, fontWeight: 600 }}>
                    ⚠ Cannot start: invalid date in Settings tab (e.g. "{c.date}")
                  </div>
                )}
                {excludedFromDraw > 0 && (
                  <div style={{ color: "var(--ink-3)", fontSize: 11, fontWeight: 600 }}>
                    {excludedFromDraw} not checked in — will be excluded from the draw
                  </div>
                )}
              </>
            )}
            {isDrawReady && (
              <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
                <div style={{ display: "flex", gap: 8 }}>
                  <button type="button" className="btn btn--ghost btn--danger" onClick={discardDraw} disabled={discarding || starting || generating}>
                    {discarding && <span className="spinner" />}
                    {discarding ? "Discarding…" : "Discard draw"}
                  </button>
                  <button type="button" className="btn btn--ghost" onClick={regenerateDraw} disabled={generating || starting || discarding}>
                    {generating && <span className="spinner" />}
                    {generating ? "Regenerating…" : "Regenerate draw"}
                  </button>
                  <button type="button" className="btn btn--primary" onClick={start} disabled={starting || generating || discarding}>
                    {starting && <span className="spinner" />}
                    {starting ? "Starting…" : "Start competition →"}
                  </button>
                </div>
                <div style={{ fontSize: 11, color: "var(--ink-3)" }}>Draw generated — preview below, then start when ready</div>
              </div>
            )}
            {c.format === "league" && c.status !== "setup" && c.status !== "draw-ready" && (
              <div style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>League: standings determine the winner</div>
            )}
          </div>
        </div>

        <div className="workspace">
          <div className="side-nav">
            {sections.map((sec) => (
              <div key={sec.sec}>
                <div className="side-nav__sec">{sec.sec}</div>
                {sec.items.map((it) => (
                  <button type="button" key={it.id} className={section === it.id ? "is-active" : ""} onClick={() => onSection(it.id)}>{it.label}</button>
                ))}
              </div>
            ))}
            {t.competitions.filter((cc) => cc.id !== c.id).length > 0 && (
              <div style={{ marginTop: 16, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
                <div className="side-nav__sec" style={{ opacity: 0.6 }}>Other competitions</div>
                {t.competitions.filter((cc) => cc.id !== c.id).map((cc) => (
                  <button type="button" key={cc.id} style={{ opacity: 0.75 }} onClick={() => onOpenCompetition(cc.id)}>{cc.name}</button>
                ))}
              </div>
            )}
          </div>
          <div>
            {section === "overview" && <AdminCompOverview c={c} pools={pools} poolMatches={poolMatches} bracket={bracket} onSection={onSection} />}
            {section === "participants" && <AdminParticipants c={c} tournament={t} onUpdate={onUpdate} password={password} showToast={showToast} onSection={onSection} />}
            {section === "lineups" && window.AdminTeamLineupsList && <window.AdminTeamLineupsList comp={c} password={password} showToast={showToast} />}
            {section === "settings" && <AdminSettings c={c} tournament={t} onUpdate={onUpdate} onBack={onBack} password={password} showToast={showToast} onStatusChange={setLocalStatus} />}
            {section === "awards" && <FightingSpiritAwardsEditor c={c} password={password} showToast={showToast} />}
            {/* T191/T193 (FR-050d/e): Swiss-round management. */}
            {/* onViewStandings is wired to the public viewer URL so the */}
            {/* operator can navigate into the viewer-mode standings */}
            {/* page when the competition completes. */}
            {section === "swiss" && c.format === "swiss" && !isDrawReady && (
              <AdminSwissRounds
                c={c}
                poolMatches={poolMatches}
                password={password}
                showToast={showToast}
                onViewStandings={onViewerMode || null}
              />
            )}
            {section === "pools" && <AdminPools c={c} pools={pools} poolMatches={poolMatches} standings={standings} tweaks={tweaks} onEditScore={onEditScore} password={password} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onMoveCourt={onMoveCourt} tweaks={tweaks} password={password} showToast={showToast} />}
            {section === "scores" && !isDrawReady && <AdminScoreEditor c={c} t={t} onEditScore={onEditScore} onMoveCourt={onMoveCourt} restrictToCompId={c.id} password={password} />}
            {section === "export" && <AdminExport c={c} t={t} />}
          </div>
        </div>
      </div>
    </div>
  );
}

window.AdminCompetition = AdminCompetition;
