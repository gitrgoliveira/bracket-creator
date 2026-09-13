import { describe, it, expect } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

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
// window.competitionSeedingBlocker, window.checkinPid). The shared harness
// lives in admin_participants_mount_harness.jsx.

installParticipantsHarness();

describe('AdminParticipants competitor number badge (bc-pnum operator ruling)', () => {
  it('renders no number badge at all for a roster row with no assigned number, even if a stale provisionalNumbers array is still present on c', async () => {
    // The `provisionalNumbers` field carried here is what a pre-ruling
    // server response (or a stale cached list entry) used to carry; a
    // roster row with no `number` must show NO badge regardless.
    const { container } = await mountParticipants(makeParticipantsCompetition({ provisionalNumbers: ['K1', 'K2'] }));

    expect(container.querySelectorAll('.num-prefix').length).toBe(0);
  });

  it('renders the assigned number badge once the draw has set p.number', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
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
