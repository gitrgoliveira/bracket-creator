// Per-match lineup components extracted from admin_schedule.jsx (mp-d7tl).
// pickCopySource, MatchLineupSideEditor (local), MatchLineupPanel.

import { LineupNameInput } from './admin_scoring_shared.jsx';
import { sideLookupKey } from './competitor_identity.jsx';

const { useState: useStateA, useEffect: useEffectA } = React;

// pickCopySource: pure helper that selects the most recent saved lineup
// among this team's *earlier* matches ("Copy from previous match").
// Exported for unit testing.
// Candidate filter: this team's matches, not the current match, with a saved
// lineup, scheduled at or before the current match's time (when it has one).
// Sort order: scheduledAt DESC (nulls last: unscheduled matches treated as
// least-recent), then court ASC, then queue-position (index in allMatches)
// ASC, then matchId DESC.
export function pickCopySource(allMatches, currentMatchId, teamId, savedLineups) {
  // savedLineups is a map of matchId → lineup (non-null only when a lineup
  // has been saved). Candidate = match for this team, not the current match,
  // with a saved lineup.
  //
  // teamId may be a single key or an array of keys ([id, name]). A match
  // side may be keyed by the team NAME (api_serializers name-as-id fallback)
  // while the team's real id is a UUID, so we match a side against ANY of
  // the provided keys by either its id OR its name: comparing only one key
  // space would silently find zero candidates (the original copy-from-
  // previous bug).
  const keys = (Array.isArray(teamId) ? teamId : [teamId]).filter(Boolean);
  const sideMatches = (side) => {
    if (side == null) return false;
    const sid = typeof side === "object" ? (side.id ?? side.ID) : side;
    const sname = typeof side === "object" ? (side.name ?? side.Name) : side;
    return keys.includes(sid) || keys.includes(sname);
  };
  // "Previous match": only consider siblings scheduled at or before the
  // current match. When the current match has no time, don't restrict (any
  // saved sibling is a valid source). An unscheduled sibling (no time) is
  // always allowed: it sorts last anyway.
  const current = allMatches.find(m => m.id === currentMatchId);
  const currentTime = current && current.scheduledAt ? current.scheduledAt : "";
  const candidates = allMatches.filter(m => {
    if (m.id === currentMatchId) return false;
    if (!savedLineups[m.id]) return false;
    if (!sideMatches(m.sideA) && !sideMatches(m.sideB)) return false;
    if (currentTime && m.scheduledAt && m.scheduledAt > currentTime) return false;
    return true;
  });
  if (candidates.length === 0) return null;
  candidates.sort((a, b) => {
    // scheduledAt DESC; null/missing treated as "" so they sort after any real
    // time string in a DESC comparison (unscheduled = least recent).
    const aT = a.scheduledAt || "";
    const bT = b.scheduledAt || "";
    if (aT !== bT) return bT.localeCompare(aT);
    // court ASC
    const aC = a.court || "";
    const bC = b.court || "";
    if (aC !== bC) return aC.localeCompare(bC);
    // queue/sequence: original index in allMatches (lower = earlier)
    const aIdx = allMatches.indexOf(a);
    const bIdx = allMatches.indexOf(b);
    if (aIdx !== bIdx) return aIdx - bIdx;
    // matchId DESC: a defensive final tiebreak. In practice distinct match
    // objects always have distinct indices above, so this is effectively
    // unreachable: kept only so the comparator is total.
    return (b.id || "").localeCompare(a.id || "");
  });
  return candidates[0];
}

