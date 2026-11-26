// bc-sseq FR9: a court console another device runs is view only. A scoring tap
// is fire and forget (recordTap returns before anything is sent, and the editor
// reads nothing from it), so the write gate refuses it before it is queued
// (_placeWrite) and tells the editor through the tap-outcome channel: the board,
// which is the server's copy plus this tab's pending taps, stays as it was, and
// the court's sentence shows beside it. Pinned for a tap by touch and by the
// keyboard shortcut, with the real outbox; the three editor files are unchanged.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installIndividualEditor, bout, letterBtn, shiroSide, marks } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
let cc;
let control;
let saved;

const OTHER = { court: 'A', holderKey: 'bbbb', device: 'windows', youHold: false, youWereDisplaced: false, sameDevice: false, since: 1, lastActiveAt: 2 };
const HOLDS = { court: 'A', holderKey: 'aaaa', device: 'ipad', youHold: true, youWereDisplaced: false, sameDevice: true, since: 1, lastActiveAt: 2 };

beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());

beforeEach(async () => {
  saved = { fetch: global.fetch, ES: global.EventSource, BC: global.BroadcastChannel, API: window.API };
  global.BroadcastChannel = undefined;
  global.EventSource = class { constructor() { this.readyState = 0; this.close = () => {}; } };
  global.EventSource.OPEN = 1;
  localStorage.clear();
  sessionStorage.clear();
  control = OTHER;
  global.fetch = vi.fn((url) => {
    if (String(url).includes('/api/courts/')) return Promise.resolve({ ok: true, status: 200, json: async () => control });
    if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
    return new Promise(() => {});
  });
  vi.resetModules();
  const { API } = await import('../../api_client.jsx');
  cc = await import('../../court_control.jsx');
  // The real outbox, with the editor's one read of the competition answered.
  window.API = { ...API, fetchCompetitionDetails: vi.fn().mockResolvedValue({ config: {} }) };
});

afterEach(() => {
  global.fetch = saved.fetch;
  global.EventSource = saved.ES;
  global.BroadcastChannel = saved.BC;
  window.API = saved.API;
});

// The court console's control of shiaijo A, beside its inline editor.
function Console({ match }) {
  const control = cc.useCourtControl('A', 'pw');
  return (
    <div>
      <div data-testid="control">{control.state}</div>
      <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />
    </div>
  );
}

async function open() {
  let r;
  await act(async () => { r = render(<Console match={bout()} />); });
  await act(async () => { for (let i = 0; i < 5; i++) await new Promise((res) => setTimeout(res, 0)); });
  return r;
}

const refusal = (c) => c.querySelector('[data-testid="tap-refusal"]');

describe('a tap on a console another device runs', () => {
  it('by touch: the board is unchanged and the court note shows', async () => {
    const r = await open();
    expect(r.getByTestId('control').textContent).toBe('other');
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(marks(r.container)).toEqual([]);
    expect(refusal(r.container).textContent).toMatch(/^Not applied: this tablet does not run this shiaijo/);
    expect(window.API.pendingTaps('c1', 'm1')).toEqual([]);
    r.unmount();
  });

  it('by keyboard shortcut: the board is unchanged and the court note shows', async () => {
    const r = await open();
    await act(async () => { fireEvent.keyDown(window, { key: 'm' }); });
    expect(marks(r.container)).toEqual([]);
    expect(refusal(r.container).textContent).toMatch(/^Not applied: this tablet does not run this shiaijo/);
    r.unmount();
  });

  it('the same tap on a console that runs the shiaijo shows on the board, with no note', async () => {
    control = HOLDS;
    const r = await open();
    expect(r.getByTestId('control').textContent).toBe('holds');
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(marks(r.container)).toHaveLength(1);
    expect(refusal(r.container)).toBeNull();
    r.unmount();
  });
});
