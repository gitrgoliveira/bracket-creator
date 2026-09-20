// bc-kcdg: every row of the scores list names its match, because that list is
// where an operator lands after a dialog says "Match 15 was reopened". The two
// phases number independently (the knockout once across the tree, a pool bout
// inside its own pool, restarting per pool), so the label has to say which
// numbering it is quoting.

import { describe, it, expect, afterEach } from 'vitest';
import { scoreRowMatchLabel } from '../../admin_schedule_score_editor.jsx';

afterEach(() => { delete window.poolLabel; });

describe('scoreRowMatchLabel', () => {
    it('names a knockout match bare, by the number the printed tree uses', () => {
        expect(scoreRowMatchLabel({ phase: 'bracket', id: 'm-r4-0', matchNumber: 15 }))
            .toBe('Match 15');
    });

    it('names a pool bout with its pool, since every pool has a Match 1', () => {
        window.poolLabel = (m) => m.poolName;
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Pool A-1', poolName: 'Pool A' }))
            .toBe('Pool A · Match 2');
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Pool B-1', poolName: 'Pool B' }))
            .toBe('Pool B · Match 2');
    });

    it('quotes the pool through poolLabel, so a Swiss round is not a synthetic id', () => {
        // window.poolLabel owns the pool-vs-league-vs-Swiss heading; the raw
        // poolName here would print "Swiss-R3".
        window.poolLabel = () => 'Round 3';
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Swiss-R3-0', poolName: 'Swiss-R3' }))
            .toBe('Round 3 · Match 1');
    });

    it('says nothing for a pool supplementary bout, which is not a numbered bout', () => {
        window.poolLabel = (m) => m.poolName;
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Pool A-DH-1', poolName: 'Pool A' })).toBe('');
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Pool A-TB-1', poolName: 'Pool A' })).toBe('');
    });

    it('names the 3rd-place match, the one match named rather than numbered', () => {
        // The bronze hangs off the bracket's ThirdPlaceMatch field, which the
        // numbering never walks, so it has no number in the app or on the
        // printed tree. Falling back to its id would show "m-bronze".
        expect(scoreRowMatchLabel({ phase: 'bracket', id: 'm-bronze', matchNumber: 0 }))
            .toBe('the 3rd-place match');
    });

    it('says nothing for a bracket match drawn before numbering existed', () => {
        expect(scoreRowMatchLabel({ phase: 'bracket', id: 'm-r1-0', matchNumber: 0 })).toBe('');
        expect(scoreRowMatchLabel({ phase: 'bracket', id: 'm-r1-0' })).toBe('');
    });

    it('falls back to the bare number when nothing names the pool', () => {
        expect(scoreRowMatchLabel({ phase: 'pool', id: 'Pool A-0' })).toBe('Match 1');
    });
});
