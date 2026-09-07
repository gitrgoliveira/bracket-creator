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
// (rdApiPid). an id-less row has no safe wire
// identifier at all, and checkPersonEntries now skips it client-side
// (reporting the skip via toast) rather than sending a write the server
// can only 404.
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

  // checkPersonEntries now filters an id-less entry
  // out of its own `targets` before sending anything, rather than sending
  // "" and letting the server 404 it.
  it('sends no request (never "" or the name|dojo composite) for an id-less legacy row, and toasts the skip', async () => {
    const showToast = vi.fn();
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    let result;
    await act(async () => {
      result = render(
        <AdminRegistrationDeskPage
          tournament={tournament}
          onBack={noop}
          password="pw"
          showToast={showToast}
          onUpdate={noop}
          onLogout={noop}
          onViewerMode={noop}
        />
      );
    });
    const checkbox = result.getByRole('checkbox', { name: /check in kenji sato/i });
    await act(async () => { fireEvent.click(checkbox); });
    expect(toggleCheckIn).not.toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith(expect.stringContaining('no id'), 'error');
  });

  // setLocal keys its optimistic update on rdApiPid's output (id-only), so
  // a pid of "" must never match every id-less row at once -- the `pid &&`
  // guard in setLocal defends that for callers that still forward one
  // (toggleOne, bulkDojoComp). checkPersonEntries no
  // longer reaches setLocal for an id-less entry at all (it is filtered out
  // of `targets` first), so this scenario is now doubly defended; the test
  // stays as a regression guard on setLocal's own `pid &&` check. The
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

// bc-pnum (item 4): an id-less row's rdApiPid is "", so a
// check-in / chip / edit-save write for it can only 404 (empty id segment
// matches no route, or matches nothing on the server). All three controls
// are disabled client-side for such a row instead, each with a hint mirroring
// helper.MissingParticipantIDsMessage's remedy sentence. Unlike the "all"
// mode checkbox exercised above, these three controls only appear/apply in
// "comp" mode (a single competition selected via the rail), where `player`
// unambiguously names one participant record.
//
// Enters "comp" mode for the FIRST real competition in the rail (index 0 is
// always the pinned "All competitions" entry, per RdRail). Clicking by text
// is ambiguous here: in "all" mode a person entered in only one competition
// still gets a self-referential "In" chip naming that same competition, so
// `getByText(comp.name)` can match both the rail item and the chip.
function enterCompMode(container) {
  const items = container.querySelectorAll('.rd-rail__item');
  expect(items.length).toBeGreaterThan(1);
  fireEvent.click(items[1]);
}

describe('AdminRegistrationDeskPage disables writes for an id-less row in comp mode (bc-pnum)', () => {
  let toggleCheckIn;
  let replaceParticipant;

  beforeEach(() => {
    toggleCheckIn = vi.fn().mockResolvedValue({});
    replaceParticipant = vi.fn().mockResolvedValue({});
    window.API.toggleCheckIn = toggleCheckIn;
    window.API.replaceParticipant = replaceParticipant;
    window.promptAdminPassword = vi.fn().mockResolvedValue('admin-pw');
  });

  afterEach(() => {
    delete window.API.toggleCheckIn;
    delete window.API.replaceParticipant;
    delete window.promptAdminPassword;
  });

  it('disables the row check-in control for an id-less row and sends no request', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { container, getByRole } = await mount(tournament);
    enterCompMode(container);
    const checkbox = getByRole('checkbox', { name: /check in kenji sato/i });
    expect(checkbox.disabled).toBe(true);
    expect(checkbox.getAttribute('title')).toContain('No id on file');
    fireEvent.click(checkbox);
    expect(toggleCheckIn).not.toHaveBeenCalled();
  });

  it('leaves the row check-in control enabled for a stamped row in comp mode', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: 'uuid-kenji', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { container, getByRole } = await mount(tournament);
    enterCompMode(container);
    const checkbox = getByRole('checkbox', { name: /check in kenji sato/i });
    expect(checkbox.disabled).toBe(false);
    await act(async () => { fireEvent.click(checkbox); });
    expect(toggleCheckIn).toHaveBeenCalledTimes(1);
  });

  it('disables the "also in" chip for a cross-competition entry with no id and sends no request', async () => {
    const tournament = makeTournament({
      competitions: [
        {
          id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
          checkInEnabled: true,
          players: [{ id: 'uuid-kenji', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
        },
        {
          id: 'kata', name: 'Kata Individual', kind: 'individual', status: 'draw-ready',
          checkInEnabled: true,
          players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
        },
      ],
    });
    const { container, getByTitle } = await mount(tournament);
    enterCompMode(container); // Kenji's row in "men" shows an "Also in" chip for Kata
    const chip = getByTitle('No id on file. Save the roster once and the ids are assigned.');
    expect(chip.tagName).toBe('BUTTON');
    expect(chip.disabled).toBe(true);
    fireEvent.click(chip);
    expect(toggleCheckIn).not.toHaveBeenCalled();
  });

  it('disables the Edit modal Save button for an id-less row and sends no request', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { container, getByText } = await mount(tournament);
    enterCompMode(container);
    const editButton = container.querySelector('button[aria-label="Edit Kenji Sato"]');
    expect(editButton).toBeTruthy();
    fireEvent.click(editButton);

    const saveButton = getByText('Save changes');
    expect(saveButton.disabled).toBe(true);
    expect(saveButton.getAttribute('title')).toContain('No id on file');
    // Scoped to the modal's own note (.rd-edit__note): the row behind the
    // modal ALSO renders this same hint inline, so an unscoped getByText
    // would find two matches here.
    const modalNote = container.querySelector('.rd-edit__note');
    expect(modalNote?.textContent).toBe('No id on file. Save the roster once and the ids are assigned.');

    fireEvent.click(saveButton);
    expect(replaceParticipant).not.toHaveBeenCalled();
  });
});

