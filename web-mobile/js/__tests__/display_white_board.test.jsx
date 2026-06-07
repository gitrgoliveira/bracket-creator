import { describe, it, expect } from 'vitest';
import { overlayPositionLabel, TvWhiteBoard } from '../display.jsx';
import { TeamScoreboard, IndividualScore } from '../match_scoreboard.jsx';

// mp-13y: white TvDisplay board. The board is TV CHROME (court header, team-name
// row, NEXT, sponsor) that delegates the scoreboard body to the SHARED
// match_scoreboard.jsx components (TeamScoreboard / IndividualScore) — the same
// ones the viewer card uses. The scoreboard's own rendering (slots, IV/PW
// summary, DH banner) is covered by match_scoreboard.test.jsx.

describe('overlayPositionLabel — FIK names only for 5-person teams', () => {
  it('returns Senpo..Taisho for a 5-person team', () => {
    expect(overlayPositionLabel(5, 0, {})).toBe('Senpo');
    expect(overlayPositionLabel(5, 2, {})).toBe('Chuken');
    expect(overlayPositionLabel(5, 4, {})).toBe('Taisho');
  });
  it('falls back to the bare bout number for non-5 teams (3, 7, kachinuki 11/15)', () => {
    expect(overlayPositionLabel(3, 0, {})).toBe('1');
    expect(overlayPositionLabel(3, 2, {})).toBe('3');
    expect(overlayPositionLabel(11, 9, {})).toBe('10');
    expect(overlayPositionLabel(15, 14, {})).toBe('15');
  });
  it('returns Daihyosen for the rep bout (position === -1)', () => {
    expect(overlayPositionLabel(5, 0, { position: -1 })).toBe('Daihyosen');
  });
  it('uses an explicit string position verbatim when present', () => {
    expect(overlayPositionLabel(7, 0, { position: 'Jiho' })).toBe('Jiho');
  });
});

function teamPromoted(promotedKind = 'live') {
  return {
    kind: promotedKind,
    match: {
      id: 'm1', round: 'Round 1',
      sideA: { name: 'Red Team' }, sideB: { name: 'White Team' },
      subResults: [
        { position: 1, ipponsB: ['M'], ipponsA: [] },
        { position: 2, ipponsB: [], ipponsA: [] },
      ],
    },
    competition: { id: 'c1', name: 'Teams', kind: 'team', teamSize: 5 },
    isBracket: false,
  };
}

function render(props) { return JSON.stringify(TvWhiteBoard(props)); }

// Depth-first search for a vnode matching the predicate (TvWhiteBoard delegates
// the body to a child component vnode, so we assert on its type + props).
function findVnode(node, pred) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) { for (const k of node) { const f = findVnode(k, pred); if (f) return f; } return null; }
  if (pred(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) { const f = findVnode(k, pred); if (f) return f; }
  return null;
}

describe('TvWhiteBoard', () => {
  const base = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };

  it('renders a white board for a live team match, delegating to TeamScoreboard, NO "LIVE"', () => {
    const p = teamPromoted();
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const str = render(props);
    expect(str).toContain('tvd--white');
    expect(str).toContain('tvd-team-bouts');
    expect(str).toContain('White Team');
    expect(str).toContain('Red Team');
    expect(str).not.toContain('LIVE');
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.variant).toBe('tv');
    expect(sb.props.subResults.length).toBe(2);
  });

  it('delegates an individual match to IndividualScore (no team bout grid)', () => {
    const p = {
      kind: 'live',
      match: { id: 'i1', round: 'Round 1', sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
        ipponsB: ['K'], ipponsA: ['M'], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: false, subResults: [], teamSize: 0 };
    const str = render(props);
    expect(str).toContain('tvd--white');
    expect(str).not.toContain('tvd-team-bouts');
    expect(str).toContain('Shiro P');
    expect(str).toContain('Aka P');
    expect(str).not.toContain('LIVE');
    expect(findVnode(TvWhiteBoard(props), n => n.type === IndividualScore)).toBeTruthy();
    expect(findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard)).toBeNull();
  });

  it('passes showDH to TeamScoreboard when a DH sub exists', () => {
    const p = teamPromoted();
    p.match.subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: -1, ipponsB: ['M'], ipponsA: [] },
    ];
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5, showDH: true };
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.showDH).toBe(true);
  });

  it('an up-next team match shows "Starts soon" (not an empty bout grid)', () => {
    const p = teamPromoted('upnext');
    p.match.subResults = [];
    const props = { ...base, promoted: p, promotedKind: 'upnext', isTeamMatch: true, subResults: [], teamSize: 5 };
    const str = render(props);
    expect(str).toContain('Starts soon');
    expect(str).toContain('up next');
    expect(findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard)).toBeNull();
  });
});
