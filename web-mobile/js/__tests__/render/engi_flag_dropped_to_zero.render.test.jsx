// bc-evts stage 4 review 1 (F1): an engi flag count that drops to 0 on another
// device clears on this one. The server leaves a 0 count out of the match it
// pushes (FlagsA `omitempty`), and a push is merged onto the copy held, so an
// omitted count used to keep the old one: this device kept showing Aka 2 after
// device B took both flags off, and its Save recorded the wrong winner. The one
// owner is mergeMatchPatch (data.jsx): a full match result (it names a status)
// that leaves a flag count out says 0 for a match that carries flags.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, engiBout, counts, NOW } from '../helpers/engi_editor.js';
import { applyPatch } from '../../patch.jsx';
import { applyPatchToTree } from '../../court_bridge.jsx';
import { mergeMatchPatch } from '../../data.jsx';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;

// The server's push after device B took both of Aka's flags off: Shiro 1, and
// no flagsA at all.
const push = (over = {}) => ({
  type: 'match_updated',
  data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'running', flagsB: 1, modifiedAt: NOW + 50, scoreVersion: 5, ...over } },
});
const tree = (match) => ({ id: 'c1', engi: true, participants: [], poolMatches: [match] });

describe('a push that omits a count which fell to 0', () => {
  it('the board of an open engi editor shows Aka 0, not the old 2', async () => {
    let comp = tree(engiBout({ flagsB: 1, flagsA: 2 }));
    let r;
    await act(async () => { r = render(el(comp.poolMatches[0])); });
    expect(counts(r.container)).toEqual([1, 2]);

    comp = applyPatch(comp, push());
    await act(async () => { r.rerender(el(comp.poolMatches[0])); });
    expect(counts(r.container)).toEqual([1, 0]);
  });

  it('a display on the bridge reads the same 0', () => {
    const msg = { type: 'patch', compId: 'c1', payload: push().data };
    const next = applyPatchToTree({ competitions: [tree(engiBout({ flagsB: 1, flagsA: 2 }))] }, msg);
    expect(next.competitions[0].poolMatches[0].flagsA).toBe(0);
    expect(next.competitions[0].poolMatches[0].flagsB).toBe(1);
  });

  it('a count the push carries is taken as it is, and a push naming no status is not a full result', () => {
    const held = engiBout({ flagsB: 1, flagsA: 2 });
    expect(mergeMatchPatch(held, { id: 'm1', status: 'running', flagsA: 1, scoreVersion: 5 }).flagsA).toBe(1);
    expect(mergeMatchPatch(held, { id: 'm1', court: 'B' }).flagsA).toBe(2);
  });

  it('a match that carries no flags gains none', () => {
    const plain = { id: 'm1', status: 'running', ipponsA: ['M'] };
    const merged = mergeMatchPatch(plain, { id: 'm1', status: 'completed' });
    expect('flagsA' in merged).toBe(false);
    expect('flagsB' in merged).toBe(false);
  });
});
