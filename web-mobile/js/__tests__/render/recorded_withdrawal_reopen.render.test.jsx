// bc-tmfn: Clear withdrawal and reopen (operator rulings 2026-09-24:
// "Everything should be able to be fixed, in case of a wrong entry. The
// operator just needs to be aware of the consequences, if it affects
// downstream matches."). Correcting a match a withdrawal ended shows what is
// recorded and ONE way to remove it, from one shared component
// (RecordedWithdrawal, admin_scoring_shared.jsx) in BOTH editors: a reopen,
// with its reason asked first, never a score write. These pin the line, the
// request, the downstream confirm, the court-busy remedy, and where the
// control must not appear (kachinuki, which keeps its own Reopen; a match no
// withdrawal decided).

import React from 'react';
import { render, act, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { readStylesheet, cssBlock } from '../helpers/source.js';
import { DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED } from '../../write_result.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn().mockResolvedValue({ reopenedMatches: [] }),
    requeueBlockerAndReopen: vi.fn().mockResolvedValue({ reopenedMatches: [] }),
  };
});

// A fixed-order team pool match Kyoto (aka) withdrew from after bout 1.
function teamWithdrawal(overrides = {}) {
  return {
    id: 'm-pool-1', compId: 'comp1', status: 'completed', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' }],
    decision: 'kiken-voluntary', decisionBy: 'aka', decisionReason: 'knee',
    winner: { id: 'team-osaka', name: 'Osaka' }, ipponsB: ['○', '○'],
    ...overrides,
  };
}

// An individual knockout match Tanaka (shiro) did not appear for.
function individualWithdrawal(overrides = {}) {
  return {
    id: 'm-r1-0', compId: 'comp1', status: 'completed', phase: 'knockout', round: 'Round 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ipponsA: ['○', '○'], ipponsB: [], hansokuA: 0, hansokuB: 0,
    decision: 'fusenpai', decisionBy: 'shiro',
    winner: { id: 'p1', name: 'Yamada' },
    ...overrides,
  };
}

async function mount(match, props = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" {...props} />);
  });
  return view;
}

// LiveHost behaves like the real hosts (the Scores tab, the pools page, the
// court console): the open match is resolved from live data, so when a reopen
// lands the SAME editor re-renders with the match running (flip, standing in
// for the SSE refetch), and onClose really unmounts it. A vi.fn onClose over
// a frozen match cannot show what the operator sees after the reopen.
let flip;
function LiveHost({ initial, onClose }) {
  const [match, setMatch] = React.useState(initial);
  const [open, setOpen] = React.useState(true);
  flip = (patch) => setMatch((m) => ({ ...m, ...patch }));
  if (!open) return <div data-testid="host-closed" />;
  return (
    <ScoreEditorModal
      match={match}
      onClose={() => { onClose?.(); setOpen(false); }}
      onSubmit={vi.fn().mockResolvedValue(undefined)}
      password="secret"
    />
  );
}

async function mountLive(match, onClose) {
  await act(async () => { render(<LiveHost initial={match} onClose={onClose} />); });
}

// What the server leaves after Clear withdrawal and reopen on these fixtures:
// running, verdict cleared, the winner's default-win maru gone (neither
// withdrawn side had struck anything at match level).
const REOPENED = { status: 'running', decision: '', decisionBy: '', decisionReason: '', winner: null, ipponsA: [], ipponsB: [] };

async function clearAndConfirm() {
  await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
  const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
  await act(async () => { fireEvent.click(confirm); });
}

