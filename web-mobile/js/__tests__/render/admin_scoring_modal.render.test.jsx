import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { downstreamKnockoutRunningMessage } from '../../write_result.jsx';

// Window globals required by admin_scoring_modal.jsx (and its transitive
// import admin_scoring_shared.jsx). Divide into:
//  SYNC: called synchronously in the component body on every render
//  LAZY: only called inside event handlers or async effects
//
// All are set before the dynamic import so module-level capture lines like
//   const TEAM_POSITIONS = Array.from({length: window.MAX_TEAM_SIZE}, ...)
// resolve to real values rather than undefined.
const STUBBED_GLOBALS = {
  // SYNC: called in the component body on every render
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  // LAZY: only reached from event handlers / async effects
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  // Glossary components: used by admin_scoring_shared.jsx
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
let DraftKeptNotice;
let ReasonPrompt;
let DecisionPrompt;
let RecordedWithdrawal;
let ReopenFeedback;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ({ DraftKeptNotice, ReasonPrompt, DecisionPrompt, RecordedWithdrawal, ReopenFeedback } = await import('../../admin_scoring_shared.jsx'));
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

function makeIndividualMatch(overrides = {}) {
  return {
    id: 'm1',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    // No compId → fetchCompetitionDetails useEffect returns early (guard: if (!m.compId) return)
    ...overrides,
  };
}

function makeTeamMatch(overrides = {}) {
  return {
    id: 'm2',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 5,
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    ...overrides,
  };
}

function renderModal(match) {
  return render(
    <ScoreEditorModal
      match={match}
      onClose={vi.fn()}
      onSubmit={vi.fn()}
      password=""
    />
  );
}

describe('ScoreEditorModal render-smoke', () => {
  it('renders individual scheduled match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch())).not.toThrow();
  });

  it('renders individual running match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ status: 'running' }))).not.toThrow();
  });

  it('renders individual completed match (correction mode) without throwing', () => {
    expect(() =>
      renderModal(makeIndividualMatch({
        status: 'completed',
        ipponsA: ['M'],
        ipponsB: [],
        winner: { id: 'p1', name: 'Yamada' },
      }))
    ).not.toThrow();
  });

  it('renders individual pool match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ phase: 'pool', poolName: 'Pool 2' }))).not.toThrow();
  });

  it('renders individual knockout match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ phase: 'knockout', round: 'Semi-final' }))).not.toThrow();
  });

  it('renders team scheduled match (routes to TeamScoreEditorModal) without throwing', () => {
    expect(() => renderModal(makeTeamMatch())).not.toThrow();
  });

  it('renders team running match without throwing', () => {
    expect(() => renderModal(makeTeamMatch({ status: 'running' }))).not.toThrow();
  });

  it('renders team completed match without throwing', () => {
    expect(() => renderModal(makeTeamMatch({ status: 'completed' }))).not.toThrow();
  });
});

// A TINTED surface carries no SHIRO/AKA badge (operator ruling 2026-09-20,
// bc-sccl, superseding bc-dnst's "named once in text, by the header badge").
// Both editors' halves are tinted -- --red-soft for Aka, --white-side plus the
// 45° hatch for Shiro -- so the pill was a second statement of the same fact.
// This file previously asserted the badges were PRESENT; it now pins their
// removal, and pins the sr-only label that has to replace them, because dropping
// the badge without it would leave colour as the only signal (DESIGN.md §4).
describe('ScoreEditorModal side labelling', () => {
  for (const [label, make] of [['individual', makeIndividualMatch], ['team', makeTeamMatch]]) {
    it(`renders no SHIRO/AKA pill on the ${label} editor, and names the side for a screen reader`, () => {
      const { container } = renderModal(make());
      expect(container.querySelector('.sb-side__badge')).toBeNull();
      const srText = [...container.querySelectorAll('.sb-side .sr-only')].map(e => e.textContent.trim());
      expect(srText).toContain('Shiro:');
      expect(srText).toContain('Aka:');
    });
  }
});

