// Admin shell: chrome (Breadcrumbs, AdminTopbar, CourtPicker) plus the
// dashboard page (AdminDashboard, CompCard). All cross-page shared admin UI
// lives here so it loads before any section-specific file. See
// web-mobile/admin_split_plan.md.

import { renderQR } from './qr.js';

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

// Producers (loaded earlier).
const sideName = window.sideName;
const compMatchStats = window.compMatchStats;
const pluralize = window.pluralize;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const Icon = window.Icon;
const formatLabelShort = window.formatLabelShort;
const formatAdminHeaderSub = window.formatAdminHeaderSub;

// Maximum running-match chips rendered in the topbar status strip before the
// "+N more" overflow indicator kicks in.
const RUNNING_STRIP_MAX_CHIPS = 6;

// Plain-language expansions for the insider format codes emitted by
// formatLabelShort (ui.jsx): "P+KO" / "KO". Used as a tooltip + aria-label at
// the call site so the abbreviation stays legible to new operators.
const FORMAT_LABEL_LONG = {
  mixed: "Pools then knockout",
  league: "League (round-robin)",
  swiss: "Swiss rounds",
  playoffs: "Knockout (direct elimination)",
};

function Breadcrumbs({ items }) {
  return (
    <div className="crumbs">
      {items.map((item, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="sep">/</span>}
          {item.onClick ? (
            // No document.activeElement.blur() here: it dumped keyboard focus
            // to <body> after every breadcrumb navigation, stranding tab order.
            // The clicked button keeps focus naturally; the destination view
            // manages its own focus. (The old blur appeared to target a mobile
            // keyboard / hover dismissal, but a blanket blur is the wrong tool —
            // it breaks keyboard users for a problem the browser already handles
            // when the navigated-away input unmounts.)
            <button type="button" onClick={() => item.onClick()}>
              {i === 0 && <span style={{ marginRight: 4 }}>←</span>}
              {item.label}
            </button>
          ) : (
            <span>{item.label}</span>
          )}
        </React.Fragment>
      ))}
    </div>
  );
}

// How long the SSE feed must stay down before the topbar escalates from the
// calm "Reconnecting…" pill to the full-width alert banner. Set above the
// stream's 5s retry interval so a single failed-then-recovered cycle never
// trips it — only an outage that survives the first retry escalates.
const SUSTAINED_DOWN_MS = 8000;

// Framework-free tracker for the escalation timer. `note(connected)` is called
// on every connection-status change; `onSustained(true|false)` fires when the
// feed has been continuously down for `thresholdMs`, and `false` the moment it
// recovers. The timer starts on the first disconnect and is NOT restarted by
// later error events, so the threshold measures total downtime rather than
// time-since-last-event. Extracted from AdminTopbar so the timing logic is
// unit-testable with fake timers without rendering the component.
function watchSustainedDisconnect(thresholdMs, onSustained) {
  let timer = null;
  return {
    note(connected) {
      if (connected) {
        if (timer) { clearTimeout(timer); timer = null; }
        onSustained(false);
      } else if (!timer) {
        timer = setTimeout(() => { timer = null; onSustained(true); }, thresholdMs);
      }
    },
    stop() {
      if (timer) { clearTimeout(timer); timer = null; }
    },
  };
}

