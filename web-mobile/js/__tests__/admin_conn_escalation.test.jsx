// Tests for watchSustainedDisconnect : the timer that escalates the admin
// connection indicator from the calm "Reconnecting…" pill to the full-width
// alert banner only after the SSE feed has been down past a threshold (so
// routine 5s reconnect blips never trip the banner). The component effect that
// consumes this can't be exercised by the React-stub test harness (useEffect
// is a no-op, useState setters are no-ops), so the timing contract is tested
// here on the extracted helper directly.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { watchSustainedDisconnect } from '../admin_shell.jsx';

describe('watchSustainedDisconnect', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  const THRESHOLD = 8000;

  it('fires onSustained(true) only after the feed stays down for the threshold', () => {
    const onSustained = vi.fn();
    const m = watchSustainedDisconnect(THRESHOLD, onSustained);

    m.note(false); // disconnected
    expect(onSustained).not.toHaveBeenCalled(); // transient : no escalation yet

    vi.advanceTimersByTime(THRESHOLD - 1);
    expect(onSustained).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1);
    expect(onSustained).toHaveBeenCalledWith(true);
    expect(onSustained).toHaveBeenCalledTimes(1);
    m.stop();
  });

  it('a recovery before the threshold cancels the escalation (no cry-wolf on a blip)', () => {
    const onSustained = vi.fn();
    const m = watchSustainedDisconnect(THRESHOLD, onSustained);

    m.note(false);
    vi.advanceTimersByTime(THRESHOLD - 1000);
    m.note(true); // reconnected before the threshold
    expect(onSustained).toHaveBeenLastCalledWith(false);

    // The pending timer must have been cleared : advancing past it does nothing.
    vi.advanceTimersByTime(5000);
    expect(onSustained).not.toHaveBeenCalledWith(true);
    m.stop();
  });

  it('repeated error events do NOT restart the timer (threshold measures total downtime)', () => {
    const onSustained = vi.fn();
    const m = watchSustainedDisconnect(THRESHOLD, onSustained);

    m.note(false);
    vi.advanceTimersByTime(THRESHOLD - 2000);
    m.note(false); // another error mid-outage : must not reset the clock
    vi.advanceTimersByTime(2000); // total downtime now == THRESHOLD
    expect(onSustained).toHaveBeenCalledWith(true);
    m.stop();
  });

  it('stop() clears a pending timer so it never fires', () => {
    const onSustained = vi.fn();
    const m = watchSustainedDisconnect(THRESHOLD, onSustained);

    m.note(false);
    m.stop();
    vi.advanceTimersByTime(THRESHOLD * 2);
    expect(onSustained).not.toHaveBeenCalledWith(true);
  });

  it('escalates again after recover → drop (timer re-arms on the next disconnect)', () => {
    const onSustained = vi.fn();
    const m = watchSustainedDisconnect(THRESHOLD, onSustained);

    m.note(false);
    vi.advanceTimersByTime(THRESHOLD);
    expect(onSustained).toHaveBeenCalledWith(true);

    m.note(true); // recover
    onSustained.mockClear();

    m.note(false); // drop again
    vi.advanceTimersByTime(THRESHOLD);
    expect(onSustained).toHaveBeenCalledWith(true);
    m.stop();
  });
});
