import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { installSettingsHarness, mountSettings } from './settings_mount_harness.jsx';

// bc-symm: pins the wiring behind a browser-verified gap this PR itself made
// reachable. normalizePoolConfig (internal/mobileapp/handlers_competition.go)
// zeroes poolSize/poolWinners on every stored league/knockout competition;
// normalizeConfigForFormat (competition_shape.jsx) only clears those fields
// on the way OUT of "mixed", so flipping such a competition back INTO
// "mixed" on the Settings screen is a no-op for them. Before the fix, the
// "Players per pool" field then showed 0, both Save buttons stayed enabled,
// and a click took an HTTP 400 whose raw server string ("mixed format
// requires a pool size of at least 1") reached the operator verbatim -- the
// exact combination the create form already refused client-side.
//
// competition_shape.test.jsx proves poolSettingsError itself is correct in
// isolation. It does NOT prove the Settings screen's Save button and inline
// error actually route through it -- this file closes that gap by driving
// the real component: mount AdminSettings with a stored knockout
// competition, click the "Pools + Knockout" pill, and assert Save disables
// and the error appears on screen; then repair the values and assert Save
// re-enables.
//
// The second describe block below is the more load-bearing case: a
// competition that ALREADY carries an invalid poolSize on disk (hand-edited
// config.md, or written before this guard existed) must stay editable for
// every OTHER field. Gating Save on the bare error rather than on
// `(formatChanged || poolFieldsChanged)` would lock such an operator out of
// the whole settings form over a value they never touched this session --
// exactly the lockout `savedCourtsErr` (the courts equivalent) is written
// to avoid.
//
// The shared harness lives in settings_mount_harness.jsx.

installSettingsHarness();

const noop = () => {};

// courts: ['A'] (a single shiaijo) is legal for every format regardless of
// venue size (shiaijoCountError short-circuits at n <= 1), which keeps
// blockingCourtsErr out of these tests entirely -- the only thing under
// test is the pool-settings gate.
function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
    status: 'setup',
    format: 'knockout',
    kind: 'individual',
    teamSize: 0,
    teamMatchType: 'fixed',
    poolSize: 0,
    poolSizeMode: 'min',
    poolWinners: 0,
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

const byText = (container, tag, text) =>
  Array.from(container.querySelectorAll(tag)).find((el) => el.textContent.trim() === text);

const saveButtons = (container) =>
  Array.from(container.querySelectorAll('button')).filter((b) => b.textContent.trim() === 'Save changes');

const fieldInput = (container, labelText) => {
  const label = Array.from(container.querySelectorAll('label')).find((l) => l.textContent.trim() === labelText);
  return label ? label.parentElement.querySelector('input') : null;
};

const POOL_ERR = 'Players per pool must be a whole number ≥ 3.';

describe('bc-symm: Settings gates Save on poolSettingsError, change-scoped', () => {
  it('switching a stored knockout competition (poolSize 0) to "Pools + Knockout" blocks both Save buttons and shows the error, until the operator repairs the values', async () => {
    const { container } = await mountSettings(makeCompetition(), noop);

    // Sanity: the pool-size fields are not even rendered yet -- the "mixed"
    // section is gated on local.format === "mixed" and we start on knockout.
    expect(fieldInput(container, 'Players per pool')).toBeNull();

    const mixedPill = byText(container, 'button', 'Pools + Knockout');
    expect(mixedPill, 'expected a "Pools + Knockout" Format pill').toBeDefined();
    expect(mixedPill.disabled).toBe(false);
    await act(async () => { fireEvent.click(mixedPill); });

    // The reproduced state: poolSize/poolWinners stayed 0 across the format
    // flip (normalizeConfigForFormat is a no-op going INTO mixed), so the
    // field now renders showing the invalid stored value.
    const poolSizeInput = fieldInput(container, 'Players per pool');
    expect(poolSizeInput, 'expected "Players per pool" to render once format is mixed').not.toBeNull();
    expect(poolSizeInput.value).toBe('0');

    expect(container.textContent).toContain(POOL_ERR);

    const buttonsAfterFlip = saveButtons(container);
    expect(buttonsAfterFlip.length, 'expected both the header and footer "Save changes" buttons').toBe(2);
    for (const b of buttonsAfterFlip) {
      expect(b.disabled, 'Save must be blocked: the format flip itself made poolSettingsErr non-null').toBe(true);
    }

    // Repair: a valid pool size and winners count.
    const winnersInput = fieldInput(container, 'Winners per pool');
    await act(async () => { fireEvent.change(poolSizeInput, { target: { value: '3' } }); });
    await act(async () => { fireEvent.change(winnersInput, { target: { value: '1' } }); });

    expect(container.textContent).not.toContain(POOL_ERR);
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'Save should re-enable once poolSize/poolWinners are repaired').toBe(false);
    }
  });

  // The lockout-regression guard. A "mixed" competition already sitting on
  // an invalid poolSize (hand-edited config.md, or written before this guard
  // existed) must not lose the ability to save an UNRELATED edit -- exactly
  // the rule savedCourtsErr already follows for the courts equivalent. This
  // is the subtest that actually exercises the `(formatChanged ||
  // poolFieldsChanged)` term: format is untouched (still "mixed") and
  // poolSize/poolWinners are untouched (still the stored 0/0), so
  // blockingPoolSettingsErr must read false even though poolSettingsErr
  // itself is non-null throughout.
  it('a stored mixed competition with an invalid poolSize (0) still allows saving an unrelated field, e.g. the display name', async () => {
    const { container } = await mountSettings(makeCompetition({ format: 'mixed', poolSize: 0, poolWinners: 0 }), noop);

    // The error is shown on load: rendering it is NOT gated on any change
    // (poolSettingsErr is rendered unconditionally in the mixed block),
    // only Save is.
    expect(container.textContent).toContain(POOL_ERR);
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'nothing has been edited yet; Save should be disabled by !isDirty, not by the pool error').toBe(true);
    }

    const nameInput = fieldInput(container, 'Display name');
    expect(nameInput, 'expected a "Display name" input').not.toBeNull();
    await act(async () => { fireEvent.change(nameInput, { target: { value: 'Autumn Cup (renamed)' } }); });

    // The error is still on screen -- poolSize is still 0 -- but Save must
    // be live: the operator only touched the name, so
    // (formatChanged || poolFieldsChanged) is false and blockingPoolSettingsErr
    // must not veto this save.
    expect(container.textContent).toContain(POOL_ERR);
    const buttonsAfterNameEdit = saveButtons(container);
    expect(buttonsAfterNameEdit.length).toBe(2);
    for (const b of buttonsAfterNameEdit) {
      expect(
        b.disabled,
        'Save was blocked by the pre-existing invalid poolSize even though the operator only edited the name -- ' +
        'this is the lockout blockingPoolSettingsErr\'s change-scoping exists to prevent'
      ).toBe(false);
    }
  });
});
