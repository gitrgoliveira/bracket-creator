// bc-hnam, the client half: a competition that hides its names from this
// reader sends each competitor's number where the name goes, and marks the
// payload `namesHidden`. These cases pin what the client does with it:
//   - the aggregate read hoists the marker and every record is stamped
//     nameHidden (normalizeViewerCompItem -> normalizeCompetitionDetail ->
//     buildRoster), a revealed competitor (a real name beside the number) not;
//   - a hidden record is found by its number only, never by the text of its
//     printed "name" (competitorMatchesQuery);
//   - the watchlist keeps no name for one, drops a name it stored before, and
//     a shared link resolves to an entry with none;
//   - the number badge prints once when the name IS the number.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { nameHidden, isMaskedName, nameBesideBadge } from '../competitor_identity.jsx';
import { competitorMatchesQuery } from '../competitor_search.jsx';
import {
  buildRoster, addPlayerToWatchlist, normalizeWatchlistEntry, scrubHiddenWatchNames,
  resolveWatchedPlayers,
} from '../viewer_watchlist_core.jsx';
import { watchlistTokens, resolveWatchlistTokens } from '../watchlist_link.jsx';
import { NumberedName } from '../numbered_name.jsx';
import { sideLabel } from '../display_helpers.jsx';
import { buildPlayerMap, normalizeMatch, normalizeCompetitionDetail } from '../api_serializers.jsx';
import { collectText } from './helpers/vdom.js';

function jsonOnce(body) {
  return vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) }));
}

const hiddenItem = () => ({
  config: {
    id: 'c1', name: 'Autumn Cup', kind: 'individual', numberPrefix: 'K',
    players: [
      { id: 'p1', name: 'K1', number: 'K1', dojo: 'Dojo A' },
      // Revealed to this reader by their own link: a real name beside the number.
      { id: 'p2', name: 'Tanaka', number: 'K2', dojo: 'Dojo B' },
    ],
  },
  poolMatches: [],
  bracket: null,
  namesHidden: true,
});

describe('isMaskedName: the one stamping rule', () => {
  it('hides nothing in a payload that is not marked', () => {
    expect(isMaskedName(false, 'K1', 'K1')).toBe(false);
    expect(isMaskedName(undefined, '', '')).toBe(false);
  });
  it('hides a name that is the number, empty, or the fail-closed "?"', () => {
    expect(isMaskedName(true, 'K1', 'K1')).toBe(true);
    expect(isMaskedName(true, '', '')).toBe(true);
    expect(isMaskedName(true, '?', 'K1')).toBe(true);
  });
  it('keeps a revealed competitor (a real name beside the number)', () => {
    expect(isMaskedName(true, 'Tanaka', 'K2')).toBe(false);
  });
});

describe('the aggregate read stamps nameHidden', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('hoists namesHidden and stamps the masked record only', async () => {
    global.fetch = jsonOnce([hiddenItem()]);
    const comps = await API.fetchCompetitions();
    expect(comps).toHaveLength(1);
    expect(comps[0].namesHidden).toBe(true);
    const byId = new Map(comps[0].players.map((p) => [p.id, p]));
    expect(nameHidden(byId.get('p1'))).toBe(true);
    expect(nameHidden(byId.get('p2'))).toBe(false);

    const roster = buildRoster(comps, { competitions: comps });
    const rById = new Map(roster.map((p) => [p.id, p]));
    expect(nameHidden(rById.get('p1'))).toBe(true);
    expect(nameHidden(rById.get('p2'))).toBe(false);
  });

  it('stamps nothing when the payload is not marked', async () => {
    const it0 = hiddenItem();
    delete it0.namesHidden;
    global.fetch = jsonOnce([it0]);
    const comps = await API.fetchCompetitions();
    expect(comps[0].players.some((p) => nameHidden(p))).toBe(false);
  });
});

describe('the competition detail carries the marker on its config', () => {
  // The public competition page renders off `config`, and its names-hidden
  // note reads `config.namesHidden`: the marker, a sibling of config on the
  // wire, is copied onto it.
  it('a marked detail marks its config', () => {
    expect(normalizeCompetitionDetail(hiddenItem()).config.namesHidden).toBe(true);
  });
  it('an unmarked detail leaves its config unmarked', () => {
    const it0 = hiddenItem();
    delete it0.namesHidden;
    expect(normalizeCompetitionDetail(it0).config.namesHidden).toBeUndefined();
  });
});

describe('a side printed "?" takes no one else\'s identity', () => {
  // Two unnumbered competitors in a hiding competition both read "?": neither
  // may be found by that name, or a side the server printed "?" with no id
  // would take the last one's id, dojo and number.
  const comp = {
    namesHidden: true,
    players: [
      { id: 'p1', name: '?', dojo: 'Dojo A' },
      { id: 'p2', name: '?', dojo: 'Dojo B' },
    ],
  };

  it('a "?" side with no id stays id-less and hidden', () => {
    const m = normalizeMatch({ id: 'm1', sideA: '?', sideB: '?', status: 'scheduled' }, buildPlayerMap(comp), true);
    expect(m.sideA.id).toBe('');
    expect(m.sideA.dojo).toBeUndefined();
    expect(nameHidden(m.sideA)).toBe(true);
  });

  it('a "?" side with an id resolves by that id', () => {
    const m = normalizeMatch({ id: 'm1', sideA: '?', sideAId: 'p1', sideB: '?', sideBId: 'p2', status: 'scheduled' }, buildPlayerMap(comp), true);
    expect(m.sideA.dojo).toBe('Dojo A');
    expect(m.sideB.dojo).toBe('Dojo B');
    expect(nameHidden(m.sideA)).toBe(true);
  });
});