function AdminTopbar({ onLogout, onViewerMode, tournament, hideRunningStrip }) {
  // Render running matches as chips below the topbar so admins always
  // know what's under way, regardless of which screen they're on. Clicking
  // a chip jumps to that competition's score editor via the global
  // navigator helper set up by AdminApp. sideName (hoisted to module
  // scope) handles the three possible side shapes; chips with no real
  // name on either side are filtered out.
  const runningMatches = useMemoA(() => {
    if (!tournament || !window.compMatches) return [];
    return (tournament.competitions || [])
      .flatMap(cc => window.compMatches(cc))
      .filter(m => m.status === "running" && sideName(m.sideA) && sideName(m.sideB));
  }, [tournament]);

  // Connection-state indicator (PRODUCT.md: "never leave the user guessing
  // about the tournament state"). The admin chrome previously showed nothing
  // when the SSE stream dropped, so stale data sat silently. We open our own
  // lightweight subscription purely to read the connection status exposed by
  // API.subscribeToEvents' second (onStatus) callback — 'open' vs 'error'.
  // The callback arg is a no-op here: the dashboard already drives data
  // refreshes off its own subscription; this one only observes the connection state.
  // Two-tier signal. A brief drop is normal — the SSE stream auto-retries
  // every 5s — so a transient blip only flips the calm topbar pill to
  // "Reconnecting…". A *sustained* outage (still down after the first retry
  // should have landed) escalates to an unmissable banner, because by then
  // the operator is scoring against data that may be stale. The threshold
  // filters reconnect noise so the banner never cries wolf. The timer
  // orchestration lives in watchSustainedDisconnect (below) so it can be
  // unit-tested with fake timers, independent of the component.
  const [connected, setConnected] = useStateA(true);
  const [sustainedDown, setSustainedDown] = useStateA(false);
  useEffectA(() => {
    if (!window.API || typeof window.API.subscribeToEvents !== "function") return;
    const monitor = watchSustainedDisconnect(SUSTAINED_DOWN_MS, setSustainedDown);
    const unsub = window.API.subscribeToEvents(
      () => {},
      (status) => {
        const up = status === "open";
        setConnected(up);
        monitor.note(up);
      }
    );
    return () => { monitor.stop(); unsub(); };
  }, []);

  const onOpenScore = (m) => {
    if (typeof window.__adminNavigateToScore === "function") {
      window.__adminNavigateToScore(m.compId);
    }
  };

  return (
    // Wrap topbar + running-strip in a single sticky container so they scroll
    // together. This lets the topbar size naturally (min-height instead of a
    // fixed height) — robust to font scaling / browser zoom — while still
    // keeping the running-strip visually anchored beneath it.
    <div className="topbar-stack">
      <div className="topbar">
        <div className="topbar__brand">
          <img src="/api/branding/logo" onError={(e) => { e.target.onerror = null; e.target.src = "/logo.jpeg"; }} alt="Tournament logo" className="topbar__logo" decoding="async" />
          <div>
            <div className="topbar__title">{tournament?.name || "Bracket Creator"}</div>
            <div className="topbar__sub">Admin console</div>
          </div>
        </div>
        <div className="topbar__spacer"></div>
        {/* Connection-status indicator for the SSE stream. Labelled "Connected"
            so it reads as the data-connection state, not on-court match
            activity, and styled as a calm STATIC dot, deliberately not the
            pulsing `.dot--running` signal that flags a match in progress (the
            strip below). Only the disconnected state pulses, so motion flags
            the moment that actually needs attention. role=status + aria-live
            announce the change to assistive tech without alarming. */}
        <span
          className={`topbar__conn${connected ? "" : " topbar__conn--down"}`}
          role="status"
          aria-live="polite"
          title={connected ? "Receiving real-time updates" : "Connection lost, reconnecting"}
        >
          <span aria-hidden="true" className="topbar__conn__dot"></span>
          {connected ? "Connected" : "Reconnecting…"}
        </span>
        <button type="button" className="viewer-toggle" onClick={onViewerMode}><Icon name="eye" /> Public viewer</button>
        <button type="button" className="btn btn--ghost btn--sm" onClick={onLogout}>Sign out</button>
      </div>
      {!connected && sustainedDown && (
        <div className="conn-alert" role="alert">
          <span className="conn-alert__dot" aria-hidden="true"></span>
          Connection interrupted. Reconnecting… Scores on screen may be out of date.
        </div>
      )}
      {!hideRunningStrip && runningMatches.length > 0 && (
        <div className="running-strip" role="region" aria-label={`${pluralize(runningMatches.length, "match", "matches")} in progress`}>
          <span className="running-strip__lbl"><span className="dot dot--running"></span> {pluralize(runningMatches.length, "match", "matches")} in progress</span>
          <div className="running-strip__chips">
            {runningMatches.slice(0, RUNNING_STRIP_MAX_CHIPS).map(m => {
              const a = sideName(m.sideA);
              const b = sideName(m.sideB);
              const courtLabel = m.court ? `Shiaijo ${m.court}` : "Unassigned";
              const courtPhrase = m.court ? `on Shiaijo ${m.court}` : "with no court assigned";
              return (
                <button type="button"
                  key={`${m.compId}:${m.id}`}
                  className="running-strip__chip"
                  onClick={() => onOpenScore && onOpenScore(m)}
                  aria-label={`Open score editor for ${b} versus ${a} ${courtPhrase}`}
                >
                  <span className="running-strip__court">{courtLabel}</span>
                  <span className="running-strip__names">{b} – {a}</span>
                </button>
              );
            })}
            {runningMatches.length > RUNNING_STRIP_MAX_CHIPS && <span className="running-strip__more">+{runningMatches.length - RUNNING_STRIP_MAX_CHIPS} more</span>}
          </div>
        </div>
      )}
    </div>
  );
}

function initialSectionFor(status) {
  if (status === "setup") return "participants";
  if (status === "pools" || status === "playoffs") return "scores";
  return "overview";
}

