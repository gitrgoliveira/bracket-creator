// bc-lww1: a score write can come back HTTP 200 with `{applied:false,
// reason:'superseded'}` -- the server stored NOTHING, because another
// device's newer result already won the timestamp last-write-wins guard.
// write_result.jsx's writeDidNotLand()/writeWasSuperseded() are the ONE
// predicate every consuming surface must ask before behaving as if a write
// landed: closing an editor, advancing to the next match, reporting success.
//
// score_editor_mount_sites.render.test.jsx pins WHAT each surface WIRES to
// ScoreEditorModal (onSubmit is a function, onSubmitAndNext is null, etc).
// It never INVOKES those handlers. This file pins the other half: what
// actually happens when the wired handler runs and the awaited write result
// says "did not land". Three call sites had no direct-invocation test:
//
//   A. admin_schedule_score_editor.jsx:309 onSubmit          -- must NOT close
//   B. admin_schedule_score_editor.jsx:361 onSubmitAndNext   -- must NOT advance
//   C. viewer_match.jsx:319 onSubmit (public self-run)       -- must NOT close
//
// Mechanism (same probe technique as the sibling file): window.ScoreEditorModal
// is replaced with a prop-recording probe BEFORE the surfaces are imported (A
// captures it at module-eval time), each surface is driven until its editor
// mounts, and then the CAPTURED handler is invoked directly with a controlled
// onEditScore / API.recordScore resolution -- landed, superseded, queued --
// so we exercise exactly the branch an operator hits when the server's LWW
// guard silently drops their write.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { writeDidNotLand } from '../../write_result.jsx';

// ── the probe ────────────────────────────────────────────────────────────────

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return React.createElement('div', { 'data-testid': 'probe-score-editor' });
}

// ── window stubs ───────────────────────────────────────────────────────────
// Trimmed to what admin_schedule_score_editor.jsx and viewer_match.jsx
// actually read (grep-verified: no PoolsViewer/BracketTree/LeagueStandings/
// confirmDialog references in either file). writeDidNotLand is the REAL
// implementation, not a stand-in -- this file is exactly testing that callers
// obey it, so faking the predicate would test nothing.

