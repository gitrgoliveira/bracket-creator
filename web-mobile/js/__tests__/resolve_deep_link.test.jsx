// resolveDeepLink no longer reads ?playerNumber=. A printed tag's QR used to
// be `?playerNumber=<number>` (mp-yin4); it is now the one-entry watchlist
// permalink `?w=<number>` (bc-wlpl, helper.playerTagURL), read by the ?w=
// effect with its retry while a roster loads. The operator ruled on
// 2026-09-23 that tags printed before that need not keep working. This file
// pins the removal, because a deleted arm otherwise leaves no failing test.
// The ?player= / ?name= arms are covered in viewer_deeplink.test.jsx.
import { describe, it, expect } from 'vitest';
import { resolveDeepLink } from '../viewer.jsx';

const roster = [
  { id: 'uuid-001', name: 'Alice Tanaka', number: 'K1' },
  { id: 'uuid-002', name: 'Bob Yamada', number: 'K2' },
];

describe('resolveDeepLink', () => {
  it('does not read ?playerNumber= (tags print ?w= now)', () => {
    expect(resolveDeepLink('?playerNumber=K1', roster)).toBeNull();
  });

  it('a stray ?playerNumber= does not outrank ?name=', () => {
    const result = resolveDeepLink('?playerNumber=K2&name=Alice+Tanaka', roster);
    expect(result.player.id).toBe('uuid-001');
  });

  it('returns null when all params are missing', () => {
    expect(resolveDeepLink('', roster)).toBeNull();
    expect(resolveDeepLink('?foo=bar', roster)).toBeNull();
  });
});
