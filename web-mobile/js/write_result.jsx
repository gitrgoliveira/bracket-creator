// Owner of ONE question: did a score write actually land, and if not, is the
// state the operator is looking at ever going to become true?
//
// EVERY consumer imports this module directly -- api_client.jsx, the two
// scoring editors (admin_scoring_team, admin_scoring_individual),
// admin_scoring_shared.jsx, admin_shiaijo.jsx, admin.jsx (the single
// editMatchScore chokepoint every score-editor host routes through), the
// schedule score editor and viewer_match.jsx. Nothing reads these names off
// `window`: the mirrors api_client used to publish are gone, so there is
// exactly one binding per name and no second spelling to drift.
//
// This is a leaf on purpose (no imports, no window reads), and it is
// import-only: it has no <script type="module"> tag of its own and must never
// gain one. A module that is BOTH script-tagged and ES-imported loads twice
// under two URLs and splits its module-level singleton state -- a known,
// previously-shipped failure here (mp-zd1v), and the reason a script-tagged
// surface like admin_shiaijo.jsx cannot import api_client.jsx to reach this
// rule. Because THIS file is never tagged, importing it is safe from either
// kind of module, which is what makes the direct import available everywhere
// and the `window` mirrors unnecessary.
//
// Prefer the import over any re-derivation of the test: two of the shiaijo call
// sites sit inside a swallowing catch, where a missing binding would degrade
// into the silent not-saved failure these predicates exist to remove.
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

// SUPERSEDED_REASON / SUPERSEDED_ADVICE: the copy for the one case where
// re-entering is the wrong move (bc-lww1). Every OTHER write failure ends in
// "re-enter the result", and here that is actively wrong: re-entering
// re-stamps the write with the current clock, so it would beat the newer
// stored result and undo it. One owner for this string pair: api_client.jsx's
// `_notifyScoreSuperseded` broadcast uses it, and so does notLandedBanner at
// the foot of this file -- the answer for every explicit-tap call site that
// submits with status:"running", the shape that broadcast deliberately stays
// silent for (a superseded autosave is routine noise; an operator tapping
// "Start match" or "Record bout" and having it silently do nothing is not) and
// which therefore builds this banner state from the awaited result instead of
// relying on the subscription.
export const SUPERSEDED_REASON = 'a newer result for this match is already recorded';
export const SUPERSEDED_ADVICE = 'Check the recorded result before re-entering anything: re-submitting would overwrite the newer one.';

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

// CLOCK_SKEW_REASON_TEXT / CLOCK_SKEW_ADVICE: the copy for the OTHER
// not-landed verdict (bc-cse). The server refuses a write whose modifiedAt is
// implausibly far in its own future with 200 {"applied": false, "reason":
// "clock_skew"}, and the remedy is the exact OPPOSITE of the superseded one:
//
//   - superseded  -> a newer result IS stored. Re-entering re-stamps the write
//                    with the current clock, so it would beat that newer result
//                    and undo it. Look first, and only then decide.
//   - clock_skew  -> NOTHING is stored. No other writer won, nothing about the
//                    match changed, and this device's stamp was simply wrong.
//                    Re-entering IS the remedy, and it is safe.
//
// So they must never share a banner. Telling a clock-refused operator to "check
// the recorded result before re-entering" points them at a result that does not
// exist, and tells them not to do the one thing that would save their work.
//
// Owned here, beside the superseded pair, for the same reason that pair is: the
// consumers are api_client.jsx and notLandedBanner below, which is what the
// explicit-tap call sites in the scoring editors ask to turn an awaited result
// into this banner state.
export const CLOCK_SKEW_REASON_TEXT = "this device's clock was out of step with the server";
export const CLOCK_SKEW_ADVICE = 'The clock has been resynced. Nothing was recorded, so enter the result again.';

// CLOCK_SKEW_UNHEALED_ADVICE: the same verdict AFTER the client has already
// healed and retried once and been refused again. Everything above still holds
// (nothing is stored, re-entering is safe), but the resync did not fix the
// frame, so promising that it did would send the operator round the identical
// refusal with no idea why. The device itself has to be corrected.
export const CLOCK_SKEW_UNHEALED_ADVICE = 'Nothing was recorded. This device\'s clock needs fixing, or entering the result again will be refused the same way.';

// writeWasRefusedForClock: the NARROW half of writeWasSuperseded. Both are
// `applied === false`, so every advance-skip and banner gate that asks
// writeWasSuperseded keeps treating a clock refusal as not-landed - which is
// right, because a refused write must never advance a bracket whatever the
// reason it was refused for.
//
// This is additive on top of that: ask it FIRST wherever the operator is told
// WHY, so the two verdicts get their own copy (see the pair above). Anywhere the
// question is only "did this land?", writeWasSuperseded remains the right ask.
export function writeWasRefusedForClock(res) {
    return !!res && res.applied === false && res.reason === 'clock_skew';
}

