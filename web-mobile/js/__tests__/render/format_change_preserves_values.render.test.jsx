import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// bc-symm-settings-create-parity: pins the browser-reproduced defect and its
// fix. AdminSettings's Format pills used to call updateFormat, which ran
// normalizeConfigForFormat (competition_shape.jsx) and immediately STAGED
// the derived poolSize/poolWinners/extraQualifiers into `local` -- so the
// operator's own values were overwritten before Save was ever clicked.
//
// Reproduced on a stored mixed competition (poolSize: 4, poolWinners: 2):
//   1. Tap "Knockout only" -> normalizeConfigForFormat stages poolSize: 0,
//      poolWinners: 0.
//   2. Tap "Pools + Knockout" to go straight back -> going INTO mixed is a
//      no-op for those fields (normalizeConfigForFormat only clears them on
//      the way OUT of "mixed"), so they stay 0.
//   3. Net zero change to format, but the form now shows "Players per pool
//      = 0", "Winners per pool = 0", and Save is BLOCKED by
//      blockingPoolSettingsErr.
//
// The fix: updateFormat now stages ONLY `format` (admin_competition_
// settings.jsx). Normalization moved to the PUT-payload boundary in
// saveNow (the `shaped` value), so it still genuinely happens on an actual
// Save -- and pendingConfigClears (competition_shape.jsx) tells the
// operator what that save would clear, rendered as a non-blocking notice
// directly under the Format/Kind controls.
//
// Harness copied from kind_flip_reconciliation.render.test.jsx /
// pool_settings_error_gate.render.test.jsx (same mount-through-
// AdminCompetition shape; AdminSettings is module-internal so it isn't
// reachable directly), including their STUBBED_GLOBALS and the
// getScheduleClashes stub saveNow's post-save clash check needs.

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

// A stored mixed competition, poolSize 4 / poolWinners 2 -- the exact
// values the browser reproduction started from.
function makeMixedCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Spring Cup',
    status: 'setup',
    format: 'mixed',
    kind: 'individual',
    teamSize: 0,
    teamMatchType: 'fixed',
    poolSize: 4,
    poolSizeMode: 'min',
    poolWinners: 2,
    extraQualifiers: '',
    players: [],
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

const byText = (container, tag, text) =>
  Array.from(container.querySelectorAll(tag)).find((el) => el.textContent.trim() === text);

const saveButtons = (container) =>
  Array.from(container.querySelectorAll('button')).filter((b) => b.textContent.trim() === 'Save changes');

const fieldInput = (container, labelText) => {
  const label = Array.from(container.querySelectorAll('label')).find((l) => l.textContent.trim() === labelText);
  return label ? label.parentElement.querySelector('input') : null;
};

describe('bc-symm-settings-create-parity: a format round trip does not destroy staged pool settings', () => {
  it('Knockout only -> Pools + Knockout leaves poolSize/poolWinners exactly as they were, and Save stays enabled', async () => {
    const { container } = await mountSettings(makeMixedCompetition(), noop);

    // Sanity: the stored values are showing before any click.
    expect(fieldInput(container, 'Players per pool').value).toBe('4');
    expect(fieldInput(container, 'Winners per pool').value).toBe('2');

    const knockoutOnlyPill = byText(container, 'button', 'Knockout only');
    expect(knockoutOnlyPill, 'expected a "Knockout only" Format pill').toBeDefined();
    await act(async () => { fireEvent.click(knockoutOnlyPill); });

    // The pool-size fields are gone entirely once format !== "mixed" -- this
    // is the state the old defect left unrecoverable from.
    expect(fieldInput(container, 'Players per pool')).toBeNull();
    expect(fieldInput(container, 'Winners per pool')).toBeNull();

    const mixedPill = byText(container, 'button', 'Pools + Knockout');
    expect(mixedPill, 'expected a "Pools + Knockout" Format pill').toBeDefined();
    await act(async () => { fireEvent.click(mixedPill); });

    // The regression assertion. Under the old updateFormat (which staged
    // normalizeConfigForFormat's result on every tap), poolSize/poolWinners
    // would now read 0: this fails if that behaviour comes back.
    const poolSizeInput = fieldInput(container, 'Players per pool');
    const poolWinnersInput = fieldInput(container, 'Winners per pool');
    expect(poolSizeInput, 'expected "Players per pool" to render once format is back to mixed').not.toBeNull();
    expect(
      poolSizeInput.value,
      'Players per pool was clobbered to 0 by the round trip -- updateFormat must stage only `format`, ' +
      'not re-stage normalizeConfigForFormat\'s result on every tap'
    ).toBe('4');
    expect(
      poolWinnersInput.value,
      'Winners per pool was clobbered to 0 by the round trip'
    ).toBe('2');

    // And Save must not be wedged: a net-zero format change with untouched,
    // still-valid pool values must not trip blockingPoolSettingsErr.
    const buttons = saveButtons(container);
    expect(buttons.length).toBe(2);
    for (const b of buttons) {
      expect(
        b.disabled,
        'Save was blocked after a format round trip that changed nothing about the stored pool settings'
      ).toBe(false);
    }
  });

  it('shows the pending-clears notice after "Knockout only", naming both fields, without blocking Save', async () => {
    const { container } = await mountSettings(makeMixedCompetition(), noop);

    expect(container.querySelector('[data-testid="config-clears-notice"]')).toBeNull();

    const knockoutOnlyPill = byText(container, 'button', 'Knockout only');
    await act(async () => { fireEvent.click(knockoutOnlyPill); });

    const notice = container.querySelector('[data-testid="config-clears-notice"]');
    expect(notice, 'expected the config-clears-notice once "Knockout only" is staged over a mixed competition with poolSize 4 / poolWinners 2').not.toBeNull();
    expect(notice.textContent).toContain('Players per pool');
    expect(notice.textContent).toContain('Winners per pool');
    // The values about to be lost are named, not just the labels.
    expect(notice.textContent).toContain('4');
    expect(notice.textContent).toContain('2');

    // A WARNING, not a blocker: format was staged and poolSize/poolWinners
    // are untouched (still the valid stored 4/2), so poolSettingsError is
    // null and Save must stay enabled.
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'the pending-clears notice must not block Save -- it is a warning, not an error').toBe(false);
    }
  });

  it('Save from that state sends poolSize: 0 and poolWinners: 0 -- moving normalization to the payload boundary did not stop the clear', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const { container } = await mountSettings(makeMixedCompetition(), onUpdate);

    const knockoutOnlyPill = byText(container, 'button', 'Knockout only');
    await act(async () => { fireEvent.click(knockoutOnlyPill); });

    const save = byText(container, 'button', 'Save changes');
    expect(save, 'expected an enabled "Save changes" button after switching to Knockout only').toBeDefined();
    expect(save.disabled).toBe(false);
    await act(async () => { fireEvent.click(save); });

    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    const payload = onUpdate.mock.calls[0][0];
    expect(payload.format).toBe('playoffs');
    expect(
      payload.poolSize,
      'the PUT must still clear poolSize for a knockout-only competition even though `local` no longer holds 0 -- ' +
      'normalization runs at the payload boundary (saveNow\'s `shaped`), not on the pill tap'
    ).toBe(0);
    expect(
      payload.poolWinners,
      'the PUT must still clear poolWinners for a knockout-only competition'
    ).toBe(0);
  });
});
