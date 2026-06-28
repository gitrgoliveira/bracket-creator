import { describe, it, expect } from 'vitest';
import { findUpcomingOnCourt, findActiveCourts, countCourtMatches } from '../display.jsx';

// Regression for mp-turx: a mixed comp's bracket.preview strip lifts the moment
// ANY single pool resolves, so the aggregate /api/viewer payload then exposes a
// partially-resolved bracket whose un-finished pools are still "Pool X-Nth"
// placeholders. The TV/lobby display helpers must reject those placeholder bouts
// (like they already reject "Winner of rX-mY") so idle courts aren't shown active
// and phantom "Pool C-1st vs Pool D-1st" bouts aren't rendered.

function mixedCompPartiallyResolved() {
  return {
    id: 'mixed-1',
    name: 'Mixed',
    format: 'mixed',
    poolMatches: [],
    bracket: {
      preview: false, // one pool resolved → strip lifted
      rounds: [
        [
          // Resolved feeder (Pool A & B done) — a real, playable bout on court A.
          { id: 'm-r1-0', court: 'A', status: 'scheduled', sideA: 'Alice', sideB: 'Bob' },
          // Unresolved feeders (Pools C & D still running) — placeholders on court A.
          { id: 'm-r1-1', court: 'A', status: 'scheduled', sideA: 'Pool C-1st', sideB: 'Pool D-1st' },
        ],
        [
          { id: 'm-r2-0', court: 'A', status: 'scheduled', sideA: 'Winner of r1-m0', sideB: 'Winner of r1-m1' },
        ],
      ],
    },
  };
}

describe('display helpers reject pool-origin placeholders (mp-turx)', () => {
  it('findUpcomingOnCourt skips "Pool X-Nth" and "Winner of …" bracket matches', () => {
    const upcoming = findUpcomingOnCourt([mixedCompPartiallyResolved()], 'A', 10);
    const ids = upcoming.map((m) => m.id);
    expect(ids).toContain('m-r1-0'); // the resolved bout shows
    expect(ids).not.toContain('m-r1-1'); // pool placeholder hidden
    expect(ids).not.toContain('m-r2-0'); // "Winner of" placeholder hidden
  });

  it('countCourtMatches counts only resolved bouts (placeholders excluded)', () => {
    const { scheduled } = countCourtMatches([mixedCompPartiallyResolved()], 'A');
    expect(scheduled).toBe(1); // only the Alice vs Bob bout
  });

  it('findActiveCourts does not mark a court active for placeholder-only bouts', () => {
    // A comp whose ONLY court-B bouts are placeholders must not make B "active".
    const comp = {
      id: 'mixed-2',
      format: 'mixed',
      poolMatches: [],
      bracket: {
        preview: false,
        rounds: [[
          { id: 'b0', court: 'B', status: 'scheduled', sideA: 'Pool A-1st', sideB: 'Pool B-1st' },
          { id: 'b1', court: 'B', status: 'scheduled', sideA: 'Winner of r1-m0', sideB: 'Winner of r1-m1' },
        ]],
      },
    };
    const tournament = { courts: ['A', 'B'] };
    expect(findActiveCourts(tournament, [comp])).not.toContain('B');

    // But a resolved bout on B DOES make it active.
    comp.bracket.rounds[0][0] = { id: 'b0', court: 'B', status: 'scheduled', sideA: 'Alice', sideB: 'Bob' };
    expect(findActiveCourts(tournament, [comp])).toContain('B');
  });
});
