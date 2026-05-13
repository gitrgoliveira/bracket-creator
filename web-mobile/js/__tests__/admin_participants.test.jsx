import { describe, it, expect } from 'vitest';
import { mintParticipantIds } from '../admin_participants.jsx';

// Helper: parsed rows arrive from parseParticipantLines as
// { name, displayName, dojo, danGrade, tag } objects. Test rows omit the
// optional fields for brevity — mintParticipantIds doesn't read them
// other than passing them through.
const parsed = (...names) => names.map(n => ({ name: n, dojo: 'Dojo', danGrade: null, tag: null }));

const existing = (id, name, seed = null) => ({ id, name, displayName: null, dojo: 'Dojo', danGrade: null, tag: null, seed });

describe('mintParticipantIds', () => {
  describe('Copilot finding: order-dependent ID collision', () => {
    it('does not duplicate ids when a new participant appears before an existing one', () => {
      // Original bug: pasting "Zelda\nAlice\nBob" when Alice (c1-p1) and
      // Bob (c1-p2) already exist would mint Zelda as c1-p1 (because
      // usedIds was still empty), then Alice would later return its own
      // c1-p1 from existingMap — two rows with the same id.
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
      // Case-insensitive name match — "Alice" in existing matches "alice"
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

    it('preserves passed-through fields (displayName, dojo, danGrade, tag)', () => {
      const row = { name: 'Alice', displayName: 'A.', dojo: 'TestDojo', danGrade: '3 Dan', tag: 'registered' };
      const { np } = mintParticipantIds('c1', [], [row]);
      expect(np[0]).toMatchObject({
        id: 'c1-p1',
        name: 'Alice',
        displayName: 'A.',
        dojo: 'TestDojo',
        danGrade: '3 Dan',
        tag: 'registered',
        seed: null,
      });
    });
  });
});
