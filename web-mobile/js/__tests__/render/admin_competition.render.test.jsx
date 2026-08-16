import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';

// mp-hpe3 Phase 0 safety net: RENDER-SMOKE characterization of the sections
// inside admin_competition.jsx that the upcoming split moves into their own
// modules: AdminCompOverview, AdminSettings, FightingSpiritAwardsEditor,
// AdminBracket, and AdminSwissRounds.
//
// These components are module-internal (not on window), so they are exercised
// through the PUBLIC AdminCompetition entry by routing each `section`. That
// also means the test survives the split unchanged; AdminCompetition stays on
// window and pins exactly what a split must preserve: every section must
// still mount with zero console errors. The render harness mounts with REAL
// React and FAILS on any console.warn/error, so a moved component that
// references a window.* dep not yet loaded at its render time throws here
// (the load-order class of breakage that vitest's default stub cannot catch).
//
// All window.* deps captured at admin_competition.jsx module load (the
// `const X = window.X` block at the top) MUST be set before the dynamic import,
// or the captured const is undefined and the component throws on render.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  // Components rendered by AdminCompetition / its sections.
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: Stub('CourtPicker'),
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  BracketTree: Stub('BracketTree'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  // NOTE: the pure helpers admin_competition_* consumes (compMatchStats,
  // hasBothSides, hasPoolOriginPlaceholder, dmyToIso, isoToDmy, isValidDate,
  // validateAndNormalizeDate, decideNumericUpdate, deriveTournamentDays) are
  // deliberately NOT stubbed here; the render harness (vitest.setup.render.js)
  // loads the real admin_helpers.jsx, so the components run against the genuine
  // implementations and contracts. Hand-rolled stubs drifted from the real
  // signatures/shapes (e.g. decideNumericUpdate is (raw, min), not (field, value)),
  // which made the smoke test less representative; using the real helpers removes
  // that whole class of drift. Only cross-module components, browser APIs, dialogs,
  // and the backend API are stubbed below.
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({
    matches: false,
    addEventListener: noop, removeEventListener: noop,
    addListener: noop, removeListener: noop,
  }),
  // Dialogs / async: only reached from handlers; safe resolved stubs.
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptAdminPassword: vi.fn().mockResolvedValue(null),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    swissGenerateRound: vi.fn().mockResolvedValue(null),
    updateCompetitionAwards: vi.fn().mockResolvedValue(null),
    completeCompetition: vi.fn().mockResolvedValue({ status: 'completed' }),
    // Advisory fetch made on every competition mount; per-test overrides in the
    // draw-warning suite below.
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
  },
};

const originals = {};
let AdminCompetition;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_competition.jsx');
  AdminCompetition = window.AdminCompetition;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Mudansha',
    status: 'setup',
    format: 'pools',
    kind: 'individual',
    players: [
      { id: 'p1', name: 'Yamada', seed: 1 },
      { id: 'p2', name: 'Tanaka' },
    ],
    courts: ['A'],
    startTime: '09:00',
    date: '',
    fightingSpiritAwards: [],
    swissCurrentRound: 0,
    swissRounds: 0,
    ...overrides,
  };
}

function makeTournament(comp, overrides = {}) {
  return {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [comp, { id: 'c2', name: 'Yudansha' }],
    ...overrides,
  };
}

// Mount within act() so async effects (e.g. AdminSettings' schedule-estimate
// fetch) flush and settle their state updates inside the act boundary: the
// render harness fails the test on the "not wrapped in act(...)" console.error
// otherwise. Returns the render result; a throw during mount fails the test.
async function mountSection(section, { comp = makeCompetition(), tweaks = {}, bracket = null, tournament = {} } = {}) {
  const t = makeTournament(comp, tournament);
  let result;
  await act(async () => {
    result = render(
      <AdminCompetition
        tournament={t}
        competition={comp}
        pools={[]}
        poolMatches={[]}
        standings={[]}
        bracket={bracket}
        section={section}
        onSection={noop}
        onBack={noop}
        onOpenCompetition={noop}
        onUpdate={noop}
        onRefreshCompetition={noop}
        onMoveCourt={noop}
        onEditScore={noop}
        onLogout={noop}
        onViewerMode={noop}
        tweaks={tweaks}
        password=""
        showToast={noop}
      />
    );
  });
  return result;
}