describe('a hidden record is found by its number only', () => {
  const hidden = { id: 'p1', name: 'K12', number: 'K12', numberPrefix: 'K', dojo: 'Dojo A', nameHidden: true };
  it('is not found by a fragment of its printed number', () => {
    expect(competitorMatchesQuery(hidden, '12')).toBe(false);
    expect(competitorMatchesQuery(hidden, 'k1')).toBe(false);
  });
  it('is found by its whole number, its prefix and its dojo', () => {
    expect(competitorMatchesQuery(hidden, 'k12')).toBe(true);
    expect(competitorMatchesQuery(hidden, 'k')).toBe(true);
    expect(competitorMatchesQuery(hidden, 'dojo a')).toBe(true);
  });
  it('a visible record keeps its name arm', () => {
    expect(competitorMatchesQuery({ ...hidden, name: 'Tanaka', nameHidden: undefined }, 'tana')).toBe(true);
  });
});

describe('the watchlist keeps no name for a hidden competitor', () => {
  const roster = [
    { id: 'p1', name: 'K1', number: 'K1', dojo: 'Dojo A', nameHidden: true },
    { id: 'p2', name: 'Tanaka', number: 'K2', dojo: 'Dojo B' },
  ];

  it('adds a hidden competitor by id and number, with no name', () => {
    const list = addPlayerToWatchlist([], roster[0]);
    expect(list).toEqual([{ type: 'player', id: 'p1', name: '', dojo: 'Dojo A', number: 'K1', nameHidden: true }]);
    expect(addPlayerToWatchlist([], roster[1])[0].name).toBe('Tanaka');
  });

  it('a stored hidden entry never carries a name back in', () => {
    const e = normalizeWatchlistEntry({ type: 'player', id: 'p1', name: 'Leaked', nameHidden: true, number: 'K1' });
    expect(e.name).toBe('');
    expect(e.number).toBe('K1');
  });

  it('drops a name stored before the competition started hiding', () => {
    const stored = [
      { type: 'player', id: 'p1', name: 'Suzuki', dojo: 'Dojo A' },
      { type: 'player', id: 'p2', name: 'Tanaka', dojo: 'Dojo B' },
      { type: 'dojo', dojo: 'Dojo C' },
    ];
    const next = scrubHiddenWatchNames(stored, roster);
    expect(next).not.toBe(stored);
    expect(next[0]).toEqual({ type: 'player', id: 'p1', name: '', dojo: 'Dojo A', number: 'K1', nameHidden: true });
    expect(next[1]).toBe(stored[1]);
    expect(next[2]).toBe(stored[2]);
    expect(JSON.stringify(next)).not.toContain('Suzuki');
    // Nothing left to drop: the same list back, so the hook writes nothing.
    expect(scrubHiddenWatchNames(next, roster)).toBe(next);
  });

  it('a hidden entry the roster no longer holds resolves to its number, not a name', () => {
    const [w] = resolveWatchedPlayers([{ type: 'player', id: 'gone', name: '', number: 'K9', dojo: 'D', nameHidden: true }], []);
    expect(w.name).toBe('K9');
    expect(w.nameHidden).toBe(true);
  });

  it('a shared link carries the number and resolves to an entry with no name', () => {
    const stored = [{ type: 'player', id: 'p1', name: 'Suzuki', dojo: 'Dojo A' }];
    const tokens = watchlistTokens(stored, roster);
    expect(tokens).toEqual(['K1']);
    const [entry] = resolveWatchlistTokens([{ kind: 'competitor', value: 'K1' }], roster);
    expect(entry).toEqual({ type: 'player', id: 'p1', name: '', dojo: 'Dojo A', number: 'K1', nameHidden: true });
  });
});

describe('the number badge prints once when the name is the number', () => {
  it('nameBesideBadge empties the name only when it is the number', () => {
    expect(nameBesideBadge('K12', 'K12')).toBe('');
    expect(nameBesideBadge('Tanaka', 'K12')).toBe('Tanaka');
    expect(nameBesideBadge('K12', '')).toBe('K12');
  });

  it('NumberedName renders [K12] once, on either side', () => {
    expect(collectText(NumberedName({ name: 'K12', number: 'K12' }))).toBe('K12');
    expect(collectText(NumberedName({ side: 'aka', name: 'K12', number: 'K12' }))).toBe('K12');
    expect(collectText(NumberedName({ name: 'Tanaka', number: 'K12' }))).toBe('K12Tanaka');
  });

  it('the string form prints the number once', () => {
    expect(sideLabel({ name: 'K12', number: 'K12' })).toBe('K12');
    expect(sideLabel({ name: 'K12', number: 'K12' }, false, 'aka')).toBe('K12');
  });
});
