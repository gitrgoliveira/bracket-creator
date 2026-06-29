// mp-p7n: reproduce "phantom leading column appears in textarea after Apply"
//
// User-reported flow (2026-05-26):
//   - withZekkenName = false, numberPrefix empty
//   - BEFORE Apply: textarea = "Aaron Adams, Team Alpha\n..." (clean 2-col)
//   - AFTER Apply:  textarea = "Asddasd-P1, Aaron Adams, Team Alpha\n..."
//
// `Asddasd-P{N}` is sequential: matches the `${prefix}-p${i+1}` ID shape
// that `data.jsx::makePlayer` produces for sample-roster players. So the
// hypothesis is that the participant `id` (or a normalised form of it) is
// leaking into the textarea: either by the JS serialiser including p.id
// when it shouldn't, or by the round-trip through the server shifting
// columns.
//
// This test file constructs the exact starting state from the screenshot
// and walks the same path admin_participants.jsx::apply takes. If any
// step produces a 3-col line beginning with a player-id-shaped value,
// we've found the bug.

import { describe, it, expect } from 'vitest';
import { parseParticipantLines } from '../data.jsx';
import { mintParticipantIds } from '../admin_participants.jsx';

// generateText: a copy of the inline textarea serialiser in
// admin_participants.jsx:212-219 / 233-240. Pulled out verbatim so we
// can assert on its output without rendering the full component.
function generateText(players, withZekkenName) {
  return (players || []).map((p) => {
    if (withZekkenName && p.displayName) {
      const base = `${p.name ?? ''}, ${p.displayName ?? ''}, ${p.dojo ?? ''}`;
      return p.danGrade ? `${base}, ${p.danGrade}` : base;
    }
    const base = `${p.name ?? ''}, ${p.dojo ?? ''}`;
    return p.danGrade ? `${base}, ${p.danGrade}` : base;
  }).join('\n');
}

