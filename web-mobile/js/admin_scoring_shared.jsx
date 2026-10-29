// Shared helpers and small presentational components for the scoring modals
// (ScoreEditorModal / TeamScoreEditorModal in admin_scoring_modal.jsx). Split
// out so the foundation can be reused and the modal file stays focused on the
// two stateful editors. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA, useMemo: useMemoA, useLayoutEffect: useLayoutEffectA } = React;
const Icon = window.Icon;

import { DAIHYOSEN_POSITION, scoreRowMatchName, scoreRowMatchLabel } from './pool_ids.jsx';
import { classifyStartOutcome, startFailureMessage } from './start_match.jsx';
import {
  createOwnStatus, OWN_START, OWN_REOPEN, OWN_REQUEUE, OWN_REP,
  LANDED as OWN_LANDED, REFUSED as OWN_REFUSED, HELD as OWN_HELD, DISCARDED as OWN_DISCARDED,
} from './own_status.jsx';
export { OWN_START, OWN_REOPEN, OWN_REQUEUE, OWN_REP, OWN_REFUSED };
import {
  writeDidNotLand, writeRetryable, decisionWord,
  attemptScoreWrite, DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED, DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED, downstreamKnockoutReopenedNotice,
  courtBusyMessage, courtBusyReopenWarning, heldWriteDiscardLabel, heldWriteDiscardConfirm, queuedNotice,
  tapNotAppliedText, tapOutcomeQuiet, writePending, writeWasRefused,
} from './write_result.jsx';
import { sameCompetitor } from './competitor_identity.jsx';
import { sideWord, sideWithColour } from './side_cell.jsx';
import { sideMarks, defaultWinMaru } from './bracket.jsx';
import { NumberedName, numberFollowsName } from './numbered_name.jsx';
import { withdrawnSideKey } from './ineligible_match.jsx';
import { isTeamDefaultWinDecision } from './team_default_credit.jsx';
import { struckIppons, realIppons } from './result_slot.jsx';
import { acceptTap, useArmedConfirm } from './tap_guard.jsx';
// bc-evts: the board of a running match (tapBoardOf) is the server's copy with
// this device's pending taps folded on, by the JS replay port.
import { applyPending, assignMarks, APPLIED, BOUT_REMOVED, BOUT_REMOVED_MESSAGE } from './score_events.jsx';
import { isOlderRunningCopy } from './patch.jsx';
// BarredMatchNotice is a separate leaf (imports only ineligible_match.jsx +
// write_result.jsx): re-exported below, see that file's header for why.
import { BarredMatchNotice } from './barred_match_notice.jsx';
import {
  UNSAVED_ENTRIES_DISCARDED, REOPEN_ANYWAY, QUEUE_AND_REOPEN_ANYWAY, ADD_REP_BOUT_ANYWAY,
  REMOVE_REP_BOUT_ANYWAY, LEAVE_MATCH_ANYWAY, recordDecisionAnyway, unsavedDiscardDialog,
  holdsUnsaved, confirmDiscardUnsaved,
} from './unsaved_entries.jsx';

// Kendo best-of-3 cap. Mirrors the server-side `maxIpponsPerSide` in
// internal/mobileapp/validation.go: the bout ends when one side reaches
// 2 ippons, so 2-2 is an impossible scoreline. Used to gate the M/K/D/T/H
// buttons on both sides of every bout (individual + team sub-bout).
const MAX_IPPONS_PER_SIDE = 2;

