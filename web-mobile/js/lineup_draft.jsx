// lineup_draft.jsx: unsaved lineup picks survive a reload, the app's Back and
// closing a panel (bc-lnul, operator decision 2026-10-05). An editor hands
// useLineupDraft the lineup it loaded (baseline) and the one it shows (current);
// while they differ, the difference is kept as a draft in this tab's
// sessionStorage (per tab: it survives a reload and Back, and never reaches
// another tab) and handed back through onRestore when the same lineup is opened
// again.
//
// Nothing here writes to the server: a restored draft is saved only by the
// operator's Save, and an offline write stays the queue's job (api_client.jsx).
//
// useLineupForm, below, is the state machine of ONE lineup editor, shared by the
// at-court panel and the Lineups page: what was read, where it was saved, what
// is shown, whether it differs, the draft, the confirmed save and giving a
// match's own lineup up. The editors keep their layouts and their save bodies.
//
// The rules for the team's members (mergeMembers, the one rule a list that arrives is
// merged by, and newMembersWait, the wait for a team's first list) are exported too:
// the team score sheet writes members from its bout rows and keeps its own lists, and
// asks the same owner what a list that arrives does to them.
//
// Its imports are lineup_resolver.jsx, the owner of the lineup read and of which
// positions differ, and write_result.jsx, the leaf that owns the deadline a bounded
// wait is given. What a Save carries is lineup_save.jsx's. The hooks read the React
// global at call time, as tap_guard.jsx does.
import {
  changedLineupPositions, lineupFields, lineupSourceOf, lineupSourceLabel, isOwnLineup, resolveMatchLineup,
  previousLineupConfirm, PREVIOUS_LINEUP_LABEL, SAVE_QUEUED_REASON, REMOVED_UNREAD_NOTICE, STARTING_ROUND,
  lineupReadFailure,
} from './lineup_resolver.jsx';
import { FETCH_TIMEOUT_MS, TIMED_OUT, withinDeadline } from './write_result.jsx';

const STORAGE_PREFIX = 'bc.lineupDraft.v1:';
// A draft older than this is never offered: the operator has moved on.
const MAX_AGE_MS = 12 * 60 * 60 * 1000;

const RESTORED_NOTICE = 'Unsaved lineup changes restored';
const NOT_RESTORED_NOTICE = 'Not restored, the lineup changed since';

// The ONE key of a lineup's draft, so the panel and the Lineups page offer each
// other's drafts: a match's lineup, or a team's starting lineup (no match).
export const lineupDraftKey = ({ compId, teamId, matchId }) => (
  matchId ? `${compId}:${teamId}:match:${matchId}` : `${compId}:${teamId}:start`
);

// Every storage access is guarded: reaching sessionStorage throws when the
// browser blocks it, and a write throws on a full or private-mode store. A draft
// is a convenience, so a storage that refuses it must never reach the editor.
function store() {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage;
  } catch (_e) {
    return null;
  }
}

const isSide = (v) => !!v && typeof v === 'object' && !!v.positions && typeof v.positions === 'object';

function readDraft(key) {
  try {
    const raw = store()?.getItem(STORAGE_PREFIX + key);
    if (!raw) return null;
    const draft = JSON.parse(raw);
    return draft && Number.isFinite(draft.savedAt) && isSide(draft.baseline) && isSide(draft.current) ? draft : null;
  } catch (_e) {
    return null;
  }
}

function writeDraft(key, draft) {
  try {
    store()?.setItem(STORAGE_PREFIX + key, JSON.stringify(draft));
  } catch (_e) {
    // Quota or private mode: the draft is simply not kept.
  }
}

function removeDraft(key) {
  try {
    store()?.removeItem(STORAGE_PREFIX + key);
  } catch (_e) {
    // Nothing to remove from a storage that cannot be reached.
  }
}

// The names the draft held at the positions it had changed, once each: what a
// notice lists when the draft could not be restored.
function draftNames(draft, keys) {
  const names = [];
  changedLineupPositions(draft.baseline, draft.current, keys).forEach((key) => {
    const name = String(draft.current.positions[key] ?? '').trim();
    if (name && !names.includes(name)) names.push(name);
  });
  return names;
}

// Whether `loaded` already holds everything a draft shows (`shown`): the same name at
// every position, and at one with no name the same member. A name is held by the
// name alone: a name typed in has no member id in the draft, and the save that wrote
// it attached one, so the operator's own landed save differs from their draft by ids
// the draft never had.
function holdsDraft(loaded, shown, keys) {
  const held = lineupFields(loaded, keys);
  const wanted = lineupFields(shown, keys);
  const name = (side, key) => String(side.positions[key]).trim();
  return keys.every((key) => (name(wanted, key)
    ? name(held, key) === name(wanted, key)
    : !name(held, key) && held.memberIds[key] === wanted.memberIds[key]));
}

