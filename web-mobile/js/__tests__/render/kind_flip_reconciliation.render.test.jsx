import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// bc-symm Phase 5: pins the WIRING behind a review-round defect, already
// fixed in competition_shape.jsx's `normalizeConfigForKind` (its own doc
// comment has the full field-by-field rationale) and `AdminSettings`'s
// `updateKind` handler (admin_competition_settings.jsx). The defect: flipping
// `kind` on the Settings page used to stage a config the server rejects, with
// no control left on screen to repair it before the operator hits Save.
//
//   team -> individual left `teamSize: 5`, which ValidateCompetitionTeamSize
//   rejects, while teamFieldsVisible had just HIDDEN the Team size input --
//   so the 400 came back with nothing on screen the operator could fix.
//
//   team(kachinuki) -> individual left `teamMatchType: "kachinuki"` with
//   teamSize 0, rejected by ValidateTeamMatchType.
//
// `normalizeConfigForKind` itself is pinned by unit tests in
// competition_shape.test.jsx; those prove the FUNCTION is correct in
// isolation. They do NOT prove the kind pills on the Settings page actually
// CALL it -- a regression that re-wired the pills to a raw
// `update("kind", ...)` (bypassing normalization entirely) would leave every
// one of those unit tests green. This file closes that gap by driving the
// real component: mount AdminSettings with a team(kachinuki) competition,
// click the "Individual" pill, click Save, and assert the PUT payload the
// operator's click actually produced carries the repaired
// `teamMatchType: "fixed"` -- proving the wiring end to end, not just the
// function in isolation.
//
// KNOWN GAP, reported rather than fixed here (out of this file's permitted
// scope -- admin_competition_settings.jsx -- per this bead's Phase 5
// instructions): the round-trip cannot currently also assert
// `teamSize: 0` the same way. `AdminSettings.saveNow` builds the PUT body's
// teamSize via `safeInt(effective.teamSize, latestC.teamSize)`
// (admin_competition_settings.jsx, ~line 313), and safeInt's guard is
// `v >= 1`. That floor exists to protect a CLEARED number input (NaN) from
// clobbering the last-saved value on an unrelated save (see safeInt's own
// comment) -- it predates normalizeConfigForKind and was never taught that a
// kind flip can deliberately stage a legitimate `0`. The result: even when
// updateKind correctly stages `local.teamSize = 0`, safeInt rejects that 0 as
// "not a usable positive integer" and silently substitutes the STALE
// `latestC.teamSize` back into the PUT body -- reproducing the exact defect
// this bead's fix was supposed to close, one layer downstream of where the
// fix lives. Confirmed live: mounting this component, clicking "Individual",
// and clicking Save currently sends `teamSize: 5` (the stale value) to
// `onUpdate`, not `0`. Confirmed NOT a wiring bug: pointing the kind pills at
// a raw `update("kind", ...)` (bypassing normalizeConfigForKind entirely)
// produces the exact same `teamSize: 5` in the payload -- because with the
// wiring broken, `effective.teamSize` is simply the untouched original `5`,
// which safeInt correctly lets through unchanged. Both the correctly-wired
// and the incorrectly-wired code paths currently produce an IDENTICAL
// `teamSize: 5` PUT body, for two different reasons -- so a `teamSize`
// assertion here cannot currently distinguish "the wiring is correct" from
// "the wiring is broken", and would not be a guard for either. teamMatchType
// has no such collision (its merge is `effective.X || latestC.X || ""`, not
// safeInt), which is why the assertion below uses it instead. See the PR /
// task report for the suggested fix (a safeInt variant that allows 0 through
// the way the sibling safeNonNegInt already does for the duration fields).
//
// Harness copied from qualifier_settings_save.render.test.jsx (same
// mount-through-AdminCompetition shape, AdminSettings is module-internal so
// it isn't reachable directly), including its STUBBED_GLOBALS and the
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
  competitionKindLabel: () => 'Team',
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

// A team competition with NO roster: kindChangeBlockedReason(playerCount)
// only unblocks the kind pills at playerCount <= 0
// (competition_shape.jsx), so an empty roster is required for this test to
// be able to click "Individual" at all -- a roster-bearing fixture would
// pin nothing (the pills would be disabled and the click a no-op).
function makeTeamCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Team Cup',
    status: 'setup',
    format: 'mixed',
    kind: 'team',
    teamSize: 5,
    // Kachinuki, not "fixed": normalizeConfigForKind's individual branch
    // always writes "fixed", so starting from "fixed" would make the flip a
    // no-op for this field (update() only stages a field whose normalized
    // value actually differs from local) and the assertion below would pass
    // whether or not the wiring runs at all. Starting from "kachinuki" makes
    // "fixed" in the PUT body proof the reconciliation actually ran.
    teamMatchType: 'kachinuki',
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

