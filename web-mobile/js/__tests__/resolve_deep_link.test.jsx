// mp-yin4: playerNumber deep-link resolution in the public viewer.
import { describe, it, expect } from 'vitest';
import { resolveDeepLink } from '../viewer.jsx';

const roster = [
  { id: 'uuid-001', name: 'Alice Tanaka', number: 'K1' },
  { id: 'uuid-002', name: 'Bob Yamada',   number: 'K2' },
  { id: 'uuid-003', name: 'Carol Suzuki', number: 'M1' },
  { id: 'uuid-004', name: 'Dave Ito',     number: '' },   // no prefix
];

describe('resolveDeepLink: ?playerNumber=', () => {
  it('resolves a numbered player by exact match', () => {
    const result = resolveDeepLink('?playerNumber=K1', roster);
    expect(result).not.toBeNull();
    expect(result.player.id).toBe('uuid-001');
    expect(result.player.name).toBe('Alice Tanaka');
  });

  it('resolves a different prefix', () => {
    const result = resolveDeepLink('?playerNumber=M1', roster);
    expect(result).not.toBeNull();
    expect(result.player.id).toBe('uuid-003');
  });

  it('returns null when number does not match any player', () => {
    const result = resolveDeepLink('?playerNumber=Z99', roster);
    expect(result).toBeNull();
  });

  it('does not match a player with an empty number', () => {
    const result = resolveDeepLink('?playerNumber=', roster);
    expect(result).toBeNull();
  });

  it('number match is case-sensitive (QR encodes exact value)', () => {
    const result = resolveDeepLink('?playerNumber=k1', roster);
    expect(result).toBeNull();
  });

  it('UUID ?player= takes precedence over ?playerNumber=', () => {
    const result = resolveDeepLink('?player=uuid-002&playerNumber=K1', roster);
    expect(result).not.toBeNull();
    expect(result.player.id).toBe('uuid-002'); // Bob, not Alice
  });

  it('?playerNumber= takes precedence over ?name=', () => {
    const result = resolveDeepLink('?playerNumber=K2&name=Alice+Tanaka', roster);
    expect(result).not.toBeNull();
    expect(result.player.id).toBe('uuid-002'); // Bob via number, not Alice via name
  });

  it('falls through to ?name= when ?playerNumber= has no match', () => {
    const result = resolveDeepLink('?playerNumber=Z99&name=Alice+Tanaka', roster);
    expect(result).not.toBeNull();
    expect(result.player.id).toBe('uuid-001');
  });

  it('returns null when all params are missing', () => {
    expect(resolveDeepLink('', roster)).toBeNull();
    expect(resolveDeepLink('?foo=bar', roster)).toBeNull();
  });
});
