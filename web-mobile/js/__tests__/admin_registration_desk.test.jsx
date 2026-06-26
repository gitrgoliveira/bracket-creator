import { describe, it, expect } from 'vitest';
import {
  rdNorm, rdPersonKey, rdPid, rdTokenScore, rdQueryScore, rdPlayerTag,
  rdBuildPeopleIndex, rdHaystack, rdPresence, rdZekken,
} from '../admin_registration_desk.jsx';

// Pure helpers behind the Registration desk (mp-25bk). These carry the
// cross-competition identity, fuzzy search, player-tag resolution, and
// presence logic — pinning them behaviorally so a refactor can't silently
// regress the desk's arrival loop.

describe('rdNorm', () => {
  it('lowercases, trims, and collapses whitespace', () => {
    expect(rdNorm('  Akira   Tanaka ')).toBe('akira tanaka');
  });
  it('strips combining diacritics (fallback path)', () => {
    // window.normalizeParticipantName is absent under vitest, so rdNorm uses
    // its NFD-strip fallback. "Aïko" → "aiko".
    expect(rdNorm('Aïko')).toBe('aiko');
  });
  it('is null/undefined safe', () => {
    expect(rdNorm(undefined)).toBe('');
    expect(rdNorm(null)).toBe('');
  });
});

describe('rdPersonKey', () => {
  it('keys a person by normalized name + dojo (cross-competition identity)', () => {
    expect(rdPersonKey({ name: 'Akira Tanaka', dojo: 'Gyokusen' }))
      .toBe(rdPersonKey({ name: 'akira  tanaka', dojo: ' GYOKUSEN ' }));
  });
  it('distinguishes same name at different dojos', () => {
    expect(rdPersonKey({ name: 'John Smith', dojo: 'Wakaba' }))
      .not.toBe(rdPersonKey({ name: 'John Smith', dojo: 'Tora' }));
  });
});

describe('rdPid', () => {
  it('prefers the UUID, falls back to the name', () => {
    expect(rdPid({ id: 'uuid-1', name: 'A' })).toBe('uuid-1');
    expect(rdPid({ name: 'A' })).toBe('A');
  });
});

describe('rdTokenScore (fuzzy)', () => {
  it('scores a contiguous substring by position (prefix best)', () => {
    expect(rdTokenScore('tan', 'akira tanaka gyokusen')).toBeGreaterThanOrEqual(0);
    expect(rdTokenScore('aki', 'akira tanaka')).toBe(0); // prefix
    expect(rdTokenScore('aki', 'x akira')).toBeGreaterThan(0); // later position
  });
  it('accepts a tight subsequence (typo tolerance) but ranks it below substrings', () => {
    const sub = rdTokenScore('tnk', 'tanaka');
    expect(sub).not.toBeNull();
    expect(sub).toBeGreaterThan(rdTokenScore('tan', 'tanaka'));
  });
  it('rejects a scattered subsequence with gaps larger than the needle', () => {
    // "yama" coincidentally subsequences "ryo nakamura wakaba" but is not a
    // real hit — the gap cap rejects it.
    expect(rdTokenScore('yama', 'ryo nakamura wakaba transfer')).toBeNull();
  });
  it('rejects at the gap-cap boundary (gaps == needle length)', () => {
    // "yama" over "ryonakama": y@1,a@5,m@8,a@9 → gaps 3+2+0 = ... the boundary
    // case where total gaps equal the needle length must be rejected (>=), not
    // accepted. A spread this wide is never a real desk hit.
    expect(rdTokenScore('yama', 'ryonakama')).toBeNull();
  });
  it('returns null when a character is missing entirely', () => {
    expect(rdTokenScore('zzz', 'tanaka')).toBeNull();
  });
});

describe('rdQueryScore', () => {
  it('returns 0 for an empty query (everything matches)', () => {
    expect(rdQueryScore('', 'anything')).toBe(0);
  });
  it('requires every token to match (AND), order-independent', () => {
    const hay = 'akira tanaka gyokusen';
    expect(rdQueryScore('tanaka akira', hay)).not.toBeNull();
    expect(rdQueryScore('tanaka kobayashi', hay)).toBeNull();
  });
});

