// Admin side — single tournament. Tournament has multiple Competitions.
// Top-level: Tournament dashboard (all competitions), per-competition pages.

import { applyPatch as patchCompetitionData } from './patch.jsx';

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const REFRESHABLE_EVENTS = new Set([
  "competition_started",
  "competition_completed",
  "match_updated",
  "tournament_updated",
  // T099: competitor_status_updated fires when a participant's eligibility
  // changes (kiken/fusenpai/fusensho/recovery). Treat it like match_updated
  // for the purposes of refetching this competition's data — the change
  // affects who's eligible for downstream matches and the kachinuki
  // line-up, both of which the schedule/score-editor reads from the
  // competition-details payload. patch.jsx re-broadcasts the event as a
  // window-level CustomEvent for views that maintain their own cache.
  "competitor_status_updated",
  // schedule_updated fires when an operator moves a match to a different
  // court or changes its scheduled time. It carries no competitionId (the
  // relevance check at the subscription site treats any nil-data event as
  // tournament-wide and always triggers the fetchCompetitionDetails refresh).
  "schedule_updated",
  // T191/T192 (US13 — FR-050d): swiss_round_generated fires when a new
  // Swiss round's matches are persisted. Carries competitionId +
  // swissCurrentRound + matchCount; the admin section refetches comp
  // detail so the round counter, match list, and "Generate next round"
  // button enable-state all reconcile.
  "swiss_round_generated",
  // participants_updated fires when a participant is checked in or out.
  "participants_updated",
]);

// Page components rendered by AdminApp's view switch (produced by sibling
// admin_*.jsx files, all loaded before this one — see web-mobile/admin_split_plan.md).
const AdminDashboard = window.AdminDashboard;
const AdminCompetition = window.AdminCompetition;
const AdminEditTournament = window.AdminEditTournament;
const AdminCreateCompetition = window.AdminCreateCompetition;
const AdminImportPage = window.AdminImportPage;
const AdminSchedulePage = window.AdminSchedulePage;
const AdminScoreEditorPage = window.AdminScoreEditorPage;

// Pure helper for the "merge an updated competition into the latest
// tournament state" pattern used by AdminApp's async handlers. Takes
// the freshest tournament (from a ref, not the closure-captured prop)
// plus a competitions-array mutator, and returns the new tournament
// to pass to onUpdate.
//
// Bug shape this fixes: AdminApp's handlers (updateCompetition,
// moveMatchCourt, editMatchScore, addCompetition, startCompetition,
// createPlayoff, startAllCompetitions, the import onImported callback)
// all do `await window.API.X(...)` then
// `onUpdate({ ...t, competitions: comps })`. The closure-captured `t`
// is the tournament at handler-definition time — if SSE fires during
// the in-flight await and updates the tournament (another comp's
// match completes, a comp starts, etc.), the post-await onUpdate
// clobbers the SSE update with stale state. The tRef / onUpdateRef
// pair (declared in AdminApp at the top of the function and kept
// fresh via useEffect) exists for exactly this purpose, but the
// handlers weren't using it. Extracted as a pure helper so the merge
// logic can be unit-tested in isolation of React state.
function mergeCompetitionsIntoTournament(currentT, mutator) {
  return { ...currentT, competitions: mutator(currentT.competitions || []) };
}

// Pure helper for the "merge a tournament-level patch onto the latest
// tournament state" pattern used by AdminApp.updateTournament. Same
// bug-shape rationale as mergeCompetitionsIntoTournament above, but for
// top-level fields (name/date/venue/password) rather than the
// competitions array.
//
// Session-password restore (file mode only): the viewer API strips
// tournament.password from server responses, so a tournament loaded via
// the viewer endpoint has password="" locally even when the session
// still holds the real password. When the patch doesn't carry a new
// password, we restore the session password to the merged result so a
// subsequent re-save (or the API body we build from it) doesn't silently
// wipe the on-disk password.
//
// Locked mode: the backend now rejects PUT /api/tournament with a
// non-empty password (the env-var bcrypt hash is authoritative; the
// on-disk Password is irrelevant). Restoring the session password here
// would cause every locked-mode admin edit (name/venue/courts) to
// 400. When `locked` is true we explicitly send an empty password —
// the backend's locked-mode transform preserves whatever's on disk
// regardless.
function mergeTournamentPatch(currentT, patch, sessionPassword, locked) {
  const next = { ...currentT, ...patch };
  if (locked) {
    next.password = "";
    return next;
  }
  if (!patch.password) next.password = sessionPassword;
  return next;
}

