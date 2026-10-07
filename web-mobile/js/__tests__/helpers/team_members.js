// What the server does to a team member, for the tests that mock the member writes. A
// member carries `modifiedAt`: 0 until it is created or named, then the stamp the server
// gave that write, which only grows for a member (max(now, stored + 1)). Every write
// (an add, a rename, a cleared name) answers the member as the server holds it, stamped,
// and a member an editor builds by hand from what was typed carries no stamp, so a mock
// that answers `true` or an unstamped copy models a server that does not exist.
let clock = 1_700_000_000_000;

/** A stamp later than every one handed out so far. */
export const nextStamp = () => {
  clock += 1000;
  return clock;
};

/** `member` as the server holds it once a write changed it by `changes`, stamped. */
export const answered = (member, changes = {}) => ({ ...member, ...changes, modifiedAt: nextStamp() });

/** `member` named later than `than` was, as another device would have named it. */
export const namedLater = (than, name) => ({ ...than, name, modifiedAt: (than.modifiedAt || 0) + 500 });
