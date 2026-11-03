// The one home of the held-writes list, shared by the admin topbar and the
// public page.

import { heldWriteLine, heldWriteDiscardConfirm, matchLabel, heldLineupLabel, HELD_WRITES_TITLE, HELD_WRITES_EMPTY, HELD_WRITE_DISCARD_ONE_LABEL, queuedWritesNoun, isFinishedResultWrite } from './write_result.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
import { useLatestRef } from './latest_ref.jsx';

const { useState, useEffect, useRef } = React;

// useHeldWritesRefresh: runs `refresh` whenever the sync status or this tab's
// unsent-write count moves, until unmount (never after it). Returns the
// mounted flag for a caller whose own async work must check it too.
function useHeldWritesRefresh(refresh) {
  const mountedRef = useRef(true);
  const refreshRef = useLatestRef(refresh);
  useEffect(() => () => { mountedRef.current = false; }, []);
  useEffect(() => {
    const onChange = () => { if (mountedRef.current) refreshRef.current(); };
    const offs = [window.subscribeSyncStatus, window.subscribeUnsentWrites]
      .filter((subscribe) => typeof subscribe === 'function')
      .map((subscribe) => subscribe(onChange));
    return () => offs.forEach((off) => { if (typeof off === 'function') off(); });
  }, [refreshRef]);
  return mountedRef;
}

// heldWriteWhere: which match (or team) one held write is about, named the
// way the score list names it (scoreRowMatchLabel) when the competition's
// matches are loaded, else by the match's number or id (matchLabel). The
// competition is named too: the list spans every competition on the device.
export function heldWriteWhere(held, competitions) {
  const comp = (competitions || []).find((c) => c && c.id === held.compID) || null;
  const compName = (comp && comp.name) || held.compID || '';
  let what;
  if (held.kind === 'lineup') {
    // A competition's roster is `players` (normalizeViewerCompItem); a team
    // is one entry in it, found by its participant id.
    const team = comp && Array.isArray(comp.players)
      ? comp.players.find((p) => p && p.id === held.teamId) : null;
    what = heldLineupLabel(team && team.name);
  } else {
    const m = comp && typeof window.compMatches === 'function'
      ? (window.compMatches(comp) || []).find((x) => x && x.id === held.matchID) : null;
    what = (m && scoreRowMatchLabel(m)) || matchLabel(m ? { number: m.matchNumber, id: m.id } : { id: held.matchID });
  }
  return compName && what ? `${compName} · ${what}` : (compName || what);
}

// HeldWritesPanel: every write held on this device, opened from the topbar's
// held-writes indicator. A write the server keeps refusing can be discarded
// here, one at a time and after a confirm, whatever it is: a running
// autosave or a lineup save has no editor of its own to
// offer it (the score editors' banner offers it for a result). A write that is
// only waiting for the connection, or for a sign-in, is listed without one: it
// lands on its own. Re-read whenever the sync status or the held count moves.
export function HeldWritesPanel({ competitions, onClose }) {
  const Modal = typeof window !== 'undefined' ? window.Modal : null;
  const api = typeof window !== 'undefined' ? window.API : null;
  const read = () => (api && typeof api.heldWrites === 'function' ? api.heldWrites() : []);
  const [items, setItems] = useState(read);
  const [busyKey, setBusyKey] = useState(null);
  const mountedRef = useHeldWritesRefresh(() => setItems(read()));
  const discard = async (held) => {
    setBusyKey(held.key);
    try {
      const ok = typeof window.confirmDialog === 'function'
        ? await window.confirmDialog(heldWriteDiscardConfirm(held))
        : false;
      if (!ok || !mountedRef.current) return;
      if (api && typeof api.discardHeldWrite === 'function') {
        api.discardHeldWrite(held.key);
      }
      setItems(read());
    } finally {
      if (mountedRef.current) setBusyKey(null);
    }
  };
  if (!Modal) return null;
  return (
    <Modal title={HELD_WRITES_TITLE} onClose={onClose}>
      {items.length === 0 ? (
        <p className="held-writes__empty">{HELD_WRITES_EMPTY}</p>
      ) : (
        <ul className="held-writes" data-testid="held-writes">
          {items.map((held) => (
            <li key={held.key} className="held-writes__item" data-testid="held-write">
              <div className="held-writes__text">
                <div className="held-writes__where">{heldWriteWhere(held, competitions)}</div>
                <div className={`held-writes__state${held.keepsFailing ? ' held-writes__state--error' : ''}`}>
                  {heldWriteLine(held)}
                </div>
              </div>
              {held.keepsFailing && (
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  data-testid="held-write-discard-one"
                  disabled={busyKey !== null}
                  onClick={() => discard(held)}
                >{HELD_WRITE_DISCARD_ONE_LABEL}</button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}

function getHeldBadgeText() {
  const api = typeof window !== 'undefined' ? window.API : null;
  if (!api || typeof api.heldWrites !== 'function') return null;
  const list = api.heldWrites();
  if (!Array.isArray(list)) return null;
  const held = list.filter((item) => item && item.held === true);
  if (held.length === 0) return null;
  const finished = held.filter(isFinishedResultWrite).length;
  const { n, noun } = queuedWritesNoun(finished, held.length, { finished: false });
  return `${n} ${noun} not sent`;
}

export function PublicHeldWrites({ competitions }) {
  const [badgeText, setBadgeText] = useState(getHeldBadgeText);
  const [open, setOpen] = useState(false);
  useHeldWritesRefresh(() => setBadgeText(getHeldBadgeText()));

  if (!badgeText && !open) return null;

  return (
    <>
      {badgeText && (
        <button
          type="button"
          className="public-held"
          data-testid="public-held"
          aria-haspopup="dialog"
          onClick={() => setOpen(true)}
        >
          <span aria-hidden="true" className="public-held__dot" />
          <span role="status" aria-live="polite">
            {badgeText}
          </span>
        </button>
      )}
      {open && (
        <HeldWritesPanel
          competitions={competitions}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}
