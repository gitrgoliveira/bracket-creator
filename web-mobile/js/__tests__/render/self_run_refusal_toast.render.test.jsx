// F1: the public score sheet tells a refused result through the outbox's one
// rule (_tellRefusal): the editor open on the match says it, else an error
// toast does. It never raises a window.alert, and it claims no refusal of its
// own (only the Start refusal the editor says itself is passed through).
//
// The real outbox is driven here: a Finish is answered pending at once and
// closes the editor, then the server refuses it with a real 409
// `result_finalized` body (a participant's write over a finished match,
// handlers_match_refusals.go). The queue alert is turned into a toast as
// app.jsx does it, so the message is the one a participant reads. The words are
// the server's own sentence: the toast ends on it, with no "Re-enter it."
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const SENTENCE = 'This match result has already been reported. Contact the tournament organizer to correct it.';

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return <div data-testid="probe-score-editor" />;
}

let restoreGlobals;
let MatchViewerModal, toast, app, mod, unsubscribe;
let alertSpy;

const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

beforeEach(async () => {
  vi.useFakeTimers();
  vi.resetModules();
  window.scrollTo = vi.fn();
  Element.prototype.scrollIntoView = vi.fn();
  restoreGlobals = installWindowStubs({
    ScoreEditorModal: ProbeScoreEditor,
    confirmDialog: vi.fn().mockResolvedValue(true),
    Term: ({ children }) => <span>{children}</span>,
    GlossaryHint: ({ name }) => <span title={name} />,
  });
  global.EventSource = class { constructor() { this.close = () => {}; } };
  global.EventSource.OPEN = 1;
  global.fetch = vi.fn((url) => {
    if (String(url).includes('/api/time')) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ nowMs: Date.now() }) });
    // Only the score write is answered; every other request (the App's own
    // loads, which app.jsx starts as it mounts) stays pending.
    if (!String(url).includes('/score')) return new Promise(() => {});
    return Promise.resolve({
      ok: false,
      status: 409,
      json: () => Promise.resolve({ error: 'result_finalized', message: SENTENCE }),
    });
  });
  // app.jsx mounts the whole App into #root as it loads; only its exported
  // queue-alert wording is used here, and the App draws into a detached node.
  const root = document.createElement('div');
  root.id = 'root';
  document.body.appendChild(root);
  mod = await import('../../api_client.jsx');
  app = await import('../../app.jsx');
  toast = await import('../../toast_queue.jsx');
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
  // The one subscriber app.jsx has: a queue alert becomes a toast.
  unsubscribe = mod.subscribeQueueAlert((alert) => {
    const message = app.queueAlertMessage(alert);
    if (message) toast.pushToast(message, app.queueAlertToastType(alert), app.queueAlertToastOptions(alert));
  });
  alertSpy = vi.spyOn(window, 'alert').mockImplementation(() => {});
  probe.props = null;
});

afterEach(() => {
  unsubscribe && unsubscribe();
  if (mod && mod.API && typeof mod.API.clearQueue === 'function') mod.API.clearQueue();
  if (toast) toast.resetToastQueue();
  document.getElementById('root')?.remove();
  vi.clearAllTimers();
  vi.useRealTimers();
  alertSpy.mockRestore();
  restoreGlobals();
  vi.resetModules();
});

const match = () => ({
  id: 'm1', compId: 'c1', compName: 'Comp', status: 'running',
  phase: 'bracket', matchNumber: 2, court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
});

async function openEditor(onClose = vi.fn()) {
  await act(async () => {
    render(<MatchViewerModal match={match()} onClose={onClose} tournament={{ mode: 'self-run' }} compId="c1" />);
  });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
  return onClose;
}

describe('a refused result on the public score sheet', () => {
  it('reaches an error toast once, with the server\'s whole sentence, and no window.alert', async () => {
    const logWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    // app.jsx's App mounts as it loads and settles outside act(); not this test's subject.
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onClose = await openEditor();
    await act(async () => {
      await probe.props.onSubmit({ status: 'completed', winner: 'Yamada', ipponsA: ['M', 'K'], ipponsB: [] });
    });
    expect(onClose, 'a Finish closes the sheet at once').toHaveBeenCalled();
    await act(async () => { await vi.advanceTimersByTimeAsync(50); await flush(); });

    const shown = toast.toastSnapshot();
    expect(shown, 'the refusal is told').not.toBeNull();
    expect(shown.type).toBe('error');
    expect(shown.message, 'the match is named and the server\'s own sentence closes it').toBe(
      `A result in Match 2 was refused by the server and cannot be saved. ${SENTENCE}`);
    expect(toast.queuedToastCount(), 'told once').toBe(0);
    expect(alertSpy, 'no window.alert').not.toHaveBeenCalled();
    logWarn.mockRestore();
    logError.mockRestore();
  });

  it('a direct failure of the write is an error toast, never a window.alert', async () => {
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    window.API.recordScore = vi.fn(async () => { throw new Error(SENTENCE); });
    await openEditor();
    await act(async () => { await probe.props.onSubmit({ status: 'running', ipponsA: ['M'] }); });
    expect(toast.toastSnapshot()).toMatchObject({ message: SENTENCE, type: 'error' });
    expect(alertSpy).not.toHaveBeenCalled();
    logError.mockRestore();
  });
});
