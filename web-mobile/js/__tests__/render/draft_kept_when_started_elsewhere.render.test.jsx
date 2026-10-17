// Operator decision D1 (a), 2026-10-17: a draft typed on a match that is not
// running (the Scores tab's Score on a scheduled match) is KEPT on screen when
// another device starts the match. The editor says so, with Save and Discard:
// Save sends the draft as the scoring taps the same gestures send on a running
// match (draft_taps.jsx), through the outbox in order; Discard drops the draft
// and shows the board. One behaviour for the individual, engi and team
// editors. This editor's own Start keeps no draft (its Start carried it).
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, marks, NOW,
} from '../helpers/individual_editor.js';
import { engiBout, counts, btn } from '../helpers/engi_editor.js';
import { teamBout, ipponBtn, marks as boutMarks, fusenshoBtn } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const pending = () => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) });
const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn(pending)} password="pw" variant="inline" {...props} />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const events = () => API.recordTap.mock.calls.map((c) => c[2]);
const notice = () => screen.queryByTestId('draft-kept-notice');
const button = (text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const NOTE = 'This match was started on another device. Your unsaved entries are still here: Save adds them to the match, Discard drops them.';

describe('the individual editor', () => {
  const scheduled = bout({ status: 'scheduled', scoreVersion: 2 });
  // Another device started the match and struck a K for Aka.
  const startedElsewhere = bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } });

  it('keeps the draft with the note when another device starts the match, and Save sends it as taps', async () => {
    const r = render(el(scheduled));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(startedElsewhere));
    expect(notice()?.textContent).toContain(NOTE);
    expect(marks(r.container).join(' '), 'the draft is still shown').toMatch(/M/);
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(button('Finish'), 'no finish while the draft is held').toBeUndefined();
    const save = screen.getByTestId('draft-kept-save');
    await tap(save);
    expect(API.recordTap, 'Save is two taps').not.toHaveBeenCalled();
    await tap(screen.getByTestId('draft-kept-save'));
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' }]);
    expect(notice()).toBeNull();
    expect(marks(r.container).join(' '), 'the board: the other device\'s K and the M saved').toMatch(/K/);
    expect(marks(r.container).join(' ')).toMatch(/M/);
  });

  it('Discard drops the draft and shows the board', async () => {
    const r = render(el(scheduled));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(startedElsewhere));
    await tap(screen.getByTestId('draft-kept-discard'));
    expect(notice()).toBeNull();
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(marks(r.container).join(' ')).not.toMatch(/M/);
    expect(marks(r.container).join(' ')).toMatch(/K/);
  });

  it('an untouched editor keeps nothing and shows the board at once', async () => {
    const r = render(el(scheduled));
    r.rerender(el(startedElsewhere));
    expect(notice()).toBeNull();
  });

  it('its own Start keeps no draft: the Start carried it', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(scheduled, { onSubmit }));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    r.rerender(el(bout({ status: 'running', ipponsB: ['M'], modifiedAt: NOW + 50, scoreVersion: 4 }), { onSubmit }));
    expect(notice()).toBeNull();
  });
});

describe('the engi editor', () => {
  it('keeps the flag draft; Save sends flag.add for it', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await tap(btn(r.container, 'engi-shiro-inc'));
    r.rerender(el(engiBout({ status: 'running', flagsA: 1, modifiedAt: NOW + 50, scoreVersion: 5 })));
    expect(notice()?.textContent).toContain(NOTE);
    expect(counts(r.container), 'the draft: Shiro 1, Aka 0').toEqual([1, 0]);
    await tap(screen.getByTestId('draft-kept-save'));
    await tap(screen.getByTestId('draft-kept-save'));
    expect(events()).toEqual([expect.objectContaining({ kind: 'flag.add', side: 'b' })]);
    expect(notice()).toBeNull();
    expect(counts(r.container), 'the board: the other device\'s Aka flag and the saved Shiro flag').toEqual([1, 1]);
  });

  it('Discard shows the board', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await tap(btn(r.container, 'engi-shiro-inc'));
    r.rerender(el(engiBout({ status: 'running', flagsA: 1, modifiedAt: NOW + 50, scoreVersion: 5 })));
    await tap(screen.getByTestId('draft-kept-discard'));
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(counts(r.container)).toEqual([0, 1]);
  });
});

describe('the team editor', () => {
  it('keeps the draft rows; Save sends a bout point and a bout decision as taps', async () => {
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' })));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(fusenshoBtn(r.container, 2, 'shiro'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 })));
    expect(notice()?.textContent).toContain(NOTE);
    expect(boutMarks(r.container, 0, 'aka'), 'the draft is still shown').toEqual(['M']);
    expect(API.recordTap).not.toHaveBeenCalled();
    await tap(screen.getByTestId('draft-kept-save'));
    await tap(screen.getByTestId('draft-kept-save'));
    expect(events()).toEqual([
      { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' },
      { kind: 'bout.decision', pos: 3, side: 'b', value: 'fusensho' },
    ]);
    expect(notice()).toBeNull();
    expect(boutMarks(r.container, 0, 'aka')).toEqual(['M']);
  });

  it('Discard shows the board', async () => {
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' })));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 })));
    await tap(screen.getByTestId('draft-kept-discard'));
    expect(notice()).toBeNull();
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(boutMarks(r.container, 0, 'aka')).toEqual([]);
  });
});