const NOTHING = { restored: false, stale: null };

// useLineupDraft: keeps and restores one lineup's draft.
//   key         the lineup's lineupDraftKey
//   ready       true once baseline and current show what the server holds for THIS
//               key (the load succeeded): until then the hook touches nothing,
//               since the empty form of a lineup still loading equals its empty
//               baseline and would read as "nothing to keep"
//   baseline    the lineup as loaded or last saved, { positions, memberIds }
//   current     the lineup as shown, in the same shape
//   positionKeys  the lineup's position keys
//   onRestore   called with the { positions, memberIds } to show, for a restored
//               draft and for discard()
// Returns { restored, stale, discard, resolved }: `restored` while a restored draft
// is still unsaved; `stale` is { names } when the lineup changed meanwhile and the
// draft was dropped rather than overwrite it, and it stays until the editor calls
// `resolved()` (the lineup was saved, or given up) or the lineup is left.
export function useLineupDraft({ key, ready, baseline, current, positionKeys, onRestore }) {
  const { useState, useRef, useCallback, useLayoutEffect } = React;
  const [outcome, setOutcome] = useState({ key: '', ...NOTHING });
  // The key whose draft was last read: a draft is read once, when its lineup has
  // loaded, and the sync that follows must not remove it before that.
  const readKey = useRef('');
  const baseFields = lineupFields(baseline, positionKeys);
  const nowFields = lineupFields(current, positionKeys);
  const latest = useRef(null);
  latest.current = { baseFields, nowFields, positionKeys, onRestore };
  // `current` and `baseline` are new objects every render: the effect runs on
  // what they hold.
  const signature = JSON.stringify([positionKeys, baseFields, nowFields]);

  // A layout effect, so a draft is on disk the moment an edit commits (a reload
  // right after it still finds it) and a restore lands before the first paint.
  useLayoutEffect(() => {
    if (!ready || !key) return;
    const { baseFields: loaded, nowFields: shown, positionKeys: keys, onRestore: restore } = latest.current;
    if (readKey.current !== key) {
      readKey.current = key;
      // A notice left from another lineup, or from this one's last visit, is not
      // this visit's.
      const settle = (next) => setOutcome((o) => (
        !o.restored && !o.stale && !next.restored && !next.stale ? o : { key, ...next }
      ));
      const draft = readDraft(key);
      if (!draft) {
        // None stored (a no-op), or one that cannot be read: it goes.
        removeDraft(key);
        settle(NOTHING);
        return;
      }
      const expired = Date.now() - draft.savedAt > MAX_AGE_MS;
      if (expired || changedLineupPositions(draft.baseline, draft.current, keys).length === 0
        || holdsDraft(loaded, draft.current, keys)) {
        // Too old, nothing in it, or the saved lineup already holds all of it (the
        // operator's own save, written after they left, included).
        removeDraft(key);
        settle(NOTHING);
      } else if (changedLineupPositions(draft.baseline, loaded, keys).length > 0) {
        // The lineup changed meanwhile (another device): never overwrite it.
        removeDraft(key);
        settle({ restored: false, stale: { names: draftNames(draft, keys) } });
      } else {
        restore(lineupFields(draft.current, keys));
        settle({ restored: true, stale: null });
      }
      return;
    }
    if (changedLineupPositions(loaded, shown, keys).length > 0) {
      writeDraft(key, { savedAt: Date.now(), baseline: loaded, current: shown });
    } else {
      removeDraft(key);
      setOutcome((o) => (o.restored ? { ...o, restored: false } : o));
    }
  }, [key, ready, signature]);

  const discard = useCallback(() => {
    const { baseFields: loaded, positionKeys: keys, onRestore: restore } = latest.current;
    removeDraft(key);
    restore(lineupFields(loaded, keys));
    setOutcome((o) => (o.restored ? { ...o, restored: false } : o));
  }, [key]);

  // "Not restored" says which names are missing from the lineup as it was opened,
  // so editing does not answer it, not even putting the edit back: only the lineup
  // being saved, or given up, does.
  const resolved = useCallback(() => {
    setOutcome((o) => (o.stale ? { ...o, stale: null } : o));
  }, []);

  const mine = outcome.key === key ? outcome : NOTHING;
  return { restored: mine.restored, stale: mine.stale, discard, resolved };
}

