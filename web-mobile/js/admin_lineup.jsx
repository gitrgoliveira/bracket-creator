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
import { squadSlotLabel } from './squad_member_label.jsx';
import { rosterWithoutPlacedElsewhere, memberPlacedElsewhere } from './lineup_resolver.jsx';
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

// lineupPositionLabel: the operator-facing name for a position KEY, reusing
// POS_LABELS_5's own labels (Senpo, Jiho, ...) rather than a second copy of
// them, so the two can never drift. A numeric-size key (positionsForSize's
// "1".."N") has no FIK name, so it reads as "Position N": unambiguous on
// its own outside the table context memberIdentityWarning's callers render
// it in.
const POS_LABEL_BY_KEY = POS_LABELS_5.reduce((acc, p) => { acc[p.key] = p.label; return acc; }, {});
function lineupPositionLabel(posKey) {
  return POS_LABEL_BY_KEY[posKey] || `Position ${posKey}`;
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

// positionNumberForKey: the 1-based squad-member index a lineup position KEY
// corresponds to, matching how squad.go seeds a fresh team with one blank
// member per position ("senpo"->1, "jiho"->2, ..., numeric "3"->3). Used by
// resolveMemberIdsForPositions below to find the blank member SEEDED for a
// position, so a typed name can be attached to that member's own number
// rather than minting a new, number-less one. Returns 0 for anything
// unrecognised, which never matches a real squad index (indices start at 1),
// so an unrecognised key safely skips the blank-slot reuse.
function positionNumberForKey(posKey) {
  const n = Number(posKey);
  if (Number.isInteger(n) && n > 0) return n;
  const idx = POS_LABELS_5.findIndex(p => p.key === posKey);
  return idx >= 0 ? idx + 1 : 0;
}

// blankMemberForPosition finds the squad member a name typed or picked into
// `posKey` should be attached to when no EXISTING named member already
// matches it by name: the member currently pinned to this position by id
// (currentIds[posKey]), when that member is still blank, else the member
// SEEDED for this position's index (positionNumberForKey). Returns null
// when neither exists, meaning the caller must mint a new squad member.
//
// Shared by resolveMemberIdsForPositions below (the score sheet's inline
// picker and the match-scoped panel) and commitAdd (the Lineups page's own
// "+ Add new member…" option, bc-dnst): both need to answer the same
// question, "does this slot already have a blank home, or would this
// mint?", and a second, independently-written lookup would risk drifting
// from this one the first time either surface's rule changed alone.
function blankMemberForPosition(squad, posKey, currentIds) {
  const currentSquad = Array.isArray(squad) ? squad : [];
  const ids = currentIds || {};
  const currentMemberId = ids[posKey];
  const pickedBlankMember = currentMemberId
    ? currentSquad.find(mem => mem && mem.id === currentMemberId && !(mem.name || "").trim())
    : null;
  if (pickedBlankMember) return pickedBlankMember;
  // The seeded blank member is only free to take this position's name when
  // it is not already fielded at ANOTHER position (bc-dnst): renaming a
  // member the lineup holds elsewhere would name the wrong row's fighter,
  // and the write that followed would then be refused as a duplicate after
  // the rename had already landed. With its slot taken, the name mints.
  const placedElsewhere = (mem) => Object.entries(ids).some(([key, id]) => key !== posKey && id && id === mem.id);
  const slotNumber = positionNumberForKey(posKey);
  return slotNumber
    ? currentSquad.find(mem => mem && mem.index === slotNumber && !(mem.name || "").trim() && !placedElsewhere(mem)) || null
    : null;
}

// resolveMemberIdsForPositions resolves a WHOLE positions map (posKey →
// name) to its memberIds counterpart against an already-loaded `squad`.
// Shared by BOTH match-scoped lineup writers (admin_schedule_lineup.jsx's
// free-text panel and admin_scoring_team.jsx's inline in-modal picker) so
// the resolve/attach contract lives once.
//
// bc-dnst (operator ruling 2026-09-15): the number shown on a bout row
// belongs to the SQUAD MEMBER, never to the row or the position, because
// team order can change between team matches -- picking a different member
// into a position moves THAT member's number onto the row. A fresh team's
// seeded blank slots are members that already carry a number and no name
// (squad.go), so a name typed into an unnamed fixed-order position FILLS
// that position's blank seeded member: this function renames the blank
// member at the matching index (positionNumberForKey) via
// window.API.renameTeamMember, keeping its id and its number, so the name
// stays attached to the number it was shown with. Minting a brand new
// member is only the FALLBACK for a squad with no blank member at that
// index (e.g. a reserve position beyond TeamSize) -- the same one-step
// create this file's own "+ Add new member…" picker option uses
// (commitAdd above).
//
// Sequential, not parallel: two positions typed with the SAME new name
// must mint (or rename onto) it only once -- the second lookup then finds
// the first write's member in the growing local squad copy instead of
// racing a duplicate add the server would refuse anyway.
//
// A rename/mint failure (offline venue wifi, a typed name that normalises
// onto an existing member, a team no longer on the roster, a stale
// password) NEVER blocks the write: the operator ruling for this bead is
// the write half of the question, and it stays exactly as it was -- that
// position's id is simply omitted from the result, the lineup write still
// proceeds with whatever ids resolved, and the load-time legacy-upgrade
// repair (EnsureLegacyUpgraded) fills the rest in once a participants.csv
// write re-arms it. The WARN half (bc-cse gap closure): a failure is
// reported in `failures` so a caller can tell the operator -- via
// memberIdentityWarning below -- without ever refusing or retrying the
// save on its own account.
//
// currentIds (bc-dnst, optional) is the lineup's OWN memberIds map before
// this write -- buildInlineLineupWrite (admin_scoring_team.jsx) passes
// lineup?.memberIds. It covers naming a slot that was PICKED by number
// rather than typed: an operator who picks a blank squad entry from the
// row's list (LineupNameInput's object-entry shape) writes that member's
// id directly, by-id, without ever calling this resolver (see
// buildInlineLineupWrite's own doc comment) -- so the position already
// carries that member's id when the operator later TYPES a name into the
// now-visible empty box. Without currentIds, a typed name would fall back
// to positionNumberForKey's INDEX default, which is only that position's
// default member and may not be the member actually picked (a reserve, or
// a blank slot moved in from elsewhere via the row's list). When
// currentIds[posKey] names a squad member who is still blank, THAT member
// is renamed instead, through the exact same rename path as the index
// default below -- the number stays attached to the member the operator
// actually chose.
//
// Returns { memberIds, squad, failures }: `squad` is handed back (possibly
// extended by a mint, or with a member renamed) so the caller can cache it
// without a second fetch. `failures` is `[{ position, name, reason }]`, one
// entry per position whose rename/mint failed; `reason` is the server's own
// message (API.renameTeamMember/addTeamMember throw with err.error from the
// response body) -- never a raw Error object or a stack. This function
// never throws.
async function resolveMemberIdsForPositions(compId, teamId, positions, squad, password, currentIds) {
  let currentSquad = Array.isArray(squad) ? squad : [];
  const memberIds = {};
  const failures = [];
  const ids = currentIds || {};
  for (const [posKey, rawName] of Object.entries(positions || {})) {
    const name = (rawName || "").trim();
    if (!name) continue;
    const existing = resolveMemberIdForName(currentSquad, name);
    if (existing) {
      memberIds[posKey] = existing.id;
      continue;
    }
    const blankMember = blankMemberForPosition(currentSquad, posKey, ids);
    if (blankMember) {
      try {
        await window.API.renameTeamMember(compId, teamId, blankMember.id, name, password);
        currentSquad = currentSquad.map(mem => (mem === blankMember ? { ...mem, name } : mem));
        memberIds[posKey] = blankMember.id;
      } catch (e) {
        failures.push({ position: posKey, name, reason: (e && e.message) || "" });
      }
      continue;
    }
    try {
      const member = await window.API.addTeamMember(compId, teamId, name, password);
      currentSquad = [...currentSquad, member];
      memberIds[posKey] = member.id;
    } catch (e) {
      // Minting failed: leave this position's id unresolved (see doc above)
      // and record why.
      failures.push({ position: posKey, name, reason: (e && e.message) || "" });
    }
  }
  return { memberIds, squad: currentSquad, failures };
}

// memberIdentityWarning composes the ONE operator-facing sentence every
// lineup-writing surface shows after a save whose squad-member attachment
// partially or wholly failed (see resolveMemberIdsForPositions above), so
// admin_lineup.jsx, admin_schedule_lineup.jsx and admin_scoring_team.jsx
// never grow three different wordings for the same event. Order is fixed by
// the operator's ruling: the lineup WAS saved, which positions lack an
// identity and why, then that scores are unaffected. Copy rules this repo
// enforces: never "live", no em-dashes, "squad member" not "member id"
// (operator vocabulary, not internal jargon).
//
// squadUnavailable takes priority over `failures` and is checked first: when
// the squad itself could not be loaded this session, EVERY name in the
// lineup looked new to the resolver, so a per-position list would just be
// the same root cause repeated once per slot -- misleading, since it reads
// as N unrelated problems instead of the one real one. One sentence naming
// the actual cause replaces the whole list in that case.
//
// Returns "" when there is nothing to say.
function memberIdentityWarning(failures, squadUnavailable) {
  if (squadUnavailable) {
    return "Lineup saved, but the squad list could not be loaded, so no position could be linked to a squad member this time. Scores will still record normally.";
  }
  const list = (Array.isArray(failures) ? failures : []).filter(f => f && f.position);
  if (list.length === 0) return "";
  const parts = list.map(f => {
    const label = lineupPositionLabel(f.position);
    const who = f.name ? `${label} (${f.name})` : label;
    return f.reason
      ? `${who} could not be linked to a squad member (${f.reason})`
      : `${who} could not be linked to a squad member`;
  });
  return `Lineup saved, but ${parts.join(". ")}. Scores will still record normally.`;
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
  // bc-cse gap closure: did the team's squad fail to load this session? Fed
  // into memberIdentityWarning below, alongside a save's own per-position
  // `failures`, so a squad fetch failure -- which otherwise silently leaves
  // every existing member looking "new" -- is disclosed rather than
  // discarded. A save is never blocked on it.
  const [squadUnavailable, setSquadUnavailable] = useStateA(false);
  // The composed warning shown after a SUCCESSFUL save (see save() below).
  // Deliberately a separate channel from `error`: the save did not fail,
  // so it must never read like the red error banner above.
  const [saveWarning, setSaveWarning] = useStateA("");

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

  // Load the existing lineup: positions/memberIds. 404 -> fresh form
  // (server contract, unchanged).
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
          const nextValues = {};
          const nextIds = {};
          positions.forEach(p => {
            nextValues[p.key] = (lineup.positions || {})[p.key] || "";
            nextIds[p.key] = (lineup.memberIds || {})[p.key] || "";
          });
          setValues(nextValues);
          setMemberIds(nextIds);
        }
      } catch (e) {
        if (!cancelled) setError(e?.message || "Failed to load lineup");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId, round]);

  // Load the team's squad: the pickable member list operation 1 (SELECT)
  // needs. Independent of the lineup load above -- a squad fetch failure
  // must never block the lineup from loading or the form from being usable
  // (bc-pnum gap closure); an empty/missing squad is simply []. It DOES mean
  // the picker shows no existing members this session, so anything typed
  // through "+ Add new member..." looks new to the resolver even when it
  // is not; squadUnavailable carries that fact to memberIdentityWarning.
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

  // Operation 2 (ADD): resolve the typed name through the SAME rule the
  // score sheet's inline picker uses (resolveMemberIdsForPositions, bc-dnst),
  // so naming a position whose seeded slot is still blank RENAMES that
  // blank member instead of minting a new one -- the score sheet and the
  // Lineups page must agree on when a name mints a new squad position and
  // when it merely fills one that already exists. Minting still happens,
  // through the very same resolver, whenever the position has no blank
  // slot to take the name.
  //
  // bc-pnum: confirm first. Creating a squad member is rare and deliberate,
  // so a confirmation here is cheap and is the last guard against a typo
  // becoming a permanent squad position; the copy names which of the two
  // outcomes will happen (blankMemberForPosition decides which, using the
  // same lookup the resolver itself uses) so the operator is never
  // surprised by which one occurred. Declining leaves the add-row open
  // with the typed name intact so the operator can correct it rather than
  // restarting the picker.
  const commitAdd = async () => {
    const posKey = addingPos;
    const name = addingName.trim();
    if (!posKey || !name) { setAddingPos(null); setAddingName(""); return; }
    // The three outcomes the resolver itself has, in ITS order, so the copy
    // can never describe a branch the action will not take. First: the name
    // is an EXISTING member's (same normalisation as the resolver): nothing
    // is created or renamed, that member is simply selected here, with no
    // confirmation because nothing permanent happens; unless the lineup
    // already fields them elsewhere, which is refused where the list would
    // not have offered them.
    const existingMember = resolveMemberIdForName(squad, name);
    if (existingMember) {
      const elsewhere = memberPlacedElsewhere(memberIds, posKey, existingMember.id);
      if (elsewhere) {
        setError(`${existingMember.name} is already at ${lineupPositionLabel(elsewhere)}.`);
        return;
      }
      selectMember(posKey, existingMember);
      setAddingPos(null);
      setAddingName("");
      return;
    }
    const blankMember = blankMemberForPosition(squad, posKey, memberIds);
    const message = blankMember
      ? `Name ${squadSlotLabel(teamNumber, blankMember.index)} as "${name}"?`
      : `Add "${name}" as a new squad member of ${team?.name || team?.Name || "this team"}? This adds a new position to the squad. Once added, it can be cleared but never removed.`;
    const ok = await window.confirmDialog({
      message,
      confirmLabel: blankMember ? "Name slot" : "Add member",
      cancelLabel: "Cancel",
    });
    if (!ok) return;
    setAddBusy(true);
    setError("");
    try {
      const resolved = await resolveMemberIdsForPositions(compId, teamId, { [posKey]: name }, squad, password, memberIds);
      const failure = (resolved.failures || []).find(f => f.position === posKey);
      if (failure) {
        setError(failure.reason || "Failed to add team member");
        return;
      }
      const resolvedId = resolved.memberIds[posKey];
      const resolvedMember = resolved.squad.find(m => m.id === resolvedId);
      setSquad(resolved.squad);
      if (resolvedMember) selectMember(posKey, resolvedMember);
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
    setSaveWarning("");
    setSaving(true);
    try {
      // Strip vacant positions before sending: an omitted key reads as
      // "vacant" the same way an explicit empty string would (the server's
      // ValidatePositions only checks that submitted KEYS are valid for the
      // team size, not whether values are filled), so this is a storage-
      // hygiene choice, an omitted key, not a stored empty string. A
      // position holding a picked squad slot with no name yet is NOT vacant
      // (bc-dnst): the id is the placement, so it is written with an empty
      // name, exactly as the match panel and the score sheet write it.
      const positionsOut = {};
      const memberIdsOut = {};
      Object.entries(values).forEach(([k, v]) => {
        // Trim here too (not just on commit), so a Save never persists
        // leading/trailing or whitespace-only names.
        const trimmed = (v || "").trim();
        if (trimmed || memberIds[k]) {
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
      // bc-cse gap closure: this surface's own SELECT/ADD/RENAME operations
      // already surface a mint/rename failure immediately (see commitAdd's
      // and commitRename's own `error` handling above); what a save here
      // cannot see is a squad that failed to load THIS session, which is
      // why memberIdentityWarning is fed `failures: []` unconditionally and
      // `squadUnavailable` is the one signal that varies.
      setSaveWarning(memberIdentityWarning([], squadUnavailable));
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

      {/* Non-blocking: the save above already succeeded. This rides the
          shared amber .alert--warn treatment so it can never be mistaken
          for the red error banner above -- the lineup saved fine, only its
          squad-member identity attachment fell short. */}
      {saveWarning && (
        <div className="alert alert--warn" role="status" data-testid="lineup-member-warning" style={{ marginBottom: 12 }}>
          {saveWarning}
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
                    {/* A member placed at another position is not offered
                        again (the shared rule, rosterWithoutPlacedElsewhere):
                        the server refuses one member at two positions, and
                        that refusal must never be the first the operator
                        hears of it. */}
                    {rosterWithoutPlacedElsewhere(squadSorted, { positions: values, memberIds }, p.key).map(m => (
                      <option key={m.id} value={m.id}>
                        {[squadSlotLabel(teamNumber, m.index), m.name].filter(Boolean).join(" ")}
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
                        {squadSlotLabel(teamNumber, m.index)}
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
    resolveMemberIdForName, resolveMemberIdsForPositions, memberIdentityWarning,
    lineupPositionLabel, blankMemberForPosition,
  };
}

export {
  AdminLineup, AdminTeamLineupsList, positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf,
  lineupPositionLabel, blankMemberForPosition,
  resolveMemberIdForName, resolveMemberIdsForPositions, memberIdentityWarning,
};