// notLandedBanner: the ONE owner of "which not-saved banner does this result
// deserve?", answering with the { reason, advice } pair the scoring editors put
// straight into their writeFailed state, or null when there is nothing to say.
//
// The three explicit-tap sites (Start match in both editors, Record bout in the
// team one) each held the same if/else chain: ask writeWasRefusedForClock
// first, fall back to writeWasSuperseded, say nothing otherwise. That ORDER is
// the whole point and is easy to paste wrong -- both verdicts are
// `applied === false`, so testing the broad one first swallows the narrow one
// and tells a clock-refused operator to go and look at a newer result that does
// not exist. A rule three call sites must remember to re-derive is the rule
// that drifted before (see the note at the top of this file).
//
// null covers BOTH remaining cases, and they are different on purpose:
//   - the write landed: nothing to report.
//   - the write was QUEUED: not stored yet, but it will be. The editor's
//     queued/offline surface already owns that state, so these taps say
//     nothing about it -- exactly what the pasted chains did, since `queued`
//     satisfies neither predicate.
export function notLandedBanner(res) {
    if (writeWasRefusedForClock(res)) {
        return { reason: CLOCK_SKEW_REASON_TEXT, advice: CLOCK_SKEW_ADVICE };
    }
    if (writeWasSuperseded(res)) {
        return { reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE };
    }
    return null;
}

// downstreamKnockoutPlayedRefusal / downstreamKnockoutPlayedConfirm /
// DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED (bc-kcdg / bc-cse).
//
// A THIRD not-landed shape, distinct from the two above: correcting a
// completed KNOCKOUT match whose LATER round has already been played used to
// silently repaint that later match's side while its own recorded result
// stayed put. The server now REFUSES the write outright -- HTTP 409
// {"error": "downstream_knockout_played", matchId, blockingMatchId,
// displaced, message} -- rather than the 200 {applied:false} shape the rest
// of this file owns, because this is a hard validation gate, not a
// last-write-wins drop: nothing raced the operator, the write is simply
// disallowed until they say so explicitly.
//
// api_client.jsx is the parser, exactly as it is for the 200 shapes: it
// attaches the four fields to the thrown Error as `.downstreamKnockoutPlayed`
// so a catcher never re-derives the shape from a raw response body or a
// message-string regex (the T103 decision_locked precedent this mirrors).
// Ask downstreamKnockoutPlayedRefusal(err) rather than testing
// `err.downstreamKnockoutPlayed` by hand -- the same reason every other
// predicate in this file exists.
export function downstreamKnockoutPlayedRefusal(err) {
    return (err && err.downstreamKnockoutPlayed) || null;
}

// The confirm dialog copy. It must NAME the blocking match and say plainly
// what confirming does: apply the correction AND send that later match back
// to be fought and re-entered (its recorded result is cleared). Cancelling
// leaves everything as it was -- the caller must not retry on a
// cancelled/false result, only on an explicit confirm.
export function downstreamKnockoutPlayedConfirm({ blockingMatchId, blockingMatchIds, displaced } = {}) {
    // The `displaced` default covers a shape the server genuinely sends: it is
    // the corrected match's STORED winner, and a bye-resolved slot is completed
    // with an empty winner, so a correction written over one arrives with
    // displaced "". The `blocking` default is narrower and is NOT a server
    // shape: every generated bracket match carries an id (engine/bracket.go),
    // so it only covers a caller passing an incomplete object, which is what
    // this function's own unit test does.
    const who = displaced || 'The competitor currently recorded as advancing';
    // matchList names EVERY match the confirmation clears. Normally one; a
    // semifinal feeds both the final and the bronze match, and both go
    // together, so the dialog has to say so rather than naming one and
    // clearing two.
    const ids = (blockingMatchIds && blockingMatchIds.length)
        ? blockingMatchIds
        : (blockingMatchId ? [blockingMatchId] : []);
    const many = ids.length > 1;
    const blocking = many
        ? `${ids.slice(0, -1).join(', ')} and ${ids[ids.length - 1]}`
        : (ids[0] || 'the later match');
    // ONE paragraph, no newlines: the dialog renders `message` in a plain <p>
    // (ui.jsx) whose .dialog-msg rule sets no white-space, so a \n here
    // silently collapses to a space rather than breaking the line.
    return {
        message: many
            ? `${who} already played matches ${blocking}, which were built on this match's current result. ` +
              'Applying this correction reopens both for re-entry: their recorded results are cleared, ' +
              'and they must be fought and scored again.'
            : `${who} already played match ${blocking}, which was built on this match's current result. ` +
              `Applying this correction reopens match ${blocking} for re-entry: its recorded result is ` +
              'cleared, and it must be fought and scored again.',
        confirmLabel: 'Apply correction and reopen',
        danger: true,
    };
}

// The cancellation notice: confirms to the operator that declining the
// override left the match, and the later one it would have reopened,
// completely unchanged -- neither was written.
export const DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED = 'Correction cancelled: the match and the later result it depends on were left unchanged.';

