// Stage 6 review 9 (F7): the court console reads a move it made as made until
// its feed shows it. A move the server answered ends on a feed copy stamped
// after the answer, and on one past the answer's score version whatever its
// stamp (a re-created match, or another device's clock behind this one).
import { describe, it, expect, beforeAll } from 'vitest';

let ownStatusMoveHolds;
let ownStatusMoveEnded;
beforeAll(async () => {
  ({ ownStatusMoveHolds, ownStatusMoveEnded } = await import('../../admin_shiaijo.jsx'));
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

// bc-sssf: the court console forgets an answered move once the feed shows it,
// but only on a copy not older than the move. The pre-start copy of a bout this
// console started and then sent back reads scheduled by chance.
describe('ownStatusMoveEnded', () => {
  const sentBack = { status: 'scheduled', landed: true, stamp: 777, version: 5 };
  it('a move not answered yet, or none, never ends', () => {
    expect(ownStatusMoveEnded(null, { status: 'scheduled', modifiedAt: 999 })).toBe(false);
    expect(ownStatusMoveEnded({ status: 'scheduled', landed: false }, { status: 'scheduled', modifiedAt: 999 })).toBe(false);
    expect(ownStatusMoveEnded(sentBack, null)).toBe(false);
  });
  it('does not end on an older copy that reads the move\'s status by chance', () => {
    expect(ownStatusMoveEnded(sentBack, { status: 'scheduled', modifiedAt: 500, scoreVersion: 5 })).toBe(false);
  });
  it('ends on a copy in the move\'s status stamped at or after the move', () => {
    expect(ownStatusMoveEnded(sentBack, { status: 'scheduled', modifiedAt: 777, scoreVersion: 5 })).toBe(true);
    expect(ownStatusMoveEnded(sentBack, { status: 'scheduled', modifiedAt: 900, scoreVersion: 5 })).toBe(true);
  });
  it('ends on a copy in the move\'s status past the answer\'s score version, whatever its stamp', () => {
    expect(ownStatusMoveEnded(sentBack, { status: 'scheduled', modifiedAt: 100, scoreVersion: 6 })).toBe(true);
  });
  it('the same score version is not enough: a send back can leave it where it was', () => {
    expect(ownStatusMoveEnded(sentBack, { status: 'scheduled', modifiedAt: 100, scoreVersion: 5 })).toBe(false);
  });
  it('a copy in another status ends it only once a later change made elsewhere shows', () => {
    expect(ownStatusMoveEnded(sentBack, { status: 'running', modifiedAt: 500, scoreVersion: 4 })).toBe(false);
    expect(ownStatusMoveEnded(sentBack, { status: 'running', modifiedAt: 900, scoreVersion: 5 })).toBe(true);
    expect(ownStatusMoveEnded(sentBack, { status: 'running', modifiedAt: 100, scoreVersion: 6 })).toBe(true);
  });
});