describe('AdminCompetition section render-smoke (mp-hpe3 split characterization)', () => {
  it('renders Overview section without throwing', async () => {
    const { container } = await mountSection('overview');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Settings section without throwing', async () => {
    const { container } = await mountSection('settings');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Fighting Spirit (awards) section without throwing', async () => {
    const { container } = await mountSection('awards');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Bracket section (not-generated empty state) without throwing', async () => {
    const { container } = await mountSection('bracket');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Swiss-rounds section without throwing', async () => {
    const comp = makeCompetition({ format: 'swiss', swissRounds: 5, swissCurrentRound: 1 });
    const { container } = await mountSection('swiss', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Overview for a team competition without throwing', async () => {
    const comp = makeCompetition({ kind: 'team', players: [{ id: 't1', name: 'Team A' }] });
    const { container } = await mountSection('overview', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });
});

// mp-gy6g: "Complete competition" is the only trigger for a bracket-based
// (playoffs, or mixed-after-knockout) competition to ever reach status
// "completed" — MaybeAutoCompletePools only auto-transitions League on its
// last pool match. Gated on canComplete (admin_competition.jsx), which
// delegates to bracketFullyComplete (admin_helpers.jsx, exercised for real
// here per this file's header comment).
describe('AdminCompetition "Complete competition" action (mp-gy6g)', () => {
  const realMatch = (status) => ({
    sideA: { id: 'p1', name: 'Alice' },
    sideB: { id: 'p2', name: 'Bob' },
    status,
  });
  const findButton = (container, text) =>
    Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

  it('is hidden while a bracket match is still unfinished', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('running')]] };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('stays hidden once every round match is done but the bronze match is not (thirdPlaceMatch is a sibling of rounds)', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs', naginata: true });
    const bracket = {
      rounds: [[realMatch('completed')], [realMatch('completed')]],
      thirdPlaceMatch: realMatch('running'),
    };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('appears once every bracket match, including the bronze match, is completed', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs', naginata: true });
    const bracket = {
      rounds: [[realMatch('completed')], [realMatch('completed')]],
      thirdPlaceMatch: realMatch('completed'),
    };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).not.toBeUndefined();
  });

  it('is hidden once the competition is already completed', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'completed' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('completed')]] };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('is hidden while setup/draw-ready, even if a stale bracket looks complete', async () => {
    const bracket = { rounds: [[realMatch('completed')]] };
    for (const status of ['setup', 'draw-ready']) {
      const comp = makeCompetition({ format: 'playoffs', status });
      const { container } = await mountSection('overview', { comp, bracket });
      expect(findButton(container, 'Complete competition →')).toBeUndefined();
    }
  });

  it('calls the API and refreshes on confirm', async () => {
    window.confirmDialog.mockResolvedValueOnce(true);
    // Completion is elevated-gated; promptAdminPassword resolves "" (no admin
    // password configured) so the handler proceeds.
    window.promptAdminPassword.mockResolvedValueOnce('');
    window.API.completeCompetition.mockClear();
    const onRefreshCompetition = vi.fn();
    const showToast = vi.fn();
    const comp = makeCompetition({ id: 'nagi-1', format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('completed')]] };
    const t = makeTournament(comp);
    let container;
    await act(async () => {
      ({ container } = render(
        <AdminCompetition
          tournament={t}
          competition={comp}
          pools={[]}
          poolMatches={[]}
          standings={[]}
          bracket={bracket}
          section="overview"
          onSection={noop}
          onBack={noop}
          onOpenCompetition={noop}
          onUpdate={noop}
          onRefreshCompetition={onRefreshCompetition}
          onMoveCourt={noop}
          onEditScore={noop}
          onLogout={noop}
          onViewerMode={noop}
          tweaks={{}}
          password="shiaijo2026"
          showToast={showToast}
        />
      ));
    });

    const btn = findButton(container, 'Complete competition →');
    expect(btn).not.toBeUndefined();
    await act(async () => { fireEvent.click(btn); });

    await waitFor(() => expect(window.API.completeCompetition).toHaveBeenCalledWith('nagi-1', 'shiaijo2026', ''));
    expect(onRefreshCompetition).toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith(expect.stringContaining('marked complete'));
  });

  it('does not call the API when the operator cancels the confirm dialog', async () => {
    window.confirmDialog.mockResolvedValueOnce(false);
    window.API.completeCompetition.mockClear();
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')]] };
    const { container } = await mountSection('overview', { comp, bracket });

    const btn = findButton(container, 'Complete competition →');
    await act(async () => { fireEvent.click(btn); });

    expect(window.API.completeCompetition).not.toHaveBeenCalled();
  });
});

