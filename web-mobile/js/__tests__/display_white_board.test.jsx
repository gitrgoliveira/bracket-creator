import { describe, it, expect } from 'vitest';
import { overlayPositionLabel, tvwIsScored, TvWhiteBoard } from '../display.jsx';

// mp-13y: white TvDisplay scoreboard + position-label fallback.

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

describe('tvwIsScored', () => {
  it('true when a side has an ippon', () => {
    expect(tvwIsScored({ ipponsA: ['M'], ipponsB: [] })).toBe(true);
  });
  it('false for an empty bout', () => {
    expect(tvwIsScored({ ipponsA: [], ipponsB: [] })).toBe(false);
  });
});

function teamPromoted() {
  return {
    kind: 'live',
    match: {
      id: 'm1', round: 'Round 1',
      sideA: { name: 'Red Team' }, sideB: { name: 'White Team' },
      subResults: [
        { position: 1, ipponsB: ['M'], ipponsA: [] },
        { position: 2, ipponsB: [], ipponsA: [] }, // unfought → in progress
      ],
    },
    competition: { id: 'c1', name: 'Teams', kind: 'team', teamSize: 5 },
    isBracket: false,
  };
}

function render(props) {
  return JSON.stringify(TvWhiteBoard(props));
}

// Walk the vnode tree and collect every bout-row child vnode (TvWhiteBoutRow
// is a child component, so its rendered testid isn't in the parent's
// stringified tree — identify it by its `sub` prop instead).
function collectBoutRows(vnode, out = []) {
  if (!vnode || typeof vnode !== 'object') return out;
  if (Array.isArray(vnode)) { vnode.forEach((v) => collectBoutRows(v, out)); return out; }
  const props = vnode.props || {};
  if (props.sub && (typeof props.index === 'number')) out.push(props);
  if (vnode.children) collectBoutRows(vnode.children, out);
  if (props.children) collectBoutRows(props.children, out);
  return out;
}

describe('TvWhiteBoard', () => {
  const base = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };

  it('renders a white board for a live team match with bout rows and NO "LIVE"', () => {
    const p = teamPromoted();
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const str = render(props);
    expect(str).toContain('tvd--white');
    expect(str).toContain('tvd-team-bouts');
    expect(str).toContain('White Team');
    expect(str).toContain('Red Team');
    expect(str).not.toContain('LIVE');
    const rows = collectBoutRows(TvWhiteBoard(props));
    expect(rows.length).toBe(2); // 2 regular bouts
    expect(rows.some((r) => r.isDH)).toBe(false);
  });

  it('individual match keeps the ippon score and shows no bout grid', () => {
    const p = {
      kind: 'live',
      match: { id: 'i1', round: 'Round 1', sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
        ipponsB: ['K'], ipponsA: ['M'], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const str = render({ ...base, promoted: p, promotedKind: 'live', isTeamMatch: false,
      subResults: [], teamSize: 0 });
    expect(str).toContain('tvd--white');
    expect(str).not.toContain('tvd-team-bouts');
    expect(str).toContain('Shiro P');
    expect(str).toContain('Aka P');
    expect(str).not.toContain('LIVE');
  });

  it('shows the Daihyosen banner row when showDH and a DH sub exists', () => {
    const p = teamPromoted();
    p.match.subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: -1, ipponsB: ['M'], ipponsA: [] },
    ];
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5, showDH: true };
    const rows = collectBoutRows(TvWhiteBoard(props));
    expect(rows.some((r) => r.isDH)).toBe(true); // DH row rendered
  });
});
