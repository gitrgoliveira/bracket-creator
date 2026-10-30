// Changed-group and kept-in-history infra for the score editor modals.
// Used by both ScoreEditorModal (individual) and TeamScoreEditorModal (team).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

import { toBackendMatchResult, matchWire } from './api_serializers.jsx';
import { changedGroups, statedGroupsOf, groupMatches, keptInHistoryNote, GROUP_RESULT } from './match_groups.jsx';
import { writeDidNotLand, writePending, writeWasRefused } from './write_result.jsx';

const { useState: useStateA, useEffect: useEffectA } = React;

// ---------------------------------------------------------------------------
// useChangedGroups (bc-mrgc): every score write an editor builds names the
// groups it changes (match_groups.jsx), so the server applies only those and
// keeps every other group as stored. Called once per editor with the match it
// renders from; returns claim(patch), which every patch the editor hands to
// its host goes through, autosave included, and which returns the patch with
// `changed` set.
//
// A group counts as changed when the write's value differs from either of two
// baselines, both read through the serializer the write goes through
// (api_serializers.jsx matchWire) so only a real difference counts:
//
//   - the server's value the editor's state last AGREED with for that group:
//     the match as the editor mounted on it, moved on whenever a write is
//     built while the editor's value and the match it renders from are the
//     same (its own write come back, or a value it adopted). Not simply the
//     latest copy of the match: an editor that does not follow a group (the
//     individual editor seeds its points and overtime once) would otherwise
//     read another device's later change to that group as its own and put the
//     old value back.
//   - the last write this editor built for the match: a tap taken back before
//     the first write's echo arrives reads the same as the copy that has not
//     caught up yet, and without this the second write would name nothing and
//     the server would keep the first.
// ---------------------------------------------------------------------------
export function useChangedGroups(match) {
  // React.useRef by name (not the useRefA alias), so the linter knows these are
  // refs, which the layout effect below may write.
  const matchRef = React.useRef(match);
  const stateRef = React.useRef(null);
  // Each commit brings the hook up to the match it rendered, in a layout effect:
  // it runs before every passive effect (where the editors' adopts call
  // claim.agree) and before the browser takes the next event (where they build
  // and claim a write), so both read the match the editor shows. No editor calls
  // claim during render.
  React.useLayoutEffect(() => {
    matchRef.current = match;
    const keyOf = (m) => `${(m && m.compId) || ""}\u0000${(m && m.id) || ""}`;
    const finished = !!(match && match.status === "completed");
    // The editor moving to another match starts over from that match.
    if (!stateRef.current || stateRef.current.key !== keyOf(match)) {
      stateRef.current = { key: keyOf(match), seed: matchWire(match), agreed: {}, last: {}, finished, built: new WeakMap() };
    }
    // A reopen is a write the server makes, not one the editor builds, so no build
    // moves the result the editor agrees with off the finished one. Once the server
    // no longer holds the match finished the editor agrees with the result as it now
    // stands, so ending it again with the same verdict still says the result changed.
    // That is the status leaving "completed", not the reopenPending stamp: a reopen
    // without a reason carries the stamp, but one a correction's reason comes with (a
    // downstream reopen, a requalification, a pool-rank override) leaves the match
    // scheduled with its points and no stamp, and an editor that only watched the stamp
    // named no result for it: the server kept the match as it was, with no error.
    if (!finished && stateRef.current.finished) stateRef.current.agreed[GROUP_RESULT] = matchWire(match);
    stateRef.current.finished = finished;
  });
  // `agreed[g]` is the server value the editor last AGREED with for g; `last[g]` is
  // the value it last built or took for g. Both are kept per group: a group a build
  // did not state says nothing about it, and a group the editor took from the server
  // is, from then on, the value its next edit is judged against.
  const claim = (patch) => {
    if (!patch) return patch;
    const m = matchRef.current;
    const st = stateRef.current;
    const next = toBackendMatchResult(patch, m);
    const current = matchWire(m);
    const stated = statedGroupsOf(next);
    for (const g of stated) {
      if (groupMatches(g, next, current, next)) st.agreed[g] = current;
    }
    const changed = changedGroups(next, (g) => st.agreed[g] || st.seed, (g) => st.last[g]);
    const prior = {};
    for (const g of stated) { prior[g] = st.last[g]; st.last[g] = next; }
    const out = { ...patch, changed };
    st.built.set(out, { next, prior });
    return out;
  };
  // claim.forget(patch): the write `patch` was built for did not land (refused,
  // held or discarded), so what it stated is not the value the editor last
  // had for those groups: each one it still holds goes back to what it was,
  // and the next build of the same value names it again (stage 5 review 5: a
  // retried Start named only `result`, and the server started the match
  // without the draft's points). A patch claim did not build is ignored.
  claim.forget = (patch) => {
    const st = stateRef.current;
    const rec = patch && st.built.get(patch);
    if (!rec) return;
    st.built.delete(patch);
    for (const [g, prev] of Object.entries(rec.prior)) {
      if (st.last[g] !== rec.next) continue;
      if (prev === undefined) delete st.last[g];
      else st.last[g] = prev;
    }
  };
  // claim.watch(res, patch): forgets `patch` when the write's answer says it
  // did not land, read as it comes (a direct answer at once, the outbox's
  // through `settled`, a callback, never a wait). Answers `res`.
  claim.watch = (res, patch) => {
    const notLanded = () => claim.forget(patch);
    Promise.resolve(res).then((r) => {
      if (!r) { notLanded(); return; }
      if (writePending(r)) {
        // A pending answer with no settlement says nothing more.
        if (!r.settled || typeof r.settled.then !== "function") return;
        r.settled.then((body) => { if (!body || body.discarded === true || writeWasRefused(body)) notLanded(); }, notLanded);
        return;
      }
      if (writeWasRefused(r)) notLanded();
    }, notLanded);
    return res;
  };
  // claim.agree(group): the editor ADOPTED the server's value for `group`, so it
  // agrees with the match as it now stands and that is the value it last had for the
  // group. The hook cannot see an adopt, and the baseline moves only at a build where
  // the editor's own state equals the prop (above), so without this an edit that puts
  // the group back to the value the editor mounted with was compared against that
  // mount-time seed and not named (the server kept the other device's value), and an
  // edit of something else was compared against the value of the group the editor last
  // BUILT, found it different from the adopted one, and re-sent the adopted value under
  // a newer stamp. The caller states which group it took, from inside the adopt's
  // `apply` (a skipped adopt, an editor holding an edit over a newer value, agrees
  // nothing), and only for the part it took, never from the latest prop alone (an edit
  // the editor holds over a newer prop must stay a change). The property rides on the
  // function so the editors that only claim are unchanged.
  claim.agree = (group) => {
    const st = stateRef.current;
    const adopted = matchWire(matchRef.current);
    st.agreed[group] = adopted;
    st.last[group] = adopted;
  };
  return claim;
}