// bc-draw R9 UAT gap 1: the settings screen renders Save TWICE (header and
// foot of a long form) and the two disabled conditions had drifted. The footer
// copy omitted both hasDurationError and blockingCourtsErr, so with an
// unpairable shiaijo count the header greyed out while the footer stayed live,
// fired the PUT and took a 400. Both now read one derived `saveDisabled`.
describe('AdminSettings Save buttons (bc-draw R9 gap 1)', () => {
  const saveButtons = (container) =>
    Array.from(container.querySelectorAll('button')).filter((b) => b.textContent.trim().startsWith('Save changes'));

  const clickPill = async (container, label) => {
    const pill = Array.from(container.querySelectorAll('button.radio-pill'))
      .find((b) => b.textContent.trim() === label);
    expect(pill, `pill "${label}" not found`).not.toBeUndefined();
    await act(async () => { fireEvent.click(pill); });
  };

  it('renders exactly two Save buttons, both disabled while nothing is dirty', async () => {
    const { container } = await mountSection('settings');
    const saves = saveButtons(container);
    expect(saves).toHaveLength(2);
    saves.forEach((b) => expect(b.disabled).toBe(true));
  });

  it('enables BOTH Save buttons after a valid court change', async () => {
    // Guards the fix from over-correcting into "footer always disabled":
    // A + B is a pairable allocation, so both buttons must go live.
    const comp = makeCompetition({ courts: ['A'], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    await clickPill(container, 'Shiaijo (court) B');
    const saves = saveButtons(container);
    expect(saves).toHaveLength(2);
    saves.forEach((b) => expect(b.disabled).toBe(false));
  });

  it('disables BOTH Save buttons on an unpairable shiaijo count', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    await clickPill(container, 'Shiaijo (court) C'); // → A, B, C: 3 shiaijo
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).not.toBeNull();
    const saves = saveButtons(container);
    expect(saves).toHaveLength(2);
    saves.forEach((b) => expect(b.disabled).toBe(true));
  });
});

// bc-draw R9 UAT gap 3: shrinking the tournament's shiaijo count left the
// competition holding a court the venue no longer has. The pills were built
// from tournament.courts alone, so a competition storing [A B C D] under a
// 3-shiaijo tournament rendered three selected pills while four were on disk:
// the screen showed one allocation and would have saved another, and no rule
// fired because the SHOWN count (3) was not the STORED count (4).
describe('AdminSettings orphaned shiaijo (bc-draw R9 gap 3)', () => {
  const pillLabels = (container) =>
    Array.from(container.querySelectorAll('button.radio-pill'))
      .map((b) => b.textContent.trim())
      .filter((t) => t.startsWith('Shiaijo'));

  const orphanComp = () => makeCompetition({ courts: ['A', 'B', 'C', 'D'], format: 'playoffs' });
  const threeCourtVenue = { courts: ['A', 'B', 'C'] };

  it('renders a flagged pill for a court the tournament no longer has', async () => {
    const { container } = await mountSection('settings', { comp: orphanComp(), tournament: threeCourtVenue });
    const labels = pillLabels(container);
    // Four pills for four stored courts: what is shown equals what is stored.
    expect(labels).toHaveLength(4);
    expect(labels[3]).toContain('D');
    expect(labels[3]).toContain('not in tournament');
    expect(container.querySelector('[data-testid="orphan-court-D"]')).not.toBeNull();
  });

  it('every rendered pill is selected, matching the stored allocation exactly', async () => {
    const { container } = await mountSection('settings', { comp: orphanComp(), tournament: threeCourtVenue });
    const selected = Array.from(container.querySelectorAll('button.radio-pill.is-active'))
      .map((b) => b.textContent.trim())
      .filter((t) => t.startsWith('Shiaijo'));
    expect(selected).toHaveLength(4);
  });

  it('explains the orphaned shiaijo instead of silently hiding it', async () => {
    const { container } = await mountSection('settings', { comp: orphanComp(), tournament: threeCourtVenue });
    const hint = container.querySelector('[data-testid="orphan-shiaijo-hint"]');
    expect(hint).not.toBeNull();
    expect(hint.textContent).toContain('D');
    expect(hint.textContent).toContain('no longer part of this tournament');
  });

  it('says nothing when every assigned shiaijo still exists', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: threeCourtVenue });
    expect(container.querySelector('[data-testid="orphan-shiaijo-hint"]')).toBeNull();
    expect(pillLabels(container)).toHaveLength(3);
  });

  it('blocks Generate draw and explains why, rather than letting it 400', async () => {
    // The engine refuses a draw onto a shiaijo the tournament lacks, so the
    // overview must not offer the button. Needs >=2 players and status setup.
    const comp = orphanComp();
    const { container } = await mountSection('overview', { comp, tournament: threeCourtVenue });
    const draw = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Generate draw');
    expect(draw).not.toBeUndefined();
    expect(draw.disabled).toBe(true);
    const block = container.querySelector('[data-testid="draw-block"]');
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('no longer part of this tournament');
  });
});

