import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// bc-pnum operator ruling: "Pool by pool, after the draw, is the correct
// way. Remove the provisional numbers, they are confusing." AdminParticipants'
// check-in list therefore renders a competitor number badge (.num-prefix)
// only when the server has actually assigned one (p.number, filled in from
// pools.csv after the draw); a pooled competition's competitors show NO
// number of any kind before that.
//
// Mounted for REAL (not stubbed): the render setup (vitest.setup.render.js)
// already preloads admin_helpers, viewer_utils, data and ui, which is
// everything admin_participants.jsx needs at module-eval and render time
// (window.pluralize, window.EmptyState, window.StableInput,
// window.competitionSeedingBlocker, window.checkinPid).

let AdminParticipants;

beforeAll(async () => {
  await import('../../admin_participants.jsx');
  AdminParticipants = window.AdminParticipants;
});

const noop = () => {};

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
    status: 'setup',
    format: 'mixed',
    kind: 'individual',
    poolSize: 4,
    poolWinners: 2,
    checkInEnabled: false,
    withZekkenName: false,
    numberPrefix: 'K',
    players: [
      { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice' },
      { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob' },
    ],
    ...overrides,
  };
}

async function mount(c) {
  let result;
  await act(async () => {
    result = render(
      <AdminParticipants
        c={c}
        tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={noop}
        password=""
        showToast={noop}
        onSection={noop}
        onBack={noop}
      />
    );
  });
  return result;
}

describe('AdminParticipants competitor number badge (bc-pnum operator ruling)', () => {
  it('renders no number badge at all for a roster row with no assigned number, even if a stale provisionalNumbers array is still present on c', async () => {
    // The `provisionalNumbers` field carried here is what a pre-ruling
    // server response (or a stale cached list entry) used to carry; a
    // roster row with no `number` must show NO badge regardless.
    const { container } = await mount(makeCompetition({ provisionalNumbers: ['K1', 'K2'] }));

    expect(container.querySelectorAll('.num-prefix').length).toBe(0);
  });

  it('renders the assigned number badge once the draw has set p.number', async () => {
    const { container } = await mount(makeCompetition({
      players: [
        { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice', number: 'K1' },
        { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob', number: 'K2' },
      ],
    }));

    const badges = container.querySelectorAll('.num-prefix');
    expect(badges.length).toBe(2);
    expect(Array.from(badges).map((el) => el.textContent)).toEqual(['K1', 'K2']);
  });
});
