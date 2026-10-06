// takeMembers, recordWrites and changedMembers (lineup_draft.jsx) are what an editor that
// writes team members itself keeps them with: a list that arrives is merged with the
// members shown, and a name the editor wrote stands over a list that predates it until a
// list shows the name, or shows another one, which another device gave.

import { describe, it, expect } from 'vitest';
import { changedMembers, recordWrites, takeMembers } from '../lineup_draft.jsx';

const member = (n, name = '') => ({ id: `m${n}`, index: n, name });
const names = (members) => members.map((m) => `${m.index}:${m.name}`);
// What an editor wrote for a member that no list has shown yet: the name it gave, and the
// names the member had in the lists that predate the write (none for a member it added).
const wrote = (name, ...was) => ({ name, was });

describe('takeMembers: a list arrives with nothing written by the editor', () => {
  it('takes the list as it holds each member, and keeps a member shown that it lacks', () => {
    const shown = [member(1), member(2, 'Kai'), member(6, 'Added elsewhere')];
    const arriving = [member(2, 'Kai Mori'), member(1, 'Ren')];

    const { members, pending } = takeMembers(shown, arriving, {});

    expect(names(members)).toEqual(['1:Ren', '2:Kai Mori', '6:Added elsewhere']);
    expect(pending).toEqual({});
  });
});

describe('takeMembers: a name the editor wrote', () => {
  it('stands over a list that holds the name the member had before the write', () => {
    const shown = [member(1, 'Ito'), member(2, 'Kai')];
    const arriving = [member(1), member(2, 'Kai')];

    const { members, pending } = takeMembers(shown, arriving, { m1: wrote('Ito', '') });

    expect(names(members)).toEqual(['1:Ito', '2:Kai']);
    expect(pending).toEqual({ m1: wrote('Ito', '') });
  });

  it('stands over a list that holds the name the member had before the write, whatever that was', () => {
    const shown = [member(1, 'Ito')];

    const { members, pending } = takeMembers(shown, [member(1, 'Old name')], { m1: wrote('Ito', 'Old name') });

    expect(names(members)).toEqual(['1:Ito']);
    expect(pending).toEqual({ m1: wrote('Ito', 'Old name') });
  });

  it('stands over a list that lacks the member', () => {
    const shown = [member(1, 'Ren'), member(2, 'Ito')];

    const { members, pending } = takeMembers(shown, [member(1, 'Ren')], { m2: wrote('Ito', '') });

    expect(names(members)).toEqual(['1:Ren', '2:Ito']);
    expect(pending).toEqual({ m2: wrote('Ito', '') });
  });

  it('keeps a member the editor added when the list lacks it', () => {
    const shown = [member(1, 'Ren'), member(6, 'Newcomer')];

    const { members, pending } = takeMembers(shown, [member(1, 'Ren')], { m6: wrote('Newcomer') });

    expect(names(members)).toEqual(['1:Ren', '6:Newcomer']);
    expect(pending).toEqual({ m6: wrote('Newcomer') });
  });

  it('is done with once a list shows the name, and a later change elsewhere shows', () => {
    const shown = [member(1, 'Ito')];

    const caughtUp = takeMembers(shown, [member(1, 'Ito')], { m1: wrote('Ito', '') });
    expect(names(caughtUp.members)).toEqual(['1:Ito']);
    expect(caughtUp.pending).toEqual({});

    const renamed = takeMembers(caughtUp.members, [member(1, 'Ito Kato')], caughtUp.pending);
    expect(names(renamed.members)).toEqual(['1:Ito Kato']);
  });

  // A rename and a rename back: the name written is also a name the member had before. A list
  // that shows it is the server caught up, not one that predates the write, so the write is
  // done with and a change made elsewhere to the name in between shows from then on.
  it('is done with once a list shows the name, even when the member had that name before: a rename and a rename back', () => {
    const shown = [member(1, 'Ito')];

    const caughtUp = takeMembers(shown, [member(1, 'Ito')], { m1: wrote('Ito', 'Ito', 'Itoh') });

    expect(names(caughtUp.members)).toEqual(['1:Ito']);
    expect(caughtUp.pending).toEqual({});
    const changed = takeMembers(caughtUp.members, [member(1, 'Itoh')], caughtUp.pending);
    expect(names(changed.members)).toEqual(['1:Itoh']);
  });

  it('is judged member by member', () => {
    const shown = [member(1, 'Ito'), member(2, 'Mori')];
    const arriving = [member(1, 'Ito'), member(2)];

    const { members, pending } = takeMembers(shown, arriving, { m1: wrote('Ito', ''), m2: wrote('Mori', '') });

    expect(names(members)).toEqual(['1:Ito', '2:Mori']);
    expect(pending).toEqual({ m2: wrote('Mori', '') });
  });

  it('does not touch a member the editor did not write', () => {
    const shown = [member(1, 'Ito'), member(2, 'Kai')];
    const arriving = [member(1), member(2, 'Kai Mori')];

    const { members } = takeMembers(shown, arriving, { m1: wrote('Ito', '') });

    expect(names(members)).toEqual(['1:Ito', '2:Kai Mori']);
  });
});

