import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// spec 007 R9: two surfaces bypassed the shiaijo-count blocker.
//
// The competition header derived `courtsPairingErr` and disabled its
// Generate/Start buttons. The dashboard competition card's "Start competition
// →" and the tournament-level "Start all" picker did not: both fired a request
// the server refuses with a 400 whose only trace is a toast that expires. "Start
// all" even offered a competition it could not start, so the operator got a
// guaranteed entry in the failure list carrying a raw API message.
//
// All three now read ONE derived value (competitionDrawBlockedReason /
// partitionStartableCompetitions, admin_helpers.jsx). These tests mount the two
// bypass surfaces with REAL React.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  // viewer.jsx publishes these in the browser; it is not loaded in the render
  // harness. Everything else CompCard/StartAllModal use (pluralize, Modal,
  // StatusBadge, formatDate, formatLabelShort, Icon, compMatchStats and the
  // court helpers) is the real implementation, loaded by vitest.setup.render.js.
  competitionKindLabel: () => 'Individual',
  linkBase: () => 'http://localhost',
  StatusBadge: Stub('StatusBadge'),
  API: {
    fetchTournament: vi.fn().mockResolvedValue(null),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    startCompetition: vi.fn().mockResolvedValue(null),
  },
};

const originals = {};
let CompCard;
let StartAllModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shell.jsx');
  CompCard = window.CompCard;
  ({ StartAllModal } = await import('../../admin.jsx'));
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

const makeComp = (over = {}) => ({
  id: 'c1',
  name: 'Mudansha',
  status: 'setup',
  format: 'mixed',
  kind: 'individual',
  courts: ['A', 'B'],
  startTime: '09:00',
  players: [{ id: 'p1', name: 'Yamada' }, { id: 'p2', name: 'Tanaka' }],
  ...over,
});

async function mountCard({ c = makeComp(), tournament = { courts: ['A', 'B', 'C'] }, onStart = vi.fn() } = {}) {
  let result;
  await act(async () => {
    result = render(
      <CompCard c={c} onOpen={noop} onStart={onStart} tournament={tournament} showToast={noop} />
    );
  });
  return { ...result, onStart };
}

const startButton = (container) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Start competition →');

describe('Dashboard CompCard start blocker (spec 007 R9)', () => {
  it('offers the start when the allocation is valid', async () => {
    const { container } = await mountCard();
    expect(startButton(container).disabled).toBe(false);
    expect(container.querySelector('[data-testid="card-shiaijo-count-block"]')).toBeNull();
  });

  it('blocks the start on a 3-shiaijo allocation and names the fix', async () => {
    const { container, onStart } = await mountCard({ c: makeComp({ courts: ['A', 'B', 'C'] }) });
    const btn = startButton(container);
    expect(btn.disabled).toBe(true);
    await act(async () => { fireEvent.click(btn); });
    expect(onStart).not.toHaveBeenCalled();

    const block = container.querySelector('[data-testid="card-shiaijo-count-block"]');
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('3 shiaijo cannot be paired down to a single bracket');
    // The card renders the reason ALONE - no venue-aware hint beneath it - so
    // on this 3-shiaijo venue it must not offer a 4th shiaijo that does not
    // exist. mountCard's default tournament is exactly that venue.
    expect(block.textContent).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(block.textContent).not.toContain('4');
    expect(block.textContent).toContain('reassign shiaijo in Settings');
  });

  it('still offers the count above once the venue can supply it', async () => {
    // Same allocation, bigger hall: 4 is reachable, so naming it is right.
    const venue = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P'];
    const { container } = await mountCard({
      c: makeComp({ courts: ['A', 'B', 'C'] }), tournament: { courts: venue },
    });
    const block = container.querySelector('[data-testid="card-shiaijo-count-block"]');
    expect(block.textContent).toContain('Use 2 or 4, or 1');
  });

  it('blocks a 6-shiaijo allocation, which the previous parity rule allowed', async () => {
    const courts = ['A', 'B', 'C', 'D', 'E', 'F'];
    const { container } = await mountCard({ c: makeComp({ courts }), tournament: { courts } });
    expect(startButton(container).disabled).toBe(true);
  });

  it('blocks a shiaijo the tournament no longer has', async () => {
    const { container } = await mountCard({ c: makeComp({ courts: ['A', 'D'] }) });
    expect(startButton(container).disabled).toBe(true);
    expect(container.querySelector('[data-testid="card-shiaijo-count-block"]').textContent)
      .toContain('no longer part of this tournament');
  });

  it('leaves a league card alone: the count rule does not govern it', async () => {
    const { container } = await mountCard({ c: makeComp({ format: 'league', courts: ['A', 'B', 'C'] }) });
    expect(startButton(container).disabled).toBe(false);
  });

  it('leaves a DRAW-READY competition startable: its start is a status flip', async () => {
    // The draw already exists, so the server accepts the start. Blocking here
    // would be a new refusal the backend does not make.
    const { container } = await mountCard({ c: makeComp({ status: 'draw-ready', courts: ['A', 'B', 'C'] }) });
    expect(startButton(container).disabled).toBe(false);
    expect(container.querySelector('[data-testid="card-shiaijo-count-block"]')).toBeNull();
  });
});

