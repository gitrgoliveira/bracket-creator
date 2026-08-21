import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// bc-qual review round: the "Knockout qualifiers" radio on the competition
// SETTINGS page must be able to save its STANDARD value.
//
// Standard's wire value is the empty string, which is falsy, so the settings
// payload builder's habitual `effective.X || latestC.X || ""` idiom -- correct
// for a field whose empty value means "unset" (mirror, teamMatchType) -- reads
// the operator's explicit "Standard" pick as absent and re-sends the stored
// non-standard value. Two operator-visible failures follow, and this file pins
// both against the PUT payload rather than against the source text:
//
//   1. Picking "Standard" saves nothing: the PUT carries the old value back and
//      the pill snaps to it on the next SSE sync.
//   2. Worse, switching "Pool size is a" to maximum stages poolSizeMode="max"
//      AND extraQualifiers="" together; resurrecting the old value ships the
//      pair state.ValidateExtraQualifiers rejects, so EVERY settings save 400s
//      with no control on the screen able to clear it.
//
// Mounted through the public AdminCompetition entry (AdminSettings is
// module-internal), same harness shape as admin_competition.render.test.jsx.

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
  CourtPicker: Stub('CourtPicker'),
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  BracketTree: Stub('BracketTree'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({
    matches: false,
    addEventListener: noop, removeEventListener: noop,
    addListener: noop, removeListener: noop,
  }),
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptAdminPassword: vi.fn().mockResolvedValue(null),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    swissGenerateRound: vi.fn().mockResolvedValue(null),
    updateCompetitionAwards: vi.fn().mockResolvedValue(null),
    completeCompetition: vi.fn().mockResolvedValue({ status: 'completed' }),
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
    // saveNow runs a post-save clash check before navigating away.
    getScheduleClashes: vi.fn().mockResolvedValue([]),
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

// A mixed competition already saved with a NON-standard qualifier mode: the
// only starting state from which "save Standard" is a change at all.
function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Mudansha',
    status: 'setup',
    format: 'mixed',
    kind: 'individual',
    poolSize: 3,
    poolSizeMode: 'min',
    poolWinners: 1,
    extraQualifiers: 'larger-pools',
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

async function mountSettings(comp, onUpdate) {
  const t = {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [comp, { id: 'c2', name: 'Yudansha' }],
  };
  let result;
  await act(async () => {
    result = render(
      <AdminCompetition
        tournament={t}
        competition={comp}
        pools={[]}
        poolMatches={[]}
        standings={[]}
        bracket={null}
        section="settings"
        onSection={noop}
        onBack={noop}
        onOpenCompetition={noop}
        onUpdate={onUpdate}
        onRefreshCompetition={noop}
        onMoveCourt={noop}
        onEditScore={noop}
        onLogout={noop}
        onViewerMode={noop}
        tweaks={{}}
        password=""
        showToast={noop}
      />
    );
  });
  return result;
}

const byText = (container, text) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

async function clickThenSave(container, label) {
  await act(async () => { fireEvent.click(byText(container, label)); });
  const save = byText(container, 'Save changes');
  expect(save, 'expected an enabled "Save changes" button after the edit').toBeDefined();
  expect(save.disabled, `"Save changes" stayed disabled after clicking "${label}": the edit was not registered as a change`).toBe(false);
  await act(async () => { fireEvent.click(save); });
}

describe('bc-qual: the Settings page can save the Standard qualifier mode', () => {
  it('sends extraQualifiers "" when the operator picks Standard', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const { container } = await mountSettings(makeCompetition(), onUpdate);

    await clickThenSave(container, 'Standard');

    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    const payload = onUpdate.mock.calls[0][0];
    expect(
      payload.extraQualifiers,
      'the PUT resurrected the stored "larger-pools": Standard is unreachable from this screen'
    ).toBe('');
  });

  it('clears extraQualifiers alongside the switch to maximum pool sizing', async () => {
    // The "Knockout qualifiers" radio is hidden under maximum sizing, so this
    // is the one transition where the operator cannot fix the value by hand
    // afterwards: the pair must leave the screen already consistent.
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const { container } = await mountSettings(makeCompetition(), onUpdate);

    await clickThenSave(container, 'maximum');

    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    const payload = onUpdate.mock.calls[0][0];
    expect(payload.poolSizeMode).toBe('max');
    expect(
      payload.extraQualifiers,
      'maximum sizing shipped with a non-standard qualifier mode: state.ValidateExtraQualifiers rejects that pair, so every later settings save 400s'
    ).toBe('');
  });

  it('still round-trips a stored non-standard value when the operator edits something else', async () => {
    // The guard the `|| latestC` fallback was there for: omitting the field
    // JSON-encodes to "" and clobbers the stored mode. `effective` already
    // carries the stored value for an untouched field, so dropping the
    // fallback must not reintroduce that.
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const { container } = await mountSettings(makeCompetition(), onUpdate);

    const nameInput = container.querySelector('input.input');
    expect(nameInput).not.toBeNull();
    await act(async () => { fireEvent.change(nameInput, { target: { value: 'Mudansha Cup' } }); });
    const save = byText(container, 'Save changes');
    await act(async () => { fireEvent.click(save); });

    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    expect(onUpdate.mock.calls[0][0].extraQualifiers).toBe('larger-pools');
  });
});

describe('bc-qual: the page head names the shiaijo an inherited draw would use', () => {
  it('says which courts a competition with none of its own would draw on', async () => {
    // An empty courts list MEANS "inherit the tournament's" (the engine
    // materialises them at draw time). "No shiaijo assigned" alone is true but
    // leaves the operator with no idea the draw runs on all of the venue's --
    // on the very screen that is the documented remedy for shiaijo problems.
    const comp = makeCompetition({ courts: [] });
    const { container } = await mountSettings(comp, vi.fn());
    const head = container.querySelector('.page-head__sub');
    expect(head).not.toBeNull();
    expect(head.textContent).toContain('No shiaijo of its own');
    expect(head.textContent).toContain('A, B');
  });

  it('lists the competition\'s own shiaijo when it has them', async () => {
    const { container } = await mountSettings(makeCompetition(), vi.fn());
    const head = container.querySelector('.page-head__sub');
    expect(head.textContent).toContain('A');
    expect(head.textContent).not.toContain('No shiaijo');
  });
});
