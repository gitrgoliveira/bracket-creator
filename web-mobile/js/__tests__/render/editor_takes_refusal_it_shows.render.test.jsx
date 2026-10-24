// The editor open on a match takes a refusal of that match's write and shows
// it, answering true on the terminal-write channel, so the outbox raises no
// alert saying it again (refusal_told_once.test.jsx pins the outbox half). An
// editor on another match takes nothing, and an editor gone leaves no
// listener, so the outbox's alert is then the one tell.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installIndividualEditor, resetOutbox, bout } from '../helpers/individual_editor.js';
import { teamBout, kachinukiBout } from '../helpers/team_editor.js';
import { engiBout } from '../helpers/engi_editor.js';

let ScoreEditorModal;
let restore;
let listeners;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => {
  resetOutbox();
  listeners = new Set();
  window.subscribeTerminalWriteFailed = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };
});

// What the outbox does with a refusal: asks every listener, taken when one
// answers true.
const raise = async (info) => {
  let taken = false;
  await act(async () => { for (const fn of listeners) { if (fn(info) === true) taken = true; } });
  return taken;
};
const banner = (c) => c.querySelector('.pending-write-banner--failed');
const REFUSAL = { kind: 'score', status: 409, reason: 'This match has already finished.', sentence: true };

const EDITORS = [
  ['individual', bout()],
  ['engi', engiBout()],
  ['team (fixed order)', teamBout()],
  ['team (kachinuki End match)', kachinukiBout()],
];

describe.each(EDITORS)('%s editor', (_name, match) => {
  it('takes and shows a refusal of its own match', async () => {
    let c;
    await act(async () => {
      ({ container: c } = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />));
    });
    expect(await raise({ ...REFUSAL, compID: match.compId, matchID: match.id })).toBe(true);
    expect(banner(c)?.textContent).toContain('This match has already finished.');
  });

  it('takes nothing for another match, and nothing once it is gone', async () => {
    let c;
    let unmount;
    await act(async () => {
      ({ container: c, unmount } = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />));
    });
    expect(await raise({ ...REFUSAL, compID: match.compId, matchID: 'another' })).toBe(false);
    expect(banner(c)).toBeNull();
    await act(async () => { unmount(); });
    expect(await raise({ ...REFUSAL, compID: match.compId, matchID: match.id })).toBe(false);
  });
});