describe('mp-p7n: Apply with clean 2-col paste does not introduce a phantom leading column', () => {
  // The sample-data roster from data.jsx::makePlayer uses ids in the
  // shape `${compID}-p${i+1}`. With compID="asddasd" this matches the
  // bug-report screenshot's "Asddasd-P1..P8" pattern.
  const SAMPLE_PLAYERS = [
    { id: 'asddasd-p1', name: 'Aaron Adams',    dojo: 'Team Alpha',   displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p2', name: 'Albus Blake',    dojo: 'Team Delta',   displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p3', name: 'Arthur Dick',    dojo: 'Team Gamma',   displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p4', name: 'Benjamin Granger', dojo: 'Team Lambda', displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p5', name: 'Bilbo Herbert',  dojo: 'Team Omega',   displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p6', name: 'Bram Lannister', dojo: 'Team Pi',      displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p7', name: 'Caleb Martinez', dojo: 'Team Sigma',   displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
    { id: 'asddasd-p8', name: 'Charles Rodriguez', dojo: 'Team Upsilon', displayName: '', danGrade: '', source: null, seed: null, checkedIn: false },
  ];

  it('initial textarea (from c.players) renders 2-col lines, not 3-col with id prefix', () => {
    // First gate: does the serialiser itself leak p.id?  This file is what
    // the user sees BEFORE clicking Apply.
    const text = generateText(SAMPLE_PLAYERS, /*withZekkenName=*/false);
    const lines = text.split('\n');
    expect(lines).toHaveLength(8);
    expect(lines[0]).toBe('Aaron Adams, Team Alpha');
    expect(lines[0]).not.toMatch(/^asddasd-/i);
    // Sanity: no line starts with the id-shaped prefix.
    for (const ln of lines) {
      expect(ln).not.toMatch(/^[a-z]+-p\d+,/i);
    }
  });

  it('Apply with unchanged 2-col textarea keeps the data 2-col (no phantom column)', () => {
    // Simulate: user opened the page, didn't edit the textarea, just
    // pressed Apply. The textarea text equals the serialised c.players
    // output from the previous test.
    const text = generateText(SAMPLE_PLAYERS, false);
    const lines = text.split('\n').filter(l => l.trim());
    const parsed = parseParticipantLines(lines, /*withZekken=*/false);
    const { np } = mintParticipantIds('asddasd', SAMPLE_PLAYERS, parsed);

    // The PUT body sent to the server is `np`.  If any element of np has
    // name="<id-shaped>" or danGrade=<dojo-shaped>, the bug is in
    // parse/mint.  Each np[i] must mirror the user-intended fields.
    expect(np).toHaveLength(8);
    expect(np[0].name).toBe('Aaron Adams');
    expect(np[0].dojo).toBe('Team Alpha');
    expect(np[0].danGrade).toBeFalsy();
    // Now serialise np back through the textarea generator (what
    // useEffectA(c.players) produces after the apply response lands).
    const afterText = generateText(np, false);
    const afterLines = afterText.split('\n');
    expect(afterLines[0]).toBe('Aaron Adams, Team Alpha');
    expect(afterLines[0]).not.toMatch(/^asddasd-p\d+,/i);
  });

  it('Apply after the user EDITED the textarea down to clean 2-col input still produces 2-col', () => {
    // Variant: user deletes the (possibly already-phantom) leading column
    // and presses Apply on hand-typed clean text.
    const lines = SAMPLE_PLAYERS.map(p => `${p.name}, ${p.dojo}`);
    const parsed = parseParticipantLines(lines, false);
    const { np } = mintParticipantIds('asddasd', SAMPLE_PLAYERS, parsed);
    const afterText = generateText(np, false);
    const afterLines = afterText.split('\n');
    expect(afterLines).toEqual(lines);
  });

  it('regression: simulating a corrupted starting state where p.name = id-shaped, p.dojo = real name, p.danGrade = team', () => {
    // If the user's c.players is ALREADY corrupt (3-col shift), what
    // does generateText render? This codifies the post-bug state from
    // the screenshot so we can detect any regression that re-introduces
    // it during Apply.
    const corrupt = [
      { id: 'x', name: 'Asddasd-P1', dojo: 'Aaron Adams', danGrade: 'Team Alpha', source: null, seed: null, checkedIn: false, displayName: '' },
    ];
    const text = generateText(corrupt, false);
    // This is the BUG output. The test pins this so a fix that ALSO
    // changes the serialiser breaks this test loudly.
    expect(text).toBe('Asddasd-P1, Aaron Adams, Team Alpha');
  });

  it('end-to-end: starting from a corrupt c.players, Apply with clean text CLEANS the data', () => {
    // The "after Apply, phantom column appears" symptom means the
    // opposite must be true: starting from corrupt state, an Apply of
    // clean text should REMOVE the corruption. Pin that contract.
    const corrupt = [
      { id: 'asddasd-p1', name: 'Asddasd-P1', dojo: 'Aaron Adams', danGrade: 'Team Alpha', source: null, seed: null, checkedIn: false, displayName: '' },
      { id: 'asddasd-p2', name: 'Asddasd-P2', dojo: 'Albus Blake', danGrade: 'Team Delta', source: null, seed: null, checkedIn: false, displayName: '' },
    ];
    const userTypedLines = ['Aaron Adams, Team Alpha', 'Albus Blake, Team Delta'];
    const parsed = parseParticipantLines(userTypedLines, false);
    const { np } = mintParticipantIds('asddasd', corrupt, parsed);
    const afterText = generateText(np, false);
    expect(afterText).toBe('Aaron Adams, Team Alpha\nAlbus Blake, Team Delta');
    // Verify the new player records are clean:
    expect(np[0]).toMatchObject({ name: 'Aaron Adams', dojo: 'Team Alpha' });
    expect(np[0].danGrade).toBeFalsy();
  });
});
