// bc-ltap: on the individual editor, taking a mark off and tapping it back
// while the removal is still saving returned the board to the value the
// server still showed, so the editor read it as untouched, and the removal's
// echo then wiped the tap; the tap's own save named no change. The board now
// keeps every server copy stamped before the operator's last edit, the rule the
// team sheet's rows follow (bc-kclr).

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn(),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
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

const NOW = 1_800_000_000_000;
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(NOW); });
afterEach(() => { vi.useRealTimers(); });

// A running individual pool bout. No compId: the competition fetch stays out.
const bout = (overrides = {}) => ({
  id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], modifiedAt: NOW - 5000,
  ...overrides,
});

// A save the server took: the answer a landed write comes back with.
const LANDED = { id: 'm1', status: 'running' };

const el = (match, onSubmit) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" variant="inline" />
);

const marks = (c) => [...c.querySelectorAll('[aria-label*=" slot "]')]
  .map((s) => s.getAttribute('aria-label'))
  .filter((l) => /: remove /.test(l));
const menButton = (side) => [...side.querySelectorAll('.ipt-btn')].find((b) => b.textContent.trim() === 'M');
const tap = async (node) => { await act(async () => { fireEvent.click(node); }); };
const wait = async (ms) => { await act(async () => { vi.advanceTimersByTime(ms); }); };

