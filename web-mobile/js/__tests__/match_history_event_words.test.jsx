// bc-evts stage 3 (S7): the History view has words for a tap (door "event")
// and for what the server did with it. Side "a" is Aka, "b" is Shiro.
import { describe, it, expect } from 'vitest';
import { historyEntryView, historyDoorWords } from '../match_history_view.jsx';

const tap = (event, eventOutcome, extra = {}) => ({
    door: 'event', stamp: 1000, changed: ['points'], outcomes: { points: eventOutcome === 'applied' || eventOutcome === 'recorded' ? 'applied' : 'held' },
    event, eventOutcome, ...extra,
});

describe('a tap in the match history', () => {
    it('names the tap: who, what and which mark', () => {
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'applied')).door).toBe('Tap: Shiro M added');
        expect(historyEntryView(tap({ kind: 'ippon.remove', side: 'a', target: 'x' }, 'applied')).door).toBe('Tap: Aka point taken off');
        expect(historyEntryView(tap({ kind: 'foul.add', side: 'a' }, 'applied')).door).toBe('Tap: Aka foul added');
        expect(historyEntryView(tap({ kind: 'foul.remove', side: 'b' }, 'applied')).door).toBe('Tap: Shiro foul taken off');
        expect(historyEntryView(tap({ kind: 'encho.add' }, 'applied')).door).toBe('Tap: overtime added');
        expect(historyEntryView(tap({ kind: 'flag.add', side: 'a' }, 'applied')).door).toBe('Tap: Aka flag added');
    });
    it('an applied tap has no reason line', () => {
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'applied')).reason).toBeNull();
    });
    it('a tap not applied says why, in the server words when it sends them', () => {
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'capped')).reason).toBe('Not applied: the bout was already decided');
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'matchClosed')).reason).toBe('Not applied: the match was already finished');
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'beforeBaseline')).reason).toBe('Not applied: the result was recorded without this change');
        expect(historyEntryView(tap({ kind: 'ippon.remove', side: 'b' }, 'noTarget')).reason).toBe('Not applied: the mark it took off was already gone');
        // A strike made after Remove this bout on a device that had not seen it.
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M', pos: 2 }, 'noTarget', { reason: 'bout_removed' })).reason)
            .toBe('Not applied: the bout it was made on had been removed');
        expect(historyEntryView(tap({ kind: 'ippon.add', side: 'b', letter: 'M' }, 'matchClosed', { reason: 'the match was already finished' })).reason)
            .toBe('Not applied: the match was already finished');
    });
    // K1 (c): the server seats a kachinuki bout 1 from the lineup with events
    // of its own; the operator reads that plainly, not as a tap.
    it('the server\'s own fighter event reads as set from the lineup', () => {
        expect(historyEntryView(tap({ kind: 'bout.fighter', session: 'server', pos: 1, side: 'b', name: 'Baba' }, 'applied')).door)
            .toBe('Bout 1: Shiro fighter set from the lineup (Baba)');
        expect(historyEntryView(tap({ kind: 'bout.fighter', session: 's1', pos: 2, side: 'a', name: 'Chiba' }, 'applied')).door)
            .toBe('Tap: Aka fighter named on bout 2');
    });
    it('a door the view does not know still reads, and the replay doors have words', () => {
        expect(historyDoorWords('event')).toBe('Tap');
        expect(historyDoorWords('migrate')).toBe('Score log started');
        expect(historyDoorWords('nonsense')).toBe('Updated');
    });
});
