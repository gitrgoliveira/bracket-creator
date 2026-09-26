// bc-sync: API.runDurably makes a running score write durable by putting it
// into the persisted outbox, and that only works if the write reaches
// API.recordScore INSIDE the runDurably callback, i.e. synchronously. The
// score editor hosts call editMatchScore (admin.jsx), which calls
// attemptScoreWrite first thing; this pins that attemptScoreWrite hands the
// write to recordScore in the same tick. An `await` added in front of that
// call would silently make every pagehide flush a plain fetch again.
import { describe, it, expect, vi } from 'vitest';
import { attemptScoreWrite } from '../write_result.jsx';

describe('attemptScoreWrite reaches recordScore in the same tick', () => {
  it('calls recordScore before returning control to its caller', () => {
    const recordScore = vi.fn(() => new Promise(() => {}));
    attemptScoreWrite({
      recordScore,
      confirmDialog: vi.fn(),
      compId: 'c1', matchId: 'm1', result: { status: 'running' }, password: 'pw', match: null,
    });
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledWith('c1', 'm1', { status: 'running' }, 'pw', null);
  });
});