const STUBBED_GLOBALS = {
  // MODULE-EVAL capture: admin_schedule_score_editor.jsx reads
  // window.ScoreEditorModal at module load (line 15), so it must exist
  // before that import in beforeAll.
  ScoreEditorModal: ProbeScoreEditor,
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  compMatches: () => [],
  startPatch: () => ({ status: 'running', winner: null }),
  writeDidNotLand,
  API: {
    recordScore: vi.fn(),
  },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let AdminScoreEditor, MatchViewerModal;

beforeAll(async () => {
  window.scrollTo = vi.fn(); // jsdom doesn't implement it; the schedule surface calls it on open.
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  const sched = await import('../../admin_schedule_score_editor.jsx');
  const viewer = await import('../../viewer_match.jsx');
  AdminScoreEditor = sched.AdminScoreEditor;
  MatchViewerModal = viewer.MatchViewerModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => {
  probe.props = null;
  window.compMatches = STUBBED_GLOBALS.compMatches;
  window.API = { recordScore: vi.fn() };
});

function runningMatch(overrides = {}) {
  return {
    id: 'm1', compId: 'c1', compName: 'Comp', status: 'running',
    phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

// The three shapes a write result can take.
const LANDED = { applied: true };
const SUPERSEDED = { applied: false, reason: 'superseded' };
const QUEUED = { queued: true };

// A "finish" patch: NOT a start (status isn't "running" and it carries a
// winner), so at call site A a landed write takes the ordinary
// close-the-editor branch rather than the special "stay open, we just
// started the match" branch. Reused at C too, where the distinction doesn't
// exist (see that describe block).
const FINISH_PATCH = { status: 'completed', winner: { id: 'p1', name: 'Yamada' } };

// ── A. admin_schedule_score_editor.jsx onSubmit (Scores tab) ────────────────

describe('call site A: admin_schedule_score_editor.jsx onSubmit', () => {
  async function openEditor(onEditScore) {
    window.compMatches = () => [runningMatch()];
    await act(async () => {
      render(
        <AdminScoreEditor
          t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
          onEditScore={onEditScore}
          onMoveCourt={null}
          password="pw"
          showToast={vi.fn()}
        />
      );
    });
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
  }

  it('superseded: the editor stays open', async () => {
    const onEditScore = vi.fn().mockResolvedValue(SUPERSEDED);
    await openEditor(onEditScore);
    await act(async () => { await probe.props.onSubmit(FINISH_PATCH); });
    // If this closed, the operator would see the match list with no error at
    // all -- indistinguishable from a genuinely saved finish -- while the
    // server actually kept a DIFFERENT device's result and this scoreline
    // was never stored. The editor staying open (with its not-saved banner)
    // is the only thing that tells them to go check.
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
  });

  it('queued: the editor stays open too (same "not stored yet" fact as superseded, at this call site)', async () => {
    const onEditScore = vi.fn().mockResolvedValue(QUEUED);
    await openEditor(onEditScore);
    await act(async () => { await probe.props.onSubmit(FINISH_PATCH); });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
  });

  it('landed: closes the editor as normal (counter-case -- a test that only checks "stays open" would pass even if the editor NEVER closed)', async () => {
    const onEditScore = vi.fn().mockResolvedValue(LANDED);
    await openEditor(onEditScore);
    await act(async () => { await probe.props.onSubmit(FINISH_PATCH); });
    expect(screen.queryByTestId('probe-score-editor')).toBeFalsy();
  });
});

// ── B. admin_schedule_score_editor.jsx onSubmitAndNext (Finish + Start Next) ─

describe('call site B: admin_schedule_score_editor.jsx onSubmitAndNext', () => {
  // Two same-court matches so a next active match exists and onSubmitAndNext
  // is wired (see score_editor_mount_sites.render.test.jsx scenario 4a).
  async function openEditorWithNext(onEditScore) {
    const m1 = runningMatch();
    const m2 = runningMatch({ id: 'm2', status: 'scheduled' });
    window.compMatches = () => [m1, m2];
    await act(async () => {
      render(
        <AdminScoreEditor
          t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
          onEditScore={onEditScore}
          onMoveCourt={null}
          password="pw"
          showToast={vi.fn()}
        />
      );
    });
    // m1 (running) sorts before m2 (scheduled); open it.
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    expect(typeof probe.props.onSubmitAndNext).toBe('function');
    expect(probe.props.match.id).toBe('m1');
  }

  it('superseded: stays on the current match, does not advance', async () => {
    const onEditScore = vi.fn().mockResolvedValue(SUPERSEDED);
    await openEditorWithNext(onEditScore);
    await act(async () => { await probe.props.onSubmitAndNext(FINISH_PATCH); });
    // Operator terms: without this guard, "Finish + Start Next" would open
    // AND START m2 on court A while m1's real (server-held) result is some
    // OTHER device's write -- two matches now claiming the same shiaijo, and
    // the operator has no idea m1's finish never actually saved.
    expect(probe.props.match.id).toBe('m1');
    // Only the finish write should have gone out. A second call means the
    // code tried to start m2 anyway, silently, despite the guard.
    expect(onEditScore).toHaveBeenCalledTimes(1);
  });

  it('queued: stays on the current match too, same as superseded here', async () => {
    const onEditScore = vi.fn().mockResolvedValue(QUEUED);
    await openEditorWithNext(onEditScore);
    await act(async () => { await probe.props.onSubmitAndNext(FINISH_PATCH); });
    expect(probe.props.match.id).toBe('m1');
    expect(onEditScore).toHaveBeenCalledTimes(1);
  });

  it('landed: advances to the next same-court match and starts it (counter-case -- a test that only checks "does not advance" would pass even if it NEVER advanced)', async () => {
    const onEditScore = vi.fn().mockResolvedValue(LANDED);
    await openEditorWithNext(onEditScore);
    await act(async () => { await probe.props.onSubmitAndNext(FINISH_PATCH); });
    // The finish write landed, so the operator is now looking at m2 --
    // and a second write went out to start it (mirrors "Finish + Start Next"
    // actually starting the next match, not just opening it).
    expect(probe.props.match.id).toBe('m2');
    expect(onEditScore).toHaveBeenCalledTimes(2);
  });
});

// ── C. viewer_match.jsx onSubmit (public self-run surface) ──────────────────

describe('call site C: viewer_match.jsx onSubmit (public self-run)', () => {
  // Unlike site A, this call site has no separate "we just started the
  // match" branch: any landed write unconditionally closes the editor and
  // the outer viewer modal, so FINISH_PATCH's exact shape doesn't matter
  // here -- only the write result does.
  async function openViewerEditor(recordScoreImpl) {
    window.API.recordScore = vi.fn(recordScoreImpl);
    const onClose = vi.fn();
    await act(async () => {
      render(
        <MatchViewerModal
          match={runningMatch()}
          onClose={onClose}
          tournament={{ mode: 'self-run' }}
          compId="c1"
        />
      );
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Report result' }));
    });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    return onClose;
  }

  it('superseded: the editor stays open', async () => {
    const onClose = await openViewerEditor(async () => SUPERSEDED);
    await act(async () => { await probe.props.onSubmit(FINISH_PATCH); });
    // This surface is the public self-run page: no toast, no admin console
    // to cross-check against. The not-saved banner on a still-open editor is
    // the ONLY signal an attendee gets that their tap did nothing -- closing
    // here would read as a confirmed, saved result that was never stored.
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  // No separate queued case here: writeDidNotLand treats queued and
  // superseded identically at every one of these three call sites (they only
  // diverge at the shiaijo bracket-advance site, tested elsewhere), and that
  // equivalence is already pinned at site A above. Repeating it per site
  // would only restate the same fact against write_result.jsx's own logic.

  it('landed: closes the editor and the outer modal (counter-case -- a test that only checks "stays open" would pass even if the editor NEVER closed)', async () => {
    const onClose = await openViewerEditor(async () => LANDED);
    await act(async () => { await probe.props.onSubmit(FINISH_PATCH); });
    expect(screen.queryByTestId('probe-score-editor')).toBeFalsy();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
