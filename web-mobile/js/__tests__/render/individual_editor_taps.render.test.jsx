// bc-evts stage 3 (S6): on a RUNNING individual match every scoring control
// is a tap event handed to the outbox (API.recordTap), and nothing else is
// written while the match runs: no PUT /score. The board is the server's last
// copy with this device's pending taps folded on top (applyPending). A match
// that is not running keeps local edits, sent by Save as before, and the
// editor follows the match across that line in both directions.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

let outbox;
const API = {
  fetchCompetitionDetails: vi.fn(),
  recordScore: vi.fn(),
  recordDaihyosen: vi.fn(),
  removeDaihyosen: vi.fn(),
  putMatchLineup: vi.fn(),
  recordDecision: vi.fn(),
};
const STUBBED_GLOBALS = {
  isHikiwake: (v) => v === 'hikiwake',
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API,
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});
afterAll(() => restoreGlobals());

beforeEach(() => {
  outbox = makeFakeOutbox();
  Object.assign(API, outbox.api);
  API.recordScore = vi.fn().mockResolvedValue({ applied: true });
});

const NOW = 1_800_000_000_000;
const bout = (overrides = {}) => ({
  id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], modifiedAt: NOW, scoreVersion: 3,
  ...overrides,
});
const el = (match, props = {}) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={props.onSubmit || vi.fn()} password="pw" variant="inline" {...props} />
);
const mount = async (match, props) => {
  let r;
  await act(async () => { r = render(el(match, props)); });
  return r;
};
const click = async (node) => { await act(async () => { fireEvent.click(node); }); };
const shiro = (c) => c.querySelector('.sb-side--shiro');
const aka = (c) => c.querySelector('.sb-side--aka');
const letterBtn = (side, l) => [...side.querySelectorAll('.ipt-btn')].find((b) => b.textContent.trim() === l);
const marks = (c) => [...c.querySelectorAll('[aria-label*=" slot "]')]
  .map((s) => s.getAttribute('aria-label')).filter((l) => /: remove /.test(l));
const sent = () => API.recordTap.mock.calls.map((c) => c[2]);

