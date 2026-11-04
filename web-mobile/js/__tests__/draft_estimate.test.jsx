import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { finishClock } from '../admin_schedule_utils.jsx';
import { DraftEstimatePanel, useDraftEstimate, idleShiaijoText } from '../draft_estimate.jsx';
import { makeReactive } from './helpers/reactive_react.js';

// Helper to extract plain text content from a JSX element tree.
function getNodeText(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(getNodeText).join('');
  const children = node.children || node.props?.children;
  return getNodeText(children);
}

// Helper to find an element with data-testid in a JSX element tree.
function findTestId(node, testId) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) {
    for (const item of node) {
      const hit = findTestId(item, testId);
      if (hit) return hit;
    }
    return null;
  }
  if (node.props?.['data-testid'] === testId) return node;
  const rawChildren = node.children || node.props?.children;
  const children = Array.isArray(rawChildren) ? rawChildren : (rawChildren ? [rawChildren] : []);
  for (const child of children) {
    const found = findTestId(child, testId);
    if (found) return found;
  }
  return null;
}

describe('finishClock', () => {
  it('handles plain finish time on same day', () => {
    const res = finishClock('09:30', 248);
    expect(res).toEqual({ clock: '13:38', nextDay: false });
  });

  it('handles finish at exactly midnight', () => {
    const res = finishClock('23:00', 60);
    expect(res).toEqual({ clock: '00:00', nextDay: true });
  });

  it('handles finish past midnight', () => {
    const res = finishClock('23:00', 100);
    expect(res).toEqual({ clock: '00:40', nextDay: true });
  });

  it('returns empty clock and nextDay false for bad inputs', () => {
    expect(finishClock('bad', 60)).toEqual({ clock: '', nextDay: false });
    expect(finishClock(null, 60)).toEqual({ clock: '', nextDay: false });
    expect(finishClock('', 60)).toEqual({ clock: '', nextDay: false });
    expect(finishClock('25:00', 60)).toEqual({ clock: '', nextDay: false });
    expect(finishClock('09:65', 60)).toEqual({ clock: '', nextDay: false });
    expect(finishClock('09:00', NaN)).toEqual({ clock: '', nextDay: false });
    expect(finishClock('09:00', Infinity)).toEqual({ clock: '', nextDay: false });
  });
});

describe('idleShiaijoText', () => {
  it('words the idle shiaijo count for various usable and chosen counts', () => {
    expect(idleShiaijoText(1, 2)).toBe('a second shiaijo');
    expect(idleShiaijoText(2, 3)).toBe('one shiaijo');
    expect(idleShiaijoText(1, 4)).toBe('3 shiaijo');
  });
});

