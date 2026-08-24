import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock, CLOCK_SKEW_REASON_TEXT } from '../../write_result.jsx';

// bc-lww1 regression. The shiaijo console optimistically advances its LOCAL
// bracket when a knockout bout is scored to completion, so a court running
// offline sees the next match become runnable without waiting for the server.
//
// That is right for a QUEUED write (it lands on reconnect) and wrong for a
// SUPERSEDED one: the server stored nothing because a different device's newer
// result won, so advancing paints the operator's own DISCARDED winner onto the
// queue — on the very surface the "Not saved: check the recorded result" banner
// is telling them to go and look at. Nothing reconciles it either: a superseded
// write emits no SSE broadcast and deliberately does not fire the bracket-resync
// channel, so the phantom advance survives until an unrelated refetch.
//
// The optimistic advance used to run BEFORE the not-landed check, so it applied
// to both. These tests pin the split. They observe the real symptom rather than
// spying on an internal: AdminShiaijoPage derives its whole match list from
// courtComps via window.tournamentMatches({competitions}), so the competitions
// handed to that function on the next render ARE the local bracket.

const capturedSubmit = { onSubmit: null };
const tournamentMatchesSpy = vi.fn();

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  // Capture the editor's submit handler so the test can drive the exact code
  // path the operator's "Finish" tap takes.
  ScoreEditorModal: (props) => {
    capturedSubmit.onSubmit = props.onSubmit;
    return <div data-testid="score-editor" />;
  },
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
  },
  startPatch: vi.fn(() => ({ status: 'running' })),
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
  // The REAL predicates: the point of the test is that the call site consults
  // them correctly, so stubbing them would test nothing.
  writeDidNotLand,
  writeWasSuperseded,
  writeWasRefusedForClock,
  CLOCK_SKEW_REASON_TEXT,
};

const originals = {};
let AdminShiaijoPage;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

// A two-round knockout: the running semifinal r1-m0 feeds the final r2-m0,
// whose sideA is still a "Winner of" placeholder. Advancing r1-m0 writes the
// winner into that slot and promotes the final to scheduled, which is exactly
// the state that must NOT appear after a superseded write.
//
// The final's sideB is a RESOLVED competitor (the other semifinal is already
// decided) on purpose: propagateBracketWinnerLocal only promotes a match to
// scheduled once BOTH sides are real, so leaving it a "Winner of" placeholder
// would make the final stay pending for a reason unrelated to what these tests
// are about, and the no-advance assertion would pass trivially.
function makeBracketTournament() {
  return {
    name: 'Test Tournament',
    courts: ['A'],
    competitions: [{
      id: 'c1',
      name: 'Cup',
      bracket: {
        rounds: [
          [{
            id: 'r1-m0', status: 'running',
            sideA: { id: 'p1', name: 'Yamada' },
            sideB: { id: 'p2', name: 'Tanaka' },
          }],
          [{
            id: 'r2-m0', status: 'pending',
            sideA: { id: '', name: 'Winner of r1-m0' },
            sideB: { id: 'p3', name: 'Sato' },
          }],
        ],
      },
    }],
  };
}

const runningSemi = {
  id: 'r1-m0', compId: 'c1', compName: 'Cup', status: 'running',
  phase: 'bracket', matchNumber: 1, court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
};

// The completing patch the score editor emits when the operator taps Finish.
const finishPatch = { status: 'completed', winner: { id: 'p1', name: 'Yamada' } };

