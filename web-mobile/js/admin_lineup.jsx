// Per-team lineup form for FR-040 (T129/T130).
//
// Teams pick which player occupies each named position (Senpo, Jiho,
// Chuken, Fukusho, Taisho for 5-person teams; numeric "1"..."N" for
// other sizes). Lineups are always editable; operators can change them
// at any time before or during a match.
//
// The page edits one lineup at a time (operator ruling 2026-10-05: a team
// carries the lineup of its previous team match unless one is entered for a
// match): the team's STARTING lineup, stored as its round-0 entry, or the
// lineup of one of its team matches. A match shows the lineup in force there
// and says where it was saved (lineup_resolver.jsx).
//
// Wire shape (matches domain.TeamLineup; a match's own lineup carries
// `matchId` where this one has `round`):
//   {
//     teamId: "team-1",
//     competitionId: "...",
//     round: 0,
//     positions: { senpo: "Sato", ... },
//     memberIds: { senpo: "member-uuid", ... }
//   }
// A save does not send the lineup: it names the positions it changed
// (`changed`, with those positions' names and ids), and the server puts them on
// the lineup it holds (lineup_save.jsx, operator decision 2026-10-07).
//
// bc-tmid pass 3: a position now carries the squad MEMBER's stable id
// (memberIds) alongside the display NAME (positions) it always carried.
// The squad itself, the actual people on the team, is no longer read off
// team.metadata (the untyped array a team's roster row shares with an
// individual's dan grade, and the confirmed source of several data-loss
// bugs, see bc-tmid). It is loaded from its own per-competition store via
// GET /api/competitions/:id/team-members, and edited through exactly THREE
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
import { rosterWithoutPlacedElsewhere, memberPlacedElsewhere, memberRefusalNote, lineupDuplicateNote, alreadyPlacedNote, lineupPositionLabel, STARTING_ROUND } from './lineup_resolver.jsx';
import { poolMatchNumberOf, isSupplementaryBout, scoreRowMatchLabel } from './pool_ids.jsx';
import { normalizeParticipantName } from './data.jsx';
import { renameMemberFields } from './lineup_rename.jsx';
import { changedLineupSave } from './lineup_save.jsx';
import { queuedNotice } from './write_result.jsx';
import { useLineupForm, LineupSourceLine, LineupProblem, LineupDraftNotice } from './lineup_draft.jsx';

const { useState: useStateA, useMemo: useMemoA } = React;

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
// team.metadata; the Lineups page (AdminLineup, below) no longer
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
// Lineups page (AdminLineup, below) no longer calls it.
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

// teamServerIdOf is teamIdOf WITHOUT the name arm, for the one thing teamIdOf
// must never be used for here: addressing the server. This page mints members,
// renames them and saves lineups, all keyed by team id. A name sent as an id is
// not refused everywhere -- addTeamMember 404s, but SetTeamLineup does not check
// the roster at all, so the lineup persists under a key no id-keyed reader ever
// finds. The deleted Settings section carried this same split deliberately and
// rendered the id-less team an explanation instead; that guard came back with
// its section removed (bc-dnst). teamIdOf keeps its name arm for the LOCAL uses
// (the select's value, the remount key) where no server call is behind it.
function teamServerIdOf(team) {
  return idOf(team) || team?.ID || "";
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
  // ASKED of the owner rather than re-derived here: this body was a
  // byte-for-byte copy of memberPlacedElsewhere minus its return value, and
  // the picker filter in that same owner was rewritten this round to ask
  // rather than copy for exactly this reason. Agreement now holds by
  // construction on all three of the offer, the refusal and this rename.
  const placedElsewhere = (mem) => !!memberPlacedElsewhere(ids, posKey, mem.id);
  const slotNumber = positionNumberForKey(posKey);
  return slotNumber
    ? currentSquad.find(mem => mem && mem.index === slotNumber && !(mem.name || "").trim() && !placedElsewhere(mem)) || null
    : null;
}

