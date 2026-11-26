// bc-drev K4: the marks of the Swap competitors mode, on Pools (preview) and
// Bracket (preview). Once a first competitor is picked, the other picks are
// marked by the server's verdict for swapping the two: better (green) and
// worse (amber) carry their word too, same keeps the plain dashed edge. The
// marks clear on Cancel, on the second pick (which opens the check) and on
// Done; a newer pick ignores an older read's late answer; a draw that moved
// under the page goes the check's draw-changed way; any other failure shows
// no marks and one quiet line.
import React from 'react';
import { render, act, screen, cleanup, renderHook } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap } from '../helpers/tap_events.js';

let restore;
let AdminPools;
let AdminBracket;
let useSwapCandidates;
let now;

beforeAll(async () => {
  restore = installWindowStubs({
    ScoreEditorModal: () => null,
    CourtPicker: () => null,
    API: { swapDrawPlaces: vi.fn(), fetchSwapCandidates: vi.fn() },
  });
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  }
  await import('../../bracket.jsx');
  const { PoolsViewer } = await import('../../viewer_standings.jsx');
  window.PoolsViewer = PoolsViewer;
  await import('../../admin_pools.jsx');
  await import('../../admin_competition_bracket.jsx');
  ({ useSwapCandidates } = await import('../../admin_swap_candidates.jsx'));
  AdminPools = window.AdminPools;
  AdminBracket = window.AdminBracket;
});
afterAll(() => restore());

