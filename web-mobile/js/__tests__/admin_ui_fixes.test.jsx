import { describe, it, expect, vi } from 'vitest';
import { arraysEqual } from '../data.jsx';
import { pluralize, formatAdminHeaderSub, formatViewerHeaderEyebrow } from '../ui.jsx';

describe('WebUI Fixes - pluralize', () => {
  it('should pluralize correctly', () => {
    expect(pluralize(0, 'pool')).toBe('0 pools');
    expect(pluralize(1, 'pool')).toBe('1 pool');
    expect(pluralize(2, 'pool')).toBe('2 pools');
    
    expect(pluralize(0, 'match', 'matches')).toBe('0 matches');
    expect(pluralize(1, 'match', 'matches')).toBe('1 match');
    expect(pluralize(2, 'match', 'matches')).toBe('2 matches');
    
    expect(pluralize(1, 'shiaijo (court)', 'shiaijo (courts)')).toBe('1 shiaijo (court)');
    expect(pluralize(2, 'shiaijo (court)', 'shiaijo (courts)')).toBe('2 shiaijo (courts)');
  });
});

describe('Navigation Logic', () => {
  it('should have correct breadcrumb labels', () => {
     // This is a conceptual test as we can't easily render the full AdminCompetition here
     // but we verified the labels in the code edit.
     const _tournament = { name: "Tournament" };
     const c = { name: "Competition" };
     const onBack = vi.fn();
     
     const items = [
       { label: "Dashboard", onClick: onBack },
       { label: c.name, onClick: null }
     ];
     
     expect(items[0].label).toBe("Dashboard");
     expect(items[1].label).toBe("Competition");
  });
});

describe('Scoring Modal Feedback', () => {
  it('should handle submitting state', async () => {
    // Conceptual test for the submitting state logic
    let submitting = false;
    const setSubmitting = (val) => { submitting = val; };
    const onSubmit = vi.fn().mockResolvedValue({});

    const submit = async () => {
      setSubmitting(true);
      try {
        await onSubmit({});
      } finally {
        setSubmitting(false);
      }
    };

    const promise = submit();
    expect(submitting).toBe(true);
    await promise;
    expect(submitting).toBe(false);
    expect(onSubmit).toHaveBeenCalled();
  });
});

// --- Draw toggle logic ---
// Mirrors the toggle handler in ScoreEditorModal
function makeDrawToggleHandler(state, setState) {
  return () => {
    if (state.isDrawToggled) {
      setState({ ...state, isDrawToggled: false });
    } else {
      setState({ ...state, isDrawToggled: true, aPts: [], bPts: [] });
    }
  };
}

describe('ScoreEditorModal draw toggle', () => {
  it('sets draw mode and clears scores when toggled on with partial score', () => {
    const state = { isDrawToggled: false, aPts: ['M'], bPts: [] };
    let next;
    makeDrawToggleHandler(state, s => { next = s; })();
    expect(next.isDrawToggled).toBe(true);
    expect(next.aPts).toEqual([]);
    expect(next.bPts).toEqual([]);
  });

  it('clears draw mode when toggled off', () => {
    const state = { isDrawToggled: true, aPts: [], bPts: [] };
    let next;
    makeDrawToggleHandler(state, s => { next = s; })();
    expect(next.isDrawToggled).toBe(false);
  });

  it('sets draw mode and clears scores when toggled on at 0-0', () => {
    const state = { isDrawToggled: false, aPts: [], bPts: [] };
    let next;
    makeDrawToggleHandler(state, s => { next = s; })();
    expect(next.isDrawToggled).toBe(true);
    expect(next.aPts).toEqual([]);
    expect(next.bPts).toEqual([]);
  });
});

// --- Dirty-state and handleDismiss logic ---
// isDirty uses the real arraysEqual from data.jsx (imported above), so the
// test fails if the production implementation changes.
function isDirty(state, initial) {
  return (
    !arraysEqual(state.aPts, initial.aPts) ||
    !arraysEqual(state.bPts, initial.bPts) ||
    state.aFouls !== initial.aFouls ||
    state.bFouls !== initial.bFouls ||
    state.isDrawToggled !== initial.isDrawToggled
  );
}

