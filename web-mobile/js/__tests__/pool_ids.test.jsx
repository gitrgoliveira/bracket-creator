import { describe, it, expect } from 'vitest';
import { poolNameOf, isSupplementaryBout, isPoolDaihyosenBout } from '../pool_ids.jsx';

describe('poolNameOf', () => {
  it('parses regular / DH / TB ids to the pool name, hyphens preserved', () => {
    expect(poolNameOf('Pool A-0')).toBe('Pool A');
    expect(poolNameOf('Pool A-DH-1')).toBe('Pool A');
    expect(poolNameOf('Pool A-TB-2')).toBe('Pool A');
    expect(poolNameOf('Pool A-East-DH-0')).toBe('Pool A-East');
  });
  it('returns "" for non-pool-shaped ids and non-strings', () => {
    expect(poolNameOf('QF-0')).toBe('QF'); // caveat: any "-N" id parses; callers gate on format
    expect(poolNameOf('nodash')).toBe('');
    expect(poolNameOf(undefined)).toBe('');
    expect(poolNameOf(null)).toBe('');
  });
});

describe('isSupplementaryBout (routing: DH + TB are both rep bouts)', () => {
  it('true for both daihyosen and tiebreaker ids', () => {
    expect(isSupplementaryBout('Pool A-DH-0')).toBe(true);
    expect(isSupplementaryBout('Pool A-TB-0')).toBe(true);
    expect(isSupplementaryBout('Pool A-East-TB-3')).toBe(true);
  });
  it('false for regular pool matches and non-strings', () => {
    expect(isSupplementaryBout('Pool A-0')).toBe(false);
    expect(isSupplementaryBout('QF-0')).toBe(false);
    expect(isSupplementaryBout(undefined)).toBe(false);
  });
});

describe('isPoolDaihyosenBout (label: daihyosen ONLY, not tiebreaker)', () => {
  it('true only for daihyosen ("-DH-") ids', () => {
    expect(isPoolDaihyosenBout('Pool A-DH-0')).toBe(true);
    expect(isPoolDaihyosenBout('Pool A-East-DH-2')).toBe(true);
  });
  it('FALSE for tiebreaker ("-TB-") ids — a tiebreaker is not a daihyosen', () => {
    expect(isPoolDaihyosenBout('Pool A-TB-0')).toBe(false);
    expect(isPoolDaihyosenBout('Pool A-East-TB-1')).toBe(false);
  });
  it('false for regular pool matches and non-strings', () => {
    expect(isPoolDaihyosenBout('Pool A-0')).toBe(false);
    expect(isPoolDaihyosenBout('QF-0')).toBe(false);
    expect(isPoolDaihyosenBout(null)).toBe(false);
  });
  it('suffix match: a pool NAME containing "-DH-" does not false-positive a regular match', () => {
    // Pool literally named "Pool A-DH-East": its regular match is
    // "Pool A-DH-East-0". A naive includes("-DH-") would flag it as a DH win.
    expect(isPoolDaihyosenBout('Pool A-DH-East-0')).toBe(false);
    // The pool's real daihyosen bout still matches (ends in -DH-N).
    expect(isPoolDaihyosenBout('Pool A-DH-East-DH-0')).toBe(true);
  });
});
