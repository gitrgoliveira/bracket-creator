import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { installSettingsHarness, mountSettings } from './settings_mount_harness.jsx';

// bc-symm-settings-create-parity, review round. Five defects the Format/Kind
// editors on the Settings screen made reachable, each driven through the
// real component rather than asserted against a helper in isolation --
// competition_shape.test.jsx already proves the helpers; what these pin is
// that the SCREEN routes through them.
//
//   1. A cleared "Team size" saved 5, not the last-saved value. The guard
//      was `safeNonNegInt(shaped.teamSize, latestC.teamSize)`, applied to
//      normalizeConfigForKind's OUTPUT -- which is always a finite integer,
//      so it never fired.
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

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
    status: 'setup',
    format: 'playoffs',
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
    withZekkenName: false,
    engi: false,
    roundRobin: true,
    poolFormat: 'full',
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

// The screen has no "did you PUT this" seam other than onUpdate, which
// receives the finalNext payload verbatim.
async function saveAndCapture(container) {
  const sent = [];
  const btn = saveButtons(container).find((b) => !b.disabled);
  expect(btn, 'expected an enabled "Save changes" button').toBeDefined();
  await act(async () => { fireEvent.click(btn); });
  return sent;
}

describe('bc-symm review: a cleared or under-floor Team size falls back to the last-saved value', () => {
  it('clearing "Team size" on a stored team competition sends the stored size, not DEFAULT_TEAM_SIZE', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    const teamSizeInput = fieldInput(container, 'Team size');
    expect(teamSizeInput, 'expected a "Team size" input on a team competition').not.toBeNull();
    expect(teamSizeInput.value).toBe('3');

    await act(async () => { fireEvent.change(teamSizeInput, { target: { value: '' } }); });
    expect(teamSizeInput.value, 'a cleared number input must render empty, not collapse to 0').toBe('');

    await saveAndCapture(container);
    expect(sent, 'expected the settings PUT payload via onUpdate').not.toBeNull();
    expect(
      sent.teamSize,
      'a cleared Team size must fall back to the last-saved 3; saving 5 is the silent default the dead post-shape guard allowed'
    ).toBe(3);
  });

  // The discriminating case for the ORDER of the two guards. With no
  // format/kind change staged, shapeConfigForSave passes the config through
  // untouched, so a post-shape safeNonNegInt would still catch a cleared
  // NaN and the bug hides. Stage a format flip and the normalizer runs: the
  // cleared NaN becomes DEFAULT_TEAM_SIZE before any post-shape guard sees
  // it, and 5 lands on the wire over the operator's 3.
  it('clearing "Team size" while ALSO staging a format flip still sends the stored size', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'playoffs' });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '' } }); });

    const mixedPill = byText(container, 'button', 'Pools + Knockout');
    expect(mixedPill, 'expected a "Pools + Knockout" Format pill').toBeDefined();
    await act(async () => { fireEvent.click(mixedPill); });
    // The flip lands on poolSize 0, which blocks Save -- repair it, since
    // the team size is what is under test here.
    await act(async () => { fireEvent.change(fieldInput(container, 'Players per pool'), { target: { value: '4' } }); });
    await act(async () => { fireEvent.change(fieldInput(container, 'Winners per pool'), { target: { value: '2' } }); });

    await saveAndCapture(container);
    expect(
      sent.teamSize,
      'the staged team size must be resolved BEFORE normalizeConfigForKind, or its under-floor branch turns the cleared input into 5'
    ).toBe(3);
  });

  it('typing a 1 -- below the field\'s own min -- also falls back rather than saving 5', async () => {
    let sent = null;
    const comp = makeCompetition({ kind: 'team', teamSize: 3, format: 'mixed', poolSize: 4, poolWinners: 2 });
    const { container } = await mountSettings(comp, (payload) => { sent = payload; });

    await act(async () => { fireEvent.change(fieldInput(container, 'Team size'), { target: { value: '1' } }); });
    await saveAndCapture(container);
    expect(sent.teamSize).toBe(3);
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
