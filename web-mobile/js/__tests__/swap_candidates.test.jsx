// bc-drev K4: the marks of the Swap competitors mode. The server judges each
// candidate; these helpers only read its answer (better, worse, same) into
// marks and word what the marks mean on the banner's second line.
import { describe, it, expect } from 'vitest';
import { candidateVerdicts, marksKey, MARK_KEYS } from '../admin_swap_candidates.jsx';
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
});

describe('what the banner says of the marks', () => {
  const marks = (...vs) => new Map(vs.map((v, i) => [`p${i}`, v]));

  it('names the colours that show and what they mean', () => {
    expect(marksKey({ marks: marks('better', 'worse') })).toBe('both');
    expect(MARK_KEYS.both).toEqual({ text: 'Green keeps dojo-mates apart longer, amber brings some closer.', quiet: false });
    expect(marksKey({ marks: marks('better') })).toBe('better');
    expect(marksKey({ marks: marks('worse') })).toBe('worse');
  });

  it('says quietly when no swap changes anything, or the marks could not be read, and nothing while the read is out', () => {
    expect(marksKey({ marks: marks() })).toBe('same');
    expect(MARK_KEYS.same).toEqual({ text: 'No swap changes when dojo-mates can meet.', quiet: true });
    expect(marksKey({ marks: null, failed: true })).toBe('failed');
    expect(MARK_KEYS.failed).toEqual({ text: 'The swaps could not be marked.', quiet: true });
    expect(marksKey({ marks: null, failed: false })).toBeNull();
  });

  it('keeps the pick line the same length whatever the marks say', () => {
    const picked = { name: 'Player 08', number: 'P1', place: { pool: 'Pool A', position: 1 } };
    expect(swapPickLine(picked, null)).toBe('Swap Player 08 (Pool A, P1) with… tap another competitor.');
  });
});