beforeEach(() => {
  now = 1_000_000;
  vi.spyOn(Date, 'now').mockImplementation(() => now);
  Element.prototype.scrollIntoView = function scrollIntoView() {};
  window.API.swapDrawPlaces = vi.fn(() => new Promise(() => {}));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const later = () => { now += 1000; };

const elijah = { id: 'p1', name: 'Elijah Everdeen', dojo: 'Kenshi', number: 'IMS2' };
const arthur = { id: 'p2', name: 'Arthur Dick', dojo: 'Mumei', number: 'IMS3' };
const lena = { id: 'p3', name: 'Lena Park', dojo: 'Kenshi', number: 'IMS4' };
const omar = { id: 'p4', name: 'Omar Sy', dojo: 'Mumei', number: 'IMS5' };

const pools = [{ poolName: 'Pool A', players: [elijah, arthur] }, { poolName: 'Pool B', players: [lena, omar] }];
const poolMatch = (id, a, b) => ({ id, sideA: { id: a.id, name: a.name }, sideB: { id: b.id, name: b.name }, status: 'scheduled', court: 'A' });
const poolMatches = [poolMatch('Pool A-0', elijah, arthur), poolMatch('Pool B-0', lena, omar)];
const mixed = { id: 'c1', name: 'Cup', format: 'mixed', kind: 'individual', status: 'draw-ready', numberPrefix: 'IMS' };

const side = (p) => ({ id: p.id, name: p.name, dojo: p.dojo, number: p.number });
const knockoutBracket = {
  rounds: [
    [
      { id: 'm-r0-0', matchNumber: 1, court: 'A', status: 'scheduled', sideA: side(elijah), sideB: side(arthur) },
      { id: 'm-r0-1', matchNumber: 2, court: 'A', status: 'scheduled', sideA: side(lena), sideB: side(omar) },
    ],
    [{ id: 'm-r1-0', matchNumber: 3, court: 'A', status: 'scheduled', sideA: { id: '', name: 'Winner of r0-m0' }, sideB: { id: '', name: 'Winner of r0-m1' } }],
  ],
};

const verdicts = (pickId) => ({
  pick: { id: pickId },
  candidates: [{ id: 'p2', verdict: 'better' }, { id: 'p3', verdict: 'worse' }, { id: 'p4', verdict: 'same' }],
});

const tabs = {
  'Pools (preview)': async () => {
    await act(async () => {
      render(<AdminPools c={mixed} pools={pools} poolMatches={poolMatches} standings={{}} tweaks={{ showDojo: true }} onEditScore={vi.fn()} password="pw" />);
    });
  },
  'Bracket (preview)': async () => {
    await act(async () => {
      render(<AdminBracket c={{ ...mixed, format: 'knockout' }} t={{ courts: ['A'] }} bracket={knockoutBracket} onMoveCourt={vi.fn()}
        onEditScore={vi.fn()} tweaks={{ showDojo: true, cardVariant: 1 }} password="pw" />);
    });
  },
};

const target = (name) => screen.queryAllByTestId('swap-target').find((el) => el.textContent.includes(name));
const shownWord = (el) => {
  const w = el.querySelector('.swap-verdict__word.is-shown');
  return w ? w.textContent.replace(/^, /, '') : null;
};
const markOf = (el) => (el.className.match(/swap-pick--(better|worse)/) || [])[1] || null;
const tap = async (el) => { await pointerTap(el); later(); };
const keyLines = () => [...screen.getByTestId('swap-marks-key').children];
const shownKey = () => {
  const shown = keyLines().filter((l) => l.classList.contains('is-shown'));
  return shown.length ? shown.map((l) => l.textContent).join('|') : null;
};

for (const [tab, mount] of Object.entries(tabs)) {
  describe(`the marks on ${tab}`, () => {
    it('appear once the first pick is read, each better or worse with its word, same left plain', async () => {
      let answer;
      window.API.fetchSwapCandidates = vi.fn(() => new Promise((res) => { answer = res; }));
      await mount();
      await tap(screen.getByTestId('swap-mode-on'));
      // The words' space is held from the moment the mode is on.
      expect(target('Arthur Dick').querySelectorAll('.swap-verdict__word')).toHaveLength(2);
      await tap(target('Elijah Everdeen'));
      expect(window.API.fetchSwapCandidates).toHaveBeenCalledWith('c1', 'p1', 'pw');
      // The key line is there from the pick on, every sentence it can say
      // drawn and none shown, so the read answering moves nothing below it.
      expect(keyLines().map((l) => l.dataset.key)).toEqual(['both', 'better', 'worse', 'same', 'failed']);
      expect(shownKey()).toBeNull();
      const lineBefore = screen.getByTestId('swap-mode-line').textContent;
      // While the read is out, every pick keeps its plain edge.
      expect(['Arthur Dick', 'Lena Park', 'Omar Sy'].map((n) => markOf(target(n)))).toEqual([null, null, null]);
      await act(async () => { answer(verdicts('p1')); });
      expect(markOf(target('Arthur Dick'))).toBe('better');
      expect(shownWord(target('Arthur Dick'))).toBe('better');
      expect(markOf(target('Lena Park'))).toBe('worse');
      expect(shownWord(target('Lena Park'))).toBe('worse');
      expect(markOf(target('Omar Sy'))).toBeNull();
      expect(shownWord(target('Omar Sy'))).toBeNull();
      expect(markOf(target('Elijah Everdeen')), 'the pick itself is not marked').toBeNull();
      expect(screen.getByTestId('swap-mode-line').textContent, 'the pick line does not change').toBe(lineBefore);
      expect(keyLines()).toHaveLength(5);
      expect(keyLines().filter((l) => l.classList.contains('is-shown'))).toHaveLength(1);
      expect(screen.getByTestId('swap-mode-line').textContent).toContain('with… tap another competitor.');
      expect(shownKey()).toBe('Green keeps dojo-mates apart longer, amber brings some closer.');
      expect(screen.getByTestId('swap-mode-cancel')).toBeInTheDocument();
    });

    it('clear on Cancel, on the second pick, and on Done', async () => {
      window.API.fetchSwapCandidates = vi.fn((_c, pickId) => Promise.resolve(verdicts(pickId)));
      await mount();
      await tap(screen.getByTestId('swap-mode-on'));
      await tap(target('Elijah Everdeen'));
      expect(markOf(target('Arthur Dick'))).toBe('better');
      await tap(screen.getByTestId('swap-mode-cancel'));
      expect(markOf(target('Arthur Dick'))).toBeNull();
      expect(shownWord(target('Lena Park'))).toBeNull();
      expect(screen.queryByTestId('swap-marks-key'), 'no pick, no key line').toBeNull();

      await tap(target('Elijah Everdeen'));
      expect(markOf(target('Lena Park'))).toBe('worse');
      await tap(target('Lena Park'));
      expect(screen.getByTestId('draw-swap-check')).toBeInTheDocument();
      expect(markOf(target('Arthur Dick')), 'the check is open for the second pick').toBeNull();

      await act(async () => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })); });
      expect(markOf(target('Arthur Dick')), 'back to the first pick: read again').toBe('better');
      await tap(screen.getByTestId('swap-mode-done'));
      expect(screen.queryAllByTestId('swap-target')).toHaveLength(0);
    });

    it('a draw that moved under the page drops the pick and says so', async () => {
      window.API.fetchSwapCandidates = vi.fn().mockRejectedValue(Object.assign(new Error('Elijah Everdeen is no longer in the draw.'), { code: 'not_in_draw' }));
      await mount();
      await tap(screen.getByTestId('swap-mode-on'));
      await tap(target('Elijah Everdeen'));
      expect(target('Elijah Everdeen')).toHaveAttribute('aria-pressed', 'false');
      expect(screen.getByTestId('swap-mode-line').textContent).toBe('Elijah Everdeen is no longer in the draw.');
    });

    it('any other failure shows no marks and one quiet line, and the swap still opens its check', async () => {
      window.API.fetchSwapCandidates = vi.fn().mockRejectedValue(Object.assign(new Error('Not sent'), { code: 'network' }));
      await mount();
      await tap(screen.getByTestId('swap-mode-on'));
      await tap(target('Elijah Everdeen'));
      expect(target('Elijah Everdeen')).toHaveAttribute('aria-pressed', 'true');
      expect(shownKey()).toBe('The swaps could not be marked.');
      expect(keyLines()).toHaveLength(5);
      expect(screen.getByTestId('swap-mode-line').textContent).toContain('with… tap another competitor.');
      expect(markOf(target('Arthur Dick'))).toBeNull();
      await tap(target('Lena Park'));
      expect(screen.getByTestId('draw-swap-check')).toBeInTheDocument();
    });
  });
}