// PLACE_STYLE: icons and labels for podium places 1/2/3.
// Replicated locally from viewer.jsx (PLACE_STYLE is not exported to window).
const PLACE_STYLE_ADMIN = {
  1: { icon: "🏆", label: "1st" },
  2: { icon: "🥈", label: "2nd" },
  3: { icon: "🥉", label: "3rd" },
};

// buildAllWinners resolves podium for each completed comp using the shared
// resolveCompetitionAwards helper.
// Signature: buildAllWinners(completedComps, fetchers)
// Returns a Promise resolving to an array of { comp, state, podium } objects
// (plus an `error` string field when state === "error").
// state is one of "final" | "in-progress" | "error".
async function buildAllWinners(completedComps, fetchers) {
  return Promise.all(
    completedComps.map(async (comp) => {
      try {
        const { state, podium } = await window.resolveCompetitionAwards(comp, fetchers);
        return { comp, state, podium };
      } catch (err) {
        return { comp, state: "error", podium: [], error: err?.message || String(err) };
      }
    })
  );
}

// AllWinnersModal renders a summary of every completed competition with its
// podium placings, computed via window.resolveCompetitionAwards.
function AllWinnersModal({ comps, onClose }) {
  const completed = (comps || []).filter((c) => c.status === "completed");
  const [state, setState] = useStateA({ loading: true, results: [], error: null });

  window.useEscapeToClose(onClose);

  // Stable signature of every comp's id:status. A pools+knockout podium can
  // change without the *completed* set changing — e.g. a mixed comp is already
  // completed (pools done) while its linked playoffs comp finishes its final.
  // Keying the effect on all comps' statuses makes the open modal refetch when
  // any competition completes or its knockout resolves, rather than showing
  // stale results.
  const compsSig = (comps || []).map((c) => `${c.id}:${c.status}`).join("|");

  useEffectA(() => {
    let cancelled = false;
    setState((s) => ({ ...s, loading: true }));
    buildAllWinners(completed, {
      fetchCompetitionDetails: window.API.fetchCompetitionDetails.bind(window.API),
      swissStandings: window.API.swissStandings ? window.API.swissStandings.bind(window.API) : null,
    }).then((results) => {
      if (!cancelled) setState({ loading: false, results, error: null });
    }).catch((err) => {
      if (!cancelled) setState({ loading: false, results: [], error: err?.message || String(err) });
    });
    return () => { cancelled = true; };
  }, [compsSig]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 640, width: "95%" }} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="All winners">
        <div className="modal__head">
          <div className="modal__title" style={{ display: "inline-flex", alignItems: "center", gap: 8 }}><Icon name="trophy" size={18} />All winners</div>
          <button type="button" className="modal__close" onClick={onClose} aria-label="Close">✕</button>
        </div>
        <div className="modal__body" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {state.loading && (
            <div style={{ textAlign: "center", color: "var(--ink-3)", padding: "24px 0" }}>Loading results…</div>
          )}
          {!state.loading && state.error && (
            <div style={{ color: "var(--red)", padding: "8px 0" }}>Failed to load results: {state.error}</div>
          )}
          {!state.loading && !state.error && state.results.length === 0 && (
            <div style={{ color: "var(--ink-3)", padding: "8px 0" }}>No completed competitions.</div>
          )}
          {!state.loading && state.results.map(({ comp, state: compState, podium, error: compErr }) => (
            <div key={comp.id} className="card" style={{ padding: "12px 16px" }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
                <div style={{ fontWeight: 700, fontSize: 15 }}>{comp.name}</div>
                <div style={{ fontSize: 12, color: "var(--ink-3)", background: "var(--surface-2)", borderRadius: 4, padding: "1px 6px" }}>
                  {window.competitionKindLabel(comp)}
                </div>
              </div>
              {compErr && (
                <div style={{ fontSize: 13, color: "var(--red)" }}>Could not load results: {compErr}</div>
              )}
              {!compErr && compState === "in-progress" && podium.length === 0 && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }}>Knockout in progress</div>
              )}
              {!compErr && compState !== "in-progress" && podium.length === 0 && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }}>No results yet</div>
              )}
              {!compErr && podium.length > 0 && (
                <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                  {podium.map((entry, idx) => {
                    const style = PLACE_STYLE_ADMIN[entry.place] || PLACE_STYLE_ADMIN[3];
                    return (
                      <div key={idx} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14 }}>
                        <span style={{ fontSize: 16, minWidth: 22 }}>{style.icon}</span>
                        <span style={{ fontWeight: 600, minWidth: 32, color: "var(--ink-3)", fontSize: 12 }}>{style.label}</span>
                        <span style={{ fontWeight: 600 }}>{entry.name}</span>
                        {entry.dojo && <span style={{ color: "var(--ink-3)", fontSize: 13 }}>{entry.dojo}</span>}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          ))}
        </div>
        <div className="modal__foot">
          <button type="button" className="btn" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  );
}

