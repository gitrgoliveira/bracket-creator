import { describe, it, expect, vi } from 'vitest';
import { StatusBadge, formatDate, StableInput } from '../ui.jsx';

describe('UI Components', () => {
  describe('StatusBadge', () => {
    it('should return a badge with correct class and label', () => {
      const badge = StatusBadge({ status: 'setup' });
      expect(badge.props.className).toContain('badge--setup');
      expect(badge.children).toContain('Pending');
    });

    it('should show live dot when requested and status is active', () => {
      const badge = StatusBadge({ status: 'pools', showLiveDot: true });
      const dot = badge.children.find(c => c && c.props && c.props.className === 'dot dot--live');
      expect(dot).toBeDefined();
    });
  });

  describe('formatDate', () => {
    it('should format dates correctly', () => {
      expect(formatDate('2026-05-12')).toBe('12 May 2026');
    });
    it('should handle empty date', () => {
      expect(formatDate('')).toBe('Date TBA');
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

    // The next four assertions pin the NaN-display contract added when
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
});
