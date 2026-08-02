// Pins the estimator's bouts-per-team-match default against the SERVER's own
// default, which is the number the real schedule is laid out with.
//
// The bug this guards: the panel used to seed 2N-1 for regular team matches.
// That is the kachinuki worst case (every bout retiring exactly one fighter),
// which a regular encounter — where each position plays its opposite number
// once — can never reach. At the FIK default of N=5 it priced 9 bouts instead
// of 5, so an organiser sizing a hall booking read an estimate ~80% too long,
// and one that disagreed with the schedule the app itself would generate.

import { describe, it, expect } from 'vitest';
import { estimatorDefaultBouts } from '../admin_schedule_page.jsx';

describe('estimatorDefaultBouts', () => {
  it('is the team size, which is what a regular encounter fights', () => {
    // Every position plays its opposite number exactly once.
    expect(estimatorDefaultBouts(5)).toBe(5);
    expect(estimatorDefaultBouts(3)).toBe(3);
    expect(estimatorDefaultBouts(7)).toBe(7);
  });

  it('is NOT the old 2N-1 worst case', () => {
    // Stated as its own case so a regression cannot pass by coincidence:
    // 2*5-1 = 9 and 2*3-1 = 5, the second of which collides with a valid
    // team size and would slip past a single-value check.
    expect(estimatorDefaultBouts(5)).not.toBe(9);
    expect(estimatorDefaultBouts(3)).not.toBe(5);
  });

  it('matches the server default the real schedule is built from', () => {
    // perMatchElapsedMinutes (internal/engine/scheduler_slots.go) uses
    // `bouts = comp.TeamSize` for a team competition, and EstimateForCounts
    // prices the same competition through it. The panel must not tell the
    // operator a different story from the scheduler.
    const serverDefaultBouts = (teamSize) => teamSize; // mirror of the Go rule
    for (const n of [2, 3, 4, 5, 6, 7]) {
      expect(estimatorDefaultBouts(n)).toBe(serverDefaultBouts(n));
    }
  });

  it('reports 0 bouts for an individual competition', () => {
    // TeamSize 0 selects the individual-match branch server-side; a bouts
    // value of 0 is what keeps the request on that branch.
    expect(estimatorDefaultBouts(0)).toBe(0);
  });

  it('never returns a negative count for a nonsensical team size', () => {
    // The field is freeform, so a stray "-1" must not reach the API as a
    // negative bout count (the old 2N-1 form returned -3 for it).
    expect(estimatorDefaultBouts(-1)).toBe(0);
  });
});
