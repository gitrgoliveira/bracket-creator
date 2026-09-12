import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { installSettingsHarness, mountSettings, makeSettingsCompetition } from './settings_mount_harness.jsx';

// bc-symm-settings-create-parity, review round. Five defects the Format/Kind
// editors on the Settings screen made reachable, each driven through the
// real component rather than asserted against a helper in isolation --
// competition_shape.test.jsx already proves the helpers; what these pin is
// that the SCREEN routes through them.
//
//   1. A cleared "Team size" saved 5, not the last-saved value. The guard
//      was `safeNonNegInt(shaped.teamSize, latestC.teamSize)`, applied to
//      normalizeConfigForKind's OUTPUT -- which is always a finite integer,
//      so it never fired. resolveTeamSize replaced it, and then a later
//      round found that the fallback was ALSO the operator's only feedback:
//      an invalid Team size is now refused visibly instead of discarded
//      quietly (see that describe block).
//   2. The pendingConfigClears notice named a value the save would not send.
//   3. Switching to "Swiss" on a record with no round count left Save live
//      and took validateSwissConfig's raw server string.
//   4. A stored team competition carrying withZekkenName had it forced false
//      by a save that touched only the start time -- a 409 at draw-ready.
//   5. The "Pool size is a" pills lit NEITHER for a stored poolSizeMode of
//      "" (which nothing on the server fills in), while the draw ran
//      minimum sizing.
//
// The shared harness lives in settings_mount_harness.jsx.

installSettingsHarness({ competitionKindLabel: () => 'Team' });

const noop = () => {};

// No differences from the shared settings fixture.
function makeCompetition(overrides = {}) {
  return makeSettingsCompetition(overrides);
}

const byText = (container, tag, text) =>
  Array.from(container.querySelectorAll(tag)).find((el) => el.textContent.trim() === text);

const saveButtons = (container) =>
  Array.from(container.querySelectorAll('button')).filter((b) => b.textContent.trim() === 'Save changes');

const fieldInput = (container, labelText) => {
  const label = Array.from(container.querySelectorAll('label')).find((l) => l.textContent.trim() === labelText);
  return label ? label.parentElement.querySelector('input') : null;
};

// The screen has no "did you PUT this" seam other than onUpdate, which
// receives the finalNext payload verbatim.
async function saveAndCapture(container) {
  const sent = [];
  const btn = saveButtons(container).find((b) => !b.disabled);
  expect(btn, 'expected an enabled "Save changes" button').toBeDefined();
  await act(async () => { fireEvent.click(btn); });
  return sent;
}