describe('each control on a running match emits its event', () => {
  it('an ippon button is ippon.add for its side, shown at once, and no score write', async () => {
    const r = await mount(bout());
    await click(letterBtn(shiro(r.container), 'M'));
    await click(letterBtn(aka(r.container), 'K'));
    expect(sent()).toEqual([
      { kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' },
      { kind: 'ippon.add', pos: 0, side: 'a', letter: 'K' },
    ]);
    expect(API.recordTap.mock.calls[0][0]).toBeUndefined();
    expect(API.recordTap.mock.calls[0][1]).toBe('m1');
    expect(API.recordTap.mock.calls[0][3]).toBe('pw');
    expect(marks(r.container), 'both pending taps are on the board').toHaveLength(2);
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('the cap is the board\'s: a third point on a side holding two sends nothing', async () => {
    const r = await mount(bout({ ipponsB: ['M'], markIds: { a: [], b: ['b:0:b:M:1'] } }));
    await click(letterBtn(shiro(r.container), 'K'));
    await click(letterBtn(shiro(r.container), 'D'));
    expect(sent()).toHaveLength(1);
  });

  it('tapping a scored mark is ippon.remove naming its id; a pending tap is removable by its own id', async () => {
    const r = await mount(bout({ ipponsA: ['M'], markIds: { a: ['b:0:a:M:1'], b: [] } }));
    const label = marks(r.container)[0];
    await click(r.container.querySelector(`[aria-label="${label}"]`));
    expect(sent()[0]).toEqual({ kind: 'ippon.remove', pos: 0, side: 'a', target: 'b:0:a:M:1' });
    expect(marks(r.container), 'the removal shows at once').toHaveLength(0);
    await click(letterBtn(aka(r.container), 'K'));
    const [k] = marks(r.container);
    await click(r.container.querySelector(`[aria-label="${k}"]`));
    expect(sent()[2]).toMatchObject({ kind: 'ippon.remove', side: 'a', target: API.recordTap.mock.results[1].value.id });
    expect(marks(r.container)).toHaveLength(0);
  });

  it('a mark with no id is not removable by a tap (nothing is sent)', async () => {
    const r = await mount(bout({ ipponsA: ['M'] }));
    const label = marks(r.container)[0];
    await click(r.container.querySelector(`[aria-label="${label}"]`));
    expect(sent()).toEqual([]);
    expect(marks(r.container)).toHaveLength(1);
  });

  it('the foul + and − are foul.add and foul.remove; a second foul lands the H on the board', async () => {
    const r = await mount(bout());
    const add = r.container.querySelector('[aria-label="Add a Shiro foul"]');
    await click(add);
    await click(r.container.querySelector('[aria-label="Remove a Shiro foul"]'));
    await click(add);
    await click(add);
    expect(sent()).toEqual([
      { kind: 'foul.add', pos: 0, side: 'b' },
      { kind: 'foul.remove', pos: 0, side: 'b' },
      { kind: 'foul.add', pos: 0, side: 'b' },
      { kind: 'foul.add', pos: 0, side: 'b' },
    ]);
    expect(marks(r.container), 'two fouls land an H for Aka').toEqual([expect.stringMatching(/^Aka slot \d: remove H$/)]);
  });

  it('the overtime stepper is encho.add and encho.remove, one per period', async () => {
    const r = await mount(bout());
    await click(r.container.querySelector('[data-testid="scoring-modal-encho-pill"]'));
    await click(r.container.querySelector('[data-testid="scoring-modal-encho-checkbox"]'));
    await click(r.container.querySelector('[aria-label="Increase overtime period count"]'));
    await click(r.container.querySelector('[aria-label="Decrease overtime period count"]'));
    expect(sent()).toEqual([
      { kind: 'encho.add', pos: 0 },
      { kind: 'encho.add', pos: 0 },
      { kind: 'encho.remove', pos: 0 },
    ]);
    expect(r.container.textContent).toContain('×1');
  });

  it('a representative pick is pool.rep for its side, shown at once', async () => {
    const r = await mount(bout({ repIsTeam: true, repRosterA: ['A1', 'A2'], repRosterB: ['B1', 'B2'] }));
    const sel = r.container.querySelector('[data-testid="rep-shiro-select"]');
    await act(async () => { fireEvent.change(sel, { target: { value: 'B2' } }); });
    expect(sent()).toEqual([{ kind: 'pool.rep', pos: 0, side: 'b', name: 'B2' }]);
    expect(r.container.querySelector('[data-testid="rep-shiro-select"]').value).toBe('B2');
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('the keyboard point keys are taps too', async () => {
    await mount(bout());
    await act(async () => { fireEvent.keyDown(window, { key: 'm' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'K', shiftKey: true }); });
    expect(sent()).toEqual([
      { kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' },
      { kind: 'ippon.add', pos: 0, side: 'a', letter: 'K' },
    ]);
  });
});

describe('a match that is not running keeps its edits local until Save', () => {
  it('a scheduled match: a point is shown and sends nothing', async () => {
    const r = await mount(bout({ status: 'scheduled' }));
    await click(letterBtn(shiro(r.container), 'M'));
    expect(sent()).toEqual([]);
    expect(marks(r.container)).toHaveLength(1);
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('a correction of a finished match: taps stay local and Save correction sends them', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    const r = await mount(bout({ status: 'completed', ipponsB: ['M'], winner: { id: 'p2', name: 'Tanaka' } }), { onSubmit });
    await click(letterBtn(shiro(r.container), 'K'));
    expect(sent()).toEqual([]);
    expect(marks(r.container)).toHaveLength(2);
  });
});

describe('the editor follows the match across the running line, both ways', () => {
  it('started elsewhere while open: the next point is a tap on the server\'s board', async () => {
    const onSubmit = vi.fn();
    const r = await mount(bout({ status: 'scheduled' }), { onSubmit });
    await act(async () => { r.rerender(el(bout({ status: 'running', ipponsA: ['K'], markIds: { a: ['x.1'], b: [] }, scoreVersion: 5 }), { onSubmit })); });
    expect(marks(r.container), 'the server\'s board').toHaveLength(1);
    await click(letterBtn(shiro(r.container), 'M'));
    expect(sent()).toEqual([{ kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' }]);
    expect(marks(r.container)).toHaveLength(2);
  });

  it('finished elsewhere while open: the board takes the finished copy and edits go local', async () => {
    const onSubmit = vi.fn();
    const r = await mount(bout(), { onSubmit });
    await click(letterBtn(shiro(r.container), 'M'));
    expect(sent()).toHaveLength(1);
    await act(async () => { outbox.ack(undefined, 'm1', null, {}); });
    await act(async () => {
      r.rerender(el(bout({ status: 'completed', ipponsB: ['M', 'K'], winner: { id: 'p2', name: 'Tanaka' }, scoreVersion: 9 }), { onSubmit }));
    });
    expect(marks(r.container), 'the finished scoreline').toHaveLength(2);
    expect(r.container.textContent).toContain('CORRECTION');
    const label = marks(r.container)[0];
    await click(r.container.querySelector(`[aria-label="${label}"]`));
    expect(sent(), 'a correction tap is local').toHaveLength(1);
    expect(marks(r.container)).toHaveLength(1);
  });
});
