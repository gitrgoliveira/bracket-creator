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
// Its one import is lineup_resolver.jsx, the owner of the lineup read and of
// which positions differ. The hooks read the React global at call time, as
// tap_guard.jsx does.
import {
  changedLineupPositions, lineupFields, lineupSourceOf, lineupSourceLabel, isOwnLineup, resolveMatchLineup,
  previousLineupConfirm, PREVIOUS_LINEUP_LABEL, SAVE_QUEUED_REASON, REMOVED_UNREAD_NOTICE, STARTING_ROUND,
  lineupReadFailure,
} from './lineup_resolver.jsx';

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
// Returns { restored, stale, discard }: `restored` while a restored draft is still
// unsaved; `stale` is { names } when the lineup changed meanwhile and the draft
// was dropped rather than overwrite it.
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
      const draft = readDraft(key);
      if (!draft) {
        // None stored (a no-op), or one that cannot be read: it goes.
        removeDraft(key);
        return;
      }
      const expired = Date.now() - draft.savedAt > MAX_AGE_MS;
      if (expired || changedLineupPositions(draft.baseline, draft.current, keys).length === 0
        || changedLineupPositions(draft.current, loaded, keys).length === 0) {
        // Too old, nothing in it, or the saved lineup already holds all of it.
        removeDraft(key);
      } else if (changedLineupPositions(draft.baseline, loaded, keys).length > 0) {
        // The lineup changed meanwhile (another device): never overwrite it.
        removeDraft(key);
        setOutcome({ key, restored: false, stale: { names: draftNames(draft, keys) } });
      } else {
        restore(lineupFields(draft.current, keys));
        setOutcome({ key, restored: true, stale: null });
      }
      return;
    }
    // "Not restored" is about the lineup as it was opened: once the operator has
    // changed it, or saved it, the notice no longer says anything.
    if (changedLineupPositions(loaded, shown, keys).length > 0) {
      writeDraft(key, { savedAt: Date.now(), baseline: loaded, current: shown });
      setOutcome((o) => (o.stale ? { ...o, stale: null } : o));
    } else {
      removeDraft(key);
      setOutcome((o) => (o.restored || o.stale ? { ...o, restored: false, stale: null } : o));
    }
  }, [key, ready, signature]);

  const discard = useCallback(() => {
    const { baseFields: loaded, positionKeys: keys, onRestore: restore } = latest.current;
    removeDraft(key);
    restore(lineupFields(loaded, keys));
    setOutcome((o) => (o.restored ? { ...o, restored: false } : o));
  }, [key]);

  const mine = outcome.key === key ? outcome : NOTHING;
  return { restored: mine.restored, stale: mine.stale, discard };
}

// LineupDraftNotice: the amber notice under an editor's source label, from what
// useLineupDraft returned. A restored draft always offers Discard; a draft dropped
// because the lineup changed meanwhile names what was not restored. Discard is a
// plain .btn, so the coarse-pointer floor reaches it through the class.
export function LineupDraftNotice({ draft, testId }) {
  const { restored, stale, discard } = draft;
  if (!restored && !stale) return null;
  const names = stale ? stale.names.join(', ') : '';
  return (
    <div className="alert alert--warn lineup-draft-note" role="status" data-testid={testId}>
      <span>{restored ? RESTORED_NOTICE : (names ? `${NOT_RESTORED_NOTICE}: ${names}` : NOT_RESTORED_NOTICE)}</span>
      {restored && <button type="button" className="btn btn--sm" onClick={discard}>Discard</button>}
    </div>
  );
}

const EMPTY_SIDE = { positions: {}, memberIds: {} };
const NO_CHANGES_TITLE = 'No changes to save';

// A name box per position, all empty: the form of a lineup nothing is known of.
const blankNames = (positionKeys) => Object.fromEntries(positionKeys.map((key) => [key, '']));

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
// and gives the editor
//   confirmSaved(lineup)  after the server confirmed a save: that is now the
//                         baseline, and a match's own lineup
//   dropOwnLineup()       confirm, then remove a match's own lineup, so the match
//                         carries its team's previous one, and show that one
//   removeStored(remove, failure)  the same for any other stored lineup
//   retry()               read again ("Try again")
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
  // The read whose answer may still be shown: another lineup, or leaving, ends it.
  const attempt = useRef(0);

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
    try {
      const lineup = await readLineup();
      if (attempt.current === mine) adopt(lineup);
    } catch (e) {
      if (attempt.current === mine) setLoadError(lineupReadFailure(e));
    } finally {
      if (attempt.current === mine) setLoading(false);
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
  // The effects below key on the lineup alone and call these as of the render they
  // run in.
  const live = useRef(null);
  live.current = { load, queuedNow };

  useEffect(() => {
    if (!compId || !teamId) {
      setLoading(false);
      return undefined;
    }
    live.current.load();
    return () => { attempt.current += 1; };
  }, [compId, teamId, matchId]);

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

  const read = loadedKey === draftKey;
  const current = { positions: values, memberIds };
  const dirty = changedLineupPositions(baseline, current, positionKeys).length > 0;

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

  // What the server answered to a save is what it holds now.
  const confirmSaved = ({ positions, memberIds: ids }) => {
    const stored = lineupFields({ positions, memberIds: ids }, positionKeys);
    setValues(stored.positions);
    setMemberIds(stored.memberIds);
    setBaseline(stored);
    if (matchId) setSource({ matchId });
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
    try {
      try {
        await remove();
      } catch (e) {
        setError(e?.message || failure);
        return;
      }
      removeDraft(draftKey);
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
    removing, removeStored, dropOwnLineup, saveQueued, confirmSaved,
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