describe('a tap made while the previous save is out is never wiped by that save\'s echo (bc-ltap)', () => {
  it('remove a mark, tap it back before the removal\'s echo: the mark stays and is saved', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    const stored = bout({ ipponsA: ['M'] });
    let r;
    await act(async () => { r = render(el(stored, onSubmit)); });
    const [label] = marks(r.container);
    expect(label, 'precondition: one M on the board').toMatch(/ slot \d: remove M$/);
    const side = r.container.querySelector(label.startsWith('Aka') ? '.sb-side--aka' : '.sb-side--shiro');

    await tap(r.container.querySelector(`[aria-label="${label}"]`));
    await wait(350); // the removal's autosave goes out
    const removal = onSubmit.mock.calls.at(-1)?.[0];
    expect(removal?.ipponsA, 'precondition: the removal was saved').toEqual([]);

    vi.setSystemTime(NOW + 400);
    await tap(menButton(side)); // the M again, while the removal is out
    expect(marks(r.container), 'the tap shows').toHaveLength(1);

    // The removal's echo: the server now holds no mark, stamped at the removal.
    await act(async () => { r.rerender(el(bout({ ipponsA: [], modifiedAt: NOW + 50 }), onSubmit)); });
    expect(marks(r.container), 'the echo of the earlier removal does not wipe the newer tap').toHaveLength(1);

    await wait(350); // the tap's own autosave
    const save = onSubmit.mock.calls.at(-1)?.[0];
    expect(save?.ipponsA, 'the tap is sent').toEqual(['M']);
    expect(save?.changed, 'and named as a change').toContain('points');
  });

  it('add a mark, take it off before the add\'s echo: the board stays empty', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    const aka = r.container.querySelector('.sb-side--aka');
    await tap(menButton(aka));
    await wait(350);
    const [label] = marks(r.container);
    vi.setSystemTime(NOW + 600);
    await tap(r.container.querySelector(`[aria-label="${label}"]`));
    expect(marks(r.container)).toHaveLength(0);

    const added = onSubmit.mock.calls.at(-1)[0];
    await act(async () => {
      r.rerender(el(bout({ ipponsA: added.ipponsA, ipponsB: added.ipponsB, modifiedAt: NOW + 50 }), onSubmit));
    });
    expect(marks(r.container), 'the echo of the earlier add does not bring the mark back').toHaveLength(0);
  });

  it('a change made elsewhere after the last tap is still followed', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await wait(350);
    const added = onSubmit.mock.calls.at(-1)[0];
    // The server copy carries this tap, then another device's K for the other
    // side, stamped after the tap.
    const later = bout({ ipponsA: added.ipponsA, ipponsB: added.ipponsB, modifiedAt: NOW + 50 });
    await act(async () => { r.rerender(el(later, onSubmit)); });
    const otherKey = (added.ipponsA || []).length ? 'ipponsB' : 'ipponsA';
    await act(async () => {
      r.rerender(el({ ...later, [otherKey]: ['K'], modifiedAt: NOW + 5000 }, onSubmit));
    });
    expect(marks(r.container).some((l) => l.endsWith('remove K')), 'the later change shows').toBe(true);
  });

  it('once the tap\'s own save has landed, a server copy stamped just before the tap is followed', async () => {
    // The write's stamp is worked out at send, less the edit's age, so it can
    // read a millisecond before the tap that made it. Once that save has
    // landed the board holds nothing unsent, and a change the server applied
    // under such a stamp (another device's overtime, made before the tap and
    // arriving after it) is the match as it stands.
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await wait(350); // the tap's save goes out and lands
    await act(async () => {});
    const added = onSubmit.mock.calls.at(-1)[0];
    const echo = bout({ ipponsA: added.ipponsA, ipponsB: added.ipponsB, modifiedAt: NOW - 1 });
    await act(async () => { r.rerender(el(echo, onSubmit)); });
    await act(async () => { r.rerender(el({ ...echo, encho: { periodCount: 1 } }, onSubmit)); });

    vi.setSystemTime(NOW + 1000);
    await tap(menButton(r.container.querySelector('.sb-side--shiro')));
    await wait(350);
    expect(onSubmit.mock.calls.at(-1)[0].encho, 'the board took the overtime, so the next save keeps it').toEqual({ periodCount: 1 });
  });

  it('a save built before the latest tap clears nothing when it lands', async () => {
    let landFirst;
    const first = new Promise((resolve) => { landFirst = resolve; });
    const onSubmit = vi.fn().mockReturnValueOnce(first).mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await wait(350); // the add's save is out and unanswered
    const added = onSubmit.mock.calls.at(-1)[0];
    expect(added.ipponsA, 'precondition: the add was saved').toEqual(['M']);

    vi.setSystemTime(NOW + 400);
    const [label] = marks(r.container);
    await tap(r.container.querySelector(`[aria-label="${label}"]`)); // a tap the save in flight was not built with
    expect(marks(r.container)).toHaveLength(0);
    await act(async () => { landFirst(LANDED); });

    await act(async () => {
      r.rerender(el(bout({ ipponsA: added.ipponsA, ipponsB: added.ipponsB, modifiedAt: NOW + 50 }), onSubmit));
    });
    expect(marks(r.container), 'the add landing does not release the board from the removal it never carried').toHaveLength(0);

    await wait(350); // the removal's own autosave
    expect(onSubmit.mock.calls.at(-1)[0].ipponsA, 'the removal is sent').toEqual([]);
  });

  // A REFUSED save is not here: it takes the server's copy instead
  // (individual_engi_refused_autosave_takes_server.render.test.jsx).
  it('a queued save keeps the board against an older copy', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await wait(350);
    const added = onSubmit.mock.calls.at(-1)[0];
    vi.setSystemTime(NOW + 400);
    const [label] = marks(r.container);
    await tap(r.container.querySelector(`[aria-label="${label}"]`));
    await wait(350);
    expect(onSubmit.mock.calls.length, 'precondition: both taps were saved').toBe(2);

    await act(async () => {
      r.rerender(el(bout({ ipponsA: added.ipponsA, ipponsB: added.ipponsB, modifiedAt: NOW + 50 }), onSubmit));
    });
    expect(marks(r.container), 'a write that did not land released nothing').toHaveLength(0);
  });

  it('a scheduled match is not autosaved, so a net-zero fiddle holds nothing against another device', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout({ status: 'scheduled' }), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    const [label] = marks(r.container);
    expect(label, 'precondition: the scheduled board takes the tap').toBeTruthy();
    await tap(r.container.querySelector(`[aria-label="${label}"]`));
    expect(marks(r.container)).toHaveLength(0);
    await wait(350);
    expect(onSubmit, 'precondition: nothing was autosaved').not.toHaveBeenCalled();

    // Another device's K, stamped before the taps.
    await act(async () => { r.rerender(el(bout({ status: 'scheduled', ipponsB: ['K'], modifiedAt: NOW - 1000 }), onSubmit)); });
    expect(marks(r.container).some((l) => l.endsWith('remove K')), 'the copy from the other device is followed').toBe(true);
  });
});

describe('a stamp left by a write the match\'s leaving running dropped is cleared (bc-ltap)', () => {
  it('sent back to the queue inside the debounce: a later scheduled copy stamped before the tap is adopted', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await tap(r.container.querySelector(`[aria-label="${marks(r.container)[0]}"]`)); // net zero
    expect(marks(r.container), 'precondition: the board is empty again').toHaveLength(0);
    // Another device sends the match back to the queue, same score, inside the
    // debounce: the autosave is dropped at fire time, so no landed save clears the stamp.
    await act(async () => { r.rerender(el(bout({ status: 'scheduled' }), onSubmit)); });
    await wait(350);
    expect(onSubmit, 'the autosave was dropped').not.toHaveBeenCalled();
    // A later-arriving copy, stamped before the tap (and before the match left running).
    await act(async () => { r.rerender(el(bout({ status: 'scheduled', ipponsB: ['M'], modifiedAt: NOW - 4000 }), onSubmit)); });
    expect(marks(r.container), 'the copy is adopted, not held against a stamp nothing can clear').toHaveLength(1);
  });
});
