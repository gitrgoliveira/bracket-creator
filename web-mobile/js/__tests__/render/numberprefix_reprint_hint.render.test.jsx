import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { installSettingsHarness, mountSettings } from './settings_mount_harness.jsx';
import { HINT_NUMBER_PREFIX } from '../../competition_shape.jsx';

// bc-pnum A3/A7/D7: the number-prefix field on the Settings screen.
//
//  - G4b: the field is NEVER disabled, in any status, unlike every other
//    output-affecting field on this screen -- RenumberCompetitors rewrites
//    pools.csv in place after a save, so a prefix change never invalidates
//    an existing draw the way format/courts/pool sizing would.
//  - The reprint warning fires only once a draw exists AND the pending value
//    differs from the stored one AND is not blank.
//  - A7: Swiss competitors carry no number at all (RenumberCompetitors is a
//    permanent no-op for Swiss), so a running Swiss competition must never
//    show the warning, even with a changed, non-blank prefix.

installSettingsHarness();

const noop = () => {};

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
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
    withZekkenName: false,
    engi: false,
    roundRobin: true,
    poolFormat: 'full',
    numberPrefix: 'K',
    ...overrides,
  };
}

const prefixInput = (container) => container.querySelector('input[placeholder="e.g. A"]');
const prefixHint = (container) => prefixInput(container)?.parentElement.querySelector('.field__hint');

describe('AdminCompetition settings number-prefix field (bc-pnum A3/A7/D7)', () => {
  it('is never disabled, even for a running (draw-ready) competition', async () => {
    const { container } = await mountSettings(makeCompetition({ status: 'draw-ready' }), noop);
    expect(prefixInput(container).disabled).toBe(false);
  });

  it('shows the plain hint at mount, untouched', async () => {
    const { container } = await mountSettings(makeCompetition({ status: 'draw-ready' }), noop);
    expect(prefixHint(container).textContent).toBe(HINT_NUMBER_PREFIX);
    expect(prefixHint(container).textContent).not.toContain('reprinted');
  });

  it('appends the reprint warning once a draw-ready MIXED competition\'s prefix is changed', async () => {
    const { container } = await mountSettings(makeCompetition({ status: 'draw-ready', numberPrefix: 'K' }), noop);
    await act(async () => {
      fireEvent.change(prefixInput(container), { target: { value: 'X' } });
    });
    expect(prefixHint(container).textContent).toContain('renumbered');
    expect(prefixHint(container).textContent).toContain('reprinted');
  });

  it('clearing the field back to blank returns to the base hint', async () => {
    const { container } = await mountSettings(makeCompetition({ status: 'draw-ready', numberPrefix: 'K' }), noop);
    await act(async () => {
      fireEvent.change(prefixInput(container), { target: { value: 'X' } });
    });
    expect(prefixHint(container).textContent).toContain('reprinted');
    await act(async () => {
      fireEvent.change(prefixInput(container), { target: { value: '' } });
    });
    expect(prefixHint(container).textContent).not.toContain('reprinted');
  });

  // bc-pnum A7: the core fix. Same shape as the mixed case above (running,
  // changed, non-blank prefix) but Swiss, where the warning describes a
  // renumber that can never happen.
  it('never shows the reprint warning for a running Swiss competition', async () => {
    const { container } = await mountSettings(
      makeCompetition({ status: 'pools', format: 'swiss', swissRounds: 4, numberPrefix: 'K' }),
      noop
    );
    await act(async () => {
      fireEvent.change(prefixInput(container), { target: { value: 'X' } });
    });
    expect(prefixHint(container).textContent).not.toContain('reprinted');
  });
});
