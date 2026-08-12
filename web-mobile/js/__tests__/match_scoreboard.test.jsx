// mp-13y: the ONE shared FIK scoreboard (match_scoreboard.jsx) used by the
// viewer card, the self-run modal and the TV display. Covers the rendering the
// delegation tests in viewer.test.jsx / display_white_board.test.jsx don't see.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { matchMiddleMark, defaultWinMaru } from '../bracket.jsx';
import { boutRows, findInTree, collectText } from './helpers/vdom.js';

describe('match_scoreboard: withNumber', () => {
  let withNumber;
  beforeEach(async () => { vi.resetModules(); ({ withNumber } = await import('../match_scoreboard.jsx')); });
  afterEach(() => { vi.resetModules(); });

  it('prepends the assigned competitor number when present', () => {
    expect(withNumber({ name: 'Tanaka', number: 'K1' })).toBe('K1 Tanaka');
    expect(withNumber({ name: 'Suzuki', number: 'K12' })).toBe('K12 Suzuki');
  });
  it('returns the bare name when no number is set', () => {
    expect(withNumber({ name: 'Tanaka' })).toBe('Tanaka');
    expect(withNumber({ name: 'Tanaka', number: '' })).toBe('Tanaka');
  });
  it('returns "TBD" for null/undefined sides', () => {
    expect(withNumber(null)).toBe('TBD');
    expect(withNumber(undefined)).toBe('TBD');
  });
  it('returns plain-string sides verbatim (bracket placeholders)', () => {
    expect(withNumber('Pool A-1st')).toBe('Pool A-1st');
  });
  it('honours the zekken displayName when withZekkenName=true', () => {
    const side = { name: 'Tanaka Kenji', displayName: 'TANAKA', number: 'K1' };
    expect(withNumber(side, true)).toBe('K1 TANAKA');
    expect(withNumber(side, false)).toBe('K1 Tanaka Kenji');
  });
});