function AdminDashboard({ tournament, password, onOpenCompetition, onCreateCompetition, onEditTournament, onAnnounce, onOpenSchedule, onOpenScoreEditor, onOpenImport, onOpenShiaijo, onStartAll, onStartCompetition, onLogout, onViewerMode, onUpdate, showToast, authConfig }) {
  const t = tournament;
  const comps = t.competitions || [];
  const [exportPdfOpen, setExportPdfOpen] = useStateA(false);
  const [allWinnersOpen, setAllWinnersOpen] = useStateA(false);
  const scheduleEnabled = !!(authConfig?.scheduleEnabled);

  useEffectA(() => {
    // Coalesce bursts of events into a single dashboard refresh. On a busy
    // event the SSE stream can fire dozens of match_updated per minute, and
    // each one previously triggered a full tournament+competitions fetch.
    let pending = null;
    let fetching = false;
    // Track events that arrive during a pending timer or in-flight fetch
    // so they don't silently get dropped — kick off one more refresh in
    // the finally block when set.
    let needsRefresh = false;
    const scheduleRefresh = () => {
      if (pending || fetching) { needsRefresh = true; return; }
      const jitter = 500 + Math.random() * 1000; // 500-1500ms window
      pending = setTimeout(() => {
        pending = null;
        // Keep the gate closed for the duration of the fetch so a second
        // burst can't fire a concurrent request that might resolve
        // out-of-order and clobber fresher data.
        fetching = true;
        Promise.all([
          window.API.fetchTournament(),
          window.API.fetchCompetitions()
        ]).then(([tourney, competitions]) => {
          onUpdate({ ...tourney, competitions });
        }).catch(err => console.error("Dashboard refresh failed", err))
          .finally(() => {
            fetching = false;
            if (needsRefresh) {
              needsRefresh = false;
              scheduleRefresh();
            }
          });
      }, jitter);
    };
    const unsub = window.API.subscribeToEvents((event) => {
      if (event.type === "tournament_updated" || event.type === "competition_started" || event.type === "competition_completed" || event.type === "competition_deleted" || event.type === "match_updated" || event.type === "schedule_updated") {
        scheduleRefresh();
      }
    });
    return () => {
      if (pending) clearTimeout(pending);
      unsub();
    };
  }, []);

  const { totalMatches, doneMatches, runningMatches, totalParticipants } = useMemoA(() => {
    let totalMatches = 0, doneMatches = 0, runningMatches = 0, totalParticipants = 0;
    comps.forEach((c) => {
      totalParticipants += (c.players || []).length;
      const s = compMatchStats(c);
      totalMatches += s.total; doneMatches += s.done; runningMatches += s.running;
    });
    return { totalMatches, doneMatches, runningMatches, totalParticipants };
  }, [comps]);

  const running = comps.filter((c) => c.status === "pools" || c.status === "playoffs");
  // No competitions yet: the "Add competition" tile is the operator's primary
  // next step, so it's promoted from a quiet dashed placeholder to a loud CTA.
  const noComps = comps.length === 0;

  // Derive the tournament-level status from competition activity instead of
  // trusting t.status, which can read "Pending" (setup) even while competitions
  // are mid-pools — a contradiction next to the "Currently running" list below.
  const allCompleted = comps.length > 0 && comps.every((c) => c.status === "completed");
  const tournamentInProgress = running.length > 0;

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1280 }}>
        <div className="page-head">
          <div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{t.name}</h1>
              {tournamentInProgress
                ? <span className="badge badge--pools">In progress</span>
                : <StatusBadge status={allCompleted ? "completed" : "setup"} />}
            </div>
            <div className="page-head__sub">
              {formatAdminHeaderSub(
                formatDate(t.date),
                t.venue,
                window.courtCount(t.courts),
                comps.length,
                totalParticipants
              )}
            </div>
          </div>
          <div className="page-head__actions">
            <button type="button" className="btn" onClick={onAnnounce} disabled={noComps} title={noComps ? "Add a competition first" : undefined}><Icon name="megaphone" />Announce</button>
            <button type="button" className="btn" onClick={() => setExportPdfOpen(true)} disabled={noComps} title={noComps ? "Add a competition first" : undefined}><Icon name="printer" />Export PDFs</button>
            {comps.some(c => c.status === "completed") && (
              <button type="button" className="btn" onClick={() => setAllWinnersOpen(true)}><Icon name="trophy" />All winners</button>
            )}
            <button type="button" className="btn" onClick={onEditTournament}>Edit details</button>
            {comps.some(c => c.status === "setup" && (c.players || []).length >= 2) && (
              <button type="button" className="btn btn--danger" onClick={onStartAll}>Start all</button>
            )}
            {/* When the tournament is empty the first-run empty state below carries
                the single primary "Add competition" CTA, so we drop the duplicate
                header button to keep one clear call to action on the first screen. */}
            {!noComps && (
              <button type="button" className="btn btn--primary" onClick={onCreateCompetition}>+ Add competition</button>
            )}
          </div>
        </div>

        {/* First-run: an empty tournament has no matches, so the match-count
            stat strip ("Matches done 0/0", "Now 0"), the tournament-wide score
            editor, and the per-shiaijo operator views are all dead widgets. Show
            a single oriented empty state instead and skip the rest of the
            dashboard until the first competition exists. */}
        {noComps ? (
          <div className="empty" style={{ padding: "48px 24px", marginBottom: 24 }}>
            <div className="icon" aria-hidden="true">🥋</div>
            <h3>Add your first competition</h3>
            <div style={{ fontSize: 13, color: "var(--ink-2)", maxWidth: 460, margin: "0 auto", lineHeight: 1.5 }}>
              A competition is one event within {t.name} — like Men's Individual or Women's Teams. Add one to enter participants, build the draw, and run matches.
            </div>
            <div className="empty__cta" style={{ display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
              <button type="button" className="btn btn--primary" onClick={onCreateCompetition}>+ Add competition</button>
              <button type="button" className="btn" onClick={onOpenImport}>Import from folder</button>
            </div>
          </div>
        ) : (<>
        <div className="stats-strip">
          <div className="stat-box"><div className="v">{comps.length}</div><div className="l">Competitions</div></div>
          <div className="stat-box"><div className="v">{totalParticipants}</div><div className="l">Participants</div></div>
          <div className="stat-box"><div className="v">{doneMatches}/{totalMatches}</div><div className="l">Matches done</div></div>
          <div className="stat-box"><div className="v" style={{ color: runningMatches > 0 ? "var(--red)" : "inherit" }}>{runningMatches}</div><div className="l">Now</div></div>
        </div>

        <div className="row" style={{ marginBottom: 24 }}>
          {scheduleEnabled && (
            <button type="button" className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={onOpenSchedule}>
              <div className="card__title" style={{ marginBottom: 6, display: "inline-flex", alignItems: "center", gap: 8 }}><Icon name="calendar" size={18} />Tournament schedule →</div>
              <div className="card__sub">All matches across courts. Move matches between shiaijo, filter by player.</div>
            </button>
          )}
          <button type="button" className="card" style={{ textAlign: "left", cursor: noComps ? "not-allowed" : "pointer", opacity: noComps ? 0.6 : 1, border: "1px solid var(--line)" }} onClick={onOpenScoreEditor} disabled={noComps} title={noComps ? "Add a competition first" : undefined}>
            <div className="card__title" style={{ marginBottom: 6, display: "inline-flex", alignItems: "center", gap: 8 }}><Icon name="pencil" size={18} />Score editor →</div>
            <div className="card__sub">Update results or correct past matches across the tournament.</div>
          </button>
        </div>
        {onOpenShiaijo && (t.courts || []).length > 0 && (
          <div style={{ marginBottom: 24 }}>
            <div className="section-title">Shiaijo operator views</div>
            <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
              {(t.courts || []).map(court => (
                <button type="button" key={court} className="btn" style={{ minWidth: 120 }} onClick={() => onOpenShiaijo(court)}>
                  Shiaijo {court} →
                </button>
              ))}
            </div>
          </div>
        )}

        {running.length > 0 && (<>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            {/* Static dot, not dot--running: these are started competitions, which
                is not the same as a match in progress right now. The pulsing
                signal is reserved for matches actually under way (topbar strip
                + match rows) per DESIGN.md Principle 3. */}
            <span className="dot"></span> Currently running
          </div>
          <div className="tlist" style={{ marginBottom: 24 }}>
            {running.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, "scores")} onStart={() => onStartCompetition(c.id)} tournament={t} showToast={showToast} />)}
          </div>
        </>)}

        <div className="section-title">All competitions</div>
        <div className="tlist">
          {comps.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, initialSectionFor(c.status))} onStart={() => onStartCompetition(c.id)} tournament={t} showToast={showToast} />)}
          {/* This section only renders when competitions exist (the empty-tournament
              case is handled by the first-run empty state above), so the dashed
              tile stays a quiet "add another", not the promoted first-run CTA. */}
          <button type="button" className="tcard tcard--add" onClick={onCreateCompetition}>
            <div style={{ fontSize: 28, color: "var(--ink-3)" }}>+</div>
            <div style={{ fontWeight: 600, marginTop: 4 }}>Add competition</div>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>Individual or Team</div>
          </button>
          <button type="button" className="tcard tcard--add" onClick={onOpenImport}>
            <div style={{ color: "var(--ink-3)" }}><Icon name="folder" size={28} /></div>
            <div style={{ fontWeight: 600, marginTop: 4 }}>Import competitions</div>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>From folder with manifest.yaml</div>
          </button>
        </div>
        </>)}
        {window.VersionFooter && <window.VersionFooter />}
      </div>
      {exportPdfOpen && (
        <ExportPdfModal
          tournament={t}
          password={password}
          showToast={showToast}
          onClose={() => setExportPdfOpen(false)}
        />
      )}
      {allWinnersOpen && (
        <AllWinnersModal
          comps={comps}
          onClose={() => setAllWinnersOpen(false)}
        />
      )}
    </div>
  );
}

