// Admin side — single tournament. Tournament has multiple Competitions.
// Top-level: Tournament dashboard (all competitions), per-competition pages.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const REFRESHABLE_EVENTS = new Set([
  "competition_started",
  "competition_completed",
  "match_updated",
  "tournament_updated",
]);

// mergeMatchPatch is produced by data.jsx and used by patchCompetitionData below.
const mergeMatchPatch = window.mergeMatchPatch;

// Page components rendered by AdminApp's view switch (produced by sibling
// admin_*.jsx files, all loaded before this one — see web-mobile/admin_split_plan.md).
const AdminDashboard = window.AdminDashboard;
const AdminCompetition = window.AdminCompetition;
const AdminEditTournament = window.AdminEditTournament;
const AdminCreateCompetition = window.AdminCreateCompetition;
const AdminImportPage = window.AdminImportPage;
const AdminSchedulePage = window.AdminSchedulePage;
const AdminScoreEditorPage = window.AdminScoreEditorPage;

function patchCompetitionData(prev, event) {
  if (!prev || !event.data) return prev;
  const { result, results } = event.data;
  const resultsToApply = results || (result ? [result] : []);
  if (resultsToApply.length === 0) return prev;

  const resultMap = new Map(resultsToApply.map(r => [r.id, r]));
  const next = { ...prev };
  let changed = false;

  if (next.poolMatches) {
    next.poolMatches = next.poolMatches.map(m => {
      const update = resultMap.get(m.id);
      if (update) { changed = true; return mergeMatchPatch(m, update); }
      return m;
    });
  }

  if (next.bracket && next.bracket.rounds) {
    let bChanged = false;
    const rounds = next.bracket.rounds.map(round =>
      round.map(m => {
        const update = resultMap.get(m.id);
        if (update) {
          bChanged = true; changed = true;
          // Map MatchResult to BracketMatch fields if needed
          const patch = { ...update };
          if (patch.ipponsA) patch.scoreA = patch.ipponsA.join("");
          if (patch.ipponsB) patch.scoreB = patch.ipponsB.join("");
          return mergeMatchPatch(m, patch);
        }
        return m;
      })
    );
    if (bChanged) next.bracket = { ...next.bracket, rounds };
  }

  return changed ? next : prev;
}