describe('rdPlayerTag', () => {
  it('uses the team name for team competitions', () => {
    expect(rdPlayerTag({ kind: 'team' }, { name: 'Tora A', number: 'T1' }))
      .toEqual({ kind: 'team', value: 'Tora A' });
  });
  it('uses the assigned number for individuals', () => {
    expect(rdPlayerTag({ kind: 'individual' }, { name: 'Akira', number: 'M1' }))
      .toEqual({ kind: 'number', value: 'M1' });
  });
  it('reports pending when an individual has no number yet (pre-draw)', () => {
    expect(rdPlayerTag({ kind: 'individual' }, { name: 'Akira', number: '' }))
      .toEqual({ kind: 'pending', value: null });
  });
});

describe('rdBuildPeopleIndex', () => {
  const comps = [
    { id: 'm', kind: 'individual', players: [{ name: 'Akira Tanaka', dojo: 'Gyokusen', checkedIn: true }, { name: 'Kenji Sato', dojo: 'Mumeishi' }] },
    { id: 'v', kind: 'individual', players: [{ name: 'Akira Tanaka', dojo: 'Gyokusen', checkedIn: false }] },
  ];
  it('dedups a person across competitions and collects their entries', () => {
    const idx = rdBuildPeopleIndex(comps);
    expect(idx.size).toBe(2); // Akira (×2 comps) + Kenji
    const akira = idx.get(rdPersonKey({ name: 'Akira Tanaka', dojo: 'Gyokusen' }));
    expect(akira.entries.map((e) => e.comp.id)).toEqual(['m', 'v']);
  });
  it('is empty-safe', () => {
    expect(rdBuildPeopleIndex(undefined).size).toBe(0);
  });
});

describe('rdHaystack', () => {
  it('includes name, dojo, zekken, number, and team name', () => {
    const hay = rdHaystack('Akira Tanaka', 'Gyokusen', [
      { comp: { kind: 'individual', withZekkenName: true }, player: { displayName: 'TANAKA', number: 'M1' } },
    ]);
    expect(hay).toContain('akira tanaka');
    expect(hay).toContain('gyokusen');
    expect(hay).toContain('tanaka');
    expect(hay).toContain('m1');
  });
  it('includes the team name for team entries', () => {
    const hay = rdHaystack('Tora A', 'Tora Dojo', [
      { comp: { kind: 'team' }, player: { name: 'Tora A' } },
    ]);
    expect(hay).toContain('tora a');
  });
});

describe('rdPresence', () => {
  const e = (checkedIn) => ({ player: { checkedIn } });
  it('reports none / partial / all', () => {
    expect(rdPresence([e(false), e(false)])).toBe('none');
    expect(rdPresence([e(true), e(false)])).toBe('partial');
    expect(rdPresence([e(true), e(true)])).toBe('all');
  });
});

describe('rdZekken', () => {
  it('returns the display name only from a zekken competition', () => {
    expect(rdZekken([{ comp: { withZekkenName: true }, player: { name: 'Akira Tanaka', displayName: 'TANAKA' } }]))
      .toBe('TANAKA');
  });
  it('ignores derived display names from non-zekken competitions', () => {
    // Women's etc. carry a server-derived DisplayName ("A. SATO") that is not a
    // real zekken — it must not surface.
    expect(rdZekken([{ comp: { withZekkenName: false }, player: { name: 'Aiko Sato', displayName: 'A. SATO' } }]))
      .toBe('');
  });
  it('ignores a display name that merely echoes the name', () => {
    expect(rdZekken([{ comp: { withZekkenName: true }, player: { name: 'Akira', displayName: 'Akira' } }]))
      .toBe('');
  });
  it('picks the zekken entry when a person spans zekken and non-zekken competitions', () => {
    expect(rdZekken([
      { comp: { withZekkenName: false }, player: { name: 'Akira Tanaka', displayName: 'A. TANAKA' } },
      { comp: { withZekkenName: true }, player: { name: 'Akira Tanaka', displayName: 'TANAKA' } },
    ])).toBe('TANAKA');
  });
});
