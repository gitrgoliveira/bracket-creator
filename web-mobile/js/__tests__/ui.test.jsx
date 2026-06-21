import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { StatusBadge, formatDate, StableInput, LoadingSpinner } from '../ui.jsx';
import { makeReactive } from './helpers/reactive_react.js';

describe('UI Components', () => {
  describe('StatusBadge', () => {
    it('should return a badge with correct class and label', () => {
      const badge = StatusBadge({ status: 'setup' });
      expect(badge.props.className).toContain('badge--setup');
      expect(badge.children).toContain('Pending');
    });

    it('should show running dot when requested and status is active', () => {
      const badge = StatusBadge({ status: 'pools', showRunningDot: true });
      const dot = badge.children.find(c => c && c.props && c.props.className === 'dot dot--running');
      expect(dot).toBeDefined();
    });

    it('should show "League" label for pools status when format is league', () => {
      const badge = StatusBadge({ status: 'pools', format: 'league' });
      expect(badge.children).toContain('League');
    });

    it('league status badge must NEVER render "Pools" (terminology boundary, mp-8rc9)', () => {
      const badge = StatusBadge({ status: 'pools', format: 'league' });
      // children = [runningDot?, label]; the label must be "League", not "Pools".
      expect(badge.children).not.toContain('Pools');
    });

    it('should still show "Pools" label for pools status when format is mixed', () => {
      const badge = StatusBadge({ status: 'pools', format: 'mixed' });
      expect(badge.children).toContain('Pools');
    });
  });

  describe('formatDate', () => {
    it('should format canonical DD-MM-YYYY dates correctly', () => {
      // DD-MM-YYYY is the canonical storage / API format end-to-end
      // (see admin_helpers.jsx normalizeDate). This is the only shape
      // production callers should pass formatDate.
      expect(formatDate('12-05-2026')).toBe('12 May 2026');
    });
    it('should also accept ISO YYYY-MM-DD as transition convenience', () => {
      // ISO is what HTML <input type="date"> produces natively; the
      // frontend converts at the input boundary via isoToDmy, but
      // formatDate is tolerant so any direct ISO consumer still works.
      expect(formatDate('2026-05-12')).toBe('12 May 2026');
    });
    it('should handle empty date', () => {
      expect(formatDate('')).toBe('Date TBA');
    });
    it('should fall back to "Date TBA" for unrecognized shapes', () => {
      expect(formatDate('not-a-date')).toBe('Date TBA');
    });
  });

  describe('StableInput', () => {
    it('should render an input with local state', () => {
      const onChange = vi.fn();
      const input = StableInput({ value: 'hello', onChange, type: 'text' });
      expect(input.type).toBe('input');
      expect(input.props.value).toBe('hello');
    });

    it('should update local state on change (logic check)', () => {
      // Since we can't easily test hook updates in this simple setup,
      // we at least verify it renders the right type.
      const input = StableInput({ value: 10, onChange: vi.fn(), type: 'number' });
      expect(input.props.type).toBe('number');
    });

    // The following assertions pin the NaN-display contract added when
    // we changed `+e.target.value` (collapses "" to 0) to NaN-on-clear.
    // Without these tests, a regression that drops the displayValue
    // mapping would silently reintroduce React's "Received NaN for the
    // value attribute" warning, OR collapse the cleared input back to
    // "0".
    it('renders NaN local state as empty string for type="number"', () => {
      const input = StableInput({ value: NaN, onChange: vi.fn(), type: 'number' });
      expect(input.props.value).toBe('');
    });

    it('renders Infinity local state as empty string for type="number"', () => {
      const input = StableInput({ value: Infinity, onChange: vi.fn(), type: 'number' });
      expect(input.props.value).toBe('');
    });

    it('renders finite number local state as-is for type="number"', () => {
      const input = StableInput({ value: 42, onChange: vi.fn(), type: 'number' });
      expect(input.props.value).toBe(42);
    });

    it('parses cleared "" to NaN on change for type="number"', () => {
      // Pin the "cleared input does not collapse to 0" contract.
      // Pre-fix: `+""` → 0 → onChange called with 0; the visible field
      // would suddenly show "0" after the user emptied it. Now: empty
      // string → NaN → parent guards via Number.isFinite / Number.isInteger.
      vi.useFakeTimers();
      try {
        const onChange = vi.fn();
        const input = StableInput({ value: 5, onChange, type: 'number' });
        input.props.onChange({ target: { value: '' } });
        // The 200ms debounce fires the onChange callback.
        vi.runAllTimers();
        expect(onChange).toHaveBeenCalledTimes(1);
        expect(Number.isNaN(onChange.mock.calls[0][0])).toBe(true);
      } finally {
        vi.useRealTimers();
      }
    });

    it('parses non-empty numeric strings normally for type="number"', () => {
      vi.useFakeTimers();
      try {
        const onChange = vi.fn();
        const input = StableInput({ value: 0, onChange, type: 'number' });
        input.props.onChange({ target: { value: '7' } });
        vi.runAllTimers();
        expect(onChange).toHaveBeenCalledWith(7);
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe('LoadingSpinner', () => {
    it('does not render immediately if delay > 0', () => {
      const spinner = LoadingSpinner({ text: 'Loading...', delay: 200 });
      expect(spinner).toBeNull();
    });

    it('renders immediately if delay is 0', () => {
      const spinner = LoadingSpinner({ text: 'Loading...', delay: 0 });
      expect(spinner).not.toBeNull();
      expect(spinner.type).toBe('div');
      expect(spinner.props.className).toBe('loading-page');
    });

    it('supports custom size parameters', () => {
      const spinner = LoadingSpinner({ text: 'Loading...', delay: 0, size: 48 });
      expect(spinner).not.toBeNull();
      const spinnerEl = spinner.children.find(c => c && c.props && c.props.className === 'loading-spinner');
      expect(spinnerEl).toBeDefined();
      expect(spinnerEl.props.style).toEqual({ width: '48px', height: '48px' });
    });

    describe('reactive behavior', () => {
      const realReact = global.React;
      let runtime;
      let LoadingSpinnerReactive;

      beforeEach(async () => {
        runtime = makeReactive();
        global.React = runtime.React;
        vi.resetModules();
        const mod = await import('../ui.jsx');
        LoadingSpinnerReactive = mod.LoadingSpinner;
      });

      afterEach(() => {
        runtime.unmount();
        global.React = realReact;
        vi.resetModules();
      });

      it('renders null initially and shows spinner after delay', () => {
        vi.useFakeTimers();
        try {
          runtime.mount(LoadingSpinnerReactive, { text: 'Delayed...', delay: 200 });
          // Initially it should be null because of the delay
          expect(runtime.currentTree()).toBeNull();

          // Advance timers by 199ms - should still be null
          vi.advanceTimersByTime(199);
          expect(runtime.currentTree()).toBeNull();

          // Advance timers past the 200ms mark
          vi.advanceTimersByTime(1);
          const current = runtime.currentTree();
          expect(current).not.toBeNull();
          expect(current.type).toBe('div');
          expect(current.props.className).toBe('loading-page');
        } finally {
          vi.useRealTimers();
        }
      });

      it('updates visibility immediately if delay is updated to 0', () => {
        vi.useFakeTimers();
        try {
          runtime.mount(LoadingSpinnerReactive, { text: 'Delayed...', delay: 200 });
          expect(runtime.currentTree()).toBeNull();

          // Update delay prop to 0
          runtime.updateProps({ text: 'Delayed...', delay: 0 });
          const current = runtime.currentTree();
          expect(current).not.toBeNull();
          expect(current.type).toBe('div');
          expect(current.props.className).toBe('loading-page');
        } finally {
          vi.useRealTimers();
        }
      });
    });
  });
});