// The rule reached the operator ONLY as a rejection: pick a bad count, get a
// red line. The standing hint states what may be picked, and why, before
// anything is blocked, and is venue-aware so a 3-shiaijo tournament answers
// "why can't I pick all three of my shiaijo" at the field.
describe('AdminSettings standing shiaijo hint (spec 007 R9)', () => {
  const threeCourtVenue = { courts: ['A', 'B', 'C'] };
  const hintText = (container) => {
    const el = container.querySelector('[data-testid="shiaijo-count-hint"]');
    return el && el.textContent;
  };
  const clickPill = async (container, label) => {
    const pill = Array.from(container.querySelectorAll('button.radio-pill'))
      .find((b) => b.textContent.trim() === label);
    expect(pill, `pill "${label}" not found`).not.toBeUndefined();
    await act(async () => { fireEvent.click(pill); });
  };

  it('shows on a VALID allocation, stating the counts and the reason', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: threeCourtVenue });
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    const hint = hintText(container);
    expect(hint).not.toBeNull();
    expect(hint).toContain('can use 1 or 2 shiaijo');
    expect(hint).toContain('this tournament has 3');
    expect(hint).toContain('merge in pairs');
    expect(hint).toContain('halve cleanly');
  });

  it('names every valid count a bigger venue allows', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('settings', {
      comp, tournament: { courts: ['A', 'B', 'C', 'D', 'E'] },
    });
    expect(hintText(container)).toContain('can use 1, 2 or 4 shiaijo');
  });

  it('survives the selection going invalid, without restating the mechanism', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: threeCourtVenue });
    await clickPill(container, 'Shiaijo (court) C'); // → A, B, C
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).not.toBeNull();
    const hint = hintText(container);
    expect(hint).toContain('can use 1 or 2 shiaijo');
    expect(hint).not.toContain('halve cleanly');
  });

  // Deselecting every pill is an unfinished form, not a request to inherit.
  // shiaijoCountError answers null for 0 by design (an empty list ON DISK does
  // mean "inherit"), so without its own rule this screen showed nothing and
  // offered a live Save that stored an allocation the operator never chose.
  //
  // Both formats, because the emptiness rule applies whatever the format,
  // unlike the count rule.
  it.each(['playoffs', 'league'])('refuses a selection the operator has emptied, and blocks Save (%s)', async (format) => {
    const comp = makeCompetition({ courts: ['A', 'B'], format });
    const { container } = await mountSection('settings', { comp, tournament: threeCourtVenue });
    await clickPill(container, 'Shiaijo (court) A');
    await clickPill(container, 'Shiaijo (court) B'); // → none selected

    const err = container.querySelector('[data-testid="shiaijo-count-error"]');
    expect(err).not.toBeNull();
    expect(err.textContent).toContain(window.SHIAIJO_NONE_SELECTED);
    const save = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim().startsWith('Save'));
    expect(save).not.toBeUndefined();
    expect(save.disabled).toBe(true);
  });

  // A STORED empty list is the legacy/imported record that legitimately
  // inherits, and inheriting a legal count is fine. The hint tells the operator
  // to make it explicit, but Save must stay live: blocking here would lock them
  // out of every unrelated edit on this screen, which is the one outcome this
  // rule must not cause.
  it('does not block Save on a record that merely arrived with no shiaijo', async () => {
    const comp = makeCompetition({ courts: [], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts: ['A', 'B'] } });
    // Inheriting 2 of 2 is a legal allocation, so no banner...
    expect(container.querySelector('[data-testid="shiaijo-count-banner"]')).toBeNull();
    // ...and nothing is blocking a save. (The Save buttons are disabled here
    // only because the form is not dirty, which is why this asserts on the
    // block message rather than on `disabled`.)
    expect(container.textContent).not.toContain('Fix shiaijo allocation');
    // And NO red rule under the pills either. The operator has not emptied
    // anything: this is what arrived off disk, and it is legal. Demanding they
    // "select at least one" here contradicts both the absent banner and the
    // live Save, and points at a competition with nothing wrong with it.
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
  });

  it('is absent for league, whose courts the rule does not govern', async () => {
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'league' });
    const { container } = await mountSection('settings', { comp, tournament: threeCourtVenue });
    expect(hintText(container)).toBeNull();
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
  });
});

