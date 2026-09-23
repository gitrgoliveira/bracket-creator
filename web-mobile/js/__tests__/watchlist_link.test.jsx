import { describe, it, expect } from 'vitest';
import {
  WATCHLIST_PARAM,
  watchlistTokens,
  buildWatchlistLink,
  parseWatchlistTokens,
  resolveWatchlistTokens,
  stripWatchlistParam,
} from '../watchlist_link.jsx';
import { buildRoster } from '../viewer_watchlist_core.jsx';

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
    expect(tokens).toEqual(['K12', bobId, ':Hagane%20Dojo']);
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

describe('the ":" sentinel does not collide with a real competitor number or dojo name', () => {
  // The first version of DOJO_SENTINEL was "d~" and was ambiguous: nothing
  // stops an operator picking "d~" as a competition's number prefix
  // (helper.ValidateNumberPrefix checks length only), so a real roster can
  // hand this module a competitor number that looks exactly like the OLD
  // marker. Under "d~" that competitor silently became a dojo entry named
  // "1" and was dropped. ":" cannot repeat this: it is in the set
  // encodeURIComponent ALWAYS escapes, so an encoded competitor number or id
  // can never begin with a literal ":" (see DOJO_SENTINEL's header).
  it('a competitor numbered "d~1" round-trips as a COMPETITOR, never a dojo named "1"', () => {
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'd~1' }]),
    ]);
    const watchlist = [{ type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' }];

    const url = buildWatchlistLink('https://example.test/viewer', watchlist, roster);
    const search = url.slice(url.indexOf('?'));
    const tokens = parseWatchlistTokens(search);
    expect(tokens).toEqual([{ kind: 'competitor', value: 'd~1' }]);

    const resolved = resolveWatchlistTokens(tokens, roster);
    expect(resolved).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' }]);
  });

  it('a dojo whose name begins with ":" still round-trips correctly', () => {
    // The marker is read on the RAW (pre-decode) token, and encodeURIComponent
    // always escapes ":", so a dojo name starting with a literal colon can
    // never produce a raw token starting with an UNESCAPED ":" of its own --
    // only the sentinel byte itself does, so tagging stays unambiguous.
    const dojoName = ':Colonised Dojo';
    const watchlist = [{ type: 'dojo', dojo: dojoName }];
    const url = buildWatchlistLink('https://example.test/viewer', watchlist, []);
    const search = url.slice(url.indexOf('?'));
    const tokens = parseWatchlistTokens(search);
    expect(tokens).toEqual([{ kind: 'dojo', value: dojoName }]);

    const resolved = resolveWatchlistTokens(tokens, []);
    expect(resolved).toEqual([{ type: 'dojo', dojo: dojoName }]);
  });
});


describe('resolveWatchlistTokens drops what does not resolve, keeps the rest', () => {
  const roster = buildRoster([
    comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' }]),
  ]);

  it('drops a stale/unknown token -- a competitor who left the roster, or a number re-minted by a re-draw', () => {
    const resolved = resolveWatchlistTokens(
      [
        { kind: 'competitor', value: 'K1' },
        { kind: 'competitor', value: 'K99' },
        { kind: 'competitor', value: 'not-a-real-id' },
      ],
      roster,
    );
    expect(resolved).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' }]);
  });

  it('a malformed percent-escape is dropped by parseWatchlistTokens without throwing, and the rest of the list still resolves', () => {
    const search = `?${WATCHLIST_PARAM}=K1,%zz,K99`;
    let tokens;
    expect(() => { tokens = parseWatchlistTokens(search); }).not.toThrow();
    // "%zz" decoded to "" by safeDecodeToken and filtered out
    expect(tokens).toEqual([
      { kind: 'competitor', value: 'K1' },
      { kind: 'competitor', value: 'K99' },
    ]);
    expect(resolveWatchlistTokens(tokens, roster)).toEqual([
      { type: 'player', id: 'p1', name: 'Alice', dojo: 'Shibuya' },
    ]);
  });
});

