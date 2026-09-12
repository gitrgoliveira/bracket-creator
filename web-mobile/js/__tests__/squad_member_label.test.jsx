import { describe, it, expect } from 'vitest';
import { squadMemberLabel } from '../squad_member_label.jsx';

// squadMemberLabel is the ONE primitive that composes a squad member's
// visible label (bc-tmid pass 3): the team's competitor number plus the
// member's display index, e.g. "T10.1". It regenerates from the team's
// CURRENT number every call rather than being stored, so a renumber after
// the draw cannot leave a stale label behind.

describe('squadMemberLabel', () => {
  it('composes "<number>.<index>" for a numbered team', () => {
    expect(squadMemberLabel('T10', 1)).toBe('T10.1');
    expect(squadMemberLabel('T10', 2)).toBe('T10.2');
    expect(squadMemberLabel('A3', 7)).toBe('A3.7');
  });

  it('accepts a string index (as arrives off the wire) identically to a number', () => {
    expect(squadMemberLabel('T10', '1')).toBe('T10.1');
  });

  it('returns "" when the team has no number assigned yet', () => {
    // Pre-draw, or a competitor excluded from the draw: no meaningful label.
    expect(squadMemberLabel('', 1)).toBe('');
    expect(squadMemberLabel(null, 1)).toBe('');
    expect(squadMemberLabel(undefined, 1)).toBe('');
  });

  it('returns "" when the member has no index (defensive)', () => {
    expect(squadMemberLabel('T10', null)).toBe('');
    expect(squadMemberLabel('T10', undefined)).toBe('');
    expect(squadMemberLabel('T10', '')).toBe('');
  });

  it('treats index 0 as a real index, not a missing one', () => {
    // 0 is falsy in JS but a legitimate index value; the guard must check
    // for null/undefined/"" specifically, not just truthiness.
    expect(squadMemberLabel('T10', 0)).toBe('T10.0');
  });
});