// ---------------------------------------------------------------------------
// useKeptInHistoryNote (bc-mrgc): the quiet note a score editor shows when a
// write's answer kept something in the match's history: part of the write
// (a newer change to the same thing was recorded first, or it would leave the
// finished match without a winner), or a later change the write moved there
// (writeDisplacedGroups). The editor's flow carries on; the note only names
// what was kept (keptInHistoryNote, match_groups.jsx). A write that lands
// keeping nothing clears it; one that did not land (queued, or refused, which
// has its own banner) leaves it.
// Returns { note, noteFromWrite }, noteFromWrite stable across renders so the
// autosave can hold it.
// ---------------------------------------------------------------------------
export function useKeptInHistoryNote() {
  const [note, setNote] = useStateA(null);
  const mountedRef = React.useRef(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  // The note last set, so a write that changes nothing about it (the common
  // case: every landed autosave) renders nothing.
  const shownRef = React.useRef(null);
  // One function for the editor's life (setNote never changes).
  const noteFromWrite = React.useCallback((res) => {
    if (!mountedRef.current || !res) return;
    let next = shownRef.current;
    // keptInHistoryNote is the one wording: groups of this write held
    // (applied in part, or held because the finished match needs a
    // winner, whether the rest applied or not), and later changes this
    // write moved to the history. A landed write that kept nothing clears
    // the note; a write that did not land leaves it.
    const kept = keptInHistoryNote(res);
    if (kept) next = kept;
    else if (!writeDidNotLand(res)) next = null;
    if (next === shownRef.current) return;
    shownRef.current = next;
    setNote(next);
  }, [setNote]);
  return { note, noteFromWrite };
}

export function KeptInHistoryNote({ note }) {
  if (!note) return null;
  return <div className="sb-hint" role="status" data-testid="kept-in-history-note">{note}</div>;
}
