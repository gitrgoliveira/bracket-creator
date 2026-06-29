// Per-team lineup form for FR-040 (T129/T130).
//
// Teams pick which player occupies each named position (Senpo, Jiho,
// Chuken, Fukusho, Taisho for 5-person teams; numeric "1"..."N" for
// other sizes) before the round's first match starts. Once the
// server stamps LockedAt the form locks; a "Revise" affordance reopens
// it locally as long as the current round is finished AND the next
// round hasn't started yet.
//
// Wire shape (matches domain.TeamLineup):
//   {
//     teamId: "team-1",
//     competitionId: "...",
//     round: 0,
//     positions: { senpo: "p_001", ... },
//     lockedAt: null | ISO-string
//   }
//
// The team's roster lives in the competition's player object: for team
// competitions, each c.players[i] is the team (its Name is the team
// name, its Metadata is the list of member names per the CSV parser at
// internal/helper/tournament.go).

import { LineupNameInput } from './admin_scoring_shared.jsx';

const { useState: useStateA, useEffect: useEffectA, useMemo: useMemoA } = React;

// Term: kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and this module doesn't matter (both
// are type="module" and execute asynchronously). U1 / glossary.md.
function TermAL(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Canonical FIK position order for 5-person teams. Numeric sizes use
// "1".."N" generated below. Each label carries an optional `termId` so
// the renderer can wrap the label in a <Term> tooltip (U1).
const POS_LABELS_5 = [
  { key: "senpo", label: "Senpo", termId: "senpo" },
  { key: "jiho", label: "Jiho", termId: "jiho" },
  { key: "chuken", label: "Chuken", termId: "chuken" },
  { key: "fukusho", label: "Fukusho", termId: "fukusho" },
  { key: "taisho", label: "Taisho", termId: "taisho" },
];

function positionsForSize(teamSize) {
  if (teamSize === 5) return POS_LABELS_5;
  return Array.from({ length: teamSize }, (_, i) => ({
    key: String(i + 1),
    label: String(i + 1),
  }));
}

// Pull the member roster off the team Player object. The CSV parser
// stores member names in Metadata; fall back to the team name itself
// so the dropdown is never empty (the operator can still save and the
// server will validate the positions against the lineup rules).
function rosterFor(team) {
  if (!team) return [];
  if (Array.isArray(team.metadata) && team.metadata.length > 0) return team.metadata;
  if (Array.isArray(team.Metadata) && team.Metadata.length > 0) return team.Metadata;
  return [];
}

// mergeRosterWithAssigned unions a team's base roster (its registered members,
// from team.metadata via rosterFor) with any names already assigned in the
// team's lineup. An operator who enters a substitute via the picker's "+ Add …"
// row stores a free name that is NOT in team.metadata; without this union that
// name would never reappear in the autocomplete for the team's OTHER positions.
// Base (registered) names come first in their original order; extra assigned
// names follow in first-seen order. De-duplication is case-insensitive; blank /
// whitespace assignments are ignored. The base array is never mutated.
function mergeRosterWithAssigned(baseRoster, lineup) {
  const base = Array.isArray(baseRoster) ? baseRoster : [];
  const positions = lineup && lineup.positions ? lineup.positions : null;
  if (!positions) return base;
  const seen = new Set(base.map(n => String(n).trim().toLowerCase()));
  const extras = [];
  for (const raw of Object.values(positions)) {
    const name = String(raw == null ? "" : raw).trim();
    if (!name) continue;
    const key = name.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    extras.push(name);
  }
  return extras.length ? [...base, ...extras] : base;
}

// Resolve the team's stable ID. Backend uses player.id (UUID assigned
// at first persist); pre-persist teams may not have one yet: fall back
// to name as a best-effort key.
function teamIdOf(team) {
  return team?.id || team?.ID || team?.name || team?.Name || "";
}

// "Revise" is enabled when the current round's matches are all completed
// AND the next round hasn't started yet. Uses m.roundIndex (0-based
// backend index stamped by compMatches) for precise round filtering.
function canRevise(competition, round) {
  if (!competition || !window.compMatches) return false;
  const all = window.compMatches(competition);
  const currentMatches = all.filter(m => m.phase === "bracket" && m.roundIndex === round);
  const nextMatches = all.filter(m => m.phase === "bracket" && m.roundIndex === round + 1);
  if (!currentMatches.length) return false;
  const inProgressNext = nextMatches.some(m => m.status === "running" || m.status === "completed");
  if (inProgressNext) return false;
  return currentMatches.every(m => m.status === "completed");
}

function AdminLineup({ comp, team, round, password, showToast, onClose }) {
  const teamSize = comp?.teamSize || 5;
  const positions = useMemoA(() => positionsForSize(teamSize), [teamSize]);
  const roster = useMemoA(() => rosterFor(team), [team]);
  const teamId = teamIdOf(team);
  const compId = comp?.id || "";
  const reviseEligible = canRevise(comp, round);

  // Lineup state: { position-key -> player name (or member ID once
  // members are first-class). The backend stores player IDs but our
  // current Player model exposes member names through Metadata: so
  // we save what the dropdown returns. The server validates either way.
  const [values, setValues] = useStateA(() => {
    const init = {};
    positions.forEach(p => { init[p.key] = ""; });
    return init;
  });
  const [lockedAt, setLockedAt] = useStateA(null);
  const [loading, setLoading] = useStateA(true);
  const [saving, setSaving] = useStateA(false);
  const [error, setError] = useStateA("");
  const [revising, setRevising] = useStateA(false);

  // Load any existing lineup for (compId, teamId, round). 404 → fresh
  // form (server returns 404 when no lineup has been submitted yet,
  // which the api_client.fetchTeamLineup maps to null).
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId) {
      setLoading(false);
      return;
    }
    (async () => {
      try {
        const lineup = await window.API.fetchTeamLineup(compId, teamId, round);
        if (cancelled) return;
        if (lineup) {
          const next = {};
          positions.forEach(p => {
            next[p.key] = (lineup.positions || {})[p.key] || "";
          });
          setValues(next);
          setLockedAt(lineup.lockedAt || null);
        }
      } catch (e) {
        if (!cancelled) setError(e?.message || "Failed to load lineup");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId, round]);

  const isLocked = !!lockedAt && !revising;

  // Autocomplete suggestions: the registered roster (if any) plus names already
  // typed into this lineup, so a name entered once is reusable across positions.
  const suggestions = mergeRosterWithAssigned(roster, { positions: values });

  const save = async () => {
    setError("");
    setSaving(true);
    try {
      // Strip empty positions so the server doesn't see them as
      // explicit empty strings: domain.TeamLineup.Validate counts
      // empty values as "missing", which is what we want.
      const positionsOut = {};
      Object.entries(values).forEach(([k, v]) => {
        // Trim here too (not just onBlur) so a Save triggered without a blur:
        // e.g. Enter: never persists leading/trailing or whitespace-only names.
        const trimmed = (v || "").trim();
        if (trimmed) positionsOut[k] = trimmed;
      });
      const updated = await window.API.putTeamLineup(compId, teamId, round, positionsOut, password);
      // F5: a queued (offline/transient) write is NOT a confirmed save: don't
      // clear the revising state or show "saved"; the write is durable and will
      // retry. Keep the form editable and tell the operator it's pending.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast("Offline: lineup not saved yet, will retry");
        return;
      }
      setLockedAt(updated.lockedAt || null);
      setRevising(false);
      if (typeof showToast === "function") showToast("Lineup saved");
    } catch (e) {
      setError(e?.message || "Failed to save lineup");
    } finally {
      setSaving(false);
    }
  };

  const onRevise = () => {
    setError("");
    // Local-only flip: the server will accept the next PUT only if
    // the round's first match hasn't started yet (same 409 path).
    setRevising(true);
  };

  if (loading) {
    return <div className="page" style={{ padding: 24 }}>Loading lineup…</div>;
  }

  return (
    <div className="page" data-testid="lineup-form-root" style={{ padding: 24, maxWidth: 640 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
        <div>
          <div className="overline">
            {comp?.name} · Round {round + 1}
          </div>
          <h2 style={{ margin: "4px 0 0 0", fontSize: 22, fontWeight: 700 }}>
            {team?.name || team?.Name || "Team"} : Lineup
          </h2>
          <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>
            {teamSize}-person team
            {comp?.teamMatchType === "kachinuki" && <span style={{ marginLeft: 8, color: "var(--accent)", fontWeight: 700 }}>· <TermAL name="kachinuki">Kachinuki</TermAL> (winner-stays)</span>}
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
          {isLocked && (
            <span className="viewer__admin-pill" style={{ fontSize: 11, fontWeight: 700, background: "var(--bg-2, #fafafa)", border: "1px solid var(--line, #ddd)", padding: "2px 8px", borderRadius: 4 }}>
              🔒 Locked
            </span>
          )}
          {onClose && (
            <button type="button" className="btn btn--ghost btn--sm" onClick={onClose}>✕ Close</button>
          )}
        </div>
      </div>

      {error && (
        <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 12, padding: 8, border: "1px solid var(--danger, #c00)", borderRadius: 4, background: "rgba(204,0,0,0.05)" }}>
          {error}
        </div>
      )}

      <div className="card" style={{ padding: 16 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {positions.map(p => (
            <label key={p.key} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
              <span style={{ fontSize: 12, fontWeight: 600, color: "var(--ink-2)" }}>
                {p.termId
                  ? <TermAL name={p.termId}>{p.label}</TermAL>
                  : p.label}
              </span>
              <LineupNameInput
                value={values[p.key] || ""}
                roster={suggestions}
                ariaLabel={`${p.label} player`}
                disabled={isLocked || saving}
                onSelect={(name) => setValues(v => ({ ...v, [p.key]: name }))}
              />
            </label>
          ))}
          {roster.length === 0 && (
            <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
              This team has no registered members: type each competitor's name directly.
            </div>
          )}
        </div>

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 20 }}>
          {isLocked ? (
            <button type="button"
              className="btn btn--primary"
              onClick={onRevise}
              disabled={!reviseEligible}
              title={reviseEligible
                ? "Revise lineup (allowed before the next round starts)"
                : "Cannot revise: next round has already begun"}
            >
              Revise
            </button>
          ) : (
            <button type="button"
              className="btn btn--primary"
              onClick={save}
              disabled={saving}
            >
              {saving ? "Saving…" : "Save lineup"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

// AdminTeamLineupsList: a small selector that picks a team from the
// competition's player list and renders AdminLineup for it. Mounted by
// the "Lineups" sidebar entry in admin_competition.jsx (T136 nav hook).
function AdminTeamLineupsList({ comp, password, showToast }) {
  const teams = (comp?.players || []);
  const [teamId, setTeamId] = useStateA(teams[0] ? teamIdOf(teams[0]) : "");
  const [round, setRound] = useStateA(0);
  const selectedTeam = teams.find(t => teamIdOf(t) === teamId) || teams[0];

  if ((comp?.kind || "") !== "team") {
    return (
      <div className="page" style={{ padding: 24 }}>
        <p style={{ color: "var(--ink-3)", fontStyle: "italic" }}>
          Lineups are only used for team competitions.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div style={{ display: "flex", gap: 12, alignItems: "center", marginBottom: 16, padding: "0 24px", paddingTop: 24 }}>
        <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <span className="overline">Team</span>
          <select
            className="input"
            value={teamId}
            onChange={(e) => setTeamId(e.target.value)}
            style={{ padding: "6px 8px", fontSize: 14, minWidth: 200 }}
          >
            {teams.map(t => (
              <option key={teamIdOf(t)} value={teamIdOf(t)}>{t.name || t.Name}</option>
            ))}
          </select>
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <span className="overline">Round</span>
          <input
            className="input"
            type="number"
            min={1}
            value={round + 1}
            onChange={(e) => {
              const v = parseInt(e.target.value, 10);
              if (Number.isFinite(v) && v >= 1) setRound(v - 1);
            }}
            style={{ padding: "6px 8px", fontSize: 14, width: 80 }}
          />
        </label>
      </div>
      {selectedTeam ? (
        <AdminLineup
          comp={comp}
          team={selectedTeam}
          round={round}
          password={password}
          showToast={showToast}
          // pass a stable key on the inner form so switching teams /
          // rounds remounts the loader cleanly instead of stale state.
          key={`${teamIdOf(selectedTeam)}-${round}`}
        />
      ) : (
        <div className="page" style={{ padding: 24, color: "var(--ink-3)" }}>
          No teams registered yet.
        </div>
      )}
    </div>
  );
}

if (typeof window !== "undefined") {
  window.AdminLineup = AdminLineup;
  window.AdminTeamLineupsList = AdminTeamLineupsList;
  // mp-bkg: expose pure helpers so admin_schedule.jsx can import them
  // via window.AdminLineupHelpers without creating a cross-module import
  // dependency (both files are type="module" but share the window object
  // at runtime in the browser and in the esbuild bundle).
  window.AdminLineupHelpers = { positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf, canRevise };
}

export { AdminLineup, AdminTeamLineupsList, positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf, canRevise };
