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
// The rules for the team's members (mergeMembers, changedMembers, takeMembers) are
// exported too: the team score sheet writes members from its bout rows and keeps its
// own lists, and asks the same owner what a list that arrives does to them.
//
// Its imports are lineup_resolver.jsx, the owner of the lineup read, of which
// positions differ and of what a Save writes, and write_result.jsx, the leaf that
// owns the deadline a bounded wait is given. The hooks read the React global at
// call time, as tap_guard.jsx does.
import {
  changedLineupPositions, composeLineupSave, lineupFields, lineupSourceOf, lineupSourceLabel, isOwnLineup, resolveMatchLineup,
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
        || changedLineupPositions(draft.current, loaded, keys).length === 0) {
        // Too old, nothing in it, or the saved lineup already holds all of it.
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

// The team's members (the API answers every team's, keyed by id), or null when
// they cannot be read: whoever asks keeps the list it has.
async function readMembers(compId, teamId, password) {
  try {
    const squads = await window.API.fetchSquads(compId, password);
    return (squads && squads[teamId]) || [];
  } catch (_e) {
    return null;
  }
}

// The team's members once `arriving` is shown beside `shown`: every member of the
// list that arrives, as it holds them, and every member shown that it lacks, in
// member number order, the order the pickers list them in. No screen removes a
// member, so one a list lacks was added after that list was read, or after the list
// an editor built its update on, and was not taken away.
export function mergeMembers(shown, arriving) {
  const arrived = new Set(arriving.map((m) => m.id));
  return [...arriving, ...shown.filter((m) => !arrived.has(m.id))]
    .sort((a, b) => (a.index || 0) - (b.index || 0));
}

// The members of `after` that `before` lacks, or holds under another name: what a
// change that built `after` out of `before` wrote.
export function changedMembers(before, after) {
  const was = new Map(before.map((m) => [m.id, m]));
  return after.filter((m) => !was.has(m.id) || (was.get(m.id).name || '') !== (m.name || ''));
}

// A list of the team's members arrives (a read's answer, the host's own copy) at an
// editor that writes members itself. `pending` holds what the editor wrote that no
// list that arrived has shown yet, by member id: the name it gave. A list can predate
// a write, so the write stands over it: the member the editor named stays named, and
// one it added stays (the list shown holds it, which mergeMembers keeps). A list that
// shows the name is the server caught up, so the write is done with and a change to
// that member made elsewhere shows from then on. Returns the members to show and the
// writes still pending.
export function takeMembers(shown, arriving, pending) {
  const standing = {};
  const members = mergeMembers(shown, arriving).map((m) => {
    const wrote = pending[m.id];
    if (wrote === undefined) return m;
    const theirs = arriving.find((a) => a.id === m.id);
    if (theirs && (theirs.name || '') === wrote) return m;
    standing[m.id] = wrote;
    return { ...m, name: wrote };
  });
  return { members, pending: standing };
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
//   squad               the team's members as the API answers them (the editors
//                       set it after a rename or a mint), a list that arrives
//                       being merged with the one shown by member id so that none
//                       is lost, squadRef, the same list for a handler that
//                       resumes after lineupToSave, and squadUnavailable, that
//                       they could not be read
// and gives the editor
//   lineupToSave()        what a Save writes: the lineup as stored now with the
//                         operator's changes on it, `{ positions, memberIds,
//                         changed }`. It is read again for it, under the deadline
//                         a bounded request has, and taken from the baseline
//                         when it cannot be read, so an offline save is written
//                         and queued as it always was. What it read on positions
//                         the operator left alone is shown, with their changes,
//                         and the team's members are read again then, which the
//                         Save waits for (up to the deadline of any bounded
//                         request) so that nothing of that read lands after the
//                         Save's own writes
//   confirmSaved(lineup)  after the server confirmed a save: that is now the
//                         baseline, and a match's own lineup, and what a draft
//                         could not restore no longer applies
//   dropOwnLineup()       confirm, then remove a match's own lineup, so the match
//                         carries its team's previous one, and show that one
//   removeStored(remove, failure)  the same for any other stored lineup
//   retry()               read again ("Try again")
// A lineup change announced for the competition (the lineup-updated event) is
// followed: the lineup is read again and shown, unless the form has edits or a
// removal is out (one announced while a read of the lineup is out waits for that
// read, and is followed once it has shown the lineup, or dropped when it fails),
// and the team's members are read again with it, so a member another device
// created is in the list when the lineup names them. They are shown whether or
// not the lineup is (the list is not the operator's edit); a members read that
// fails or is not answered shows nothing and keeps the list as it was.
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
  // list as it is and flags that it could not be read, unless a read that began
  // after it is shown: a lineup is still read and saved without it. A read made
  // later that fails shows nothing and changes no flag.
  const [squad, setSquadList] = useState([]);
  const [squadUnavailable, setSquadUnavailable] = useState(false);
  // squadRef mirrors squad for the editor's save, which resolves a typed name after
  // it awaits lineupToSave. That Save may have read the team's members again and
  // brought in a member another device created, which the list the save closed over
  // when Save was tapped lacks: the resolver would mint the member a second time,
  // and a member can never be removed. A list that arrives (setSquad, below) is put
  // on it as it is shown, so it holds that list when lineupToSave returns, whether
  // or not the render that shows it has run.
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
  // The same for the team's members, which four reads ask for (the one made when
  // the editor opens, the one a followed lineup makes, the one a Save makes when
  // it shows a lineup another device changed, and the one an editor's own change of
  // the members makes) and which need not answer in the order they began. Each takes
  // the next number from membersRead as it begins; membersShown is the number of the
  // read whose list is shown. An answer is shown only when its read began after that
  // one, so an older answer never replaces a newer one, and a read that shows nothing
  // (it failed, was not answered in time, or was dropped) never stops an older read
  // still out from landing. An editor's own change ends every read begun before it
  // (see changeMembers).
  const membersRead = useRef(0);
  const membersShown = useRef(0);

  // The ONE door every list of the team's members comes through: showMembers, below,
  // for a read's answer, and changeMembers for an editor's own change. It may be
  // passed a function, which is given the list shown. The list is merged with the one
  // shown (see mergeMembers), so no member is lost to a read begun before they were
  // added or to a change built on an older list, and it is on squadRef at once. It is
  // not handed to the editors: what an editor changes goes through changeMembers,
  // which also settles the reads still out.
  const setSquad = (next) => {
    const merged = mergeMembers(squadRef.current, typeof next === 'function' ? next(squadRef.current) : next);
    squadRef.current = merged;
    setSquadList(merged);
  };

  // Shows what a members read answered: a list, not a failure (null) or the
  // deadline (TIMED_OUT), from a read that began after the one shown.
  const showMembers = (mine, members) => {
    if (!Array.isArray(members) || mine <= membersShown.current) return;
    membersShown.current = mine;
    setSquad(members);
    setSquadUnavailable(false);
  };

  // The ONE door for an editor's own change to the team's members, made once the
  // server holds it (an add, a rename, a cleared name, a mint; it may pass a function,
  // which is given the list shown). A members read begun before it can answer after
  // it with the list from before, and an arriving list wins over the one shown (see
  // mergeMembers): a rename would be undone, and the new name typed next would find
  // nobody, so it would be put on an unnamed member or minted as a second one. Once a
  // list is shown, every read begun so far is therefore ended. While none is (the
  // read made as the editor opened is still out, and a Save minted a typed name
  // meanwhile), that read is not ended: it holds the rest of the team, and the
  // change has nothing of it to undo. Either way the members are read again: that
  // read begins after the change, so it holds it, and a member another device added
  // meanwhile still arrives. One that fails shows nothing and changes no flag.
  const changeMembers = (next) => {
    const listed = squadRef.current.length > 0;
    setSquad(next);
    if (listed) membersShown.current = membersRead.current;
    const mine = ++membersRead.current;
    readMembers(compId, teamId, password).then((members) => showMembers(mine, members));
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
  // The API answers with the lineup that save would write, or null: only whether
  // one is queued matters here.
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
    const membersMine = ++membersRead.current;
    const [lineup, members] = await Promise.allSettled([
      readLineup(),
      withinDeadline(readMembers(compId, teamId, password), FETCH_TIMEOUT_MS),
    ]);
    if (attempt.current !== mine) return;
    showMembers(membersMine, members.value);
    if (lineup.status === 'rejected') {
      if (!shown) setLoadError(lineupReadFailure(lineup.reason));
    } else if (!live.current.touched) {
      adopt(lineup.value);
    }
    setLoading(false);
  };

  // The effects below key on the lineup alone and call these as of the render they
  // run in.
  const live = useRef(null);
  live.current = { load, follow, queuedNow, touched: dirty || removing };

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
      if (reading.current === attempt.current) followDue.current = true;
      else if (!live.current.touched) live.current.follow();
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
  const squadTeam = useRef(`${compId}/${teamId}`);
  useEffect(() => {
    const team = `${compId}/${teamId}`;
    if (squadTeam.current === team) return;
    squadTeam.current = team;
    squadRef.current = [];
    setSquadList([]);
  }, [compId, teamId]);

  // The team's members, independent of the lineup read: one that fails must not
  // block loading or saving the lineup. `password` is a dependency, not just a
  // closure read, and a read that succeeds clears the flag a failed one raised:
  // the re-auth modal is a SIBLING of the admin app, so a 401 here unmounts
  // nothing and neither compId nor teamId ever changes. Without both halves one
  // 401 would leave the pickers empty for the editor's whole life, and every typed
  // name would mint a new member instead of resolving to the one already on the
  // team. Changing the team or the password, or leaving, ends every members read
  // begun so far, this one and any a followed lineup, a Save or an editor's own change
  // made: they were made for what no longer applies.
  useEffect(() => {
    if (!compId || !teamId) return undefined;
    const mine = ++membersRead.current;
    readMembers(compId, teamId, password).then((members) => {
      if (members) showMembers(mine, members);
      else if (mine > membersShown.current) setSquadUnavailable(true);
    });
    return () => { membersShown.current = membersRead.current; };
  }, [compId, teamId, password]);

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

  // What a Save writes (operator decision 2026-10-05): the lineup as stored now with
  // the operator's changes on it, not the form restated, which would put back on
  // every position the operator left alone the value the form was read with, over
  // a change another device made since. The lineup is read again for it, and from
  // the baseline (the lineup as loaded or last confirmed) when that read fails or is
  // not answered in time, so an offline save is written, and queued, as it always
  // was. `changed` names the positions the operator changed.
  //
  // A position the operator left alone that the read holds differently was changed
  // on another device. It is shown now, whether or not the save then goes through:
  // the read is the baseline, the form is the composed lineup (the operator's changes
  // kept) and the source follows what was read, so a save that is refused leaves the
  // conflict on screen, and putting such a position back to what it was loaded with
  // is a change again. The team's members are read again then too, so a member
  // another device created is in the list the pickers offer, and in squadRef, which
  // the editor's save resolves a typed name against. The Save waits for that read,
  // up to the deadline of any bounded request, so that nothing of it lands after the
  // Save's own writes (a mint among them) and replaces the list they updated; a read
  // that fails or is not answered changes nothing. A lineup read still out when the
  // Save begins is ended, so a followed lineup's members cannot land after those
  // writes either. The server's answer to a save that does go through is shown
  // after it (confirmSaved).
  const lineupToSave = async () => {
    attempt.current += 1;
    let stored = baseline;
    let answered = false;
    try {
      const fresh = await withinDeadline(readLineup(), FETCH_TIMEOUT_MS);
      answered = fresh !== TIMED_OUT;
      if (answered) stored = fresh;
    } catch (_e) {
      // The server cannot be asked: the lineup as loaded stands in for it.
    }
    const composed = composeLineupSave(baseline, current, stored, positionKeys);
    const changed = changedLineupPositions(baseline, current, positionKeys);
    if (answered && changedLineupPositions(baseline, stored, positionKeys).some((key) => !changed.includes(key))) {
      setBaseline(lineupFields(stored, positionKeys));
      setValues(composed.positions);
      setMemberIds(composed.memberIds);
      setSource(lineupSourceOf(stored));
      const membersMine = ++membersRead.current;
      showMembers(membersMine, await withinDeadline(readMembers(compId, teamId, password), FETCH_TIMEOUT_MS));
    }
    return { ...composed, changed };
  };

  // What the server answered to a save is what it holds now.
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

  // Removes a stored lineup, then shows what the match carries without it. A
  // removal the server refuses changes nothing. Once it is done the draft goes
  // (it was made against the lineup that has just gone), and a read of what is
  // carried now that fails leaves an empty form that cannot be saved, never the
  // removed lineup shown as the match's own.
  const removeStored = async (remove, failure) => {
    setRemoving(true);
    setError('');
    setWarning('');
    // A read begun before the removal would answer with the lineup that is going.
    attempt.current += 1;
    try {
      try {
        await remove();
      } catch (e) {
        setError(e?.message || failure);
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

  const dropOwnLineup = async () => {
    if (saveQueued) return;
    const ok = await window.confirmDialog(previousLineupConfirm(matchLabel, teamName));
    if (!ok) return;
    await removeStored(
      () => window.API.deleteMatchLineup(compId, teamId, matchId, password),
      "Failed to use the previous match's lineup",
    );
  };

  return {
    values, setValues, memberIds, setMemberIds, memberIdsRef, baseline, setBaseline, source,
    loading, loadError, retry, read, dirty, canSave: read && dirty,
    saveTitle: read && !dirty ? NO_CHANGES_TITLE : undefined,
    // A form that holds nothing read has no draft to offer back or discard.
    draft: read ? draft : { ...draft, restored: false, stale: null },
    error, setError, warning, setWarning,
    squad, changeMembers, squadRef, squadUnavailable,
    removing, removeStored, dropOwnLineup, saveQueued, lineupToSave, confirmSaved,
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
