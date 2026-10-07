// bc-crpn: the two sentences the court console shows when it refuses a tap
// because a match is live or a correction is open (operator ruling
// 2026-09-27). The words are the operator's, so they are pinned here once.

import { describe, it, expect } from 'vitest';
import { startWhileCorrectingMessage, correctWhileRunningMessage, courtBusyMessage } from '../write_result.jsx';

describe('startWhileCorrectingMessage', () => {
    it('names the correction and the remedy', () => {
        expect(startWhileCorrectingMessage({ label: 'Pool A · Match 2 · Kato vs Sato' }))
            .toBe('Finish or cancel the correction of Pool A · Match 2 · Kato vs Sato first, then start this match.');
    });
});

describe('correctWhileRunningMessage', () => {
    it('names the court and the running bout, with the court-busy remedy', () => {
        const msg = correctWhileRunningMessage({ court: 'A', label: 'Match 3 · Ito vs Abe' });
        expect(msg).toBe('Shiaijo A is running Match 3 · Ito vs Abe. Finish it or send it back to the queue first. Then correct this match.');
        // Same wording and order as the server's court-busy refusal, plus the next step.
        expect(msg.startsWith(courtBusyMessage({ court: 'A', label: 'Match 3 · Ito vs Abe' }).replace(/\.$/, ''))).toBe(true);
    });
});

describe('the refusal copy follows the house words', () => {
    it('carries no em-dash and never says mat', () => {
        const all = startWhileCorrectingMessage({ label: 'x' }) + correctWhileRunningMessage({ court: 'A', label: 'x' });
        expect(all).not.toMatch(/—/);
        expect(all).not.toMatch(/\bmats?\b/i);
    });
});