describe('the read behind the marks', () => {
  it('keeps only the answer of the pick that is still the pick', async () => {
    const answers = {};
    window.API.fetchSwapCandidates = vi.fn((_c, pickId) => new Promise((res) => { answers[pickId] = res; }));
    const { result, rerender } = renderHook((props) => useSwapCandidates(props), {
      initialProps: { compId: 'c1', pickId: 'p1', password: 'pw', onDrawChanged: vi.fn() },
    });
    await act(async () => {});
    rerender({ compId: 'c1', pickId: 'p2', password: 'pw', onDrawChanged: vi.fn() });
    await act(async () => {});
    await act(async () => { answers.p1(verdicts('p1')); });
    expect(result.current.marks, 'the older read answered late').toBeNull();
    await act(async () => { answers.p2({ pick: { id: 'p2' }, candidates: [{ id: 'p3', verdict: 'worse' }] }); });
    expect([...result.current.marks.entries()]).toEqual([['p3', 'worse']]);
    rerender({ compId: 'c1', pickId: null, password: 'pw', onDrawChanged: vi.fn() });
    expect(result.current.marks, 'no pick, no marks').toBeNull();
  });

  it('reads again when the draw is read again under the same pick, showing no old marks meanwhile', async () => {
    const answers = [];
    window.API.fetchSwapCandidates = vi.fn(() => new Promise((res) => { answers.push(res); }));
    const before = [{ poolName: 'Pool A' }];
    const after = [{ poolName: 'Pool A' }];
    const props = (drawKey) => ({ compId: 'c1', pickId: 'p1', drawKey, password: 'pw', onDrawChanged: vi.fn() });
    const { result, rerender } = renderHook((p) => useSwapCandidates(p), { initialProps: props(before) });
    await act(async () => {});
    await act(async () => { answers[0](verdicts('p1')); });
    expect(result.current.marks.get('p2')).toBe('better');
    // Another device swapped: the draw is read again, the pick is kept.
    rerender(props(after));
    expect(result.current.marks, 'the old draw\'s marks are dropped at once').toBeNull();
    await act(async () => {});
    expect(window.API.fetchSwapCandidates).toHaveBeenCalledTimes(2);
    await act(async () => { answers[1]({ pick: { id: 'p1' }, candidates: [{ id: 'p2', verdict: 'worse' }] }); });
    expect(result.current.marks.get('p2')).toBe('worse');
    // The same draw again reads nothing new.
    rerender(props(after));
    await act(async () => {});
    expect(window.API.fetchSwapCandidates).toHaveBeenCalledTimes(2);
  });
});
