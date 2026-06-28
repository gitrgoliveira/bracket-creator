import { describe, it, expect } from 'vitest';
import { mintParticipantIds, findSeedMatchIndex, participantSearchTarget, generateRosterText, validateRosterRows } from '../admin_participants.jsx';

// Helper: parsed rows arrive from parseParticipantLines as
// { name, displayName, dojo, danGrade, tag } objects. Test rows omit the
// optional fields for brevity : mintParticipantIds doesn't read them
// other than passing them through.
const parsed = (...names) => names.map(n => ({ name: n, dojo: 'Dojo', danGrade: null, source: null }));

const existing = (id, name, seed = null) => ({ id, name, displayName: null, dojo: 'Dojo', danGrade: null, source: null, seed });

describe('mintParticipantIds', () => {
  describe('Copilot finding: order-dependent ID collision', () => {
    it('does not duplicate ids when a new participant appears before an existing one', () => {
      // Original bug: pasting "Zelda\nAlice\nBob" when Alice (c1-p1) and
      // Bob (c1-p2) already exist would mint Zelda as c1-p1 (because
      // usedIds was still empty), then Alice would later return its own
      // c1-p1 from existingMap : two rows with the same id.
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice'), existing('c1-p2', 'Bob')],
        parsed('Zelda', 'Alice', 'Bob'),
      );
      const ids = np.map(p => p.id);
      expect(new Set(ids).size).toBe(ids.length); // no duplicates
      // Alice and Bob keep their stable ids; Zelda gets the next free slot.
      expect(np[0]).toMatchObject({ id: 'c1-p3', name: 'Zelda' });
      expect(np[1]).toMatchObject({ id: 'c1-p1', name: 'Alice' });
      expect(np[2]).toMatchObject({ id: 'c1-p2', name: 'Bob' });
    });

    it('does not duplicate ids when multiple new participants interleave with existing ones', () => {
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice'), existing('c1-p2', 'Bob'), existing('c1-p3', 'Carol')],
        parsed('Yara', 'Alice', 'Xavier', 'Bob', 'Zelda', 'Carol'),
      );
      const ids = np.map(p => p.id);
      expect(new Set(ids).size).toBe(ids.length);
      // Existing keep their ids; new participants fill p4 onward.
      expect(np.find(p => p.name === 'Alice').id).toBe('c1-p1');
      expect(np.find(p => p.name === 'Bob').id).toBe('c1-p2');
      expect(np.find(p => p.name === 'Carol').id).toBe('c1-p3');
      const newIds = np.filter(p => ['Yara', 'Xavier', 'Zelda'].includes(p.name)).map(p => p.id).sort();
      expect(newIds).toEqual(['c1-p4', 'c1-p5', 'c1-p6']);
    });
  });

  describe('existing-player preservation', () => {
    it('preserves a player\'s stable id when only the casing changes', () => {
      // Case-insensitive name match : "Alice" in existing matches "alice"
      // in parsed. The stable id (and seed) are preserved.
      const { np, updatedCount } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice', 3)],
        parsed('alice'),
      );
      expect(np[0]).toMatchObject({ id: 'c1-p1', name: 'alice', seed: 3 });
      expect(updatedCount).toBe(1);
    });

    it('counts only kept rows as "updated"', () => {
      const { added, updatedCount } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice')],
        parsed('Alice', 'Bob'),
      );
      expect(updatedCount).toBe(1);
      expect(added).toBe(1);
    });

    it('preserves existing seed when row is kept', () => {
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice', 5)],
        parsed('Alice'),
      );
      expect(np[0].seed).toBe(5);
    });

    it('new players get seed: null', () => {
      const { np } = mintParticipantIds('c1', [], parsed('Alice'));
      expect(np[0].seed).toBeNull();
    });
  });

  describe('removed-player slot reuse', () => {
    it('reuses the id of a removed player for a new participant', () => {
      // Alice removed, Bob and Charlie kept, Zelda added. Zelda should
      // get c1-p1 (Alice's old slot) rather than c1-p4, keeping the
      // numbering compact.
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice'), existing('c1-p2', 'Bob'), existing('c1-p3', 'Charlie')],
        parsed('Bob', 'Charlie', 'Zelda'),
      );
      expect(np.find(p => p.name === 'Zelda').id).toBe('c1-p1');
    });

    it('does not reuse a kept player\'s id even when later removed in parsed list order', () => {
      // Alice kept; Bob removed; Charlie kept; Zelda added.
      // Zelda should get c1-p2 (Bob's old slot).
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice'), existing('c1-p2', 'Bob'), existing('c1-p3', 'Charlie')],
        parsed('Alice', 'Charlie', 'Zelda'),
      );
      expect(np.find(p => p.name === 'Zelda').id).toBe('c1-p2');
    });
  });

  describe('edge cases', () => {
    it('handles an empty existing roster', () => {
      const { np, added, updatedCount } = mintParticipantIds('c1', [], parsed('Alice', 'Bob'));
      expect(np.map(p => p.id)).toEqual(['c1-p1', 'c1-p2']);
      expect(added).toBe(2);
      expect(updatedCount).toBe(0);
    });

    it('handles null existing roster (defensive)', () => {
      const { np, added } = mintParticipantIds('c1', null, parsed('Alice'));
      expect(np[0].id).toBe('c1-p1');
      expect(added).toBe(1);
    });

    it('handles an empty parsed roster', () => {
      const { np, added, updatedCount } = mintParticipantIds(
        'c1',
        [existing('c1-p1', 'Alice')],
        [],
      );
      expect(np).toEqual([]);
      expect(added).toBe(0);
      expect(updatedCount).toBe(0);
    });

    it('skips over non-canonical existing ids when minting slots', () => {
      // Existing players with non-pN ids (e.g. UUIDs from a legacy
      // import) shouldn't block the next slot from being c1-p1.
      const { np } = mintParticipantIds(
        'c1',
        [existing('some-uuid-here', 'Alice')],
        parsed('Alice', 'Zelda'),
      );
      expect(np.find(p => p.name === 'Alice').id).toBe('some-uuid-here');
      expect(np.find(p => p.name === 'Zelda').id).toBe('c1-p1');
    });

    it('respects compID in the generated slot ids', () => {
      const { np } = mintParticipantIds('mens-individual', [], parsed('Alice'));
      expect(np[0].id).toBe('mens-individual-p1');
    });

    it('does not collide when an existing kept player holds a high-numbered slot', () => {
      // Alice has c1-p7. Bob is new. Bob should get c1-p1, not c1-p7+1.
      const { np } = mintParticipantIds(
        'c1',
        [existing('c1-p7', 'Alice')],
        parsed('Bob', 'Alice'),
      );
      expect(np.find(p => p.name === 'Alice').id).toBe('c1-p7');
      expect(np.find(p => p.name === 'Bob').id).toBe('c1-p1');
    });

    it('preserves passed-through fields (displayName, dojo, danGrade, source)', () => {
      const row = { name: 'Alice', displayName: 'A.', dojo: 'TestDojo', danGrade: '3 Dan', source: 'registered' };
      const { np } = mintParticipantIds('c1', [], [row]);
      expect(np[0]).toMatchObject({
        id: 'c1-p1',
        name: 'Alice',
        displayName: 'A.',
        dojo: 'TestDojo',
        danGrade: '3 Dan',
        source: 'registered',
        seed: null,
      });
    });
  });
});

