import { act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { installSettingsHarness, mountSettings } from './settings_mount_harness.jsx';

// bc-symm Phase 5: pins the WIRING behind a review-round defect, already
// fixed in competition_shape.jsx's `normalizeConfigForKind` (its own doc
// comment has the full field-by-field rationale) and in
// admin_competition_settings.jsx: the Kind pills stage the raw value via
// `update("kind", ...)`, and `AdminSettings.saveNow` runs
// normalizeConfigForKind(normalizeConfigForFormat(effective)) once, at the
// PUT payload boundary, into a `shaped` value that teamSize/teamMatchType/
// engi/withZekkenName are all read from (see saveNow's own comment on why
// normalization moved to that one spot instead of running from a pill's
// onClick). The defect: flipping `kind` on the Settings page used to stage
// a config the server rejects, with no control left on screen to repair it
// before the operator hits Save.
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
// feed it -- a regression that re-wired the pills to skip normalization at
// the saveNow boundary would leave every one of those unit tests green.
// This file closes that gap by driving the real component: mount
// AdminSettings with a team(kachinuki) competition, click the "Individual"
// pill, click Save, and assert the PUT payload the operator's click
// actually produced carries the repaired values -- proving the wiring end
// to end, not just the function in isolation.
//
// Both `teamMatchType: "fixed"` AND `teamSize: 0` are asserted below. That
// was NOT always true, and the history is worth keeping: `teamMatchType`'s
// merge (`shaped.teamMatchType || latestC.teamMatchType || ""`) let a
// correctly staged "fixed" survive to the PUT body from the start, but
// teamSize's merge used to be `safeInt(effective.teamSize, latestC.teamSize)`
// with `safeInt`'s `v >= 1` floor -- a guard that predates
// normalizeConfigForKind and was never taught that a kind flip can
// deliberately stage a legitimate `0`. That floor discarded the staged `0`
// and silently substituted the stale `latestC.teamSize` (5) back into the
// PUT body, so a CORRECTLY wired kind flip and an INCORRECTLY wired one
// (one that skipped normalization entirely, leaving `effective.teamSize`
// untouched at 5) produced the exact same `teamSize: 5` PUT body, for two
// different reasons -- a `teamSize` assertion could not have distinguished
// "the wiring is correct" from "the wiring is broken" and would have
// guarded neither. `teamMatchType` had no such collision, which is why the
// assertion originally relied on it alone. What made `teamSize` assertable:
// the serializer's teamSize merge was changed to `safeNonNegInt` (the `>= 0`
// sibling of `safeInt`, already used for the duration fields and for
// poolSize/poolWinners), so the legitimate `0` now survives to the wire and
// 0 vs 5 discriminates the two cases the way `teamMatchType` always could.
//
// The shared harness lives in settings_mount_harness.jsx.

installSettingsHarness({ competitionKindLabel: () => 'Team' });

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