describe.each([
  ['team', teamWithdrawal, 'Recorded: Kiken – Voluntary, Kyoto withdrew.', 'Kyoto'],
  ['individual', individualWithdrawal, 'Recorded: Fusenpai, Tanaka did not appear.', 'Tanaka'],
])('%s editor: Clear withdrawal and reopen', (_kind, fixture, recorded, withdrawn) => {
  it('says what is recorded and reopens with the reason asked first, never a score write', async () => {
    const onClose = vi.fn();
    await mountLive(fixture(), onClose);
    const line = screen.getByTestId('recorded-withdrawal');
    expect(line.textContent).toContain(recorded);
    expect(screen.queryByTestId('clear-withdrawal-consequence')).toBeNull();

    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent)
      .toContain(`${withdrawn} can compete again`);
    expect(window.API.reopenMatch).not.toHaveBeenCalled();

    const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
    await act(async () => { fireEvent.click(confirm); });
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', fixture().id, 'secret', { reason: 'Withdrawal recorded by mistake', force: false },
    );
    expect(window.API.recordScore).not.toHaveBeenCalled();

    // bc-tmfn R3: the operator is told to score the rest and finish it, so
    // the editor stays open and follows the match to running in place.
    await act(async () => { flip(REOPENED); });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByTestId('host-closed')).toBeNull();
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
    expect(screen.queryByText('CORRECTION')).toBeNull();
  });

  it('asks before reopening a later match with its own result, then retries with force', async () => {
    const refusal = Object.assign(new Error('refused'), {
      downstreamKnockoutPlayed: { matchId: fixture().id, blockingMatchId: 'm-r2-0', blockingMatches: [{ id: 'm-r2-0', number: 2 }], displaced: 'Yamada' },
    });
    window.API.reopenMatch = vi.fn()
      .mockRejectedValueOnce(refusal)
      .mockResolvedValueOnce({ reopenedMatches: [{ id: 'm-r2-0', number: 2 }] });
    const onClose = vi.fn();
    await mountLive(fixture(), onClose);
    await clearAndConfirm();
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(2));
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('Match 2');
    expect(window.API.reopenMatch.mock.calls[1][3]).toEqual({ reason: 'Withdrawal recorded by mistake', force: true });

    // bc-tmfn R3: the notice is read AFTER the match is running again, which
    // is when the operator looks for it. It used to live inside
    // RecordedWithdrawal, which unmounts on that flip, in an editor whose
    // host had already closed it.
    await act(async () => { flip(REOPENED); });
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('withdrawal-reopen-notice').textContent)
      .toBe('Match 2 was reopened: it must be fought and scored again.');
  });

  it('declining the downstream confirm leaves everything as it was and says so', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('refused'), {
      downstreamKnockoutPlayed: { blockingMatchId: 'm-r2-0', blockingMatches: [{ id: 'm-r2-0', number: 2 }] },
    }));
    const onClose = vi.fn();
    await mount(fixture(), { onClose });
    await clearAndConfirm();
    await waitFor(() => expect(screen.getByTestId('withdrawal-reopen-error').textContent).toBe(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED));
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a busy court offers the remedy, which carries the same reason', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('Court A already has a running match (m-blk).'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1' },
    ));
    await mount(fixture());
    await clearAndConfirm();
    await waitFor(() => expect(screen.getByTestId('withdrawal-reopen-conflict')).toBeTruthy());
    expect(screen.getByTestId('withdrawal-reopen-conflict-warning').textContent).toMatch(/clears any score already entered/);
    await act(async () => { fireEvent.click(screen.getByTestId('withdrawal-reopen-requeue-button')); });
    expect(window.API.requeueBlockerAndReopen).toHaveBeenCalledWith(
      'comp1', fixture().id, 'comp1', 'm-blk', 'secret', { reason: 'Withdrawal recorded by mistake', force: false },
    );
  });
});

// bc-tmfn R7: a kachinuki encounter a withdrawal decided reopens through the
// same RecordedWithdrawal as every other editor (the line, the consequence,
// the reason), because that reopen also makes the withdrawn team eligible
// again; its plain one-tap Reopen stays for every other kachinuki result. One
// control, one consequence text, and the kachinuki feedback ids still apply.
describe('kachinuki: a withdrawal reopens through Clear withdrawal and reopen', () => {
  const kachinukiWithdrawal = (o = {}) => teamWithdrawal({ teamMatchType: 'kachinuki', compFormat: 'mixed', ...o });

  it('shows the recorded line in place of the one-tap Reopen, and states the consequence first', async () => {
    const onClose = vi.fn();
    await mountLive(kachinukiWithdrawal(), onClose);
    expect(screen.getByTestId('recorded-withdrawal').textContent).toContain('Recorded: Kiken – Voluntary, Kyoto withdrew.');
    expect(screen.queryByTestId('kachinuki-reopen-button')).toBeNull();

    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('Kyoto can compete again');
    expect(window.API.reopenMatch).not.toHaveBeenCalled();
    const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
    await act(async () => { fireEvent.click(confirm); });
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', 'm-pool-1', 'secret', { reason: 'Withdrawal recorded by mistake', force: false },
    );
    await act(async () => { flip(REOPENED); });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
  });

  it('keeps the kachinuki feedback ids for what the reopen came back with', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('Court A already has a running match (m-blk).'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1' },
    ));
    await mount(kachinukiWithdrawal());
    await clearAndConfirm();
    await waitFor(() => expect(screen.getByTestId('kachinuki-reopen-conflict')).toBeTruthy());
  });

  it('an encounter no withdrawal decided keeps the one-tap Reopen and no recorded line', async () => {
    await mount(kachinukiWithdrawal({ decision: 'kachinuki-exhaustion', decisionBy: '', decisionReason: '', winner: { id: 'team-kyoto', name: 'Kyoto' } }));
    expect(screen.getByTestId('kachinuki-reopen-button')).toBeTruthy();
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
  });
});