// bc-rfsw: a withdrawal re-recorded on a knockout match whose later match is
// being fought now is refused (409 downstream_knockout_running, operator
// decision 2026-09-27). The editor shows the operator's sentence on the
// decision error line, asks nothing first (no "Proceed anyway?"), and does not move the court on.
describe('ScoreEditorModal: a decision refused because a later match is being fought', () => {
  for (const [label, make] of [
    ['individual', () => makeIndividualMatch({ id: 'm-r1-0', compId: 'c1', status: 'running', phase: 'knockout', ipponsA: [], ipponsB: [] })],
    ['team', () => makeTeamMatch({ id: 'm-r1-0', compId: 'c1', status: 'running', phase: 'knockout', teamSize: 3 })],
  ]) it(`${label}: shows the sentence on the decision error line, with no confirm and no advance`, async () => {
    const sentence = downstreamKnockoutRunningMessage([{ id: 'm-r2-0', number: 3, label: 'Match 3 (Final)', court: 'A' }]);
    expect(sentence).toBe('Match 3 (Final) is being fought now on Shiaijo A. Finish it or send it back to the queue, then save this correction again.');
    const refusal = Object.assign(new Error(sentence), {
      code: 'downstream_knockout_running',
      downstreamKnockoutRunning: { matchId: 'm-r1-0', runningMatches: [{ id: 'm-r2-0', number: 3, label: 'Match 3 (Final)', court: 'A' }] },
    });
    const prevKiken = window.isKikenDecision;
    window.isKikenDecision = (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury';
    window.API.recordDecision = vi.fn().mockRejectedValue(refusal);
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const onAfterDecision = vi.fn().mockResolvedValue(undefined);
    try {
      await act(async () => {
        render(<ScoreEditorModal
          match={make()}
          onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onAfterDecision={onAfterDecision} password="secret" />);
      });
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Kiken \(voluntary\)$/ })); });
      const form = document.querySelector('form.decision-prompt');
      fireEvent.click(form.querySelector('input[name="decision-side"][value="shiro"]'));
      await act(async () => { fireEvent.submit(form); });

      expect(screen.getByText(sentence)).toBeTruthy();
      expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
      expect(window.confirmDialog).not.toHaveBeenCalled();
      expect(onAfterDecision).not.toHaveBeenCalled();
    } finally {
      window.isKikenDecision = prevKiken;
    }
  });
});

