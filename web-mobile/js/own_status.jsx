// own_status.jsx: the ONE owner of "this score editor's own status writes and
// the draft it keeps" (bc-evts stage 5 review 4, rebuilt as one owner after
// three rounds of patches to scattered flags opened new edges each time).
//
// A score editor makes status writes of its own: a Start (scheduled to
// running; the kachinuki sheet's Record bout on a queued encounter is one), a
// reopen or the court-busy requeue-and-reopen (completed to
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
    // The own writes out or landed over one move, a CHAIN (stage 5 review 7):
    // {from, entries: [{token, kind, fields, landed}], sawTarget, leftTarget,
    // leftDraft}. Every own status write moves the match to running, so a
    // write begun while an earlier one from the same status is still open
    // (the team sheet's pre-save Start and its representative-bout add, a
    // Start and an add made before the feed shows it, an add and a remove)
    // claims the SAME move: the move is the editor's own while any entry has
    // landed or is still out, and only an empty chain makes it another
    // device's. A write that does not land takes only its own entry off.
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

  const anyLanded = (p) => !!(p && p.entries.some((e) => e.landed));
  const hasStart = (p) => !!(p && p.entries.some((e) => e.kind === OWN_START));
  const draftDiffers = () => !!(st.base && st.draft && !same(st.base, st.draft));
  const keepDraft = (from) => {
    if (draftDiffers()) st.kept = { base: st.base, from };
  };
  const reset = () => { st.resetSeq += 1; };
  // An own write the server took is over: the draft it carried (a Start) or
  // replaced (a reopen, a representative-bout change) goes with it, so no
  // draft noted before it can surface later, e.g. as a draft "dropped" when a
  // later own write meets a move elsewhere before the editor notes again
  // (stage 5 review 6). The whole chain is over with it: its other entries
  // claimed the same move, and their answers act on nothing.
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

  // A status change the feed shows, judged against the chain of own writes.
  const moved = (prev, status) => {
    const p = st.pending;
    // Every own status write moves the match to running.
    // A chain whose move the feed already showed and left is no longer the
    // one a new move could be (review 5).
    const ownMove = p && !p.leftTarget && p.from === prev && status === 'running';
    if (status === 'running' && (prev === 'scheduled' || prev === 'completed')) {
      if (ownMove && anyLanded(p)) {
        // The own write, as the feed now shows it.
        retireLanded();
      } else if (ownMove) {
        // The feed shows the move before any own write's answer: whose it is
        // waits for the answers (answer below).
        p.sawTarget = true;
      } else {
        keepDraft(prev);
      }
    }
    if (prev === 'running' && status !== 'running') {
      dropKept(status);
      // A move the feed showed while the own writes were unanswered, now
      // left: the answers still say whose it was (refused ones drop the draft
      // with the note there).
      if (p && p.sawTarget && !anyLanded(p)) { p.leftTarget = status; p.leftDraft = draftDiffers(); }
      else if (p && anyLanded(p)) retireLanded();
      reset();
    }
    // The feed jumped from where the own writes began to somewhere they do
    // not move the match (another device moved the match on before the feed
    // caught up): a landed write is over, and unanswered ones are left as
    // above, the draft said if the answers turn them down; either way the
    // draft takes the server's copy (review 5).
    const q = st.pending;
    if (q && prev === q.from && status !== 'running' && status !== prev && !q.leftTarget) {
      if (anyLanded(q)) retireLanded();
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
      if (p && anyLanded(p) && status === p.from) {
        // A landed own Start keeps its override over a copy older than its
        // own write (a refetch answered late): a copy stamped at or after the
        // Start's own stamp (another device sent it back) ends it; with no
        // stamp to compare it stands until the feed shows another status
        // (stage 5 review 8).
        const start = p.entries.find((x) => x.kind === OWN_START && x.landed);
        if (start && (!start.at || !at || at < start.at)) return;
        retireLanded();
        reset();
      } else if (p && !p.leftTarget && !hasStart(p) && status === p.from) {
        // A reopen or representative-bout change is out and another device
        // changed the match it was made over (a correction of the finished
        // match): the draft takes the server's copy, and is said if the
        // writes are then turned down (an own Start keeps its override until
        // its answer, which judges the restamp itself).
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

    // begin: an own status write is sent; its token. A write begun while the
    // chain from the same status is open joins it (they claim one move); one
    // begun from another status, or over a chain whose move the feed already
    // left, starts a new chain, and the old chain's answers are stale.
    begin(kind, fields) {
      st.token += 1;
      // Every own status write moves the match to running: a write begun
      // while the feed shows the match running (the team sheet's
      // representative-bout change on a running match) moves no status and is
      // none. Nothing is recorded, so a restamp or a move elsewhere while it
      // is out is judged as any foreign one, and its answer acts on nothing
      // (stage 5 review 6).
      if (st.feed.status === 'running') return st.token;
      const entry = { token: st.token, kind, fields: fields || null, landed: false };
      const p = st.pending;
      if (p && p.from === st.feed.status && !p.leftTarget) p.entries.push(entry);
      else st.pending = { from: st.feed.status, entries: [entry], sawTarget: false, leftTarget: null, leftDraft: false };
      st.refusal = '';
      st.dropped = null;
      return st.token;
    },

    // carry: what a LANDED own write's answer says the match now holds (the
    // kachinuki Record bout's bout log, with the pairing the advance
    // appended), laid on the fields its override shows until the feed shows
    // the move (stage 6 review 14, item 5: the screen shows what the operator
    // did). An entry already retired, or one the chain no longer holds, takes
    // nothing.
    carry(token, fields) {
      const p = st.pending;
      const e = p && p.entries.find((x) => x.token === token);
      if (!e || !e.landed || !fields) return;
      e.fields = { ...(e.fields || {}), ...fields };
    },

    // answer: the server's answer to own write `token`: LANDED, or REFUSED,
    // HELD or DISCARDED (`msg`, the sentence to say for a refused Start).
    answer(token, outcome, msg, at) {
      const p = st.pending;
      const e = p && p.entries.find((x) => x.token === token);
      if (!e) return;
      if (outcome === LANDED) {
        e.landed = true;
        // The stamp the server gave the write, when its answer names one.
        e.at = Number(at) || 0;
        // The feed already showed the move: it was this write.
        if (p.sawTarget || p.leftTarget) retireLanded();
        return;
      }
      // This write did not land: only its own entry goes.
      p.entries = p.entries.filter((x) => x !== e);
      const now = st.feed.status;
      // A turned-down Start says why, unless a newer Start of the chain is
      // still open (its answer is the one that counts).
      if (e.kind === OWN_START && now === 'scheduled' && msg && !p.leftTarget && !hasStart(p)) st.refusal = msg;
      // Another write of the chain landed or is still out: the move is still
      // the editor's own, and that write decides it.
      if (p.entries.length > 0) return;
      st.pending = null;
      if (p.leftTarget) {
        // Another device moved the match and moved it on again before this
        // answer: the draft the editor held was taken from the board then
        // (the reset), and is said now.
        if (p.leftDraft) st.dropped = { at: now, reset: true };
        if (e.kind === OWN_START && now === 'scheduled' && msg) st.refusal = msg;
        return;
      }
      if (now === p.from) {
        // Nothing moved: the editor shows the server's copy again, with the
        // draft the write carried, and a refused Start says why.
        return;
      }
      if (p.sawTarget && now === 'running') {
        // The move the feed showed was another device's.
        keepDraft(p.from);
      }
    },

    // saysRefusal: will a refusal of own write `token`, arriving now, be said
    // here? True for an own Start still open while the feed shows the match
    // scheduled: `answer` then states its sentence beside the board
    // (StartRefusedNotice), so the editor claims the refusal from the outbox,
    // which then raises no second alert for it. Anything else is left to the
    // outbox to tell.
    saysRefusal(token) {
      const p = st.pending;
      return !!(p && st.feed.status === 'scheduled'
        && p.entries.some((x) => x.token === token && x.kind === OWN_START));
    },

    // release: the kept draft was saved or discarded.
    release() { st.kept = null; },

    // rebaseKept: the editor laid the kept draft's own changes over the
    // server's copy (stage 5 review 9, m2), so that copy is the one Save
    // diffs against from now on.
    rebaseKept(base) { if (st.kept) st.kept = { ...st.kept, base }; },

    // discardDraft: the operator chose Discard entries before an own action
    // (R1, stage 5 review 8): the editor took the server's copy, and no draft
    // noted before is kept, said dropped, or kept later when a write is
    // turned down after a move elsewhere.
    discardDraft() {
      st.kept = null;
      st.base = null;
      st.draft = null;
      if (st.pending) st.pending.leftDraft = false;
    },

    view() {
      const p = st.pending;
      // The latest own Start shows what it carried.
      let start = null;
      if (p) for (const x of p.entries) if (x.kind === OWN_START) start = x;
      // Never over a feed that moved since the Start was sent (it showed the
      // match running, someone else's or not yet known): the feed's copy shows.
      const overriding = !!(start && st.feed.status === 'scheduled' && p.from === 'scheduled' && !p.sawTarget);
      const status = st.feed.status;
      return {
        shownStatus: overriding ? 'running' : status,
        override: overriding ? (start.fields || {}) : null,
        startOffered: status === 'scheduled' && !start,
        refusal: status === 'scheduled' ? st.refusal : '',
        kept: status === 'running' && st.kept ? st.kept : null,
        droppedShown: !!(st.dropped && status === st.dropped.at),
        resetSeq: st.resetSeq,
        // Testing aid: the latest own write still open.
        pendingKind: p && p.entries.length ? p.entries[p.entries.length - 1].kind : null,
      };
    },
  };
}
