// Pins result_slot.jsx's two literal tokens against their Go counterparts,
// via the shared golden fixture internal/domain/testdata/ippon_marks.json —
// see that file's `_comment` for why the fixture is shared and why it pins
// values, not source text. Go half: TestIpponMarks_GoldenFixture in
// internal/domain/ippon_test.go.
//
// A PR review finding (bc-dmsr review round) claimed these were spelled two
// different ways in this file (a "•" literal in one place, "•" in
// another) and might therefore be two different Unicode codepoints - which
// would be a real bug (a predicate silently missing the placeholder another
// function writes). A codepoint dump (python3, reading the file as utf-8 and
// printing ord() for every non-ascii character) showed both were already
// U+2022 BULLET; the finding's premise was wrong, but the file has since been
// consolidated onto two named constants (IPPON_PLACEHOLDER, HANTEI_MARK) so
// there is only ONE literal of each to go stale.
//
// A second review finding noted that hardcoding both values here only pinned
// JS-side drift: no Go test read these constants, so the realistic drift (one
// language changed together with its own tests in the same PR) passed both
// suites. Reading the shared fixture, as internal/export/testdata/encho_labels.json
// already does for enchoLabel, closes that gap: editing either the fixture or
// one language's constant now fails the OTHER language's suite too.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { IPPON_PLACEHOLDER, HANTEI_MARK } from '../result_slot.jsx';

const fixture = JSON.parse(
  readFileSync(
    resolve(__dirname, '..', '..', '..', 'internal', 'domain', 'testdata', 'ippon_marks.json'),
    'utf8'
  )
);

describe('result_slot.jsx constants match internal/domain/ippon.go', () => {
  // Load-bearing: a degraded/empty fixture must fail loudly rather than let
  // the assertions below pass vacuously against blank strings.
  it('the shared golden fixture is present and non-empty', () => {
    expect(fixture.ipponPlaceholder, 'ippon_marks.json parsed with an empty ipponPlaceholder').toBeTruthy();
    expect(fixture.hanteiMark, 'ippon_marks.json parsed with an empty hanteiMark').toBeTruthy();
  });

  it('IPPON_PLACEHOLDER is exactly U+2022 BULLET, matching domain.IpponPlaceholder', () => {
    expect(IPPON_PLACEHOLDER).toBe(fixture.ipponPlaceholder);
    expect(IPPON_PLACEHOLDER.codePointAt(0)).toBe(0x2022);
    expect(IPPON_PLACEHOLDER.length).toBe(1);
  });

  it('HANTEI_MARK matches domain.HanteiMark', () => {
    expect(HANTEI_MARK).toBe(fixture.hanteiMark);
  });
});
