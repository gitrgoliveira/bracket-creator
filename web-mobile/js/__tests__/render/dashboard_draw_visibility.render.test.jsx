import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { startHiddenDrawMessage, startAllHiddenDrawsNote } from '../../draw_visibility.jsx';
import { partitionStartableCompetitions } from '../../admin_helpers.jsx';

// bc-drpb: the dashboard card says who can see a draft's draw (FR16) and its
// Start competition asks first on a hidden draft (FR9); Start all names the
// hidden draws it makes public (FR10). Mounted with REAL
// React and the real draw_visibility.jsx; only the confirm dialog is stubbed,
// so each test decides what the operator answers.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const confirmDialog = vi.fn();

const STUBBED_GLOBALS = {
  competitionKindLabel: () => 'Individual',
  linkBase: () => 'http://localhost',
  StatusBadge: Stub('StatusBadge'),
  confirmDialog,
  API: {
    fetchTournament: vi.fn().mockResolvedValue(null),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    startCompetition: vi.fn().mockResolvedValue(null),
  },
};

let restoreGlobals;
let CompCard;
let StartAllModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shell.jsx');
  CompCard = window.CompCard;
  ({ StartAllModal } = await import('../../admin.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => confirmDialog.mockReset());

const OFFICIATED = { mode: 'officiated', courts: ['A', 'B'] };
const SELF_RUN = { mode: 'self-run', courts: ['A', 'B'] };

const makeComp = (over = {}) => ({
  id: 'c1',
  name: 'Mudansha',
  status: 'draw-ready',
  format: 'mixed',
  kind: 'individual',
  courts: ['A', 'B'],
  players: [{ id: 'p1', name: 'Yamada' }, { id: 'p2', name: 'Tanaka' }],
  ...over,
});

async function mountCard({ c = makeComp(), tournament = OFFICIATED, onStart = vi.fn(), onOpen = noop, onSetDrawVisibility = vi.fn().mockResolvedValue(undefined) } = {}) {
  let result;
  await act(async () => {
    result = render(
      <CompCard c={c} onOpen={onOpen} onStart={onStart} onSetDrawVisibility={onSetDrawVisibility} tournament={tournament} showToast={noop} />
    );
  });
  return { ...result, onStart, onSetDrawVisibility };
}

const line = (container) => container.querySelector('[data-testid="card-draw-visibility"]');
const startButton = (container) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Start competition');

describe('Dashboard card: who can see the draw (FR16)', () => {
  it('a hidden draft shows the crossed-out eye and "Hidden from the public"', async () => {
    const { container } = await mountCard({ c: makeComp({ drawHidden: true }) });
    const el = line(container);
    expect(el).not.toBeNull();
    expect(el.textContent).toBe('Hidden from the public');
    expect(el.querySelector('svg').getAttribute('data-icon')).toBe('eye-off');
    expect(el.classList.contains('draw-visibility-line--hidden')).toBe(true);
  });

  it('a published draft shows the open eye and "Public"', async () => {
    const { container } = await mountCard({ c: makeComp() });
    const el = line(container);
    expect(el.textContent).toBe('Public');
    expect(el.querySelector('svg').getAttribute('data-icon')).toBe('eye');
    expect(el.classList.contains('draw-visibility-line--public')).toBe(true);
  });

  it.each(['setup', 'pools', 'knockout', 'completed'])('a competition in %s shows no line', async (status) => {
    const { container } = await mountCard({ c: makeComp({ status, drawHidden: true }) });
    expect(line(container)).toBeNull();
  });

  it('a self-run tournament shows no line, even for a draft stored as hidden', async () => {
    const { container } = await mountCard({ c: makeComp({ drawHidden: true }), tournament: SELF_RUN });
    expect(line(container)).toBeNull();
  });
});

const visibilityToggle = (container) => container.querySelector('[data-testid="card-draw-visibility-toggle"]');

describe('Dashboard card: Publish draw and Unpublish (round critique-2 D4)', () => {
  it('a hidden draft offers Publish draw, which asks the host to publish without opening the card', async () => {
    const onOpen = vi.fn();
    const { container, onSetDrawVisibility } = await mountCard({ c: makeComp({ drawHidden: true }), onOpen });
    const btn = visibilityToggle(container);
    expect(btn.textContent).toBe('Publish draw');
    expect(btn.querySelector('svg').getAttribute('data-icon')).toBe('eye');
    await act(async () => { fireEvent.click(btn); });
    expect(onSetDrawVisibility).toHaveBeenCalledWith(true);
    expect(onOpen).not.toHaveBeenCalled();
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('a published draft offers Unpublish, which asks the host to hide it', async () => {
    const { container, onSetDrawVisibility } = await mountCard({ c: makeComp() });
    const btn = visibilityToggle(container);
    expect(btn.textContent).toBe('Unpublish');
    await act(async () => { fireEvent.click(btn); });
    expect(onSetDrawVisibility).toHaveBeenCalledWith(false);
  });

  // Busy is aria-disabled, never disabled: a disabled button drops the keyboard
  // focus it holds to the page body (audit of PR #525).
  it('the button stays busy, and keeps focus, until the host has answered; a second tap sends nothing', async () => {
    let answer;
    const onSetDrawVisibility = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    const { container } = await mountCard({ c: makeComp({ drawHidden: true }), onSetDrawVisibility });
    visibilityToggle(container).focus();
    await act(async () => { fireEvent.click(visibilityToggle(container)); });
    expect(visibilityToggle(container).getAttribute('aria-disabled')).toBe('true');
    expect(visibilityToggle(container).disabled).toBe(false);
    expect(visibilityToggle(container).textContent).toBe('Publishing…');
    expect(document.activeElement).toBe(visibilityToggle(container));
    await act(async () => { fireEvent.click(visibilityToggle(container)); });
    expect(onSetDrawVisibility).toHaveBeenCalledTimes(1);
    await act(async () => { answer(); });
    expect(visibilityToggle(container).hasAttribute('aria-disabled')).toBe(false);
    expect(document.activeElement).toBe(visibilityToggle(container));
  });

  it.each(['setup', 'pools', 'knockout', 'completed'])('a competition in %s offers no Publish draw', async (status) => {
    const { container } = await mountCard({ c: makeComp({ status, drawHidden: true }) });
    expect(visibilityToggle(container)).toBeNull();
  });

  it('a self-run tournament offers no Publish draw', async () => {
    const { container } = await mountCard({ c: makeComp({ drawHidden: true }), tournament: SELF_RUN });
    expect(visibilityToggle(container)).toBeNull();
  });
});

describe('Dashboard card: Start on a hidden draft asks first (FR9)', () => {
  it('asks, and Cancel leaves the draft not started', async () => {
    confirmDialog.mockResolvedValue(false);
    const { container, onStart } = await mountCard({ c: makeComp({ drawHidden: true }) });
    await act(async () => { fireEvent.click(startButton(container)); });
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    const opts = confirmDialog.mock.calls[0][0];
    expect(opts.title).toBe('Start Mudansha?');
    expect(opts.message).toBe(startHiddenDrawMessage('Mudansha'));
    expect(opts.confirmLabel).toBe('Start competition');
    expect(opts.cancelLabel).toBe('Cancel');
    expect(onStart).not.toHaveBeenCalled();
  });

  it('asks, and Start competition starts it once', async () => {
    confirmDialog.mockResolvedValue(true);
    const { container, onStart } = await mountCard({ c: makeComp({ drawHidden: true }) });
    await act(async () => { fireEvent.click(startButton(container)); });
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(onStart).toHaveBeenCalledTimes(1);
  });

  it('a published draft starts at once, in the same tap, with no question', async () => {
    const { container, onStart } = await mountCard({ c: makeComp() });
    fireEvent.click(startButton(container));
    expect(onStart).toHaveBeenCalledTimes(1);
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('a self-run draft starts at once: its draw is already public', async () => {
    const { container, onStart } = await mountCard({ c: makeComp({ drawHidden: true }), tournament: SELF_RUN });
    fireEvent.click(startButton(container));
    expect(onStart).toHaveBeenCalledTimes(1);
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('a setup competition starts at once: it has no draw to hide yet', async () => {
    const { container, onStart } = await mountCard({ c: makeComp({ status: 'setup' }) });
    fireEvent.click(startButton(container));
    expect(onStart).toHaveBeenCalledTimes(1);
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('the Start tap does not also open the competition', async () => {
    confirmDialog.mockResolvedValue(true);
    const onOpen = vi.fn();
    let container;
    await act(async () => {
      ({ container } = render(
        <CompCard c={makeComp({ drawHidden: true })} onOpen={onOpen} onStart={vi.fn()} tournament={OFFICIATED} showToast={noop} />
      ));
    });
    await act(async () => { fireEvent.click(startButton(container)); });
    expect(onOpen).not.toHaveBeenCalled();
  });
});

describe('Start all: names the hidden draws it makes public (FR10)', () => {
  // The modal is handed what the dashboard's Start all hands it: the
  // partition of the tournament's competitions, so a draw-ready competition
  // reaches it only if the app really offers one.
  async function mountStartAll({ comps, tournament = OFFICIATED, phase = 'confirm' }) {
    const { startable, blocked } = partitionStartableCompetitions(comps, tournament.courts);
    let result;
    await act(async () => {
      result = render(
        <StartAllModal
          state={{ phase, comps: startable, blocked, failed: [] }}
          tournament={tournament}
          onConfirm={noop}
          onRetry={noop}
          onClose={noop}
        />
      );
    });
    return result;
  }
  const note = () => document.body.querySelector('[data-testid="start-all-hidden-draws"]');

  const hiddenA = makeComp({ id: 'a', name: 'Mudansha', drawHidden: true });
  const hiddenB = makeComp({ id: 'b', name: 'Kyu grades', drawHidden: true });
  const published = makeComp({ id: 'p', name: 'Open' });
  const setup = makeComp({ id: 's', name: 'Juniors', status: 'setup' });

  it('names every hidden draft it would start, in list order, and no other', async () => {
    await mountStartAll({ comps: [hiddenA, published, setup, hiddenB] });
    expect(note()).not.toBeNull();
    expect(note().textContent).toBe('Starting makes these draws visible to the public: Mudansha, Kyu grades.');
    expect(note().textContent).toBe(startAllHiddenDrawsNote(OFFICIATED, [hiddenA, published, setup, hiddenB]));
  });

  it('lists the drawn competitions with the one in setup, as the dashboard hands them over', async () => {
    await mountStartAll({ comps: [hiddenA, published, setup, hiddenB] });
    const names = [...document.body.querySelectorAll('.start-all__list li')].map((li) => li.textContent);
    expect(names).toEqual(['Mudansha', 'Open', 'Juniors', 'Kyu grades']);
  });

  it('says nothing when no competition it starts has a hidden draw', async () => {
    await mountStartAll({ comps: [published, setup] });
    expect(note()).toBeNull();
  });

  it('says nothing in a self-run tournament, whose draws are already public', async () => {
    await mountStartAll({ comps: [hiddenA, hiddenB], tournament: SELF_RUN });
    expect(note()).toBeNull();
  });

  it('is shown only while the operator is asked, not once the starts have gone', async () => {
    await mountStartAll({ comps: [hiddenA], phase: 'result' });
    expect(note()).toBeNull();
  });
});