describe('ScoreEditorModal read-only (view-only console)', () => {
  for (const [label, make] of [
    ['individual', () => makeIndividualMatch({ compId: 'c1', status: 'scheduled' })],
    ['team', () => makeTeamMatch({ compId: 'c1', status: 'scheduled', teamSize: 2 })],
  ]) {
    it(`${label}: keeps History toggle, Prev/Next, and Ippon key enabled while disabling write controls`, async () => {
      const onPrev = vi.fn();
      const onNext = vi.fn();
      const prevMatch = makeIndividualMatch({ id: 'm0', sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Kato' } });
      const nextMatch = makeIndividualMatch({ id: 'm9', sideA: { id: 'p5', name: 'Endo' }, sideB: { id: 'p6', name: 'Fujii' } });
      let utils;
      await act(async () => {
        utils = render(
          <ScoreEditorModal
            match={make()}
            onClose={vi.fn()}
            onSubmit={vi.fn()}
            prevMatch={prevMatch}
            nextMatch={nextMatch}
            onPrev={onPrev}
            onNext={onNext}
            password="pw"
            readOnly={true}
          />
        );
      });

      const historyBtn = utils.getByTestId('match-history-toggle');
      expect(historyBtn.disabled).toBe(false);
      expect(historyBtn.matches(':disabled')).toBe(false);

      const prevBtn = utils.getByRole('button', { name: 'Prev' });
      const nextBtn = utils.getByRole('button', { name: 'Next' });
      expect(prevBtn.disabled).toBe(false);
      expect(prevBtn.matches(':disabled')).toBe(false);
      expect(nextBtn.disabled).toBe(false);
      expect(nextBtn.matches(':disabled')).toBe(false);
      await act(async () => { fireEvent.click(prevBtn); });
      await act(async () => { fireEvent.click(nextBtn); });
      expect(onPrev).toHaveBeenCalledTimes(1);
      expect(onNext).toHaveBeenCalledTimes(1);

      const legend = utils.getByTestId('scoring-modal-ippon-legend');
      expect(legend.closest('fieldset:disabled')).toBeNull();

      const startBtn = utils.getByRole('button', { name: 'Start match' });
      expect(startBtn.disabled).toBe(true);
      expect(startBtn.matches(':disabled')).toBe(true);

      const scoreBtn = utils.container.querySelector('.ipt-btn');
      expect(scoreBtn).toBeTruthy();
      expect(scoreBtn.disabled).toBe(true);
      expect(scoreBtn.matches(':disabled')).toBe(true);
    });
  }

  it('engi: disables flag counters and save, ignores A/S/Backspace/Enter, hides disabled shortcut hints, and keeps Prev/Next working', async () => {
    const onSubmit = vi.fn();
    const onPrev = vi.fn();
    const onNext = vi.fn();
    const prevMatch = makeIndividualMatch({ id: 'm0', sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Kato' } });
    const nextMatch = makeIndividualMatch({ id: 'm9', sideA: { id: 'p5', name: 'Endo' }, sideB: { id: 'p6', name: 'Fujii' } });
    const engiMatch = makeIndividualMatch({ compId: 'c1', compEngi: true, status: 'scheduled', flagsA: 2, flagsB: 1 });
    let utils;
    await act(async () => {
      utils = render(
        <ScoreEditorModal
          match={engiMatch}
          onClose={vi.fn()}
          onSubmit={onSubmit}
          prevMatch={prevMatch}
          nextMatch={nextMatch}
          onPrev={onPrev}
          onNext={onNext}
          password="pw"
          readOnly={true}
        />
      );
    });

    for (const id of ['engi-shiro-dec', 'engi-shiro-inc', 'engi-aka-dec', 'engi-aka-inc', 'engi-submit']) {
      const btn = utils.getByTestId(id);
      expect(btn.disabled).toBe(true);
      expect(btn.matches(':disabled')).toBe(true);
    }

    await act(async () => {
      fireEvent.keyDown(document.body, { key: 'a' });
      fireEvent.keyDown(document.body, { key: 's' });
      fireEvent.keyDown(document.body, { key: 'Backspace' });
      fireEvent.keyDown(document.body, { key: 'Enter' });
    });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(utils.getByTestId('engi-aka-count').textContent).toBe('2');
    expect(utils.getByTestId('engi-shiro-count').textContent).toBe('1');

    const prevBtn = utils.getByRole('button', { name: 'Prev' });
    const nextBtn = utils.getByRole('button', { name: 'Next' });
    expect(prevBtn.disabled).toBe(false);
    expect(nextBtn.disabled).toBe(false);
    await act(async () => {
      fireEvent.click(prevBtn);
      fireEvent.click(nextBtn);
      fireEvent.keyDown(document.body, { key: 'ArrowLeft' });
      fireEvent.keyDown(document.body, { key: 'ArrowRight' });
    });
    expect(onPrev).toHaveBeenCalledTimes(2);
    expect(onNext).toHaveBeenCalledTimes(2);

    const hint = utils.getByTestId('engi-shortcut-hint');
    expect(hint.textContent).not.toContain('add Aka/Shiro flag');
    expect(hint.textContent).not.toContain('undo');
    expect(hint.textContent).not.toContain('save');
    expect(hint.textContent).toContain('prev/next');
    expect(hint.textContent).toContain('close');

    await act(async () => {
      utils.rerender(
        <ScoreEditorModal
          match={engiMatch}
          onClose={vi.fn()}
          onSubmit={onSubmit}
          password="pw"
          canClose={false}
          readOnly={true}
        />
      );
    });
    expect(utils.queryByTestId('engi-shortcut-hint')).toBeNull();
    expect(utils.getByTestId('engi-total').textContent).toContain('Total: 3');
  });

  it('individual and kachinuki team: ignores scoring keys and Enter, hides point shortcut hints, and keeps arrow navigation', async () => {
    for (const [label, make] of [
      ['individual', () => makeIndividualMatch({ compId: 'c1', status: 'scheduled', ipponsA: ['M'], ipponsB: [] })],
      ['team', () => makeTeamMatch({ compId: 'c1', status: 'scheduled', teamSize: 3, teamMatchType: 'kachinuki' })],
    ]) {
      const onSubmit = vi.fn();
      const onPrev = vi.fn();
      const onNext = vi.fn();
      const prevMatch = makeIndividualMatch({ id: 'm0', sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Kato' } });
      const nextMatch = makeIndividualMatch({ id: 'm9', sideA: { id: 'p5', name: 'Endo' }, sideB: { id: 'p6', name: 'Fujii' } });
      let utils;
      await act(async () => {
        utils = render(
          <ScoreEditorModal
            match={make()}
            onClose={vi.fn()}
            onSubmit={onSubmit}
            prevMatch={prevMatch}
            nextMatch={nextMatch}
            onPrev={onPrev}
            onNext={onNext}
            password="pw"
            readOnly={true}
          />
        );
      });

      const slotSelector = label === 'individual' ? '.sb-slot--filled' : '.editor-side__pt--filled';
      const initialFilled = utils.container.querySelectorAll(slotSelector).length;

      await act(async () => {
        fireEvent.keyDown(document.body, { key: 'm' });
        fireEvent.keyDown(document.body, { key: 'K', shiftKey: true });
        fireEvent.keyDown(document.body, { key: 'Enter' });
        fireEvent.keyDown(document.body, { key: 'ArrowLeft' });
        fireEvent.keyDown(document.body, { key: 'ArrowRight' });
      });

      expect(onSubmit).not.toHaveBeenCalled();
      expect(utils.container.querySelectorAll(slotSelector).length).toBe(initialFilled);
      expect(onPrev).toHaveBeenCalledTimes(1);
      expect(onNext).toHaveBeenCalledTimes(1);

      const hint = utils.getByTestId('scoring-modal-shortcut-hint');
      expect(hint.textContent).not.toContain('Shiro,');
      expect(hint.textContent).not.toContain('Aka');
      expect(hint.textContent).toContain('prev/next');
      expect(hint.textContent).toContain('close');

      await act(async () => {
        utils.rerender(
          <ScoreEditorModal
            match={make()}
            onClose={vi.fn()}
            onSubmit={onSubmit}
            password="pw"
            canClose={false}
            readOnly={true}
          />
        );
      });
      expect(utils.queryByTestId('scoring-modal-shortcut-hint')).toBeNull();
      expect(utils.getByTestId('scoring-modal-ippon-legend')).toBeTruthy();
      utils.unmount();
    }
  });

  it('kachinuki: disables done-bout edit, Remove this bout, and Encho/Undo encho controls when readOnly, while keeping an open bout Collapse button enabled', async () => {
    const doneBoutMatch = makeTeamMatch({
      id: 'm-kach-done',
      status: 'running',
      teamSize: 3,
      teamMatchType: 'kachinuki',
      subResults: [
        { position: 1, sideA: 'Alice', sideB: 'Bob', ipponsA: ['M'], ipponsB: [], winner: 'Alice', recorded: true },
        { position: 2, sideA: 'Alice', sideB: 'Carol', ipponsA: [], ipponsB: [] },
      ],
    });
    let utils;
    await act(async () => {
      utils = render(
        <ScoreEditorModal
          match={doneBoutMatch}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={true}
        />
      );
    });

    const doneBout = utils.getByTestId('kachinuki-done-bout-0');
    expect(doneBout.getAttribute('role')).toBeNull();
    expect(doneBout.getAttribute('tabIndex')).toBeNull();
    await act(async () => { fireEvent.click(doneBout); });
    expect(utils.queryByTestId('kachinuki-done-collapse-0')).toBeNull();
    expect(utils.getByTestId('kachinuki-remove-bout-button').disabled).toBe(true);
    expect(utils.getByTestId('kachinuki-add-bout-button').disabled).toBe(true);
    expect(utils.getByRole('button', { name: 'Record bout' }).disabled).toBe(true);
    expect(utils.getByTestId('kachinuki-end-match-button').disabled).toBe(true);

    // Open bout 1 while writable, then switch to readOnly: Collapse stays live while scoring buttons disable.
    await act(async () => {
      utils.rerender(
        <ScoreEditorModal
          match={doneBoutMatch}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={false}
        />
      );
    });
    await act(async () => { fireEvent.click(utils.getByTestId('kachinuki-done-bout-0')); });
    expect(utils.getByTestId('kachinuki-done-collapse-0')).toBeTruthy();

    await act(async () => {
      utils.rerender(
        <ScoreEditorModal
          match={doneBoutMatch}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={true}
        />
      );
    });
    const collapseBtn = utils.getByTestId('kachinuki-done-collapse-0');
    expect(collapseBtn.disabled).toBe(false);
    expect(collapseBtn.matches(':disabled')).toBe(false);
    expect(utils.container.querySelector('.ipt-btn').disabled).toBe(true);

    await act(async () => { fireEvent.click(collapseBtn); });
    expect(utils.queryByTestId('kachinuki-done-collapse-0')).toBeNull();
    expect(utils.getByTestId('kachinuki-done-bout-0')).toBeTruthy();
    utils.unmount();

    const enchoMatch = makeTeamMatch({
      id: 'm-kach-encho',
      status: 'running',
      teamSize: 3,
      teamMatchType: 'kachinuki',
      subResults: [
        { position: 1, sideA: 'Alice', sideB: 'Bob', ipponsA: ['M'], ipponsB: ['K'], encho: { periodCount: 1 } },
      ],
    });
    await act(async () => {
      utils = render(
        <ScoreEditorModal
          match={enchoMatch}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={true}
        />
      );
    });
    expect(utils.getByTestId('kachinuki-encho-button').disabled).toBe(true);
    expect(utils.getByTestId('kachinuki-encho-undo-button').disabled).toBe(true);
    utils.unmount();
  });

  it('keeps kept-draft Discard enabled and Save disabled when readOnly (individual and engi editors)', async () => {
    for (const [label, match, incSelector] of [
      ['individual', makeIndividualMatch({ id: 'm-ind-kept', status: 'scheduled' }), (u) => u.container.querySelector('.ipt-btn')],
      ['engi', makeIndividualMatch({ id: 'm-engi-kept', compEngi: true, status: 'scheduled' }), (u) => u.getByTestId('engi-aka-inc')],
    ]) {
      let utils;
      await act(async () => {
        utils = render(
          <ScoreEditorModal
            match={match}
            onClose={vi.fn()}
            onSubmit={vi.fn()}
            password="pw"
            readOnly={false}
          />
        );
      });
      await act(async () => { fireEvent.click(incSelector(utils)); });

      await act(async () => {
        utils.rerender(
          <ScoreEditorModal
            match={{ ...match, status: 'running' }}
            onClose={vi.fn()}
            onSubmit={vi.fn()}
            password="pw"
            readOnly={true}
          />
        );
      });

      expect(utils.getByTestId('draft-kept-notice'), `${label}: notice shown`).toBeTruthy();
      const saveBtn = utils.getByTestId('draft-kept-save');
      const discardBtn = utils.getByTestId('draft-kept-discard');
      expect(saveBtn.disabled).toBe(true);
      expect(saveBtn.matches(':disabled')).toBe(true);
      expect(discardBtn.disabled).toBe(false);
      expect(discardBtn.matches(':disabled')).toBe(false);

      await act(async () => { fireEvent.click(discardBtn); });
      expect(utils.queryByTestId('draft-kept-notice')).toBeNull();
      utils.unmount();
    }
  });

  it('keeps ReasonPrompt, DecisionPrompt, RecordedWithdrawal Undo, and ReopenFeedback dismiss enabled while disabling their write controls when readOnly', async () => {
    const completedEngi = makeIndividualMatch({ id: 'm-engi-corr', compEngi: true, status: 'completed', flagsA: 3, flagsB: 0 });
    let utils;
    await act(async () => {
      utils = render(
        <ScoreEditorModal
          match={completedEngi}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={false}
        />
      );
    });
    await act(async () => { fireEvent.click(utils.getByTestId('engi-submit')); });
    await act(async () => {
      utils.rerender(
        <ScoreEditorModal
          match={completedEngi}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password="pw"
          readOnly={true}
        />
      );
    });
    const cancelPromptBtn = utils.getByRole('button', { name: 'Cancel' });
    const confirmPromptBtn = utils.getByRole('button', { name: 'Confirm' });
    expect(confirmPromptBtn.disabled).toBe(true);
    expect(utils.getByLabelText('Reason category').disabled).toBe(true);
    expect(utils.getByLabelText('Reason note').disabled).toBe(true);
    expect(cancelPromptBtn.disabled).toBe(false);
    expect(cancelPromptBtn.matches(':disabled')).toBe(false);
    await act(async () => { fireEvent.click(cancelPromptBtn); });
    expect(utils.queryByRole('button', { name: 'Confirm' })).toBeNull();
    expect(utils.getByTestId('engi-submit').disabled).toBe(true);
    utils.unmount();

    // Shared recovery components directly with readOnly={true}
    const onSave = vi.fn();
    const onDiscard = vi.fn();
    utils = render(<DraftKeptNotice from="scheduled" onSave={onSave} onDiscard={onDiscard} disabled={false} readOnly={true} />);
    expect(utils.getByTestId('draft-kept-save').disabled).toBe(true);
    expect(utils.getByTestId('draft-kept-discard').disabled).toBe(false);
    fireEvent.click(utils.getByTestId('draft-kept-discard'));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    utils.unmount();

    const onConfirm = vi.fn();
    const onCancelReason = vi.fn();
    utils = render(<ReasonPrompt presets={['Scoring error']} onConfirm={onConfirm} onCancel={onCancelReason} readOnly={true} />);
    expect(utils.getByRole('button', { name: 'Confirm' }).disabled).toBe(true);
    expect(utils.getByRole('button', { name: 'Cancel' }).disabled).toBe(false);
    fireEvent.click(utils.getByRole('button', { name: 'Cancel' }));
    expect(onCancelReason).toHaveBeenCalledTimes(1);
    utils.unmount();

    const onCancelDecision = vi.fn();
    utils = render(
      <DecisionPrompt
        kind="fusenpai"
        sideA={{ name: 'Yamada' }}
        sideB={{ name: 'Tanaka' }}
        askReason={false}
        submitting={false}
        readOnly={true}
        onCancel={onCancelDecision}
        onSubmit={vi.fn()}
      />
    );
    for (const radio of utils.container.querySelectorAll('input[type="radio"]')) {
      expect(radio.disabled).toBe(true);
    }
    expect(utils.getByRole('button', { name: 'Record' }).disabled).toBe(true);
    expect(utils.getByRole('button', { name: 'Cancel' }).disabled).toBe(false);
    fireEvent.click(utils.getByRole('button', { name: 'Cancel' }));
    expect(onCancelDecision).toHaveBeenCalledTimes(1);
    utils.unmount();

    const onUndo = vi.fn();
    const withdrawnMatch = makeIndividualMatch({ status: 'completed', decision: 'fusenpai', decisionBy: 'aka' });
    utils = render(
      <RecordedWithdrawal
        match={withdrawnMatch}
        ctl={{ reopen: vi.fn(), busy: false, landed: false }}
        readOnly={true}
        removal={{ removed: true, held: false, onRemove: vi.fn(), onUndo }}
      />
    );
    expect(utils.getByTestId('remove-withdrawal-undo').disabled).toBe(false);
    fireEvent.click(utils.getByTestId('remove-withdrawal-undo'));
    expect(onUndo).toHaveBeenCalledTimes(1);
    utils.unmount();

    const dismissConflict = vi.fn();
    utils = render(
      <ReopenFeedback
        testIdPrefix="withdrawal-reopen"
        readOnly={true}
        ctl={{
          notice: '',
          err: '',
          busy: false,
          allowCourtForce: true,
          reopenAnyway: vi.fn(),
          requeueBlocker: vi.fn(),
          dismissConflict,
          conflict: { court: 'A', matchId: 'm-blk', compId: 'c1', label: 'Match 2' },
        }}
      />
    );
    expect(utils.getByTestId('withdrawal-reopen-reopen-anyway-button').disabled).toBe(true);
    expect(utils.getByTestId('withdrawal-reopen-requeue-button').disabled).toBe(true);
    expect(utils.getByTestId('withdrawal-reopen-conflict-dismiss').disabled).toBe(false);
    fireEvent.click(utils.getByTestId('withdrawal-reopen-conflict-dismiss'));
    expect(dismissConflict).toHaveBeenCalledTimes(1);
    utils.unmount();
  });
});