// downstreamKnockoutPlayedQueueDrop (bc-cse): the copy for THIS refusal
// arriving on a QUEUED replay rather than a live tap. A correction typed
// while offline (or during a transient 5xx run) is retried automatically on
// reconnect; if the later match was played in the meantime, the retry hits
// this same 409. There is no operator at the keyboard for the flush loop to
// prompt -- attemptScoreWrite's confirm dialog above has nothing to show a
// tap into -- so the write is dropped exactly like any other non-retryable
// 4xx (the api_client.jsx flush loop's generic "rejected" branch), but with
// this reason instead of the bare "downstream_knockout_played" token: a
// dropped correction is finished work the operator must be told about in
// words, and the queue must not retry it forever (it will never land
// without forceDownstreamReopen, which nothing sets automatically).
//
// Returns the { reason, advice } shape _notifyTerminalWriteFailed's payload
// and this file's own notLandedBanner both use, so a caller passes it
// straight into that channel rather than composing a third copy of the
// who/blocking defaults downstreamKnockoutPlayedConfirm already states.
export function downstreamKnockoutPlayedQueueDrop({ blockingMatchId, displaced } = {}) {
    const who = displaced || 'The competitor currently recorded as advancing';
    const blocking = blockingMatchId || 'the later match';
    return {
        reason: `${who} already played match ${blocking}, so this queued correction could not be applied automatically`,
        advice: `Redo the correction now that you're online: you'll be asked to confirm reopening match ${blocking} for re-entry.`,
    };
}

// downstreamKnockoutReopenedNotice: what to tell the operator AFTER a
// confirmed correction, naming the matches it reopened. The counterpart to
// downstreamKnockoutPlayedConfirm, which names them before.
//
// Without this, "did the next match actually reopen?" is answered only by the
// presence of a dialog beforehand and by reading the board afterwards. The
// operator authorised something specific; the app should confirm it happened.
export function downstreamKnockoutReopenedNotice(ids) {
    const list = (ids || []).filter(Boolean);
    if (!list.length) return null;
    if (list.length === 1) return `Match ${list[0]} was reopened: it must be fought and scored again.`;
    const named = `${list.slice(0, -1).join(', ')} and ${list[list.length - 1]}`;
    return `Matches ${named} were reopened: they must be fought and scored again.`;
}

// attemptScoreWrite (bc-kcdg / bc-cse): the generic confirm+retry loop for the
// refusal above. Takes recordScore/confirmDialog as INJECTED collaborators
// (never read off `window`, never imported from a host module) so it works
// from either script-tagged host that needs it: admin.jsx's editMatchScore
// (the single chokepoint every score-editor host and both editor bodies route
// a score write through) and admin_shiaijo.jsx's ResolveFeedersModal (the
// override-winner "Run now" recovery). Those two cannot import each other --
// both are `<script type="module">` entry points in index.html, and a module
// that is both script-tagged and ES-imported evaluates twice under two URLs,
// splitting its module-level singleton state (see this file's header) -- so
// the shared loop lives here instead, beside the refusal shape it orchestrates,
// which this file is safe to import from anywhere.
//
// On a refusal, prompts via confirmDialog with the message/label
// downstreamKnockoutPlayedConfirm builds; on confirm, resends the SAME result
// with forceDownstreamReopen:true, which the server applies alongside
// reopening the blocking match(es) for re-entry. Declining leaves everything
// as it was.
//
// Throws on any failure, including a declined override; in that one case the
// thrown error carries `.downstreamKnockoutPlayedCancelled = true` so the
// caller can pick DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED instead of the generic
// error copy.
export async function attemptScoreWrite({ recordScore, confirmDialog, compId, matchId, result, password, match }) {
    try {
        return await recordScore(compId, matchId, result, password, match);
    } catch (e) {
        const refusal = downstreamKnockoutPlayedRefusal(e);
        // The forceDownstreamReopen guard on `result` stops a second refusal
        // (e.g. a genuine race between two operators) from looping the confirm
        // dialog: only the first attempt for a given patch is offered the
        // override; a refusal on the forced retry is treated like any other error.
        if (!refusal || result.forceDownstreamReopen) throw e;
        const { message, confirmLabel, danger } = downstreamKnockoutPlayedConfirm(refusal);
        const ok = await confirmDialog({ message, confirmLabel, danger });
        if (ok) {
            const applied = await attemptScoreWrite({
                recordScore, confirmDialog, compId, matchId,
                result: { ...result, forceDownstreamReopen: true },
                password, match,
            });
            // Say what the confirmation DID, not just that it went through.
            // The operator agreed to reopen specific matches; a silent success
            // leaves them to work out from the board whether it happened. The
            // ids come from the refusal they just read, so the notice names the
            // same matches the dialog named. Attached rather than toasted here
            // because this module owns the words, not the surface.
            if (applied && typeof applied === 'object' && !writeDidNotLand(applied)) {
                applied.downstreamReopened = refusal.blockingMatchIds
                    || (refusal.blockingMatchId ? [refusal.blockingMatchId] : []);
            }
            return applied;
        }
        e.downstreamKnockoutPlayedCancelled = true;
        throw e;
    }
}
