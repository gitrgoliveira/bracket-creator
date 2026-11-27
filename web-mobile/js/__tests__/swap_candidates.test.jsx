// bc-drev K4: the marks of the Swap competitors mode. The server judges each
// candidate; these helpers only read its answer (better, worse, same) into
// marks and word what the marks mean on the banner's second line.
import { describe, it, expect } from 'vitest';
import { candidateVerdicts, candidateMarks, markText, marksKey, MARK_KEYS } from '../admin_swap_candidates.jsx';
import { swapPickLine } from '../admin_competition_bracket.jsx';

const answer = (pickId, candidates) => ({ pick: { id: pickId, place: { pool: 'Pool A', position: 1 } }, candidates });

describe('the marks of one pick', () => {
  it('marks better and worse, and leaves same and anything else plain', () => {
    const marks = candidateVerdicts(answer('p1', [
      { id: 'p2', verdict: 'better' },
      { id: 'p3', verdict: 'worse' },
      { id: 'p4', verdict: 'same' },
      { id: 'p5', verdict: 'unknown' },
      { id: '', verdict: 'better' },
    ]), 'p1');
    expect([...marks.entries()]).toEqual([['p2', 'better'], ['p3', 'worse']]);
  });

  it('never marks the pick itself, and takes no answer about another pick', () => {
    expect(candidateVerdicts(answer('p1', [{ id: 'p1', verdict: 'better' }]), 'p1').size).toBe(0);
    expect(candidateVerdicts(answer('p9', [{ id: 'p2', verdict: 'better' }]), 'p1').size).toBe(0);
    expect(candidateVerdicts(null, 'p1').size).toBe(0);
    expect(candidateVerdicts(answer('p1', [{ id: 'p2', verdict: 'worse' }]), null).size).toBe(0);
  });

  it('keeps the dojos each mark is about', () => {
    const { marks, dojos } = candidateMarks(answer('p1', [
      { id: 'p2', verdict: 'worse', dojos: ['Fudo', 'Hizen'] },
      { id: 'p3', verdict: 'better', dojos: ['Kenyu'] },
      { id: 'p4', verdict: 'same' },
    ]), 'p1');
    expect([...marks.keys()]).toEqual(['p2', 'p3']);
    expect(dojos.get('p2')).toEqual(['Fudo', 'Hizen']);
    expect(dojos.get('p3')).toEqual(['Kenyu']);
    expect(dojos.has('p4')).toBe(false);
  });

  it('names the dojos a mark affects, never reading as the candidate\'s own', () => {
    expect(markText('worse', ['Fudo', 'Hizen'])).toBe('Worse for Fudo, Hizen');
    expect(markText('better', ['Kenyu'])).toBe('Better for Kenyu');
    expect(markText('same', ['Fudo'])).toBe('');
    expect(markText(null)).toBe('');
  });
});

describe('what the banner says of the marks', () => {
  const marks = (...vs) => new Map(vs.map((v, i) => [`p${i}`, v]));

  it('names the colours that show and what they mean', () => {
    expect(marksKey({ marks: marks('better', 'worse') })).toBe('both');
    expect(MARK_KEYS.both).toEqual({ text: 'Green keeps dojo-mates apart longer, amber brings some closer.', quiet: false });
    expect(marksKey({ marks: marks('better') })).toBe('better');
    expect(marksKey({ marks: marks('worse') })).toBe('worse');
    // Amber alone: nothing green to explain, and no swap of this pick helps.
    expect(MARK_KEYS.worse).toEqual({ text: 'No swap here helps. Amber brings dojo-mates closer.', quiet: false });
  });

  it('says quietly when no swap changes anything, or the marks could not be read, and nothing while the read is out', () => {
    expect(marksKey({ marks: marks() })).toBe('same');
    expect(MARK_KEYS.same).toEqual({ text: 'No swap changes when dojo-mates can meet.', quiet: true });
    expect(marksKey({ marks: null, failed: true })).toBe('failed');
    expect(MARK_KEYS.failed).toEqual({ text: 'The swaps could not be marked.', quiet: true });
    expect(marksKey({ marks: null, failed: false })).toBe('loading');
    expect(MARK_KEYS.loading).toEqual({ text: 'Marking the swaps…', quiet: true });
  });

  it('names the pick number first, then the place, the same length whatever the marks say', () => {
    const picked = { name: 'Player 08', number: 'P1', place: { pool: 'Pool A', position: 1 } };
    expect(swapPickLine(picked, null)).toBe('Swap P1 Player 08 (Pool A) with…');
    const bracket = { rounds: [[{ matchNumber: 1 }, { matchNumber: 2 }, { matchNumber: 3 }]] };
    expect(swapPickLine({ name: 'Player 01', number: 'K1', place: { slot: 5 } }, bracket)).toBe('Swap K1 Player 01 (M3) with…');
    expect(swapPickLine({ name: 'Arthur', place: { pool: 'Pool B', position: 2 } }, null), 'no number when the competition does not number')
      .toBe('Swap Arthur (Pool B) with…');
  });
});