// The stored-allocation banner and the blocked-start explanation both carry
// the rule, so both move with it. Their test ids used to say "odd", which the
// power-of-two rule made wrong (6 is even and still invalid).
describe('AdminSettings stored-allocation banner (spec 007 R9)', () => {
  it('warns about a stored 6-shiaijo allocation the old parity rule allowed', async () => {
    // Exactly the tolerated case: a record written by a pre-rule binary must
    // keep loading and rendering, with the banner explaining the block.
    const courts = ['A', 'B', 'C', 'D', 'E', 'F'];
    const comp = makeCompetition({ courts, format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts } });
    const banner = container.querySelector('[data-testid="shiaijo-count-banner"]');
    expect(banner).not.toBeNull();
    expect(banner.textContent).toContain('6 shiaijo cannot be paired down');
    expect(banner.textContent).toContain('halve cleanly');
    // Venue-aware, because this banner sits at the top of the card with no
    // hint beside it to correct an option the venue cannot supply. The venue
    // here has 6 shiaijo, so 8 is not on offer.
    expect(banner.textContent).toContain('This tournament has 6, so this competition can use 1, 2 or 4');
    expect(banner.textContent).not.toContain('or 8');
  });

  // The banner judges the RESOLVED allocation. An empty court list MEANS
  // "inherit the tournament's shiaijo" and is what the server stores and
  // validates, so measuring the raw list asked about a value never persisted:
  // shiaijoCountError(0) is null, so this screen showed nothing at all while
  // the dashboard refused the draw and sent the operator here to fix it.
  it('warns about an INHERITED allocation the competition never chose', async () => {
    const comp = makeCompetition({ courts: [], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    const banner = container.querySelector('[data-testid="shiaijo-count-banner"]');
    expect(banner).not.toBeNull();
    // Names where the count came from, or the operator is handed a number they
    // never picked and no way to connect it to anything on screen.
    expect(banner.textContent).toContain('no shiaijo of its own');
    expect(banner.textContent).toContain("all 3 of the tournament's");
    expect(banner.textContent).toContain('3 shiaijo cannot be paired down');
    // The competition's own list is empty; that must not be reported as its
    // allocation.
    expect(banner.textContent).not.toContain('assigned 0');
  });

  it('stays silent when the inherited count is legal', async () => {
    const comp = makeCompetition({ courts: [], format: 'playoffs' });
    const { container } = await mountSection('settings', { comp, tournament: { courts: ['A', 'B'] } });
    expect(container.querySelector('[data-testid="shiaijo-count-banner"]')).toBeNull();
  });

  it('blocks the start of a stored 6-shiaijo competition and says why', async () => {
    const courts = ['A', 'B', 'C', 'D', 'E', 'F'];
    // Date set on purpose: an invalid one disables the same button, so without
    // it this would pass even with the shiaijo blocker gone.
    const comp = makeCompetition({ courts, format: 'playoffs', date: '01-06-2026' });
    const { container } = await mountSection('overview', { comp, tournament: { courts } });
    const start = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Start competition →');
    expect(start).not.toBeUndefined();
    expect(start.disabled).toBe(true);
    const block = container.querySelector('[data-testid="draw-block"]');
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('6 shiaijo cannot be paired down to a single bracket');
  });
});

// An incomplete seeding is refused by the draw, so the console refuses it
// FIRST. The operator reaches this state through the seeding panel's ordinary
// behaviour: it saves each rank as it is typed, so entering seed 4 before seeds
// 1 to 3 leaves the competition holding {4}. Pre-fix, both header buttons were
// live and the only trace of the refusal was an expiring toast.
describe('Header start blocked by an incomplete seeding', () => {
  // A valid date is load-bearing: the same buttons are disabled by an invalid
  // one, so a blank date would make every `disabled` assertion below pass
  // whether or not the seeding blocker exists.
  const halfSeeded = () => makeCompetition({
    courts: ['A', 'B'],
    format: 'playoffs',
    date: '01-06-2026',
    players: [
      { id: 'p1', name: 'Yamada' },
      { id: 'p2', name: 'Tanaka' },
      { id: 'p3', name: 'Suzuki' },
      { id: 'p4', name: 'Kobayashi', seed: 4 },
    ],
  });

  it('disables Generate draw and Start, and names the ranks to type', async () => {
    const { container } = await mountSection('overview', {
      comp: halfSeeded(), tournament: { courts: ['A', 'B'] },
    });
    const button = (label) =>
      Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === label);
    expect(button('Generate draw').disabled).toBe(true);
    expect(button('Start competition →').disabled).toBe(true);

    const block = container.querySelector('[data-testid="draw-block"]');
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('seed ranks 1, 2 and 3 have not been set');
    expect(block.textContent).toContain('but rank 4 has');
    // The remedy has to name the screen that fixes THIS blocker.
    expect(block.textContent).toContain('Participants & seeds');
    expect(block.textContent).not.toContain('Reassign shiaijo');
  });

  it('routes the checklist to the seeding panel, not to Settings', async () => {
    const { container } = await mountSection('overview', {
      comp: halfSeeded(), tournament: { courts: ['A', 'B'] },
    });
    const step = container.querySelector('[data-testid="step-generate"]');
    expect(step.textContent).toContain('seed ranks 1, 2 and 3 have not been set');
    expect(step.textContent).toContain('Fix seeding →');
  });

  it('leaves the buttons live once the seeding is complete', async () => {
    const comp = makeCompetition({
      courts: ['A', 'B'],
      format: 'playoffs',
      date: '01-06-2026',
      players: [
        { id: 'p1', name: 'Yamada', seed: 1 },
        { id: 'p2', name: 'Tanaka', seed: 2 },
        { id: 'p3', name: 'Suzuki' },
      ],
    });
    const { container } = await mountSection('overview', { comp, tournament: { courts: ['A', 'B'] } });
    const generate = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Generate draw');
    expect(generate.disabled).toBe(false);
    expect(container.querySelector('[data-testid="draw-block"]')).toBeNull();
  });
});

