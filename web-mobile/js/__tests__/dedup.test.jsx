// Tests for the shared participant-name normalization (Tier-1 dedup key and
// the JS↔Go parity fixture). The Tier-2 near-duplicate *detection* is computed
// authoritatively in Go (internal/helper/dedup_test.go) and surfaced via the
// roster-PUT response, so there is no JS fuzzy implementation to test here.

import { describe, it, expect } from 'vitest';
import { normalizeParticipantName } from '../data.jsx';

// ---------------------------------------------------------------------------
// normalizeParticipantName
// ---------------------------------------------------------------------------

describe('normalizeParticipantName', () => {
  const cases = [
    ['lowercase', 'Alice Smith', 'alice smith'],
    ['trim spaces', '  Bob  ', 'bob'],
    ['collapse internal spaces', 'Chau  Earn  Tan', 'chau earn tan'],
    ['Latin diacritic fold — Müller', 'Müller', 'muller'],
    ['Latin diacritic fold — Ï', 'Ï', 'i'],
    ['Latin diacritic fold — accented', 'Résumé Café', 'resume cafe'],
    ['Latin diacritic fold — e-acute', 'Renée', 'renee'],
    // Japanese dakuten U+3099 is OUTSIDE the stripped range [U+0300–U+036F]
    // and MUST survive. が (U+304C) decomposes to か(U+304B) + ゛(U+3099) in
    // NFD; U+3099 is not stripped; re-NFC gives が again.
    ['Japanese dakuten preserved — が', 'が', 'が'],
    ['Japanese dakuten preserved — phrase', '剣道が好き', '剣道が好き'],
    // CJK characters pass through untouched.
    ['CJK zekken — 渡邉', '渡邉', '渡邉'],
    ['CJK zekken — 早大 堀池', '早大 堀池', '早大 堀池'],
    ['empty string', '', ''],
    ['already normalized', 'alice smith', 'alice smith'],
  ];

  cases.forEach(([desc, input, expected]) => {
    it(desc, () => {
      expect(normalizeParticipantName(input)).toBe(expected);
    });
  });
});

// ---------------------------------------------------------------------------
// Shared fixture: Go must produce byte-identical output for these inputs.
// Keep this table in sync with the Go TestNormalizeParticipantName table.
// ---------------------------------------------------------------------------

describe('normalizeParticipantName — shared fixture (Go parity)', () => {
  // These are the same cases tested in dedup_test.go.
  it('Müller → muller (Go parity)', () => {
    expect(normalizeParticipantName('Müller')).toBe('muller');
  });
  it('が → が (Go parity — dakuten preserved)', () => {
    expect(normalizeParticipantName('が')).toBe('が');
  });
  it('渡邉 → 渡邉 (Go parity — CJK)', () => {
    expect(normalizeParticipantName('渡邉')).toBe('渡邉');
  });
});
