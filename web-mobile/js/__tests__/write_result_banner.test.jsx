// notLandedBanner is the ONE owner of "which not-saved banner does this write
// result deserve?". Three explicit-tap call sites (Start match in both scoring
// editors, Record bout in the team one) used to hold the same if/else chain,
// and the ORDER in that chain is the whole point: both verdicts are
// `applied === false`, so asking the broad question first swallows the narrow
// one and tells a clock-refused operator to go and check a newer result that
// does not exist.
//
// These cases pin the mapping, including the two silences. A queued write is
// NOT a banner: it is not stored yet, but it will be, and the editor's
// queued/offline surface already owns that state.

import { describe, it, expect } from 'vitest';
import {
    notLandedBanner,
    terminalFailureBanner,
    notSavedText,
    NOT_SAVED_ADVICE,
    dependentActionBlocked,
    SUPERSEDED_REASON,
    SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT,
    CLOCK_SKEW_ADVICE,
} from '../write_result.jsx';

describe('notLandedBanner', () => {
    it('maps a clock refusal to the clock copy, not the superseded copy', () => {
        const b = notLandedBanner({ applied: false, reason: 'clock_skew' });
        expect(b).toEqual({ reason: CLOCK_SKEW_REASON_TEXT, advice: CLOCK_SKEW_ADVICE });
        // The narrow verdict must win: a clock refusal stored NOTHING, so the
        // superseded advice ("check the recorded result first") would point the
        // operator at a result that does not exist and talk them out of the one
        // action that saves their work.
        expect(b.reason).not.toBe(SUPERSEDED_REASON);
    });

    it('maps a plain supersede to the superseded copy', () => {
        expect(notLandedBanner({ applied: false })).toEqual({
            reason: SUPERSEDED_REASON,
            advice: SUPERSEDED_ADVICE,
        });
    });

    it('maps an applied:false carrying some other reason to the superseded copy', () => {
        // Only the exact 'clock_skew' wire value takes the narrow branch; any
        // other reason is still just "a newer result won".
        expect(notLandedBanner({ applied: false, reason: 'something_else' })).toEqual({
            reason: SUPERSEDED_REASON,
            advice: SUPERSEDED_ADVICE,
        });
    });

    it('says nothing about a write that landed', () => {
        expect(notLandedBanner({ applied: true })).toBeNull();
        expect(notLandedBanner({})).toBeNull();
    });

    it('says nothing about a queued write', () => {
        // Deliberate silence, and the behaviour the pasted chains had: `queued`
        // satisfies neither predicate, and the write still lands on reconnect.
        expect(notLandedBanner({ queued: true })).toBeNull();
    });

    it('says nothing when there is no result at all', () => {
        // The call sites await a submit helper that can resolve undefined when
        // the host swallowed the error, so this must not throw.
        expect(notLandedBanner(null)).toBeNull();
        expect(notLandedBanner(undefined)).toBeNull();
    });
});

// F1 (bc-p3-dh-lineups-excel review): dependentActionBlocked answers a
// narrower question than notLandedBanner -- not "does this write deserve a
// banner" but "does a caller-specific dependent action need to say why it
// silently did nothing". A refusal already has notLandedBanner's own banner,
// so this stays silent for that case on purpose (no duplicate message).
describe('dependentActionBlocked', () => {
    it('names a queued write as the reason a dependent action did not run', () => {
        expect(dependentActionBlocked({ queued: true })).toBe(
            "Couldn't save the current scores (offline or server busy). Try again once the connection is back."
        );
    });

    it('says nothing about a refusal (notLandedBanner already reports it)', () => {
        expect(dependentActionBlocked({ applied: false })).toBeNull();
        expect(dependentActionBlocked({ applied: false, reason: 'clock_skew' })).toBeNull();
    });

    it('says nothing about a write that landed, or when there is no result at all', () => {
        expect(dependentActionBlocked({ applied: true })).toBeNull();
        expect(dependentActionBlocked({})).toBeNull();
        expect(dependentActionBlocked(null)).toBeNull();
        expect(dependentActionBlocked(undefined)).toBeNull();
    });
});

// notSavedText is the ONE line every not-saved banner shows (the three score
// editors and the barred-match notice). A refusal the server worded itself is
// shown as it is: a full stop and advice after it doubled the stop, and the
// default advice could contradict it.
describe('notSavedText', () => {
    const REMOVED = "This match's representative bout was removed on another device. Check the scores and finish again.";

    it('shows a sentence the server wrote as it is, once', () => {
        expect(notSavedText({ reason: REMOVED, sentence: true })).toBe(`Not saved: ${REMOVED}`);
    });

    it('words a code or a status around it, with the advice given or the default', () => {
        expect(notSavedText({ reason: 'conflict' })).toBe(`Not saved: conflict. ${NOT_SAVED_ADVICE}`);
        expect(notSavedText({ reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE })).toBe(`Not saved: ${SUPERSEDED_REASON}. ${SUPERSEDED_ADVICE}`);
    });

    it('reads every banner notLandedBanner makes as before', () => {
        for (const res of [{ applied: false, reason: 'clock_skew' }, { applied: false }]) {
            const b = notLandedBanner(res);
            expect(notSavedText(b)).toBe(`Not saved: ${b.reason}. ${b.advice}`);
        }
    });
});

describe('terminalFailureBanner', () => {
    it('keeps the server-sentence mark a replayed refusal carries', () => {
        expect(terminalFailureBanner({ status: 409, reason: 'Said by the server.', sentence: true }))
            .toEqual({ reason: 'Said by the server.', advice: undefined, sentence: true });
    });

    it('names the status when a failure carries no reason, and marks nothing', () => {
        const b = terminalFailureBanner({ status: 400 });
        expect(b).toEqual({ reason: 'save rejected (400)', advice: undefined });
        expect(notSavedText(b)).toBe(`Not saved: save rejected (400). ${NOT_SAVED_ADVICE}`);
    });
});