async function copyToClipboard(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text);
  }
  // Fallback for non-secure context (LAN without TLS)
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try {
    const ok = document.execCommand('copy');
    if (!ok) throw new Error('execCommand copy failed');
  } finally {
    document.body.removeChild(ta);
  }
}

function ShareRegistrationModal({ url, onClose, showToast }) {
  const canvasRef = useRefA(null);

  useEffectA(() => {
    if (canvasRef.current) {
      try {
        renderQR(canvasRef.current, url, { moduleSize: 6, quietZone: 4 });
      } catch (e) {
        console.error("QR render failed", e);
      }
    }
  }, [url]);

  window.useEscapeToClose(onClose);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Share registration link">
        <div className="modal__head">
          <div className="modal__title">Share registration link</div>
          <button type="button" className="modal__close" onClick={onClose} aria-label="Close">✕</button>
        </div>
        <div className="modal__body" style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 16 }}>
          <canvas ref={canvasRef} style={{ display: "block", imageRendering: "pixelated" }} />
          <div style={{ width: "100%", background: "var(--surface-2)", borderRadius: 6, padding: "8px 12px", fontFamily: "monospace", fontSize: 13, wordBreak: "break-all", userSelect: "all" }}>
            {url}
          </div>
          <p style={{ margin: 0, fontSize: 13, color: "var(--ink-3)", textAlign: "center" }}>
            Participants can scan this QR code or open the link to register.
          </p>
        </div>
        <div className="modal__foot">
          <button type="button" className="btn btn--primary" onClick={() => {
            copyToClipboard(url).then(() => showToast && showToast("Registration link copied!")).catch(() => showToast && showToast("Copy failed — select the link above manually", "error"));
          }}>Copy link</button>
          <button type="button" className="btn" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  );
}

