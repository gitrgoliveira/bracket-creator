// takeMembers and changedMembers (lineup_draft.jsx) are what an editor that writes team
// members itself keeps them with: a list that arrives is merged with the members shown,
// and a name the editor wrote stands over a list that predates it until a list shows it.

import { describe, it, expect } from 'vitest';
import { changedMembers, takeMembers } from '../lineup_draft.jsx';

const member = (n, name = '') => ({ id: `m${n}`, index: n, name });
const names = (members) => members.map((m) => `${m.index}:${m.name}`);

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
  it('stands over a list that predates the write', () => {
    const shown = [member(1, 'Ito'), member(2, 'Kai')];
    const arriving = [member(1), member(2, 'Kai')];

    const { members, pending } = takeMembers(shown, arriving, { m1: 'Ito' });

    expect(names(members)).toEqual(['1:Ito', '2:Kai']);
    expect(pending).toEqual({ m1: 'Ito' });
  });

  it('keeps a member the editor added when the list lacks it', () => {
    const shown = [member(1, 'Ren'), member(6, 'Newcomer')];

    const { members, pending } = takeMembers(shown, [member(1, 'Ren')], { m6: 'Newcomer' });

    expect(names(members)).toEqual(['1:Ren', '6:Newcomer']);
    expect(pending).toEqual({ m6: 'Newcomer' });
  });

  it('is done with once a list shows the name, and a later change elsewhere shows', () => {
    const shown = [member(1, 'Ito')];

    const caughtUp = takeMembers(shown, [member(1, 'Ito')], { m1: 'Ito' });
    expect(names(caughtUp.members)).toEqual(['1:Ito']);
    expect(caughtUp.pending).toEqual({});

    const renamed = takeMembers(caughtUp.members, [member(1, 'Ito Kato')], caughtUp.pending);
    expect(names(renamed.members)).toEqual(['1:Ito Kato']);
  });

  it('is judged member by member', () => {
    const shown = [member(1, 'Ito'), member(2, 'Mori')];
    const arriving = [member(1, 'Ito'), member(2)];

    const { members, pending } = takeMembers(shown, arriving, { m1: 'Ito', m2: 'Mori' });

    expect(names(members)).toEqual(['1:Ito', '2:Mori']);
    expect(pending).toEqual({ m2: 'Mori' });
  });

  it('does not touch a member the editor did not write', () => {
    const shown = [member(1, 'Ito'), member(2, 'Kai')];
    const arriving = [member(1), member(2, 'Kai Mori')];

    const { members } = takeMembers(shown, arriving, { m1: 'Ito' });

    expect(names(members)).toEqual(['1:Ito', '2:Kai Mori']);
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
