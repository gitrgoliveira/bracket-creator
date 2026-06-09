// Pure-logic tests for resolveDeepLink (the ?player= / ?name= deep-link
// resolution helper extracted from ViewerHome's useEffect). Covers the
// four outcomes: no-op, UUID hit + auto-apply, name hit + auto-apply,
// and conflict-detection (pending=true) when a different player is already
// being followed.
import { describe, it, expect } from 'vitest';
import { resolveDeepLink } from '../viewer.jsx';

const ROSTER = [
  { id: 'uuid-alice', name: 'Alice Doe', dojo: 'Dojo A' },
  { id: 'uuid-bob',   name: 'Bob Smith',  dojo: 'Dojo B' },
  { id: 'uuid-carol', name: 'Carol King', dojo: 'Dojo C' },
];

describe('resolveDeepLink', () => {
  describe('no-op cases', () => {
    it('returns null when searchString is empty', () => {
      expect(resolveDeepLink('', ROSTER, null)).toBeNull();
    });

    it('returns null when neither ?player= nor ?name= is present', () => {
      expect(resolveDeepLink('?foo=bar', ROSTER, null)).toBeNull();
    });

    it('returns null when ?player= and ?name= are both blank strings', () => {
      expect(resolveDeepLink('?player=&name=', ROSTER, null)).toBeNull();
    });

    it('returns null when the UUID does not match any roster entry', () => {
      expect(resolveDeepLink('?player=uuid-nobody', ROSTER, null)).toBeNull();
    });

    it('returns null when the name query matches nothing in the roster', () => {
      expect(resolveDeepLink('?name=zzz', ROSTER, null)).toBeNull();
    });
  });

  describe('UUID match — auto-apply (no conflict)', () => {
    it('returns { player, pending:false } when UUID matches and no one is followed', () => {
      const result = resolveDeepLink('?player=uuid-alice', ROSTER, null);
      expect(result).not.toBeNull();
      expect(result.player).toEqual({ id: 'uuid-alice', name: 'Alice Doe' });
      expect(result.pending).toBe(false);
    });

    it('returns pending:false when UUID matches the already-followed player', () => {
      const followed = { id: 'uuid-alice', name: 'Alice Doe' };
      const result = resolveDeepLink('?player=uuid-alice', ROSTER, followed);
      expect(result.pending).toBe(false);
    });
  });

  describe('name match — auto-apply (no conflict)', () => {
    it('falls back to substring name match when UUID is not in roster', () => {
      const result = resolveDeepLink('?player=alice', ROSTER, null);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-alice');
      expect(result.pending).toBe(false);
    });

    it('prefers ?name= over ?player= for name lookup', () => {
      const result = resolveDeepLink('?player=uuid-nobody&name=bob', ROSTER, null);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-bob');
    });

    it('name lookup is case-insensitive', () => {
      const result = resolveDeepLink('?name=CAROL', ROSTER, null);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-carol');
    });
  });

  describe('conflict detection — pending banner', () => {
    it('returns pending:true when UUID matches a DIFFERENT followed player', () => {
      const followed = { id: 'uuid-alice', name: 'Alice Doe' };
      const result = resolveDeepLink('?player=uuid-bob', ROSTER, followed);
      expect(result).not.toBeNull();
      expect(result.player).toEqual({ id: 'uuid-bob', name: 'Bob Smith' });
      expect(result.pending).toBe(true);
    });

    it('returns pending:true when name matches a DIFFERENT followed player', () => {
      const followed = { id: 'uuid-alice', name: 'Alice Doe' };
      const result = resolveDeepLink('?name=carol', ROSTER, followed);
      expect(result.pending).toBe(true);
      expect(result.player.id).toBe('uuid-carol');
    });

    it('returns pending:false when followed player has no id (anonymous follow)', () => {
      // followedPlayer without an id cannot conflict — treat as no selection.
      const followed = { id: '', name: 'Someone' };
      const result = resolveDeepLink('?player=uuid-bob', ROSTER, followed);
      expect(result.pending).toBe(false);
    });
  });
});