// isBoutDecided: true once either side has reached the best-of-3 cap.
// The UI uses this to disable the add-ippon buttons on BOTH sides at
// that point: the bout would have ended at first-to-2, so neither side
// can legitimately add another ippon. Server enforces the same invariant
// in validateIppons (rejects 2-2 with HTTP 400).
function isBoutDecided(aPts, bPts) {
  return (aPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE
      || (bPts?.length ?? 0) >= MAX_IPPONS_PER_SIDE;
}

// HeldWriteDiscard: the way past a result held on this device that the server
// keeps refusing (a 5xx on every retry). Rendered inside a score editor's
// pending banner, it shows nothing until the queue reports that this match's
// held write has crossed the server-error threshold, then offers to discard
// that write, and only it, after a confirm (copy owned by write_result.jsx).
// It never blocks another write (each queued write is sent on its own), but
// without this the only way to stop it was signing out, which discards every
// held result. `onDiscarded` lets the editor drop its pending banner.
// useMatchHeldWrite: what this device still holds for one match: `held`,
// any write at all (a running update, a result, a decision), and `stuck`, one
// the server keeps refusing. Re-read on every sync status change and every
// change of the held counts, which include the failing count, so a second
// write crossing the threshold, a write landing, and a discard made from the
// topbar's list all reach it. The editors' pending banner, its line
// (HeldWriteNotice) and its Discard read it.
export function useMatchHeldWrite(compId, matchId, match = null) {
  const api = window.API;
  // The match the editor shows names its held writes, and its teams' (H5).
  if (match && api && compId && typeof api.noteShownMatch === 'function') api.noteShownMatch(compId, match);
  // `held` is null when this page has no queue to ask (the editors' render
  // tests stub window.API without one): unknown, so nothing is cleared.
  // `blocker` is the write of another kind this match waits for that the
  // server keeps refusing (a team member's add; stage 3 review 4, G2), or null.
  // `stuck` and `blocker` come from ONE read of the queue (heldWriteState,
  // stage 3 review 5, H2); a page whose API has only the two older questions
  // asks them.
  const read = () => {
    const asks = !!(api && compId && matchId);
    const state = asks && typeof api.heldWriteState === 'function' ? api.heldWriteState(compId, matchId) : null;
    return {
      held: asks && typeof api.hasHeldWrite === 'function' ? api.hasHeldWrite(compId, matchId) : null,
      stuck: state ? !!state.keepsFailing
        : !!(asks && typeof api.heldWriteKeepsFailing === 'function' && api.heldWriteKeepsFailing(compId, matchId)),
      blocker: (state ? state.blocker
        : (asks && typeof api.heldWriteBlocker === 'function' && api.heldWriteBlocker(compId, matchId))) || null,
    };
  };
  const [state, setState] = useStateA(read);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  useEffectA(() => {
    const refresh = () => {
      if (!mountedRef.current) return;
      const next = read();
      const blockerKey = (s) => (s.blocker ? s.blocker.key : '');
      setState((prev) => (prev.held === next.held && prev.stuck === next.stuck && blockerKey(prev) === blockerKey(next) ? prev : next));
    };
    refresh();
    const offs = [];
    if (typeof window.subscribeSyncStatus === 'function') offs.push(window.subscribeSyncStatus(refresh));
    if (typeof window.subscribeUnsentWrites === 'function') offs.push(window.subscribeUnsentWrites(refresh));
    return () => offs.forEach((off) => { if (typeof off === 'function') off(); });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [compId, matchId]);
  return state;
}

// useClearPendingWhenNothingHeld: the editors' pending banner goes once this
// device holds no write for the match: it landed, or it was discarded, here
// or from the topbar's list. Waiting for the whole queue to drain left it up
// while another match's write was still held, saying this one was saved on
// the device when nothing was any more. `onClear` resets the editor's banner
// state (and its kept submit closure).
export function useClearPendingWhenNothingHeld(compId, matchId, pendingWrite, onClear) {
  const { held } = useMatchHeldWrite(compId, matchId);
  useEffectA(() => {
    if (pendingWrite && held === false) onClear();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingWrite, held]);
}

// The refusals a tap gets while the server holds the match as not running
// (operator S7 a): the server's sentence is shown next to the board.
const TAP_NOT_RUNNING_REASONS = new Set(['match_not_started', 'match_sent_back']);

// useTapOutcomeNote (bc-evts stage 3): what the outbox reported about a tap of
// this editor's match the server did not apply (API.subscribeTapOutcome).
// `note` is the quiet line (tapNotAppliedText); `refusal` the server's sentence
// for a tap refused because the match is not running. `clear()` drops both,
// called as the operator taps again.
export function useTapOutcomeNote(compId, matchId) {
  const [state, setState] = useStateA({ note: null, refusal: null, id: null });
  useEffectA(() => {
    setState({ note: null, refusal: null, id: null });
    const api = window.API;
    if (!api || typeof api.subscribeTapOutcome !== 'function') return undefined;
    return api.subscribeTapOutcome(compId, matchId, (o) => {
      if (o && o.takenBack) {
        // The server applied a tap this note named not applied (the board
        // judged it from a copy behind the server): the note goes (stage 6
        // review 8, m12). Only a notice the outbox takes back takes it down,
        // never any quiet answer: a `duplicate` can be a tap turned away
        // answered again (review 9, F3).
        setState((s) => (s.id && s.id === o.id ? { note: null, refusal: null, id: null } : s));
        return;
      }
      if (tapOutcomeQuiet(o)) return;
      const text = tapNotAppliedText(o);
      if (!text) return;
      const id = (o && o.id) || null;
      if (o && TAP_NOT_RUNNING_REASONS.has(o.reason)) setState((s) => ({ ...s, refusal: text, id }));
      else setState((s) => ({ ...s, note: text, id }));
    });
  }, [compId, matchId]);
  const clearRef = useRefA(null);
  if (!clearRef.current) {
    clearRef.current = () => setState((s) => (s.note || s.refusal ? { note: null, refusal: null, id: null } : s));
  }
  return { note: state.note, refusal: state.refusal, clear: clearRef.current };
}

// The scoring fields a board takes from the newest copy the outbox holds. The
// flags are an engi match's: the server leaves a count of 0 out, so a missing
// count is left missing (an individual match must not gain flags the TV board
// would show) and the engi editor reads it as 0.
const DERIVED_BOARD_FIELDS = {
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, encho: null, markIds: undefined,
  flagsA: undefined, flagsB: undefined, scoreVersion: 0, modifiedAt: 0, eventSeqs: undefined,
};

// sameSides: whether the copy a score-events answer handed back is of the
// match the host renders, by its competitors (bc-evts stage 3 review 11, X4).
// Match ids are name slugs, so a competition made again under the same name
// reuses them; a copy whose side ids differ is the old match. A side with no
// id on either copy (a legacy row) decides nothing.
function sideIdOf(x, key) {
  const flat = x[`side${key}Id`];
  if (flat) return flat;
  const side = x[`side${key}`];
  return (side && typeof side === "object" && side.id) || "";
}
// unloggedUnderAnswer: the host's running copy carries no score version (no
// line of the match's score log: the queue row of a match nobody has scored,
// shown running from this console's own start until the court's feed shows it,
// bc-strt), while a score-events answer for the same match does. The answer is
// the newer copy: it is the log's, and the host's copy predates every line of
// it. Without this an answered tap left the board until the feed caught up,
// and for good on a device that went offline first (stage 6). The board's own
// rule, never keepNewerMatches': there a refetch with no version replaces the
// held copy (a draw made again reuses the match ids), which the answer cannot
// be, since an answer for other sides is never taken (sameSides) and the
// answers are forgotten with their competition's draw (_forgetShownMatches).
function unloggedUnderAnswer(m, derived) {
  return m.status === "running" && derived.status === "running"
    && !(Number(m.scoreVersion) > 0) && Number(derived.scoreVersion) > 0;
}
function sameSides(m, derived) {
  for (const key of ["A", "B"]) {
    const a = sideIdOf(m, key);
    const b = sideIdOf(derived, key);
    if (a && b && a !== b) return false;
  }
  return true;
}

// tapBoardOf (bc-evts stage 3, S6): the board of a RUNNING match, for the
// individual, engi (stage 4) and team (stage 5) editors alike. Its base is the newer of the
// match the host renders and the newest copy a score-events answer handed back
// (API.latestDerivedMatch), by isOlderRunningCopy's rule (scoreVersion first,
// then the stamp); this device's pending taps are folded on top (applyPending,
// `opts` its fold options: {engi: true} for an engi match, whose flag taps the
// fold refuses otherwise; {team: true, kachinuki} for a team match, whose bout
// taps it refuses otherwise). `base` is what the taps were made against.
export function tapBoardOf(m, opts) {
  const api = window.API || {};
  const derived = typeof api.latestDerivedMatch === "function" ? api.latestDerivedMatch(m.compId, m.id) : null;
  let base = m;
  if (derived && sameSides(m, derived) && (isOlderRunningCopy(m, derived.status, derived.modifiedAt, derived.scoreVersion) || unloggedUnderAnswer(m, derived))) {
    base = { ...m };
    for (const [f, none] of Object.entries(DERIVED_BOARD_FIELDS)) base[f] = derived[f] ?? none;
    // A rep pick the answer does not carry is the one the host renders.
    if (derived.repPlayerA !== undefined) base.repPlayerA = derived.repPlayerA;
    if (derived.repPlayerB !== undefined) base.repPlayerB = derived.repPlayerB;
    // A team match's bout rows (stage 5), when the answer carries them.
    if (Array.isArray(derived.subResults)) base.subResults = derived.subResults;
  }
  // `opts.prepare` lets an editor add to the base what the server has not
  // answered for yet but taps already name (the team editor's kachinuki rows
  // added by hand, whose add is queued ahead of those taps).
  const all = (typeof api.pendingTaps === "function" && api.pendingTaps(m.compId, m.id)) || [];
  const folded = applyPending(opts && opts.prepare ? opts.prepare(base) : base, all, opts);
  // `moving`: the pending taps that still change the board (a tap the base
  // already holds, or one with nothing left to take, changes nothing).
  const moving = all.filter((e) => folded.outcomes[e.id] === APPLIED);
  // A pending tap on a bout the copy shows removed is not on the board, and
  // the operator is told so once, now, as the server's answer would tell it
  // (stage 6 review 7; the fold's own rule, BOUT_REMOVED, never a second one).
  if (typeof api.tellPendingTapRefused === "function") {
    for (const e of all) {
      if ((folded.reasons || {})[e.id] !== BOUT_REMOVED) continue;
      api.tellPendingTapRefused(m.compId, m.id,
        { id: e.id, outcome: folded.outcomes[e.id], reason: BOUT_REMOVED, message: BOUT_REMOVED_MESSAGE },
        { kind: e.kind, side: e.side, pos: e.pos, letter: e.letter });
    }
  }
  return { base, match: folded.match, pending: all, moving };
}

// shownMarkIds: the mark id of each mark a side shows (realIppons order: the
// placeholders and the hantei mark dropped) for a match (pos 0) or a team bout
// row (its position), "" where none can be named. An ippon.remove names the
// mark by this id. A copy that names no ids for a side is one whose score log
// has not begun (a match last written before mark ids existed): the server's
// first baseline mints each mark's id from what the mark is
// (b:<pos>:<side>:<letter>:<n>, engine.MigrationBaseline through assignMarks),
// so the same ids are minted here.
export function shownMarkIds(holder, key) {
  const raw = (key === "a" ? holder.ipponsA : holder.ipponsB) || [];
  const named = holder.markIds && holder.markIds[key];
  const ids = named || (holder.markIds ? [] : assignMarks([], [], raw, Number(holder.position) || 0, key));
  const out = [];
  raw.forEach((x, i) => { if (realIppons([x]).length) out.push(ids[i] || ""); });
  return out;
}

// useTapBoard re-renders the editor when this device's pending taps for the
// match or the newest derived copy move, and answers the board while `on`.
// DRAFT_KEPT_NOTE: what a score editor says while it keeps a draft the match
// was started under by another device (operator decision D1 (a), 2026-10-17).
export const DRAFT_KEPT_NOTE = "This match was started on another device. Your unsaved entries are still here: Save adds them to the match, Discard drops them.";
// DRAFT_KEPT_REOPENED_NOTE: the same for a correction draft over a finished
// match another device reopened (stage 5 review 3, under D1's rule).
export const DRAFT_KEPT_REOPENED_NOTE = "This match was reopened on another device. Your unsaved entries are still here: Save adds them to the match, Discard drops them.";
// DRAFT_DROPPED_NOTE: what it says when the match leaves running elsewhere
// while it keeps the draft (stage 5 review 2): the draft is dropped, never
// silently, and the editor shows the server's copy.
export const DRAFT_DROPPED_NOTE = "This match was finished or sent back to the queue on another device. Your unsaved entries were dropped.";

// useOwnStatus: one editor's own_status.jsx owner (the ONE owner of this
// editor's own status writes and the draft it keeps), observing `feedMatch`
// (the host's copy of the match, the court console's `started` included, and
// the team sheet's own representative-bout override) on every render. It
// answers the owner's view (shownStatus, override, startOffered, refusal,
// kept, droppedShown, resetSeq) and its doors:
//   - note(base, draft): what the draft would tap, each render;
//   - begin(kind, fields): an own Start (OWN_START, `fields` the score the
//     Start carried, shown until the feed shows the start), reopen
//     (OWN_REOPEN), requeue-and-reopen (OWN_REQUEUE) or representative-bout
//     change (OWN_REP); its token;
//   - settle(token, res): the write's answer, read as it comes: a direct
//     answer at once, the outbox's through `settled` (a callback, never a
//     wait); `{ rejections: false }` leaves a rejection to the caller's own
//     refusal callback (answer);
//   - answer(token, outcome, msg), release().
export function useOwnStatus(feedMatch) {
  const ref = useRefA(null);
  if (!ref.current) ref.current = createOwnStatus(feedMatch.status, Number(feedMatch.modifiedAt) || 0);
  const mountedRef = useRefA(true);
  useEffectA(() => { mountedRef.current = true; return () => { mountedRef.current = false; }; }, []);
  const [, bump] = useStateA(0);
  const own = ref.current;
  own.observe(feedMatch.status, Number(feedMatch.modifiedAt) || 0);
  const rerender = () => { if (mountedRef.current) bump((n) => n + 1); };
  const answer = (token, outcome, msg, at) => { own.answer(token, outcome, msg || "", at); rerender(); };
  const stampOf = (body) => (body && typeof body === "object" ? Number(body.modifiedAt) || 0 : 0);
  const view = own.view();
  return {
    ...view,
    // Read when the note renders, after this render's `note`, so the first
    // edit of a new draft takes it down at once.
    get droppedShown() { return own.view().droppedShown; },
    note: (base, draft) => own.note(base, draft),
    begin: (kind, fields) => { const t = own.begin(kind, fields); rerender(); return t; },
    answer,
    // claimRefusal: the `onRefusal` for own write `token`'s outbox entry. It
    // claims a refusal this editor says itself (an own Start's sentence,
    // StartRefusedNotice), so the outbox raises no `rejected` alert with the
    // same sentence; the settle below then states it. Anything else, or an
    // editor gone, is left to the outbox to tell.
    claimRefusal: (token) => () => mountedRef.current && own.saysRefusal(token),
    settle: (token, res, opts = {}) => {
      if (!res) { answer(token, OWN_REFUSED); return; }
      if (writePending(res)) {
        // A pending answer with no settlement says nothing more.
        if (!res.settled || typeof res.settled.then !== "function") return;
        res.settled.then((body) => {
          if (body && body.discarded === true) { answer(token, OWN_DISCARDED); return; }
          const out = classifyStartOutcome(body);
          answer(token, out.ok ? OWN_LANDED : OWN_HELD, out.ok ? "" : out.msg, out.ok ? stampOf(body) : 0);
        }, (e) => { if (opts.rejections !== false) answer(token, OWN_REFUSED, startFailureMessage(e)); });
        return;
      }
      if (writeWasRefused(res)) { answer(token, OWN_REFUSED); return; }
      answer(token, OWN_LANDED, "", stampOf(res));
    },
    // carry(token, fields): a landed own write's answer laid on its override
    // (own_status.jsx carry), read after settle has answered it.
    carry: (token, fields) => { own.carry(token, fields); rerender(); },
    release: () => { own.release(); rerender(); },
    rebaseKept: (base) => { own.rebaseKept(base); rerender(); },
    discardDraft: () => { own.discardDraft(); rerender(); },
  };
}

// DraftKeptNotice: the note, Save (two taps, as the editors' Finish: the
// bounce of the arming tap cannot save) and Discard (one tap). `from` is the
// status the match left: "completed" says it was reopened.
export function DraftKeptNotice({ onSave, onDiscard, disabled, from }) {
  const { armed, confirm } = useArmedConfirm();
  return (
    <div className="pending-write-banner" role="status" aria-live="polite" data-testid="draft-kept-notice">
      <span>{from === "completed" ? DRAFT_KEPT_REOPENED_NOTE : DRAFT_KEPT_NOTE}</span>
      <button type="button" className={`btn btn--sm btn--primary ${armed ? "btn--confirm" : ""}`} data-testid="draft-kept-save" disabled={disabled}
        onClick={(ev) => { if (confirm(ev)) onSave(); }}>
        {armed ? "Tap again to save" : "Save"}
      </button>
      <button type="button" className="btn btn--sm btn--ghost" data-testid="draft-kept-discard" disabled={disabled} onClick={onDiscard}>Discard</button>
    </div>
  );
}

// ownStartRefusalText: that sentence, naming the match.
export function ownStartRefusalText(match, msg) {
  const label = scoreRowMatchLabel(match || {});
  return label ? `${label}: ${msg}` : msg;
}

// StartRefusedNotice: the sentence beside the board.
export function StartRefusedNotice({ text }) {
  if (!text) return null;
  return (
    <div className="pending-write-banner pending-write-banner--failed" role="alert" aria-live="assertive" data-testid="start-refused-notice">
      <span>{text}</span>
    </div>
  );
}

// DraftDroppedNotice: the note a dropped draft leaves (DRAFT_DROPPED_NOTE).
export function DraftDroppedNotice() {
  return (
    <div className="pending-write-banner" role="status" aria-live="polite" data-testid="draft-dropped-notice">
      <span>{DRAFT_DROPPED_NOTE}</span>
    </div>
  );
}

export function useTapBoard(m, on, opts) {
  const [, setTick] = useStateA(0);
  useEffectA(() => {
    const api = window.API || {};
    if (!on) return undefined;
    const bump = () => setTick((t) => t + 1);
    const offs = [];
    if (typeof api.subscribePendingTaps === "function") offs.push(api.subscribePendingTaps(m.compId, m.id, bump));
    if (typeof api.subscribeDerivedMatch === "function") offs.push(api.subscribeDerivedMatch(m.compId, m.id, bump));
    return () => { for (const off of offs) if (typeof off === "function") off(); };
  }, [m.compId, m.id, on]);
  return on ? tapBoardOf(m, opts) : null;
}

// HeldWriteNotice: the pending banner's line, worded for where the held
// write stands (queuedNotice): waiting for the connection, held in this page
// only, or refused by the server on every attempt. The three editors render it.
export function HeldWriteNotice({ compId, matchId, res, match = null }) {
  const { stuck, blocker } = useMatchHeldWrite(compId, matchId, match);
  return <span>{queuedNotice(res, { keepsFailing: stuck, blockedBy: blocker })}</span>;
}

export function HeldWriteDiscard({ compId, matchId, onDiscarded, disabled = false, match = null }) {
  const api = window.API;
  const { stuck, blocker } = useMatchHeldWrite(compId, matchId, match);
  const [busy, setBusy] = useStateA(false);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  if (!stuck) return null;
  const discard = async () => {
    setBusy(true);
    try {
      const ok = typeof window.confirmDialog === 'function'
        ? await window.confirmDialog(heldWriteDiscardConfirm(blocker || undefined))
        : false;
      if (!ok || !mountedRef.current) return;
      if (blocker) {
        // The write the match waits for (G2): the match's own writes stay
        // queued and go once it is gone, so the editor keeps what it holds.
        api.discardHeldWrite(blocker.key);
        return;
      }
      api.discardFailingHeldWrites(compId, matchId);
      if (typeof onDiscarded === 'function') onDiscarded();
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };
  return (
    <button
      type="button"
      className="btn btn--sm btn--ghost"
      data-testid="held-write-discard"
      disabled={disabled || busy}
      onClick={discard}
    >
      {heldWriteDiscardLabel(blocker)}
    </button>
  );
}

// getIpponButtons: returns the ordered array of scoring button labels for a
// bout. Naginata adds "S" (Sune, shin strike) between "T" and "H".
function getIpponButtons(isNaginata) {
  return isNaginata ? ["M", "K", "D", "T", "S", "H"] : ["M", "K", "D", "T", "H"];
}

// getValidPointKeys: returns the string of valid single-character keyboard
// shortcuts for scoring. Keyboard handler checks `validKeys.includes(upper)`.
function getValidPointKeys(isNaginata) {
  return isNaginata ? "MKDTSH" : "MKDTH";
}

// IPPON_LETTER_LEGEND: the M/K/D/T/H (+S for naginata) ippon-type letters
// and their kendo meaning. The strike-target names mirror the kendo glossary
// (internal/domain/glossary.go datapoint "datapoint"/"sune"/"hansoku"). H is
// the hansoku-derived free point ("Two hansoku … give the opponent one free
// point", glossary "hansoku"). The viewer glossary doesn't define these
// single letters as terms, so the wording is authored here for the operator.
const IPPON_LETTER_LEGEND = [
  ["M", "Men: head strike"],
  ["K", "Kote: wrist strike"],
  ["D", "Do: torso strike"],
  ["T", "Tsuki: throat thrust"],
  ["S", "Sune: shin strike (naginata)"],
  ["H", "Hansoku point: opponent's 2nd foul"],
];

// IpponLegend: compact, always-present key mapping each scoring letter to its
// kendo meaning, so the admin scoring modal isn't dependent on the viewer-only
// glossary. The "S" (Sune) row only shows for naginata competitions. Styled
// inline with DESIGN tokens (this region of styles.css is owned elsewhere).
function IpponLegend({ isNaginata }) {
  const rows = IPPON_LETTER_LEGEND.filter(([letter]) => letter !== "S" || isNaginata);
  return (
    <details
      data-testid="scoring-modal-ippon-legend"
      style={{ marginTop: 10, fontSize: 12, color: "var(--ink-3)" }}
    >
      <summary style={{ cursor: "pointer", fontWeight: 600, color: "var(--ink-2)", userSelect: "none" }}>
        <span aria-hidden="true" style={{
          display: "inline-block", width: 16, height: 16, lineHeight: "16px",
          textAlign: "center", borderRadius: 999, border: "1px solid var(--line)",
          color: "var(--ink-2)", fontWeight: 700, marginRight: 6,
        }}>?</span>
        Ippon-type key
      </summary>
      <dl style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 10px", margin: "8px 0 0", padding: 0 }}>
        {rows.map(([letter, meaning]) => (
          <React.Fragment key={letter}>
            <dt style={{
              fontFamily: "var(--font-mono)", fontWeight: 700, color: "var(--ink-1)",
              margin: 0, textAlign: "center",
            }}>{letter}</dt>
            <dd style={{ margin: 0, color: "var(--ink-2)" }}>{meaning}</dd>
          </React.Fragment>
        ))}
      </dl>
    </details>
  );
}

// ScoringShortcutHint: quiet reminder of the keyboard shortcuts the editor
// supports. Rendered below the prev/next nav so the affordance sits where
// operators look for navigation. Clarity over decoration: plain muted text, no
// animation. The container's layout lives in the `.scoring-shortcut-hint`
// rule in styles.css, NOT inline: an inline display:flex outranked the
// coarse-pointer `display: none` and showed the hint on the iPad (bc-kbhn).
// pointKeys: the valid ippon letters ("MKDTH" / "MKDTSH") when this editor
// supports keyboard scoring — individual matches and kachinuki bouts — else
// "" (fixed-order team bouts score by tap only). Listed so the shortcut is
// discoverable instead of hidden in the code.
// hasNav / canClose: whether ←/→ and Esc actually do something on this host.
// A key that does nothing is never listed (the inline court console wires
// neither), and with nothing to list the hint renders nothing at all.
function ScoringShortcutHint({ pointKeys = "", hasNav = false, canClose = false }) {
  const kbd = {
    fontFamily: "var(--font-mono)", fontSize: 11, padding: "1px 5px",
    border: "1px solid var(--line)", borderRadius: 4, background: "var(--surface)",
    color: "var(--ink-3)", margin: "0 1px",
  };
  const keys = pointKeys ? [...pointKeys] : [];
  const groups = [];
  if (keys.length > 0) {
    groups.push(
      <React.Fragment key="pts">
        {keys.map((k) => <kbd key={k} style={kbd}>{k}</kbd>)}
        <span>Shiro</span>
        <span aria-hidden="true">·</span>
        <kbd style={kbd}>⇧</kbd><span>Aka</span>
      </React.Fragment>,
    );
  }
  if (hasNav) {
    groups.push(
      <React.Fragment key="nav">
        <kbd style={kbd}>←</kbd><kbd style={kbd}>→</kbd>
        <span>prev/next</span>
      </React.Fragment>,
    );
  }
  if (canClose) {
    groups.push(
      <React.Fragment key="close">
        <kbd style={kbd}>Esc</kbd>
        <span>close</span>
      </React.Fragment>,
    );
  }
  if (groups.length === 0) return null;
  return (
    <div
      className="scoring-shortcut-hint"
      data-testid="scoring-modal-shortcut-hint"
      aria-hidden="true"
    >
      {groups.map((g, i) => (
        <React.Fragment key={g.key}>
          {i > 0 && <span aria-hidden="true">·</span>}
          {g}
        </React.Fragment>
      ))}
    </div>
  );
}

// applyFusenshoToggle: pure reducer for the per-bout Fusensho button in
// TeamScoreEditorModal, over a sub-bout {aPts, bPts, aFouls, bFouls,
// fusensho, _preFusensho?, ...}. `side` is the side the default win goes TO.
// A default win gives that side its maru and leaves the OTHER side the points
// it had already struck (FIK Art. 32; engine.preserveLoserScore is the
// match-level twin). Three branches:
//   1. Toggle-on from a clean state: snapshot {aPts,bPts,aFouls,bFouls}
//      into _preFusensho, then write the default win.
//   2. Side-switch (fusensho is already on the other side): keep the
//      original _preFusensho and build from IT, so the new loser keeps what
//      it struck before any fusensho, never the circles it was just given,
//      and a later untoggle restores the genuine pre-fusensho score. With no
//      snapshot (a reopened row), the base is the row without its circles
//      (fusenshoBase), so an untoggle cannot bring them back as points.
//   3. Toggle-off (re-clicking the active side): restore from _preFusensho.
//      With no snapshot (the editor was reopened from saved state, which does
//      not carry one), drop the circles through clearFusensho: struck points
//      stay, and the circles never survive as ordinary points. The winner's
//      own pre-fusensho strikes cannot come back then (the default win
//      replaced them), as for a match-level default win.
// A fusensho against a side that already won the bout is refused
// (fusenshoAllowed): circles against two struck points would be a 2-2.
// EVERY branch spreads `...prev` so per-sub fields this reducer does NOT
// own survive the toggle — notably the kachinuki `encho` marker (mp-gmcg)
// and manually typed side names. A bespoke object literal silently dropped
// them, which erased the (E) audit mark and inflated an encho default win
// from one maru to two. draw and fusensho are mutually exclusive, so a set
// draw is cleared when fusensho is applied.
// A scoring edit on the row goes through applyBoutScoreEdit, which ends the
// fusensho unless the edit only takes a mark off the losing side.
function applyFusenshoToggle(prev, side) {
  if (prev.fusensho === side) {
    const snap = prev._preFusensho;
    if (snap) return { ...prev, aPts: snap.aPts, bPts: snap.bPts, aFouls: snap.aFouls, bFouls: snap.bFouls, fusensho: "", _preFusensho: undefined };
    return clearFusensho(prev);
  }
  if (!fusenshoAllowed(prev, side)) return prev;
  const snap = fusenshoBase(prev);
  // The maru cells come from the shared count rule (defaultWinMaru in
  // bracket.jsx): one maru per point, so two in regulation but ONE in encho.
  // Pass THIS bout's encho period — a per-bout fusensho can land on a pairing
  // already fighting on in overtime — and let the shared rule decide; a zero or
  // absent period reads as regulation there, so no local branch is needed.
  const maru = defaultWinMaru({ periodCount: prev.encho });
  const base = { ...prev, aFouls: 0, bFouls: 0, _preFusensho: snap, ...(prev.draw ? { draw: false } : {}) };
  if (side === "a") return { ...base, aPts: maru, bPts: struckIppons(snap.bPts), fusensho: "a" };
  return { ...base, aPts: struckIppons(snap.aPts), bPts: maru, fusensho: "b" };
}

// fusenshoBase: the genuine pre-fusensho state of a sub-bout, i.e. the
// snapshot a fusensho already took, else the row as it stands without any
// default-win circles. A row reopened from saved state carries its fusensho
// but no snapshot, and taking its circles into a snapshot let an untoggle
// after a side-switch restore them as ordinary points.
const fusenshoBase = (prev) => {
  if (prev._preFusensho) return prev._preFusensho;
  const row = clearFusensho(prev);
  return { aPts: row.aPts, bPts: row.bPts, aFouls: row.aFouls, bFouls: row.bFouls };
};

// fusenshoAllowed: can a default win go to `side` on this sub-bout? Not when
// the other side had already struck MAX_IPPONS_PER_SIDE points: that side has
// won the bout, and circles against its two points would be an impossible
// 2-2 (validateIppons refuses it on every save). Undoing an active fusensho
// is always allowed.
function fusenshoAllowed(prev, side) {
  if (prev.fusensho === side) return true;
  const base = fusenshoBase(prev);
  const other = side === "a" ? base.bPts : base.aPts;
  return struckIppons(other).length < MAX_IPPONS_PER_SIDE;
}

// clearFusensho: end a sub-bout's fusensho without restoring a snapshot. The
// winner's default-win circles go (struckIppons over that side), struck points
// on both sides stay, and the flag and snapshot are cleared. The identity on
// pts when no fusensho is set, so every edit on a team bout row can end
// through it. Apply the edit FIRST and this LAST: an edit computed from the
// rendered pts can still carry a circle, and clearing last strips it.
function clearFusensho(prev) {
  if (!prev.fusensho) return { ...prev, _preFusensho: undefined };
  const key = prev.fusensho === "a" ? "aPts" : "bPts";
  return { ...prev, [key]: struckIppons(prev[key]), fusensho: "", _preFusensho: undefined };
}

// applyBoutScoreEdit: the row a scoring edit on a team bout leaves, given the
// row before it (`prev`) and the edit applied to it (`next`). A fought score
// is neither a default win nor a draw, so an edit ends the fusensho through
// clearFusensho, applied LAST (an edit computed from the rendered pts can
// still carry a circle, and clearing last strips it), and clears the draw.
// One edit is a correction instead: taking a mark off the side the fusensho
// went AGAINST. It keeps the fusensho, since the operator only fixed that
// side's points, and the snapshot follows it, so undoing the fusensho later
// does not bring the removed mark back. Adding a point is a fresh strike and
// still ends it. An edit that changes nothing leaves the row as it was.
function applyBoutScoreEdit(prev, next) {
  if (next === prev) return prev;
  if (removesLosingSideMark(prev, next)) {
    const key = prev.fusensho === "a" ? "bPts" : "aPts";
    const snap = prev._preFusensho && { ...prev._preFusensho, [key]: next[key] };
    return { ...next, _preFusensho: snap };
  }
  return { ...clearFusensho(next), draw: false };
}

// removesLosingSideMark: `next` differs from `prev` only in a mark taken off
// the side a fusensho went against.
function removesLosingSideMark(prev, next) {
  if (!prev.fusensho || next.fusensho !== prev.fusensho) return false;
  const [won, lost] = prev.fusensho === "a" ? ["aPts", "bPts"] : ["bPts", "aPts"];
  return next[won] === prev[won]
    && next.aFouls === prev.aFouls && next.bFouls === prev.bFouls
    && next[lost].length < prev[lost].length;
}

// applyFoulIncrement: pure helper modelling a single `+` press on a
// side's foul counter. Per FIK rules (and internal/domain/glossary.go):
// "Two hansoku awarded to a competitor give the opponent one free point."
// The 2nd foul auto-awards an "H" ippon to the opponent and resets this
// side's counter to 0. The counter is "outstanding fouls not yet
// discharged into an H": discharged Hs live in the opponent's pts array.
//
// Bout-decided guard: if EITHER side is already at maxIppons the bout is
// over: the counter still resets to 0 on the 2nd foul but no new H is
// awarded. This prevents an auto-award from creating an invalid 2-2
// scoreline that the server's validateIppons would reject. The UI
// also disables the `+` button via isBoutDecided as a defense in depth.
// To undo a previously awarded H, the operator removes it from the
// opponent's slot directly.
function applyFoulIncrement(fouls, opponentPts, thisSidePts = [], maxIppons = MAX_IPPONS_PER_SIDE) {
  const next = fouls + 1;
  if (next < 2) return { fouls: next, opponentPts };
  if (opponentPts.length >= maxIppons || thisSidePts.length >= maxIppons) {
    return { fouls: 0, opponentPts };
  }
  return { fouls: 0, opponentPts: [...opponentPts, "H"] };
}

// reconcileFoulsAtOpen: pure helper for the reopen/correction flow.
// Pre-fix builds stored hansoku as a cumulative raw count (0..N) alongside
// the already-discharged "H" entries in the opponent's pts array. The new
// counter is "outstanding fouls not yet discharged" (0 or 1). Naively
// taking `rawFouls % 2` strips full pairs: but if the opponent's pts is
// MISSING the expected H entries (older data, partial save, imported
// match), the strip silently loses points. This helper tops up the
// opponent's pts with the missing H's (capped at maxIppons) before
// returning the outstanding remainder. Idempotent: when the H's are
// already present it leaves opponentPts unchanged.
function reconcileFoulsAtOpen(rawFouls, opponentPts, maxIppons = MAX_IPPONS_PER_SIDE) {
  const safe = Math.max(0, rawFouls);
  const expectedH = Math.floor(safe / 2);
  const haveH = opponentPts.filter(x => x === "H").length;
  const missing = Math.max(0, expectedH - haveH);
  const topUp = Math.min(missing, Math.max(0, maxIppons - opponentPts.length));
  const newOpp = topUp > 0 ? [...opponentPts, ...Array(topUp).fill("H")] : opponentPts;
  return { outstandingFouls: safe % 2, opponentPts: newOpp };
}

// nextFoulOnDecrement: pure helper for the `−` button. Returns the new
// foul value (a NUMBER, not a React-style functional updater), suitable
// for setters like the team sub-match `rs.setFouls(value)` shape that
// doesn't accept fn-updaters. Extracted so the team-modal `−` regression
// (a fn-updater silently storing as state) is unit-testable.
function nextFoulOnDecrement(currentFouls) {
  return Math.max(0, currentFouls - 1);
}

// Term: kendo-glossary tooltip wrapper. Read lazily off window so the
// load order between glossary.js and this module doesn't matter (both
// are type="module" scripts and may execute in any order). Falls back
// to a plain pass-through when window.Term isn't available yet (e.g.
// vitest harness, or pre-mount of the glossary module).
//
// RULE (bc-otpl): a Term is never inside a button or label. A Term is itself
// a control (its own click handler stops propagation), so nested in another
// control it swallows the tap meant for the outer one. Put the control's text
// in plain words and GlossaryHintAS BESIDE the control, as the Kiken/Fusenpai
// buttons and EnchoControl do.
function TermAS(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Lazily loaded from window for the same load-order reason as TermAS above.
// Falls back to null: the icon is purely decorative; no content to preserve.
// No align prop: the tooltip now self-positions (Term in glossary.jsx
// measures its room on open and flips leftwards itself), so no call site
// needs to say where in a wrapping row its hint landed.
function GlossaryHintAS({ name }) {
  if (typeof window !== 'undefined' && window.GlossaryHint) {
    return React.createElement(window.GlossaryHint, { name });
  }
  return null;
}

// T093–T098: shared helpers for the decision (kiken/fusenpai/fusensho) flow.
//
// Resolve the password for the /decision POST and the representative-bout add
// and remove. The helper only uses the prop (no window fallback); callers must
// pass the password explicitly. "" is what the public self-run page passes: the
// representative-bout routes accept it there, and every gated route (/decision
// included) answers it with 401, surfacing a caller that forgot the prop.
function resolveDecisionPassword(propPassword) {
  return propPassword || "";
}

// T093/T094: build the /decision POST body. Pure helper so we can pin the
// wire shape (decision/decisionBy/decisionReason/encho) against a
// moving server contract.
function buildDecisionBody(kind, { decisionBy, decisionReason }, enchoPeriodCount, opts = {}) {
  const body = { decision: kind, decisionBy };
  if (decisionReason) body.decisionReason = decisionReason;
  if (enchoPeriodCount > 0) body.encho = { periodCount: enchoPeriodCount };
  // The match the decision was recorded on (bc-hlck): recordDecision floors
  // the stamp by it and never sends it.
  if (opts.seenModifiedAt > 0) body.seenModifiedAt = opts.seenModifiedAt;
  return body;
}

// mp-os3: shared decision-submit path used by both ScoreEditorModal and
// TeamScoreEditorModal. Wraps buildDecisionBody + recordDecision and
// resolves the password from the explicit prop. Extracted so the regression
// test pins the production call site (rather than re-implementing the chain
// inside the test, which was how the original gap slipped through). Returns
// the promise from window.API.recordDecision so callers can await it.
//
// bc-cse: routed through attemptScoreWrite (write_result.jsx), the SAME
// confirm-and-retry loop admin.jsx's editMatchScore gives the score path, so
// a kiken/fusenpai/daihyosen decision that corrects a completed knockout
// match whose later round already played gets the identical
// downstream_knockout_played 409 experience instead of a raw error token
// with no way forward. The recordScore collaborator here deliberately
// declares only 4 parameters (compId, matchId, result, password) so the
// `match` argument attemptScoreWrite forwards is dropped rather than reaching
// window.API.recordDecision, whose call-site contract (pinned in
// admin_scoring_modal.test.jsx) is exactly 4 arguments. `result` is the
// already-built decision body: attemptScoreWrite's retry spreads
// `forceDownstreamReopen: true` onto it exactly as it does the score body,
// since both are plain objects the server reads the same field off.
function submitDecisionRequest(compId, matchId, kind, decisionPayload, enchoPeriodCount, password, opts = {}) {
  const body = buildDecisionBody(kind, decisionPayload, enchoPeriodCount, opts);
  // bc-evts: the decision is the outbox's; `seenScoreVersion` is the score
  // log line count the editor showed, so a correction over a finished match
  // carries a basis that says it saw the result (I1).
  const seen = Number(opts.seenScoreVersion) || 0;
  return attemptScoreWrite({
    recordScore: (cId, mId, result, pwd, _match, o) => window.API.recordDecision(
      cId, mId, result, pwd, { ...o, ...(seen > 0 ? { seen } : {}) },
    ),
    confirmDialog: window.confirmDialog,
    compId, matchId, result: body, password: resolveDecisionPassword(password),
  });
}

// makeSubmitDecision builds the kiken/fusenpai decision submit handler shared by
// ScoreEditorModal and TeamScoreEditorModal. The two modals had byte-identical
// copies of this, so it lives here once. The returned closure:
//   - POSTs the decision, then for every decision alike (kiken included:
//     operator ruling 2026-09-26, recording a withdrawal changes only the
//     match it is recorded on) calls onAfterDecision when provided and the
//     match is not a correction (item 7: starts next match), else falls back
//     to onClose.
// Call it fresh each render so it captures the current enchoPeriodCount/password.
// withLocalDecision: what a host's onAfterDecision reads (bc-evts stage 3
// review 11, X3). The outbox's pending answer carries nothing of the decision,
// so the decision as recorded here rides on it: its kind, the side it names
// (the side that withdrew, did not appear or is barred) and the winner that
// gives, for the hosts' local advance and their skip of the barred
// competitor's other matches. A direct answer is the stored match already.
function withLocalDecision(res, match, kind, decisionBy) {
  if (!writePending(res)) return res;
  const lost = withdrawnSideKey({ decisionBy });
  const winner = lost === "a" ? match.sideB : lost === "b" ? match.sideA : null;
  return { ...res, decision: kind, decisionBy, ...(winner ? { winner } : {}) };
}

function makeSubmitDecision({
  match,
  enchoPeriodCount,
  password,
  mountedRef,
  setDecisionSubmitting,
  setDecisionErr,
  setDecisionPromptKind,
  onClose,
  // item 7: optional zero-arg callback invoked after a decision succeeds and
  // the match is not a correction. The shiaijo page wires this to
  // startMatch(next) so the operator advances without an extra tap. Kiken
  // takes this same path now (operator ruling 2026-09-26): recording a
  // withdrawal changes only the match it was recorded on, so the editor
  // advances (or closes, on a correction) exactly like any other decision.
  // Each of the withdrawn competitor's other matches is handled separately,
  // from its own existing notice, when it comes up.
  onAfterDecision,
  isComplete,       // item 7: corrections (isComplete=true) must not auto-advance
  // F5: optional pending-write handles threaded in from ScoreEditorModal so
  // a queued (offline) decision write shows the sticky "Not sent yet" banner
  // (handed the queued answer, which queuedNotice words).
  // Not provided by TeamScoreEditorModal (which has its own pending state path).
  setPendingWrite,
  pendingFnRef,
  // The server copy the screen showed, when it is not `match`'s own (the
  // individual editor's board, which may show a score-events answer's copy):
  // the decision's basis claims it and nothing more (bc-evts stage 3 review
  // 11, m1).
  seenScoreVersion,
  // The editor's isDirty (a ref or a thunk): a decision carries no draft
  // points, so over unsaved entries it asks first (R1, confirmDiscardUnsaved).
  unsaved = false,
  // What Discard entries does in the editor (drops a kept draft through its
  // owner), and the overtime the SERVER holds: a decision sent after Discard
  // entries carries the server's values for what the draft changed, so the
  // warning tells the truth (stage 5 review 8).
  onDiscard,
  serverEnchoPeriodCount,
}) {
  const submit = async (kind, { decisionBy, decisionReason }) => {
    let encho = enchoPeriodCount;
    if (holdsUnsaved(unsaved)) {
      const go = await confirmDiscardUnsaved(unsaved, recordDecisionAnyway(kind), onDiscard);
      if (!go || !mountedRef.current) return;
      if (typeof serverEnchoPeriodCount === 'number') encho = serverEnchoPeriodCount;
    }
    setDecisionSubmitting(true);
    setDecisionErr('');
    // F5: clear any prior pending-write state when the operator retries.
    if (setPendingWrite && mountedRef.current) setPendingWrite(false);
    try {
      const updated = await submitDecisionRequest(
        match.compId, match.id, kind, { decisionBy, decisionReason }, encho, password,
        { seenModifiedAt: match.modifiedAt || 0, seenScoreVersion: (seenScoreVersion !== undefined ? seenScoreVersion : match.scoreVersion) || 0 },
      );
      if (!mountedRef.current) {
        // The host read the decision from the outbox as it was queued, and
        // the decided match left this editor (the court console, stage 6
        // review 8, M4): the host still moves on, as it would have here.
        if (!writeDidNotLand(updated) && !isComplete && onAfterDecision) {
          await onAfterDecision(withLocalDecision(updated, match, kind, decisionBy));
        }
        return;
      }
      // A decision that did not land must not advance ANYTHING below this
      // line: not onAfterDecision (which the shiaijo page wires to a LOCAL
      // bracket advance), not the close. This asks the owner predicate rather
      // than `updated.queued` so BOTH not-landed shapes take the same exit --
      // queued (F5: offline/transient, will land on reconnect) and
      // `applied:false` (bc-lww1: the server refused it, so it never will).
      // The score path already guards this way; a decision that advanced a
      // bracket on a winner the server discarded is the same failure.
      //
      // LIVE since mp-jnvl: the /decision write now carries a real
      // modifiedAt (recordDecision, api_client.jsx), so the server can
      // genuinely answer with applied:false for either reason (superseded or
      // clock_skew) -- this is no longer a defence against a shape it could
      // not yet send.
      //
      // F5: a QUEUED decision enters pending-write mode so the banner shows in
      // the footer, and saves the submit closure so "Retry now" can re-invoke
      // it directly. A refused one (applied:false) does neither
      // (writeRetryable): the terminal-failure channel's not-saved banner
      // reports it, with no Retry beside it.
      if (writeDidNotLand(updated)) {
        if (setPendingWrite && writeRetryable(updated)) {
          setPendingWrite(updated);
          if (pendingFnRef) pendingFnRef.current = () => submit(kind, { decisionBy, decisionReason });
        }
        return;
      }
      // The decision is stored, so its form closes whatever the host does
      // next: a host that cannot move on (the next match's start refused, or
      // no next match to start) leaves this match on screen, where the open
      // form would offer Record again and hide the recorded decision.
      setDecisionPromptKind('');
      if (!isComplete && onAfterDecision) {
        // Item 7: fusenpai, kiken (operator ruling 2026-09-26) and any other
        // decision advances to the next match on the same court. The
        // decision was already persisted via /decision POST so we do NOT
        // issue another score PUT: just advance. Pass the resolved result
        // (winner/status) so an offline host can also advance the LOCAL
        // bracket for a decision-completed bout (mp-y3nk), matching the
        // score path's maybeAdvanceLocal.
        await onAfterDecision(withLocalDecision(updated, match, kind, decisionBy));
      } else {
        onClose();
      }
    } catch (e) {
      const msg = e?.message || 'Failed to record decision';
      // bc-cse: the operator declined attemptScoreWrite's downstream-knockout
      // confirm dialog (submitDecisionRequest above). Declining left the
      // match and the later result it depends on unchanged -- the SAME
      // cancellation copy the score path's editMatchScore shows (admin.jsx),
      // not the raw refusal message the thrown error still carries.
      if (e && e.downstreamKnockoutPlayedCancelled) {
        if (mountedRef.current) setDecisionErr(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED);
        return;
      }
      // Any other refusal is shown as the server worded it; nothing is
      // retried with a stronger flag.
      if (mountedRef.current) setDecisionErr(msg);
    } finally {
      if (mountedRef.current) setDecisionSubmitting(false);
    }
  };
  return submit;
}

// mp-m4bn: encho periods are UNBOUNDED. How many overtime periods a match
// runs, and how a still-tied match is finally resolved (hantei for an
// individual bout, daihyosen for a team encounter), is the shimpan's call,
// not the software's. The operator records what was fought, so the stepper
// only guards the lower bound; there is no maximum to clamp against.
function nextEnchoPeriod(current) {
  return current + 1;
}

function prevEnchoPeriod(current) {
  return Math.max(1, current - 1);
}

// mp-4pc: once a daihyosen (rep bout, wire position DAIHYOSEN_POSITION) exists, the encho
// rides on that sub, not the top-level match (enchoBlock suppresses the
// match-level encho when hasDaihyosen). On re-open we must restore the
// period count from the sub: else a persisted decidedByHantei replays
// without encho and the backend rejects the next save
// ("requires encho with at least one period"). Exported for vitest.
function initialEnchoPeriodsForMatch(m) {
  const daihyosen = (m.subResults || []).find(s => s.position === DAIHYOSEN_POSITION);
  if (daihyosen) return daihyosen.encho?.periodCount || 0;
  return m.encho?.periodCount || 0;
}

// daihyosenEnchoFields: pure builder for the encho/decidedByHantei wire
// fields on the daihyosen representative bout (position DAIHYOSEN_POSITION). The backend
// invariant (validation.go validateSubBout) is: encho and hantei are valid
// ONLY on the daihyosen. Encho is OPTIONAL for hantei: a tied daihyosen may
// be taken straight to a judges' decision without overtime: so the two
// fields are emitted independently: encho whenever the counter is > 0, and
// decidedByHantei explicitly (operator ruling "all results must be recorded
// into storage"): true when armed on a tied scoreline, otherwise an EXPLICIT
// false. That boolean is a LOCAL signal, not a wire field: toBackendMatchResult
// (api_serializers.jsx) consumes it and states the verdict as the "Ht" mark in
// the winner's ippon list instead - true places the mark, false leaves the
// arrays markless - so the flag itself never reaches the server.
//
// What the server reads as verdict-SILENCE is therefore the ippons, and the
// discriminator is nil versus empty: a writer that omits the arrays entirely
// (stale snapshots, quick-score) said nothing and has its stored verdict
// preserved, while an explicit [] says "zero points" and withdraws a 0-0
// verdict rather than being read as silence (engine preserveSubHantei).
// Emitting the false is what drives that stripping, so a caller that HAS a
// say still must state it.
//
// This builder itself always states the boolean when it runs, never omits
// it: that is safe because of where it is called from - the team editor
// adopts whatever verdict the server holds (its adopt effect), so a call
// into this function can never state an authoritative false about something
// it has not displayed. But `buildPatch` (admin_scoring_team.jsx) now has a
// SILENT branch (`daihyosenSilent`) that skips this builder entirely for a
// daihyosen row that is untouched this session AND nothing about it is known
// locally - that omission, not a call into this function, is how genuine
// silence reaches the wire today. This builder is still the ONLY caller that
// states the boolean, and it is still the case that when it runs, it commits
// to a boolean rather than a third "unknown" state - that part of the
// adoption argument still holds; it is simply no longer unconditional, since
// `buildPatch` may choose not to call it at all.
//
// There used to be a `hanteiKnown` parameter for exactly the case where an
// editor mounted before the verdict existed kept a mount-frozen armed flag,
// so it would have erased another device's judges' decision on the next
// unrelated autosave, and passing false made the write SILENT instead.
// Freezing bought that safety by letting the editor show something other
// than the stored result, which breaks the rule that a match has one result
// and every surface shows the same one. Adoption fixes the display and
// removes THAT blind spot (a mounted-and-stale editor), which is why the
// parameter was removed from this function; the untouched-and-never-yet-seen
// case is instead handled one layer up, by `buildPatch` choosing not to call
// this builder at all. Returns the fields to merge into the entry.
// Exported for vitest.
function daihyosenEnchoFields({ enchoPeriodCount, daihyosenTied, daihyosenHantei }) {
  const fields = { decidedByHantei: !!(daihyosenTied && daihyosenHantei) };
  if (enchoPeriodCount > 0) fields.encho = { periodCount: enchoPeriodCount };
  return fields;
}

// Pure decision for the draw-toggle action (button and keyboard shortcut).
// Returns:
//   {action: "enter"}  : set draw=true, clear pts (only when no scores exist)
//   {action: "cancel"}: set draw=false (always allowed when in draw mode)
//   {action: "noop"}   : blocked: scores exist while not in draw mode (button disabled)
// Exported for vitest.
function decideDrawToggle({ isDrawToggled, aTotal, bTotal }) {
  if (isDrawToggled) return { action: "cancel" };
  if (aTotal === 0 && bTotal === 0) return { action: "enter" };
  return { action: "noop" };
}

// shouldBlockScoringKeys: pure predicate consumed by the onKeyDown handler.
// Returns true when scoring keys (M/K/D/T/H/S and x/X draw toggle) must be
// suppressed. Currently this happens when hantei is armed: the backend
// requires a tied scoreline at that point, so any score mutation would
// produce a 400 on submit.
function shouldBlockScoringKeys({ decidedByHantei }) {
  return !!decidedByHantei;
}

// EnchoControl: collapsed by default to a small "⏱ Overtime" pill so
// it occupies <24px of vertical space in the live scoring modal. The
// full counter UI mounts only when overtime is active (enchoPeriodCount
// > 0) OR the operator clicks the pill (local showCounter state). The
// counter is the −/×N/+ stepper, unbounded above (see nextEnchoPeriod).
// Used by both ScoreEditorModal and TeamScoreEditorModal.
function EnchoControl({ enchoPeriodCount, setEnchoPeriodCount }) {
  const [showCounter, setShowCounter] = useStateA(enchoPeriodCount > 0);
  const expanded = showCounter || enchoPeriodCount > 0;
  if (!expanded) {
    return (
      <div className="encho-row encho-row--collapsed">
        <button
          type="button"
          className="encho-pill"
          data-testid="scoring-modal-encho-pill"
          onClick={() => setShowCounter(true)}
          aria-label="Show overtime (encho) controls"
        >
          <span aria-hidden="true" className="encho-pill__icon">{Icon ? <Icon name="timer" size={14} /> : "⏱"}</span>
          Overtime
        </button>
        <GlossaryHintAS name="encho" />
      </div>
    );
  }
  return (
    <div className="encho-row encho-row--expanded">
      <label className="encho-row__label">
        <input
          data-testid="scoring-modal-encho-checkbox"
          type="checkbox"
          checked={enchoPeriodCount > 0}
          onChange={(e) => {
            const next = e.target.checked ? Math.max(1, enchoPeriodCount) : 0;
            setEnchoPeriodCount(next);
            if (!e.target.checked) setShowCounter(false);
          }}
        />
        Encho started (overtime)
      </label>
      <GlossaryHintAS name="encho" />
      {enchoPeriodCount > 0 && (
        <div className="encho-row__stepper">
          <button
            type="button"
            className="btn btn--sm encho-row__btn"
            onClick={() => setEnchoPeriodCount(prevEnchoPeriod)}
            disabled={enchoPeriodCount <= 1}
            aria-label="Decrease overtime period count"
          >−</button>
          <span className="encho-row__count">×{enchoPeriodCount}</span>
          <button
            type="button"
            className="btn btn--sm encho-row__btn"
            onClick={() => setEnchoPeriodCount(nextEnchoPeriod)}
            aria-label="Increase overtime period count"
          >+</button>
        </div>
      )}
    </div>
  );
}

// Render the inline kiken/fusenpai prompt that replaces the score controls
// while open. Side picker uses radio inputs labelled "SHIRO (White)" / "AKA
// (Red)" to stay consistent with the score board legend; the value submitted
// to the backend is "shiro" or "aka" per DecisionRequest.Validate. The reason
// is always optional, a reopened match included: a match can be reopened
// without any reason, and ending it again asks for none (operator ruling
// 2026-09-25).
function DecisionPrompt({ kind, sideA, sideB, askReason, onCancel, onSubmit, submitting }) {
  // No side is preselected (operator decision 2026-10-07, bc-dsid): a
  // withdrawal or no-show recorded against the wrong side inverts the result,
  // so the operator must pick, and Record stays off until they do.
  const [side, setSide] = useStateA("");
  const [reason, setReason] = useStateA("");
  const showReason = askReason;
  // Display rule (locked, glossary.md §Display rule): render the
  // romaji term ALONE: the popover (via <Term>) carries the gloss.
  // We keep "Decision" untouched (it's already plain English) and
  // wrap the kendo terms so a volunteer hovering/tapping the title
  // gets the full tooltip. The title is not a control, so its Term is fine;
  // the side options below are labels and carry plain text (the rule above
  // TermAS).
  const isKiken = window.isKikenDecision(kind);
  const title = isKiken || kind === "fusenpai"
    ? React.createElement(TermAS, { name: kind }, withdrawalLabel(kind))
    : "Decision";
  // Each side's words and competitor, in one place: Shiro is sideB, Aka is
  // sideA, as everywhere in the editors. The side word comes from sideWord.
  const sides = {
    shiro: { name: sideB?.name },
    aka: { name: sideA?.name },
  };
  const nameOf = (key) => sides[key].name || sideWord(key);

  const submit = (e) => {
    e?.preventDefault?.();
    if (submitting || !side) return;
    onSubmit({ decisionBy: side, decisionReason: showReason ? reason.trim() : "" });
  };

  const verb = isKiken ? "withdrew" : "did not show up";
  const sideOption = (key) => (
    <label className={`radio-pill decision-prompt__side${side === key ? " is-active" : ""}`}>
      <input type="radio" name="decision-side" value={key} checked={side === key} onChange={() => setSide(key)} />
      <span>{sideWithColour(key)}{sides[key].name ? `: ${sides[key].name}` : ""}</span>
    </label>
  );

  return (
    <form className="decision-prompt" onSubmit={submit} style={{ border: "1px solid var(--line)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2)" }}>
      <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 8 }}>{title}</div>
      <div style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 12 }}>
        <div style={{ fontWeight: 600 }}>{isKiken ? "Which side withdrew?" : "Which side did not show up?"}</div>
        <div className="radio-group">
          {sideOption("shiro")}
          {sideOption("aka")}
        </div>
        {side ? (
          <div className="decision-prompt__consequence" data-testid="decision-prompt-consequence" aria-live="polite">
            {withdrawalConsequence(kind, nameOf(side), nameOf(side === "shiro" ? "aka" : "shiro"))}
          </div>
        ) : (
          <div className="decision-prompt__hint" data-testid="decision-prompt-hint">
            Pick the side that {verb}.
          </div>
        )}
        {showReason && (
          <label style={{ display: "flex", flexDirection: "column", gap: 4, marginTop: 4 }}>
            <span style={{ fontWeight: 600 }}>
              Reason (optional, ≤200 chars)
            </span>
            <input
              type="text"
              className="input"
              maxLength={200}
              value={reason}
              onInput={(e) => setReason(e.target.value)}
              placeholder="e.g. injury, no-show"
              data-testid="decision-reason"
            />
          </label>
        )}
      </div>
      <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 10 }}>
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={submitting}>Cancel</button>
        <button type="submit" className="btn btn--primary btn--sm" disabled={submitting || !side}>
          {submitting ? "Saving…" : side ? `Record: ${sideWord(side).toUpperCase()} ${verb}` : "Record"}
        </button>
      </div>
    </form>
  );
}

