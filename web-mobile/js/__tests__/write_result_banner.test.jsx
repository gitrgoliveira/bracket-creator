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