// LineupDraftNotice: the amber notice under an editor's source label, from what
// useLineupDraft returned. A restored draft always offers Discard; a draft dropped
// because the lineup changed meanwhile names what was not restored. Discard is a
// plain .btn, so the coarse-pointer floor reaches it through the class. It waits
// while `busy` (the editor's own: a save is out): the save writes the changes
// Discard would take back.
export function LineupDraftNotice({ draft, busy, testId }) {
  const { restored, stale, discard } = draft;
  if (!restored && !stale) return null;
  const names = stale ? stale.names.join(', ') : '';
  return (
    <div className="alert alert--warn lineup-draft-note" role="status" data-testid={testId}>
      <span>{restored ? RESTORED_NOTICE : (names ? `${NOT_RESTORED_NOTICE}: ${names}` : NOT_RESTORED_NOTICE)}</span>
      {restored && <button type="button" className="btn btn--sm" onClick={discard} disabled={busy}>Discard</button>}
    </div>
  );
}

const EMPTY_SIDE = { positions: {}, memberIds: {} };
const NO_CHANGES_TITLE = 'No changes to save';

// A name box per position, all empty: the form of a lineup nothing is known of.
const blankNames = (positionKeys) => Object.fromEntries(positionKeys.map((key) => [key, '']));

// The request for every team's members of a competition, shared by the reads begun in
// the same tick: the at-court panel's two editors each ask for their own team's as they
// open and when a lineup change is announced, and one answer holds both. A read begun
// any later is a request of its own, so it holds whatever the server has by then: a
// read made for a change announced must not be handed an answer that began before it.
const requestsOfThisTick = new Map();
function requestMembers(compId, password) {
  const key = `${compId}\n${password}`;
  if (!requestsOfThisTick.has(key)) {
    requestsOfThisTick.set(key, window.API.fetchSquads(compId, password));
    Promise.resolve().then(() => requestsOfThisTick.delete(key));
  }
  return requestsOfThisTick.get(key);
}

// The team's members (the API answers every team's, keyed by id), or null when
// they cannot be read: whoever asks keeps the list it has.
async function readMembers(compId, teamId, password) {
  try {
    const squads = await requestMembers(compId, password);
    return (squads && squads[teamId]) || [];
  } catch (_e) {
    return null;
  }
}

// The team's members once `arriving` is shown beside `shown`: every member of both
// lists, once, in member number order, the order the pickers list them in. No screen
// removes a member, so one a list lacks was added after that list was read, or after
// the list an editor built its update on, and was not taken away. For a member both
// lists hold, the copy with the larger `modifiedAt` is kept, the arriving one on a tie.
// The server stamps a member each time it is created or named (a member nobody has
// touched carries 0, and a missing stamp reads as 0), so which copy is newer is read off
// the member itself, never off which list arrived last or which read began first: a
// list read before a rename never undoes it, and a rename made on another device after
// this one's own shows over it. This is the ONE door every list of members passes, an
// editor's own write included, which is merged as the server answered it, stamp and all:
// a copy built from what was typed carries no stamp and would lose to every list.
export function mergeMembers(shown, arriving) {
  const kept = new Map(shown.map((m) => [m.id, m]));
  arriving.forEach((m) => {
    const held = kept.get(m.id);
    if (!held || (m.modifiedAt || 0) >= (held.modifiedAt || 0)) kept.set(m.id, m);
  });
  return [...kept.values()].sort((a, b) => (a.index || 0) - (b.index || 0));
}

// The first list of a team's members that a read shows, and the wait for it. A name
// typed before one is shown is resolved against no members: a new name is minted where
// the member's seeded slot is free, and the name of a member the team already has is
// refused by the server as a second member of that name. `listed` says a READ's list has
// been shown for the team (a change an editor made itself is not one). `pending()` is
// null when there is nothing out to wait for (a list is shown, or the read failed: the
// editor goes on without one, as it always did), else a promise that ends when a list
// is shown, when the read fails or at the deadline of any bounded request, and says
// whether a list was shown by then: when none was, the editor owns up to going on
// without (the members-unavailable warning). The deadline ends the wait for good: a read
// that hangs holds up the first name typed and no other, which goes on at once, and
// `onTimeout` lets the editor flag the members as unavailable for it as it does when
// the read fails. A list that arrives later is still shown (`show`). Another team starts
// a new wait.
export function newMembersWait(onTimeout) {
  let settle;
  const done = new Promise((resolve) => { settle = resolve; });
  const end = () => {
    wait.ended = true;
    settle();
  };
  const wait = {
    listed: false,
    ended: false,
    show: () => {
      wait.listed = true;
      end();
    },
    fail: end,
    pending: () => (wait.ended ? null : withinDeadline(done, FETCH_TIMEOUT_MS).then((outcome) => {
      if (outcome === TIMED_OUT) {
        end();
        if (onTimeout) onTimeout();
      }
      return wait.listed;
    })),
  };
  return wait;
}