async function mountStartAll(state, { onConfirm = vi.fn() } = {}) {
  let result;
  await act(async () => {
    result = render(
      <StartAllModal state={state} onConfirm={onConfirm} onRetry={noop} onClose={noop} />
    );
  });
  return { ...result, onConfirm };
}

const confirmButton = (container) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim().startsWith('Start '));

describe('Start-all picker blocker (spec 007 R9)', () => {
  const blocked = (name, reason) => ({ comp: { id: name, name }, reason });

  it('counts only the startable competitions in the confirm button', async () => {
    const { container } = await mountStartAll({
      phase: 'confirm',
      comps: [{ id: 'c1', name: 'Mudansha' }],
      blocked: [blocked('Yudansha', '3 shiaijo cannot be paired down to a single bracket.')],
      failed: [],
    });
    expect(confirmButton(container).textContent).toContain('Start 1 competition');
    expect(confirmButton(container).disabled).toBe(false);
  });

  it('names the competitions it will NOT start, with the reason', async () => {
    const { container } = await mountStartAll({
      phase: 'confirm',
      comps: [{ id: 'c1', name: 'Mudansha' }],
      blocked: [blocked('Yudansha', '3 shiaijo cannot be paired down to a single bracket.')],
      failed: [],
    });
    const panel = container.querySelector('[data-testid="start-all-blocked"]');
    expect(panel).not.toBeNull();
    expect(panel.textContent).toContain('Yudansha');
    expect(panel.textContent).toContain('3 shiaijo cannot be paired down to a single bracket');
    expect(panel.textContent).toContain('Reassign shiaijo in its Settings tab');
    // The blocked competition is not also listed as startable.
    expect(container.querySelector('.start-all__list').textContent).not.toContain('Yudansha');
  });

  it('cannot be confirmed when every eligible competition is blocked', async () => {
    const { container, onConfirm } = await mountStartAll({
      phase: 'confirm',
      comps: [],
      blocked: [blocked('Yudansha', '3 shiaijo cannot be paired down to a single bracket.')],
      failed: [],
    });
    const btn = confirmButton(container);
    expect(btn.disabled).toBe(true);
    await act(async () => { fireEvent.click(btn); });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(container.textContent).toContain('Nothing can be started yet.');
  });

  it('says nothing extra when no competition is blocked', async () => {
    const { container } = await mountStartAll({
      phase: 'confirm',
      comps: [{ id: 'c1', name: 'Mudansha' }],
      blocked: [],
      failed: [],
    });
    expect(container.querySelector('[data-testid="start-all-blocked"]')).toBeNull();
  });
});