function AdminApp({ tournament, onUpdate, onLogout, onViewerMode, onPasswordChange, tweaks, password, view: propView, setView: propSetView, showToast, authConfig }) {
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
  // AdminApp unmounts on logout / viewer-mode switch. Several async
  // handlers below (updateCompetition's fetchCompetitionDetails,
  // startAllCompetitions, createPlayoff) setAdminCompData / setAdminLoading
  // / setView post-await. Narrow window but real — gate via mountedRef.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Best-effort post-mutation refresh. Several mutation handlers below
  // (moveMatchCourt, editMatchScore, addCompetition, createPlayoff,
  // startCompetition) used to wrap the mutation AND the follow-up
  // fetchCompetitions in the same try/catch — so a transient refresh
  // failure (server slow, network blip) surfaced as a mutation failure,
  // even though the action was already persisted on disk. That misled
  // operators into retrying score saves / start-competition / create-playoff
  // ops that had already succeeded, sometimes producing "already started"
  // or duplicate-ID errors on the second attempt. Separating the refresh
  // into this helper makes the contract explicit: mutation errors throw,
  // refresh errors log + toast a "reload to see latest" hint.
  const refreshCompsBestEffort = async (actionLabel) => {
    try {
      const comps = await window.API.fetchCompetitions();
      if (!mountedRef.current) return;
      onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current, () => comps));
    } catch (e) {
      console.warn(`refresh after ${actionLabel} failed (action did succeed):`, e);
      if (mountedRef.current) showToast(`${actionLabel} succeeded; refresh failed — reload to see latest`, "error");
    }
  };

  // Specialised variant for CREATE flows (addCompetition / createPlayoff).
  // On refresh failure, merge the just-created record into local state so
  // the caller's immediate navigation to `view.kind="competition", id:
  // created.id` finds the comp in `t.competitions`. Pre-fix, the caller
  // would render the "Competition not found" branch until the next
  // refresh succeeded — confusing for the user who just clicked "Create".
  // Skips the merge if a (possibly newer) record with the same ID
  // already exists locally; otherwise appends.
  //
  // Defensive normalization: the create-response from the Go server can
  // ship `players: null` (Go nil slice → JSON null) when the handler
  // didn't populate Players before sending. Render paths read
  // `c.players.length`, which crashes on null. Server-side fixes
  // (handlers_competition.go) populate Players for the response, but
  // client-side defense ensures the fallback works even against an
  // older server that still ships null. See normalizeCreatedRecord at
  // the bottom of this file for the pure helper.
  const refreshCompsAfterCreate = async (created, actionLabel) => {
    try {
      const comps = await window.API.fetchCompetitions();
      if (!mountedRef.current) return;
      onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current, () => comps));
    } catch (e) {
      console.warn(`refresh after ${actionLabel} failed (action did succeed):`, e);
      if (mountedRef.current) {
        const normalized = normalizeCreatedRecord(created);
        onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current,
          comps => comps.some(c => c.id === normalized.id) ? comps : [...comps, normalized]));
        showToast(`${actionLabel} succeeded; refresh failed — reload to see latest`, "error");
      }
    }
  };

  // Re-throws after surfacing the error toast so callers can branch on
  // success vs failure. Pre-fix the catch swallowed the rejection, so
  // every caller that chained .then / .catch (or expected to await this)
  // saw a resolved Promise even when the underlying PUT failed —
  // producing UX like "Saved!" + "Save failed" toasts in quick
  // succession (AdminParticipants.apply) or a "Last saved 13:45:22"
  // stamp on a failed save (AdminSettings.saveNow).
  const updateCompetition = async (cid, next) => {
    let updated;
    try {
      updated = await window.API.updateCompetition(cid, next, password);
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
    // Merge PUT response into the LATEST tournament state. tRef.current
    // (not the closure-captured `t`) — see mergeCompetitionsIntoTournament
    // at the top of this file for why: SSE may have updated `t` during
    // the await above, and using the closure-captured `t` would clobber
    // those updates with stale state. Same reasoning applies to
    // onUpdateRef.current vs the closure-captured onUpdate.
    onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current,
      comps => comps.map(c => c.id === cid ? { ...c, ...updated } : c)));
    // Best-effort detail refresh. Isolated from the rethrow above —
    // a transient failure fetching details after a successful save
    // should not make callers (AdminSettings.saveNow, AdminParticipants.apply)
    // treat the save as failed. Log and move on; SSE will catch up.
    if (view.kind === "competition" && view.id === cid) {
      try {
        const details = await window.API.fetchCompetitionDetails(cid);
        if (mountedRef.current) setAdminCompData(details);
      } catch (e) {
        console.error("Failed to refresh competition details after save:", e);
      }
    }
  };

  // Mutation handlers split into two phases:
  //   1. Server mutation — failure shows the error toast and rethrows
  //      (or returns, depending on caller contract) so caller sees the
  //      real outcome.
  //   2. Post-mutation refresh — best-effort via refreshCompsBestEffort.
  //      A refresh failure cannot make the mutation "look failed."
  const moveMatchCourt = async (compId, matchId, newCourt) => {
    try {
      await window.API.moveMatchCourt(compId, matchId, newCourt, password);
    } catch (e) {
      showToast(e.message, "error");
      return;
    }
    await refreshCompsBestEffort("Move");
  };

  const editMatchScore = async (compId, matchId, result, match) => {
    try {
      await window.API.recordScore(compId, matchId, result, password, match);
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
    await refreshCompsBestEffort("Score");
  };

  const addCompetition = async (c) => {
    let created;
    try {
      created = await window.API.createCompetition(c, password);
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
    // Use the create-flow refresh: on refresh failure, merge `created`
    // into local state so the caller's immediate navigation to the new
    // comp's view doesn't render "Competition not found" — pre-fix the
    // generic refreshCompsBestEffort left local state stale, breaking
    // the post-create navigation.
    await refreshCompsAfterCreate(created, "Create");
    return created;
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

    // Wrap the refresh in try/finally so a fetchCompetitions() reject
    // doesn't leave setAdminLoading=true latched on (the loading spinner
    // would stay visible indefinitely until the next view change). The
    // mountedRef gate is preserved — if the user navigated away during
    // the start, we still skip setState but always clear the loading
    // flag via finally to avoid stale-state-on-remount surprises.
    try {
      const comps = await window.API.fetchCompetitions();
      if (!mountedRef.current) return;
      onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current, () => comps));
    } catch (e) {
      console.error("startAllCompetitions: post-start refresh failed", e);
      if (mountedRef.current) showToast("Failed to refresh after start. Try reloading.", "error");
    } finally {
      if (mountedRef.current) setAdminLoading(false);
    }

    if (fail > 0) {
      showToast(`Started ${success} competitions, but ${fail} failed.`, "error");
    } else if (success > 0) {
      showToast(`Successfully started all ${success} competitions.`);
    }
  };

  const createPlayoff = async (sourceId) => {
    let created;
    try {
      created = await window.API.createPlayoff(sourceId, password);
    } catch (e) {
      if (mountedRef.current) showToast(e.message, "error");
      return;
    }
    // See addCompetition: refresh-failure merges `created` into local
    // state so setView's navigation to the new playoff's ID finds it in
    // t.competitions. Pre-fix, refresh failure left the caller's
    // setView({kind:"competition", id: created.id, ...}) navigating to
    // a comp that AdminApp's `t.competitions.find(cc => cc.id === view.id)`
    // can't locate, producing the "Competition not found" empty state
    // until the next refresh.
    await refreshCompsAfterCreate(created, "Playoff create");
    if (!mountedRef.current) return;
    setView({ kind: "competition", id: created.id, section: "participants" });
    showToast(`Playoff "${created.name}" created`);
  };

  const startCompetition = async (cid) => {
    const c = t.competitions.find(cc => cc.id === cid);
    if (!c) return;
    showToast(`Starting ${c.name}…`);
    try {
      await window.API.startCompetition(cid, password);
    } catch (e) {
      showToast(e.message, "error");
      return;
    }
    await refreshCompsBestEffort("Start");
    showToast(`${c.name} started`);
  };

  const updateTournament = async (patch) => {
    try {
      // Surface a new password to the parent BEFORE the API call so the
      // session/store updates even if the request errors after (matches
      // pre-fix timing — onPasswordChange fired pre-await in the original).
      if (patch.password && onPasswordChange) onPasswordChange(patch.password);
      // Use tRef.current (not the closure-captured `t`) for BOTH the API
      // body and the post-await local-state update — same closure-capture
      // bug shape that mergeCompetitionsIntoTournament's docstring
      // describes for the other async handlers. If SSE updates the
      // tournament (e.g. a competition starts, a match completes in
      // another comp) during the in-flight save, the closure-captured
      // `t` would be stale; using it for `onUpdate(next)` would clobber
      // the SSE-driven updates with pre-save state until the next refresh.
      // Treat an unknown (null) authConfig conservatively — same as locked.
      // If the mode hasn't resolved yet, sending the session password in the
      // PUT body would trigger a 400 "password rotation is disabled" on a
      // locked-mode server. In file mode an empty body password is safe:
      // the backend preserves the on-disk value rather than clearing it.
      const locked = authConfig === null || authConfig.mode === "locked";
      const sendBody = mergeTournamentPatch(tRef.current, patch, password, locked);
      await window.API.updateTournament(sendBody, password);
      // Bail if the admin navigated away during the in-flight PUT —
      // mirrors startAllCompetitions / refreshCompsAfterCreate /
      // createPlayoff. Without this gate, onUpdateRef.current (parent's
      // setState) and showToast fire on an unmounted component,
      // producing the standard React setState-after-unmount warning.
      // Caught by /deep-review iterative pass after round-11 swept
      // updateTournament to tRef/onUpdateRef but missed the unmount
      // guard the other handlers carry.
      if (!mountedRef.current) return;
      // Re-read tRef.current AFTER the await — additional SSE events may
      // have landed during the save. The patch we just persisted is
      // applied on top of the freshest snapshot.
      onUpdateRef.current(mergeTournamentPatch(tRef.current, patch, password, locked));
      showToast("Tournament updated");
    } catch (e) {
      if (!mountedRef.current) return;
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
            // Apply partial update immediately for responsive UI.
            // T099: competitor_status_updated also routes through applyPatch
            // so the window-level CustomEvent fires for downstream listeners;
            // applyPatch returns `prev` unchanged for this event type, so
            // the setAdminCompData call is identity-preserving (no
            // gratuitous re-render).
            if (event.type === "match_updated" || event.type === "competitor_status_updated") {
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
              || event.type === "competition_completed"
              || event.type === "competitor_status_updated"
              || event.type === "swiss_round_generated") {
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
      authConfig={authConfig}
      password={password}
      showToast={showToast}
    />;
  }

  if (view.kind === "schedule") {
    return <AdminSchedulePage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onMoveCourt={moveMatchCourt}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
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
      password={password}
    />;
  }

  if (view.kind === "import") {
    return <AdminImportPage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onImported={async () => {
        const comps = await window.API.fetchCompetitions();
        // tRef/onUpdateRef so SSE updates during the in-flight import
        // are preserved — see mergeCompetitionsIntoTournament docstring.
        onUpdateRef.current(mergeCompetitionsIntoTournament(tRef.current, () => comps));
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
      onRefreshCompetition={() => window.API.fetchCompetitionDetails(c.id).then(setAdminCompData).catch(err => console.error("refresh failed:", err))}
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

// Pure helper for the "refresh-failure fallback merges raw server
// response" guard. The Go server can ship `players: null` (Go nil
// slice → JSON null) on CREATE-shape responses; if that lands in
// local state, render paths reading `c.players.length` crash.
// Defense-in-depth alongside the server-side fix.
function normalizeCreatedRecord(created) {
  return { ...created, players: created.players ?? [] };
}

window.AdminApp = AdminApp;
window.mergeCompetitionsIntoTournament = mergeCompetitionsIntoTournament;
window.mergeTournamentPatch = mergeTournamentPatch;
window.normalizeCreatedRecord = normalizeCreatedRecord;

export { mergeCompetitionsIntoTournament, mergeTournamentPatch, normalizeCreatedRecord };
