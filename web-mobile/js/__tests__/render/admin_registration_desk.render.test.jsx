import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';

// RENDER-SMOKE for the Registration desk (mp-25bk). The unit suite calls the
// pure helpers directly but never MOUNTS the page, so a window.* dep that's
// captured at module-eval (the `const X = window.X` block) or read at render
// time would slip through. This harness mounts with REAL React 18 + jsdom and
// FAILS on any console.warn/error, so a missing global throws here. The
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

// bc-pnum: the roster row's check-in control used to send rdPid(player) --
// id when present, else the "name|dojo" composite -- to toggleCheckIn/
// bulkCheckIn. Name and dojo are operator-editable after the draw, so that
// composite is not a safe wire identifier. It must send the id ONLY
// (rdApiPid); an id-less row has no safe wire identifier at all and the
// write is left to the server to refuse.
describe('AdminRegistrationDeskPage check-in sends the id-only wire pid (bc-pnum)', () => {
  let toggleCheckIn;
  let savedFetchCompetitions;

  beforeEach(() => {
    toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API.toggleCheckIn = toggleCheckIn;
    savedFetchCompetitions = window.API.fetchCompetitions;
  });

  afterEach(() => {
    delete window.API.toggleCheckIn;
    window.API.fetchCompetitions = savedFetchCompetitions;
  });

  it('sends the real id, not the name|dojo composite, for a stamped row', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: 'uuid-akira', name: 'Akira Tanaka', dojo: 'Gyokusen', checkedIn: false }],
      }],
    });
    const { getByRole } = await mount(tournament);
    const checkbox = getByRole('checkbox', { name: /check in akira tanaka/i });
    await act(async () => { fireEvent.click(checkbox); });
    expect(toggleCheckIn).toHaveBeenCalledWith('men', 'uuid-akira', true, 'pw');
  });

  it('sends "" (never the name|dojo composite) for an id-less legacy row', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { getByRole } = await mount(tournament);
    const checkbox = getByRole('checkbox', { name: /check in kenji sato/i });
    await act(async () => { fireEvent.click(checkbox); });
    expect(toggleCheckIn).toHaveBeenCalledWith('men', '', true, 'pw');
    expect(toggleCheckIn).not.toHaveBeenCalledWith('men', 'Kenji Sato|Mumeishi', true, 'pw');
  });

  // setLocal keys its optimistic update on rdApiPid's output (id-only), so
  // an id-less write's pid is "". Without the `pid &&` guard, EVERY id-less
  // row would match "" and get optimistically flipped together. The
  // post-write `refresh()` re-fetches from the (mocked) server and would
  // paper over the transient optimistic state either way, so this test
  // makes refresh fail (console.warn expected and suppressed) to observe
  // exactly what setLocal itself left behind.
  it('does not optimistically flip every id-less row when one is clicked', async () => {
    // Local spy replaces the outer beforeEach spy for this test's duration
    // (same pattern as admin_shiaijo.render.test.jsx's intentional-throw
    // test): mockRestore() before the test ends clears its recorded calls
    // so the outer afterEach's fail-on-console.warn guard sees none.
    const localWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    window.API.fetchCompetitions = vi.fn().mockRejectedValue(new Error('refresh disabled for this test'));
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [
          { id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false },
          { id: '', name: 'Yuki Ito', dojo: 'Tora', checkedIn: false },
        ],
      }],
    });
    try {
      const { getByRole } = await mount(tournament);
      const checkboxKenji = getByRole('checkbox', { name: /check in kenji sato/i });
      await act(async () => { fireEvent.click(checkboxKenji); });
      const checkboxYuki = getByRole('checkbox', { name: /check in yuki ito/i });
      expect(checkboxYuki.getAttribute('aria-checked')).toBe('false');
    } finally {
      localWarn.mockRestore();
    }
  });
});
