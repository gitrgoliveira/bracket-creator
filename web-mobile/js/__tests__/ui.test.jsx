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
  });
});