// BarredMatchNotice lives in its own leaf (barred_match_notice.jsx): it
// imports only ineligible_match.jsx and write_result.jsx, so a surface that
// does not otherwise need this module's graph (which pulls in bracket.jsx
// for sideMarks) can import it directly without that graph riding along --
// see that file's header. Re-exported here so the editors' existing
// `from './admin_scoring_shared.jsx'` import lists need no change.

// sideColorName: the human-readable side name for a "shiro"/"aka" colour key.
// Named for the colour it takes so it cannot be confused with admin_helpers.jsx's
// sideName(side), which takes a side object and returns a participant name.
// Used in aria-labels only: the foul counter's two buttons below and the
// individual editor's slot grid. The sr-only side labels come from SideLabel
// (side_cell.jsx), never from here. (It named the header badges until bc-sccl
// removed them; a reader tracing the badge rule from here would look for
// markup that no longer exists.)
function sideColorName(color) {
  // Delegates: side_cell.jsx owns the word, so the two can never disagree.
  return sideWord(color);
}

// Reusable foul counter: independent +/- buttons per side with clear labeling.
// The `+` button delegates to `onIncrement` which applies the
// applyFoulIncrement rule (auto-award H + reset at the 2-foul boundary);
// `setFouls` is kept for the `−` button (simple decrement). After the
// 2-foul auto-award the awarded H lives in the opponent's pts array, so
// the counter shows only "outstanding fouls not yet discharged."
function FoulCounter({ fouls, setFouls, onIncrement, color, disabled }) {
  // bc-dtip: a double tap on `+` recorded two fouls, and the second one awards
  // an H to the opponent. A repeat pointer tap within TAP_BOUNCE_MS is the
  // bounce, not a second foul. `−` is not guarded: one foul too few is visible
  // and one tap to undo, and nobody gains a point from it.
  const incTapRef = useRefA(null);
  const onIncrementTap = (ev) => {
    if (acceptTap(incTapRef, ev)) onIncrement();
  };
  // color is "shiro" or "aka": surface as data-testid so Playwright probes
  // (T023a) can target each side without depending on the className.
  // `disabled` freezes the `+` button when the bout is already decided:
  // a 2nd-foul auto-award in that state would create an invalid 2-2.
  // In the individual editor the two counters sit BELOW the board in
  // .sb-fouls, left = Shiro, right = Aka (the app-wide column convention),
  // with the Aka box tinted. The visible label names no side (it is
  // conveyed by position and tint alone, matching the bout rows above), so
  // "Fouls" stays unprefixed; the aria-labels below carry the side for
  // assistive tech.
  return (
    <div className={`foul-counter foul-counter--${color}`} data-testid={`scoring-modal-hansoku-${color}`}>
      <div className="foul-counter__label">Fouls</div>
      <div className="foul-counter__controls">
        <button type="button" className="foul-counter__btn foul-counter__btn--dec" aria-label={`Remove a ${sideColorName(color)} foul`} onClick={() => setFouls(Math.max(0, fouls - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 1 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
        </div>
        <button type="button" className="foul-counter__btn foul-counter__btn--inc" aria-label={`Add a ${sideColorName(color)} foul`} onClick={onIncrementTap} disabled={disabled}>+</button>
      </div>
    </div>
  );
}

// LineupNameInput: typeable player picker/writer for a team lineup slot.
// Type to filter the team roster (matches show in a dropdown); click one or
// press Enter to pick. Unlike a plain picker, a name not on the roster can be
// ADDED as-is via the "+ Add …" row: the lineup stores a free name string,
// which is what an operator needs when entering a late substitute while bouts
// are running. The × clears the slot. Keyboard: ↑/↓ move, Enter picks, Esc closes.
//
// roster entries are either plain name strings (admin_schedule_lineup.jsx's
// suggestion list) or squad-member objects `{ id, index, name, label }`
// (admin_scoring_team.jsx's rosterForSide, bc-dnst: the row's list must show
// every numbered slot the team can field, blank ones included, so the
// operator can pick a number and name it). Both shapes are normalised to
// `{ name, label, isObject }` once up front; isObject is what a plain
// string entry never gets, so it alone decides whether the dropdown row
// renders a number chip. Picking an object entry threads the WHOLE entry
// through onSelect as a second argument (`onSelect(name, entry)`); a plain
// string match, the "+ Add" row, and a typed-query commit call onSelect
// with the name alone, exactly as before.
// `clearable` (bc-dnst): show the clear button even with an empty value, for a
// host whose position is occupied by a picked squad slot that has no name yet
// (the Up Next lineup panel); without it a nameless placement had no way out.
// `onListPick` is told of a pick made by tapping a row of the open list (an
// option or "+ Add"), never of a typed commit, Enter, the clear button or a
// click outside: only that pick closes the list under the finger.
// The name list is absolutely placed under its input. It opens down when the room
// below fits the list, else toward the larger room, and caps its height to that
// room. The room is bounded by the viewport (the visual one, which the iPad
// keyboard shrinks), by the team sheet's pinned header (top) and footer (bottom:
// the inline dock, the overlay's foot), and by the nearest ancestor that cuts off
// what overflows it (the overlay's scroll body, the inline panel's clip, the
// at-court panel's scroll box): a list taller than that room would be cut off or
// stretch the ancestor's scroll. A host with none of these (the Lineups page)
// uses the viewport edges. (bc-tmfd)
const LINEUP_LIST_MAX_H = 240;
const LINEUP_LIST_GAP = 8;
const LINEUP_LIST_MIN_H = 72;

// Whether an element cuts off what overflows it vertically: hidden, auto, scroll
// or clip on the vertical axis, or hidden/auto/scroll on the horizontal one,
// which makes the vertical axis cut off too.
function clipsVertically(el) {
  const { overflowX, overflowY } = getComputedStyle(el);
  const cuts = (v) => v === "hidden" || v === "auto" || v === "scroll";
  return cuts(overflowY) || overflowY === "clip" || cuts(overflowX);
}

// The nearest ancestor that clips. The page itself is no ancestor of this kind:
// the viewport edges stand for it (and a dialog locks the body's scroll).
function clippingAncestor(el) {
  for (let a = el.parentElement; a && a !== document.body && a !== document.documentElement; a = a.parentElement) {
    if (clipsVertically(a)) return a;
  }
  return null;
}

// The elements that bound a name list's room, found once when the list opens: the
// page scrolling and the keyboard rising move the input, never these.
function lineupListEdges(wrapper) {
  const scope = wrapper.closest(".scoring-panel, .editor-modal");
  return {
    pin: scope && scope.querySelector(".team-sheet-pin"),
    foot: scope && scope.querySelector(".editor-modal__foot--nav"),
    clip: clippingAncestor(wrapper),
  };
}

function lineupListPlacement({ pin, foot, clip }, bar) {
  // The visual viewport shrinks under the iPad keyboard where innerHeight does not.
  const vv = window.visualViewport;
  const viewTop = vv ? vv.offsetTop : 0;
  const viewH = vv ? vv.offsetTop + vv.height : window.innerHeight;
  const clipRect = clip ? clip.getBoundingClientRect() : null;
  const topEdge = Math.max(viewTop, pin ? pin.getBoundingClientRect().bottom : 0, clipRect ? clipRect.top : 0, 0);
  const r = bar.getBoundingClientRect();
  // The footer is the bottom edge wherever it sits below the input: the inline
  // dock is sticky and the overlay's is always visible under the scroll body.
  const footTop = foot ? foot.getBoundingClientRect().top : viewH;
  const dockEdge = foot && footTop >= r.bottom ? Math.min(viewH, footTop) : viewH;
  const bottomEdge = clipRect ? Math.min(dockEdge, clipRect.bottom) : dockEdge;
  const roomBelow = bottomEdge - r.bottom;
  const roomAbove = r.top - topEdge;
  const up = roomBelow < LINEUP_LIST_MAX_H && roomAbove > roomBelow;
  const room = (up ? roomAbove : roomBelow) - LINEUP_LIST_GAP;
  const maxHeight = room < LINEUP_LIST_MAX_H ? Math.max(LINEUP_LIST_MIN_H, Math.floor(room)) : undefined;
  return { up, maxHeight };
}

function LineupNameInput({ value, roster, onSelect, onListPick, disabled, ariaLabel, color, clearable, inputId, placeholder = "Add player…" }) {
  const [query, setQuery] = useStateA("");
  const [open, setOpen] = useStateA(false);
  const [active, setActive] = useStateA(-1); // -1 = no explicit selection yet
  const ref = useRefA(null);
  const barRef = useRefA(null);
  const listRef = useRefA(null);
  const [placement, setPlacement] = useStateA({ up: false, maxHeight: undefined });
  // Measured on open, then again whenever the page scrolls or the viewport
  // changes (the keyboard rising), one frame at a time. What bounds the room (the
  // pinned bars, the clipping ancestor) is found once, on open.
  useLayoutEffectA(() => {
    if (!open || !ref.current) return;
    const edges = lineupListEdges(ref.current);
    let frame = 0;
    const measure = () => {
      frame = 0;
      if (!ref.current || !barRef.current) return;
      const next = lineupListPlacement(edges, barRef.current);
      setPlacement(p => (p.up === next.up && p.maxHeight === next.maxHeight ? p : next));
    };
    const schedule = (e) => {
      // The list scrolling itself moves nothing it is placed against.
      const target = e && e.target;
      if (target instanceof Node && listRef.current && listRef.current.contains(target)) return;
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    const vv = window.visualViewport;
    window.addEventListener("scroll", schedule, { capture: true, passive: true });
    window.addEventListener("resize", schedule);
    if (vv) { vv.addEventListener("resize", schedule); vv.addEventListener("scroll", schedule); }
    return () => {
      window.removeEventListener("scroll", schedule, { capture: true });
      window.removeEventListener("resize", schedule);
      if (vv) { vv.removeEventListener("resize", schedule); vv.removeEventListener("scroll", schedule); }
      if (frame) cancelAnimationFrame(frame);
    };
  }, [open]);
  // Guards against double-commit when click-outside fires first and the blur
  // event arrives immediately after (mousedown precedes blur in browser order).
  const skipBlurRef = useRefA(false);
  const q = query.trim();
  const ql = q.toLowerCase();
  // `raw` keeps the ORIGINAL roster item (unmodified: no isObject marker,
  // no spread copy) so a caller picking an object entry gets back exactly
  // what it put into `roster`, never an internal-shape lookalike. Memoised
  // (bc-rvfx) on `roster` alone: this component is mounted once per lineup
  // row and its own `query` state changes on every keystroke, which used to
  // re-map the whole roster on every one of those renders even though
  // `roster` itself had not changed.
  const entries = useMemoA(() => (roster || []).map(r =>
    typeof r === "string"
      ? { name: r, label: "", isObject: false, raw: r }
      : { name: r?.name || "", label: r?.label || "", isObject: true, raw: r }
  ), [roster]);
  // matches/exact still recompute on every keystroke (ql is one of their own
  // dependencies), but no longer on a re-render this component causes for
  // itself where neither `entries` nor `ql` changed -- arrow-key navigation
  // (`active`) and open/close (`open`) are the common case, and previously
  // re-filtered and re-capped the whole roster on each of those too.
  const { matches, exact } = useMemoA(() => {
    const matches = entries.filter(e => {
      if (!ql) return true;
      const nameHit = (e.name || "").toLowerCase().includes(ql);
      const labelHit = (e.label || "").toLowerCase().includes(ql);
      return nameHit || labelHit;
    });
    // Capped only while FILTERING. With no query the operator is BROWSING this
    // team's own numbered slots, and every one has to be reachable: the list
    // grew from "the team's named members" to every seeded slot (team size plus
    // two reserves, bc-dnst) plus any assigned-name tail, so a flat cap of 12
    // silently hid the highest slots on an 11-person team -- exactly the reserve
    // slots the +2 exists to expose, and the operator could only reach them by
    // guessing that typing narrows the list. The dropdown scrolls, so showing
    // the whole of a team's own roster costs nothing; a QUERY can still match
    // the long legacy tail, which is what the cap is for.
    if (ql) matches.splice(12);
    // Scans `entries`, not the capped `matches`: an exact match past the cap
    // must still count as "already on the roster", or the "+ Add" row would
    // offer to add a name that is already there, just scrolled out of view.
    const exact = entries.some(e => (e.name || "").toLowerCase() === ql);
    return { matches, exact };
  }, [entries, ql]);
  const canAddNew = q.length > 0 && !exact;
  const optionCount = matches.length + (canAddNew ? 1 : 0);

  // On click-outside: commit a typed but un-submitted name rather than
  // discarding it. Without this, tabbing quickly between slots loses names.
  // Note: option onMouseDown uses preventDefault so focus stays in the input;
  // the option itself commits on CLICK (bc-flst), so the whole tap lands on
  // the option before the list closes. The outside mousedown only fires when
  // clicking a genuinely external target (q is already "" after commit).
  window.useClickOutside(ref, () => {
    if (q) {
      skipBlurRef.current = true;
      commit(q);
    } else {
      setOpen(false);
      setQuery("");
    }
  }, open);

  // commit's optional second argument is the matched roster entry (when the
  // commit came from picking one). It is called with ONE argument (never a
  // trailing explicit `undefined`) for the "+ Add" row, a typed-query
  // commit, and the clear button -- calling onSelect(name, undefined)
  // there instead would change every existing single-arg
  // `onSelect(name)`/`toHaveBeenCalledWith(name)` consumer's call shape
  // (Vitest's toHaveBeenCalledWith does not ignore a trailing explicit
  // undefined), so this stays a real arity difference, not a value one.
  const commit = (name, entry) => {
    if (entry !== undefined) onSelect(name, entry);
    else onSelect(name);
    setOpen(false); setQuery(""); setActive(-1);
  };
  // A string-origin entry commits exactly like a plain string always did
  // (ONE argument); an object entry threads its ORIGINAL raw value back as
  // onSelect's second argument.
  const commitEntry = (entry) => (
    entry.isObject ? commit(entry.name || "", entry.raw) : commit(entry.name)
  );
  const onKeyDown = (e) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      // First ArrowDown opens the list highlighting the FIRST option (index 0);
      // subsequent presses move down. Without the open-guard the first row is
      // skipped (jumps straight to index 1).
      if (!open) { setOpen(true); setActive(0); }
      else setActive(a => Math.min(a + 1, Math.max(0, optionCount - 1)));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(a => Math.max(a - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      // Commit only on an explicit choice: a navigated list row, the add-row,
      // or a typed query. A bare focus + Enter (active === -1, empty query)
      // must NOT overwrite the current slot.
      if (active >= 0 && active < matches.length) commitEntry(matches[active]);
      else if (active === matches.length && canAddNew) commit(q);
      else if (q) commit(q);
    } else if (e.key === "Escape") {
      // One Escape closes one layer: a DRAWN list takes it, and keeps it from the
      // overlay score editor's own Escape (a window listener that closes the
      // editor). No list drawn (it is drawn only with an option to show: a closed
      // box, or an open one with nothing to offer) leaves it to the editor.
      if (!open || optionCount === 0) return;
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
      setQuery("");
    }
  };

  return (
    <div className={`pmf lineup-name lineup-name--${color}${!value ? " lineup-name--empty" : ""}`} ref={ref}>
      <div className="pmf__bar lineup-name__bar" ref={barRef}>
        <input
          id={inputId}
          className="pmf__input"
          placeholder={value || placeholder}
          aria-label={ariaLabel}
          disabled={disabled}
          value={open ? query : (value || "")}
          onChange={(e) => { setQuery(e.target.value); setOpen(true); setActive(-1); }}
          onFocus={() => { setOpen(true); setQuery(""); setActive(-1); }}
          // A tap on a box that still has focus (after Escape closed the
          // list, or after a pick in a host that keeps the box enabled, e.g.
          // admin_schedule_lineup.jsx) fires no focus event, so the tap
          // itself reopens the list (bc-flst). A host that disables the box
          // while the pick's own write is out (the team editor) drops focus
          // instead, so onFocus reopens it there. This onClick only ever
          // opens, never toggles closed, so the click that follows the first
          // focus is a no-op.
          onClick={() => { if (!open && !disabled) { setOpen(true); setQuery(""); setActive(-1); } }}
          onKeyDown={onKeyDown}
          onBlur={(e) => {
            // Do not close if focus moved to something inside the wrapper
            // (e.g. a dropdown option button).
            if (ref.current && ref.current.contains(e.relatedTarget)) return;
            // Skip if the click-outside path already committed (it fires on
            // mousedown, before blur; it sets skipBlurRef to prevent a double
            // onSelect call).
            if (skipBlurRef.current) { skipBlurRef.current = false; return; }
            // Tab-away with a typed name: commit it so the operator does not
            // lose a batch-entry value.
            if (q) commit(q);
            else if (open) { setOpen(false); setQuery(""); }
          }}
        />
        {(value || clearable) && !disabled && (
          <button type="button" className="lineup-name__clear" title="Clear player" aria-label="Clear player"
            onMouseDown={(e) => e.preventDefault()} onClick={() => commit("")}>×</button>
        )}
      </div>
      {open && optionCount > 0 && (
        <div
          ref={listRef}
          className="pmf__dropdown lineup-name__dropdown"
          style={{
            ...(placement.up ? { top: "auto", bottom: "calc(100% + 4px)" } : null),
            maxHeight: placement.maxHeight ?? LINEUP_LIST_MAX_H,
          }}
        >
          {matches.map((entry, i) => (
            <button type="button" key={entry.isObject ? (entry.raw?.id || entry.raw?.index) : entry.name}
              className={`pmf__option ${i === active ? "pmf__option--active" : ""}`}
              onMouseDown={(e) => e.preventDefault()} onClick={() => { onListPick?.(); commitEntry(entry); }}>
              {entry.isObject ? (
                <>
                  <span className="pmf__opt-label">{entry.label}</span>
                  <span className={`pmf__opt-name${entry.name ? "" : " pmf__opt-name--blank"}`}>{entry.name || "no name yet"}</span>
                </>
              ) : (
                <span className="pmf__opt-name">{entry.name}</span>
              )}
            </button>
          ))}
          {canAddNew && (
            <button type="button"
              className={`pmf__option lineup-name__add ${active === matches.length ? "pmf__option--active" : ""}`}
              onMouseDown={(e) => e.preventDefault()} onClick={() => { onListPick?.(); commit(q); }}>
              <span className="pmf__opt-name">+ Add “{q}”</span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ReasonPrompt: inline form for collecting a mandatory audit justification
// when an operator corrects a completed match or edits a lineup mid-match.
//
// Renders a preset <select> + optional free-text note; calls onConfirm with
// the combined "<category>: <note>" string (or just "<category>" when note is
// empty). Calls onCancel to dismiss without submitting.
//
// presets: array of string labels (e.g. ["Scoring error","Wrong competitor",…])
// Registered as window.ReasonPrompt for use from other modules.
// Module-level empty default keeps the prop reference stable across renders
// (a `= []` inline default allocates a fresh array every call).
const REASON_PROMPT_NO_PRESETS = [];
function ReasonPrompt({ label = "Reason for change", presets = REASON_PROMPT_NO_PRESETS, onConfirm, onCancel, submitting = false }) {
  const [category, setCategory] = useStateA(presets[0] || "");
  const [note, setNote] = useStateA("");
  const built = note.trim() ? `${category}: ${note.trim()}` : category;
  const canConfirm = !!category && !submitting;
  const submit = (e) => {
    e.preventDefault();
    if (!canConfirm) return;
    onConfirm(built);
  };
  return (
    <form
      className="reason-prompt"
      onSubmit={submit}
      style={{ border: "1px solid var(--line)", borderRadius: 6, padding: 12, marginTop: 8, marginBottom: 8, background: "var(--bg-2)" }}
    >
      <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>{label}</div>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <select
          className="input"
          style={{ flex: "0 0 auto", minWidth: 160 }}
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          disabled={submitting}
          aria-label="Reason category"
        >
          {presets.map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <input
          className="input"
          style={{ flex: "1 1 160px", minWidth: 100 }}
          type="text"
          placeholder="Optional note…"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          disabled={submitting}
          aria-label="Reason note"
        />
      </div>
      <div style={{ display: "flex", gap: 8, marginTop: 8, justifyContent: "flex-end" }}>
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={submitting}>Cancel</button>
        <button type="submit" className="btn btn--sm btn--primary" disabled={!canConfirm}>
          {submitting ? "Saving…" : "Confirm"}
        </button>
      </div>
    </form>
  );
}

window.ReasonPrompt = ReasonPrompt;

const CORRECTION_PRESETS = ["Scoring error", "Wrong competitor", "Data entry", "Other"];

// withdrawalLabel: the operator's name for a match-level withdrawal decision,
// the ONE copy of it: DecisionPrompt's title and the Recorded line in both
// editors read it. Any kiken that is not the injury kind reads as voluntary,
// the legacy bare "kiken" included, which the server loads as voluntary too.
function withdrawalLabel(decision) {
  if (decision === "fusenpai" || decision === "fusensho") {
    // bc-cse: derives its word from decisionWord (write_result.jsx) rather
    // than restating the map -- "fusensho" used to read "Default win
    // (fusensho)" here, which named a thing kendo does not have alongside
    // the thing it does (operator ruling 2026-10-04).
    const word = decisionWord(decision);
    return word.charAt(0).toUpperCase() + word.slice(1);
  }
  // bc-rawm: a match-level fusensho, the shape a match-level default win
  // takes when the operator taps "Record fusensho for <opponent>" on a
  // match against an already-withdrawn competitor -- the match's own notice
  // (BarredMatchNotice / defaultWinDecisionBodyForSide, ineligible_match.jsx)
  // -- falls through the branch above. Short label for the same "fact" slots
  // kiken/fusenpai use (e.g. admin_scoring_team.jsx's team-summary-decision);
  // the fuller "Fusensho for <winner>" sentence is composed in
  // RecordedWithdrawal, which needs the winner's name this function does not
  // have.
  return decision === "kiken-injury" ? "Kiken – Injury" : "Kiken – Voluntary";
}

// withdrawalConsequence: the one sentence that says what recording a
// withdrawal or no-show does, shown once the operator has picked the side
// (DecisionPrompt). The kiken-injury kind is the reinstateable one; every
// other kiken, the legacy bare one included, and the no-show are permanent.
function withdrawalConsequence(kind, withdrawnName, otherName) {
  const barred = kind === "kiken-injury"
    ? "cannot fight again unless reinstated"
    : "cannot fight again in this competition";
  return `${withdrawnName} ${barred}. ${otherName} wins this match.`;
}

// withdrawnSideOf: the side a recorded withdrawal names as the one that
// withdrew (or did not appear), or null when the match cannot say.
// decisionBy is authoritative (aka = sideA, shiro = sideB, the server's own
// mapping in RecordDecisionTx). A ruling without it falls back to the side the
// recorded winner is NOT, attributed by sameCompetitor (id first), never by a
// bare name comparison.
function withdrawnSideOf(m) {
  const key = withdrawnKeyOf(m);
  if (key === "a") return m.sideA || null;
  if (key === "b") return m.sideB || null;
  return null;
}

// withdrawnKeyOf: the same answer as withdrawnSideOf, as the editors' side key
// ("a" = Aka/sideA, "b" = Shiro/sideB), or "" when the match cannot say.
function withdrawnKeyOf(m) {
  return withdrawnSideKey(m);
}

// withdrawalInForce: the one statement of "a recorded withdrawal is in force
// on this correction": the match is completed and a withdrawal (any kiken, or
// fusenpai) decided it. Both editors ask it: it gates RecordedWithdrawal, and
// the individual editor locks the winner's default-win maru while it holds
// (Save correction keeps the withdrawal and its maru, engine
// keptWithdrawalScoreline, so the maru is not the operator's to edit). It stops
// holding the moment the withdrawal is cleared, because the reopen puts the
// match back to running.
function withdrawalInForce(m) {
  // bc-rawm: widened to the default-win class. A match-level fusensho is
  // the shape a match-level default win takes for a match against a
  // competitor already withdrawn elsewhere -- recorded from that match's own
  // notice (BarredMatchNotice / the queue row's Record default win, both
  // building the body via defaultWinDecisionBodyForSide): same default-win
  // outcome as a kiken/fusenpai on this match, so
  // it belongs in the same "a recorded withdrawal decided this" class as
  // kiken/fusenpai, not a separate one. So this asks the match-level
  // default-win class, whose one JS owner is isTeamDefaultWinDecision
  // (team_default_credit.jsx), rather than ORing a fusensho arm onto a
  // narrower kiken/fusenpai check.
  return m.status === "completed" && isTeamDefaultWinDecision(m.decision);
}

// The unsaved-entries warning (R1) has one owner, unsaved_entries.jsx.
export {
  UNSAVED_ENTRIES_DISCARDED, REOPEN_ANYWAY, QUEUE_AND_REOPEN_ANYWAY, ADD_REP_BOUT_ANYWAY,
  REMOVE_REP_BOUT_ANYWAY, LEAVE_MATCH_ANYWAY, recordDecisionAnyway, unsavedDiscardDialog,
  holdsUnsaved, confirmDiscardUnsaved,
};

// usePublishUnsaved: an editor hands its host a way to ask whether it holds
// unsaved entries (`hostRef`, the host's ref, passed down as the editor's
// `hostUnsavedRef`), so a host's leave action asks R1's question before it
// unmounts or swaps the editor. The thunk reads the editor's own `unsaved`
// as it is when asked; an editor that goes away takes it back. The thunk also
// carries the editor's own Discard entries as `discard` (`onDiscard`, read
// when called), so a host confirm that says the entries are discarded drops
// them at once, as the editor's own actions do (stage 5 review 9, m1;
// unsaved_entries.jsx `discardUnsaved`).
export function usePublishUnsaved(hostRef, unsaved, onDiscard) {
  const probe = useRefA(null);
  const unsavedRef = useRefA(unsaved);
  unsavedRef.current = unsaved;
  const discardRef = useRefA(onDiscard);
  discardRef.current = onDiscard;
  if (!probe.current) {
    probe.current = () => !probe.current.cleared && holdsUnsaved(unsavedRef.current);
    // Run only while this editor is mounted: a host holding the thunk past
    // the editor's unmount (a question answered after it went) drops nothing.
    probe.current.discard = () => {
      if (probe.current.gone || typeof discardRef.current !== "function") return;
      discardRef.current();
      // The entries are dropped at once, but the editor's own unsaved ref is
      // set by its next render: until then it still reports them, and an exit
      // that re-asks the thunk after the answer (the leave's `go`) would ask a
      // second time (stage 5 review 11, minor 1). Say clean now, in the ref
      // the editor keeps and in the thunk; the next render states the truth.
      const held = unsavedRef.current;
      if (held && typeof held === "object" && "current" in held) held.current = false;
      probe.current.cleared = true;
    };
  }
  // A render states the editor's own answer again: an entry made after the
  // discard is unsaved.
  probe.current.cleared = false;
  if (hostRef && hostRef.current !== probe.current) hostRef.current = probe.current;
  useEffectA(() => () => { if (hostRef && hostRef.current === probe.current) hostRef.current = null; }, [hostRef]);
  useEffectA(() => { probe.current.gone = false; return () => { probe.current.gone = true; }; }, []);
}

// useMatchReopen: the one client of POST .../reopen and its court-busy remedy
// POST .../requeue-blocker-and-reopen, for every editor that reopens a match:
// the kachinuki Reopen and Clear withdrawal and reopen in the individual and
// team editors, both one tap with no reason. It was
// the kachinuki editor's own state machine; lifting it here is what lets the
// individual editor offer the same door without a second copy of it.
//
// Three outcomes, each of which the returned state carries:
//   - success: the editor STAYS OPEN and follows the match to running in
//     place, for both doors (the kachinuki Reopen and Clear withdrawal and
//     reopen): `notice`, which names any later match the reopen also
//     reopened, is only readable in an editor that is still there. busy
//     clears on the success response and `landed` is set, so the button says
//     the reopen landed rather than "Reopening…" even when the match_updated
//     broadcast that flips the prop never arrives, while a double tap still
//     cannot post a second reopen the server would 409 as "not completed".
//     The effect below clears `landed` once the match is running;
//   - court_busy: `conflict` names the match holding the court, and
//     requeueBlocker() is the remedy (it carries the same reason and, once
//     given, the same downstream confirmation);
//   - a later knockout match with its own result (downstream_knockout_played):
//     attemptScoreWrite, the score path's confirm-and-retry loop, asks the
//     operator with the same dialog a correction gets and retries with the
//     force flag (operator ruling 2026-09-24: "The operator just needs to be
//     aware of the consequences"). What that reopened is named in `notice`;
//     declining leaves everything as it was and says so in `err`.
// Every other refusal is the server's sentence, shown verbatim in `err`.
function useMatchReopen({ match, password, isComplete, own, unsaved = false, onDiscard, allowCourtForce = false }) {
  const mountedRef = useRefA(true);
  // The unsaved-entries warning is open (R1): a second tap meanwhile asks
  // nothing more.
  const askingRef = useRefA(false);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  const [busy, setBusy] = useStateA(false);
  // The server answered success; the match prop may not have flipped yet.
  const [landed, setLanded] = useStateA(false);
  const [err, setErr] = useStateA("");
  // { court, matchId, compId, message, label, reason, force }: the match
  // ALREADY running on this court, plus what the refused reopen carried.
  // `label` is the server's operator-facing name for it, when sent -- it
  // names no COMPETITOR (bc-cse), so ReopenFeedback prefers the fetched
  // blockerLabel below over it. `message` is the server's raw refusal
  // sentence, kept on the object but no longer rendered (see ReopenFeedback:
  // it named an internal id and told the operator to finish that match,
  // contradicting this panel's own button).
  const [conflict, setConflict] = useStateA(null);
  // Best-effort "Match 2 · Shiro vs Aka" for the blocking match. The operator
  // is about to wipe that match's score, so naming its competitors (not just
  // the server's opaque id) is a safety property, not decoration.
  const [blockerLabel, setBlockerLabel] = useStateA("");
  const [notice, setNotice] = useStateA("");
  // Set once the operator has confirmed the downstream reopen, so the
  // court-busy remedy that may follow does not ask them a second time.
  const confirmedRef = useRefA(false);

  // Once the match reopens (status flips to running), a stale error or
  // conflict describes a completed-state action that no longer applies. When it
  // completes AGAIN (a new result or decision), the reopen notice is history:
  // it described that reopen, not the match now on screen. Keyed on the value,
  // so the notice set while the match is still completed survives the flip.
  useEffectA(() => {
    if (!isComplete) { setErr(""); setConflict(null); setBusy(false); setLanded(false); }
    else setNotice("");
  }, [isComplete]);

  const applyFailure = (e, reason) => {
    if (e && e.downstreamKnockoutPlayedCancelled) {
      setConflict(null);
      setErr(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED);
      return;
    }
    const msg = String(e?.message || "Failed to reopen match");
    if (e?.code === "court_busy" && e?.matchId) {
      setErr("");
      setConflict({
        court: e.court || match.court || "",
        matchId: e.matchId,
        compId: e.compId || match.compId,
        message: msg,
        // The server's own operator label for the blocking match
        // (api_client.jsx's reopenFailureError carries it through when
        // present). bc-cse: it names no competitor ("Pool A · Match 2"), so
        // ReopenFeedback prefers the best-effort FETCHED blockerLabel over
        // it and falls back to this only when that fetch has not resolved;
        // either way the heading never falls back to printing the raw
        // matchId.
        label: e.label || "",
        reason,
        force: confirmedRef.current,
      });
      return;
    }
    setConflict(null);
    setErr(msg);
  };

  // bc-evts: a reopen (and the requeue remedy) is the outbox's: the panel
  // reads it as landed at once, and a refusal arriving later through the
  // outbox (court busy, a downstream match, anything else) is claimed here
  // (`onRefusal`) and put back on the panel, so nothing waits on the network.
  const noteReopened = (ids) => {
    const n = downstreamKnockoutReopenedNotice(ids);
    if (n && mountedRef.current) setNotice(n);
  };
  const run = (call, reason, force, onRefusal, onForced, forceCourt = false) => attemptScoreWrite({
    recordScore: (_compId, _matchId, result, pw, _match, o) => call(pw, {
      reason,
      force: !!result.forceDownstreamReopen,
      forceCourt: !!result.forceCourt,
      seen: match.scoreVersion || 0,
      onRefusal: o && o.onRefusal,
    }),
    onRefusal,
    onForced,
    onDownstreamReopened: noteReopened,
    confirmDialog: async (opts) => {
      const ok = await window.confirmDialog(opts);
      if (ok) confirmedRef.current = true;
      return ok;
    },
    compId: match.compId,
    matchId: match.id,
    result: {
      ...(force ? { forceDownstreamReopen: true } : {}),
      ...(forceCourt ? { forceCourt: true } : {}),
    },
    password: resolveDecisionPassword(password),
    match,
  });

  const finish = (res) => {
    const reopened = downstreamKnockoutReopenedNotice(res && res.downstreamReopened);
    if (reopened) setNotice(reopened);
    setBusy(false);
    setLanded(true);
  };

  // R1: an action over unsaved entries asks first; with none it goes on in
  // the same tick, as it always did.
  // Asked only when holdsUnsaved(unsaved), so nothing else awaits.
  const unsavedDeclined = async (question) => {
    if (askingRef.current) return true;
    askingRef.current = true;
    try {
      return !(await confirmDiscardUnsaved(unsaved, question, onDiscard)) || !mountedRef.current;
    } finally {
      askingRef.current = false;
    }
  };

  const reopen = async (reason = "", question = REOPEN_ANYWAY) => {
    if (busy || landed) return;
    if (holdsUnsaved(unsaved) && await unsavedDeclined(question)) return;
    setErr("");
    setConflict(null);
    setNotice("");
    confirmedRef.current = false;
    setBusy(true);
    // The editor's own reopen (useOwnStatus): a draft it holds is not kept as
    // if another device had reopened the match.
    const token = own ? own.begin(OWN_REOPEN) : 0;
    // A refusal the outbox brings back later puts the panel back as it was.
    const refused = (e) => {
      if (own) own.answer(token, OWN_REFUSED);
      if (!mountedRef.current) return false;
      setLanded(false);
      applyFailure(e, reason);
      setBusy(false);
      return true;
    };
    try {
      // A reopen the downstream confirm sends again is settled by that write.
      const forced = (f) => { if (own) own.settle(token, f, { rejections: false }); };
      const res = await run((pw, opts) => window.API.reopenMatch(match.compId, match.id, pw, opts), reason, false, refused, forced);
      // Its answer, as it comes; a refusal is `refused`'s (a downstream
      // confirm may still send it again).
      if (own) own.settle(token, res, { rejections: false });
      if (!mountedRef.current) return;
      finish(res);
    } catch (e) {
      if (own) own.answer(token, OWN_REFUSED);
      if (!mountedRef.current) return;
      applyFailure(e, reason);
      setBusy(false);
    }
  };

  const runConflictAction = async (call, ownKind, question, forceCourt = false) => {
    const c = conflict;
    if (!c || busy || landed) return;
    if (holdsUnsaved(unsaved) && await unsavedDeclined(question)) return;
    setErr("");
    setBusy(true);
    const token = own ? own.begin(ownKind) : 0;
    const refused = (e) => {
      if (own) own.answer(token, OWN_REFUSED);
      if (!mountedRef.current) return false;
      setLanded(false);
      applyFailure(e, c.reason);
      setBusy(false);
      return true;
    };
    try {
      const forced = (f) => { if (own) own.settle(token, f, { rejections: false }); };
      const res = await run(
        (pw, opts) => call(pw, opts, c),
        c.reason,
        c.force,
        refused,
        forced,
        forceCourt,
      );
      if (own) own.settle(token, res, { rejections: false });
      if (!mountedRef.current) return;
      setConflict(null);
      finish(res);
    } catch (e) {
      if (own) own.answer(token, OWN_REFUSED);
      if (!mountedRef.current) return;
      applyFailure(e, c.reason);
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  // The remedy: send the blocking match back to the queue and reopen this one,
  // in ONE server call under one court lock (mp-gmcg review A4). The blocking
  // match keeps its score in the queue (bc-sbq), and the panel says so before
  // this can be tapped, because it takes that match off the court.
  const requeueBlocker = async () => runConflictAction(
    (pw, opts, c) => window.API.requeueBlockerAndReopen(match.compId, match.id, c.compId, c.matchId, pw,
      { ...opts, match, blockerLabel: c.label }),
    OWN_REQUEUE,
    QUEUE_AND_REOPEN_ANYWAY,
    false,
  );

  // bc-adna: reopen anyway with forceCourt when court is busy on admin hosts
  const reopenAnyway = async () => runConflictAction(
    (pw, opts) => window.API.reopenMatch(match.compId, match.id, pw, opts),
    OWN_REOPEN,
    REOPEN_ANYWAY,
    true,
  );

  useEffectA(() => {
    setBlockerLabel("");
    if (!conflict?.matchId || !conflict?.compId) return;
    if (typeof window.compMatchesForCompetition !== "function" || typeof window.API?.fetchCompetitionDetails !== "function") return;
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(conflict.compId);
        if (cancelled || !mountedRef.current) return;
        // compMatchesForCompetition (viewer_utils.jsx) owns the recombination
        // of the details payload's config with its matches (mp-dej2).
        const hit = (window.compMatchesForCompetition
          ? window.compMatchesForCompetition(detail.config || detail, detail)
          : []).find(x => x.id === conflict.matchId);
        if (!hit) return;
        const shiro = hit.sideB?.name || hit.sideB || "";
        const aka = hit.sideA?.name || hit.sideA || "";
        // bc-rawm: a POOL match has no matchNumber (bracket matches alone are
        // numbered), so it fell through to the bare "Shiro vs Aka" -- correct
        // but silent about which pool, unlike every bracket blocker, which
        // names its round via matchNumber's "Match N". window.poolLabel is
        // the one owner of that pool-name translation (mp-dej2), so a pool
        // blocker now leads with it exactly as a bracket blocker leads with
        // its number.
        const lead = hit.phase === "pool" && typeof window.poolLabel === "function"
          ? window.poolLabel(hit)
          : (hit.matchNumber ? `Match ${hit.matchNumber}` : "");
        const label = [
          lead,
          shiro && aka ? `${shiro} vs ${aka}` : "",
        ].filter(Boolean).join(" · ");
        if (label) setBlockerLabel(label);
      } catch { /* best effort: the panel falls back to the server's own label, or "another match" */ }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conflict?.compId, conflict?.matchId]);

  return { busy, landed, err, conflict, blockerLabel, notice, reopen, reopenAnyway, requeueBlocker, dismissConflict: () => setConflict(null), allowCourtForce };
}

// useWithdrawalRemoval: Remove withdrawal (operator ruling 2026-10-03, "the
// fix must leave the match resolved"), the one state machine both editors run
// for it. The operator takes the recorded ruling off in the editor, enters
// the result as it was fought, and Save correction sends it with
// clearWithdrawal, which replaces the ruling on the server
// (engine.KeepsWithdrawalRuling). Nothing is sent before that save.
//
// `enabled` is the editor's one statement of whether it can offer it at all
// (the team editor passes !isKachinuki: a finished kachinuki encounter has no
// Save correction). onRemove/onUndo are the editor's own side effects (what
// its board does when the ruling goes or comes back); the hook flips the
// state around them.
//
// ONE reset rule: a removal belongs to the match and the ruling it was made
// against, so a change of the match id, the decision or the side it names
// ends a pending removal, and the ruling shows again. Keyed on those three
// values, never on the match object, which SSE re-creates on every broadcast.
// What the board does then is onReset, which defaults to onUndo: the
// individual board must re-seed, or the winner's side would lock again over
// the operator's letters. The team editor keeps its bout edits (an edit in
// progress survives a verdict adopted from another device) and only clears
// its refusal notice.
//
// Returns:
//   removing    - a removal is pending: the editor obeys the bouts, not the ruling
//   rulingShown - a recorded withdrawal is in force and is still shown
//   removal     - RecordedWithdrawal's `removal` prop, null when !enabled
//   patchBlock  - spread into every completed write
// `held` is the editor's pendingWrite: the correction carrying the removal
// was saved on this device and is waiting to be sent. Undo cannot recall
// it, so the removal then reads as sent and offers no Undo.
function useWithdrawalRemoval({ match, enabled, held = false, onRemove, onUndo, onReset = onUndo }) {
  const [removed, setRemoved] = useStateA(false);
  const inForce = withdrawalInForce(match);
  const removing = !!enabled && inForce && removed;
  const rulingShown = inForce && !removing;
  useEffectA(() => {
    if (!removed) return;
    setRemoved(false);
    if (onReset) onReset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [match.id, match.decision, match.decisionBy]);
  const removal = enabled ? {
    removed: removing,
    held: removing && !!held,
    onRemove: () => { setRemoved(true); if (onRemove) onRemove(); },
    onUndo: () => { setRemoved(false); if (onUndo) onUndo(); },
  } : null;
  const patchBlock = removing ? { clearWithdrawal: true } : {};
  return { removing, rulingShown, removal, patchBlock };
}

// ReopenFeedback: what a reopen made through useMatchReopen came back with,
// rendered ONCE per editor, outside the control that started the reopen
// (testIdPrefix names the door). It must outlive that control: Clear
// withdrawal and reopen (RecordedWithdrawal) unmounts the moment the match is
// running again, which is exactly when its `notice` has something to say, so
// a copy inside it could never be seen. The court-busy panel's
// warning is NOT optional chrome: the remedy WIPES the blocking match's partial
// score, and the operator tapping it is looking at their own match, not that
// one, so the consequence is stated in full above the button.
function ReopenFeedback({ ctl, testIdPrefix }) {
  const c = ctl.conflict;
  // bc-adna: on the organiser's admin hosts the panel warns what a second
  // running bout means and offers Reopen anyway; the court console keeps the
  // panel as it was. The blocker is named as the heading names it (the
  // fetched label first, since it names the competitors).
  const blocker = c ? (ctl.blockerLabel || c.label || "another match") : "";
  const forceWarn = (c && ctl.allowCourtForce) ? courtBusyReopenWarning({ court: c.court, label: blocker }) : null;
  return (
    <>
      {ctl.notice && (
        <div role="status" data-testid={`${testIdPrefix}-notice`} style={{ fontSize: 12, color: "var(--ink-2)", marginBottom: 6 }}>{ctl.notice}</div>
      )}
      {ctl.err && (
        <div data-testid={`${testIdPrefix}-error`} style={{ color: "var(--danger)", fontSize: 12, marginBottom: 6 }}>{ctl.err}</div>
      )}
      {c && (
        <div className="alert alert--error reopen-conflict" data-testid={`${testIdPrefix}-conflict`}>
          {/* bc-cse: the best-effort FETCHED blockerLabel wins over the
              server's own `label` -- the server always sends one, but it is
              "Pool A · Match 2" with no names, never the "Match 2 · Shiro vs
              Aka" this panel is about to wipe the score of; that safety
              property only exists in the fetched label. Only when the fetch
              cannot resolve it (still loading, or a DIFFERENT match has since
              taken the court) does the server's label stand in, and only
              "another match" when neither is available. Reuses
              courtBusyMessage (write_result.jsx) rather than repeating its
              sentence here. The raw c.message line that used to sit below
              this heading is gone -- it named the internal id and told the
              operator to "finish that match before reopening", the opposite
              of the remedy this panel's own button offers. */}
          <div className="reopen-conflict__head">
            {forceWarn ? forceWarn.heading : courtBusyMessage({ court: c.court, label: blocker })}
          </div>
          {forceWarn && (
            <div className="reopen-conflict__warn" data-testid={`${testIdPrefix}-conflict-force-warning`}>
              {forceWarn.body}
            </div>
          )}
          <div className="reopen-conflict__warn" data-testid={`${testIdPrefix}-conflict-warning`}>
            Sending it back to the queue keeps any score already entered for it: it carries on
            from there when it is started again.
          </div>
          <div className="reopen-conflict__actions">
            {ctl.allowCourtForce && (
              <button
                type="button"
                className="btn btn--sm btn--danger"
                data-testid={`${testIdPrefix}-reopen-anyway-button`}
                onClick={ctl.reopenAnyway}
                disabled={ctl.busy}
              >
                Reopen anyway
              </button>
            )}
            <button
              type="button"
              className="btn btn--sm btn--danger"
              data-testid={`${testIdPrefix}-requeue-button`}
              onClick={ctl.requeueBlocker}
              disabled={ctl.busy}
              title="Returns that match to the queue with its score, then reopens this one"
            >
              {ctl.busy ? "Working…" : "Queue it and reopen"}
            </button>
            <button
              type="button"
              className="btn btn--sm btn--ghost"
              data-testid={`${testIdPrefix}-conflict-dismiss`}
              onClick={ctl.dismissConflict}
              disabled={ctl.busy}
            >
              Leave it running
            </button>
          </div>
        </div>
      )}
    </>
  );
}

// How long RecordedWithdrawal holds its one-tap clear for the later matches
// that decide its copy, at most (see `settled` there).
export const LATER_MATCHES_HOLD_MS = 2000;

// RecordedWithdrawal: what a correction of a match a kiken, fusenpai or
// fusensho decided shows, and the two fixes it offers, identical in the
// individual and team editors (operator ruling 2026-09-24: "Everything should
// be able to be fixed, in case of a wrong entry").
//
// "Clear <decision> and reopen" is a REOPEN, not a score write: the match
// goes back to running with what was fought kept and the withdrawn side
// eligible again, and the operator scores the rest and finishes it normally
// (engine.ReopenMatch). It is ONE TAP WITH NO REASON (operator ruling
// 2026-09-25: a match can be reopened without any reason, and nothing is
// gated on that); the consequence is spelled out beside the button rather
// than behind a confirm step. On a single bout (singleBout: the individual
// editor, which also scores a team's -DH-/-TB- rep bout) that consequence
// names the one thing the reopen cannot keep: the winner's points, which
// recording the decision had already replaced with its own circles.
//
// "Remove <decision>" is the other fix (useWithdrawalRemoval; operator ruling
// 2026-10-03, "the fix must leave the match resolved"): the match stays
// finished, the operator enters the result as it was fought, and Save
// correction sends it with clearWithdrawal so it replaces the ruling
// (engine.KeepsWithdrawalRuling). Save correction asks for a reason there;
// the reopen above does not. The removed state lives in useWithdrawalRemoval,
// which the EDITOR runs and reads to unlock the board, and which hands this
// component the `removal` prop ({ removed, onRemove, onUndo }) as it is;
// this component only offers the switch and says what it does. Without
// `removal` (kachinuki, which has no Save correction) only the reopen is
// offered.
//
// A kachinuki encounter a kiken, fusenpai or fusensho decided renders this
// too, in place of its plain one-tap Reopen, so such a decision has one
// control pair and one set of consequence text in every editor; the plain
// Reopen stays for every other kachinuki result. Switching the withdrawal to
// the other side stays with the editor's own withdrawal controls. What the
// reopen came back with (the notice, an error, the court-busy remedy) is
// rendered by the editor through ReopenFeedback, never here: this unmounts
// as soon as the match is running.
//
// A POOL match of a pools-then-knockout competition adds one line: finishing
// the reopened match may change who qualifies from its pool, and the save
// that finishes it shows which knockout matches that affects before anything
// is saved (the server's qualifierChange refusal, confirmed through
// attemptScoreWrite like any other).
//
// bc-rawm: widened past kiken and fusenpai to a fusensho recorded at match
// level (withdrawalInForce above), for a match closed because the competitor
// was already ineligible from an earlier withdrawal. Reads and the reopen
// remedy are otherwise identical; only the copy differs: the Recorded line
// names the winner, and the clear button drops "and reopen" ("Clear
// fusensho", or "Clear kiken"/"Clear fusenpai" for a competitor barred by
// another match instead), because such a match may go back to the queue
// rather than onto the court.
function RecordedWithdrawal({ match, ctl, disabled = false, singleBout = false, removal = null }) {
  const withdrawnKey = withdrawnKeyOf(match);
  const withdrawn = withdrawnSideOf(match);
  const who = withdrawn?.name || "";
  const winner = withdrawnKey === "a" ? match.sideB : withdrawnKey === "b" ? match.sideA : null;
  const winnerName = winner?.name || "";
  const isDefaultWin = match.decision === "fusensho";
  const what = match.decision === "fusenpai" ? "did not appear" : "withdrew";
  const feedsKnockout = match.phase === "pool" && match.compFormat === "mixed";
  // bc-cse: the operator's word for what IS recorded on this match --
  // "kiken", "fusenpai", or "fusensho" -- never "withdrawal"/"default win"
  // as two names for the same thing (operator ruling 2026-10-04: "default
  // win" does not exist in kendo). Always resolves: RecordedWithdrawal
  // renders only once withdrawalInForce(match) is true, which gates on
  // exactly this decision class.
  const decisionNoun = decisionWord(match.decision) || "withdrawal";

  // bc-cse: "Everything should be able to be fixed... the operator just needs
  // to be aware of the consequences" (operator ruling 2026-09-24) extends past
  // THIS match: if the withdrawn competitor has LATER matches a match-level
  // default win, recorded from the match's own notice (BarredMatchNotice /
  // the queue row's Record default win), already closed (fusensho, naming
  // this competitor's side as the one that withdrew), clearing THIS
  // withdrawal does not touch those -- they keep their own recorded result
  // and must be reopened separately to be fought. Fetched the same way
  // (fetch + compMatchesForCompetition), when the recorded withdrawal is shown: the
  // clear is one tap (operator ruling 2026-09-25), so the consequences are
  // stated beside the button, before it is tapped, rather than in a confirm
  // step. Best-effort: a fetch failure just omits the list rather than
  // blocking the reopen the operator came here to do.
  //
  // Where the withdrawn competitor's status stands now (eligible again, the
  // match that bars them, whether they can be reinstated) comes with the
  // match: the server stamps it on every completed match a withdrawal or
  // default win decided (withdrawnStatus, mobileapp.annotateEligibility). It
  // matters for EVERY recorded withdrawal, not only a fusensho (bc-kfup): a
  // fusenpai recorded against a competitor another match already barred
  // chains onto that bar and records no status of its own, and a kiken whose
  // competitor was reinstated and then withdrew again elsewhere no longer
  // names this match either. In both, clearing this match restores nobody and
  // the server returns it to the queue while the other bar holds
  // (engine.reopenTargetStatus), exactly as for a fusensho; the status record
  // is how the editor tells them from an ordinary withdrawal, whose record
  // names THIS match. The server stamps every completed withdrawal or default
  // win whose withdrawn side has a status record, a finished competition
  // included, so no stamp means no record: nobody bars them, and the plain
  // copy (the match reopens running) is the true one. The pushes of the writes
  // that record one carry it too (stampWithdrawnStatus), so a row taken from a
  // push reads the same as its refetch.
  const withdrawnStatus = match.withdrawnStatus || null;
  //
  // The later list decides the copy beside the one-tap clear too (a chained
  // fusenpai in it moves the bar, see barMovesOn), so `settled` holds the
  // clear until it is in and the copy is never the wrong one.
  const [laterDefaultWins, setLaterDefaultWins] = useStateA(null);
  const [settled, setSettled] = useStateA(false);
  useEffectA(() => {
    setLaterDefaultWins(null);
    if (!withdrawn || !match.compId) { setSettled(true); return; }
    let cancelled = false;
    setSettled(false);
    // The hold is brief by design: the fetch is best-effort and carries no
    // timeout, and a request hanging on venue wifi must not keep the one-tap
    // correction disabled. After the cap the tap is available with the copy
    // an unknown answer gives (the ordinary one), exactly as a failed fetch.
    const cap = setTimeout(() => { if (!cancelled) setSettled(true); }, LATER_MATCHES_HOLD_MS);
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(match.compId);
        if (cancelled) return;
        const all = window.compMatchesForCompetition
          ? window.compMatchesForCompetition(detail.config || detail, detail)
          : [];
        // fusenpai too (bc-kfup): a fusenpai recorded on a competitor's
        // remaining match after they withdrew chains onto this bar, the
        // same default win for the opponent a fusensho records.
        const hits = all.filter(x => (
          x.id !== match.id &&
          x.status === "completed" &&
          (x.decision === "fusensho" || x.decision === "fusenpai") &&
          ((x.decisionBy === "aka" && sameCompetitor(x.sideA, withdrawn)) ||
            (x.decisionBy === "shiro" && sameCompetitor(x.sideB, withdrawn)))
        ));
        if (!cancelled) setLaterDefaultWins(hits);
      } catch (_e) {
        if (!cancelled) setLaterDefaultWins([]);
      } finally {
        if (!cancelled) setSettled(true);
      }
    })();
    return () => { cancelled = true; clearTimeout(cap); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [match.compId, match.id, withdrawn?.id, withdrawn?.name]);
  // The barred competitor's record names a DIFFERENT match (see above).
  const barredElsewhere = !!(withdrawnStatus && withdrawnStatus.eligible === false &&
    withdrawnStatus.matchId && withdrawnStatus.matchId !== match.id);
  const clearsDefaultWin = isDefaultWin || barredElsewhere;
  const canReinstate = !!(withdrawnStatus && withdrawnStatus.eligible === false && withdrawnStatus.reinstateable);
  // bc-cse: the barred competitor's own status record now reads eligible --
  // reinstated, or their earlier withdrawal cleared elsewhere -- so the
  // server reopens THIS match straight to running rather than the queue
  // (see the fusensho branch below).
  const isEligibleAgain = !!(withdrawnStatus && withdrawnStatus.eligible === true);
  // bc-kfup: the bar THIS match recorded moves on instead of lifting when a
  // fusenpai chained onto it is still on record (engine.standingWithdrawalOf):
  // the competitor stays withdrawn because of that match, so the server
  // returns this one to the queue too (engine.reopenTargetStatusTx).
  const barMovesOn = !clearsDefaultWin && !!laterDefaultWins
    && laterDefaultWins.some(x => x.decision === "fusenpai");
  const removed = !!(removal && removal.removed);
  // Where the withdrawn competitor stands once this ruling is gone, the ONE
  // answer both fixes describe (the reopen copy and the removal sentence):
  //   staysLater     - the bar moves onto a later no-show (barMovesOn);
  //   staysElsewhere - another match bars them (barredElsewhere), or this is
  //                    a match-level fusensho, which never recorded a bar of
  //                    its own, and the server does not read them eligible;
  //   restored       - they can compete again: this match's record was the
  //                    bar, or the server already reads them eligible.
  let eligibility = "restored";
  if (barMovesOn) eligibility = "staysLater";
  else if (clearsDefaultWin && !isEligibleAgain) eligibility = "staysElsewhere";
  const withdrawnWho = who || "The withdrawn competitor";
  const removeEligibility = {
    staysLater: ` ${withdrawnWho} stays withdrawn because of the later match listed below.`,
    staysElsewhere: ` ${withdrawnWho} stays withdrawn because of another match.`,
    restored: ` ${withdrawnWho} can compete again.`,
  }[eligibility];

  return (
    <div className="decision-recorded" data-testid="recorded-withdrawal" style={{ marginTop: 10, fontSize: 13 }}>
      <div>
        {/* bc-rawm: a match-level fusensho (recorded from the match's own
            notice: BarredMatchNotice / the queue row's Record fusensho for
            <opponent>) names the WINNER first -- "Fusensho for <winner>" --
            so the operator sees who benefits from the same reading the wire's
            decisionBy already carries, then names who had withdrawn as a
            second sentence. withdrawalLabel cannot build this on its own: it
            takes only the decision string, not the winner's name. */}
        <span>
          {isDefaultWin
            ? `Recorded: Fusensho for ${winnerName || "the opponent"}. ${who || "The withdrawn competitor"} had withdrawn.`
            : `Recorded: ${withdrawalLabel(match.decision)}${who ? `, ${who} ${what}` : ""}.`}
        </span>
        {" "}
        {/* One tap, no reason (operator ruling 2026-09-25: a match can be
            reopened without any reason, and nothing is gated on that). What
            the clear does is stated below, before it is tapped. The one
            second step left is useMatchReopen's own, when a later knockout
            match has already been fought ("Reopen both"). */}
        {removed && removal.held ? (
          // The correction is saved on this device and waiting to be sent: it
          // will remove the ruling when it lands, and Undo could not recall it.
          <span data-testid="remove-withdrawal-pending">
            The correction is saved on this device and removes the {decisionNoun} when it is sent.
          </span>
        ) : removed ? (
          // Removed in the editor, not yet saved: nothing has been sent, so
          // Undo just puts the recorded result back on the board.
          <>
            <span data-testid="remove-withdrawal-pending">
              {`The ${decisionNoun} will be removed when you save the correction.`}
            </span>
            {" "}
            <button
              type="button"
              className="btn btn--sm"
              data-testid="remove-withdrawal-undo"
              onClick={removal.onUndo}
              disabled={disabled}
            >
              Undo
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              className="btn btn--sm"
              data-testid="clear-withdrawal-reopen"
              onClick={() => ctl.reopen("", !(clearsDefaultWin || barMovesOn) ? REOPEN_ANYWAY : `Clear the ${decisionNoun} anyway?`)}
              disabled={disabled || ctl.busy || ctl.landed || !settled}
            >
              {/* bc-cse: "Clear <decision>" -- no "and reopen" -- because a
                  fusensho reopen no longer always lands running: the server
                  returns the match to SCHEDULED when the barred competitor
                  is still barred (BarredMatchNotice shows again), and only
                  to running once they no longer are, so this button cannot
                  promise "and reopen" for either outcome uniformly. */}
              {ctl.busy ? "Reopening…" : ctl.landed ? "Reopened" : !(clearsDefaultWin || barMovesOn) ? `Clear ${decisionNoun} and reopen`
                : `Clear ${decisionNoun}`}
            </button>
            {removal && (
              <>
                {" "}
                {/* No reason here: Save correction asks for one, and
                    nothing is sent until then. */}
                <button
                  type="button"
                  className="btn btn--sm"
                  data-testid="remove-withdrawal"
                  onClick={removal.onRemove}
                  // Held until the later-matches lookup settles, like the
                  // clear beside it: the sentence a removal shows (who stays
                  // withdrawn, barMovesOn) depends on it.
                  disabled={disabled || ctl.busy || ctl.landed || !settled}
                >
                  {`Remove ${decisionNoun}`}
                </button>
              </>
            )}
          </>
        )}
      </div>
      {!removed && (<>
          {clearsDefaultWin ? (
            // bc-cse: a fusensho names the OTHER competitor as barred, not
            // this match's own withdrawal, so clearing it does not simply
            // "reopen to running" -- the barred competitor is almost always
            // still barred (whatever withdrew them elsewhere is still on
            // record), so that is the default the copy states; only when a
            // live check finds them reinstateable does it also offer that
            // route. When the live check instead finds them ELIGIBLE
            // AGAIN (reinstated, or their earlier withdrawal cleared), the
            // server reopens this match straight to running, so the copy
            // says that instead of promising the queue.
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              {eligibility === "restored"
                ? `${who || "The barred competitor"} can fight again, so the match reopens in progress. Score it and finish it as usual.`
                : <>
                  {/* bc-cse: "again" is only true when THIS match's cleared
                      decision was itself a fusensho (isDefaultWin): clearing
                      it undoes exactly one fusensho, so recording one to
                      re-close the (now queued) match is a repeat. A chained
                      kiken/fusenpai (barredElsewhere, isDefaultWin false)
                      never recorded a fusensho on this match at all -- it is
                      the OTHER match that barred this competitor -- so this
                      is the first fusensho for THIS match's winner, named by
                      winnerName exactly as the Recorded line above names it. */}
                  The match goes back to the queue. {who || "The barred competitor"} is still withdrawn, so
                  record {isDefaultWin ? "the fusensho again" : `a fusensho for ${winnerName || "the opponent"}`}
                  {canReinstate ? `, or reinstate ${who || "them"} first to fight it` : ""}.
                </>}
            </p>
          ) : eligibility === "staysLater" ? (
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              The match goes back to the queue. {who || "The withdrawn competitor"} also did not
              appear for a later match, listed below, so they are still withdrawn because of it.
            </p>
          ) : singleBout && match.decision === "fusenpai" ? (
            // A no-show fought nothing: there are no points to keep or lose,
            // so the reopen only removes the win the fusenpai gave.
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running, the win the fusenpai gave
              to {winnerName || "the other side"} is removed, and {who || "the side marked absent"} can
              compete again. Then score the match and finish it.
            </p>
          ) : singleBout ? (
            // A single bout (an individual match, or a team -DH-/-TB- rep
            // bout) loses the WINNER's points on a reopen: recording the
            // decision replaced them with its own award (recordDecisionTx)
            // and the reopen drops that verdict, so only what the withdrawing
            // side struck is still there to keep (engine singleBoutFightOf).
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running and {who || "the withdrawn side"} can
              compete again. {winnerName || "The winner"}&apos;s points were replaced by the {decisionNoun}
              {" "}when the withdrawal was recorded, so enter them again; {who ? `${who}'s` : "the withdrawn side's"} points
              are kept. Then score the rest and finish it.
            </p>
          ) : (
            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
              This reopens the match: it goes back to running with what was fought kept,{" "}
              {who || "the withdrawn side"} can compete again, and you score the rest and finish it.
            </p>
          )}
      </>)}
      {removal && (
        // The one-save fix beside the reopen (operator ruling 2026-10-03):
        // the match never leaves the finished state and never takes the
        // court. Offered, it follows the reopen copy; pending, it stands in
        // for it. The eligibility tail is the same answer either way.
        <p data-testid="remove-withdrawal-consequence" style={{ margin: "6px 0 0" }}>
          {removed
            ? "Enter the result as it was fought, then save the correction. The match stays finished."
            : "Or remove it: the match stays finished, you enter the result as it was fought and save the correction."}
          {removeEligibility}
        </p>
      )}
          {feedsKnockout && (
            // A pool match of a pools-then-knockout competition feeds the
            // knockout through its standings, so the result it is finished
            // with may seat someone else there. The reopen itself moves
            // nobody; the finishing save is what is checked, and it names
            // any knockout match already fought before anything is saved.
            <p data-testid="clear-withdrawal-qualifier-note" style={{ margin: "6px 0 0" }}>
              Finishing it may change who qualifies from {match.poolName || "its pool"}. If that moves
              someone who has already fought in the knockout, you will be shown which knockout
              matches it affects before anything is saved.
            </p>
          )}
          {laterDefaultWins && laterDefaultWins.length > 0 && (
            <div data-testid="clear-withdrawal-later-matches" style={{ margin: "6px 0 0" }}>
              {/* bc-cse: named by the match's row label first -- a pairing
                  alone cannot be found in the scores list, which is where the
                  operator has to go to reopen it -- with the pairing appended
                  ("Pool A · Match 2 · Shiro vs Aka"). scoreRowMatchName owns
                  that composition, shared with the court console's refusal
                  notices (bc-crpn). */}
              {laterDefaultWins.map((x) => {
                const noun = decisionWord(x.decision) || "decision";
                return (
                  <p key={x.id} data-testid={`clear-withdrawal-later-match-${x.id}`} style={{ margin: "4px 0 0" }}>
                    {scoreRowMatchName(x)} keeps its {noun}; reopen it to fight it.
                  </p>
                );
              })}
            </div>
          )}
    </div>
  );
}

// WithdrawalMarkedName: a side's name in an editor header, carrying the RESULT
// mark (Kiken / Fus.) of a recorded withdrawal beside the competitor it names
// while that withdrawal is in force, so a correction shows the recorded
// decision before any edit (bc-kcsh). WHICH mark is sideMarks (bracket.jsx),
// the owner the score strings and the bracket card already read
// ("M Kiken vs ○○"); WHICH side is withdrawnKeyOf, the answer the Recorded line
// under the board gives, so the two cannot disagree. The mark sits on the
// INNER side of the name, across it from the number (numberFollowsName), and
// never in the centre: the middle is a closed set, and Kiken/Fus. name one
// competitor. Both editors render their header names through here, the
// individual board and the team encounter header alike. `rulingShown` is the
// editor's own answer to "is the recorded ruling on show" (useWithdrawalRemoval:
// false during a pending Remove withdrawal), and the mark appears only when it
// is true, so a surface that forgets to pass it shows no mark rather than a
// mark the board no longer agrees with.
function WithdrawalMarkedName({ match, sideKey, side, name, number, rulingShown = false }) {
  // bc-cse: take BOTH marks once and place the right one on the right name,
  // rather than always reading .loser -- for a match-level fusensho the
  // withdrawal names the BARRED (losing) side, and sideMarks' fusensho arm
  // puts its "Fus." on .winner, not .loser, so reading .loser alone showed
  // NO mark at all on either header while the bracket, list rows, TV
  // headline and export all marked the winner "Fus.".
  const inForce = rulingShown === true && withdrawalInForce(match);
  const withdrawnKey = inForce ? withdrawnKeyOf(match) : "";
  let mark = "";
  if (inForce && withdrawnKey) {
    const marks = sideMarks(match.decision, false);
    mark = sideKey === withdrawnKey ? marks.loser : marks.winner;
  }
  if (!mark) return <NumberedName side={side} name={name} number={number} />;
  const markEl = <span className="sb-result-mark" data-testid={`withdrawal-mark-${side}`}>{mark}</span>;
  return numberFollowsName(side)
    ? <>{markEl}{" "}<NumberedName side={side} name={name} number={number} /></>
    : <><NumberedName side={side} name={name} number={number} />{" "}{markEl}</>;
}

// A match has ONE result and every surface asking for it shows the same one,
// and the score editors are such surfaces: while one is open, the viewer card,
// the bracket, the TV board, the lobby and the export are all already showing
// whatever the server holds, so an editor showing something else is a
// divergence whatever its reason. Every editor field is local state seeded at
// MOUNT, so following the server takes a deliberate effect per channel — and
// each channel written by hand is a channel a later reader has to NOTICE is
// missing. Three were written by hand, in three shapes, and the team editor's
// numbered bouts were the one nobody noticed (bc-tsub). This is that rule as
// ONE primitive, so a new channel either calls it or is visibly absent.
//
// `signature` must change exactly when the SERVER's value for this channel
// moves — key on the values the effect WRITES, not the fields it reads: those
// two lists drift (the individual editor's did), and a signature built from
// the seeds cannot, since whatever the derivation starts reading is
// automatically part of the key. NEVER key on the match object: SSE re-creates
// it on every broadcast, so an object-keyed effect fights the operator's every
// tap.
//
// `keepLocalEdits` picks the policy, and the two are deliberately different:
//
//   false — adopt unconditionally. Right for a channel that is ONE indivisible
//     value (the hantei verdict, the encho count): keying on the VALUE already
//     means a local arm/pick/cancel stands, because that does not move the
//     server's value. Also right for a channel whose `apply` MERGES rather than
//     replaces — the team editor's bout board does exactly that, keeping the
//     rows the operator touched and taking the server's for the rest, which is
//     strictly better than the all-or-nothing gate below and is why that
//     channel does NOT set this flag.
//
//   true — an operator with UNSAVED work keeps ALL of it, and the server's
//     change is dropped for as long as they are dirty. Use this only where the
//     channel cannot be merged, i.e. where local and server are rival readings
//     of the SAME indivisible thing: the individual editor's scoreline is one
//     fight, so there is no per-row seam to merge along. Prefer a merging
//     `apply` wherever the channel has independent parts, because this policy
//     necessarily discards the newer reading of the parts the operator never
//     touched — and the next full-snapshot write then sends the stale ones
//     back out over it.
//
// The dirty flag read is the PREVIOUS render's, which is the subtle bit this
// hook exists to own: `isDirty` measures local state against what the server
// holds NOW, so on the very render a server change lands it reads true for an
// editor nobody touched — the exact case being corrected. Callers pass the
// current value and the hook remembers it; the tracking effect is registered
// AFTER the adopt effect, so the adopt always sees the value from the render
// BEFORE the change. It self-corrects: adopting makes the next render clean.
//
// THE FULL SET, because "visibly absent" is only true of a set a reader can
// enumerate. There are THREE editor bodies behind ONE entry point, and every
// host mounts the entry point, never a body:
//
//   ScoreEditorModal      admin_scoring_individual.jsx — the entry point AND
//                         the individual editor; dispatches the other two
//   EngiScoreEditorModal  admin_scoring_engi.jsx       — flag counts
//   TeamScoreEditorModal  admin_scoring_team.jsx       — the bout board
//
// Four hosts mount ScoreEditorModal: the shiaijo inline scorer, the admin
// schedule editor, the pools tab and the competition bracket. Which body runs
// is decided by the competition, not the host, so a channel missing from a body
// is missing on ALL FOUR surfaces — which is why the count that matters here is
// three, not four.
//
// All three call this hook. If you add a fourth body, or a new piece of local
// state to one of these three, it needs a channel or an explicit reason.
function useAdoptFromServer({ signature, apply, keepLocalEdits = false, isDirty = false }) {
  // Hold `apply` in a ref so the adopt effect can depend on `signature` ALONE:
  // `apply` is a fresh closure every render and would otherwise re-run the
  // adopt on every render and stomp the operator.
  //
  // Assigned in an effect, NOT during render. A render React starts and then
  // discards (concurrent rendering; StrictMode's double invoke) must not leave
  // the ref holding a closure over props that were never committed — the adopt
  // would then seed from a board nobody was shown. Effects run in declaration
  // order after commit, so this one always refreshes the ref BEFORE the adopt
  // below reads it, and both see the committed render.
  const applyRef = useRefA(apply);
  useEffectA(() => { applyRef.current = apply; });
  const wasDirtyRef = useRefA(false);
  useEffectA(() => {
    if (keepLocalEdits && wasDirtyRef.current) return;
    applyRef.current();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature]);
  // Only the keepLocalEdits policy reads this, but the hook call itself must be
  // unconditional (rules of hooks), so the CHEAP part is the branch inside.
  useEffectA(() => { if (keepLocalEdits) wasDirtyRef.current = !!isDirty; });
}

// ES exports: the modal file imports these and re-exports the test-facing
// subset, so `import { … } from './admin_scoring_modal.jsx'` keeps working.
export {
  MAX_IPPONS_PER_SIDE,
  sideColorName,
  isBoutDecided,
  getIpponButtons,
  getValidPointKeys,
  IpponLegend,
  ScoringShortcutHint,
  applyFusenshoToggle,
  fusenshoAllowed,
  clearFusensho,
  applyBoutScoreEdit,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  TermAS,
  GlossaryHintAS,
  resolveDecisionPassword,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  useAdoptFromServer,
  EnchoControl,
  DecisionPrompt,
  BarredMatchNotice,
  FoulCounter,
  LineupNameInput,
  ReasonPrompt,
  CORRECTION_PRESETS,
  withdrawalLabel,
  withdrawalConsequence,
  withdrawnSideOf,
  withdrawnKeyOf,
  withdrawalInForce,
  useMatchReopen,
  useWithdrawalRemoval,
  ReopenFeedback,
  RecordedWithdrawal,
  WithdrawalMarkedName,
};