describe('where Clear withdrawal and reopen is not offered', () => {

  it.each([
    ['team', () => teamWithdrawal({ decision: '', decisionBy: '', winner: { id: 'team-kyoto', name: 'Kyoto' } })],
    ['individual', () => individualWithdrawal({ decision: '', decisionBy: '', ipponsA: ['M', 'K'] })],
  ])('a %s match no withdrawal decided', async (_kind, fixture) => {
    await mount(fixture());
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
  });

  it('a match still being fought', async () => {
    await mount(individualWithdrawal({ status: 'running', decision: '', decisionBy: '', winner: null, ipponsA: ['M'] }));
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
  });
});

// bc-tmfn R1: Save correction on an individual match a withdrawal decided
// keeps the withdrawal and the winner's default-win maru and saves the
// withdrawer's letters as entered (operator ruling 2026-09-24: "Save
// correction should just save what the operator enters"). The maru used to
// count as two points, so the bout read as decided, every add button was
// off, and the only way to change the withdrawer's letter was to delete the
// winner's maru, which the server then silently put back.
describe('individual correction while a withdrawal is recorded', () => {
  // Yamada (aka) won by default when Tanaka (shiro) withdrew holding a men.
  const kiken = (o = {}) => individualWithdrawal({ decision: 'kiken-voluntary', decisionBy: 'shiro', ipponsB: ['M'], ...o });
  const slots = (side) => [...document.querySelectorAll(`.sb-slots--${side} .sb-slot`)];
  const addButton = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} .ipt-btn`)].find((b) => b.textContent === letter);

  it("shows the winner's maru read-only and saves the withdrawer's corrected letter", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(kiken(), { onSubmit });

    expect(slots('aka').map((b) => b.textContent)).toEqual(['○', '○']);
    slots('aka').forEach((b) => expect(b.disabled).toBe(true));
    expect(addButton('aka', 'M').disabled).toBe(true);

    // The men was entered by mistake: tap it off and enter the kote.
    const men = slots('shiro').find((b) => b.textContent === 'M');
    expect(men.disabled).toBe(false);
    await act(async () => { fireEvent.click(men); });
    expect(addButton('shiro', 'K').disabled).toBe(false);
    await act(async () => { fireEvent.click(addButton('shiro', 'K')); });
    // One point is the withdrawer's most: two would have won the bout.
    expect(addButton('shiro', 'D').disabled).toBe(true);
    expect(slots('aka').map((b) => b.textContent)).toEqual(['○', '○']);

    const save = [...document.querySelectorAll('.score-nav button')].find((b) => b.textContent === 'Save correction');
    await act(async () => { fireEvent.click(save); });
    const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
    await act(async () => { fireEvent.click(confirm); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.ipponsB).toEqual(['K']);
    expect(patch.ipponsA).toEqual(['○', '○']);
    expect(patch.winner?.name).toBe('Yamada');
  });

  // UAT (bc-tmfn): the winner's letters and maru were disabled but looked
  // exactly like the withdrawer's live ones: full opacity, same colours, a
  // pointer cursor. jsdom applies no stylesheet, so this pins the two halves
  // that make the look: the read-only controls ARE disabled .ipt-btn /
  // .sb-slot buttons, and styles.css gives exactly those the disabled look
  // the board's other disabled controls have (.btn:disabled). Opacity, not a
  // colour: the side's tint and the maru's fill keep their colours.
  it("the winner's read-only side is styled as disabled, and the withdrawer's is not", async () => {
    await mount(kiken());
    const readOnly = [...slots('aka'), ...['M', 'K', 'D', 'T', 'H'].map((l) => addButton('aka', l))];
    readOnly.forEach((b) => {
      expect(b.disabled).toBe(true);
      expect(b.matches('.ipt-btn:disabled, .sb-slot:disabled')).toBe(true);
    });
    // The withdrawer's recorded men is the operator's to correct: live.
    expect(slots('shiro').find((b) => b.textContent === 'M').matches('.sb-slot:disabled')).toBe(false);
    // The Aka half still carries its tint and its side in text.
    expect(document.querySelector('.sb-side--aka')).not.toBeNull();

    const css = readStylesheet();
    const btnDisabled = cssBlock(css, '.btn:disabled');
    for (const sel of ['.ipt-btn:disabled', '.sb-slot:disabled']) {
      const rule = cssBlock(css, sel);
      expect(rule, `${sel} has no rule in styles.css`).not.toBeNull();
      expect(rule).toMatch(/cursor:\s*not-allowed/);
      expect(rule).toMatch(/opacity:\s*0\.6/);
      expect(rule).not.toMatch(/(^|[^-])(color|background)\s*:/);
    }
    expect(btnDisabled).toMatch(/opacity:\s*0\.6/);
    // A disabled letter does not react to the pointer.
    expect(css).toMatch(/^\.ipt-btn:hover:not\(:disabled\)\s*\{/m);
    expect(css).toMatch(/^\.ipt-btn--h:hover:not\(:disabled\)\s*\{/m);
  });

  it('a reopen made with unsaved taps on the board leaves no maru behind', async () => {
    // The editor stays open through the reopen, so its board must drop the
    // winner's maru with the ruling even when the operator had tapped
    // something first: kept, the maru would be tappable and autosaved as
    // two points on a match that no longer has a withdrawal.
    await mountLive(kiken());
    const men = slots('shiro').find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(men); });
    await clearAndConfirm();
    await act(async () => { flip({ ...REOPENED, ipponsB: ['M'] }); });
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
    expect(slots('aka').map((b) => b.textContent)).toEqual(['·', '·']);
    expect(slots('shiro').map((b) => b.textContent).filter((t) => t !== '·')).toEqual(['M']);
    expect(addButton('aka', 'M').disabled).toBe(false);
  });

  it('the keyboard cannot add to the winner either (one maru, in encho)', async () => {
    await mount(kiken({ ipponsA: ['○'], ipponsB: [], encho: { periodCount: 1 } }));
    expect(slots('aka').filter((b) => b.textContent === '○')).toHaveLength(1);
    await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
    expect(slots('aka').map((b) => b.textContent).filter((t) => t !== '·')).toEqual(['○']);
    await act(async () => { fireEvent.keyDown(window, { key: 'm' }); });
    expect(slots('shiro').map((b) => b.textContent).filter((t) => t !== '·')).toEqual(['M']);
  });
});

// bc-tmfn R2: on a single bout the reopen cannot keep the winner's points
// (the withdrawal replaced them with the default win when it was recorded),
// so the consequence says so and asks for them to be entered again. A team
// encounter keeps its bouts and keeps its wording.
describe("Clear withdrawal and reopen names the winner's lost points on a single bout", () => {
  it('individual kiken', async () => {
    await mount(individualWithdrawal({ decision: 'kiken-injury', ipponsB: ['M'] }));
    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toContain("Yamada's points were replaced by the default win when the withdrawal was recorded, so enter them again");
    expect(text).toContain("Tanaka's points are kept");
    expect(text).not.toContain('with what was fought kept');
  });

  it('individual fusenpai: a no-show fought nothing, so only the default win goes', async () => {
    await mount(individualWithdrawal());
    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toContain('the default win given to Yamada is removed, and Tanaka can compete again');
    expect(text).not.toContain('points');
  });

  it('team', async () => {
    await mount(teamWithdrawal());
    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toContain('with what was fought kept');
    expect(text).not.toContain('replaced by the default win');
  });
});

// A pool match of a pools-then-knockout competition feeds the knockout through
// its standings, so the consequence adds that finishing it may change who
// qualifies, and that the save will show any knockout match that affects
// before anything is saved. A league pool (no knockout) and a knockout match
// do not say it.
describe('Clear withdrawal and reopen: pool match feeding a knockout', () => {
  it('a mixed competition pool match says finishing may change who qualifies', async () => {
    await mount(teamWithdrawal({ compFormat: 'mixed' }));
    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    expect(screen.getByTestId('clear-withdrawal-qualifier-note').textContent.replace(/\s+/g, ' '))
      .toBe('Finishing it may change who qualifies from Pool 1. If that moves someone who has already fought in the knockout, you will be shown which knockout matches it affects before anything is saved.');
  });

  it.each([
    ['a league pool match', () => teamWithdrawal({ compFormat: 'league' })],
    ['a knockout match', () => individualWithdrawal({ compFormat: 'mixed' })],
  ])('%s does not', async (_label, fixture) => {
    await mount(fixture());
    await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
    expect(screen.getByTestId('clear-withdrawal-consequence')).toBeTruthy();
    expect(screen.queryByTestId('clear-withdrawal-qualifier-note')).toBeNull();
  });
});