function AdminApp({ tournament, onUpdate, onLogout, onViewerMode, onPasswordChange, tweaks, password, view: propView, setView: propSetView, showToast }) {
  const [internalView, setInternalView] = useStateA({ kind: "dashboard" });
  const view = propView || internalView;
  const setView = propSetView || setInternalView;
  // Hooks must be declared unconditionally before any conditional returns.
  const [adminCompData, setAdminCompData] = useStateA(null);
  const [adminLoading, setAdminLoading] = useStateA(false);

  // Expose a navigation helper used by AdminTopbar's live-strip chips,
  // avoiding prop-drilling through every screen. Set once per mount.
  useEffectA(() => {
    window.__adminNavigateToScore = (compId) => setView({ kind: "competition", id: compId, section: "scores" });
    return () => { delete window.__adminNavigateToScore; };
  }, [setView]);

  const t = tournament;
  // Refs so the SSE subscription can read the latest tournament/onUpdate
  // without being torn down every time the tournament object is replaced.
  const tRef = useRefA(t);
  const onUpdateRef = useRefA(onUpdate);
  // Refs are read by the SSE subscription (which has narrower deps to avoid
  // teardown); refresh them only when the underlying props change.
  useEffectA(() => { tRef.current = t; onUpdateRef.current = onUpdate; }, [t, onUpdate]);

  const updateCompetition = async (cid, next) => {
    try {
      const updated = await window.API.updateCompetition(cid, next, password);
      // Merge PUT response locally; SSE will reconcile cross-client.
      const comps = (t.competitions || []).map(c => c.id === cid ? { ...c, ...updated } : c);
      onUpdate({ ...t, competitions: comps });
      if (view.kind === "competition" && view.id === cid) {
        const details = await window.API.fetchCompetitionDetails(cid);
        setAdminCompData(details);
      }
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const moveMatchCourt = async (compId, matchId, newCourt) => {
    try {
      await window.API.moveMatchCourt(compId, matchId, newCourt, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const editMatchScore = async (compId, matchId, result, match) => {
    try {
      await window.API.recordScore(compId, matchId, result, password, match);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
  };

  const addCompetition = async (c) => {
    try {
      const created = await window.API.createCompetition(c, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      return created;
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
  };

  const startAllCompetitions = async () => {
    const setupComps = (t.competitions || []).filter(c => c.status === "setup" && (c.players || []).length >= 2);
    if (setupComps.length === 0) return;

    showToast(`Starting ${setupComps.length} competitions…`);

    setAdminLoading(true);
    const results = await Promise.allSettled(setupComps.map(c => window.API.startCompetition(c.id, password)));

    let success = 0, fail = 0;
    results.forEach((r, i) => {
      if (r.status === "fulfilled") success++;
      else {
        console.error(`Failed to start ${setupComps[i].name}:`, r.reason);
        fail++;
      }
    });

    const comps = await window.API.fetchCompetitions();
    onUpdate({ ...t, competitions: comps });
    setAdminLoading(false);

    if (fail > 0) {
      showToast(`Started ${success} competitions, but ${fail} failed.`, "error");
    } else if (success > 0) {
      showToast(`Successfully started all ${success} competitions.`);
    }
  };

  const createPlayoff = async (sourceId) => {
    try {
      const created = await window.API.createPlayoff(sourceId, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      setView({ kind: "competition", id: created.id, section: "participants" });
      showToast(`Playoff "${created.name}" created`);
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const startCompetition = async (cid) => {
    const c = t.competitions.find(cc => cc.id === cid);
    if (!c) return;
    showToast(`Starting ${c.name}…`);
    try {
      await window.API.startCompetition(cid, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      showToast(`${c.name} started`);
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const updateTournament = async (patch) => {
    try {
      const next = { ...t, ...patch };
      // If password was not in the patch, preserve the current session password
      // (since tournament.password is cleared in the viewer API)
      if (!patch.password) {
        next.password = password;
      } else {
        // New password provided in the patch - update parent state and storage
        if (onPasswordChange) onPasswordChange(patch.password);
      }
      await window.API.updateTournament(next, password);
      onUpdate(next);
      showToast("Tournament updated");
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  useEffectA(() => {
    if (view.kind !== "competition") {
      setAdminCompData(null);
      return;
    }
    let cancelled = false;
    // Clear stale data from the previously-viewed competition so the
    // header/modal don't briefly render with another comp's identity.
    setAdminCompData(prev => (prev && prev.config && prev.config.id === view.id) ? prev : null);
    setAdminLoading(true);
    window.API.fetchCompetitionDetails(view.id)
      .then(data => {
        if (!cancelled) { setAdminCompData(data); setAdminLoading(false); }
      })
      .catch(err => {
        if (!cancelled) { console.error(err); setAdminLoading(false); }
      });
    return () => { cancelled = true; };
  }, [view.id, view.kind]);

  useEffectA(() => {
    if (view.kind === "competition") {
      // `cancelled` is the master gate: cleanup flips it true, and every
      // async resolution paths through it before calling state setters.
      // This is what makes navigation-during-in-flight-fetch safe.
      let cancelled = false;
      const compId = view.id;
      const timers = new Set();
      const schedule = (fn) => {
        const id = setTimeout(() => {
          timers.delete(id);
          if (cancelled) return;
          fn(compId);
        }, Math.random() * 500);
        timers.add(id);
      };
      // Coalesce tournament-wide refreshes across all events so the topbar's
      // live-strip stays current without firing one fetchCompetitions() per
      // match update.
      let pendingTournament = null;
      let tournamentFetching = false;
      // Tracks whether any event in the coalesced burst needs the tournament
      // config refetched (name/date/venue/courts). Match-level events only
      // need the competitions list; tournament_updated needs both.
      let pendingNeedsTournamentFetch = false;
      const scheduleTournamentRefresh = (needsTournament) => {
        if (needsTournament) pendingNeedsTournamentFetch = true;
        if (pendingTournament || tournamentFetching) return;
        const jitter = 500 + Math.random() * 1000;
        pendingTournament = setTimeout(() => {
          pendingTournament = null;
          if (cancelled) return;
          // tournamentFetching keeps the coalescing gate closed until the
          // fetch settles, so an event arriving during the in-flight window
          // can't kick off a second concurrent fetch that might resolve
          // out-of-order and clobber fresher data.
          tournamentFetching = true;
          const fetchTournament = pendingNeedsTournamentFetch;
          pendingNeedsTournamentFetch = false;
          const work = fetchTournament
            ? Promise.all([window.API.fetchTournament(), window.API.fetchCompetitions()])
                .then(([tourney, comps]) => ({ tourney, comps }))
            : window.API.fetchCompetitions().then(comps => ({ tourney: null, comps }));
          work
            .then(({ tourney, comps }) => {
              if (cancelled) return;
              const base = tourney || tRef.current;
              onUpdateRef.current({ ...base, competitions: comps });
            })
            .catch(err => console.error("Failed to refresh tournament/competitions", err))
            .finally(() => { tournamentFetching = false; });
        }, jitter);
      };
      const unsub = window.API.subscribeToEvents((event) => {
        if (cancelled) return;
        if (REFRESHABLE_EVENTS.has(event.type)) {
          // tournament_updated carries null data (tournament-wide change) — always
          // relevant.  match_updated / competition_started / competition_completed
          // carry competitionId — skip if they belong to a different competition.
          const relevant = event.type === "tournament_updated"
            || !event.data?.competitionId
            || event.data.competitionId === compId;
          if (relevant) {
            // Apply partial update immediately for responsive UI
            if (event.type === "match_updated") {
              setAdminCompData(prev => patchCompetitionData(prev, event));
            }
            // Still trigger full refresh (jittered) to reconcile standings/propagation
            schedule((targetId) => {
              window.API.fetchCompetitionDetails(targetId)
                .then(data => {
                  // Cancelled flag is the primary navigation guard. The id
                  // check is belt-and-braces in case a fetch comes back with
                  // unexpected data shape.
                  if (cancelled || data?.config?.id !== targetId) return;
                  setAdminCompData(data);
                })
                .catch(err => console.error("Failed to refresh competition details", err));
            });
          }
          // Any of these may change the topbar live-strip (matches becoming
          // live/done in *other* competitions, a comp starting, a dependent
          // playoff unblocking, …). Coalesced so a burst is one fetch.
          // tournament_updated also needs the tournament config itself.
          if (event.type === "match_updated"
              || event.type === "competition_started"
              || event.type === "competition_completed") {
            scheduleTournamentRefresh(false);
          } else if (event.type === "tournament_updated") {
            scheduleTournamentRefresh(true);
          }
        }
      });
      return () => {
        cancelled = true;
        timers.forEach(clearTimeout);
        timers.clear();
        if (pendingTournament) clearTimeout(pendingTournament);
        unsub();
      };
    }
  }, [view.id, view.kind]); // t and onUpdate accessed via refs to avoid churn

  if (view.kind === "dashboard") {
    return <AdminDashboard
      tournament={t}
      onOpenCompetition={(id, section) => setView({ kind: "competition", id, section: section || "overview" })}
      onCreateCompetition={() => setView({ kind: "createComp" })}
      onEditTournament={() => setView({ kind: "editTournament" })}
      onOpenSchedule={() => setView({ kind: "schedule" })}
      onOpenScoreEditor={() => setView({ kind: "scoreEditor" })}
      onOpenImport={() => setView({ kind: "import" })}
      onStartAll={startAllCompetitions}
      onStartCompetition={startCompetition}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      onUpdate={onUpdate}
    />;
  }

  if (view.kind === "createComp") {
    return <AdminCreateCompetition
      tournament={t}
      onCancel={() => setView({ kind: "dashboard" })}
      onCreate={async (c) => {
        try {
          const created = await addCompetition(c);
          setView({ kind: "competition", id: created.id, section: "participants" });
        } catch {
          // error already alerted inside addCompetition
        }
      }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
    />;
  }

  if (view.kind === "editTournament") {
    return <AdminEditTournament
      tournament={t}
      onCancel={() => setView({ kind: "dashboard" })}
      onSave={(patch) => { updateTournament(patch); setView({ kind: "dashboard" }); }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
    />;
  }

  if (view.kind === "schedule") {
    return <AdminSchedulePage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onMoveCourt={moveMatchCourt}
      onEditScore={editMatchScore}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
      password={password}
    />;
  }

  if (view.kind === "scoreEditor") {
    return <AdminScoreEditorPage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onEditScore={editMatchScore}
      onMoveCourt={moveMatchCourt}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
    />;
  }

  if (view.kind === "import") {
    return <AdminImportPage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onImported={async () => {
        const comps = await window.API.fetchCompetitions();
        onUpdate({ ...t, competitions: comps });
        setView({ kind: "dashboard" });
      }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      password={password}
    />;
  }

  if (view.kind === "competition") {
    const c = t.competitions.find((cc) => cc.id === view.id);
    if (!c) return <div className="page"><div className="empty"><h3>Competition not found</h3></div></div>;
    // Only use adminCompData when it matches the current view; otherwise it's stale.
    const detail = adminCompData && adminCompData.config && adminCompData.config.id === view.id ? adminCompData : null;
    if (adminLoading && !detail) return <div className="page"><div className="loading">Loading details...</div></div>;

    return <AdminCompetition
      tournament={t}
      competition={detail?.config || c}
      pools={detail?.pools}
      poolMatches={detail?.poolMatches}
      standings={detail?.standings}
      bracket={detail?.bracket}
      reservedSlots={detail?.reservedSlots || []}
      section={view.section}
      onSection={(section) => setView({ ...view, section })}
      onBack={() => setView({ kind: "dashboard" })}
      onOpenCompetition={(id, section) => setView({ kind: "competition", id, section: section || "overview" })}
      onUpdate={(next) => updateCompetition(c.id, next)}
      onCreatePlayoff={createPlayoff}
      onMoveCourt={moveMatchCourt}
      onEditScore={editMatchScore}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
      password={password}
      showToast={showToast}
    />;
  }
}

window.AdminApp = AdminApp;