// MatchLineupSideEditor: inline lineup editor for one team side within
// the per-match lineup panel. Handles load/save/copy-from-previous for a
// single (compId, teamId, matchId) triple.
// Reuses admin_lineup.jsx's exported helpers (positionsForSize, rosterFor,
// teamIdOf) so there is no duplication of position-label / roster logic.
// The helpers are read lazily on each render so module evaluation order
// does not matter (safe in test/bundler contexts too).
export function MatchLineupSideEditor({ comp, team, match, allMatches, password, showToast }) {
  const teamSize = comp?.teamSize || 5;
  const { positionsForSize: lineupPositionsForSize, rosterFor: lineupRosterFor, teamIdOf: lineupTeamIdOf } = window.AdminLineupHelpers || {};
  const positions = (typeof lineupPositionsForSize === "function")
    ? lineupPositionsForSize(teamSize)
    : [];
  // The team's members as the PRE-SQUAD model stored them, in the roster
  // row's untyped metadata array. Kept only as a fallback: see effectiveRoster
  // below, which prefers the squad.
  const legacyRoster = (typeof lineupRosterFor === "function")
    ? lineupRosterFor(team)
    : [];
  const teamId = (typeof lineupTeamIdOf === "function")
    ? lineupTeamIdOf(team)
    : (team?.id || team?.name || "");
  const compId = comp?.id || "";
  const matchId = match?.id || "";

  // A match "involves" this team when either side resolves to it by id OR by
  // name. Match sides may be keyed by team NAME (api_serializers name-as-id
  // fallback) while teamId is the participant UUID, so we compare against
  // both keys: the same id-vs-name pitfall the roster resolver hits.
  const teamKeys = [team?.id, team?.ID, team?.name, team?.Name].filter(Boolean);
  const sideMatchesTeam = (side) => {
    if (side == null) return false;
    const sid = typeof side === "object" ? (side.id ?? side.ID) : side;
    const sname = typeof side === "object" ? (side.name ?? side.Name) : side;
    return teamKeys.includes(sid) || teamKeys.includes(sname);
  };
  const matchInvolvesTeam = (mm) => sideMatchesTeam(mm.sideA) || sideMatchesTeam(mm.sideB);

  const [values, setValues] = useStateA(() => {
    const init = {};
    positions.forEach(p => { init[p.key] = ""; });
    return init;
  });
  const [loading, setLoading] = useStateA(true);
  const [saving, setSaving] = useStateA(false);
  const [copying, setCopying] = useStateA(false);
  const [error, setError] = useStateA("");
  // Track whether the current match's lineup was loaded from a per-match
  // entry (true) or is inheriting the round default (false).
  const [isMatchOverride, setIsMatchOverride] = useStateA(false);
  // bc-cse gap closure: the composed operator-facing warning shown after a
  // SUCCESSFUL save whose squad-member attachment fell short (see doSave
  // below). Deliberately a separate channel from `error`: the save did not
  // fail, so it must never look like the red error banner above it.
  const [lineupWarning, setLineupWarning] = useStateA("");

  // bc-pnum gap closure: this team's squad, loaded once so save() can
  // resolve a typed/picked name to its member id (see doSave below) --
  // this panel is free-text (LineupNameInput), unlike the round-scoped
  // AdminLineup form's select-by-id picker, so a name→id lookup is needed
  // here at all. Independent of the lineup-load effect below: a squad
  // fetch failure must not block loading OR saving the lineup itself, so
  // it is swallowed and the resolver (window.AdminLineupHelpers.
  // resolveMemberIdsForPositions) simply mints for every name it cannot
  // find against an empty list. bc-cse: `squadUnavailable` records that this
  // happened, so doSave's warning names the real root cause instead of
  // reporting every position the resolver then "failed" to match.
  const [squad, setSquad] = useStateA([]);
  const [squadUnavailable, setSquadUnavailable] = useStateA(false);

  // bc-pnum: the suggestion list reads the SQUAD, because that is where a
  // team's members now live. rosterFor reads the roster row's metadata array,
  // which was their home before this PR moved them out, so a team whose
  // members were entered through the squad UI arrived here looking empty --
  // the operator was told "this team has no registered members" under a full
  // squad, and had to retype every name the app already knew. Blank entries
  // are skipped: a squad is seeded with one unnamed position per team size,
  // and an unnamed position is not a person to suggest.
  //
  // The metadata array stays as the fallback for the two windows where the
  // squad is not available: before the load-time migration has folded a
  // legacy team's members into one, and when the squad fetch above failed
  // (squadUnavailable), where falling back beats suggesting nothing.
  const squadNames = squad.map(m => ((m && m.name) || "").trim()).filter(Boolean);
  const roster = squadNames.length > 0 ? squadNames : legacyRoster;
  const suggestions = (window.AdminLineupHelpers && typeof window.AdminLineupHelpers.mergeRosterWithAssigned === "function")
    ? window.AdminLineupHelpers.mergeRosterWithAssigned(roster, { positions: values })
    : roster;
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId) return;
    (async () => {
      try {
        const squads = await window.API.fetchSquads(compId, password);
        if (!cancelled) setSquad((squads && squads[teamId]) || []);
      } catch (_e) {
        if (!cancelled) setSquadUnavailable(true);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId]);

  // Load per-match lineup on mount; record whether it was a real hit.
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId || !matchId) {
      setLoading(false);
      return;
    }
    (async () => {
      try {
        const matchLineup = await window.API.fetchMatchLineup(compId, teamId, matchId);
        if (cancelled) return;
        if (matchLineup) {
          const next = {};
          positions.forEach(p => {
            next[p.key] = (matchLineup.positions || {})[p.key] || "";
          });
          setValues(next);
          setIsMatchOverride(true);
        } else {
          // No per-match entry: reflect the round default (fetch-and-show,
          // but do NOT set isMatchOverride so the label says "inheriting").
          const round = window.resolveRoundIndex(match);
          try {
            const roundLineup = await window.API.fetchTeamLineup(compId, teamId, round);
            if (cancelled) return;
            if (roundLineup) {
              const next = {};
              positions.forEach(p => {
                next[p.key] = (roundLineup.positions || {})[p.key] || "";
              });
              setValues(next);
            }
          } catch (_e) { /* no round lineup: leave blank */ }
          setIsMatchOverride(false);
        }
      } catch (e) {
        if (!cancelled) setError(e?.message || "Failed to load lineup");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId, matchId]);

  const doSave = async (positionsOut, successMsg = "Match lineup saved") => {
    setError("");
    setLineupWarning("");
    setSaving(true);
    try {
      // bc-pnum gap closure: resolve each occupied position's name to a
      // squad member id before writing. A name not on the squad is a
      // substitute typed straight into the slot; per operator ruling,
      // adding a new name in a position MINTS the member in that one step
      // (see resolveMemberIdsForPositions, admin_lineup.jsx -- the ONE
      // place this resolve/mint contract lives, shared with the inline
      // in-modal picker in admin_scoring_team.jsx). A resolve/mint failure
      // (offline venue wifi -- this panel's whole reason for existing) must
      // never block the save: the helper simply omits that position's id
      // and the write proceeds with the names alone, exactly as it
      // behaves today. bc-cse: the failure is no longer discarded either --
      // `memberFailures` carries it through to the warning shown below on
      // a successful save.
      let memberIdsOut = {};
      let memberFailures = [];
      try {
        const resolver = window.AdminLineupHelpers?.resolveMemberIdsForPositions;
        if (typeof resolver === "function") {
          const resolved = await resolver(compId, teamId, positionsOut, squad, password);
          memberIdsOut = resolved.memberIds || {};
          memberFailures = resolved.failures || [];
          setSquad(resolved.squad);
        }
      } catch (_e) {
        // Defense in depth on top of the helper's own per-position mint
        // catch: even an unexpected failure IN the resolver itself must
        // not block the save. Proceed with the names alone.
      }
      const hasMemberIds = Object.keys(memberIdsOut).length > 0;
      const updated = await window.API.putMatchLineup(
        compId, teamId, matchId, positionsOut, password, hasMemberIds ? memberIdsOut : undefined
      );
      // F5: a queued (offline/transient) write is NOT confirmed. Do NOT rebuild
      // the form from updated.positions (which is absent, would clear every
      // field) or show success; keep the operator's entered values and report
      // pending. The write is durable and will retry.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast("Offline: match lineup not saved yet, will retry");
        return;
      }
      // Reflect exactly what was persisted.
      const next = {};
      positions.forEach(p => { next[p.key] = (updated.positions || {})[p.key] || ""; });
      setValues(next);
      setIsMatchOverride(true);
      if (typeof showToast === "function") showToast(successMsg);
      const composer = window.AdminLineupHelpers?.memberIdentityWarning;
      if (typeof composer === "function") {
        setLineupWarning(composer(memberFailures, squadUnavailable));
      }
    } catch (e) {
      setError(e?.message || "Failed to save lineup");
    } finally {
      setSaving(false);
    }
  };

  const save = () => {
    // Strip empty positions before PUT. The handler replaces the whole
    // positions map (TeamLineup{Positions: req.Positions}), and the domain
    // validator treats an absent key the same as an explicit "": both
    // "missing". Sending explicit empties only bloats the persisted YAML.
    const positionsOut = {};
    positions.forEach(p => {
      // Trim here too (not only at the picker's onSelect) so a Save can never
      // persist leading/trailing or whitespace-only names: matches AdminLineup.
      const v = (values[p.key] || "").trim();
      if (v) positionsOut[p.key] = v;
    });
    doSave(positionsOut);
  };

  const hasSiblings = allMatches.some(m => m.id !== matchId && matchInvolvesTeam(m));

  const copyFromPrevious = async () => {
    setCopying(true);
    setError("");
    try {
      // There is no bulk "lineup headers" endpoint, so probe each sibling match
      // for this team in parallel. A null result means that sibling has no saved
      // lineup; we keep the fetched lineup objects so the chosen source needs no
      // second round-trip.
      const siblings = allMatches.filter(m => m.id !== matchId && matchInvolvesTeam(m));
      const results = await Promise.all(
        siblings.map(s =>
          window.API.fetchMatchLineup(compId, teamId, s.id)
            .then(l => ({ matchId: s.id, lineup: l }))
            .catch(() => ({ matchId: s.id, lineup: null }))
        )
      );
      const savedLineups = {};
      results.forEach(({ matchId: mid, lineup }) => { if (lineup) savedLineups[mid] = lineup; });

      const source = pickCopySource(allMatches, matchId, [teamId, ...teamKeys], savedLineups);
      if (!source) {
        setError("No previous match found to copy from.");
        return;
      }
      const sourceLineup = savedLineups[source.id];
      if (!sourceLineup) {
        setError("Previous lineup is empty or unavailable.");
        return;
      }
      // Strip empty positions (see save()): copy only the slots that are set.
      const next = {};
      positions.forEach(p => { const v = (sourceLineup.positions || {})[p.key]; if (v) next[p.key] = v; });

      // doSave applies the copied values from the persisted server response on
      // success, so we deliberately do NOT setValues eagerly here: a failed
      // save must not leave unpersisted copied values on screen.
      await doSave(next, "Lineup copied from previous match");
    } catch (e) {
      setError(e?.message || "Failed to copy lineup");
    } finally {
      setCopying(false);
    }
  };

  if (loading) return <div style={{ fontSize: 12, color: "var(--ink-3)" }}>Loading lineup…</div>;

  const teamName = team?.name || team?.Name || "Team";

  return (
    <div data-testid={`match-lineup-side-${teamId}`}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <span style={{ fontWeight: 700, fontSize: 13 }}>{teamName}</span>
        {isMatchOverride
          ? <span style={{ fontSize: 11, color: "var(--accent, #1d73d5)", fontWeight: 600 }}>Override for this match</span>
          : <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Inheriting round default</span>
        }
      </div>

      {error && (
        <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 8, padding: "6px 8px", border: "1px solid var(--danger, #c00)", borderRadius: 4, background: "rgba(204,0,0,0.05)" }}>
          {error}
        </div>
      )}

      {/* Non-blocking: the save above already succeeded. Amber .alert--warn
          so it can never be mistaken for the red error banner above. */}
      {lineupWarning && (
        <div className="alert alert--warn" role="status" data-testid={`match-lineup-warning-${teamId}`} style={{ marginBottom: 8 }}>
          {lineupWarning}
        </div>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 12 }}>
        {positions.map(p => (
          <label key={p.key} data-testid={`match-lineup-pos-${teamId}-${p.key}`} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
            <span style={{ minWidth: 72, fontWeight: 600, color: "var(--ink-2)", fontSize: 12 }}>{p.label}</span>
            <LineupNameInput
              value={values[p.key] || ""}
              roster={suggestions}
              ariaLabel={`${p.label} player`}
              disabled={saving || copying}
              onSelect={(name) => setValues(v => ({ ...v, [p.key]: (name || "").trim() }))}
            />
          </label>
        ))}
        {roster.length === 0 && (
          <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
            This team has no registered members: type each competitor's name directly.
          </div>
        )}
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <button type="button"
          className="btn btn--primary btn--sm"
          onClick={save}
          disabled={saving || copying}
        >
          {saving ? "Saving…" : "Save lineup"}
        </button>
        <button type="button"
          className="btn btn--sm"
          onClick={copyFromPrevious}
          disabled={!hasSiblings || copying || saving}
          title={hasSiblings
            ? "Find and copy the lineup from the most recent previous match"
            : "No other matches for this team"}
        >
          {copying ? "Copying…" : "Copy from previous match"}
        </button>
      </div>
    </div>
  );
}