// "Next steps" told the operator to press a button the court rule had just
// disabled. A checklist that points at a dead control is worse than silent.
describe('AdminCompOverview next steps under a court block (spec 007 R9)', () => {
  const stepText = (container, id) => {
    const el = container.querySelector(`[data-testid="step-${id}"]`);
    return el && el.textContent;
  };

  it('points at the Generate draw button when nothing blocks it', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    expect(stepText(container, 'generate')).toContain('Use the "Generate draw" button in the header above');
  });

  it('points at Settings instead when the shiaijo count blocks the draw', async () => {
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    const text = stepText(container, 'generate');
    expect(text).not.toContain('Use the "Generate draw" button in the header above');
    expect(text).toContain('3 shiaijo cannot be paired down to a single bracket');
    expect(text).toContain('Reassign shiaijo in Settings.');
  });
});

// The draw's seed-placement warnings (spec 007 R2/D7). The seeding rules never
// refuse a draw: a constraint the configuration cannot satisfy gives way and
// the operator is TOLD what was relaxed. The banner is therefore an advisory
// that sits alongside the generated draw, never a blocker, and it must not
// appear at all for a competition that had nothing to relax.
describe('AdminCompetition draw seed warnings (spec 007 R2/D7)', () => {
  const banner = (container) => container.querySelector('[data-testid="draw-seed-warnings"]');
  const withWarnings = (...warnings) => {
    window.API.fetchDrawWarnings = vi.fn().mockResolvedValue(warnings);
  };

  afterEach(() => {
    window.API.fetchDrawWarnings = vi.fn().mockResolvedValue([]);
  });

  it('renders every warning the draw reported', async () => {
    withWarnings(
      'Seed 4 ignored: two seeds must never share a pool, and this competition has 3 pools for 4 seeds. The draw used seeds 1, 2 and 3.',
      'Not every seed could be given its own quarter of the draw (seeds 2 and 4). The draw was made anyway.',
    );
    const comp = makeCompetition({ status: 'draw-ready', courts: ['A', 'B'] });
    const { container } = await mountSection('overview', { comp });
    const el = banner(container);
    expect(el).not.toBeNull();
    expect(el.querySelectorAll('li').length).toBe(2);
    expect(el.textContent).toContain('Seed 4 ignored');
    expect(el.textContent).toContain('own quarter of the draw');
  });

  it('does not block the draw: Start competition stays enabled', async () => {
    withWarnings('Not every seed could be given its own quarter of the draw (seeds 2 and 4). The draw was made anyway.');
    const comp = makeCompetition({ status: 'draw-ready', courts: ['A', 'B'] });
    const { container } = await mountSection('overview', { comp });
    expect(banner(container)).not.toBeNull();
    const start = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Start competition \u2192');
    expect(start).not.toBeUndefined();
    expect(start.disabled).toBe(false);
  });

  it('renders nothing when the draw had nothing to relax', async () => {
    const comp = makeCompetition({ status: 'draw-ready', courts: ['A', 'B'] });
    const { container } = await mountSection('overview', { comp });
    expect(banner(container)).toBeNull();
  });

  // Switching between two STARTED competitions kept the previous one's
  // sentences on screen for the length of the new fetch, under the new
  // competition's name and the heading "the draw could not honour every rule".
  // The effect only cleared on its setup branch, so the non-setup path went
  // straight to the fetch with the old list still rendered. The warnings are
  // now held with the competition id they describe and rendered only when the
  // two match.
  it('never shows the previous competition\'s warnings while the new fetch is in flight', async () => {
    const first = makeCompetition({ id: 'first', name: 'Mudansha', status: 'pools', courts: ['A', 'B'] });
    withWarnings('Seed 4 ignored: Mudansha had 3 pools for 4 seeds.');
    const { container, rerender } = await mountSection('overview', { comp: first });
    expect(banner(container).textContent).toContain('Seed 4 ignored');

    // The second competition's fetch is held open, which is exactly the window
    // the stale render happened in.
    let releaseSecond;
    window.API.fetchDrawWarnings = vi.fn(() => new Promise((resolve) => { releaseSecond = resolve; }));

    const second = makeCompetition({ id: 'second', name: 'Yudansha', status: 'pools', courts: ['A', 'B'] });
    await act(async () => {
      rerender(
        <AdminCompetition
          tournament={makeTournament(second)}
          competition={second}
          pools={[]} poolMatches={[]} standings={[]} bracket={null}
          section="overview"
          onSection={noop} onBack={noop} onOpenCompetition={noop} onUpdate={noop}
          onRefreshCompetition={noop} onMoveCourt={noop} onEditScore={noop}
          onLogout={noop} onViewerMode={noop} tweaks={{}} password="" showToast={noop}
        />
      );
    });

    expect(container.textContent).toContain('Yudansha');
    expect(banner(container), "Mudansha's warnings are still on screen under Yudansha's name").toBeNull();

    // And the new competition's own warnings still arrive when they land.
    await act(async () => {
      releaseSecond(['Seed 2 ignored: Yudansha had 1 pool for 2 seeds.']);
    });
    expect(banner(container).textContent).toContain('Yudansha had 1 pool');
    expect(banner(container).textContent).not.toContain('Mudansha');
  });
});

