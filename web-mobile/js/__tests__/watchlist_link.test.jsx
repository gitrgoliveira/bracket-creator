import { describe, it, expect } from 'vitest';
import {
  WATCHLIST_PARAM,
  watchlistTokens,
  buildWatchlistLink,
  parseWatchlistTokens,
  resolveWatchlistTokens,
  watchlistLinkFitsQR,
} from '../watchlist_link.jsx';
import { buildRoster, normalizeWatchlist, WATCHLIST_MAX } from '../viewer_watchlist_core.jsx';

// watchlist_link.jsx (bc-wlpl): the watchlist as a shareable permalink. Its
// own header states the per-entry token rule and why the query is read raw
// instead of through URLSearchParams -- this suite exercises both.

const comp = (id, status, players) => ({ id, name: id, status, checkInEnabled: false, players });

describe('round trip: a mixed watchlist survives encode -> parse -> resolve', () => {
  // One numbered competitor, one pre-draw competitor (no number yet), and a
  // dojo entry, all in the same list -- the shape the module header calls out
  // as "one pre-draw competitor does not cost the QR for thirty numbered ones".
  const bobId = 'e2b1f6b0-9c2b-4a41-9c1a-2f7a6b8d1234';
  const competitions = [
    comp('A', 'running', [
      { id: 'p1', name: 'Alice', dojo: 'Shibuya Kendo Club', number: 'K12' },
      { id: bobId, name: 'Bob', dojo: 'Osaka Kendo Club', number: '' },
    ]),
  ];
  const roster = buildRoster(competitions);
  const watchlist = [
    { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya Kendo Club' },
    { type: 'player', id: bobId, name: 'Bob', dojo: 'Osaka Kendo Club' },
    { type: 'dojo', dojo: 'Hagane Dojo' },
  ];

  it('reproduces the same entries after a full round trip through the URL', () => {
    const url = buildWatchlistLink('https://example.test/viewer', watchlist, roster);
    const search = url.slice(url.indexOf('?'));
    const tokens = parseWatchlistTokens(search);
    const resolved = resolveWatchlistTokens(tokens, roster);
    expect(resolved).toEqual([
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya Kendo Club' },
      { type: 'player', id: bobId, name: 'Bob', dojo: 'Osaka Kendo Club' },
      { type: 'dojo', dojo: 'Hagane Dojo' },
    ]);
  });

  it('encodes per entry: the numbered competitor by number, the numberless one by id, in ONE list', () => {
    const tokens = watchlistTokens(watchlist, roster);
    expect(tokens).toEqual(['K12', bobId, 'd~Hagane%20Dojo']);
  });
});

describe('the delimiter collision: a dojo name carrying the separator itself', () => {
  it('round-trips a dojo name with a literal comma, a dot, and an apostrophe exactly', () => {
    // "St. Mary's Kendo Club, Bristol" carries every character the header
    // warns about: the dot survives encodeURIComponent untouched (so a naive
    // split on "." would be fine here but was never the risk), while the
    // comma is exactly SEP -- if it ever reached the split as a literal
    // comma, the name would break into two tokens.
    const dojoName = "St. Mary's Kendo Club, Bristol";
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' }]),
    ]);
    const watchlist = [
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' },
      { type: 'dojo', dojo: dojoName },
    ];

    const url = buildWatchlistLink('https://example.test/viewer', watchlist, roster);
    const search = url.slice(url.indexOf('?'));

    // The comma must survive in the URL as %2C, never as a literal ",": a
    // literal one there is indistinguishable from the real entry separator.
    expect(search).toContain('%2C');
    expect(search.split(',')).toHaveLength(2); // exactly one real SEP, between the two entries

    const tokens = parseWatchlistTokens(search);
    expect(tokens).toHaveLength(2); // the collision, if it existed, would split this into 3

    const resolved = resolveWatchlistTokens(tokens, roster);
    expect(resolved).toEqual([
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' },
      { type: 'dojo', dojo: dojoName },
    ]);
  });

  it('round-trips a non-ASCII dojo name', () => {
    const dojoName = '京都体育館';
    const watchlist = [{ type: 'dojo', dojo: dojoName }];
    const url = buildWatchlistLink('https://example.test/viewer', watchlist, []);
    const search = url.slice(url.indexOf('?'));
    const resolved = resolveWatchlistTokens(parseWatchlistTokens(search), []);
    expect(resolved).toEqual([{ type: 'dojo', dojo: dojoName }]);
  });
});