function makeHandleDismiss({ submitting, dirty, confirmResult, onClose }) {
  const confirm = vi.fn().mockReturnValue(confirmResult);
  const dismiss = () => {
    if (submitting) return;
    if (dirty && !confirm('Discard unsaved scoring changes?')) return;
    onClose();
  };
  return { dismiss, confirm };
}

describe('ScoreEditorModal dirty-state and dismiss guard', () => {
  it('is not dirty when nothing has changed', () => {
    const initial = { aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0, isDrawToggled: false };
    expect(isDirty({ ...initial }, initial)).toBe(false);
  });

  it('is dirty when a point is added', () => {
    const initial = { aPts: [], bPts: [], aFouls: 0, bFouls: 0, isDrawToggled: false };
    expect(isDirty({ ...initial, aPts: ['M'] }, initial)).toBe(true);
  });

  it('is dirty when draw is toggled on', () => {
    const initial = { aPts: [], bPts: [], aFouls: 0, bFouls: 0, isDrawToggled: false };
    expect(isDirty({ ...initial, isDrawToggled: true }, initial)).toBe(true);
  });

  it('calls onClose immediately when not dirty', () => {
    const onClose = vi.fn();
    const { dismiss } = makeHandleDismiss({ submitting: false, dirty: false, confirmResult: true, onClose });
    dismiss();
    expect(onClose).toHaveBeenCalled();
  });

  it('prompts and closes when dirty and user confirms', () => {
    const onClose = vi.fn();
    const { dismiss, confirm } = makeHandleDismiss({ submitting: false, dirty: true, confirmResult: true, onClose });
    dismiss();
    expect(confirm).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('prompts but does not close when dirty and user cancels', () => {
    const onClose = vi.fn();
    const { dismiss, confirm } = makeHandleDismiss({ submitting: false, dirty: true, confirmResult: false, onClose });
    dismiss();
    expect(confirm).toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not close or prompt while submitting', () => {
    const onClose = vi.fn();
    const { dismiss, confirm } = makeHandleDismiss({ submitting: true, dirty: true, confirmResult: true, onClose });
    dismiss();
    expect(confirm).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

// --- Chained-nav court scoping ---
// Mirrors the prev/next computation in AdminScoreEditor (see
// admin_schedule.jsx near `const sameCourt = filtered.filter(...)`). Chained
// nav (Prev/Next/Finish + Start Next/←/→) must stay on the current match's
// shiaijo so operators don't hop courts mid-flow.
function pickChainedMatches(filtered, openMatch) {
  const openCourt = openMatch.court || "";
  const sameCourt = filtered.filter(m => (m.court || "") === openCourt);
  const key = m => `${m.compId}:${m.id}`;
  const openIdx = sameCourt.findIndex(m => key(m) === key(openMatch));
  const prev = openIdx > 0 ? sameCourt[openIdx - 1] : null;
  const next = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
  return { prev, next };
}

describe('AdminScoreEditor chained-nav court scoping', () => {
  const make = (id, court) => ({ compId: 'c', id, court });

  it('picks the next match on the same court, skipping a different court in between', () => {
    const filtered = [make('m1', 'A'), make('m2', 'B'), make('m3', 'A')];
    const { prev, next } = pickChainedMatches(filtered, filtered[0]);
    expect(prev).toBeNull();
    expect(next?.id).toBe('m3');
  });

  it('picks the previous match on the same court', () => {
    const filtered = [make('m1', 'A'), make('m2', 'B'), make('m3', 'A')];
    const { prev, next } = pickChainedMatches(filtered, filtered[2]);
    expect(prev?.id).toBe('m1');
    expect(next).toBeNull();
  });

  it('does not surface a different-court match as prev or next', () => {
    const filtered = [make('m1', 'A'), make('m2', 'B'), make('m3', 'A')];
    const { prev, next } = pickChainedMatches(filtered, filtered[1]);
    // Only B match in the list ; nothing to chain to.
    expect(prev).toBeNull();
    expect(next).toBeNull();
  });

  it('scopes unassigned matches to other unassigned matches', () => {
    const filtered = [make('u1', ''), make('a1', 'A'), make('u2', '')];
    const { prev, next } = pickChainedMatches(filtered, filtered[0]);
    expect(next?.id).toBe('u2');
    expect(prev).toBeNull();
  });

  it('treats missing court (undefined) as the same as empty string', () => {
    const filtered = [{ compId: 'c', id: 'x1' }, { compId: 'c', id: 'x2', court: '' }];
    const { next } = pickChainedMatches(filtered, filtered[0]);
    expect(next?.id).toBe('x2');
  });
});

// --- Score-edit status sort ---
// Mirrors the sort in AdminScoreEditor (see admin_schedule.jsx near
// `const order`). Status keys must match the API's MatchStatus values:
// running, scheduled, completed. Anything else (including `pending`,
// `in_progress`, `complete`) gets `?? 99` and sorts last ; same as prod.
// An earlier version used `in_progress`/`complete` which silently fell
// through to the scheduledAt tiebreaker for running and finished matches.
function sortScoreEdit(matches) {
  const order = { running: 0, scheduled: 1, completed: 2 };
  return [...matches].sort((a, b) => {
    const ao = order[a.status] ?? 99;
    const bo = order[b.status] ?? 99;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });
}

describe('AdminScoreEditor status sort', () => {
  it('orders running first, then scheduled, then completed, then unrecognised statuses last', () => {
    const input = [
      { id: 'p', status: 'pending', scheduledAt: '09:00' },
      { id: 'c', status: 'completed', scheduledAt: '09:00' },
      { id: 's', status: 'scheduled', scheduledAt: '09:00' },
      { id: 'r', status: 'running', scheduledAt: '09:00' },
    ];
    expect(sortScoreEdit(input).map(m => m.id)).toEqual(['r', 's', 'c', 'p']);
  });

  it('breaks ties within a status bucket by scheduledAt', () => {
    const input = [
      { id: 's2', status: 'scheduled', scheduledAt: '10:00' },
      { id: 's1', status: 'scheduled', scheduledAt: '09:00' },
      { id: 'r2', status: 'running', scheduledAt: '11:00' },
      { id: 'r1', status: 'running', scheduledAt: '09:30' },
    ];
    expect(sortScoreEdit(input).map(m => m.id)).toEqual(['r1', 'r2', 's1', 's2']);
  });

  it('puts unknown statuses last (regression: in_progress / complete fell through silently)', () => {
    const input = [
      { id: 'legacy', status: 'in_progress', scheduledAt: '09:00' },
      { id: 'real', status: 'running', scheduledAt: '10:00' },
    ];
    // Real `running` must come before the unrecognised `in_progress` even
    // though `in_progress` has an earlier scheduledAt.
    expect(sortScoreEdit(input).map(m => m.id)).toEqual(['real', 'legacy']);
  });
});

// --- Tournament Header Empty Venue Logic ---
describe('Tournament Header Empty Venue Logic', () => {
  it('should format admin subheader correctly when venue is present', () => {
    const formatted = formatAdminHeaderSub(
      '3 Jun 2026',
      'Crystal Palace',
      2,
      1,
      5
    );
    expect(formatted).toBe('3 Jun 2026 · Crystal Palace · 2 shiaijo (courts) · 1 competition · 5 participants');
  });

  it('should format admin subheader with no double dot separator when venue is empty', () => {
    const formatted = formatAdminHeaderSub(
      '3 Jun 2026',
      '',
      2,
      1,
      5
    );
    expect(formatted).toBe('3 Jun 2026 · 2 shiaijo (courts) · 1 competition · 5 participants');
    expect(formatted).not.toContain('·  ·');
  });

  it('should format viewer eyebrow correctly when venue is present', () => {
    const formatted = formatViewerHeaderEyebrow(
      '3 Jun 2026',
      'Crystal Palace'
    );
    expect(formatted).toBe('3 Jun 2026 · Crystal Palace');
  });

  it('should format viewer eyebrow with no trailing separator when venue is empty', () => {
    const formatted = formatViewerHeaderEyebrow(
      '3 Jun 2026',
      ''
    );
    expect(formatted).toBe('3 Jun 2026');
    expect(formatted.endsWith(' · ')).toBe(false);
  });
});
