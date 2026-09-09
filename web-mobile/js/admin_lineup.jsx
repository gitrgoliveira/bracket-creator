// Per-team lineup form for FR-040 (T129/T130).
//
// Teams pick which player occupies each named position (Senpo, Jiho,
// Chuken, Fukusho, Taisho for 5-person teams; numeric "1"..."N" for
// other sizes). Lineups are always editable; operators can change them
// at any time before or during a match.
//
// Wire shape (matches domain.TeamLineup):
//   {
//     teamId: "team-1",
//     competitionId: "...",
//     round: 0,
//     positions: { senpo: "Sato", ... },
//     memberIds: { senpo: "member-uuid", ... }
//   }
//
// bc-tmid pass 3: a position now carries the squad MEMBER's stable id
// (memberIds) alongside the display NAME (positions) it always carried.
// The squad itself, the actual people on the team, is no longer read off
// team.metadata (the untyped array a team's roster row shares with an
// individual's dan grade, and the confirmed source of several data-loss
// bugs, see bc-tmid). It is loaded from its own per-competition store via
// GET /api/competitions/:id/squads, and edited through exactly THREE
// operations, per the operator's ruling: SELECT an existing squad member
// into a position, ADD a new name in a position (which creates the member
// and mints its id in that one step), and RENAME a member (which keeps
// their id). There is no member-removal operation.
//
// bc-pnum gap closure: the OTHER two lineup-writing surfaces --
// admin_schedule_lineup.jsx's free-text match-scoped panel and
// admin_scoring_team.jsx's inline in-modal picker -- carry no select-by-id
// affordance of their own; an operator there types or picks a bare NAME.
// resolveMemberIdForName / resolveMemberIdsForPositions below are the ONE
// place that turns such a name into a squad member id (matching an
// existing member, or minting one via the SAME ADD operation this file's
// "+ Add new member…" option uses), so both surfaces share the resolve/mint
// contract instead of each growing their own.

import { idOf, nameOf } from './competitor_identity.jsx';
import { squadMemberLabel } from './squad_member_label.jsx';
import { normalizeParticipantName } from './data.jsx';

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

// Pull the member roster off the team Player object. Retained for the
// OTHER lineup surface (admin_schedule_lineup.jsx's match-scoped panel,
// reached via window.AdminLineupHelpers) which has not moved off
// team.metadata; the round-scoped editor below (AdminLineup) no longer
// calls this itself, it reads the squad store instead (see module header).
// The CSV parser stores member names in Metadata; fall back to the team
// name itself so callers that DO use this never see an empty array crash.
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
//
// Retained for admin_schedule_lineup.jsx (see rosterFor's own comment); the
// round-scoped AdminLineup below no longer calls it.
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
// to name as a best-effort key. idOf/nameOf (competitor_identity.jsx) read
// the lowercase id/name; the interleaved ID/Name fallback is a legacy/
// malformed-shape defense whose PRECEDENCE (id, ID, name, Name -- pinned by
// a dedicated test) sideLookupKey's simple id-else-name shape can't
// reproduce, so this stays a local four-way chain rather than a single call.
function teamIdOf(team) {
  return idOf(team) || team?.ID || nameOf(team) || team?.Name || "";
}

// squadMemberOptions returns a team's squad sorted by display Index, the
// order the position pickers and the rename panel both list members in.
// Pure and exported so the ordering can be pinned without mounting the
// component.
function squadMemberOptions(squad) {
  return (Array.isArray(squad) ? squad : []).slice().sort((a, b) => (a.index || 0) - (b.index || 0));
}

// resolveMemberIdForName finds the squad member whose name matches `name`
// under the SAME normalization the server's duplicate-name floor
// (bc-tmdup, helper.DuplicateNamesWithKeys / NormalizeParticipantName) uses
// to enforce squad-wide uniqueness: case/diacritic-insensitive, trimmed,
// whitespace-collapsed. Because that normalization is exactly what refuses
// a second member with a colliding name, at most one squad member can ever
// match a given key -- this needs no ambiguity guard, unlike a bare
// substring search. Returns the member object, or null when nothing matches.
function resolveMemberIdForName(squad, name) {
  const key = normalizeParticipantName(name);
  if (!key) return null;
  const list = Array.isArray(squad) ? squad : [];
  return list.find(m => normalizeParticipantName(m?.name) === key) || null;
}

