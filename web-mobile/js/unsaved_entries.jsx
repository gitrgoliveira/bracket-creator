// unsaved_entries.jsx: the ONE owner of the warning an action gives before it
// discards a score editor's unsaved entries (operator decision R1,
// 2026-10-17: "If I don't save it, the ui should warn me it will be
// discarded."). A leaf (write_result.jsx is its only import), so the editors,
// the barred-match notice and every host that swaps or unmounts an editor ask
// it alike.
//
// The actions it guards are the ones that do not carry the draft:
//   - the editors' own: a reopen (Clear <decision> and reopen, the kachinuki
//     Reopen match), the court-busy Queue it and reopen, a decision (it
//     carries the server's values for what the draft changed, never the
//     draft's), the barred-match notice's Record fusensho, and the team
//     sheet's representative-bout add or remove over a sheet it does not save
//     first;
//   - a host's: leaving the editor or putting another match in its place
//     (every exit of a page that shows an editor inline: the court console's
//     Back to court, Correct on another match, a pick of another bout, the
//     court switcher, the Dashboard crumb, Sign out and Public viewer; the
//     competition page's section tabs, crumbs, other competitions, + Add
//     competition, Sign out, Public viewer and the running strip; the bracket
//     panel's choice of another match), through `confirmLeave`/`leaveThen`;
//     and a confirm of the host's own that drops the editor's match (the
//     console's Send back to queue), which says so in its own words and then
//     drops the entries at once (`discardUnsaved`).
// A Start carries the draft and asks nothing; Close and Prev/Next ask
// "Discard unsaved scoring changes?" through the editor itself.
//
// `unsaved` is the editor's isDirty, the one Close asks on: a boolean, a
// thunk, or a ref whose `current` is either (a host holds the ref an editor
// publishes its thunk into, usePublishUnsaved, with the editor's discard on
// the thunk as `discard`). The prompt is the operator's own and never waits
// on the server: Keep entries changes nothing; Discard entries runs
// `onDiscard` (an editor drops a kept draft through its owner, so no "dropped
// on another device" note follows its own write) and the action goes on.
import { decisionWord } from './write_result.jsx';

export const UNSAVED_ENTRIES_DISCARDED = "Your unsaved entries will be discarded.";
export const REOPEN_ANYWAY = "Reopen anyway?";
export const QUEUE_AND_REOPEN_ANYWAY = "Queue it and reopen anyway?";
export const ADD_REP_BOUT_ANYWAY = "Add the representative bout anyway?";
export const REMOVE_REP_BOUT_ANYWAY = "Remove the representative bout anyway?";
export const LEAVE_MATCH_ANYWAY = "Leave this match anyway?";
export function recordDecisionAnyway(kind) {
  return `Record the ${decisionWord(kind) || "decision"} anyway?`;
}

export function unsavedDiscardDialog(question = REOPEN_ANYWAY) {
  return {
    message: `${UNSAVED_ENTRIES_DISCARDED} ${question}`,
    confirmLabel: "Discard entries",
    cancelLabel: "Keep entries",
    danger: true,
  };
}

export function holdsUnsaved(unsaved) {
  let v = unsaved;
  if (v && typeof v === "object" && "current" in v) v = v.current;
  if (typeof v === "function") v = v();
  return !!v;
}

// confirmDiscardUnsaved: true when the action may go on (nothing unsaved, or
// the operator chose Discard entries, after `onDiscard`).
export async function confirmDiscardUnsaved(unsaved, question, onDiscard) {
  if (!holdsUnsaved(unsaved)) return true;
  let ok = false;
  try {
    ok = !!(await window.confirmDialog(unsavedDiscardDialog(question)));
  } catch (e) {
    // No dialog to ask with: never discard without asking.
    console.error("unsaved-entries warning could not be shown", e);
    ok = false;
  }
  if (ok && typeof onDiscard === "function") onDiscard();
  return ok;
}

// discardUnsaved: a host drops the editor's unsaved entries at once, through
// the discard the editor publishes on its thunk (usePublishUnsaved), as the
// editor's own Discard entries does (stage 5 review 9, m1). Nothing when the
// editor publishes none.
export function discardUnsaved(unsaved) {
  let v = unsaved;
  if (v && typeof v === "object" && "current" in v) v = v.current;
  if (typeof v === "function" && typeof v.discard === "function") v.discard();
}

// confirmLeave: a host's exit that drops the editor (R1). `true` at once when
// nothing is unsaved, so the exit goes in the same tick; otherwise the promise
// of the operator's answer to LEAVE_MATCH_ANYWAY, false once `alive()` (the
// host still mounted) no longer holds.
export function confirmLeave(unsaved, alive) {
  if (!holdsUnsaved(unsaved)) return true;
  return confirmDiscardUnsaved(unsaved, LEAVE_MATCH_ANYWAY).then((ok) => ok && (typeof alive !== "function" || !!alive()));
}

// whenConfirmed: run `go` once `answer` (confirmLeave's) says yes: in the same
// tick for a plain `true`.
export function whenConfirmed(answer, go) {
  if (answer === true) { go(); return; }
  if (answer && typeof answer.then === "function") answer.then((yes) => { if (yes) go(); });
}

// leaveThen: `go` behind the leave question.
export function leaveThen(unsaved, go, alive) {
  whenConfirmed(confirmLeave(unsaved, alive), go);
}
