// own_status.jsx: the ONE owner of "this score editor's own status writes and
// the draft it keeps" (bc-evts stage 5 review 4, rebuilt as one owner after
// three rounds of patches to scattered flags opened new edges each time).
//
// A score editor makes status writes of its own: a Start (scheduled to
// running), a reopen or the court-busy requeue-and-reopen (completed to
// running), and the team sheet's representative-bout change (a finished match
// shown running at once). Another device can make the same changes. What the
// editor shows depends on which made a change and how its own write was
// answered, and every piece of that is DERIVED here from two inputs only:
//
//   - the feed: the host's copy of the match (status and stamp), observed on
//     every render (`observe`), so a change is judged against the LATEST feed;
//   - the editor's own writes: each one gets a token (`begin`), and its answer
//     (`answer`, from the outbox's `settled`, a callback, never a wait) acts
//     only while that token is the current one, so a stale answer can never
//     touch a newer write.
//
// From them: the started override (an own Start shows the match running until
// the feed does, `view().override`), whether Start is offered, the refusal
// sentence of an own Start, the kept draft (operator decision D1 (a): a draft
// held when ANOTHER device moves the match to running, scheduled or completed,
// is kept with a note, Save as taps, Discard), the dropped note (a kept draft
// the match left running under elsewhere), and when the editor must take the
// server's copy into its draft (`view().resetSeq`, which grows by one each
// time: the editor resets in an effect keyed on it).
//
// A pure leaf with no imports: the hook (admin_scoring_shared.jsx,
// useOwnStatus) holds one per editor, and the permutation test drives it
// through the real editors.

export const OWN_START = 'start';
export const OWN_REOPEN = 'reopen';
export const OWN_REQUEUE = 'requeue';
export const OWN_REP = 'rep';