// PDF_EXPORT_TYPES mirrors the server's POST /api/print/:type selectors.
// "all" is offered as a single combined download.
const PDF_EXPORT_TYPES = [
  { type: "all", label: "All (everything)", hint: "Registration, names, tags, pools & trees, full bracket" },
  { type: "registration", label: "Registration", hint: "The data sheet from every competition" },
  { type: "names", label: "Names to Print", hint: "A3 landscape, one title page per competition" },
  { type: "tags", label: "Tags", hint: "Competitor tags (team competitions excluded)" },
  { type: "pools-trees", label: "Pools & Trees", hint: "Pool draw + bracket trees, page-numbered" },
  { type: "full-bracket", label: "Full bracket", hint: "Pools, matches and trees, page-numbered" },
];

// ExportPdfModal lets an admin generate the print PDFs server-side (via
// LibreOffice) and download them as a ZIP. Each row triggers one
// POST /api/print/:type. Generation is slow (30–60s) so a per-row busy state
// is shown; a 503 (LibreOffice absent) or 422 (no pages) surfaces as a toast.
function ExportPdfModal({ tournament, password, onClose, showToast }) {
  const [busyType, setBusyType] = useStateA("");

  // Gate Escape-to-close while generating, the same as the backdrop/close
  // button — otherwise Escape could unmount mid-download and the in-flight
  // setBusyType("") would fire on an unmounted component.
  window.useEscapeToClose(busyType ? undefined : onClose);

  const download = async (type) => {
    if (busyType) return; // soffice is serialized server-side — one at a time
    setBusyType(type);
    try {
      const blob = await window.API.exportPDFs(type, password);
      const dlUrl = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = dlUrl;
      const stem = (tournament && tournament.name ? tournament.name : "tournament")
        .replace(/[^\w]+/g, "_").replace(/^_+|_+$/g, "") || "tournament";
      a.download = `${stem}_${type}_pdfs.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      // Delay the revoke so the browser has time to initiate the download
      // before the blob URL is torn down. An immediate revoke races with the
      // download start and can produce an empty file in some browsers.
      setTimeout(() => window.URL.revokeObjectURL(dlUrl), 100);
      if (showToast) showToast("PDFs generated — download started.");
    } catch (err) {
      if (showToast) showToast("PDF export failed: " + err.message, "error");
      else alert("PDF export failed: " + err.message);
    } finally {
      setBusyType("");
    }
  };

  return (
    <div className="modal-backdrop" onClick={busyType ? undefined : onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Export PDFs">
        <div className="modal__head">
          <div className="modal__title">Export PDFs</div>
          <button type="button" className="modal__close" onClick={onClose} aria-label="Close" disabled={!!busyType}>✕</button>
        </div>
        <div className="modal__body" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <p style={{ margin: "0 0 4px", fontSize: 13, color: "var(--ink-3)" }}>
            Generates print-ready PDFs from the current tournament using LibreOffice.
            Each download is a ZIP. Generation can take up to a minute.
          </p>
          {PDF_EXPORT_TYPES.map(({ type, label, hint }) => (
            <div key={type} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "6px 0", borderBottom: "1px solid var(--line)" }}>
              <div>
                <div style={{ fontWeight: 600 }}>{label}</div>
                <div style={{ fontSize: 12, color: "var(--ink-3)" }}>{hint}</div>
              </div>
              <button
                className={"btn btn--sm" + (type === "all" ? " btn--primary" : "")}
                onClick={() => download(type)}
                disabled={!!busyType}
              >
                {busyType === type ? "Generating…" : "Download"}
              </button>
            </div>
          ))}
        </div>
        <div className="modal__foot">
          <button type="button" className="btn" onClick={onClose} disabled={!!busyType}>Close</button>
        </div>
      </div>
    </div>
  );
}

function CompCard({ c, onOpen, onStart, tournament, showToast }) {
  const { running: runningCount } = compMatchStats(c);
  const playerCount = (c.players || []).length;
  const courts = c.courts || [];
  const [shareOpen, setShareOpen] = useStateA(false);

  const canShare = tournament && tournament.mode === "self-run"
    && c.kind !== "team"
    && (!c.status || c.status === "setup");

  return (
    <>
      <div
        className="tcard"
        role="button"
        tabIndex={0}
        onClick={onOpen}
        // Only fire when focus is on the card itself, not on a nested
        // <button> inside .tcard__actions — otherwise Enter on
        // "Start Competition" would also trigger onOpen.
        onKeyDown={(e) => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); onOpen(); } }}
      >
        <div className="tcard__head">
          <div>
            <div className="tcard__eyebrow">{window.competitionKindLabel(c)}{c.teamSize ? ` · ${c.teamSize}-person` : ""}</div>
            <div className="tcard__name">{c.name}</div>
            <div className="tcard__meta">
              {c.date && <span style={{ fontWeight: 600 }}>{formatDate(c.date)}</span>}
              {c.date && c.startTime && " · "}
              {c.startTime && `Starts ${c.startTime}`}
              {/* Only emit the separator + court list when courts exist, and
                  only lead with " · " when something precedes it — otherwise
                  an empty/null-courts comp renders a dangling " · ". */}
              {courts.length > 0 && `${(c.date || c.startTime) ? " · " : ""}${courts.join(", ")}`}
            </div>
          </div>
          <StatusBadge status={c.status} format={c.format} />
        </div>
        <div className="tcard__stats">
          <div className="tcard__stat"><div className="v">{playerCount}</div><div className="l">{pluralize(playerCount, c.kind === "team" ? "Team" : "Player")}</div></div>
          <div className="tcard__stat"><div className="v">{courts.length}</div><div className="l">{pluralize(courts.length, "Shiaijo", "Shiaijo")}</div></div>
          {/* formatLabelShort lives in ui.jsx (not owned here); it emits the
              insider codes "P+KO" / "KO". Add a plain-language tooltip + a11y
              label inline so the abbreviation is legible without forking the
              shared helper. */}
          <div className="tcard__stat" title={FORMAT_LABEL_LONG[c.format] || "Format"}><div className="v" aria-label={FORMAT_LABEL_LONG[c.format] || undefined}>{formatLabelShort(c.format)}</div><div className="l">Format</div></div>
          {runningCount > 0 && <div className="tcard__stat"><div className="v" style={{ color: "var(--red)" }}>{runningCount}</div><div className="l">Now</div></div>}
        </div>
        <div className="tcard__actions">
          {(c.status === "draw-ready" || (c.status === "setup" && playerCount >= 2)) && (
            <button type="button" className="btn btn--primary btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onStart(); }}>Start Competition →</button>
          )}
          {(c.status === "pools" || c.status === "playoffs") && (
            <button type="button" className="btn btn--primary btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onOpen(); }}>Go to Scoring →</button>
          )}
          {c.status === "completed" && (
            <button type="button" className="btn btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onOpen(); }}>View Results</button>
          )}
          {canShare && (
            <button type="button" className="btn btn--sm btn--full" onClick={(e) => { e.stopPropagation(); setShareOpen(true); }}>
              Share registration link
            </button>
          )}
        </div>
      </div>
      {shareOpen && (
        <ShareRegistrationModal
          url={`${(window.linkBase || (() => window.location.origin))(tournament)}/register/${c.id}`}
          onClose={() => setShareOpen(false)}
          showToast={showToast}
        />
      )}
    </>
  );
}

function CourtPicker({ value, courts, onChange, btnClassName = "", label = "", align = "left" }) {
  const [open, setOpen] = useStateA(false);
  // Active option index for arrow-key navigation inside the open popover.
  const [activeIdx, setActiveIdx] = useStateA(0);
  const ref = useRefA(null);
  const triggerRef = useRefA(null);
  const optionRefs = useRefA([]);

  useEffectA(() => {
    if (!open) return;
    const close = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);

  // On open, seed the active option to the current court and move focus into
  // the popover. On close, return focus to the trigger so keyboard users
  // aren't dropped to <body>.
  useEffectA(() => {
    if (open) {
      const cur = Math.max(0, courts.indexOf(value));
      setActiveIdx(cur);
    } else {
      triggerRef.current && triggerRef.current.focus();
    }
  }, [open]);

  // Focus the active option element whenever it changes while open.
  useEffectA(() => {
    if (open && optionRefs.current[activeIdx]) {
      optionRefs.current[activeIdx].focus();
    }
  }, [open, activeIdx]);

  const select = (cc) => { setOpen(false); if (cc !== value) onChange(cc); };

  const onPopoverKeyDown = (e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
      return;
    }
    // Every branch below indexes into `courts`; with no courts there's nothing
    // to navigate or select, so bail before any `% courts.length` (÷0 → NaN) or
    // `courts[activeIdx]` (undefined) can run.
    if (!courts.length) return;
    if (e.key === "ArrowDown" || e.key === "ArrowRight") {
      e.preventDefault();
      setActiveIdx((i) => (i + 1) % courts.length);
    } else if (e.key === "ArrowUp" || e.key === "ArrowLeft") {
      e.preventDefault();
      setActiveIdx((i) => (i - 1 + courts.length) % courts.length);
    } else if (e.key === "Home") {
      e.preventDefault();
      setActiveIdx(0);
    } else if (e.key === "End") {
      e.preventDefault();
      setActiveIdx(courts.length - 1);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      select(courts[activeIdx]);
    }
  };

  return (
    <div ref={ref} style={{ position: "relative", display: "inline-block" }}>
      <button type="button"
        ref={triggerRef}
        className={btnClassName}
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
        title="Change shiaijo"
        aria-haspopup="listbox"
        aria-expanded={open}
      >{label}{value} ▾</button>
      {open && (
        <div
          className="court-popover"
          role="listbox"
          aria-label="Choose shiaijo"
          onKeyDown={onPopoverKeyDown}
          style={{ [align === "right" ? "right" : "left"]: 0, top: "100%", marginTop: 4 }}
        >
          {courts.map((cc, idx) => (
            <button type="button"
              key={cc}
              ref={(el) => { optionRefs.current[idx] = el; }}
              role="option"
              aria-selected={cc === value}
              tabIndex={idx === activeIdx ? 0 : -1}
              className={cc === value ? "is-current" : ""}
              onClick={(e) => { e.stopPropagation(); select(cc); }}
            >{cc}</button>
          ))}
        </div>
      )}
    </div>
  );
}

export { watchSustainedDisconnect };

window.Breadcrumbs = Breadcrumbs;
window.AdminTopbar = AdminTopbar;
window.AdminDashboard = AdminDashboard;
window.CompCard = CompCard;
window.CourtPicker = CourtPicker;
window.ExportPdfModal = ExportPdfModal;
window.AllWinnersModal = AllWinnersModal;
window.buildAllWinners = buildAllWinners;
