// The one home of the held-writes list, shared by the admin topbar and the
// public page.

import { heldWriteLine, heldWriteDiscardConfirm, matchLabel, heldLineupLabel, HELD_WRITES_TITLE, HELD_WRITES_EMPTY, HELD_WRITE_DISCARD_ONE_LABEL } from './write_result.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';

const { useState, useEffect, useRef } = React;

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
// autosave, a lineup save or a hand-set winner has no editor of its own to
// offer it (the score editors' banner offers it for a result). A write that is
// only waiting for the connection, or for a sign-in, is listed without one: it
// lands on its own. Re-read whenever the sync status or the held count moves.
export function HeldWritesPanel({ competitions, onClose }) {
  const Modal = typeof window !== 'undefined' ? window.Modal : null;
  const api = typeof window !== 'undefined' ? window.API : null;
  const read = () => (api && typeof api.heldWrites === 'function' ? api.heldWrites() : []);
  const [items, setItems] = useState(read);
  const [busyKey, setBusyKey] = useState(null);
  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);
  useEffect(() => {
    const refresh = () => { if (mountedRef.current) setItems(read()); };
    const offs = [];
    if (typeof window !== 'undefined') {
      if (typeof window.subscribeSyncStatus === 'function') offs.push(window.subscribeSyncStatus(refresh));
      if (typeof window.subscribeUnsentWrites === 'function') offs.push(window.subscribeUnsentWrites(refresh));
    }
    return () => offs.forEach((off) => { if (typeof off === 'function') off(); });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
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

function getHeldCount() {
  const api = typeof window !== 'undefined' ? window.API : null;
  if (!api || typeof api.heldWrites !== 'function') return 0;
  const list = api.heldWrites();
  if (!Array.isArray(list)) return 0;
  return list.filter((item) => item && item.held === true).length;
}

export function PublicHeldWrites({ competitions }) {
  const [count, setCount] = useState(getHeldCount);
  const [open, setOpen] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => () => { mountedRef.current = false; }, []);

  useEffect(() => {
    const refresh = () => {
      if (mountedRef.current) {
        setCount(getHeldCount());
      }
    };
    const offs = [];
    if (typeof window !== 'undefined') {
      if (typeof window.subscribeSyncStatus === 'function') {
        offs.push(window.subscribeSyncStatus(refresh));
      }
      if (typeof window.subscribeUnsentWrites === 'function') {
        offs.push(window.subscribeUnsentWrites(refresh));
      }
    }
    return () => {
      offs.forEach((off) => {
        if (typeof off === 'function') off();
      });
    };
  }, []);

  if (count === 0) return null;

  return (
    <>
      <button
        type="button"
        className="public-held"
        data-testid="public-held"
        aria-haspopup="dialog"
        onClick={() => setOpen(true)}
      >
        <span aria-hidden="true" className="public-held__dot" />
        <span role="status" aria-live="polite">
          {`${count} ${count === 1 ? 'change' : 'changes'} not sent`}
        </span>
      </button>
      {open && (
        <HeldWritesPanel
          competitions={competitions}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}