describe('watchlistLinkFitsQR', () => {
  it('is true exactly at the byte limit and false one byte over', () => {
    // ASCII, so bytes === characters here: this pins the boundary itself,
    // not just "small passes, huge fails".
    const atLimit = 'a'.repeat(20);
    const overLimit = 'a'.repeat(21);
    expect(new TextEncoder().encode(atLimit).length).toBe(20);
    expect(watchlistLinkFitsQR(atLimit, 20)).toBe(true);
    expect(watchlistLinkFitsQR(overLimit, 20)).toBe(false);
  });

  it('is false for an empty url, whatever the limit', () => {
    expect(watchlistLinkFitsQR('', 213)).toBe(false);
  });

  it('is false when maxBytes is 0 or undefined, even for a tiny url', () => {
    expect(watchlistLinkFitsQR('https://x', 0)).toBe(false);
    expect(watchlistLinkFitsQR('https://x', undefined)).toBe(false);
  });

  it('measures BYTES, not characters: a multi-byte character pushes a SHORT string over a limit its character count would pass', () => {
    // "京" is 3 bytes in UTF-8. Five of them is 5 characters (well under a
    // 10-character reading of the limit) but 15 bytes (over a 10-byte one).
    const url = '京'.repeat(5);
    expect(url.length).toBeLessThanOrEqual(10); // character count alone would pass
    expect(new TextEncoder().encode(url).length).toBe(15);
    expect(watchlistLinkFitsQR(url, 10)).toBe(false);
  });
});

describe('resolveWatchlistTokens drops what does not resolve, keeps the rest', () => {
  const roster = buildRoster([
    comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' }]),
  ]);

  it('drops a stale/unknown token -- a competitor who left the roster, or a number re-minted by a re-draw', () => {
    const resolved = resolveWatchlistTokens(['K1', 'K99', 'not-a-real-id'], roster);
    expect(resolved).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' }]);
  });

  it('a malformed percent-escape is dropped by parseWatchlistTokens without throwing, and the rest of the list still resolves', () => {
    const search = `?${WATCHLIST_PARAM}=K1,%zz,K99`;
    let tokens;
    expect(() => { tokens = parseWatchlistTokens(search); }).not.toThrow();
    expect(tokens).toEqual(['K1', 'K99']); // "%zz" decoded to "" by safeDecodeToken and filtered out
    expect(resolveWatchlistTokens(tokens, roster)).toEqual([
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' },
    ]);
  });
});

describe('resolution precedence: id before number, matching resolveDeepLink', () => {
  it('a token equal to one competitor\'s id AND another competitor\'s number resolves to the ID match', () => {
    const roster = [
      { id: 'DUPTOK', name: 'IdOwner', dojo: 'X', numbers: [] },
      { id: 'other-id', name: 'NumberOwner', dojo: 'Y', numbers: ['DUPTOK'] },
    ];
    expect(resolveWatchlistTokens(['DUPTOK'], roster)).toEqual([
      { type: 'player', id: 'DUPTOK', name: 'IdOwner', dojo: 'X' },
    ]);
  });
});

describe('number resolution is exact and case-sensitive (machine-generated tokens, not typed search)', () => {
  it('"k12" does not resolve a competitor numbered "K12"', () => {
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K12' }]),
    ]);
    expect(resolveWatchlistTokens(['k12'], roster)).toEqual([]);
    expect(resolveWatchlistTokens(['K12'], roster)).toEqual([
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' },
    ]);
  });
});

describe('buildWatchlistLink returns "" when there is nothing shareable, so callers can hide the control', () => {
  it('for an empty watchlist', () => {
    expect(buildWatchlistLink('https://example.test/viewer', [], [])).toBe('');
  });

  it('for a watchlist whose entries carry no usable identity (no id, no dojo name)', () => {
    const watchlist = [
      { type: 'player', id: '', name: 'Ghost' },
      { type: 'dojo', dojo: '' },
    ];
    expect(buildWatchlistLink('https://example.test/viewer', watchlist, [])).toBe('');
  });
});

// The app's own merge rule (resolveDeepLink's contract, restated in the
// module header): opening a link ADDS to the device's existing watchlist
// rather than replacing it. Import normalizeWatchlist rather than
// reimplementing its dedup/cap here.
describe('merge semantics: normalizeWatchlist([...existing, ...resolved])', () => {
  it('keeps existing entries and appends only genuinely new ones', () => {
    const existing = [{ type: 'player', id: 'p1', name: 'Alice (local)', dojo: 'Shibuya' }];
    const resolved = [
      { type: 'player', id: 'p1', name: 'Alice (from link)', dojo: 'Shibuya' }, // duplicate: existing wins
      { type: 'player', id: 'p2', name: 'Bob', dojo: 'Osaka' }, // genuinely new
    ];
    const merged = normalizeWatchlist([...existing, ...resolved]);
    expect(merged).toEqual([
      { type: 'player', id: 'p1', name: 'Alice (local)', dojo: 'Shibuya' }, // first occurrence wins
      { type: 'player', id: 'p2', name: 'Bob', dojo: 'Osaka' },
    ]);
  });

  it('caps the merge at WATCHLIST_MAX, dropping the newly-resolved entry once the existing list already fills it', () => {
    const existing = Array.from({ length: WATCHLIST_MAX }, (_, i) => (
      { type: 'player', id: `existing-${i}`, name: `P${i}`, dojo: 'X' }
    ));
    const resolved = [{ type: 'player', id: 'new-from-link', name: 'NewOne', dojo: 'Y' }];
    const merged = normalizeWatchlist([...existing, ...resolved]);
    expect(merged).toHaveLength(WATCHLIST_MAX);
    expect(merged.some((e) => e.id === 'new-from-link')).toBe(false);
  });
});