describe('DraftEstimatePanel Part D copy mapping', () => {
  it('renders Panel headline: Est. duration: about 4h 08m', () => {
    const tree = DraftEstimatePanel({
      estimate: { totalDurationMinutes: 248 },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-duration');
    expect(getNodeText(el)).toBe('Est. duration: about 4h 08m');
  });

  it('renders Panel per shiaijo: Per shiaijo: A about 4h 08m · B about 4h 02m', () => {
    const tree = DraftEstimatePanel({
      estimate: { totalDurationMinutes: 248, perCourtMinutes: [248, 242] },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-per-shiaijo');
    expect(getNodeText(el)).toBe('Per shiaijo: A about 4h 08m · B about 4h 02m');
  });

  it('renders Panel per shiaijo, an idle one: Per shiaijo: A about 55m · B not used', () => {
    const tree = DraftEstimatePanel({
      estimate: { totalDurationMinutes: 55, perCourtMinutes: [55, 0] },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-per-shiaijo');
    expect(getNodeText(el)).toBe('Per shiaijo: A about 55m · B not used');
  });

  it('renders Panel timing with pauses', () => {
    const tree = DraftEstimatePanel({
      estimate: {
        totalDurationMinutes: 248,
        timing: {
          multiplier: 1.5,
          allowancePct: 10,
          breaks: [
            { name: 'Opening ceremony', start: '09:30', end: '10:00' },
            { name: 'Lunch', start: '12:30', end: '13:30' },
          ],
        },
      },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-timing');
    expect(getNodeText(el)).toContain(
      "Uses this tournament's timing: each match counts as 1.5 times its clock time, plus 10% for the slowest shiaijo. Pauses that day: Opening ceremony 09:30 to 10:00, Lunch 12:30 to 13:30."
    );
  });

  it('renders Panel, no pauses', () => {
    const tree = DraftEstimatePanel({
      estimate: {
        totalDurationMinutes: 248,
        timing: {
          multiplier: 1.5,
          allowancePct: 10,
          breaks: [],
        },
      },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-timing');
    expect(getNodeText(el)).toContain(
      "Uses this tournament's timing: each match counts as 1.5 times its clock time, plus 10% for the slowest shiaijo. No pauses that day."
    );
  });

  it('renders Panel, too many shiaijo warning', () => {
    const tree = DraftEstimatePanel({
      estimate: {
        totalDurationMinutes: 60,
        warnings: [{ code: 'too_many_shiaijo', participants: 3, usable: 1, chosen: 2 }],
      },
      shape: { startTime: '09:30', participants: 3 },
    });
    const el = findTestId(tree, 'draft-estimate-warning');
    expect(getNodeText(el)).toContain(
      'With 3 participants only 1 match can run at a time, so a second shiaijo would stand empty. Use 1 shiaijo.'
    );
  });

  it('renders Panel, past midnight warn sentence', () => {
    const tree = DraftEstimatePanel({
      estimate: { totalDurationMinutes: 100 },
      shape: { startTime: '23:00' },
    });
    const el = findTestId(tree, 'draft-estimate-finish');
    expect(getNodeText(el)).toBe(
      'Finishes about 00:40, after midnight. Start it earlier or give it more shiaijo.'
    );
  });

  it('renders no timing line when the estimate has no timing', () => {
    const tree = DraftEstimatePanel({
      estimate: { totalDurationMinutes: 248 },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-timing');
    expect(el).toBeNull();
  });

  it('renders sent multiplier and allowance when estimate has timing', () => {
    const tree = DraftEstimatePanel({
      estimate: {
        totalDurationMinutes: 248,
        timing: {
          multiplier: 1.8,
          allowancePct: 12,
          breaks: [],
        },
      },
      shape: { startTime: '09:30' },
    });
    const el = findTestId(tree, 'draft-estimate-timing');
    expect(getNodeText(el)).toContain(
      "Uses this tournament's timing: each match counts as 1.8 times its clock time, plus 12% for the slowest shiaijo. No pauses that day."
    );
  });

  it('renders kachinuki range line without (average)', () => {
    const tree = DraftEstimatePanel({
      estimate: {
        totalDurationMinutes: 120,
        bestCaseMinutes: 90,
        worstCaseMinutes: 150,
      },
      shape: { startTime: '09:00', teamMatchType: 'kachinuki' },
    });
    const el = findTestId(tree, 'draft-estimate-kachinuki');
    expect(getNodeText(el)).toBe('Could end 10:30 to 11:30');
    expect(getNodeText(el)).not.toContain('(average)');
  });
});

describe('useDraftEstimate hook behaviour with fake timers', () => {
  let runtime;
  let prevReact;
  let latestResult;

  function TestHook({ shape, password }) {
    latestResult = useDraftEstimate(shape, password);
    return null;
  }

  beforeEach(() => {
    vi.useFakeTimers();
    runtime = makeReactive();
    prevReact = global.React;
    global.React = runtime.React;
    latestResult = null;
  });

  afterEach(() => {
    if (runtime) {
      runtime.unmount();
    }
    global.React = prevReact;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('collapses rapid changes within 250 ms debounce into one request', async () => {
    const mockEstimate = vi.fn().mockResolvedValue({ totalDurationMinutes: 120 });
    window.API = { estimateDraft: mockEstimate };

    runtime.mount(TestHook, { shape: { participants: 10, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(100);

    runtime.updateProps({ shape: { participants: 20, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(100);

    runtime.updateProps({ shape: { participants: 30, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(250);

    expect(mockEstimate).toHaveBeenCalledTimes(1);
    expect(mockEstimate).toHaveBeenCalledWith(
      expect.objectContaining({ participants: 30 }),
      'pw',
      expect.any(Object)
    );
  });

  it('aborts the previous in-flight request when a new request is sent', async () => {
    const capturedSignals = [];
    const mockEstimate = vi.fn().mockImplementation((shape, pw, signal) => {
      capturedSignals.push(signal);
      return new Promise(() => {});
    });
    window.API = { estimateDraft: mockEstimate };

    runtime.mount(TestHook, { shape: { participants: 10, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(250);
    expect(capturedSignals.length).toBe(1);
    expect(capturedSignals[0].aborted).toBe(false);

    runtime.updateProps({ shape: { participants: 20, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(250);
    expect(capturedSignals.length).toBe(2);
    expect(capturedSignals[0].aborted).toBe(true);
    expect(capturedSignals[1].aborted).toBe(false);
  });

  it('sends NO request for an empty participants count or start time', async () => {
    const mockEstimate = vi.fn().mockResolvedValue({ totalDurationMinutes: 120 });
    window.API = { estimateDraft: mockEstimate };

    runtime.mount(TestHook, { shape: { participants: '', startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(500);
    expect(mockEstimate).not.toHaveBeenCalled();
    expect(latestResult).toEqual({ estimate: null, loading: false, error: null });

    runtime.updateProps({ shape: { participants: 0, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(500);
    expect(mockEstimate).not.toHaveBeenCalled();

    runtime.updateProps({ shape: { participants: 10, startTime: '' }, password: 'pw' });
    vi.advanceTimersByTime(500);
    expect(mockEstimate).not.toHaveBeenCalled();
  });

  it('keeps the server sentence on a failed request', async () => {
    const mockEstimate = vi.fn().mockRejectedValue(new Error('Format requires at least 4 participants'));
    window.API = { estimateDraft: mockEstimate };

    runtime.mount(TestHook, { shape: { participants: 2, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(250);
    await Promise.resolve();

    expect(latestResult.error).toBe('Format requires at least 4 participants');
    expect(latestResult.estimate).toBeNull();
    expect(latestResult.loading).toBe(false);
  });

  it('cancels the timer and aborts the request on unmount', async () => {
    const capturedSignals = [];
    const mockEstimate = vi.fn().mockImplementation((shape, pw, signal) => {
      capturedSignals.push(signal);
      return new Promise(() => {});
    });
    window.API = { estimateDraft: mockEstimate };

    runtime.mount(TestHook, { shape: { participants: 10, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(100);
    runtime.unmount();
    vi.advanceTimersByTime(500);
    expect(mockEstimate).not.toHaveBeenCalled();

    // Check that an in-flight request is aborted on unmount
    runtime.mount(TestHook, { shape: { participants: 10, startTime: '09:00' }, password: 'pw' });
    vi.advanceTimersByTime(250);
    expect(capturedSignals.length).toBe(1);
    expect(capturedSignals[0].aborted).toBe(false);

    runtime.unmount();
    expect(capturedSignals[0].aborted).toBe(true);
  });
});
