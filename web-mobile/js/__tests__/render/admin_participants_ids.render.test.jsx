import { describe, it, expect } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-pnum ruling 1e: "When the participants list is applied, then the ids
// are created and displayed." The roster PUT response re-serialises the
// saved roster (server-minted UUIDs included), so once a roster has been
// applied at least once, every row's server-assigned id shows in a compact,
// muted slot beside the dojo line, with the full id always available on
// hover via a title attribute. A row with no id (never applied, or a load
// failure -- 1b's data-issues banner names that case separately) shows
// nothing in that slot.
//
// Follow-up (seen in the browser): a short slug id such as "ids-cup-p1"
// (some rosters carry these instead of server UUIDs) truncated to 8
// characters the same way a UUID does, showing "ids-cup-" for every row
// sharing that prefix -- telling the operator nothing. The rule is now
// LENGTH-gated: an id of 12 characters or fewer shows WHOLE; anything
// longer (a UUID) still truncates to the first 8. The title always carries
// the full id either way.
//
// Mounted for REAL, same setup as
// admin_participants_number_badge.render.test.jsx: the render setup
// preloads everything admin_participants.jsx needs at module-eval time. The
// shared harness lives in admin_participants_mount_harness.jsx.

installParticipantsHarness();

// This file's own default roster: a stamped UUID row plus an id-less one,
// the pairing the first test below (unlike its two siblings) relies on
// implicitly, differing from the harness's own plain default.
const idsRoster = [
  { id: '11111111-2222-4333-8444-555555555555', name: 'Alice', dojo: 'Dojo Alice' },
  { id: '', name: 'Bob', dojo: 'Dojo Bob' },
];

describe('AdminParticipants id display (bc-pnum ruling 1e)', () => {
  it('truncates a UUID id to the first 8 characters, with the full id on hover', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({ players: idsRoster }));

    const idSpans = container.querySelectorAll('.seed-row__id');
    // Only Alice (the stamped row) gets a slot; Bob's id-less row gets none.
    expect(idSpans.length).toBe(1);
    const span = idSpans[0];
    expect(span.textContent).toContain('11111111');
    expect(span.textContent).not.toContain('555555555555', 'only the first 8 characters show inline');
    expect(span.getAttribute('title')).toBe('11111111-2222-4333-8444-555555555555');
  });

  it('shows a short slug id whole (12 characters or fewer), not truncated', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [
        { id: 'ids-cup-p1', name: 'Alice', dojo: 'Dojo Alice' },
        { id: 'ids-cup-p2', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    }));

    const idSpans = container.querySelectorAll('.seed-row__id');
    expect(idSpans.length).toBe(2);
    // Truncating both to 8 characters would show "ids-cup-" for every row;
    // shown whole, the two rows are distinguishable.
    expect(idSpans[0].textContent).toContain('ids-cup-p1');
    expect(idSpans[1].textContent).toContain('ids-cup-p2');
    expect(idSpans[0].getAttribute('title')).toBe('ids-cup-p1');
    expect(idSpans[1].getAttribute('title')).toBe('ids-cup-p2');
  });

  it('shows no id slot at all for a roster with no ids yet', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [
        { id: '', name: 'Alice', dojo: 'Dojo Alice' },
        { id: '', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    }));

    expect(container.querySelectorAll('.seed-row__id').length).toBe(0);
  });
});