// bc-pnum: a hover title alone is
// unreachable on a tablet or by keyboard/screen-reader. The row check-in
// checkbox's disabled reason must ride in the aria-label AND render inline
// on the row, not only in a title attribute.
describe('AdminRegistrationDeskPage folds the id-less reason into aria-label and renders it inline (bc-pnum)', () => {
  it('states the reason in the checkbox aria-label and shows it inline on the row', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { container, getByRole } = await mount(tournament);
    enterCompMode(container);
    const checkbox = getByRole('checkbox', { name: /check in kenji sato.*no id on file/i });
    expect(checkbox).toBeTruthy();
    const inlineHint = container.querySelector('.noid-hint');
    expect(inlineHint?.textContent).toBe('No id on file. Save the roster once and the ids are assigned.');
  });

  it('does not fold a reason into the aria-label, nor render an inline hint, for a stamped row', async () => {
    const tournament = makeTournament({
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready',
        checkInEnabled: true,
        players: [{ id: 'uuid-kenji', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    const { container, getByRole } = await mount(tournament);
    enterCompMode(container);
    const checkbox = getByRole('checkbox', { name: 'Check in Kenji Sato' });
    expect(checkbox).toBeTruthy();
    expect(container.querySelector('.noid-hint')).toBeNull();
  });
});

// bc-pnum (item 3): checkPersonEntries/bulkCheckPeople
// ("All competitions" mode) used to send toggleCheckIn for EVERY entry a
// person has, including one whose OWN record carries no id (rdApiPid
// returns "" for it, which can only 404 about a row on screen) -- a comment
// nearby falsely claimed this was already skipped. A person entered in
// three competitions, one of them id-less, must send exactly two requests.
describe('AdminRegistrationDeskPage skips id-less entries in "all" mode check-in (bc-pnum)', () => {
  let toggleCheckIn;

  beforeEach(() => {
    toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API.toggleCheckIn = toggleCheckIn;
  });

  afterEach(() => {
    delete window.API.toggleCheckIn;
  });

  it('sends exactly two requests for a person entered in three competitions when one entry has no id', async () => {
    const tournament = makeTournament({
      competitions: [
        {
          id: 'men', name: "Men's Individual", kind: 'individual', status: 'draw-ready', checkInEnabled: true,
          players: [{ id: 'p-men', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
        },
        {
          id: 'kata', name: 'Kata Individual', kind: 'individual', status: 'draw-ready', checkInEnabled: true,
          players: [{ id: 'p-kata', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
        },
        {
          id: 'iaido', name: 'Iaido Individual', kind: 'individual', status: 'draw-ready', checkInEnabled: true,
          players: [{ id: '', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
        },
      ],
    });
    const { getByRole } = await mount(tournament);
    const checkbox = getByRole('checkbox', { name: /check in kenji sato for all their competitions/i });
    await act(async () => { fireEvent.click(checkbox); });
    expect(toggleCheckIn).toHaveBeenCalledTimes(2);
    expect(toggleCheckIn).toHaveBeenCalledWith('men', 'p-men', true, 'pw');
    expect(toggleCheckIn).toHaveBeenCalledWith('kata', 'p-kata', true, 'pw');
    expect(toggleCheckIn).not.toHaveBeenCalledWith('iaido', '', true, 'pw');
  });
});
