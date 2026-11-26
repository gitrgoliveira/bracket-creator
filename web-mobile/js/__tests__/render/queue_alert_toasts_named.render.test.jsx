// The walk's finding S7 reaches the alerts the outbox raised before App
// mounted too: the queue rehydrates at module load, so the channel replays
// them the moment App subscribes, while the tournament is still unread and the
// competitions are not known. They wait for that read, then name their
// competition like every later alert; with nothing to wait for they toast at
// once.
import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let useQueueAlertToasts;
let toastSnapshot;
let resetToastQueue;
let restoreGlobals;
let emit;

beforeAll(async () => {
  let listener = null;
  const buffered = [];
  emit = (alert) => { if (listener) listener(alert); else buffered.push(alert); };
  restoreGlobals = installWindowStubs({
    ReactDOM: { createRoot: () => ({ render() {} }) },
    // The real channel's contract: alerts raised before a subscriber exists
    // are replayed to it on subscribe.
    subscribeQueueAlert: (fn) => { listener = fn; buffered.splice(0).forEach(fn); return () => { listener = null; }; },
  });
  ({ useQueueAlertToasts } = await import('../../app.jsx'));
  ({ toastSnapshot, resetToastQueue } = await import('../../toast_queue.jsx'));
});

afterAll(() => restoreGlobals());
beforeEach(() => resetToastQueue());
afterEach(() => cleanup());

const refused = () => ({
  kind: 'rejected', count: 1, terminalCount: 1, compID: 'c1', matchID: 'Pool A-0',
  where: 'Pool A · Match 1', sentence: true, detail: 'It was already reported.',
});
const toastText = () => {
  const snap = toastSnapshot();
  return JSON.stringify(snap);
};

function Probe({ tournament }) {
  useQueueAlertToasts(tournament);
  return null;
}

describe('an alert replayed before the tournament was read', () => {
  it('waits for the read, then names its competition', async () => {
    emit(refused());
    let view;
    await act(async () => { view = render(<Probe tournament={undefined} />); });
    expect(toastText()).not.toContain('Pool A');
    await act(async () => {
      view.rerender(<Probe tournament={{ competitions: [{ id: 'c1', name: 'Spring Cup' }] }} />);
    });
    expect(toastText()).toContain('Spring Cup · Pool A · Match 1');
  });

  it('is told unnamed once the read finds no tournament', async () => {
    emit(refused());
    let view;
    await act(async () => { view = render(<Probe tournament={undefined} />); });
    await act(async () => { view.rerender(<Probe tournament={null} />); });
    expect(toastText()).toContain('Pool A · Match 1');
    expect(toastText()).not.toContain('Spring Cup');
  });

  it('an alert raised after the read is told at once, named', async () => {
    await act(async () => {
      render(<Probe tournament={{ competitions: [{ id: 'c1', name: 'Spring Cup' }] }} />);
    });
    await act(async () => { emit(refused()); });
    expect(toastText()).toContain('Spring Cup · Pool A · Match 1');
  });
});