// typedNameTarget: what a name typed at a position goes on, in the ONE order every
// surface that places a typed name goes by (the resolver below, the add on the Lineups
// page, and the at-court panel's check before a Save, which asks it of a copy of the
// members and writes nothing): the member the name already belongs to (`write` is
// "none"), else the unnamed member it names in place, which is the one the position
// holds, else the position's seeded slot while that slot is free (`write` is "rename"),
// else a member that does not exist yet (`write` is "add", `member` null). `ids` is the
// lineup's member ids by position.
function typedNameTarget(members, posKey, name, ids) {
  const existing = resolveMemberIdForName(members, name);
  if (existing) return { member: existing, write: "none" };
  const blank = blankMemberForPosition(members, posKey, ids);
  return blank ? { member: blank, write: "rename" } : { member: null, write: "add" };
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
// this write -- buildInlineLineupWrite (lineup_resolver.jsx) passes
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
// without a second fetch. `failures` is `[{ position, name, reason, code }]`,
// one entry per position whose rename/mint failed; `reason` is the refusal's
// own words and `code` its code, when it carries one (API.renameTeamMember/
// addTeamMember throw them, api_client.jsx _refusalError) -- never a raw
// Error object or a stack. This function never throws.
async function resolveMemberIdsForPositions(compId, teamId, positions, squad, password, currentIds) {
  let currentSquad = Array.isArray(squad) ? squad : [];
  const memberIds = {};
  const failures = [];
  const ids = currentIds || {};
  for (const [posKey, rawName] of Object.entries(positions || {})) {
    const name = (rawName || "").trim();
    if (!name) continue;
    const { member: target, write } = typedNameTarget(currentSquad, posKey, name, ids);
    if (write === "none") {
      memberIds[posKey] = target.id;
      continue;
    }
    if (write === "rename") {
      try {
        // The member as the server answered it, stamped: what the lists merge by. A copy
        // built from the name typed carries no stamp and would lose to every list.
        const renamed = await window.API.renameTeamMember(compId, teamId, target.id, name, password);
        currentSquad = currentSquad.map(mem => (mem === target ? renamed : mem));
        memberIds[posKey] = target.id;
      } catch (e) {
        failures.push(memberWriteFailure(posKey, name, e));
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
      failures.push(memberWriteFailure(posKey, name, e));
    }
  }
  return { memberIds, squad: currentSquad, failures };
}

// memberWriteFailure: one resolver failure, in the shape documented above;
// `code` only when the refusal carried one.
function memberWriteFailure(position, name, e) {
  const failure = { position, name, reason: (e && e.message) || "" };
  if (e && e.code) failure.code = e.code;
  return failure;
}

// memberIdentityWarning composes the ONE operator-facing sentence every
// lineup-writing surface shows after a save whose squad-member attachment
// partially or wholly failed (see resolveMemberIdsForPositions above), so
// admin_lineup.jsx, admin_schedule_lineup.jsx and admin_scoring_team.jsx
// never grow three different wordings for the same event. Order is fixed by
// the operator's ruling: the lineup WAS saved, which positions lack an
// identity and why, then that scores are unaffected. Copy rules this repo
// enforces: never "live", no em-dashes, and "team member" -- never "squad",
// which survives only as a code name (operator ruling, bc-dnst), and never
// "member id", which is internal jargon.
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
    return "Lineup saved, but the team member list could not be loaded, so no position could be linked to a team member this time. Scores will still record normally.";
  }
  const list = (Array.isArray(failures) ? failures : []).filter(f => f && f.position);
  if (list.length === 0) return "";
  // Each part is whole sentences, so a refusal's own sentence (memberRefusalNote)
  // follows its position as it is, not in brackets.
  const parts = list.map(f => {
    const label = lineupPositionLabel(f.position);
    const who = f.name ? `${label} (${f.name})` : label;
    const note = memberRefusalNote(f, "");
    if (note) return `${who} could not be linked to a team member. ${note}`;
    return f.reason
      ? `${who} could not be linked to a team member (${f.reason}).`
      : `${who} could not be linked to a team member.`;
  });
  return `Lineup saved, but ${parts.join(" ")} Scores will still record normally.`;
}