describe('findSeedMatchIndex', () => {
  // Deep-review round 13 finding: the old substring-fallback
  //   (p.name.includes(name) || name.includes(p.name))
  // silently mismatched "Bob" → "Bob Smith" (or vice versa) when both
  // existed in the roster, depending on array order. The fix is to
  // require an exact (case-insensitive) match against name or
  // displayName; non-matches fall through to the Levenshtein
  // suggestion UI for explicit admin confirmation.

  const players = [
    { name: 'Bob Smith', displayName: null },
    { name: 'Bob', displayName: null },
    { name: 'Alice', displayName: 'Aliciñha' },
  ];

  it('exact-name match (case-insensitive) returns the right index', () => {
    expect(findSeedMatchIndex(players, 'Bob')).toBe(1);
    expect(findSeedMatchIndex(players, 'bob')).toBe(1);
    expect(findSeedMatchIndex(players, 'BOB')).toBe(1);
  });

  it('exact-displayName match returns the right index', () => {
    expect(findSeedMatchIndex(players, 'Aliciñha')).toBe(2);
    expect(findSeedMatchIndex(players, 'aliciñha')).toBe(2);
  });

  it('"Bob" does NOT silently match "Bob Smith" when both exist', () => {
    // Old behavior: would land on "Bob Smith" by array order (first
    // entry whose name includes "bob"). Fix returns the canonical
    // "Bob" index.
    expect(findSeedMatchIndex(players, 'Bob')).toBe(1);
  });

  it('"Bob Smith" does NOT match "Bob" via reverse substring', () => {
    expect(findSeedMatchIndex(players, 'Bob Smith')).toBe(0);
  });

  it('returns -1 for partial / fuzzy matches (handed to Levenshtein UI)', () => {
    expect(findSeedMatchIndex(players, 'Bo')).toBe(-1);
    expect(findSeedMatchIndex(players, 'Bobby')).toBe(-1);
    expect(findSeedMatchIndex(players, 'Smith')).toBe(-1);
  });

  it('returns -1 for empty / whitespace candidates without matching anything', () => {
    expect(findSeedMatchIndex(players, '')).toBe(-1);
    // A whitespace-only candidate stays unmatched because no participant
    // has a whitespace name in the canonical roster.
    expect(findSeedMatchIndex(players, '   ')).toBe(-1);
  });

  it('handles empty roster without throwing', () => {
    expect(findSeedMatchIndex([], 'Alice')).toBe(-1);
  });
});