// MatchLineupPanel: modal overlay for per-match lineup editing. Renders
// one MatchLineupSideEditor per team side (sideA / sideB). Only shown for
// team competitions (compKind === "team" || teamSize > 0).
export function MatchLineupPanel({ match, tournament, password, showToast, onClose, variant = "modal" }) {
  const m = match;
  // Find the competition this match belongs to so we can access teamSize,
  // players (roster), etc.
  const comp = (tournament?.competitions || []).find(cc => cc.id === m.compId) || null;
  const isTeamComp = comp && (comp.kind === "team" || (comp.teamSize || 0) > 0);

  if (!isTeamComp) return null;

  // Resolve team objects from the competition's player list. comp.players
  // (loaded via /api/viewer/competitions) already carries each team's
  // metadata (member roster): no extra participants fetch is needed.
  //
  // The match's sideA/sideB are normalized to { id, name } (see
  // api_serializers.resolveSide); `id` is the participant's real UUID when
  // resolved, or "" when the backend has no UUID for that slot -- resolveSide
  // never invents an id from the team NAME. sideLookupKey (competitor_
  // identity.jsx) falls back to side.name in that "" case, which is the
  // recovery path this file needs: id over name (a real id decides, since a
  // UUID never coincidentally equals another team's display name), so
  // matchesKey's `p.id === key || p.name === key` only ever succeeds by
  // name when key itself carries no real id to offer (an id-less side).
  // The previous `(p.id || p.name) === sideId` form compared only the
  // first truthy key (the UUID), which never equals a name-keyed sideId,
  // so the roster silently failed to resolve and every dropdown showed
  // "No roster found" -- do not reintroduce that single-key form.
  const sideAKey = sideLookupKey(m.sideA);
  const sideBKey = sideLookupKey(m.sideB);
  const players = comp.players || [];
  const matchesKey = (p, key) =>
    !!key && (p.id === key || p.ID === key || p.name === key || p.Name === key);
  const teamA = players.find(p => matchesKey(p, sideAKey)) || (m.sideA && typeof m.sideA === "object" ? m.sideA : null);
  const teamB = players.find(p => matchesKey(p, sideBKey)) || (m.sideB && typeof m.sideB === "object" ? m.sideB : null);

  // All matches for this competition (needed for "Copy from previous" candidate search).
  const allMatches = typeof window.compMatches === "function" ? window.compMatches(comp) : [];

  const inner = (
    <>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16 }}>
          <div>
            <div className="overline">
              {comp?.name} · {m.scheduledAt || m.round || ""}
            </div>
            <h2 style={{ margin: "4px 0 0", fontSize: 20, fontWeight: 700 }}>Lineup for this match</h2>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>
              Set per-match lineups below. Changes take effect when you save; the round-default lineup is used as a fallback until then.
            </div>
          </div>
          <button type="button" className="btn btn--ghost btn--sm" onClick={onClose}>{variant === "inline" ? "Done" : "✕ Close"}</button>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }}>
          <div style={{ borderRight: "1px solid var(--line, #e5e7eb)", paddingRight: 20 }}>
            <div className="overline" style={{ marginBottom: 8 }}>
              SHIRO (white)
            </div>
            {teamB ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-b`}
                comp={comp}
                team={teamB}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
          <div>
            <div className="overline" style={{ marginBottom: 8 }}>
              AKA (red)
            </div>
            {teamA ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-a`}
                comp={comp}
                team={teamA}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
        </div>
    </>
  );

  // Inline (mp-c2yr): render in-flow inside the operator console's main
  // column: no fixed overlay, no backdrop. The shiaijo page owns the
  // surrounding card; here we just provide padding + scroll.
  if (variant === "inline") {
    return <div className="scoring-panel lineup-panel--inline" aria-label="Lineup for this match">{inner}</div>;
  }

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.35)",
      display: "flex", alignItems: "center", justifyContent: "center",
      zIndex: 1000, padding: 16
    }}>
      <div style={{
        background: "var(--bg, #fff)", borderRadius: 8,
        boxShadow: "0 8px 32px rgba(0,0,0,0.18)", padding: 24,
        width: "100%", maxWidth: 680, maxHeight: "90vh", overflowY: "auto"
      }}>
        {inner}
      </div>
    </div>
  );
}
