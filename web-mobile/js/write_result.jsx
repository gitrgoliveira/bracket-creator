// Owner of ONE question: did a score write actually land, and if not, is the
// state the operator is looking at ever going to become true?
//
// This is a leaf on purpose (no imports, no window reads) so that BOTH kinds of
// consumer can reach the same rule without either being forced into the other's
// loading model:
//
//   - api_client.jsx imports it and re-publishes both predicates on `window`,
//     which is how the script-tagged surfaces (admin_shiaijo, the schedule score
//     editor, viewer_match) ask the question.
//   - admin_scoring_shared.jsx imports it DIRECTLY, because it is itself
//     imported by the scoring modals and by unit tests that never load
//     api_client at all. It cannot import api_client to get here: api_client is
//     script-tagged, and a module that is both script-tagged and ES-imported
//     loads twice under two URLs, splitting its module-level singleton state
//     (the write queue, the listener sets). That is a known, previously-shipped
//     failure in this codebase, so the dependency has to point this way.
//
// Why the rule lives in one place at all: it was previously spelled
// `res.queued` at each call site, and when the server gained a SECOND
// not-landed shape the conversion reached five sites and missed the sixth --
// which happened to be the one guarding a hard prerequisite. A predicate a
// caller must remember to re-derive is a predicate that drifts.

// writeDidNotLand: the write is NOT stored. Two shapes, one meaning --
// `queued` (never reached the server, held for retry) and `applied:false` (the
// server's timestamp last-write-wins guard dropped it because a newer result is
// already recorded).
//
// Ask this before behaving as if the result is now stored: closing an editor,
// advancing to the next match, reporting success.
export function writeDidNotLand(res) {
    return !!res && (res.queued === true || res.applied === false);
}

// writeWasSuperseded: the STRONGER half. Both shapes above mean "not stored",
// but they differ on whether the local optimistic state will still come true.
//
// A `queued` write lands on reconnect, so applying it locally is correct -- an
// offline court advancing its own bracket is the only thing that keeps it
// moving. A superseded write NEVER lands: a different writer's result won, so
// anything derived from the operator's version (a bracket advance, a promoted
// next match, a dependent request built on that scoreline) is wrong and must be
// skipped rather than deferred.
//
// Ask writeDidNotLand before treating a result as stored; ask this before
// applying anything derived from it.
export function writeWasSuperseded(res) {
    return !!res && res.applied === false;
}