describe('participantSearchTarget', () => {
  const p = (name, displayName, dojo, danGrade) => ({ name, displayName, dojo, danGrade });

  it('returns a lowercase string combining name, displayName, dojo, and danGrade', () => {
    const target = participantSearchTarget(p('Alice', 'ALI', 'Tokyo Dojo'));
    expect(target).toBe('alice ali tokyo dojo');
  });

  it('includes danGrade (dojo club for withZekken competitions) in search target', () => {
    const target = participantSearchTarget(p('men-up-to-2d-p1', 'Alice Smith', 'SMITH', 'Team Alpha'));
    expect(target.includes('team alpha')).toBe(true);
    expect(target.includes('alpha')).toBe(true);
  });

  it('matches by name substring (case-insensitive)', () => {
    const target = participantSearchTarget(p('Alice Yamamoto', null, 'Shinjuku'));
    expect(target.includes('alice')).toBe(true);
    expect(target.includes('yamamoto')).toBe(true);
  });

  it('matches by dojo substring', () => {
    const target = participantSearchTarget(p('Bob', null, 'Osaka Kendo Club'));
    expect(target.includes('osaka')).toBe(true);
  });

  it('matches by displayName/zekken substring', () => {
    const target = participantSearchTarget(p('Bob Tanaka', 'TANAKA', 'Shinjuku'));
    expect(target.includes('tanaka')).toBe(true);
  });

  it('handles null/undefined fields without throwing', () => {
    expect(() => participantSearchTarget(p(null, null, null))).not.toThrow();
    expect(participantSearchTarget(p(null, null, null))).toBe('');
  });

  it('omits empty displayName : no double-space in search target', () => {
    const target = participantSearchTarget(p('Alice', null, 'Tokyo'));
    expect(target).toBe('alice tokyo');
    expect(target.includes('  ')).toBe(false);
  });

  it('does not match a query that is not a substring of any field', () => {
    const target = participantSearchTarget(p('Alice', null, 'Tokyo'));
    expect(target.includes('bob')).toBe(false);
  });
});

describe('generateRosterText', () => {
  describe('withZekkenName=false', () => {
    it('formats as Name, Dojo', () => {
      const players = [{ name: 'Alice Smith', dojo: 'Gyokusen', danGrade: null, displayName: null }];
      expect(generateRosterText(players, false)).toBe('Alice Smith, Gyokusen');
    });

    it('appends dan grade when present', () => {
      const players = [{ name: 'Alice Smith', dojo: 'Gyokusen', danGrade: '3', displayName: null }];
      expect(generateRosterText(players, false)).toBe('Alice Smith, Gyokusen, 3');
    });
  });

  describe('withZekkenName=true : existing players with displayName', () => {
    it('formats as Name, Zekken, Dojo when displayName is set', () => {
      const players = [{ name: 'Alice Smith', displayName: 'SMITH', dojo: 'Gyokusen', danGrade: null }];
      expect(generateRosterText(players, true)).toBe('Alice Smith, SMITH, Gyokusen');
    });

    it('appends dan grade when present', () => {
      const players = [{ name: 'Alice Smith', displayName: 'SMITH', dojo: 'Gyokusen', danGrade: '3' }];
      expect(generateRosterText(players, true)).toBe('Alice Smith, SMITH, Gyokusen, 3');
    });
  });

  describe('withZekkenName=true : sample players without displayName', () => {
    it('falls back to derived zekken so line has three columns (not two)', () => {
      // makePlayer does not set displayName; without a fallback the line is
      // "Name, Dojo" which parseParticipantLines(withZekken=true) misreads as
      // zekken=Dojo, dojo="" : silently corrupting the saved roster.
      const players = [{ name: 'Alice Smith', displayName: undefined, dojo: 'Gyokusen', danGrade: null }];
      const line = generateRosterText(players, true);
      const parts = line.split(',').map(s => s.trim());
      expect(parts).toHaveLength(3);
      expect(parts[0]).toBe('Alice Smith');
      expect(parts[2]).toBe('Gyokusen');
    });

    it('falls back when displayName is null (legacy roster shape)', () => {
      const players = [{ name: 'Alice Smith', displayName: null, dojo: 'Gyokusen', danGrade: null }];
      const line = generateRosterText(players, true);
      const parts = line.split(',').map(s => s.trim());
      expect(parts[1]).toBe('SMITH');
    });

    it('preserves an intentional empty zekken (does NOT synthesize one)', () => {
      // Copilot finding (PR #320 round 3): the prior `p.displayName || …`
      // swapped any falsy displayName for a derived value, including the
      // operator's intentional empty zekken. The new `??` form only falls
      // back for null/undefined, so `""` round-trips verbatim and the
      // rosterDirty diff stays stable across saves.
      const players = [{ name: 'Alice Smith', displayName: '', dojo: 'Gyokusen', danGrade: null }];
      const line = generateRosterText(players, true);
      expect(line).toBe('Alice Smith, , Gyokusen');
    });

    it('trailing-whitespace name derives a non-empty zekken', () => {
      // Copilot finding (PR #320 round 3): `name.split(' ').pop()` on
      // "Alice " returns "" because the last token is empty. Tokenising on
      // /\s+/ after trim filters that out and the fallback stays useful.
      const players = [{ name: 'Alice Smith ', displayName: undefined, dojo: 'Gyokusen', danGrade: null }];
      const line = generateRosterText(players, true);
      const parts = line.split(',').map(s => s.trim());
      expect(parts[1]).toBe('SMITH');
    });
  });
});