// resolveMemberIdsForPositions resolves a WHOLE positions map (posKey →
// name) to its memberIds counterpart against an already-loaded `squad`,
// MINTING a new member for any name with no match. That much IS the
// operator's ruling for this bead: typing a new name into a lineup slot
// creates the member and its id in that one step, the same as this file's own
// "+ Add new member…" picker option (commitAdd above). Shared by BOTH
// match-scoped lineup writers (admin_schedule_lineup.jsx's free-text panel
// and admin_scoring_team.jsx's inline in-modal picker) so the resolve/mint
// contract lives once.
//
// Sequential, not parallel: two positions typed with the SAME new name
// must mint it only once -- the second lookup then finds the first mint's
// member in the growing local squad copy instead of racing a duplicate add
// the server would refuse anyway.
//
// A mint failure (offline venue wifi, exactly the condition these panels
// are used under) is swallowed, NOT surfaced. That part is an engineering
// call, not something the operator ruled on, so it is open to revisiting:
// the reasoning is that a lineup slot's NAME is the load-bearing half and
// the id is an enhancement over it, so losing an operator's edit to a
// failed identity lookup trades the important half away for the lesser one.
// That position's id is simply omitted from the result -- the lineup write
// still proceeds with whatever ids resolved, and
// the load-time legacy-upgrade repair (EnsureLegacyUpgraded) fills the rest
// in once a participants.csv write re-arms it.
//
// Returns { memberIds, squad }: `squad` is handed back (possibly extended
// by a mint) so the caller can cache it without a second fetch.
async function resolveMemberIdsForPositions(compId, teamId, positions, squad, password) {
  let currentSquad = Array.isArray(squad) ? squad : [];
  const memberIds = {};
  for (const [posKey, rawName] of Object.entries(positions || {})) {
    const name = (rawName || "").trim();
    if (!name) continue;
    const existing = resolveMemberIdForName(currentSquad, name);
    if (existing) {
      memberIds[posKey] = existing.id;
      continue;
    }
    try {
      const member = await window.API.addTeamMember(compId, teamId, name, password);
      currentSquad = [...currentSquad, member];
      memberIds[posKey] = member.id;
    } catch (_e) {
      // Minting failed: leave this position's id unresolved (see doc above).
    }
  }
  return { memberIds, squad: currentSquad };
}