const byText = (container, text) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

describe('bc-symm: the Settings kind pills route through normalizeConfigForKind', () => {
  it('team(kachinuki) -> individual repairs teamMatchType to "fixed" before the PUT, not just in the hidden field', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const { container } = await mountSettings(makeTeamCompetition(), onUpdate);

    // Sanity: the Team size AND Team match format controls are visible
    // before the flip, so what follows is exercising a real
    // hide-the-controls scenario, not a fixture that was already individual.
    const teamSizeInput = container.querySelector('input[type="number"][max]');
    expect(teamSizeInput, 'expected the Team size input to be present for a team competition').not.toBeNull();
    expect(teamSizeInput.value).toBe('5');
    expect(byText(container, 'Kachinuki (winner stays on)'), 'expected the Team match format pills, with Kachinuki as the stored pick').toBeDefined();

    const individualPill = byText(container, 'Individual');
    expect(individualPill, 'expected an "Individual" kind pill').toBeDefined();
    expect(
      individualPill.disabled,
      '"Individual" pill is disabled: the fixture must have an empty roster (kindChangeBlockedReason gates on playerCount <= 0) for this click to do anything'
    ).toBe(false);
    await act(async () => { fireEvent.click(individualPill); });

    // Both team-only controls disappear immediately (teamFieldsVisible(kind))
    // -- this is the state the defect left the operator in: the values
    // staged underneath are now unreachable from this screen, so whatever
    // the PUT body ends up carrying for them can no longer be fixed by hand.
    expect(container.querySelector('input[type="number"][max]')).toBeNull();
    expect(byText(container, 'Kachinuki (winner stays on)')).toBeUndefined();

    const save = byText(container, 'Save changes');
    expect(save, 'expected an enabled "Save changes" button after the kind flip').toBeDefined();
    expect(save.disabled, 'Save stayed disabled after clicking "Individual": the flip was not registered as a change').toBe(false);
    await act(async () => { fireEvent.click(save); });

    await waitFor(() => expect(onUpdate).toHaveBeenCalled());
    const payload = onUpdate.mock.calls[0][0];
    expect(payload.kind).toBe('individual');
    // The discriminating assertion. teamMatchType's merge in saveNow is
    // `effective.teamMatchType || latestC.teamMatchType || ""` -- unlike
    // teamSize's safeInt-guarded merge, this is not floor-guarded, so a
    // correctly staged "fixed" survives to the PUT body unmolested. A
    // regression that re-wires the kind pills to a raw `update("kind", ...)`
    // (bypassing normalizeConfigForKind, and therefore never staging
    // teamMatchType at all) leaves this at the stored "kachinuki" instead,
    // which ValidateTeamMatchType rejects below teamSize 2 -- so this line
    // fails whenever the WIRING regresses, not merely when the function
    // does (which competition_shape.test.jsx already covers in isolation).
    expect(
      payload.teamMatchType,
      'the PUT still carries the stale teamMatchType "kachinuki": ValidateTeamMatchType on the server rejects ' +
      'kachinuki below teamSize 2, so this 400s with no control left on screen able to fix it -- the kind pills ' +
      'must route through normalizeConfigForKind (competition_shape.jsx) via updateKind, not a raw ' +
      'update("kind", ...)'
    ).toBe('fixed');

    // teamSize IS asserted, and only became assertable once the serializer
    // was fixed. saveNow used to build this field with safeInt (floor >= 1),
    // which discarded normalizeConfigForKind's legitimate 0 and re-sent the
    // stored 5 -- so a correct wiring and a broken one both produced 5 and an
    // assertion here would have guarded nothing. teamSize now goes through
    // safeNonNegInt (the >= 0 sibling already used by the duration fields),
    // because 0 is exactly what ValidateCompetitionTeamSize REQUIRES of a
    // non-team kind. With that, 0 vs 5 discriminates the two cases and this
    // pins the whole chain: pill -> updateKind -> normalizeConfigForKind ->
    // serializer -> wire.
    expect(
      payload.teamSize,
      'the PUT carried a non-zero teamSize for an individual competition: ValidateCompetitionTeamSize ' +
      'rejects that outright, and the Team size input is hidden by then, so the operator cannot repair it. ' +
      'Either the kind pills stopped routing through updateKind, or teamSize regressed to a >=1-floored serializer.'
    ).toBe(0);
  });
});