// useLineupForm: the state of one lineup editor, for a team's match (`matchId`)
// or for its starting lineup (no matchId). It reads the lineup (a match's is the
// one in force there, which the server works out; the starting lineup is read
// exactly, as the team's round-0 entry) and holds
//   values, memberIds   what is shown, keyed by position
//   baseline, source    what the server held when it was read or last saved, and
//                       where that lineup was saved
//   loading, loadError  the first read is out; why a lineup could not be read
//   read                the lineup was read for THIS key. Nothing may be written
//                       before: an editor that could not read a lineup shows an
//                       empty one, and saving it would write that over the real
//                       lineup and over every match that carries it
//   dirty, canSave      the lineup differs from what was read; canSave is
//                       read && dirty, and the editors gate Save on it
//   draft               useLineupDraft's answer for it
//   error, warning      the editor's own refusal and its after-save notice, here
//                       so that giving a lineup up clears both
//   removing            a removal is out
//   saveQueued          a save of this lineup is still waiting to be sent
//   squad               the team's members as the API answers them: every list
//                       that arrives, a read's or an editor's own write's, is
//                       merged with the one shown by member id and the server's
//                       stamp (mergeMembers), so that none is lost and none is
//                       undone by an older copy; squadRef, the same list for a
//                       handler that resumes after a wait, and
//                       squadUnavailable, that they could not be read
// and gives the editor
//   lineupToSave()        what a Save carries: the form as shown and `changed`, the
//                         positions the operator changed, `{ positions, memberIds,
//                         changed }`. The server puts those on the lineup it holds
//                         when the save arrives (operator decision 2026-10-07), so
//                         nothing is asked of the server for it, and an offline
//                         save is built, and queued, like any other
//   changeMembers(members)  after the server answered a write of members (an add, a
//                         mint): the members as it answered them, stamped, merged in
//   memberRenamed(member)  after the server renamed a member, or cleared the name
//                         (""): the member as it answered, merged in, and the name of
//                         the copy kept shown on the positions that hold it and on the
//                         baseline, so the rename makes no edit of the lineup
//   confirmSaved(lineup)  after the server confirmed a save: the whole lineup it
//                         answered, the positions this save left alone as another
//                         device changed them among them, is now shown and the
//                         baseline, and a match's own lineup, and what a draft
//                         could not restore no longer applies
//   dropOwnLineup()       confirm, then remove a match's own lineup, so the match
//                         carries its team's previous one, and show that one
//   retry()               read again ("Try again")
//   waitForMembers()      what an editor awaits before it resolves a typed name:
//                         null when nothing is out to wait for (a list of the team's
//                         members that was read is shown, or the read failed), else
//                         a promise that ends when one is shown, when the read fails
//                         or at the deadline of any bounded request, and says whether
//                         one was shown (newMembersWait). Against no list the name
//                         would be minted where the member's seeded slot is free, or
//                         where the team already has the member
// A lineup change announced for the competition (the lineup-updated event) is
// followed: the lineup is read again and shown, unless the form has edits or a
// removal is out (one announced while a read of the lineup is out waits for that
// read, and is followed once it has shown the lineup, or dropped when it fails),
// and the team's members are read again with it, so a member another device
// created is in the list when the lineup names them. They are shown whether or
// not the lineup is (the list is not the operator's edit): a form with edits reads
// them on their own, and leaves its positions as they are. A members read that
// fails or is not answered shows nothing and keeps the list as it was. A change
// announced for another team is not followed (one that names no team is). An
// editor's own change of the members is not followed by a read of its own: the
// server announces it that way too, for the team, to every device, this one among
// them.
// The editors keep their layouts and their save bodies.
export function useLineupForm({ compId, teamId, matchId = '', positionKeys, password, matchLabel, teamName }) {
  const { useState, useRef, useEffect } = React;
  const draftKey = lineupDraftKey({ compId, teamId, matchId });
  const [values, setValues] = useState(() => blankNames(positionKeys));
  const [memberIds, setMemberIds] = useState({});
  // memberIdsRef mirrors memberIds for the editors' handlers that AWAIT a round
  // trip (a rename, a clear) before touching it. The pickers stay interactive
  // meanwhile, so the memberIds a handler closed over can be stale by the time it
  // resolves: an operator who re-picks the position mid-flight would otherwise
  // have it blanked by the resolving clear, and Save would then write it with a
  // member id and no name, which the occupancy rule reads as a real placement
  // fielding a nameless fighter.
  const memberIdsRef = useRef(memberIds);
  memberIdsRef.current = memberIds;
  const [baseline, setBaseline] = useState(EMPTY_SIDE);
  const [source, setSource] = useState(null);
  // The draft key whose lineup was read into the form: a lineup is only judged,
  // restored over or saved once it was read, never the empty form of a failed read.
  const [loadedKey, setLoadedKey] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const [removing, setRemoving] = useState(false);
  // The team's members. The read made when the editor opens that fails leaves the
  // list as it is and flags that it could not be read, unless a read's list is shown
  // for the team by then: a lineup is still read and saved without it. A read made
  // later that fails shows nothing and changes no flag.
  const [squad, setSquadList] = useState([]);
  const [squadUnavailable, setSquadUnavailable] = useState(false);
  // squadRef mirrors squad for the editor's save, which resolves a typed name after
  // it awaits (the first list, a member write). A read of the team's members that
  // lands meanwhile (a change announced) may bring in a member another device
  // created, which the list the save closed over when Save was tapped lacks: the
  // resolver would mint the member a second time, and a member can never be removed.
  // A list that arrives (setSquad, below) is put on it as it is shown, so it holds
  // that list whenever the handler resumes, whether or not the render that shows it
  // has run.
  const squadRef = useRef(squad);
  squadRef.current = squad;
  // The read whose answer may still be shown: another lineup, or leaving, ends it.
  const attempt = useRef(0);
  // The number, in `attempt`, of the read of the lineup that is out (the first, a Try
  // again, or the one of another lineup), null when none is. A lineup change announced
  // meanwhile is not read over it: a read that replaced it could fail where it would
  // have shown the lineup. `followDue` keeps the change until that read has shown the
  // lineup, and drops it when the read fails.
  const reading = useRef(null);
  const followDue = useRef(false);
  // The team's members are asked for by two reads (the one made when the editor
  // opens, and the one a change announced for the team makes, followed or not), which
  // need not answer in the order they began, and changed by the editor's own writes. None of that is ordered here: every list is
  // merged by the stamp each member carries (mergeMembers), so a read answering late
  // cannot put an older copy over a newer one. What is tied down is the team: `teamKey`
  // is the team of the render a read, or a write's handler, was made in, and
  // `squadTeam` the one the editor shows (set by the effect below), so a read begun for
  // a team the editor has been given since, or an answer a handler of that team brings,
  // is merged into no list of another team.
  const teamKey = `${compId}/${teamId}`;
  const squadTeam = useRef(teamKey);
  // Whether a read's list is shown for this team, and the wait for it (see
  // newMembersWait): what an editor's typed name and the failure of the read made as
  // the editor opened both ask. Another team starts it again. A wait the deadline ended
  // flags the members as unavailable, as a read that failed does, so the names typed
  // after it say so too.
  const firstMembers = useRef(null);
  const newWait = () => newMembersWait(() => setSquadUnavailable(true));
  if (firstMembers.current === null) firstMembers.current = newWait();

  // The ONE door every list of the team's members comes through: showMembers, below,
  // for a read's answer, and changeMembers for an editor's own write. The list is
  // merged with the one shown (see mergeMembers): of a member both hold the copy with
  // the larger stamp stands, and no member is lost to a read begun before it was added
  // or to a list built on an older one. It is on squadRef at once. It is not handed to
  // the editors: what an editor writes goes through changeMembers.
  const setSquad = (members) => {
    const merged = mergeMembers(squadRef.current, members);
    squadRef.current = merged;
    setSquadList(merged);
  };

  // Shows what a members read for `team` answered: a list, not a failure (null) or the
  // deadline (TIMED_OUT), and not one read for a team the editor has been given since.
  const showMembers = (team, members) => {
    if (team !== squadTeam.current || !Array.isArray(members)) return;
    setSquad(members);
    setSquadUnavailable(false);
    firstMembers.current.show();
  };

  // The ONE door for an editor's own write of the team's members, made once the server
  // holds it (an add, a rename, a cleared name, a mint): `members` are the members as
  // the server answered the write, with the stamp it gave them. A copy built from what
  // was typed carries none and would lose to every list. The write is not followed by a
  // read of its own: the server announces it to every device, this one included, which
  // reads then, and the stamp keeps a read that began before the write from undoing it.
  // It is not a list that was read, so the wait for the first list is not ended by it:
  // a name typed next still needs the rest of the team. An answer a handler of another
  // team brings, after the editor was given this one, is merged into no list.
  const changeMembers = (members) => {
    if (teamKey !== squadTeam.current) return;
    setSquad(members);
  };

  // The server renamed a team member (a cleared name is the name ""): `member` is the
  // member as it answered. It is merged in, and the name of the copy that stands (a
  // list already shown may be newer than this answer, and then it is that list's) is
  // shown on every position that holds the member and on the baseline, so a rename
  // alone makes no edit of the lineup (the server rewrote the names of every stored
  // lineup by id). The positions are read as they are now, not as the handler that asks
  // closed over before its round trip: the pickers stay interactive meanwhile, and a
  // position moved meanwhile would otherwise take the new name while the position it
  // moved to kept the old spelling.
  const memberRenamed = (member) => {
    if (teamKey !== squadTeam.current) return;
    setSquad([member]);
    const { id } = member;
    const name = (squadRef.current.find((m) => m.id === id) || member).name || '';
    setValues((v) => {
      const next = { ...v };
      const ids = memberIdsRef.current;
      Object.keys(ids).forEach((key) => { if (ids[key] === id) next[key] = name; });
      return next;
    });
    setBaseline((b) => {
      const positions = { ...b.positions };
      Object.keys(b.memberIds).forEach((key) => { if (b.memberIds[key] === id) positions[key] = name; });
      return { ...b, positions };
    });
  };

  const readLineup = () => (matchId
    ? resolveMatchLineup(compId, teamId, matchId, window.API, { throwOnError: true })
    : window.API.fetchTeamLineup(compId, teamId, STARTING_ROUND));

  // Shows what was read (null when nothing applies: a fresh form) as the loaded
  // state of the form.
  const adopt = (lineup) => {
    const loaded = lineupFields(lineup, positionKeys);
    setValues(loaded.positions);
    setMemberIds(loaded.memberIds);
    setBaseline(loaded);
    setSource(lineupSourceOf(lineup));
    setLoadError('');
    // Last: the draft sees the lineup as loaded only once everything above is set.
    setLoadedKey(draftKey);
  };

  // Nothing is known of the lineup: an empty form that cannot be saved.
  const forget = () => {
    setValues(blankNames(positionKeys));
    setMemberIds({});
    setBaseline(EMPTY_SIDE);
    setSource(null);
    setLoadedKey('');
  };

  const load = async () => {
    const mine = ++attempt.current;
    reading.current = mine;
    // This read is of the lineup as it is now: a change announced before it began
    // has nothing left to add.
    followDue.current = false;
    try {
      const lineup = await readLineup();
      if (attempt.current === mine) adopt(lineup);
    } catch (e) {
      if (attempt.current === mine) {
        setLoadError(lineupReadFailure(e));
        followDue.current = false;
      }
    } finally {
      if (attempt.current === mine) {
        reading.current = null;
        setLoading(false);
      }
    }
  };

  // A save of this lineup that is still queued (offline, or refused for now)
  // replays after anything sent now: removing the lineup would be undone by it.
  // The API answers whether one is queued, which is all that matters here.
  const queuedNow = () => {
    const api = window.API;
    return !!(api && typeof api.queuedLineupSave === 'function' && compId && teamId
      && api.queuedLineupSave(compId, teamId, matchId ? { matchId } : { round: STARTING_ROUND }));
  };

  const read = loadedKey === draftKey;
  const current = { positions: values, memberIds };
  const dirty = changedLineupPositions(baseline, current, positionKeys).length > 0;

  // Another device changed a lineup of this competition: read this one again and
  // show it, unless the operator has edits on it or a removal is out, which are
  // theirs and stay (the draft covers a reopen), or a read of the lineup is still
  // out, which it waits for (see `reading`). This editor's own save is held off
  // the same way, since the form stays dirty until confirmSaved. A read of a
  // lineup already shown that fails leaves it as it is, raising no problem over a
  // form that can still be used. The team's members are read with it, so a member
  // another device created since is in the list when the lineup names them. The
  // list is not the operator's edit: it is shown with the lineup, and on its own
  // when the lineup is not shown (the form was edited meanwhile, or its read
  // failed). Their read has the deadline of any bounded request, and one that fails
  // or is not answered shows nothing and changes no flag. Both are dropped when
  // this lineup read was ended meanwhile (see attempt).
  const follow = async () => {
    const shown = read;
    const mine = ++attempt.current;
    const [lineup, members] = await Promise.allSettled([
      readLineup(),
      withinDeadline(readMembers(compId, teamId, password), FETCH_TIMEOUT_MS),
    ]);
    if (attempt.current !== mine) return;
    showMembers(teamKey, members.value);
    if (lineup.status === 'rejected') {
      if (!shown) setLoadError(lineupReadFailure(lineup.reason));
    } else if (!live.current.touched) {
      adopt(lineup.value);
    }
    setLoading(false);
  };

  // The team's members, read again on their own: the list is not the operator's edit,
  // so a form that holds edits or a removal still shows a member another device
  // created, and a typed name finds that member instead of minting it a second time.
  // The read begins now, so it holds what the server holds now. It settles once the list
  // is merged, or when the read failed or was not answered by the deadline of any
  // bounded request, which shows nothing and keeps the list as it was.
  const readMembersAgain = async () => {
    showMembers(teamKey, await withinDeadline(readMembers(compId, teamId, password), FETCH_TIMEOUT_MS));
  };

  // The effects below key on the lineup alone and call these as of the render they
  // run in.
  const live = useRef(null);
  live.current = { load, follow, readMembersAgain, queuedNow, touched: dirty || removing };

  useEffect(() => {
    if (!compId || !teamId) {
      setLoading(false);
      return undefined;
    }
    live.current.load();
    return () => { attempt.current += 1; };
  }, [compId, teamId, matchId]);

  useEffect(() => {
    if (!compId || !teamId) return undefined;
    const onUpdated = (e) => {
      if (e.detail && e.detail.competitionId !== compId) return;
      // A change to another team's lineup or members is none of this team's: an
      // announcement that names no team is read as a change to every team's.
      if (e.detail && e.detail.teamId && e.detail.teamId !== teamId) return;
      if (reading.current === attempt.current) followDue.current = true;
      else if (live.current.touched) live.current.readMembersAgain();
      else live.current.follow();
    };
    window.addEventListener('lineup-updated', onUpdated);
    return () => window.removeEventListener('lineup-updated', onUpdated);
  }, [compId, teamId, matchId]);

  // A change announced while the lineup was being read is followed once that read has
  // shown it. It starts here, not inside the read: `live` holds the render that shows
  // the lineup, so the follow knows it is shown and a failure of its own raises no
  // problem over it.
  useEffect(() => {
    if (!read || !followDue.current) return;
    followDue.current = false;
    if (!live.current.touched) live.current.follow();
  }, [read]);

  // The list shown is the one of the team it was read for: giving the editor another
  // team drops it, so its members are not merged into the new team's list.
  useEffect(() => {
    if (squadTeam.current === teamKey) return;
    squadTeam.current = teamKey;
    squadRef.current = [];
    setSquadList([]);
    firstMembers.current = newWait();
  }, [teamKey]);

  // The team's members, independent of the lineup read: one that fails must not
  // block loading or saving the lineup. `password` is a dependency, not just a
  // closure read, and a read that succeeds clears the flag a failed one raised:
  // the re-auth modal is a SIBLING of the admin app, so a 401 here unmounts
  // nothing and neither compId nor teamId ever changes. Without both halves one
  // 401 would leave the pickers empty for the editor's whole life, and every typed
  // name would mint a new member instead of resolving to the one already on the
  // team. Changing the team or the password, or leaving, ends this read: it was
  // made for what no longer applies. Its failure flags the members as unavailable
  // unless a read's list is shown for the team by then.
  useEffect(() => {
    if (!compId || !teamId) return undefined;
    let ended = false;
    readMembers(compId, teamId, password).then((members) => {
      if (ended) return;
      if (members) {
        showMembers(teamKey, members);
      } else if (!firstMembers.current.listed) {
        setSquadUnavailable(true);
        firstMembers.current.fail();
      }
    });
    return () => { ended = true; };
  }, [compId, teamId, password, teamKey]);

  const retry = () => {
    setLoadError('');
    setLoading(true);
    load();
  };

  const [saveQueued, setSaveQueued] = useState(queuedNow);
  useEffect(() => {
    const refresh = () => {
      const now = live.current.queuedNow();
      setSaveQueued((was) => (was === now ? was : now));
    };
    refresh();
    const offs = [];
    if (typeof window.subscribeSyncStatus === 'function') offs.push(window.subscribeSyncStatus(refresh));
    if (typeof window.subscribeUnsentWrites === 'function') offs.push(window.subscribeUnsentWrites(refresh));
    return () => offs.forEach((off) => { if (typeof off === 'function') off(); });
  }, [compId, teamId, matchId]);

  // Unsaved picks survive a reload, the app's Back and closing the editor; a
  // restored draft is only shown, never saved.
  const draft = useLineupDraft({
    key: draftKey,
    ready: read,
    baseline,
    current,
    positionKeys,
    onRestore: (side) => { setValues(side.positions); setMemberIds(side.memberIds); },
  });

  // What a Save carries (operator decision 2026-10-07, "Only changed positions"): the
  // form as shown and `changed`, the positions the operator changed since the lineup
  // was read or last confirmed. The server puts those on the lineup it holds when the
  // save arrives, so a position the operator left alone keeps whatever another device
  // made of it, and nothing is asked of the server here: a Save with no connection is
  // built, and queued, like any other, and a position put back to what it was loaded
  // with is no change at all.
  //
  // The team's members are not read for it either. A member another device creates is
  // announced to this one (lineup-updated, which names the team), and the editor reads
  // the members again for it, edits or not, so a typed name is resolved against a list
  // that holds them; and a name the list missed is refused by the server as a second
  // member of that name, which the editor's warning says. A read made here would hold a
  // Save up to the deadline of any request on a link that does not answer. The server's
  // answer to a save that does go through is shown after it (confirmSaved).
  const lineupToSave = () => ({
    positions: values, memberIds, changed: changedLineupPositions(baseline, current, positionKeys),
  });

  // What the server answered to a save is what it holds now, the whole lineup: a
  // position this save left alone shows what another device made of it, with its
  // own member id.
  const confirmSaved = ({ positions, memberIds: ids }) => {
    // A read begun before the save would answer with the lineup from before it.
    attempt.current += 1;
    const stored = lineupFields({ positions, memberIds: ids }, positionKeys);
    setValues(stored.positions);
    setMemberIds(stored.memberIds);
    setBaseline(stored);
    if (matchId) setSource({ matchId });
    draft.resolved();
  };

  // Removes the match's own lineup, then shows what the match carries without it. A
  // removal the server refuses changes nothing. Once it is done the draft goes
  // (it was made against the lineup that has just gone), and a read of what is
  // carried now that fails leaves an empty form that cannot be saved, never the
  // removed lineup shown as the match's own.
  const dropOwnLineup = async () => {
    if (saveQueued) return;
    const ok = await window.confirmDialog(previousLineupConfirm(matchLabel, teamName));
    if (!ok) return;
    setRemoving(true);
    setError('');
    setWarning('');
    // A read begun before the removal would answer with the lineup that is going.
    attempt.current += 1;
    try {
      try {
        await window.API.deleteMatchLineup(compId, teamId, matchId, password);
      } catch (e) {
        setError(e?.message || "Failed to use the previous match's lineup");
        return;
      }
      removeDraft(draftKey);
      draft.resolved();
      try {
        adopt(await readLineup());
      } catch (_e) {
        forget();
        setLoadError(REMOVED_UNREAD_NOTICE);
      }
    } finally {
      setRemoving(false);
    }
  };

  return {
    values, setValues, memberIds, setMemberIds, memberIdsRef, baseline, source,
    loading, loadError, retry, read, dirty, canSave: read && dirty,
    saveTitle: read && !dirty ? NO_CHANGES_TITLE : undefined,
    // A form that holds nothing read has no draft to offer back or discard.
    draft: read ? draft : { ...draft, restored: false, stale: null },
    error, setError, warning, setWarning,
    squad, changeMembers, memberRenamed, squadRef, squadUnavailable, waitForMembers: () => firstMembers.current.pending(),
    removing, dropOwnLineup, saveQueued, lineupToSave, confirmSaved,
  };
}