// AdminLineup edits one lineup of one team: its starting lineup (no matchId),
// or the lineup of the team match matchId names. matchLabel is how that match
// is named, and allMatches names the earlier match a carried lineup comes from.
function AdminLineup({ comp, team, matchId = "", matchLabel = "", notInMatch = false, allMatches, password, showToast, onClose }) {
  const teamSize = comp?.teamSize || 5;
  const positions = useMemoA(() => positionsForSize(teamSize), [teamSize]);
  const positionKeys = positions.map(p => p.key);
  const teamId = teamServerIdOf(team);
  const compId = comp?.id || "";
  // The team's OWN competitor number (e.g. "T10"), the input to
  // squadMemberLabel. Not the member's: a squad member has no number of
  // its own, only an index, and the label composes the two together.
  const teamNumber = team?.number || team?.Number || "";
  // Mirrors admin_competition_settings.jsx's own isStarted derivation (the
  // removed Squad members section used the same check): once the
  // competition has left setup/draw-ready the server refuses a clear with
  // a 409 (state.ErrTeamMemberClearAfterStart), so the button below is
  // disabled here too rather than surfacing that refusal as an error banner.
  const started = !!(comp?.status && comp.status !== "setup" && comp.status !== "draw-ready");

  // Position state mirrors domain.TeamLineup: display names in `values`,
  // squad member ids in `memberIds`, keyed by the SAME position key so a
  // partial edit to one position never disturbs another's already-resolved
  // id (bc-tmid pass 3's OrderedMembers reasoning, restated client-side).
  // They are useLineupForm's, with the lineup as it was read, where it was
  // saved, the unsaved-picks draft and giving a match's own lineup up: shared
  // with the at-court panel. `saveWarning` is the composed warning shown after
  // a SUCCESSFUL save (see save() below): deliberately a separate channel from
  // `error`, since the save did not fail, so it must never read like the red
  // error banner.
  const form = useLineupForm({
    compId, teamId, matchId, positionKeys, password, matchLabel, teamName: team?.name || team?.Name,
  });
  // The team's squad (the actual people on it), read from its own store rather than
  // team.metadata (module header), by the hook: it reads them again when it follows
  // a lineup another device saved, so a member created there is not shown as an
  // empty slot. Independent of teamSize: a squad may hold reserves beyond however
  // many positions exist. bc-cse gap closure: `squadUnavailable` is whether the
  // squad failed to load this session. Fed into memberIdentityWarning below,
  // alongside a save's own per-position `failures`, so a squad fetch failure --
  // which otherwise silently leaves every existing member looking "new" -- is
  // disclosed rather than discarded. A save is never blocked on it.
  const {
    values, setValues, memberIds, setMemberIds, memberIdsRef,
    error, setError, warning: saveWarning, setWarning: setSaveWarning,
    squad, changeMembers, squadUnavailable,
  } = form;
  const [saving, setSaving] = useStateA(false);
  // A removal of the match's own lineup is in flight.
  const busy = saving || form.removing;
  // The pickers are for a lineup that was read: until then Save is off too, and
  // the problem line says why. A match the team is no longer seated in
  // (`notInMatch`) has no lineup to edit, and the notice below says so.
  const locked = busy || !form.read || notInMatch;
  const canSave = form.canSave && !notInMatch;

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

  // CLEAR: which squad member (by id), if any, has a clear in flight. This
  // page is now the one home for a team's people (bc-dnst, operator
  // decision 2026-09-15): "Clear name" used to live on the competition
  // Settings page's now-removed Squad members section, and moved here
  // alongside Rename rather than being duplicated on both.
  const [clearingId, setClearingId] = useStateA(null);

  // A change to a team member is out. The save waits for it, and while a save (or a
  // removal) is out the controls that make one are off: a rename made then would be
  // written over by the save, and an add finished then would not be placed.
  const memberBusy = addBusy || renameBusy || clearingId !== null;

  const squadSorted = useMemoA(() => squadMemberOptions(squad), [squad]);

  // rostersByPosition (bc-rvfx): the per-position "who's left to pick" list,
  // precomputed once per render of the dependencies it actually reads rather
  // than once PER POSITION inside the JSX below. Each position's own filter
  // legitimately excludes a different slot (itself), so the per-position work
  // is real, but re-deriving all of them on a render that touched neither the
  // squad nor the lineup (e.g. typing into the Rename box) was pure waste.
  const rostersByPosition = useMemoA(() => {
    const byKey = {};
    positions.forEach(p => {
      const offered = rosterWithoutPlacedElsewhere(squadSorted, { positions: values, memberIds }, p.key);
      // A position always lists the member it holds: a lineup another device saved can
      // hold one member at two positions, and a picker without its own value would
      // show "none" where the form holds a member.
      byKey[p.key] = squadSorted.filter(m => m.id === memberIds[p.key] || offered.includes(m));
    });
    return byKey;
  }, [positions, squadSorted, values, memberIds]);

  // Operation 1 (SELECT): put an existing squad member's (name, id) pair
  // into a position, both keyed together so they can never drift apart.
  // A change to a position ends the refusal shown for the lineup as it was: the
  // next Save judges the lineup again.
  const selectMember = (posKey, member) => {
    setError("");
    setValues(v => ({ ...v, [posKey]: member.name }));
    setMemberIds(ids => ({ ...ids, [posKey]: member.id }));
  };

  const clearPosition = (posKey) => {
    setError("");
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
    // The name is judged against the team's members, so they must have been
    // read: against none, a member the team has would be added again (and the
    // server would refuse it as a second member of that name), and a position
    // whose seeded slot is free would mint rather than name it. Members that
    // cannot be read, or not in time, go without, as they always did. What is
    // judged is the list and the placements as they are once the wait is over,
    // not the ones this handler closed over.
    const waiting = form.waitForMembers();
    if (waiting) {
      setAddBusy(true);
      await waiting;
      setAddBusy(false);
    }
    const members = form.squadRef.current;
    const placed = memberIdsRef.current;
    // The three outcomes the resolver itself has, in ITS order (typedNameTarget
    // is the one owner of it), so the copy can never describe a branch the action
    // will not take. First: the name is an EXISTING member's (same normalisation
    // as the resolver): nothing is created or renamed, that member is simply
    // selected here, with no confirmation because nothing permanent happens;
    // unless the lineup already fields them elsewhere, which is refused where the
    // list would not have offered them.
    const { member: target, write } = typedNameTarget(members, posKey, name, placed);
    if (write === "none") {
      const elsewhere = memberPlacedElsewhere(placed, posKey, target.id);
      if (elsewhere) {
        setError(alreadyPlacedNote(target.name, lineupPositionLabel(elsewhere)));
        return;
      }
      selectMember(posKey, target);
      setAddingPos(null);
      setAddingName("");
      return;
    }
    const message = write === "rename"
      ? `Name ${squadSlotLabel(teamNumber, target.index)} as "${name}"?`
      : `Add "${name}" as a new member of ${team?.name || team?.Name || "this team"}? This adds a new position to the team. Once added, it can be cleared but never removed.`;
    const ok = await window.confirmDialog({
      message,
      confirmLabel: write === "rename" ? "Name slot" : "Add member",
      cancelLabel: "Cancel",
    });
    if (!ok) return;
    setAddBusy(true);
    setError("");
    try {
      const resolved = await resolveMemberIdsForPositions(compId, teamId, { [posKey]: name }, members, password, placed);
      const failure = (resolved.failures || []).find(f => f.position === posKey);
      if (failure) {
        setError(failure.reason || "Failed to add team member");
        return;
      }
      const resolvedId = resolved.memberIds[posKey];
      const resolvedMember = resolved.squad.find(m => m.id === resolvedId);
      changeMembers(resolved.squad);
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
      form.memberRenamed(await window.API.renameTeamMember(compId, teamId, id, name, password));
      setRenamingId(null);
      setRenamingName("");
    } catch (e) {
      setError(e?.message || "Failed to rename team member");
    } finally {
      setRenameBusy(false);
    }
  };

  const cancelRename = () => { setRenamingId(null); setRenamingName(""); setError(""); };

  // Clearing: the operator's own "removal" of a squad member's name. The
  // id and index survive: the member stays placed wherever it already sits
  // (memberIds keeps pointing at it), now nameless, which is a legal
  // placement everywhere this codebase reads a lineup. The button is
  // already disabled once the competition has started (`started` above),
  // but a 409 can still arrive if another device started the competition
  // after this page loaded its squad -- surfaced via `error`, never
  // swallowed.
  const clearMember = async (member) => {
    setClearingId(member.id);
    setError("");
    try {
      form.memberRenamed(await window.API.clearTeamMember(compId, teamId, member.id, password));
    } catch (e) {
      setError(e?.message || "Failed to clear the name");
    } finally {
      setClearingId(null);
    }
  };

  const save = async () => {
    // Nothing is written before the lineup was read, or while it holds no change:
    // the starting lineup as much as a match's, so a Save that changed nothing
    // never writes the lineup it carries over as a lineup of its own.
    if (!canSave) return;
    setError("");
    setSaveWarning("");
    setSaving(true);
    try {
      // The save names the positions the operator changed and carries those alone
      // (form.lineupToSave, operator decision 2026-10-07): the server puts them on the
      // lineup it holds when the save arrives, so a change another device made to a
      // position left alone since this page read the lineup stays.
      const { positions: shown, memberIds: shownIds, changed } = form.lineupToSave();
      // Trim here too (not just on commit), so a Save never persists leading/trailing
      // or whitespace-only names. A cleared position goes as its empty name, which the
      // server needs to be there, and a position holding a picked team member with no
      // name yet is not vacant (bc-dnst): the id is the placement, so it goes with an
      // empty name, exactly as the match panel and the score sheet write it.
      const named = {};
      positionKeys.forEach((k) => { named[k] = (shown[k] || "").trim(); });
      // One position per member (the shared predicate, bc-dnst), asked of the lineup
      // as the form shows it: the pickers never offer a member this form holds at
      // another position, but a lineup another device saved can hold one twice. The
      // server asks the same of the lineup it composes, and refuses naming both.
      const duplicate = lineupDuplicateNote(named, shownIds, lineupPositionLabel, positionKeys, changed);
      if (duplicate) {
        setError(duplicate);
        return;
      }
      const body = changedLineupSave(named, shownIds, changed);
      const idsOut = Object.keys(body.memberIds).length > 0 ? body.memberIds : undefined;
      const updated = matchId
        ? await window.API.putMatchLineup(compId, teamId, matchId, body.positions, password, idsOut, body.changed)
        : await window.API.putTeamLineup(compId, teamId, STARTING_ROUND, body.positions, password, idsOut, body.changed);
      // F5: a queued (offline/transient) write is NOT a confirmed save: don't
      // clear the revising state or show "saved"; the write is durable and will
      // retry. Keep the form editable and tell the operator it's pending.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast(queuedNotice(updated), "pending");
        return;
      }
      // What the server answered is the whole lineup it holds now, the positions this
      // save left alone included: the new baseline, for the starting lineup as for a
      // match, so a saved lineup leaves no draft behind.
      form.confirmSaved(updated);
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

  if (form.loading) {
    return <div className="page" style={{ padding: 24 }}>Loading lineup…</div>;
  }

  // An id-less team is EXPLAINED, not addressed. Every control below writes by
  // team id: naming a member 404s without one, and a lineup save would persist
  // under a key no id-keyed reader ever finds, which is worse because it looks
  // like it worked. The roster gains ids the first time it is saved, so the way
  // out is one sentence rather than a dead screen (bc-dnst; the deleted Settings
  // section rendered the same explanation for the same reason).
  if (!teamId) {
    return (
      <div className="page" data-testid="lineup-team-no-id" style={{ padding: 24, maxWidth: 640 }}>
        <h2 style={{ margin: 0, fontSize: 22, fontWeight: 700 }}>{team?.name || team?.Name || "Team"}</h2>
        <p style={{ color: "var(--ink-3)", marginTop: 8 }}>
          No id on file for this team, so its people cannot be recorded yet.
          Save the roster once on Participants &amp; seeds and the ids are assigned.
        </p>
      </div>
    );
  }

  return (
    <div className="page" data-testid="lineup-form-root" style={{ padding: 24, maxWidth: 640 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
        <div>
          <div className="overline">
            {matchId ? matchLabel : "Starting lineup"}
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

      <LineupProblem form={form} testId="lineup-problem" />
      {notInMatch && (
        <div className="alert alert--warn" role="alert" data-testid="lineup-not-in-match" style={{ marginBottom: 12 }}>
          {`${team?.name || team?.Name || "The team"} is no longer in ${matchLabel}. Choose another match, or the starting lineup, in Lineup for.`}
        </div>
      )}
      {matchId && (
        <LineupSourceLine form={form} matchId={matchId} allMatches={allMatches} busy={busy || notInMatch} testId="lineup-source" />
      )}

      <LineupDraftNotice draft={form.draft} busy={busy} testId="lineup-draft-notice" />

      {error && (
        <div className="alert alert--error" style={{ marginBottom: 12 }}>
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
                    disabled={locked}
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
                    {rostersByPosition[p.key].map(m => (
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
                      disabled={addBusy || busy}
                      onChange={(e) => setAddingName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") { e.preventDefault(); commitAdd(); }
                        else if (e.key === "Escape") { e.preventDefault(); cancelAdd(); }
                      }}
                    />
                    <button type="button" className="btn btn--sm" onClick={commitAdd} disabled={addBusy || busy || !addingName.trim()}>
                      {addBusy ? "Adding…" : "Add"}
                    </button>
                    <button type="button" className="btn btn--ghost btn--sm" onClick={cancelAdd} disabled={addBusy || busy}>Cancel</button>
                  </div>
                )}
              </label>
            );
          })}
          {/* An EMPTY list and a list that FAILED TO LOAD look identical, and
              telling the operator the team is empty when it is not invites
              them to mint a brand-new permanent numbered slot for a member
              who already exists on the server (a member can be cleared but
              never removed). The deleted Settings section said which of the
              two it was; say it here. */}
          {squadSorted.length === 0 && squadUnavailable && (
            <div className="field__hint field__hint--warn" data-testid="lineup-members-unavailable">
              The team member list could not be loaded, so this may not be the whole team. Reload before adding a member, or you may add a second slot for someone who already has one.
            </div>
          )}
          {squadSorted.length === 0 && !squadUnavailable && (
            <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
              This team has no members yet: choose "+ Add new member…" in a position above to add the first one.
            </div>
          )}
        </div>

        {squadSorted.length > 0 && (
          <div style={{ marginTop: 20, paddingTop: 16, borderTop: "1px solid var(--line)" }}>
            <div className="overline" style={{ marginBottom: 8 }}>Team members</div>
            {/* A `title` is the only reason a disabled Clear name carries, and
                a title needs hover, which a touch tablet does not have. The
                deleted Settings section stated it as a persistent line; state
                it here too, so the greyed-out control is never unexplained on
                the device the desk actually runs on. */}
            {started && (
              <div className="field__hint field__hint--warn" data-testid="lineup-clear-locked" style={{ marginBottom: 8 }}>
                Clearing a name is locked once the competition has started. Rename still works.
              </div>
            )}
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {squadSorted.map(m => (
                <div key={m.id} data-testid={`squad-member-${m.id}`} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  {renamingId === m.id ? renameMemberFields({
                    value: renamingName,
                    onChange: setRenamingName,
                    onCommit: commitRename,
                    onCancel: cancelRename,
                    busy: renameBusy,
                    ariaLabel: `Rename ${m.name || squadSlotLabel(teamNumber, m.index)}`,
                    inputStyle: { flex: 1 },
                    disabled: renameBusy || busy,
                  }) : (
                    <>
                      <span style={{ fontSize: 13, color: "var(--ink-3)", minWidth: 44 }}>
                        {squadSlotLabel(teamNumber, m.index)}
                      </span>
                      <span style={{ flex: 1 }}>{m.name}</span>
                      <button type="button" className="btn btn--ghost btn--sm" onClick={() => startRename(m)} disabled={busy}>Rename</button>
                      {/* Nothing to clear on an already-blank slot: the row
                          shows no Clear button at all rather than one that
                          would refuse itself (a blank candidate never
                          collides and clearing it would be a no-op the
                          operator has no reason to ask for). */}
                      {!!(m.name || "").trim() && (
                        <button
                          type="button"
                          className="btn btn--ghost btn--sm"
                          onClick={() => clearMember(m)}
                          disabled={busy || renameBusy || clearingId !== null || started}
                          title={started ? "Names cannot be cleared once the competition has started" : undefined}
                        >
                          {clearingId === m.id ? "Clearing…" : "Clear name"}
                        </button>
                      )}
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
            disabled={busy || memberBusy || !canSave}
            title={form.saveTitle}
          >
            {saving ? "Saving…" : "Save lineup"}
          </button>
        </div>
      </div>
    </div>
  );
}

// teamMatchOptions: the team matches `teamId` is seated in, as the { id, label }
// choices of the "Lineup for" select. The order follows the server's match
// order (engine/lineup_in_force.go) and is presentation only: the server alone
// decides which lineup a match carries. Pool and league matches come first, by
// their number in the pool (a Swiss team's rounds in the order they were
// drawn), then the knockout by round and position, the 3rd-place match last.
// A team is seated by participant id, as the server seats it. A pool tiebreaker
// or daihyosen is an individual bout, and a bye (a hidden match, or one with a
// side left empty, a Swiss bye included) is a match nobody fights. A knockout
// match whose opponent is not decided yet is no bye: that side carries a
// placeholder name ("Winner of ..."), so the match is listed and its lineup can
// be set before then.
function teamMatchOptions(allMatches, teamId) {
  if (!teamId) return [];
  const mine = (allMatches || []).filter(m => m && m.id && !m.hidden && !isSupplementaryBout(m.id)
    && nameOf(m.sideA) && nameOf(m.sideB)
    && (idOf(m.sideA) === teamId || idOf(m.sideB) === teamId));
  const pool = mine.filter(m => m.phase === "pool");
  const drawn = [...new Set(pool.map(m => m.poolName))];
  pool.sort((a, b) => drawn.indexOf(a.poolName) - drawn.indexOf(b.poolName) || poolMatchNumberOf(a.id) - poolMatchNumberOf(b.id));
  // Array.sort is stable, so a round's matches keep their position order.
  const knockout = mine.filter(m => m.phase === "bracket").sort((a, b) => a.roundIndex - b.roundIndex);
  return [...pool, ...knockout].map(m => ({ id: m.id, label: scoreRowMatchLabel(m) || m.id }));
}

// The "Lineup for" choice that names no match: the team's starting lineup.
const STARTING_TARGET = { id: "", label: "" };

// AdminTeamLineupsList: selectors that pick a team from the competition's
// player list and which of its lineups to edit (its starting lineup, or one of
// its team matches), and render AdminLineup for that pair. Mounted by the
// "Lineups" sidebar entry in admin_competition.jsx (T136 nav hook).
function AdminTeamLineupsList({ comp, pools, poolMatches, bracket, password, showToast }) {
  const teams = (comp?.players || []);
  const [teamId, setTeamId] = useStateA(teams[0] ? teamIdOf(teams[0]) : "");
  // The starting lineup has no id; otherwise one of the team's matches, with the
  // label it was offered under, which the page keeps for a match that stops being
  // offered.
  const [chosen, setChosen] = useStateA(STARTING_TARGET);
  const selectedTeam = teams.find(t => teamIdOf(t) === teamId) || teams[0];
  // The competition page holds the match data beside the competition's config
  // (pools, poolMatches and bracket are its own props), so the matches are read
  // from both. A competition that carries the data itself is read as it stands.
  const allMatches = useMemoA(() => {
    if (typeof window.compMatchesForCompetition !== "function") return [];
    const data = (pools || poolMatches || bracket) ? { pools, poolMatches, bracket } : undefined;
    return window.compMatchesForCompetition(comp, data);
  }, [comp, pools, poolMatches, bracket]);
  const matchOptions = useMemoA(
    () => teamMatchOptions(allMatches, teamServerIdOf(selectedTeam)),
    [allMatches, selectedTeam]
  );
  // A match the team is no longer seated in (a feeding match reopened or corrected
  // elsewhere, a draw discarded and drawn again) is no longer offered, but it stays
  // chosen: going back to the starting lineup by itself would put the operator's
  // next edit and Save on the lineup that every later match without its own carries.
  // The editor says the team is not in it, and cannot save for it.
  const offered = matchOptions.find(o => o.id === chosen.id) || null;
  const target = offered || (chosen.id ? chosen : null);

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
            onChange={(e) => { setTeamId(e.target.value); setChosen(STARTING_TARGET); }}
            style={{ padding: "6px 8px", fontSize: 14, minWidth: 200 }}
          >
            {teams.map(t => (
              <option key={teamIdOf(t)} value={teamIdOf(t)}>{t.name || t.Name}</option>
            ))}
          </select>
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <span className="overline">Lineup for</span>
          <select
            className="input"
            value={target ? target.id : ""}
            onChange={(e) => setChosen(matchOptions.find(o => o.id === e.target.value) || STARTING_TARGET)}
            style={{ padding: "6px 8px", fontSize: 14, minWidth: 200 }}
          >
            <option value="">Starting lineup</option>
            {matchOptions.map(o => (
              <option key={o.id} value={o.id}>{o.label}</option>
            ))}
            {target && !offered && <option value={target.id}>{target.label}</option>}
          </select>
        </label>
      </div>
      {selectedTeam ? (
        <AdminLineup
          comp={comp}
          team={selectedTeam}
          matchId={target ? target.id : ""}
          matchLabel={target ? target.label : ""}
          notInMatch={!!target && !offered}
          allMatches={allMatches}
          password={password}
          showToast={showToast}
          // pass a stable key on the inner form so switching teams /
          // lineups remounts the loader cleanly instead of stale state.
          key={`${teamIdOf(selectedTeam)}:${target ? `match:${target.id}` : "start"}`}
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
    resolveMemberIdsForPositions, memberIdentityWarning,
    lineupPositionLabel, typedNameTarget,
  };
}

export {
  AdminLineup, AdminTeamLineupsList, positionsForSize, rosterFor, mergeRosterWithAssigned, teamIdOf,
  lineupPositionLabel, blankMemberForPosition, typedNameTarget,
  resolveMemberIdForName, resolveMemberIdsForPositions, memberIdentityWarning,
};
