// Pure-logic tests for resolveDeepLink (the ?player= / ?name= deep-link
// resolution helper used by ViewerHome's useEffect). Covers the outcomes:
// no-op, UUID hit, and name-substring hit. Adding to the watchlist is
// non-destructive (dedup-add), so there is no overwrite-confirmation concept
// (mp-xhaa removed the old single-follow `pending` path).
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
      expect(resolveDeepLink('', ROSTER)).toBeNull();
    });

    it('returns null when neither ?player= nor ?name= is present', () => {
      expect(resolveDeepLink('?foo=bar', ROSTER)).toBeNull();
    });

    it('returns null when ?player= and ?name= are both blank strings', () => {
      expect(resolveDeepLink('?player=&name=', ROSTER)).toBeNull();
    });

    it('returns null when the UUID does not match any roster entry', () => {
      expect(resolveDeepLink('?player=uuid-nobody', ROSTER)).toBeNull();
    });

    it('returns null when the name query matches nothing in the roster', () => {
      expect(resolveDeepLink('?name=zzz', ROSTER)).toBeNull();
    });
  });

  describe('UUID match', () => {
    it('returns { player } when the UUID matches a roster entry', () => {
      const result = resolveDeepLink('?player=uuid-alice', ROSTER);
      expect(result).not.toBeNull();
      expect(result.player).toEqual({ id: 'uuid-alice', name: 'Alice Doe' });
    });
  });

  describe('name match', () => {
    it('falls back to substring name match when the UUID is not in the roster', () => {
      const result = resolveDeepLink('?player=alice', ROSTER);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-alice');
    });

    it('prefers ?name= over ?player= for name lookup', () => {
      const result = resolveDeepLink('?player=uuid-nobody&name=bob', ROSTER);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-bob');
    });

    it('name lookup is case-insensitive', () => {
      const result = resolveDeepLink('?name=CAROL', ROSTER);
      expect(result).not.toBeNull();
      expect(result.player.id).toBe('uuid-carol');
    });
  });
});