// LineupSourceLine: where the lineup shown was saved, and for a match's own
// lineup the button that gives it up (the match then carries its team's previous
// lineup). The button waits while a save of that lineup is still queued, and
// says why in a line, since a title never shows on a touchscreen. Shows nothing
// before the lineup was read: it would name a source the form does not hold.
// `form` is useLineupForm's answer; `busy` is the editor's own (a save is out).
export function LineupSourceLine({ form, matchId, allMatches, busy, testId }) {
  if (!form.read) return null;
  const own = isOwnLineup(form.source, matchId);
  return (
    <div className="lineup-source">
      <span data-testid={testId} className={own ? 'lineup-source__label lineup-source__label--own' : 'lineup-source__label'}>
        {lineupSourceLabel(form.source, matchId, allMatches)}
      </span>
      {own && (
        <button type="button" className="btn btn--sm lineup-source__use"
          onClick={form.dropOwnLineup} disabled={busy || form.removing || form.saveQueued}>
          {PREVIOUS_LINEUP_LABEL}
        </button>
      )}
      {own && form.saveQueued && (
        <div className="lineup-source__why" role="status">{SAVE_QUEUED_REASON}</div>
      )}
    </div>
  );
}

// LineupProblem: why the lineup could not be read, kept until a read succeeds,
// with the way to try again. Nothing is saved meanwhile (useLineupForm's `read`).
export function LineupProblem({ form, testId }) {
  if (!form.loadError) return null;
  return (
    <div className="alert alert--error lineup-problem" role="alert" data-testid={testId}>
      <span>{form.loadError}</span>
      <button type="button" className="btn btn--sm" onClick={form.retry} disabled={form.removing}>Try again</button>
    </div>
  );
}