function renderPage(onEditScore) {
  return render(
    <AdminShiaijoPage
      tournament={makeBracketTournament()}
      court="A"
      onBack={vi.fn()}
      onEditScore={onEditScore}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
}

// The local bracket as the page last saw it, read off the competitions the page
// handed to tournamentMatches on its most recent render.
function lastLocalSemi() {
  const calls = tournamentMatchesSpy.mock.calls;
  const last = calls[calls.length - 1][0];
  return last.competitions[0].bracket.rounds[0][0];
}

function lastLocalFinal() {
  const calls = tournamentMatchesSpy.mock.calls;
  const last = calls[calls.length - 1][0];
  return last.competitions[0].bracket.rounds[1][0];
}

beforeEach(() => {
  capturedSubmit.onSubmit = null;
  tournamentMatchesSpy.mockReset();
  tournamentMatchesSpy.mockImplementation(() => [runningSemi]);
  window.tournamentMatches = tournamentMatchesSpy;
});

describe('AdminShiaijo optimistic bracket advance vs a superseded write (bc-lww1)', () => {
  it('does NOT advance the local bracket when the server superseded the write', async () => {
    // 200 {applied:false}: a newer result for this match is already stored.
    const onEditScore = vi.fn().mockResolvedValue({ applied: false, reason: 'superseded' });
    renderPage(onEditScore);
    expect(capturedSubmit.onSubmit).toBeTypeOf('function');

    await act(async () => { await capturedSubmit.onSubmit(finishPatch); });

    // The operator's winner was discarded by the server, so it must not appear
    // anywhere locally: not as this bout's result...
    const semi = lastLocalSemi();
    expect(semi.status).toBe('running');
    expect(semi.winner).toBeUndefined();

    // ...and not promoted into the final, which must still show its placeholder.
    const final = lastLocalFinal();
    expect(final.sideA.name).toBe('Winner of r1-m0');
    expect(final.status).toBe('pending');
  });

  it('DOES advance the local bracket when the write was merely queued offline', async () => {
    // The counter-case, and the reason this is a split rather than a blanket
    // "never advance on a not-landed write": a queued write reconciles on
    // reconnect, and the local advance is the only thing that keeps an offline
    // court moving through its bracket.
    const onEditScore = vi.fn().mockResolvedValue({ queued: true });
    renderPage(onEditScore);

    await act(async () => { await capturedSubmit.onSubmit(finishPatch); });

    const semi = lastLocalSemi();
    expect(semi.status).toBe('completed');
    expect(semi.winner).toMatchObject({ id: 'p1', name: 'Yamada' });

    const final = lastLocalFinal();
    expect(final.sideA).toMatchObject({ id: 'p1', name: 'Yamada' });
    expect(final.status).toBe('scheduled');
  });

  it('advances normally when the write landed', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ id: 'r1-m0', status: 'completed' });
    renderPage(onEditScore);

    await act(async () => { await capturedSubmit.onSubmit(finishPatch); });

    expect(lastLocalSemi().status).toBe('completed');
    expect(lastLocalFinal().status).toBe('scheduled');
  });
});

// bc-cse: the Up-Next card's Start button is a start path none of the earlier
// call-site sweeps enumerated (it is pickMatch -> startMatch, not an editor
// mount). Found in browser verification: a clock_skew refusal left a dead
// first tap - the match stayed scheduled, nothing said why, and only the
// refusal-triggered relearn made the SECOND tap work. The card must say so.
describe('AdminShiaijo Up-Next start vs a clock_skew refusal (bc-cse)', () => {
  const scheduledMatch = {
    id: 'r1-m0', compId: 'c1', compName: 'Cup', status: 'scheduled',
    phase: 'bracket', matchNumber: 1, court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
  };

  it('a refused start shows the clock message and does not pretend to have started', async () => {
    tournamentMatchesSpy.mockImplementation(() => [scheduledMatch]);
    const onEditScore = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew', serverNowMs: 1 });
    renderPage(onEditScore);

    const btn = [...document.querySelectorAll('button')].find(b => (b.textContent || '').trim() === 'Start match');
    expect(btn).toBeTruthy();
    await act(async () => { btn.click(); });

    const err = [...document.querySelectorAll('[role=alert]')].map(a => a.textContent).join(' ');
    expect(err).toContain('clock');
    expect(err).toContain('try again');
    // Exactly one start attempt: the fix must not retry on its own here (the
    // relearn already ran; the RETRY is the operator's informed second tap).
    expect(onEditScore).toHaveBeenCalledTimes(1);
  });

  it('a normal start stays silent (the branch is refusal-only)', async () => {
    tournamentMatchesSpy.mockImplementation(() => [scheduledMatch]);
    const onEditScore = vi.fn().mockResolvedValue({ id: 'r1-m0', status: 'running' });
    renderPage(onEditScore);
    const btn = [...document.querySelectorAll('button')].find(b => (b.textContent || '').trim() === 'Start match');
    await act(async () => { btn.click(); });
    expect([...document.querySelectorAll('[role=alert]')].length).toBe(0);
  });
});