function AdminLineup({ comp, team, round, password, showToast, onClose }) {
  const teamSize = comp?.teamSize || 5;
  const positions = useMemoA(() => positionsForSize(teamSize), [teamSize]);
  const teamId = teamIdOf(team);
  const compId = comp?.id || "";
  // The team's OWN competitor number (e.g. "T10"), the input to
  // squadMemberLabel. Not the member's: a squad member has no number of
  // its own, only an index, and the label composes the two together.
  const teamNumber = team?.number || team?.Number || "";

  // Position state mirrors domain.TeamLineup: display names in `values`,
  // squad member ids in `memberIds`, keyed by the SAME position key so a
  // partial edit to one position never disturbs another's already-resolved
  // id (bc-tmid pass 3's OrderedMembers reasoning, restated client-side).
  const [values, setValues] = useStateA(() => {
    const init = {};
    positions.forEach(p => { init[p.key] = ""; });
    return init;
  });
  const [memberIds, setMemberIds] = useStateA({});
  // The team's squad (the actual people on it), loaded from its own store
  // rather than team.metadata (module header). Independent of teamSize: a
  // squad may hold reserves beyond however many positions exist.
  const [squad, setSquad] = useStateA([]);
  const [loading, setLoading] = useStateA(true);
  const [saving, setSaving] = useStateA(false);
  const [error, setError] = useStateA("");

  // Operation 2 (ADD): which position is mid-add, and the name typed so
  // far. Only one position can be mid-add at a time, an operator works one
  // slot at a time, so a single pair of fields is enough.
  const [addingPos, setAddingPos] = useStateA(null);
  const [addingName, setAddingName] = useStateA("");
  const [addBusy, setAddBusy] = useStateA(false);

  // Operation 3 (RENAME): which squad member (by id) is being renamed.
  const [renamingId, setRenamingId] = useStateA(null);
  const [renamingName, setRenamingName] = useStateA("");
  const [renameBusy, setRenameBusy] = useStateA(false);

  // Load the existing lineup AND the team's squad together: the lineup
  // supplies the current positions/memberIds, the squad supplies the
  // pickable member list operation 1 (SELECT) needs. 404 on the lineup ->
  // fresh form (server contract, unchanged); the squad fetch has no such
  // fallback, an empty/missing squad is simply [].
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId) {
      setLoading(false);
      return;
    }
    (async () => {
      try {
        const [lineup, squads] = await Promise.all([
          window.API.fetchTeamLineup(compId, teamId, round),
          window.API.fetchSquads(compId, password),
        ]);
        if (cancelled) return;
        if (lineup) {
          const nextValues = {};
          const nextIds = {};
          positions.forEach(p => {
            nextValues[p.key] = (lineup.positions || {})[p.key] || "";
            nextIds[p.key] = (lineup.memberIds || {})[p.key] || "";
          });
          setValues(nextValues);
          setMemberIds(nextIds);
        }
        setSquad((squads && squads[teamId]) || []);
      } catch (e) {
        if (!cancelled) setError(e?.message || "Failed to load lineup");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId, round]);

  const squadSorted = useMemoA(() => squadMemberOptions(squad), [squad]);

  // Operation 1 (SELECT): put an existing squad member's (name, id) pair
  // into a position, both keyed together so they can never drift apart.
  const selectMember = (posKey, member) => {
    setValues(v => ({ ...v, [posKey]: member.name }));
    setMemberIds(ids => ({ ...ids, [posKey]: member.id }));
  };

  const clearPosition = (posKey) => {
    setValues(v => ({ ...v, [posKey]: "" }));
    setMemberIds(ids => ({ ...ids, [posKey]: "" }));
  };

  const onPickerChange = (posKey, rawValue) => {
    if (rawValue === "__add__") {
      setAddingPos(posKey);
      setAddingName("");
      setError("");
      return;
    }
    if (rawValue === "") {
      clearPosition(posKey);
      return;
    }
    const member = squadSorted.find(m => m.id === rawValue);
    if (member) selectMember(posKey, member);
  };

  // Operation 2 (ADD): mint the member server-side, then place it, in one
  // round trip's worth of user action. commitAdd is the only place that
  // calls addTeamMember: a new name is never stored as a bare string.
  const commitAdd = async () => {
    const posKey = addingPos;
    const name = addingName.trim();
    if (!posKey || !name) { setAddingPos(null); setAddingName(""); return; }
    setAddBusy(true);
    setError("");
    try {
      const member = await window.API.addTeamMember(compId, teamId, name, password);
      setSquad(s => [...s, member]);
      selectMember(posKey, member);
      setAddingPos(null);
      setAddingName("");
    } catch (e) {
      setError(e?.message || "Failed to add team member");
    } finally {
      setAddBusy(false);
    }
  };

  const cancelAdd = () => { setAddingPos(null); setAddingName(""); setError(""); };

  const startRename = (member) => {
    setRenamingId(member.id);
    setRenamingName(member.name);
    setError("");
  };

  // Operation 3 (RENAME): keeps the member's id; only the display name
  // changes. Every position currently pointing at this id is updated
  // locally too, so the form reflects the new name immediately rather
  // than waiting for a reload.
  const commitRename = async () => {
    const id = renamingId;
    const name = renamingName.trim();
    if (!id || !name) { setRenamingId(null); setRenamingName(""); return; }
    setRenameBusy(true);
    setError("");
    try {
      await window.API.renameTeamMember(compId, teamId, id, name, password);
      setSquad(s => s.map(m => (m.id === id ? { ...m, name } : m)));
      setValues(v => {
        const next = { ...v };
        Object.keys(memberIds).forEach(posKey => {
          if (memberIds[posKey] === id) next[posKey] = name;
        });
        return next;
      });
      setRenamingId(null);
      setRenamingName("");
    } catch (e) {
      setError(e?.message || "Failed to rename team member");
    } finally {
      setRenameBusy(false);
    }
  };

  const cancelRename = () => { setRenamingId(null); setRenamingName(""); setError(""); };

  const save = async () => {
    setError("");
    setSaving(true);
    try {
      // Strip empty positions before sending: an omitted key reads as
      // "vacant" the same way an explicit empty string would (the server's
      // ValidatePositions only checks that submitted KEYS are valid for the
      // team size, not whether values are filled), so this is a storage-
      // hygiene choice, an omitted key, not a stored empty string.
      const positionsOut = {};
      const memberIdsOut = {};
      Object.entries(values).forEach(([k, v]) => {
        // Trim here too (not just on commit), so a Save never persists
        // leading/trailing or whitespace-only names.
        const trimmed = (v || "").trim();
        if (trimmed) {
          positionsOut[k] = trimmed;
          if (memberIds[k]) memberIdsOut[k] = memberIds[k];
        }
      });
      const hasMemberIds = Object.keys(memberIdsOut).length > 0;
      const updated = await window.API.putTeamLineup(
        compId, teamId, round, positionsOut, password, hasMemberIds ? memberIdsOut : undefined
      );
      // F5: a queued (offline/transient) write is NOT a confirmed save: don't
      // clear the revising state or show "saved"; the write is durable and will
      // retry. Keep the form editable and tell the operator it's pending.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast("Offline: lineup not saved yet, will retry");
        return;
      }
      if (typeof showToast === "function") showToast("Lineup saved");
    } catch (e) {
      setError(e?.message || "Failed to save lineup");
    } finally {
      setSaving(false);
    }
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
            {team?.name || team?.Name || "Team"}: Lineup
          </h2>
          <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>
            {teamSize}-person team
            {comp?.teamMatchType === "kachinuki" && <span style={{ marginLeft: 8, color: "var(--accent)", fontWeight: 700 }}>· <TermAL name="kachinuki">Kachinuki</TermAL> (winner stays on)</span>}
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
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
          {positions.map(p => {
            const memberId = memberIds[p.key] || "";
            const name = values[p.key] || "";
            const isAdding = addingPos === p.key;
            return (
              <label key={p.key} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                <span style={{ fontSize: 12, fontWeight: 600, color: "var(--ink-2)" }}>
                  {p.termId
                    ? <TermAL name={p.termId}>{p.label}</TermAL>
                    : p.label}
                </span>
                {!isAdding ? (
                  <select
                    className="input"
                    data-testid={`lineup-position-${p.key}`}
                    aria-label={`${p.label} player`}
                    disabled={saving}
                    value={memberId}
                    onChange={(e) => onPickerChange(p.key, e.target.value)}
                  >
                    <option value="">
                      {name && !memberId ? `Unresolved: ${name}` : "— none —"}
                    </option>
                    {squadSorted.map(m => (
                      <option key={m.id} value={m.id}>
                        {[squadMemberLabel(teamNumber, m.index), m.name].filter(Boolean).join(" ")}
                      </option>
                    ))}
                    <option value="__add__">+ Add new member…</option>
                  </select>
                ) : (
                  <div style={{ display: "flex", gap: 6 }}>
                    <input
                      className="input"
                      aria-label={`New member name for ${p.label}`}
                      value={addingName}
                      disabled={addBusy}
                      onChange={(e) => setAddingName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") { e.preventDefault(); commitAdd(); }
                        else if (e.key === "Escape") { e.preventDefault(); cancelAdd(); }
                      }}
                    />
                    <button type="button" className="btn btn--sm" onClick={commitAdd} disabled={addBusy || !addingName.trim()}>
                      {addBusy ? "Adding…" : "Add"}
                    </button>
                    <button type="button" className="btn btn--ghost btn--sm" onClick={cancelAdd} disabled={addBusy}>Cancel</button>
                  </div>
                )}
              </label>
            );
          })}
          {squadSorted.length === 0 && (
            <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
              This team has no squad members yet: choose "+ Add new member…" in a position above to add the first one.
            </div>
          )}
        </div>

        {squadSorted.length > 0 && (
          <div style={{ marginTop: 20, paddingTop: 16, borderTop: "1px solid var(--line, #ddd)" }}>
            <div className="overline" style={{ marginBottom: 8 }}>Squad</div>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {squadSorted.map(m => (
                <div key={m.id} data-testid={`squad-member-${m.id}`} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  {renamingId === m.id ? (
                    <>
                      <input
                        className="input"
                        style={{ flex: 1 }}
                        aria-label={`Rename ${m.name}`}
                        value={renamingName}
                        disabled={renameBusy}
                        onChange={(e) => setRenamingName(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") { e.preventDefault(); commitRename(); }
                          else if (e.key === "Escape") { e.preventDefault(); cancelRename(); }
                        }}
                      />
                      <button type="button" className="btn btn--sm" onClick={commitRename} disabled={renameBusy || !renamingName.trim()}>
                        {renameBusy ? "Saving…" : "Save"}
                      </button>
                      <button type="button" className="btn btn--ghost btn--sm" onClick={cancelRename} disabled={renameBusy}>Cancel</button>
                    </>
                  ) : (
                    <>
                      <span style={{ fontSize: 13, color: "var(--ink-3)", minWidth: 44 }}>
                        {squadMemberLabel(teamNumber, m.index)}
                      </span>
                      <span style={{ flex: 1 }}>{m.name}</span>
                      <button type="button" className="btn btn--ghost btn--sm" onClick={() => startRename(m)}>Rename</button>
                    </>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 20 }}>
          <button type="button"
            className="btn btn--primary"
            onClick={save}
            disabled={saving}
          >
            {saving ? "Saving…" : "Save lineup"}
          </button>
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
  window.AdminLineupHelpers = {
    positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf,
    resolveMemberIdForName, resolveMemberIdsForPositions,
  };
}

export {
  AdminLineup, AdminTeamLineupsList, positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf,
  squadMemberOptions, resolveMemberIdForName, resolveMemberIdsForPositions,
};