// A competition stored without a courts key arrives from the API as
// `courts: null` (Go ships a nil slice as JSON null). The page head read
// `c.courts.join(", ")` directly, so the WHOLE console died with "Cannot read
// properties of null (reading 'join')" and every section became unreachable -
// on the one screen whose Settings tab is the documented remedy for that
// record. The render harness fails on any console.error, so an unguarded read
// surfaces here as a hard failure rather than a silent blank.
describe('AdminCompetition survives a null courts list (U1)', () => {
  const SECTIONS = ['overview', 'settings', 'participants', 'export'];

  SECTIONS.forEach((section) => {
    it(`renders the ${section} section with courts: null`, async () => {
      const comp = makeCompetition({ courts: null });
      const { container } = await mountSection(section, { comp });
      expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
    });
  });

  it('says the allocation is empty rather than printing nothing', async () => {
    const comp = makeCompetition({ courts: null });
    const { container } = await mountSection('overview', { comp });
    expect(container.querySelector('.page-head__sub').textContent).toContain('No shiaijo assigned');
  });

  it('does not report 1 Court one line under "No shiaijo assigned"', async () => {
    // courtCount() floors at 1 by design (the schedule divides by it), which
    // read as a flat contradiction of the page head on this record. The
    // dashboard card already showed 0 for the same competition.
    const comp = makeCompetition({ courts: null });
    const { container } = await mountSection('overview', { comp });
    const box = Array.from(container.querySelectorAll('.stat-box'))
      .find((b) => /Courts?$/.test(b.querySelector('.l').textContent));
    expect(box.querySelector('.v').textContent).toBe('0');
    expect(box.textContent).toContain('Courts');
  });

  it('still reports a real allocation unchanged', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'] });
    const { container } = await mountSection('overview', { comp });
    const box = Array.from(container.querySelectorAll('.stat-box'))
      .find((b) => /Courts?$/.test(b.querySelector('.l').textContent));
    expect(box.querySelector('.v').textContent).toBe('2');
  });

  it('renders the league/partial court hint, the sibling read that was unguarded', async () => {
    // `local.courts.length` in the league suggestion branch of the settings
    // screen: guarding only the page head would have moved the crash here.
    const comp = makeCompetition({ courts: null, format: 'league' });
    const { container } = await mountSection('settings', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders with a null players list too', async () => {
    const comp = makeCompetition({ courts: null, players: null });
    const { container } = await mountSection('overview', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });
});

// The header block, the checklist row and their siblings all render the rule
// with nothing beneath them to correct it, so the counts they name have to be
// counts the venue can actually supply.
describe('Competition header + checklist name only reachable counts (U2)', () => {
  const threeCourtVenue = { courts: ['A', 'B', 'C'] };

  it('does not offer a 4th shiaijo to a 3-shiaijo venue in the header block', async () => {
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: threeCourtVenue });
    const block = container.querySelector('[data-testid="draw-block"]');
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(block.textContent).not.toContain('4');
  });

  it('does not offer a 4th shiaijo in the next-steps checklist either', async () => {
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: threeCourtVenue });
    const step = container.querySelector('[data-testid="step-generate"]');
    expect(step.textContent).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(step.textContent).not.toContain('4');
  });
});