// A list that holds neither the name written nor the name the member had before it was
// not read before the write: another device gave the member that name. It is shown, and
// the write is done with, so a later change shows as well. (A change made elsewhere before
// the write but read after it shows for a moment: the next list holds the write.)
describe('takeMembers: a name another device gave the member', () => {
  it('is shown over the name the editor wrote, and the write is done with', () => {
    const shown = [member(1, 'Ito')];

    const { members, pending } = takeMembers(shown, [member(1, 'Itoh')], { m1: wrote('Ito', '') });

    expect(names(members)).toEqual(['1:Itoh']);
    expect(pending).toEqual({});
  });

  it('is shown over a member the editor added, which the list holds under another name', () => {
    const shown = [member(1, 'Ren'), member(6, 'Newcomer')];

    const { members, pending } = takeMembers(shown, [member(1, 'Ren'), member(6, 'Mei Endo')], { m6: wrote('Newcomer') });

    expect(names(members)).toEqual(['1:Ren', '6:Mei Endo']);
    expect(pending).toEqual({});
  });

  it('is shown when it clears the name, if the member had a name before the write', () => {
    const shown = [member(1, 'Ito')];

    const { members, pending } = takeMembers(shown, [member(1, '')], { m1: wrote('Ito', 'Old name') });

    expect(names(members)).toEqual(['1:']);
    expect(pending).toEqual({});
  });

  it('is told from a list that predates two writes to the same member, which still shows the first name or the one before', () => {
    const shown = [member(1, 'Itoh')];
    const both = { m1: wrote('Itoh', '', 'Ito') };

    for (const read of ['', 'Ito']) {
      const stale = takeMembers(shown, [member(1, read)], both);
      expect(names(stale.members)).toEqual(['1:Itoh']);
      expect(stale.pending).toEqual(both);
    }
    expect(takeMembers(shown, [member(1, 'Itoh')], both).pending).toEqual({});
    expect(names(takeMembers(shown, [member(1, 'Other')], both).members)).toEqual(['1:Other']);
  });
});

describe('recordWrites', () => {
  it('notes with each name written the name the member had in the list shown before the write', () => {
    const shown = [member(1), member(2, 'Kai')];

    expect(recordWrites({}, shown, [member(1, 'Ito')])).toEqual({ m1: wrote('Ito', '') });
  });

  it('notes none for a member the write added', () => {
    expect(recordWrites({}, [member(1, 'Ren')], [member(6, 'Newcomer')])).toEqual({ m6: wrote('Newcomer') });
  });

  it('reads an unnamed member as the empty name, whichever way the name is missing', () => {
    expect(recordWrites({}, [{ id: 'm1', index: 1 }], [{ id: 'm1', index: 1, name: 'Ito' }])).toEqual({ m1: wrote('Ito', '') });
    expect(recordWrites({}, [member(1)], [{ id: 'm1', index: 1 }])).toEqual({ m1: wrote('', '') });
  });

  it('keeps the names a member had before an earlier write that no list has shown yet', () => {
    const first = recordWrites({}, [member(1)], [member(1, 'Ito')]);

    const second = recordWrites(first, [member(1, 'Ito')], [member(1, 'Itoh')]);

    expect(second).toEqual({ m1: wrote('Itoh', '', 'Ito') });
  });

  it('leaves the writes of other members as they are, and the pending it is given untouched', () => {
    const pending = { m2: wrote('Mori', '') };

    const noted = recordWrites(pending, [member(1)], [member(1, 'Ito')]);

    expect(noted).toEqual({ m1: wrote('Ito', ''), m2: wrote('Mori', '') });
    expect(pending).toEqual({ m2: wrote('Mori', '') });
  });
});

describe('changedMembers', () => {
  it('names the members a change renamed or added, and no other', () => {
    const before = [member(1), member(2, 'Kai')];
    const after = [member(1, 'Ito'), member(2, 'Kai'), member(6, 'Newcomer')];

    expect(changedMembers(before, after)).toEqual([member(1, 'Ito'), member(6, 'Newcomer')]);
  });

  it('reads an unnamed member as it is, whichever way the name is missing', () => {
    expect(changedMembers([{ id: 'm1', index: 1 }], [member(1)])).toEqual([]);
  });

  it('names nothing for a change that wrote nothing', () => {
    const list = [member(1, 'Ren'), member(2)];
    expect(changedMembers(list, list.map((m) => ({ ...m })))).toEqual([]);
  });
});