export const LANDED = 'landed';
export const REFUSED = 'refused';
export const HELD = 'held';
export const DISCARDED = 'discarded';

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// createOwnStatus: one editor's owner, starting from the feed's status and
// stamp.
export function createOwnStatus(status, at) {
  const st = {
    feed: { status, at },
    token: 0,
    // The current own write: {token, kind, from, fields, landed, sawTarget}.
    pending: null,
    // The refusal sentence of the last own Start, said while the match is
    // scheduled.
    refusal: '',
    // The kept draft: {base, from}.
    kept: null,
    // The dropped note: {at, reset}.
    dropped: null,
    // What the draft would tap, as typed over and as held (note).
    base: null,
    draft: null,
    resetSeq: 0,
  };

  const draftDiffers = () => !!(st.base && st.draft && !same(st.base, st.draft));
  const keepDraft = (from) => {
    if (draftDiffers()) st.kept = { base: st.base, from };
  };
  const reset = () => { st.resetSeq += 1; };
  // An own write the server took is over: the draft it carried (a Start) or
  // replaced (a reopen, a representative-bout change) goes with it, so no
  // draft noted before it can surface later, e.g. as a draft "dropped" when a
  // later own write meets a move elsewhere before the editor notes again
  // (stage 5 review 6).
  const retireLanded = () => {
    st.pending = null;
    st.base = null;
    st.draft = null;
  };
  const dropKept = (status) => {
    if (!st.kept) return;
    st.kept = null;
    st.dropped = { at: status, reset: false };
  };

  // A status change the feed shows, judged against the current own write.
  const moved = (prev, status) => {
    const p = st.pending;
    // Every own status write moves the match to running.
    // A write whose move the feed already showed and left is no longer the
    // one a new move could be (review 5).
    const ownMove = p && !p.leftTarget && p.from === prev && status === 'running';
    if (status === 'running' && (prev === 'scheduled' || prev === 'completed')) {
      if (ownMove && p.landed) {
        // The own write, as the feed now shows it.
        retireLanded();
      } else if (ownMove) {
        // The feed shows the move before the own write's answer: whose it is
        // waits for that answer (answer below).
        p.sawTarget = true;
      } else {
        keepDraft(prev);
      }
    }
    if (prev === 'running' && status !== 'running') {
      dropKept(status);
      // A move the feed showed while the own write was unanswered, now left:
      // the answer still says whose it was (a refused one drops the draft
      // with the note there).
      if (p && p.sawTarget && !p.landed) { p.leftTarget = status; p.leftDraft = draftDiffers(); }
      else if (p && p.landed) retireLanded();
      reset();
    }
    // The feed jumped from where the own write began to somewhere it does not
    // move the match (another device moved the match on before the feed
    // caught up): a landed write is over, and one still unanswered is left
    // as above, its draft said if the answer turns it down; either way the
    // draft takes the server's copy (review 5).
    const q = st.pending;
    if (q && prev === q.from && status !== 'running' && status !== prev && !q.leftTarget) {
      if (q.landed) retireLanded();
      else { q.leftTarget = status; q.leftDraft = draftDiffers(); }
      reset();
    }
    if (status !== 'scheduled') st.refusal = '';
    if (st.dropped && status !== st.dropped.at) st.dropped = null;
  };

  return {
    // observe: the feed's copy of the match on this render.
    observe(status, at) {
      const prev = st.feed.status;
      if (status === prev && at === st.feed.at) return;
      st.feed = { status, at };
      if (status !== prev) { moved(prev, status); return; }
      // The same status at a new stamp. An own write the server took that the
      // feed never showed running (the match was sent back, or finished, by
      // another device before the feed caught up) is over: retired here, so a
      // later move by another device is never read as this one (review 5),
      // and the draft takes the server's copy.
      const p = st.pending;
      if (p && p.landed && status === p.from) {
        retireLanded();
        reset();
      } else if (p && !p.landed && !p.leftTarget && p.kind !== OWN_START && status === p.from) {
        // A reopen or representative-bout change is out and another device
        // changed the match it was made over (a correction of the finished
        // match): the draft takes the server's copy, and is said if the write
        // is then turned down (an own Start keeps its override until its
        // answer, which judges the restamp itself).
        p.leftTarget = status;
        p.leftDraft = draftDiffers();
        reset();
      }
    },

    // note: what the editor's draft would tap, as typed over (`base`) and as
    // it holds it (`draft`), recorded while the editor shows a draft.
    note(base, draft) {
      if (this.view().shownStatus === 'running' || st.kept) return;
      st.base = base;
      st.draft = draft;
      if (!st.dropped) return;
      // The editor's reset after a drop brings the draft to the server's copy;
      // an edit after that is a new draft, and the dropped note goes.
      if (same(base, draft)) st.dropped.reset = true;
      else if (st.dropped.reset) st.dropped = null;
    },

    // begin: an own status write is sent; its token. A write begun while an
    // earlier one is unanswered replaces it: that one's answer is stale.
    begin(kind, fields) {
      st.token += 1;
      // Every own status write moves the match to running: a write begun
      // while the feed shows the match running (the team sheet's
      // representative-bout change on a running match) moves no status and is
      // none. No pending write is recorded, so a restamp or a move elsewhere
      // while it is out is judged as any foreign one, and its answer acts on
      // nothing (stage 5 review 6).
      if (st.feed.status === 'running') return st.token;
      st.pending = { token: st.token, kind, from: st.feed.status, fields: fields || null, landed: false, sawTarget: false, leftTarget: null, leftDraft: false };
      st.refusal = '';
      st.dropped = null;
      return st.token;
    },

    // answer: the server's answer to own write `token`: LANDED, or REFUSED,
    // HELD or DISCARDED (`msg`, the sentence to say for a refused Start).
    answer(token, outcome, msg) {
      const p = st.pending;
      if (!p || p.token !== token) return;
      if (outcome === LANDED) {
        p.landed = true;
        // The feed already showed the move: it was this write.
        if (p.sawTarget || p.leftTarget) retireLanded();
        return;
      }
      st.pending = null;
      const now = st.feed.status;
      if (p.leftTarget) {
        // Another device moved the match and moved it on again before this
        // answer: the draft the editor held was taken from the board then
        // (the reset), and is said now.
        if (p.leftDraft) st.dropped = { at: now, reset: true };
        if (p.kind === OWN_START && now === 'scheduled' && msg) st.refusal = msg;
        return;
      }
      if (now === p.from) {
        // Nothing moved: the editor shows the server's copy again, with the
        // draft the write carried, and a refused Start says why.
        if (p.kind === OWN_START && now === 'scheduled' && msg) st.refusal = msg;
        return;
      }
      if (p.sawTarget && now === 'running') {
        // The move the feed showed was another device's.
        keepDraft(p.from);
      }
    },

    // release: the kept draft was saved or discarded.
    release() { st.kept = null; },

    view() {
      const p = st.pending;
      // Never over a feed that moved since the Start was sent (it showed the
    // match running, someone else's or not yet known): the feed's copy shows.
    const overriding = !!(p && p.kind === OWN_START && st.feed.status === 'scheduled' && p.from === 'scheduled' && !p.sawTarget);
      const status = st.feed.status;
      return {
        shownStatus: overriding ? 'running' : status,
        override: overriding ? (p.fields || {}) : null,
        startOffered: status === 'scheduled' && !(p && p.kind === OWN_START),
        refusal: status === 'scheduled' ? st.refusal : '',
        kept: status === 'running' && st.kept ? st.kept : null,
        droppedShown: !!(st.dropped && status === st.dropped.at),
        resetSeq: st.resetSeq,
        // Testing aid: an own write is out.
        pendingKind: p ? p.kind : null,
      };
    },
  };
}