describe('match_scoreboard: teamIVPW', () => {
  let teamIVPW;
  beforeEach(async () => { vi.resetModules(); ({ teamIVPW } = await import('../match_scoreboard.jsx')); });
  afterEach(() => { vi.resetModules(); });

  it('counts individual victories + points won per side (shiro=B, aka=A)', () => {
    const subs = [
      { position: 1, ipponsB: ['M', 'K'], ipponsA: [] },   // shiro wins, 2 pts
      { position: 2, ipponsB: [], ipponsA: ['D'] },          // aka wins, 1 pt
      { position: 3, ipponsB: ['M'], ipponsA: ['M'] },       // tie 1-1, no IV
      { position: -1, ipponsB: ['M'], ipponsA: [] },         // DH excluded
    ];
    expect(teamIVPW(subs)).toEqual({ ivShiro: 1, ivAka: 1, pwShiro: 3, pwAka: 2 });
  });

  it('prefers the explicit winner when no ippon letters are recorded (quick-score / forfeit)', () => {
    const subs = [
      // winner set, no ippons: fusensho/kiken style. sideA=aka, sideB=shiro.
      { position: 1, sideA: 'Aka P', sideB: 'Shiro P', winner: 'Shiro P', ipponsA: [], ipponsB: [] },
      { position: 2, sideA: 'Aka P', sideB: 'Shiro P', winner: 'Aka P', ipponsA: [], ipponsB: [] },
    ];
    // IV counted from winner; PW stays 0 (no ippon points were scored).
    expect(teamIVPW(subs)).toEqual({ ivShiro: 1, ivAka: 1, pwShiro: 0, pwAka: 0 });
  });

  it('counts IV via match-level side names when sub-bout sides are empty (quick-score)', () => {
    const subs = [
      { position: 1, sideA: '', sideB: '', winner: 'Team Alpha', ipponsA: [], ipponsB: [] },
      { position: 2, sideA: '', sideB: '', winner: 'Team Alpha', ipponsA: [], ipponsB: [] },
      { position: 3, sideA: '', sideB: '', winner: 'Team Beta', ipponsA: [], ipponsB: [] },
    ];
    // matchSideA=aka(right)=Team Alpha, matchSideB=shiro(left)=Team Beta
    expect(teamIVPW(subs, 'Team Alpha', 'Team Beta')).toEqual({ ivShiro: 1, ivAka: 2, pwShiro: 0, pwAka: 0 });
  });

  it('does not false-positive on empty winner with empty sub-sides (draw)', () => {
    const subs = [
      { position: 1, sideA: '', sideB: '', winner: '', ipponsA: [], ipponsB: [] },
    ];
    expect(teamIVPW(subs, 'Team A', 'Team B')).toEqual({ ivShiro: 0, ivAka: 0, pwShiro: 0, pwAka: 0 });
  });

  it('credits AKA when the winner matches both sides, matching Go (invalid data)', () => {
    // A winner naming both sides is INVALID data (team names are unique by
    // rule); this pins the DEFENSIVE resolution: Go's isWinForSide /
    // TeamResultFrom / SideMarksLR all resolve it to side A (aka) by check
    // order, and the JS summary must agree with the server's numbers rather
    // than credit nobody and contradict the standings table.
    const subs = [
      { position: 1, sideA: '', sideB: '', winner: 'Seibukan', ipponsA: ['M'], ipponsB: ['M'] },
    ];
    expect(teamIVPW(subs, 'Seibukan', 'Seibukan')).toEqual({ ivShiro: 0, ivAka: 1, pwShiro: 1, pwAka: 1 });
  });

  it('rows and summary agree on cross-level ambiguity (shared subWinnerSides)', () => {
    // Drifted mixed-level data: the winner matches a SUB side on one flank and
    // the MATCH side on the other. Both the bout row and the IV summary go
    // through the one subWinnerSides resolver, which is aka-first like Go's
    // isWinForSide - so both attribute AKA, and neither can contradict the
    // other or the server's numbers.
    const subs = [
      { position: 1, sideA: 'X', sideB: '', winner: 'X', ipponsA: ['M'], ipponsB: ['M'] },
    ];
    expect(teamIVPW(subs, 'Y', 'X')).toEqual({ ivShiro: 0, ivAka: 1, pwShiro: 1, pwAka: 1 });
  });

  it('falls back to ippon comparison when winner matches neither side', () => {
    const subs = [
      { position: 1, sideA: 'Aka P', sideB: 'Shiro P', winner: '', ipponsB: ['M', 'K'], ipponsA: [] },
    ];
    expect(teamIVPW(subs)).toEqual({ ivShiro: 1, ivAka: 0, pwShiro: 2, pwAka: 0 });
  });
});

// What the centre cell actually holds. Asserting that a `sub-row-hantei` testid
// is absent would be VACUOUS: no production code emits a centre Ht any more, so
// such an expectation can never fail and would not catch a reintroduction under
// a different testid. Assert on the centre's contents instead.
const centreText = (tree) => collectText(findInTree(tree, n =>
  typeof n?.props?.className === 'string' && /\bmsb-vs\b/.test(n.props.className)));

