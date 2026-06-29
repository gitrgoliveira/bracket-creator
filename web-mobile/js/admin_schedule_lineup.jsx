// Per-match lineup components extracted from admin_schedule.jsx (mp-d7tl).
// pickCopySource, MatchLineupSideEditor (local), MatchLineupPanel.

import { LineupNameInput } from './admin_scoring_shared.jsx';

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
export function MatchLineupSideEditor({ comp, team, match, allMatches, password, showToast, allowDuringMatch = false }) {
  const teamSize = comp?.teamSize || 5;
  const { positionsForSize: lineupPositionsForSize, rosterFor: lineupRosterFor, teamIdOf: lineupTeamIdOf } = window.AdminLineupHelpers || {};
  const positions = (typeof lineupPositionsForSize === "function")
    ? lineupPositionsForSize(teamSize)
    : [];
  const roster = (typeof lineupRosterFor === "function")
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
  const suggestions = (window.AdminLineupHelpers && typeof window.AdminLineupHelpers.mergeRosterWithAssigned === "function")
    ? window.AdminLineupHelpers.mergeRosterWithAssigned(roster, { positions: values })
    : roster;
  const [lockedAt, setLockedAt] = useStateA(null);
  const [loading, setLoading] = useStateA(true);
  const [saving, setSaving] = useStateA(false);
  const [copying, setCopying] = useStateA(false);
  const [error, setError] = useStateA("");
  // Track whether the current match's lineup was loaded from a per-match
  // entry (true) or is inheriting the round default (false).
  const [isMatchOverride, setIsMatchOverride] = useStateA(false);
  // Audit reason for in-match lineup changes (allowDuringMatch=true + started).
  // pendingPositions holds the positions map while the ReasonPrompt is open.
  const [showLineupReasonPrompt, setShowLineupReasonPrompt] = useStateA(false);
  const [pendingPositions, setPendingPositions] = useStateA(null);

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
          setLockedAt(matchLineup.lockedAt || null);
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

  // Locked when this side's lineup carries a lockedAt OR the match itself is
  // already running/finished: the backend locks the whole match once it starts
  // (LockTeamLineupForMatch), so a side with no saved lineup yet must also
  // read as locked rather than show an editable form that 409s on save.
  const matchStarted = match?.status === "running" || match?.status === "completed";
  // allowDuringMatch is the operator override (officiated mode): a table
  // operator running behind can edit the lineup even after the match starts.
  // The save then sends force=true so the backend bypasses the same freeze.
  const isLocked = !allowDuringMatch && (!!lockedAt || matchStarted);

  const doSave = async (positionsOut, reason, successMsg = "Match lineup saved") => {
    setError("");
    setSaving(true);
    try {
      const updated = await window.API.putMatchLineup(compId, teamId, matchId, positionsOut, password, (allowDuringMatch && matchStarted), reason);
      // F5: a queued (offline/transient) write is NOT confirmed. Do NOT rebuild
      // the form from updated.positions (which is absent → would clear every
      // field) or show success; keep the operator's entered values and report
      // pending. The write is durable and will retry.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast("Offline: match lineup not saved yet, will retry");
        return;
      }
      // Reflect exactly what was persisted. This is also what applies the
      // copy-from-previous mid-match values: that path defers setValues until
      // this confirmed save, so a cancelled ReasonPrompt never leaves stale
      // copied values on screen.
      const next = {};
      positions.forEach(p => { next[p.key] = (updated.positions || {})[p.key] || ""; });
      setValues(next);
      setLockedAt(updated.lockedAt || null);
      setIsMatchOverride(true);
      if (typeof showToast === "function") showToast(successMsg);
    } catch (e) {
      // A 409 ErrLineupLocked means the match already started and the backend
      // froze the lineup. Surface the operator-friendly explanation rather than
      // the raw error string. Covers both save() and copyFromPrevious, which
      // both persist through doSave.
      const msg = e?.message || "Failed to save lineup";
      if (/ErrLineupLocked|lineup.*locked|locked/i.test(msg)) {
        setError("This match is in progress: lineup is locked and cannot be changed.");
      } else {
        setError(msg);
      }
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
    if (allowDuringMatch && matchStarted) {
      setPendingPositions(positionsOut);
      setShowLineupReasonPrompt(true);
    } else {
      doSave(positionsOut, "");
    }
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

      if (allowDuringMatch && matchStarted) {
        // Defer setValues until the save is confirmed so a cancelled
        // ReasonPrompt doesn't leave partially-copied values on screen.
        setPendingPositions(next);
        setShowLineupReasonPrompt(true);
      } else {
        // doSave applies the copied values from the persisted server response on
        // success, so we deliberately do NOT setValues eagerly here: a failed
        // save must not leave unpersisted copied values on screen. Pass the
        // distinct copy toast to preserve the pre-split confirmation.
        await doSave(next, "", "Lineup copied from previous match");
      }
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
        {isLocked && (
          <span style={{ fontSize: 11, fontWeight: 700, color: "var(--ink-3)", background: "var(--bg-2, #fafafa)", border: "1px solid var(--line, #ddd)", padding: "1px 6px", borderRadius: 3 }}>
            🔒 Locked
          </span>
        )}
      </div>

      {error && (
        <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 8, padding: "6px 8px", border: "1px solid var(--danger, #c00)", borderRadius: 4, background: "rgba(204,0,0,0.05)" }}>
          {error}
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
              disabled={isLocked || saving || copying}
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

      {/* Audit reason prompt for in-match lineup changes.
          Shown when allowDuringMatch=true and the match has started.
          Confirms a reason before calling doSave with force=true. */}
      {showLineupReasonPrompt && (
        <window.ReasonPrompt
          label="Reason for lineup change"
          presets={window.LINEUP_PRESETS || ["Late lineup", "Substitution", "Correction", "Other"]}
          submitting={saving}
          onConfirm={(r) => {
            setShowLineupReasonPrompt(false);
            doSave(pendingPositions || {}, r);
          }}
          onCancel={() => { setShowLineupReasonPrompt(false); setPendingPositions(null); }}
        />
      )}
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {!isLocked && (
          <button type="button"
            className="btn btn--primary btn--sm"
            onClick={save}
            disabled={saving || copying}
          >
            {saving ? "Saving…" : "Save lineup"}
          </button>
        )}
        <button type="button"
          className="btn btn--sm"
          onClick={copyFromPrevious}
          disabled={!hasSiblings || copying || saving || isLocked}
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
export function MatchLineupPanel({ match, tournament, password, showToast, onClose, variant = "modal", allowDuringMatch = false }) {
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
  // The match's sideA/sideB are normalized to { id, name }, where `id`
  // falls back to the team NAME when the backend has no UUID for that slot
  // (see api_serializers.normalizeMatch). A real participant's id is a
  // UUID, so the side key may be a name while the participant key is a
  // UUID (or vice-versa). We must therefore match on EITHER id OR name:
  // the previous `(p.id || p.name) === sideId` form compared only the
  // first truthy key (the UUID), which never equals a name-keyed sideId,
  // so the roster silently failed to resolve and every dropdown showed
  // "No roster found".
  const sideKey = (side) =>
    (side && typeof side === "object" ? (side.id || side.name) : side) || "";
  const sideAKey = sideKey(m.sideA);
  const sideBKey = sideKey(m.sideB);
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
              {allowDuringMatch
                ? "Set who fights each position. You can change this even after the match has started."
                : "Set per-match lineups below. Changes take effect when you save; the round-default lineup is used as a fallback until then."}
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
                allowDuringMatch={allowDuringMatch}
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
                allowDuringMatch={allowDuringMatch}
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