// A cleared or under-floor "Team size" is REFUSED, visibly, rather than
// quietly discarded. resolveTeamSize (competition_shape.jsx) still resolves
// the payload -- it is the last line of defence, unit-tested there, and it
// is what stops a NaN reaching the wire on a save the operator IS allowed
// to make -- but it was never meant to be the operator's only feedback.
// While it was, typing 1 into Team size and clicking Save stored the OLD
// size and reported "✓ Saved at HH:MM:SS": the screen claimed to have
// saved a number it had thrown away. The create form has refused the same
// value at submit all along, so this was also a create/settings divergence.
//
// These three subtests previously asserted the silent fallback FROM the
// save payload, which is why they now read the opposite way round: the
// question is no longer "what did a save send" but "was a save offered at
// all".
describe('bc-symm review: a cleared or under-floor Team size blocks Save with a visible error', () => {
  const TEAM_SIZE_ERR = 'Team size must be a whole number ≥ 2.';

  const expectBlocked = (container, why) => {
    const buttons = saveButtons(container);
    expect(buttons.length, 'expected both the header and footer "Save changes" buttons').toBe(2);
    for (const b of buttons) expect(b.disabled, why).toBe(true);
    expect(container.textContent, 'the operator must be told WHY, not just left with a dead button').toContain(TEAM_SIZE_ERR);
  };

  it('clearing "Team size" on a stored team competition blocks the save instead of silently keeping the stored size', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    const teamSizeInput = fieldInput(container, 'Team size');
    expect(teamSizeInput, 'expected a "Team size" input on a team competition').not.toBeNull();
    expect(teamSizeInput.value).toBe('3');

    await act(async () => { fireEvent.change(teamSizeInput, { target: { value: '' } }); });
    expect(teamSizeInput.value, 'a cleared number input must render empty, not collapse to 0').toBe('');

    expectBlocked(container, 'a cleared Team size must block Save; saving the stored 3 under a "✓ Saved" is the silent discard this closes');
    expect(sent, 'nothing may be PUT while the field is invalid').toBeNull();

    // And the way out works: a valid size re-enables Save and is what lands.
    await act(async () => { fireEvent.change(teamSizeInput, { target: { value: '4' } }); });
    expect(container.textContent).not.toContain(TEAM_SIZE_ERR);
    await saveAndCapture(container);
    expect(sent.teamSize).toBe(4);
  });

  // The discriminating case for the ORDER of the two guards. With no
  // format/kind change staged, shapeConfigForSave passes the config through
  // untouched; stage a format flip and normalizeConfigForKind's under-floor
  // branch would turn a cleared NaN into DEFAULT_TEAM_SIZE. Save being
  // blocked means that branch is never reached with the operator's cleared
  // input, and resolveTeamSize's ordering keeps it unreachable even if a
  // later change re-enabled the button.
  it('clearing "Team size" while ALSO staging a format flip blocks the save', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'playoffs' });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '' } }); });

    const mixedPill = byText(container, 'button', 'Pools + Knockout');
    expect(mixedPill, 'expected a "Pools + Knockout" Format pill').toBeDefined();
    await act(async () => { fireEvent.click(mixedPill); });
    // Repair the pool fields the flip invalidated, so the ONLY remaining
    // blocker is the team size under test.
    await act(async () => { fireEvent.change(fieldInput(container, 'Players per pool'), { target: { value: '4' } }); });
    await act(async () => { fireEvent.change(fieldInput(container, 'Winners per pool'), { target: { value: '2' } }); });

    expectBlocked(container, 'a cleared Team size must block Save even when the pool fields have been repaired');
    expect(sent).toBeNull();
  });

  it('typing a 1 -- below the field\'s own min -- is refused the same way', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '1' } }); });
    expectBlocked(container, 'teamSize 1 is rejected unconditionally by ValidateCompetitionTeamSize, so the client must not offer to send it');
    expect(sent).toBeNull();
  });

  // The change-scoping, and the reason resolveTeamSize's fallback is still
  // load-bearing. A record already carrying an invalid team size (a
  // hand-edited config.md) must not have every unrelated edit blocked by a
  // value the operator never touched -- the same rule savedCourtsErr and
  // blockingPoolSettingsErr follow. The error is still on screen; only Save
  // is unblocked.
  it('a stored team competition with an invalid teamSize still allows saving an unrelated field', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 0, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    expect(container.textContent, 'the stored value is invalid, so the error renders on load').toContain(TEAM_SIZE_ERR);
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'nothing edited yet: Save is disabled by !isDirty, not by the team-size error').toBe(true);
    }

    await act(async () => { fireEvent.change(fieldInput(container, 'Display name'), { target: { value: 'Autumn Cup (renamed)' } }); });

    expect(container.textContent).toContain(TEAM_SIZE_ERR);
    for (const b of saveButtons(container)) {
      expect(
        b.disabled,
        'the operator only edited the name; blocking that on a pre-existing invalid team size is the lockout the change-scoping prevents'
      ).toBe(false);
    }
    await saveAndCapture(container);
    expect(sent.name).toBe('Autumn Cup (renamed)');
  });

  it('a real edit still saves what the operator typed', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '5' } }); });
    await saveAndCapture(container);
    expect(sent.teamSize).toBe(5);
  });
});

describe('bc-symm review: the clears notice never names a value the save will not send', () => {
  it('flipping an individual competition to Team after typing a 1 does not announce "Team size (1)" as about to be cleared', async () => {
    const comp = makeCompetition({ kind: 'individual', teamSize: 0, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, noop);

    const teamPill = byText(container, 'button', 'Team');
    expect(teamPill, 'expected a "Team" Kind pill').toBeDefined();
    await act(async () => { fireEvent.click(teamPill); });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '1' } }); });

    // The save sends neither 1 nor a clear -- it sends the resolved value.
    // Announcing "Team size (1)" under a heading that says it will be
    // CLEARED was wrong twice over: team size does apply to a team, and 1 is
    // not what was about to be lost.
    expect(container.textContent).not.toContain('Team size (1)');
  });
});

describe('bc-symm review: Settings gates Save on swissSettingsError, change-scoped', () => {
  const SWISS_ERR = 'Number of Swiss rounds must be a whole number ≥ 1.';

  it('switching a stored playoffs competition (swissRounds 0) to "Swiss" blocks Save and shows the error, until repaired', async () => {
    const { container } = await mountSettings(makeCompetition(), noop);

    const swissPill = byText(container, 'button', 'Swiss');
    expect(swissPill, 'expected a "Swiss" Format pill').toBeDefined();
    await act(async () => { fireEvent.click(swissPill); });

    const roundsInput = fieldInput(container, 'Number of Swiss rounds');
    expect(roundsInput, 'expected the Swiss rounds field once format is swiss').not.toBeNull();
    expect(roundsInput.value).toBe('0');
    expect(container.textContent).toContain(SWISS_ERR);

    const after = saveButtons(container);
    expect(after.length, 'expected both the header and footer "Save changes" buttons').toBe(2);
    for (const b of after) {
      expect(b.disabled, 'Save must be blocked: the format flip itself made swissSettingsErr non-null').toBe(true);
    }

    await act(async () => { fireEvent.change(roundsInput, { target: { value: '4' } }); });
    expect(container.textContent).not.toContain(SWISS_ERR);
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'Save should re-enable once a valid round count is typed').toBe(false);
    }
  });

  // The lockout-regression guard, same shape as the pool gate's: a stored
  // swiss competition already carrying 0 must stay editable for every other
  // field, or an operator is locked out of the whole form over a value they
  // never touched this session.
  it('a stored swiss competition with swissRounds 0 still allows saving an unrelated field', async () => {
    const { container } = await mountSettings(makeCompetition({ format: 'swiss', swissRounds: 0 }), noop);

    const nameInput = fieldInput(container, 'Display name');
    await act(async () => { fireEvent.change(nameInput, { target: { value: 'Autumn Cup 2' } }); });
    for (const b of saveButtons(container)) {
      expect(b.disabled, 'an untouched invalid swissRounds must not block an unrelated edit').toBe(false);
    }
  });
});

