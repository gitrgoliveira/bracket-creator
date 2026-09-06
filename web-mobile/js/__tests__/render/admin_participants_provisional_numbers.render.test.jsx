import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// bc-pnum D8: AdminParticipants' pre-draw check-in list renders the server's
// provisionalNumbers (c.provisionalNumbers), index-aligned with c.players,
// styled distinctly (.num-prefix--provisional) from an ASSIGNED number
// (.num-prefix, no modifier). Mounted for REAL (not stubbed): the render
// setup (vitest.setup.render.js) already preloads admin_helpers, viewer_utils,
// data and ui, which is everything admin_participants.jsx needs at module-eval
// and render time (window.pluralize, window.EmptyState, window.StableInput,
// window.competitionSeedingBlocker, window.checkinPid, window.provisionalNumberMap).

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

describe('AdminParticipants provisional numbers (bc-pnum D8)', () => {
  it('renders the server-provided provisional numbers, aligned with the roster', async () => {
    const { container } = await mount(makeCompetition({ provisionalNumbers: ['K1', 'K2'] }));

    const provisional = container.querySelectorAll('.num-prefix--provisional');
    expect(provisional.length).toBe(2);
    expect(Array.from(provisional).map((el) => el.textContent)).toEqual(['K1', 'K2']);
  });

  // The mutation this pins: passing [] (or omitting provisionalNumbers) must
  // never invent a fallback -- provisionalNumberMap's own length-mismatch
  // guard (data.jsx) yields an empty map, so no .num-prefix--provisional
  // spans render at all.
  it('renders no provisional numbers when the server sends none', async () => {
    const { container } = await mount(makeCompetition({ provisionalNumbers: [] }));

    expect(container.querySelectorAll('.num-prefix--provisional').length).toBe(0);
  });

  // M8-JS: the Go side can drop `omitempty` on provisionalNumbers, so a PUT
  // response whose derivation is nil serialises the key as JSON `null`
  // rather than omitting it. The SPA's update merge is `{ ...c, ...updatedComp }`
  // (a shallow spread), so a `null` in updatedComp OVERWRITES the prior
  // array on `c` rather than leaving it alone. This pins that
  // provisionalNumberMap's own length-mismatch guard (data.jsx: `provisionalNumbers
  // || []` defaults null to an empty array, which then fails the length
  // check against a non-empty roster) is what clears the stale badges when
  // that merge happens, rather than a fallback silently keeping the old
  // numbers on screen.
  it('clears stale provisional numbers after a merge whose provisionalNumbers is null (M8-JS)', async () => {
    const withNumbers = makeCompetition({ provisionalNumbers: ['K1', 'K2'] });
    let container, rerender;
    await act(async () => {
      const result = render(
        <AdminParticipants
          c={withNumbers}
          tournament={{ name: 'Spring Taikai', courts: ['A'] }}
          onUpdate={noop}
          password=""
          showToast={noop}
          onSection={noop}
          onBack={noop}
        />
      );
      container = result.container;
      rerender = result.rerender;
    });
    expect(container.querySelectorAll('.num-prefix--provisional').length).toBe(2);

    // Same two players, but provisionalNumbers is explicitly null: the shape
    // `{ ...c, ...updatedComp }` produces when a nil derivation serialises
    // without omitempty.
    const afterNullMerge = { ...withNumbers, provisionalNumbers: null };
    await act(async () => {
      rerender(
        <AdminParticipants
          c={afterNullMerge}
          tournament={{ name: 'Spring Taikai', courts: ['A'] }}
          onUpdate={noop}
          password=""
          showToast={noop}
          onSection={noop}
          onBack={noop}
        />
      );
    });
    expect(container.querySelectorAll('.num-prefix--provisional').length).toBe(0);
  });
});