describe('resolution precedence: id before number, matching resolveDeepLink', () => {
  it('a token equal to one competitor\'s id AND another competitor\'s number resolves to the ID match', () => {
    const roster = [
      { id: 'DUPTOK', name: 'IdOwner', dojo: 'X', number: '' },
      { id: 'other-id', name: 'NumberOwner', dojo: 'Y', number: 'DUPTOK' },
    ];
    expect(resolveWatchlistTokens([{ kind: 'competitor', value: 'DUPTOK' }], roster)).toEqual([
      { type: 'player', id: 'DUPTOK', name: 'IdOwner', dojo: 'X' },
    ]);
  });
});

describe('number resolution is exact and case-sensitive (machine-generated tokens, not typed search)', () => {
  it('"k12" does not resolve a competitor numbered "K12"', () => {
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K12' }]),
    ]);
    expect(resolveWatchlistTokens([{ kind: 'competitor', value: 'k12' }], roster)).toEqual([]);
    expect(resolveWatchlistTokens([{ kind: 'competitor', value: 'K12' }], roster)).toEqual([
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

// The merge half of "opening a link" lives in watchlist_merge.test.jsx, where
// it is asserted against mergeSharedWatchlist. It is deliberately NOT restated
// here: this suite had a copy that spelled `normalizeWatchlist([...a, ...b])`
// out by hand, which is the exact expression mergeSharedWatchlist exists to
// own and the exact one a bad commit once replaced with the shared list alone.

// A whitespace-only token is NO token. normalizeWatchlistEntry trims a dojo
// name and drops an empty one, so " " used to parse as a dojo entry that could
// never land -- on an EMPTY list, with no cap involved -- which is one of the
// two ways into the write loop sharedLinkPass closes.
describe('parseWatchlistTokens drops whitespace-only tokens', () => {
  it('a blank dojo token yields nothing', () => {
    expect(parseWatchlistTokens('?w=:%20')).toEqual([]);
    expect(parseWatchlistTokens('?w=:')).toEqual([]);
  });
  it('a blank competitor token yields nothing', () => {
    expect(parseWatchlistTokens('?w=%20')).toEqual([]);
  });
  it('surrounding whitespace is trimmed off a real token', () => {
    expect(parseWatchlistTokens('?w=%20K1%20')).toEqual([{ kind: 'competitor', value: 'K1' }]);
  });
});

// stripWatchlistParam: what the address bar keeps once a shared link has been
// folded in. The reason this is a function rather than one line in the effect
// is the COMPETITOR TAG: helper.playerTagURL prints each tag's QR as
// `<publicURL>/?playerNumber=K12`, so the viewer's query is not the watchlist's
// to clear.
describe('stripWatchlistParam', () => {
  it('removes w and keeps a tag QR\'s playerNumber', () => {
    // THE regression. Clearing the whole query spent a scanned tag that had
    // resolved to nobody (its competition had not loaded yet) with no way to
    // retry it, because the reload had nothing left to read.
    expect(stripWatchlistParam('?w=K1,K2&playerNumber=K12')).toBe('?playerNumber=K12');
    expect(stripWatchlistParam('?playerNumber=K12&w=K1')).toBe('?playerNumber=K12');
  });

  it('returns the query UNCHANGED when there is no w, so the caller leaves the URL alone', () => {
    // The caller compares before it calls replaceState, so "unchanged" here is
    // what keeps a tag link untouched rather than merely intact.
    expect(stripWatchlistParam('?playerNumber=K12')).toBe('?playerNumber=K12');
    expect(stripWatchlistParam('?player=3f2a&name=Ken')).toBe('?player=3f2a&name=Ken');
    expect(stripWatchlistParam('')).toBe('');
  });

  it('drops the query entirely when w was the only parameter', () => {
    expect(stripWatchlistParam('?w=K1,K2')).toBe('');
    expect(stripWatchlistParam('?w=')).toBe('');
  });

  it('does not match a parameter that merely starts with w', () => {
    expect(stripWatchlistParam('?watch=K1')).toBe('?watch=K1');
    expect(stripWatchlistParam('?w2=K1&w=K3')).toBe('?w2=K1');
  });

  it('leaves an encoded value alone, including one carrying = or ,', () => {
    // The dojo sentinel and separator ride inside a token; nothing here may
    // re-split or decode them, since this function only removes a parameter.
    expect(stripWatchlistParam('?q=a%3Db&w=%3AHagane%20Dojo')).toBe('?q=a%3Db');
  });
});