describe('bc-symm review: a save that stages no format/kind change re-shapes nothing', () => {
  it('a stored team competition carrying withZekkenName keeps it through a start-time-only save', async () => {
    let sent = null;
    const comp = makeCompetition({
      kind: 'team', teamSize: 5, withZekkenName: true, format: 'mixed', poolSize: 4, poolWinners: 2,
    });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    const startInput = container.querySelector('input[type="time"]');
    expect(startInput, 'expected the start-time input').not.toBeNull();
    await act(async () => { fireEvent.change(startInput, { target: { value: '10:30' } }); });

    await saveAndCapture(container);
    expect(sent).not.toBeNull();
    expect(sent.startTime).toBe('10:30');
    expect(
      sent.withZekkenName,
      'an unscoped normalization forced this false, and withZekkenName is in the PUT\'s output-affecting set -- at draw-ready that 409s the operator\'s own unrelated edit, every time, with the checkbox disabled'
    ).toBe(true);
  });

  it('but a staged kind change still clears what does not apply', async () => {
    let sent = null;
    const comp = makeCompetition({
      kind: 'team', teamSize: 5, teamMatchType: 'kachinuki', withZekkenName: true,
      format: 'mixed', poolSize: 4, poolWinners: 2,
    });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    const individualPill = byText(container, 'button', 'Individual');
    expect(individualPill, 'expected an "Individual" Kind pill').toBeDefined();
    await act(async () => { fireEvent.click(individualPill); });

    await saveAndCapture(container);
    expect(sent.teamSize, 'ValidateCompetitionTeamSize requires exactly 0 for a non-team kind').toBe(0);
    expect(sent.teamMatchType).toBe('fixed');
  });
});

describe('bc-symm review: "Pool size is a" resolves a stored empty value', () => {
  it('lights "minimum" -- what the engine does with "" -- rather than neither pill', async () => {
    const { container } = await mountSettings(
      makeCompetition({ format: 'mixed', poolSize: 4, poolWinners: 2, poolSizeMode: '' }), noop);

    const maxPill = byText(container, 'button', 'maximum');
    const minPill = byText(container, 'button', 'minimum');
    expect(maxPill, 'expected a "maximum" pill').toBeDefined();
    expect(minPill, 'expected a "minimum" pill').toBeDefined();
    expect(maxPill.className).not.toContain('is-active');
    expect(
      minPill.className,
      'every engine consumer spells the test `isMax := PoolSizeMode == "max"`, so "" IS minimum sizing -- lighting neither pill told the operator nothing while the draw went ahead'
    ).toContain('is-active');
  });

  it('shows the "Knockout qualifiers" radio for a stored empty value, matching what the server would accept', async () => {
    const { container } = await mountSettings(
      makeCompetition({ format: 'mixed', poolSize: 4, poolWinners: 1, poolSizeMode: '' }), noop);
    expect(container.textContent).toContain('Knockout qualifiers');
  });
});

describe('bc-symm review: the round-robin guard wraps its own .field', () => {
  it('renders no empty .field when the checkbox is hidden', async () => {
    const { container } = await mountSettings(makeCompetition({ format: 'league' }), noop);
    const empties = Array.from(container.querySelectorAll('.field')).filter((el) => el.childElementCount === 0 && !el.textContent.trim());
    expect(
      empties.length,
      'a .field with no children still carries the class\'s own margin; the sibling poolFormatVisible / swissRoundsVisible blocks all gate the wrapper, not just its contents'
    ).toBe(0);
  });

  it('still renders the checkbox, inside a .field, for mixed + non-partial', async () => {
    const { container } = await mountSettings(
      makeCompetition({ format: 'mixed', poolFormat: 'full', poolSize: 4, poolWinners: 2 }), noop);
    const label = Array.from(container.querySelectorAll('label')).find((l) => l.textContent.includes('Round-robin in pools'));
    expect(label, 'expected the round-robin checkbox for mixed + full').toBeDefined();
    expect(label.closest('.field'), 'the checkbox must still sit inside a .field').not.toBeNull();
  });
});
