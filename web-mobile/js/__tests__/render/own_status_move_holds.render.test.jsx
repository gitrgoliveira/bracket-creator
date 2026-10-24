// Stage 6 review 9 (F7): the court console reads a move it made as made until
// its feed shows it. A move the server answered ends on a feed copy stamped
// after the answer, and on one past the answer's score version whatever its
// stamp (a re-created match, or another device's clock behind this one).
import { describe, it, expect, beforeAll } from 'vitest';

let ownStatusMoveHolds;
beforeAll(async () => {
  ({ ownStatusMoveHolds } = await import('../../admin_shiaijo.jsx'));
});

describe('ownStatusMoveHolds', () => {
  const answered = { status: 'completed', landed: true, stamp: 777, version: 5 };
  it('holds over an older running copy', () => {
    expect(ownStatusMoveHolds(answered, { status: 'running', modifiedAt: 500, scoreVersion: 4 })).toBe(true);
  });
  it('ends on a copy stamped after the answer', () => {
    expect(ownStatusMoveHolds(answered, { status: 'running', modifiedAt: 900, scoreVersion: 5 })).toBe(false);
  });
  it('ends on a copy past the answer\'s score version, whatever its stamp', () => {
    expect(ownStatusMoveHolds(answered, { status: 'running', modifiedAt: 100, scoreVersion: 6 })).toBe(false);
  });
  it('a queued move holds until the feed shows it', () => {
    expect(ownStatusMoveHolds({ status: 'scheduled', landed: false }, { status: 'running', modifiedAt: 999, scoreVersion: 9 })).toBe(true);
    expect(ownStatusMoveHolds({ status: 'scheduled', landed: false }, { status: 'scheduled' })).toBe(false);
  });
});
