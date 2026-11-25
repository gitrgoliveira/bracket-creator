// The one home of the held-writes list, shared by the admin topbar and the
// public page.

import { heldWriteLine, heldWriteWhat, heldWriteState, heldWriteDiscardConfirm, matchLabel, heldLineupLabel, HELD_WRITES_TITLE, HELD_WRITES_EMPTY, HELD_WRITE_DISCARD_ONE_LABEL, queuedWritesNoun, isFinishedResultWrite } from './write_result.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
import { useLatestRef } from './latest_ref.jsx';

const { useState, useEffect, useRef, useCallback } = React;

// useHeldWritesRefresh: runs `refresh` whenever the sync status or this tab's
// unsent-write count moves, until unmount (never after it). Returns the
// mounted flag for a caller whose own async work must check it too.
export function useHeldWritesRefresh(refresh) {
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

// useMatchHeldWrite: what this device still holds for one match, read from
// the outbox in ONE place for the editors' pending banner and the public
// match card. `held`: any write at all (a running update, a result, a
// decision) that already failed a pass; `stuck`: one the server keeps
// refusing or that sits on a wait cycle; `refused`: the match's own write is
// the one the server refuses (a wait cycle is not a refusal, so worded as a
// wait); `blocker`: the write of another kind this match waits for that is
// stuck (a team member's add; stage 3 review 4, G2); `resultUnsent`: a
// finished result or decision is queued, a representative bout change not
// counting (the public card must not claim a result for one). Re-read on
// every sync status change and every change of the held counts, which
// include the failing count, so a second write crossing the threshold, a
// write landing, and a discard made from the topbar's list all reach it.
export function useMatchHeldWrite(compId, matchId, match = null) {
  const api = window.API;
  // The match the editor shows names its held writes, and its teams' (H5).
  if (match && api && compId && typeof api.noteShownMatch === 'function') api.noteShownMatch(compId, match);
  // `held` is null when this page has no queue to ask (the editors' render
  // tests stub window.API without one): unknown, so nothing is cleared.
  // `stuck`, `refused` and `blocker` come from ONE read of the queue
  // (heldWriteState, stage 3 review 5, H2); a page whose API has only the two
  // older questions asks them.
  const read = useCallback(() => {
    const asks = !!(api && compId && matchId);
    const state = asks && typeof api.heldWriteState === 'function' ? api.heldWriteState(compId, matchId) : null;
    return {
      held: asks && typeof api.hasHeldWrite === 'function' ? api.hasHeldWrite(compId, matchId) : null,
      stuck: state ? !!state.keepsFailing
        : !!(asks && typeof api.heldWriteKeepsFailing === 'function' && api.heldWriteKeepsFailing(compId, matchId)),
      refused: !!(state && state.refused),
      blocker: (state ? state.blocker
        : (asks && typeof api.heldWriteBlocker === 'function' && api.heldWriteBlocker(compId, matchId))) || null,
      resultUnsent: !!(asks && typeof api.hasPendingResultWrite === 'function' && api.hasPendingResultWrite(compId, matchId)),
    };
  }, [api, compId, matchId]);
  const [state, setState] = useState(read);
  const refresh = () => {
    const next = read();
    const blockerKey = (s) => (s.blocker ? s.blocker.key : '');
    setState((prev) => (prev.held === next.held && prev.stuck === next.stuck && prev.refused === next.refused
      && prev.resultUnsent === next.resultUnsent && blockerKey(prev) === blockerKey(next) ? prev : next));
  };
  useHeldWritesRefresh(refresh);
  // Once for the match shown, again when the page is handed another.
  useEffect(refresh, [read]);
  return state;
}

// The held writes that belong to no competition (the court-call
// doors and the tournament settings, api_client_tournament.jsx), named by where the device put them.
const TOURNAMENT_WIDE_KINDS = new Set(['announce', 'announce-remove', 'announce-clear', 'tournament']);

// heldWriteWhere: which match (or team) one held write is about, named the
// way the score list names it (scoreRowMatchLabel) when the competition's
// matches are loaded, else by the match's number or id (matchLabel). The
// competition is named too: the list spans every competition on the device.
// A check-in names its competitor, found in the competition's roster by id; a
// court call its message, and the tournament settings themselves (`where`,
// as the API door named it, api_client_tournament.jsx or api_client_competitions.jsx); an awards save the awards.
export function heldWriteWhere(held, competitions) {
  const where = typeof held.where === 'string' ? held.where.trim() : '';
  if (TOURNAMENT_WIDE_KINDS.has(held.kind)) return where || (held.kind === 'tournament' ? 'Tournament settings' : 'Court call');
  const comp = (competitions || []).find((c) => c && c.id === held.compID) || null;
  const compName = (comp && comp.name) || held.compID || '';
  let what;
  if (held.kind === 'checkin' || held.kind === 'checkin-bulk') {
    const ids = Array.isArray(held.participantIds) ? held.participantIds : [];
    const one = ids.length === 1 && comp && Array.isArray(comp.players)
      ? comp.players.find((p) => p && p.id === ids[0]) : null;
    what = one && one.name ? `Check-in: ${one.name}` : (where || (ids.length > 1 ? `Check-in: ${ids.length} competitors` : 'Check-in'));
  } else if (held.kind === 'awards') {
    what = where || 'Fighting-spirit awards';
  } else if (held.kind === 'lineup') {
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

// heldWriteRows: the list's rows, one per match as the corner counts them
// (getHeldBadgeText: a match's finished result is one result, whatever
// score update and taps wait with it). Every write of a match that is only
// waiting (for the connection, a sign-in, or another write) shares that
// match's row, named by its finished result when it holds one; a write the
// server keeps refusing keeps a row of its own, since it is discarded alone
// and the confirm names it. A lineup save is its own row even when it names a
// match, and so is a write about no match (a check-in, a court call). Order is the order the first write of each row
// is listed in.
export function heldWriteRows(items) {
  const rows = [];
  const byMatch = new Map();
  for (const held of items || []) {
    if (!held) continue;
    const matchID = typeof held.matchID === 'string' ? held.matchID : '';
    if (held.keepsFailing || !matchID || held.kind === 'lineup') {
      rows.push({ key: held.key, lead: held, entries: [held] });
      continue;
    }
    const group = `${held.compID || ''}\u001f${matchID}`;
    let row = byMatch.get(group);
    if (!row) {
      row = { key: held.key, lead: held, entries: [] };
      byMatch.set(group, row);
      rows.push(row);
    }
    row.entries.push(held);
  }
  return rows.map((row) => {
    if (row.entries.length === 1) {
      return { ...row, line: heldWriteLine(row.lead), keepsFailing: !!row.lead.keepsFailing };
    }
    const finished = row.entries.filter(isFinishedResultWrite);
    const lead = finished[0] || row.lead;
    let what;
    if (finished.length === 1) what = heldWriteWhat(lead);
    else {
      const taps = row.entries.every((e) => e.kind === 'evt');
      const { n, noun } = queuedWritesNoun(finished.length, row.entries.length, { taps });
      what = `${n} ${noun}`;
    }
    const state = heldWriteState({
      cycled: row.entries.some((e) => e.cycled),
      authBlocked: row.entries.some((e) => e.authBlocked),
    });
    return { ...row, lead, line: `${what.charAt(0).toUpperCase() + what.slice(1)}: ${state}`, keepsFailing: false };
  });
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
          {heldWriteRows(items).map((row) => (
            <li key={row.key} className="held-writes__item" data-testid="held-write">
              <div className="held-writes__text">
                <div className="held-writes__where">{heldWriteWhere(row.lead, competitions)}</div>
                <div className={`held-writes__state${row.keepsFailing ? ' held-writes__state--error' : ''}`}>
                  {row.line}
                </div>
              </div>
              {row.keepsFailing && (
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  data-testid="held-write-discard-one"
                  disabled={busyKey !== null}
                  onClick={() => discard(row.lead)}
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
