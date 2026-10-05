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
// A leaf: its one import is lineup_resolver.jsx, the owner of which positions
// differ. The hook reads the React global at call time, as tap_guard.jsx does.
import { changedLineupPositions, lineupFields } from './lineup_resolver.jsx';

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
