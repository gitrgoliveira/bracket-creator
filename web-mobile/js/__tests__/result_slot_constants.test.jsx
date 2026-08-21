// Pins result_slot.jsx's two literal tokens against their Go counterparts.
//
// Source of truth: internal/domain/ippon.go
//   const IpponPlaceholder = "•" // U+2022 BULLET
//   const HanteiMark       = "Ht"
//
// A PR review finding (bc-dmsr review round) claimed these were spelled two
// different ways in this file (a "•" literal in one place, "•" in
// another) and might therefore be two different Unicode codepoints - which
// would be a real bug (a predicate silently missing the placeholder another
// function writes). A codepoint dump (python3, reading the file as utf-8 and
// printing ord() for every non-ascii character) showed both were already
// U+2022 BULLET; the finding's premise was wrong, but the file has since been
// consolidated onto two named constants (IPPON_PLACEHOLDER, HANTEI_MARK) so
// there is only ONE literal of each to go stale. This test is the guard
// against a FUTURE divergence, in either direction: JS drifting from Go, or a
// future edit reintroducing a second inline literal that silently differs.
import { describe, it, expect } from 'vitest';
import { IPPON_PLACEHOLDER, HANTEI_MARK } from '../result_slot.jsx';

describe('result_slot.jsx constants match internal/domain/ippon.go', () => {
  it('IPPON_PLACEHOLDER is exactly U+2022 BULLET, matching domain.IpponPlaceholder', () => {
    expect(IPPON_PLACEHOLDER).toBe('•');
    expect(IPPON_PLACEHOLDER.codePointAt(0)).toBe(0x2022);
    expect(IPPON_PLACEHOLDER.length).toBe(1);
  });

  it('HANTEI_MARK is exactly "Ht", matching domain.HanteiMark', () => {
    expect(HANTEI_MARK).toBe('Ht');
  });
});