describe('match_scoreboard components', () => {
  const realReact = global.React;
  let runtime, BoutSubRow, IndividualScore, TeamScoreboard;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.isHikiwake = vi.fn((t) => t === 'hikiwake');
    global.window.ipponsFromScore = vi.fn(() => []);
    global.window.matchMiddleMark = matchMiddleMark; // the real chip projection
    vi.resetModules();
    ({ BoutSubRow, IndividualScore, TeamScoreboard } = await import('../match_scoreboard.jsx'));
  });
  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.isHikiwake; delete global.window.ipponsFromScore; delete global.window.matchMiddleMark;
    vi.restoreAllMocks(); vi.resetModules();
  });

  it('BoutSubRow renders ippon letters in slots + bout-number fallback', () => {
    const sub = { position: 1, ipponsB: ['M', 'K'], ipponsA: ['D'] };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    const text = collectText(tree);
    expect(text).toContain('M'); expect(text).toContain('K'); expect(text).toContain('D');
    // No lineup → both names fall back to the bout number "1".
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-0')).toBeTruthy();
  });

  it('BoutSubRow marks a hikiwake with X', () => {
    const sub = { position: 1, ipponsB: [], ipponsA: [], score: { type: 'hikiwake' } };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-mid')).toBeTruthy();
  });

  it('BoutSubRow shows the red ▲ hansoku on the offending side only', () => {
    const sub = { position: 1, ipponsB: [], ipponsA: ['M'], hansokuB: 1, hansokuA: 0 };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-b')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-a')).toBeNull();
  });

  it('fills ippons from the OUTSIDE inward: aka (right) outer cell first', () => {
    // First point scored = letters[0] = OUTER cell. Shiro outer = left, aka
    // outer = right. So for two ippons D (1st) then M (2nd):
    //   shiro visual L→R = "D","M"  (D outer-left)
    //   aka   visual L→R = "M","D"  (D outer-right)  ← reversed
    const sub = { position: 1, ipponsB: ['D', 'M'], ipponsA: ['D', 'M'] };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    const akaSlots = findInTree(tree, n =>
      typeof n?.props?.className === 'string' && n.props.className.includes('msb-slots--aka'));
    const shiroSlots = findInTree(tree, n =>
      typeof n?.props?.className === 'string' && /\bmsb-slots\b/.test(n.props.className) && !n.props.className.includes('--aka'));
    expect(collectText(shiroSlots)).toBe('DM'); // outer-left first
    expect(collectText(akaSlots)).toBe('MD');   // outer-right first (reversed)
  });

  it('BoutSubRow falls back to sub.sideA / sub.sideB when no lineup is pinned (kachinuki)', () => {
    // mp-13y: kachinuki bouts carry the per-bout competitor names on the sub.
    // Without a lineup, the row should show those names: not bare bout numbers.
    const sub = { position: 3, sideA: 'Aka Player', sideB: 'Shiro Player', ipponsB: ['M'], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, { sub, index: 2, lineupA: null, lineupB: null, teamSize: 5 });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(collectText(shiro)).toBe('Shiro Player');
    expect(collectText(aka)).toBe('Aka Player');
  });

  it('BoutSubRow filters out match-level team names from sub-bout sides (quick-score path)', () => {
    // mp-3m1c: when scored via quick-score, the backend stores the TEAM name in
    // every sub-bout's sideA/sideB. Without the matchSideA/matchSideB filter,
    // the row would show "Team Alpha" on every row instead of the bout number.
    const sub = { position: 2, sideA: 'Team Alpha', sideB: 'Team Beta', ipponsA: [], ipponsB: [], winner: 'Team Beta' };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 1, lineupA: null, lineupB: null, teamSize: 5,
      matchSideA: 'Team Alpha', matchSideB: 'Team Beta',
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    // Should show bout number "#2", not team names
    expect(collectText(shiro)).toBe('#2');
    expect(collectText(aka)).toBe('#2');
  });

  it('BoutSubRow still shows real competitor names when they differ from match-level teams', () => {
    // Kachinuki: sub-bout sideA/sideB are individual competitor names, not team names.
    const sub = { position: 3, sideA: 'Tanaka', sideB: 'Suzuki', ipponsB: ['M'], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 2, lineupA: null, lineupB: null, teamSize: 5,
      matchSideA: 'Team Alpha', matchSideB: 'Team Beta',
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(collectText(shiro)).toBe('Suzuki');
    expect(collectText(aka)).toBe('Tanaka');
  });

  it('IndividualScore: same-name head-to-head does NOT mark BOTH sides as winners on an ippon-less decision', () => {
    // mp-13y: when both sides share a NAME and no ids disambiguate them, a
    // hantei/fusensho decision must not flag a win on both sides (the
    // centreMarks logic compares winner === sideA/sideB by key). Without the
    // guard, both shiro and aka would render a win mark.
    const match = {
      sideA: 'Same Name', sideB: 'Same Name',
      ipponsA: [], ipponsB: [], decidedByHantei: true,
      winner: 'Same Name',
    };
    const tree = runtime.mount(IndividualScore, { match });
    // Neither side wears the win mark. There is NO centre Ht either: the centre
    // carries shared marks only, so an unattributable winner gets no mark at
    // all rather than one in the shared cell. Unreachable through the API,
    // which requires a winner and disambiguates same-name pairs by uuid.
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeNull();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
    expect(centreText(tree)).toBe('vs');
    expect(collectText(tree)).not.toContain('Ht');
  });

  it('IndividualScore: a 1-1 hantei puts Ht in the winner\'s free slot, never the centre', () => {
    // A hantei is decided from a TIED scoreline (validation.go), so 1-1 is the
    // normal case after encho and both of the winner's slots are NOT free.
    // Ht fills the next free slot in the same outside-to-inside order a point
    // would, giving [K][ ] vs [Ht][M] with Aka (sideA) the hantei winner.
    const match = {
      sideA: { id: 'p1', name: 'Aka' }, sideB: { id: 'p2', name: 'Shiro' },
      ipponsA: ['M'], ipponsB: ['K'], decidedByHantei: true,
      winner: { id: 'p1', name: 'Aka' },
    };
    const tree = runtime.mount(IndividualScore, { match });
    // Never in the shared centre cell: it stays the plain separator.
    expect(centreText(tree)).toBe('vs');
    // The scored M survives: Ht is added, it does not replace the point.
    const text = collectText(tree);
    expect(text).toContain('Ht');
    expect(text).toContain('M');
    expect(text).toContain('K');
    // Aka's group renders inner-to-outer, so the free INNER slot holds Ht and
    // the outer (name-side) slot keeps M: reading left to right, Ht then M.
    expect(text.indexOf('Ht')).toBeLessThan(text.lastIndexOf('M'));
    // The win mark rides on Aka, not Shiro.
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
  });

  it('IndividualScore threads encho: the centre can read (E)', () => {
    // Regression: the synthesised sub dropped `encho`, so matchMiddleMark could
    // never produce (E) on an individual row — and these rows carry no separate
    // centre chip to compensate, so the overtime was invisible.
    const match = {
      sideA: { id: 'p1', name: 'Aka' }, sideB: { id: 'p2', name: 'Shiro' },
      ipponsA: ['M'], ipponsB: [], winner: { id: 'p1', name: 'Aka' },
      encho: { periodCount: 1 },
    };
    const tree = runtime.mount(IndividualScore, { match });
    expect(centreText(tree)).toBe('(E)');
  });

  it('IndividualScore threads encho: a default win in overtime is ONE maru', () => {
    // FIK marks a default win with one maru per awarded point: two in
    // regulation, ONE in encho. Without the encho passthrough every default win
    // rendered the regulation pair.
    global.window.defaultWinMaru = defaultWinMaru;
    const match = {
      sideA: { id: 'p1', name: 'Aka' }, sideB: { id: 'p2', name: 'Shiro' },
      ipponsA: [], ipponsB: [], winner: { id: 'p1', name: 'Aka' },
      decision: 'fusensho', encho: { periodCount: 1 },
    };
    // Collect the whole aka SLOT GROUP: the sub-win-a testid sits on cell 0
    // only, which reads a single maru in both cases and would not distinguish.
    const tree = runtime.mount(IndividualScore, { match });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeTruthy();
    expect(collectText(findInTree(tree, n =>
      typeof n?.props?.className === 'string' && n.props.className.includes('msb-slots--aka')))).toBe('\u25cb');
    delete global.window.defaultWinMaru;
  });

  it('IndividualScore: a default win in REGULATION is the maru pair', () => {
    // The control for the test above: same match without encho keeps ○○, so
    // the single ○ there is attributable to the passthrough and not to the
    // helper always returning one.
    global.window.defaultWinMaru = defaultWinMaru;
    const match = {
      sideA: { id: 'p1', name: 'Aka' }, sideB: { id: 'p2', name: 'Shiro' },
      ipponsA: [], ipponsB: [], winner: { id: 'p1', name: 'Aka' },
      decision: 'fusensho', encho: null,
    };
    const tree = runtime.mount(IndividualScore, { match });
    expect(collectText(findInTree(tree, n =>
      typeof n?.props?.className === 'string' && n.props.className.includes('msb-slots--aka')))).toBe('\u25cb\u25cb');
    delete global.window.defaultWinMaru;
  });

  it('does not fabricate an Ht on an UNTIED drifted scoreline', () => {
    // validation.go and boutWinnerSide both gate hantei on equal ippon counts;
    // the mark mirrors that. A drifted decidedByHantei on a 2-1 row renders
    // its letters plainly instead of displaying a judges'-decision mark on a
    // bout the ippons show was decided on points.
    const sub = {
      position: 1, sideA: 'Aka P', sideB: 'Shiro P',
      ipponsA: ['M', 'K'], ipponsB: ['D'], decidedByHantei: true, winner: 'Aka P',
    };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(collectText(tree)).not.toContain('Ht');
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeNull();
  });

  it('judges tied on RAW ippons, not the capped display pair (3-2 is untied)', () => {
    // ipponLetters caps each side at the 2 sanbon slots, so a drifted 3-2 row
    // would read 2-2 through the display pair and pass the tie gate. The gate
    // counts the raw arrays instead.
    const sub = {
      position: 1, sideA: 'Aka P', sideB: 'Shiro P',
      ipponsA: ['M', 'K', 'D'], ipponsB: ['M', 'K'], decidedByHantei: true, winner: 'Aka P',
    };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(collectText(tree)).not.toContain('Ht');
  });

  it('marks AKA (never both, never neither) when the winner matches both sides', () => {
    // A winner string-matching both match-level sides is INVALID data (team
    // names are unique by rule; only drifted or hand-edited files produce
    // it). Defensive resolution: marking BOTH asserted two winners (the
    // original bug); marking NEITHER hid the verdict AND contradicted the
    // Go-computed standings/export, which resolve this case to side A by
    // check order. One side - the same side Go picks - wears it.
    const sub = {
      position: -1, ipponsA: ['M'], ipponsB: ['M'],
      decidedByHantei: true, winner: 'Seibukan',
    };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 3, isDH: true,
      matchSideA: 'Seibukan', matchSideB: 'Seibukan',
    });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
    expect(centreText(tree)).not.toContain('Ht');
  });

  it('IndividualScore: with both slots full the Ht rides beside them, never the centre', () => {
    // resultSlot reports `loose` when there is no free slot. 2-2 is IMPOSSIBLE
    // under the rules and unreachable through any UI (ippon entry stops at 2);
    // only hand-corrupted data reaches this branch, whose sole job is to never
    // overwrite a recorded point and never write the shared centre.
    const match = {
      sideA: { id: 'p1', name: 'Aka' }, sideB: { id: 'p2', name: 'Shiro' },
      ipponsA: ['M', 'K'], ipponsB: ['M', 'K'], decidedByHantei: true,
      winner: { id: 'p1', name: 'Aka' },
    };
    const tree = runtime.mount(IndividualScore, { match });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-ht-a')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-ht-b')).toBeNull();
    // Both struck points survive: the mark did not overwrite either slot.
    const text = collectText(tree);
    expect(text).toContain('M');
    expect(text).toContain('K');
    expect(centreText(tree)).not.toContain('Ht');
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
  });

  it('IndividualScore: ids resolve a same-name head-to-head correctly (winner side gets Ht)', () => {
    // With participant ids, the same-name pair is disambiguated and the
    // winning side's slot gets the Ht mark.
    const match = {
      sideA: { id: 'p1', name: 'Same Name' }, sideB: { id: 'p2', name: 'Same Name' },
      ipponsA: [], ipponsB: [], decidedByHantei: true,
      winner: { id: 'p1', name: 'Same Name' },
    };
    const tree = runtime.mount(IndividualScore, { match });
    // p1 = sideA = aka → win mark on aka, none on shiro, plain centre.
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
    expect(centreText(tree)).toBe('vs');
  });

  it('IndividualScore renders the ippon-letter slots (§263)', () => {
    const tree = runtime.mount(IndividualScore, { match: { ipponsB: ['M'], ipponsA: ['K', 'M'] } });
    const text = collectText(tree);
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'individual-score')).toBeTruthy();
    expect(text).toContain('M'); expect(text).toContain('K');
  });

  // Engi (flag-count scoring) is the ONLY competition type where the centre
  // marks are a NUMBER; every other type renders ippon letters (tested above).
  // flagsA=sideA=Aka, flagsB=sideB=Shiro (same convention as engiFlagScore).
  it('IndividualScore renders the engi flag COUNT per side, not ippon letters', () => {
    const tree = runtime.mount(IndividualScore, { match: { flagsA: 3, flagsB: 2, ipponsA: [], ipponsB: [] } });
    const text = collectText(tree);
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-flags-b')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-flags-a')).toBeTruthy();
    expect(text).toContain('3'); expect(text).toContain('2');
  });

  it('IndividualScore marks the Aka slot as winner when flagsA > flagsB', () => {
    const tree = runtime.mount(IndividualScore, { match: { flagsA: 4, flagsB: 1, ipponsA: [], ipponsB: [] } });
    const akaSlots = findInTree(tree, n =>
      typeof n?.props?.className === 'string' && n.props.className.includes('msb-slots--aka'));
    expect(akaSlots?.props?.className).toContain('msb-slots--win');
  });

  it('IndividualScore marks the Shiro slot as winner when flagsB > flagsA', () => {
    const tree = runtime.mount(IndividualScore, { match: { flagsA: 0, flagsB: 5, ipponsA: [], ipponsB: [] } });
    const shiroSlots = findInTree(tree, n =>
      typeof n?.props?.className === 'string' && /\bmsb-slots\b/.test(n.props.className) && !n.props.className.includes('--aka'));
    expect(shiroSlots?.props?.className).toContain('msb-slots--win');
  });

  it('IndividualScore prepends the assigned competitor number (numberPrefix) when showNames is set', () => {
    // mp-13y: when a competition has a numberPrefix configured, the assigned
    // number (e.g. "K1") is set on match.sideA.number / match.sideB.number by
    // AssignPlayerNumbers and surfaced via normalizeMatch. The TV pool/round
    // feed renders names with showNames=true, so each name reads "K1 Tanaka".
    const match = {
      sideA: { name: 'Suzuki', number: 'K2' },
      sideB: { name: 'Tanaka', number: 'K1' },
      ipponsA: [], ipponsB: [],
    };
    const tree = runtime.mount(IndividualScore, { match, showNames: true });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'indiv-shiro-name');
    const aka = findInTree(tree, n => n?.props?.['data-testid'] === 'indiv-aka-name');
    expect(collectText(shiro)).toBe('K1 Tanaka');
    expect(collectText(aka)).toBe('K2 Suzuki');
  });

  it('IndividualScore degrades to the bare name when no number is set (non-numbered competition)', () => {
    const match = {
      sideA: { name: 'Suzuki' },  // no .number field
      sideB: { name: 'Tanaka' },
      ipponsA: [], ipponsB: [],
    };
    const tree = runtime.mount(IndividualScore, { match, showNames: true });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'indiv-shiro-name');
    const aka = findInTree(tree, n => n?.props?.['data-testid'] === 'indiv-aka-name');
    expect(collectText(shiro)).toBe('Tanaka');
    expect(collectText(aka)).toBe('Suzuki');
  });

  it('TeamScoreboard renders the IV/PW summary + one row per LINEUP POSITION (padding unplayed bouts)', () => {
    // mp-1oy3: a running encounter with 2 of 5 scored must still show all 5
    // position rows (the 3 still-to-come bouts pad after the scored ones), not
    // just the 2 scored rows.
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: 2, ipponsB: [], ipponsA: ['D'] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: false });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'team-summary')).toBeTruthy();
    const text = collectText(tree);
    expect(text).toContain('IV'); expect(text).toContain('PW');
    expect(boutRows(tree).length).toBe(5); // 2 scored + 3 padded positions
    expect(boutRows(tree).some(r => r.isDH)).toBe(false); // no rep bout when showDH:false
  });

  it('TeamScoreboard highlights the first unplayed bout as "now" across the padded positions when RUNNING (mp-1oy3)', () => {
    // 2 scored + 3 padded on a RUNNING match → done, done, now (first to-come), queued, queued.
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: 2, ipponsB: [], ipponsA: ['D'] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: false, isRunning: true });
    expect(boutRows(tree).map(r => r.state)).toEqual(['done', 'done', 'now', 'queued', 'queued']);
  });

  it('TeamScoreboard does NOT light a padded row "now" on a COMPLETED match with fewer subResults than teamSize (quick-score)', () => {
    // A quick-score can synthesise fewer subResults than teamSize; with the match
    // NOT running, the padded positions must stay "queued", never "now".
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: 2, ipponsB: [], ipponsA: ['D'] },
      { position: 3, ipponsB: ['K'], ipponsA: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: false /* not running */ });
    const states = boutRows(tree).map(r => r.state);
    expect(states).toEqual(['done', 'done', 'done', 'queued', 'queued']);
    expect(states).not.toContain('now');
  });

  it('TeamScoreboard leaves an up-next board (no scored bouts) all queued: no "now"', () => {
    const tree = runtime.mount(TeamScoreboard, { subResults: [], lineupA: null, lineupB: null, teamSize: 5, showDH: false });
    const states = boutRows(tree).map(r => r.state);
    expect(states.length).toBe(5);
    expect(states.every(s => s === 'queued')).toBe(true);
  });

  it('TeamScoreboard highlights bout 1 as "now" for a RUNNING 0-0 encounter (isRunning)', () => {
    // A running team match with no bouts scored yet must still mark the first
    // bout live, so it doesn't look identical to an up-next board.
    const tree = runtime.mount(TeamScoreboard, { subResults: [], lineupA: null, lineupB: null, teamSize: 5, showDH: false, isRunning: true });
    expect(boutRows(tree).map(r => r.state)).toEqual(['now', 'queued', 'queued', 'queued', 'queued']);
  });

  it('TeamScoreboard renders the rep-bout row (no redundant DAIHYOSEN text banner) when showDH AND the match is tied', () => {
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: ['M'] },   // 1-1 → IV 0-0, PW 1-1 → tied
      { position: -1, ipponsB: ['M'], ipponsA: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: true });
    // The rep bout carries its own (DH) centre mark, so the old text banner
    // was removed as redundant.
    expect(boutRows(tree).some(r => r.isDH)).toBe(true);
    expect(collectText(tree)).not.toContain('DAIHYOSEN'); // the redundant text banner stays gone
  });

  it('TeamScoreboard does NOT render the Daihyosen when the regular bouts are not tied (mp-13y #12)', () => {
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: ['M'] },   // draw
      { position: 2, ipponsB: ['M'], ipponsA: [] },        // shiro wins → IV 1-0 (NOT tied)
      { position: -1, sideA: 'Aka T', sideB: 'Shiro T', winner: 'Aka T' }, // stale/invalid DH
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: true });
    expect(boutRows(tree).some(r => r.isDH)).toBe(false);
    expect(collectText(tree)).not.toContain('DAIHYOSEN');
  });

  it('TeamScoreboard renders teamSize numbered rows when there are no bouts yet (mp-13y #4/#6)', () => {
    const tree = runtime.mount(TeamScoreboard, { subResults: [], lineupA: null, lineupB: null, teamSize: 3, showDH: false });
    expect(boutRows(tree).length).toBe(3);
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'team-summary')).toBeTruthy();
  });

  it('TeamScoreboard shows team names in the summary row when provided (mp-13y #2)', () => {
    const subResults = [{ position: 1, ipponsB: ['M'], ipponsA: [] }];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: false, shiroName: 'White Team', akaName: 'Red Team' });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'summary-shiro-name')).toBeTruthy();
    const text = collectText(tree);
    expect(text).toContain('White Team'); expect(text).toContain('Red Team');
  });

  it('BoutSubRow puts the hantei "Ht" mark on the winning side, not the centre (mp-13y #3/#7)', () => {
    const sub = { position: -1, sideA: 'Aka T', sideB: 'Shiro T', winner: 'Aka T', ipponsA: [], ipponsB: [], decidedByHantei: true };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 2, isDH: true });
    // "Ht" sits in the AKA (winner) slot; the centre cell is empty.
    const winA = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a');
    expect(winA).toBeTruthy();
    expect(collectText(winA)).toContain('Ht');
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b')).toBeNull();
    expect(centreText(tree)).not.toContain('Ht');
  });

  it('BoutSubRow marks a non-hantei ippon-less win (fusensho/kiken) with ○ on the winner', () => {
    const sub = { position: 1, sideA: 'Aka T', sideB: 'Shiro T', winner: 'Shiro T', ipponsA: [], ipponsB: [], decision: 'fusensho' };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 2 });
    const winB = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-b');
    expect(winB).toBeTruthy();
    // The testid rides the first slot cell only; count the maru across the
    // whole row — the loser side carries none, so the row total IS the
    // winner's fill: one ball per awarded point of the default win.
    const maru = (collectText(tree).match(/○/g) || []).length;
    expect(maru).toBe(2);
  });

  it('BoutSubRow matches a winner stored as the TEAM name via sub.teamA/teamB (daihyosen)', () => {
    // Copilot review batch 5: the backend can persist the daihyosen winner as
    // the TEAM name, not the rep competitor name. centreMarks must accept
    // sub.teamB / sub.teamA as fallback winner keys so the ○ / Ht mark still
    // lands on the winning side instead of falling back to centre Ht.
    const sub = {
      position: -1,
      sideA: 'Aka Rep', sideB: 'Shiro Rep',
      teamA: 'Red Team', teamB: 'White Team',
      winner: 'Red Team', // ← stored as team name, not rep name
      ipponsA: [], ipponsB: [], decidedByHantei: true,
    };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 2, isDH: true });
    const winA = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-win-a');
    expect(winA).toBeTruthy();
    expect(collectText(winA)).toContain('Ht');
    expect(centreText(tree)).not.toContain('Ht');
  });

  it('TeamScoreboard threads shiroName/akaName into the Daihyosen sub as teamB/teamA', () => {
    // Copilot review batch 5: TeamScoreboard wraps the DH sub with the parent
    // team names so a backend-persisted team-name winner key still resolves.
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: ['M'] },           // 1-1 → tied
      { position: -1, winner: 'White Team', decidedByHantei: true, ipponsA: [], ipponsB: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, {
      subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: true,
      shiroName: 'White Team', akaName: 'Red Team',
    });
    // Find the DH bout row: its sub must carry teamB/teamA.
    const dhRow = boutRows(tree).find(r => r.isDH);
    expect(dhRow).toBeTruthy();
    expect(dhRow.sub.teamB).toBe('White Team');
    expect(dhRow.sub.teamA).toBe('Red Team');
  });
});
