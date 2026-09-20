import { describe, it, expect } from 'vitest';
import { poolNameOf, isSupplementaryBout, isPoolDaihyosenBout, poolMatchNumberOf } from '../pool_ids.jsx';

describe('poolNameOf', () => {
  it('parses regular / DH / TB ids to the pool name, hyphens preserved', () => {
    expect(poolNameOf('Pool A-0')).toBe('Pool A');
    expect(poolNameOf('Pool A-DH-1')).toBe('Pool A');
    expect(poolNameOf('Pool A-TB-2')).toBe('Pool A');
    expect(poolNameOf('Pool A-East-DH-0')).toBe('Pool A-East');
  });
  it('parses any "-<digits>" suffix (incl. non-pool ids like "QF-0"); returns "" only when there is no such suffix', () => {
    // Documented caveat: poolNameOf greedily parses any id ending in "-<digits>",
    // so a non-pool id like "QF-0" yields "QF". Callers that must distinguish a
    // real pool from another phase gate on the competition format, not on a
    // truthy poolNameOf() result.
    expect(poolNameOf('QF-0')).toBe('QF');
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

describe('poolMatchNumberOf (a pool bout is numbered inside its own pool)', () => {
  it('turns the id\'s 0-based suffix into a 1-based match number', () => {
    expect(poolMatchNumberOf('Pool A-0')).toBe(1);
    expect(poolMatchNumberOf('Pool A-5')).toBe(6);
    expect(poolMatchNumberOf('Pool A-East-2')).toBe(3);
  });
  it('restarts per pool: the same number belongs to a different bout in each pool', () => {
    // This is why a render site must name the pool alongside the number.
    expect(poolMatchNumberOf('Pool A-0')).toBe(poolMatchNumberOf('Pool B-0'));
  });
  it('is 0 for a supplementary rep bout, which is not one of the numbered bouts', () => {
    expect(poolMatchNumberOf('Pool A-DH-0')).toBe(0);
    expect(poolMatchNumberOf('Pool A-TB-2')).toBe(0);
  });
  it('is 0 when there is no ordinal, and never throws', () => {
    expect(poolMatchNumberOf('nodash')).toBe(0);
    expect(poolMatchNumberOf('')).toBe(0);
    expect(poolMatchNumberOf(undefined)).toBe(0);
    expect(poolMatchNumberOf(null)).toBe(0);
  });
});