// The "Reassign shiaijo →" call to action authored on the generate step could
// never render: only an `active` step draws its button, and "Review seeds &
// settings" is emitted non-done forever, so it held `active` on every render.
describe('AdminCompOverview blocked-draw CTA is reachable (U5)', () => {
  it('renders the Reassign shiaijo button and navigates to Settings', async () => {
    const onSection = vi.fn();
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'playoffs' });
    const t = makeTournament(comp, { courts: ['A', 'B', 'C'] });
    let container;
    await act(async () => {
      ({ container } = render(
        <AdminCompetition
          tournament={t} competition={comp} pools={[]} poolMatches={[]} standings={[]}
          bracket={null} section="overview" onSection={onSection} onBack={noop}
          onOpenCompetition={noop} onUpdate={noop} onRefreshCompetition={noop}
          onMoveCourt={noop} onEditScore={noop} onLogout={noop} onViewerMode={noop}
          tweaks={{}} password="" showToast={noop}
        />
      ));
    });
    const cta = container.querySelector('[data-testid="step-generate-cta"]');
    expect(cta).not.toBeNull();
    expect(cta.textContent).toBe('Reassign shiaijo →');
    await act(async () => { fireEvent.click(cta); });
    expect(onSection).toHaveBeenCalledWith('settings');
  });

  it('shows exactly one Settings call to action, not two', async () => {
    const comp = makeCompetition({ courts: ['A', 'B', 'C'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    expect(container.querySelector('[data-testid="step-settings-cta"]')).toBeNull();
    expect(container.querySelector('[data-testid="step-generate-cta"]')).not.toBeNull();
  });

  it('leaves the ordinary checklist CTA alone when nothing blocks the draw', async () => {
    const comp = makeCompetition({ courts: ['A', 'B'], format: 'playoffs' });
    const { container } = await mountSection('overview', { comp, tournament: { courts: ['A', 'B', 'C'] } });
    expect(container.querySelector('[data-testid="step-settings-cta"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="step-generate-cta"]')).toBeNull();
  });
});
