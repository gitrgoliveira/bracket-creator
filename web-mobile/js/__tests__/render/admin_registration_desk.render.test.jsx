import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// RENDER-SMOKE for the Registration desk (mp-25bk). The unit suite calls the
// pure helpers directly but never MOUNTS the page, so a window.* dep that's
// captured at module-eval (the `const X = window.X` block) or read at render
// time would slip through. This harness mounts with REAL React 18 + jsdom and
// FAILS on any console.warn/error, so a missing global throws here — the
// load-order class of breakage the default stub can't catch.
//
// All window.* deps captured at admin_registration_desk.jsx module load
// (AdminTopbar / Breadcrumbs / StatusBadge / pluralize) MUST be set before the
// dynamic import, or the captured const is undefined and the page throws.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  StatusBadge: Stub('StatusBadge'),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  // The page opens its own SSE subscription on mount; return a no-op unsub.
  API: {
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
  },
};

const originals = {};
let AdminRegistrationDeskPage;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_registration_desk.jsx');
  AdminRegistrationDeskPage = window.AdminRegistrationDeskPage;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeTournament(overrides = {}) {
  return {
    name: 'Spring Taikai',
    competitions: [
      {
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        withZekkenName: true, checkInEnabled: true,
        players: [
          { id: 'p1', name: 'Akira Tanaka', displayName: 'TANAKA', dojo: 'Gyokusen', number: 'M1', checkedIn: true },
          { id: 'p2', name: 'Kenji Sato', dojo: 'Mumeishi', number: 'M2', checkedIn: false },
        ],
      },
      {
        id: 'team', name: 'Team Championship', kind: 'team', status: 'setup',
        checkInEnabled: true,
        players: [{ id: 't1', name: 'Tora A', dojo: 'Tora Dojo', checkedIn: false }],
      },
    ],
    ...overrides,
  };
}

async function mount(tournament) {
  let result;
  await act(async () => {
    result = render(
      <AdminRegistrationDeskPage
        tournament={tournament}
        onBack={noop}
        password="pw"
        showToast={noop}
        onUpdate={noop}
        onLogout={noop}
        onViewerMode={noop}
      />
    );
  });
  return result;
}

describe('AdminRegistrationDeskPage render-smoke', () => {
  it('mounts the populated desk without console errors', async () => {
    const { container, getByText, unmount } = await mount(makeTournament());
    expect(getByText('Registration desk')).toBeTruthy();
    // Rail renders the pinned "All competitions" entry + one item per competition.
    expect(container.querySelector('.rd-rail')).toBeTruthy();
    expect(getByText('All competitions')).toBeTruthy();
    // The roster mounts with rows in the default "all" view.
    expect(container.querySelector('.rd-row')).toBeTruthy();
    unmount();
  });

  it('mounts the empty (no competitions) state', async () => {
    const { getByText, unmount } = await mount(makeTournament({ competitions: [] }));
    expect(getByText('No competitions yet')).toBeTruthy();
    unmount();
  });
});