describe('validateRosterRows', () => {
  const row = (name, dojo, displayName = '') => ({ name, dojo, displayName });

  it('returns no problems for a valid non-zekken roster', () => {
    const parsed = [row('Alice Smith', 'Gyokusen'), row('Bob Jones', 'Tora')];
    expect(validateRosterRows(parsed, false)).toEqual([]);
  });

  it('returns no problems for a valid zekken roster (three columns)', () => {
    const parsed = [row('Alice Smith', 'Gyokusen', 'SMITH')];
    expect(validateRosterRows(parsed, true)).toEqual([]);
  });

  it('flags a zekken-comp row whose dojo is empty (the two-column misparse)', () => {
    // "Alice Smith, Gyokusen" in a zekken comp parses as
    // {name:"Alice Smith", displayName:"Gyokusen", dojo:""}.
    const parsed = [{ name: 'Alice Smith', displayName: 'Gyokusen', dojo: '' }];
    const problems = validateRosterRows(parsed, true);
    expect(problems).toHaveLength(1);
    expect(problems[0].name).toBe('Alice Smith');
    expect(problems[0].reason).toMatch(/dojo/i);
    expect(problems[0].reason).toMatch(/zekken/i);
  });

  it('flags an empty dojo in a non-zekken comp without the zekken hint', () => {
    const parsed = [row('Alice Smith', '')];
    const problems = validateRosterRows(parsed, false);
    expect(problems).toHaveLength(1);
    expect(problems[0].reason).toBe('missing dojo');
  });

  it('flags a missing name and reports its index', () => {
    const parsed = [row('Alice Smith', 'Gyokusen'), row('', 'Tora')];
    const problems = validateRosterRows(parsed, false);
    expect(problems).toHaveLength(1);
    expect(problems[0].index).toBe(1);
    expect(problems[0].reason).toBe('missing name');
  });

  it('treats whitespace-only fields as empty', () => {
    const parsed = [row('   ', 'Tora'), row('Bob', '   ')];
    const problems = validateRosterRows(parsed, false);
    expect(problems).toHaveLength(2);
    expect(problems[0].reason).toBe('missing name');
    expect(problems[1].reason).toBe('missing dojo');
  });

  it('stores the trimmed name (so a whitespace-only name renders as line N, not "   ")', () => {
    // Copilot finding (PR #320): the toast picks `"name"` when truthy and
    // `line N` when falsy. Storing the raw whitespace string showed an ugly
    // `"   " missing name` label; trimming makes the falsy branch kick in.
    const parsed = [{ name: '   ', dojo: 'Tora' }];
    const problems = validateRosterRows(parsed, false);
    expect(problems).toHaveLength(1);
    expect(problems[0].name).toBe('');
  });

  it('zekken-comp dojo-missing reason wording reads as a format hint, not a hard requirement', () => {
    // Copilot finding (PR #320): the prior "needs Name, Zekken, Dojo" wording
    // implied we enforced a non-empty zekken, but the validator only enforces
    // dojo. The reason now reads as a format hint ("use" not "need").
    const parsed = [{ name: 'Alice', displayName: 'Gyokusen', dojo: '' }];
    const problems = validateRosterRows(parsed, true);
    expect(problems[0].reason).toMatch(/missing dojo/);
    expect(problems[0].reason).toMatch(/use/i);
    expect(problems[0].reason).not.toMatch(/\bneed\b/i);
  });

  it('handles null/empty input defensively', () => {
    expect(validateRosterRows(null, true)).toEqual([]);
    expect(validateRosterRows([], false)).toEqual([]);
  });
});
