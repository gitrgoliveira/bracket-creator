// bc-tmfn: Clear withdrawal and reopen (operator rulings 2026-09-24:
// "Everything should be able to be fixed, in case of a wrong entry. The
// operator just needs to be aware of the consequences, if it affects
// downstream matches."). Correcting a match a withdrawal ended shows what is
// recorded and ONE way to remove it, from one shared component
// (RecordedWithdrawal, admin_scoring_shared.jsx) in BOTH editors: a reopen in
// one tap, no reason asked (operator ruling 2026-09-25: a match can be
// reopened without any reason), with its consequence stated beside the button
// before it is tapped, never a score write. These pin the line, the
// request, the downstream confirm, the court-busy remedy, and where the
// control must not appear (kachinuki, which keeps its own Reopen; a match no
// withdrawal decided).

import React from 'react';
import { render, act, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { readStylesheet, cssBlock } from '../helpers/source.js';
import { DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED } from '../../write_result.jsx';
import { withdrawalLabel, STATUS_HOLD_MS } from '../../admin_scoring_shared.jsx';

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

async function clearWithdrawal() {
  await act(async () => { fireEvent.click(screen.getByTestId('clear-withdrawal-reopen')); });
}

describe.each([
  ['team', teamWithdrawal, 'Recorded: Kiken – Voluntary, Kyoto withdrew.', 'Kyoto'],
  ['individual', individualWithdrawal, 'Recorded: Fusenpai, Tanaka did not appear.', 'Tanaka'],
])('%s editor: Clear withdrawal and reopen', (_kind, fixture, recorded, withdrawn) => {
  it('says what is recorded and what clearing does, then reopens in one tap with no reason, never a score write', async () => {
    const onClose = vi.fn();
    await mountLive(fixture(), onClose);
    const line = screen.getByTestId('recorded-withdrawal');
    expect(line.textContent).toContain(recorded);
    // The consequence is stated before the tap: there is no confirm step to
    // state it in.
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent)
      .toContain(`${withdrawn} can compete again`);
    expect(window.API.reopenMatch).not.toHaveBeenCalled();

    await clearWithdrawal();
    expect(document.querySelector('.reason-prompt')).toBeNull();
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', fixture().id, 'secret', { reason: '', force: false },
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
      // As api_client's reopenFailureError marks it: met by a reopen.
      downstreamKnockoutPlayed: { matchId: fixture().id, blockingMatchId: 'm-r2-0', blockingMatches: [{ id: 'm-r2-0', number: 2 }], displaced: 'Yamada', reopen: true },
    });
    window.API.reopenMatch = vi.fn()
      .mockRejectedValueOnce(refusal)
      .mockResolvedValueOnce({ reopenedMatches: [{ id: 'm-r2-0', number: 2 }] });
    const onClose = vi.fn();
    await mountLive(fixture(), onClose);
    await clearWithdrawal();
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(2));
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('Match 2');
    // Reopen words, not a correction's: the operator is reopening this match.
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('Reopening this match also reopens Match 2');
    expect(window.confirmDialog.mock.calls[0][0].message.toLowerCase()).not.toContain('correction');
    expect(window.confirmDialog.mock.calls[0][0].confirmLabel).toBe('Reopen both');
    expect(window.API.reopenMatch.mock.calls[1][3]).toEqual({ reason: '', force: true });

    // bc-tmfn R3: the notice is read AFTER the match is running again, which
    // is when the operator looks for it. It used to live inside
    // RecordedWithdrawal, which unmounts on that flip, in an editor whose
    // host had already closed it.
    await act(async () => { flip(REOPENED); });
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('withdrawal-reopen-notice').textContent)
      .toBe('Match 2 was reopened: it must be fought and scored again.');

    // A new result on the same match (here another kiken) ends that reopen:
    // the notice described it, not the match now in front of the operator.
    await act(async () => { flip({ status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka' }); });
    expect(screen.queryByTestId('withdrawal-reopen-notice')).toBeNull();
  });

  it('declining the downstream confirm leaves everything as it was and says so', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('refused'), {
      downstreamKnockoutPlayed: { blockingMatchId: 'm-r2-0', blockingMatches: [{ id: 'm-r2-0', number: 2 }], reopen: true },
    }));
    const onClose = vi.fn();
    await mount(fixture(), { onClose });
    await clearWithdrawal();
    await waitFor(() => expect(screen.getByTestId('withdrawal-reopen-error').textContent).toBe(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED));
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a busy court offers the remedy, which carries no reason either', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('Court A already has a running match (m-blk).'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1' },
    ));
    await mount(fixture());
    await clearWithdrawal();
    await waitFor(() => expect(screen.getByTestId('withdrawal-reopen-conflict')).toBeTruthy());
    expect(screen.getByTestId('withdrawal-reopen-conflict-warning').textContent).toMatch(/keeps any score already entered/);
    await act(async () => { fireEvent.click(screen.getByTestId('withdrawal-reopen-requeue-button')); });
    expect(window.API.requeueBlockerAndReopen).toHaveBeenCalledWith(
      'comp1', fixture().id, 'comp1', 'm-blk', 'secret', { reason: '', force: false },
    );
  });

  // bc-rawm: the heading must never fall back to the raw internal matchId,
  // and the server's own raw refusal sentence (which named that same id and
  // told the operator to "finish that match before reopening" -- the
  // opposite of this panel's own button) is gone entirely.
  it('the conflict heading never falls back to the raw match id, and drops the server sentence', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('Court A already has a running match (m-blk). Finish that match before reopening this one.'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1' },
    ));
    await mount(fixture());
    await clearWithdrawal();
    const conflict = await waitFor(() => screen.getByTestId('withdrawal-reopen-conflict'));
    expect(conflict.textContent).not.toContain('m-blk');
    expect(conflict.textContent).toContain('Shiaijo A is running another match.');
    expect(conflict.textContent).not.toContain('Finish that match before reopening');
  });

  // bc-cse: a server-sent `label` for the blocking match (the court_busy
  // conflict reuses the score path's structured payload) names no
  // COMPETITOR ("Pool A · Match 2"), so it is only the FALLBACK when the
  // panel's own best-effort fetched blockerLabel has not resolved (still
  // loading here: fetchCompetitionDetails resolves null in this file's
  // default beforeEach, so the fetch effect never finds the blocker).
  it('falls back to the server label while the fetched one has not resolved', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('server sentence'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1', label: 'Pool A · Match 2' },
    ));
    await mount(fixture());
    await clearWithdrawal();
    const conflict = await waitFor(() => screen.getByTestId('withdrawal-reopen-conflict'));
    expect(conflict.textContent).toContain('Shiaijo A is running Pool A · Match 2.');
  });

  // bc-cse: once the fetch DOES resolve, the panel prefers it -- naming the
  // blocking match's COMPETITORS, the safety property the operator is about
  // to wipe the score of, which the server's own bare "Pool A · Match 2"
  // label never carries.
  it('prefers the fetched label (with competitor names) over the server label once it resolves', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(
      new Error('server sentence'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'comp1', label: 'Pool A · Match 2' },
    ));
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ id: 'comp1', config: {} });
    window.compMatchesForCompetition = () => [
      { id: 'm-blk', phase: 'pool', sideA: { name: 'Aoki Taro' }, sideB: { name: 'Endo Goro' } },
    ];
    await mount(fixture());
    await clearWithdrawal();
    const conflict = await waitFor(() => screen.getByTestId('withdrawal-reopen-conflict'));
    await waitFor(() => expect(conflict.textContent).toContain('Endo Goro vs Aoki Taro'));
    expect(conflict.textContent).not.toContain('Pool A · Match 2');
  });
});

// bc-tmfn R7: a kachinuki encounter a withdrawal decided reopens through the
// same RecordedWithdrawal as every other editor (the line, the consequence,
// the one-tap clear), because that reopen also makes the withdrawn team eligible
// again; its plain one-tap Reopen stays for every other kachinuki result. One
// control, one consequence text, and the kachinuki feedback ids still apply.
describe('kachinuki: a withdrawal reopens through Clear withdrawal and reopen', () => {
  const kachinukiWithdrawal = (o = {}) => teamWithdrawal({ teamMatchType: 'kachinuki', compFormat: 'mixed', ...o });

  it('shows the recorded line in place of the one-tap Reopen, and states the consequence first', async () => {
    const onClose = vi.fn();
    await mountLive(kachinukiWithdrawal(), onClose);
    expect(screen.getByTestId('recorded-withdrawal').textContent).toContain('Recorded: Kiken – Voluntary, Kyoto withdrew.');
    expect(screen.queryByTestId('kachinuki-reopen-button')).toBeNull();

    expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('Kyoto can compete again');
    expect(window.API.reopenMatch).not.toHaveBeenCalled();
    await clearWithdrawal();
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', 'm-pool-1', 'secret', { reason: '', force: false },
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
    await clearWithdrawal();
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
    await clearWithdrawal();
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
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toContain("Yamada's points were replaced by the default win when the withdrawal was recorded, so enter them again");
    expect(text).toContain("Tanaka's points are kept");
    expect(text).not.toContain('with what was fought kept');
  });

  it('individual fusenpai: a no-show fought nothing, so only the default win goes', async () => {
    await mount(individualWithdrawal());
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toContain('the default win given to Yamada is removed, and Tanaka can compete again');
    expect(text).not.toContain('points');
  });

  it('team', async () => {
    await mount(teamWithdrawal());
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
    expect(screen.getByTestId('clear-withdrawal-qualifier-note').textContent.replace(/\s+/g, ' '))
      .toBe('Finishing it may change who qualifies from Pool 1. If that moves someone who has already fought in the knockout, you will be shown which knockout matches it affects before anything is saved.');
  });

  it.each([
    ['a league pool match', () => teamWithdrawal({ compFormat: 'league' })],
    ['a knockout match', () => individualWithdrawal({ compFormat: 'mixed' })],
  ])('%s does not', async (_label, fixture) => {
    await mount(fixture());
    expect(screen.getByTestId('clear-withdrawal-consequence')).toBeTruthy();
    expect(screen.queryByTestId('clear-withdrawal-qualifier-note')).toBeNull();
  });
});

// bc-rawm: withdrawalInForce widened to the match-level DEFAULT-WIN class
// (RemainingMatchesPanel.award writes a whole-match `decision: "fusensho"`
// for a match against a competitor already withdrawn elsewhere). Same
// RecordedWithdrawal door as kiken/fusenpai, different copy: the Recorded
// line names the winner, and the reopen button reads "Clear default win and
// reopen".
describe('a match-level fusensho default win (bc-rawm)', () => {
  // Yamada (aka) got the default win when Tanaka (shiro) withdrew elsewhere
  // and RemainingMatchesPanel awarded this still-scheduled match to Yamada.
  const defaultWin = (o = {}) => individualWithdrawal({
    decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Tanaka withdrawn',
    ipponsA: ['○', '○'], ipponsB: [],
    ...o,
  });

  it('names the winner in the Recorded line and offers "Clear default win"', async () => {
    await mount(defaultWin());
    const line = screen.getByTestId('recorded-withdrawal');
    expect(line.textContent).toContain('Recorded: Default win (fusensho) for Yamada. Tanaka had withdrawn.');
    const button = screen.getByTestId('clear-withdrawal-reopen');
    // bc-cse: no "and reopen" -- a fusensho reopen does not always land
    // running any more (see the consequence copy test below).
    expect(button.textContent).toBe('Clear default win');
  });

  // bc-cse: the server returns the match to SCHEDULED, not running, when
  // the barred competitor (Tanaka) is still barred -- almost always true,
  // since whatever barred them elsewhere is still on record -- so the
  // consequence says that plainly rather than promising "it goes back to
  // running". No fetchCompetitorStatuses stub here (the untouched default
  // API mock), so the reinstate clause is correctly absent: unknown reads
  // as "not reinstateable", never as a promise that may not hold.
  it('the reopen consequence says the match queues again and the default win must be re-recorded', async () => {
    await mount(defaultWin());
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toBe('The match goes back to the queue. Tanaka is still withdrawn, so record the default win again.');
    expect(text).not.toContain('reinstate');
    expect(text).not.toContain('goes back to running');
  });

  // bc-cse: when a live check finds Tanaka reinstateable (kiken-injury,
  // still ineligible), the consequence ALSO offers that route.
  it('offers the reinstate route too when the barred competitor is still ineligible and reinstateable', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: false, reinstateable: true },
    ]);
    await mount(defaultWin());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('reinstate'));
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toBe('The match goes back to the queue. Tanaka is still withdrawn, so record the default win again, or reinstate Tanaka first to fight it.');
  });

  // Not reinstateable (e.g. kiken-voluntary) or no longer barred: no
  // reinstate clause either way -- only kiken-injury/still-ineligible earns it.
  it('does not offer reinstate when the status says it is not reinstateable', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: false, reinstateable: false },
    ]);
    await mount(defaultWin());
    await waitFor(() => expect(window.API.fetchCompetitorStatuses).toHaveBeenCalled());
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).not.toContain('reinstate');
  });

  // bc-cse: reinstated, or their earlier withdrawal cleared elsewhere --
  // either way the barred competitor's own status now reads eligible, so
  // the server reopens THIS match straight to running rather than the
  // queue, and the copy says that instead of promising the queue and a
  // re-recorded default win.
  it('says the match reopens in progress when the barred competitor is eligible again', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: true },
    ]);
    await mount(defaultWin());
    await waitFor(() => expect(window.API.fetchCompetitorStatuses).toHaveBeenCalled());
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toBe('Tanaka can fight again, so the match reopens in progress. Score it and finish it as usual.');
    expect(text).not.toContain('goes back to the queue');
    expect(text).not.toContain('reinstate');
  });

  it('clears in one tap through the same door as kiken/fusenpai, with no reason', async () => {
    const onClose = vi.fn();
    await mountLive(defaultWin(), onClose);
    await clearWithdrawal();
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', defaultWin().id, 'secret', { reason: '', force: false },
    );
    await act(async () => { flip(REOPENED); });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByTestId('recorded-withdrawal')).toBeNull();
  });

  // withdrawalLabel's short "fact" form, for the team-summary-decision slot
  // (admin_scoring_team.jsx) and anywhere else that needs a label rather
  // than the full winner-naming sentence RecordedWithdrawal composes itself.
  it('withdrawalLabel gives the short "fact" form', () => {
    expect(withdrawalLabel('fusensho')).toBe('Default win (fusensho)');
  });
});

// bc-cse: clearing THIS withdrawal never touches a LATER match the same
// withdrawn competitor's default win (RemainingMatchesPanel.award) already
// closed -- that match keeps its own recorded result. The consequence lists it
// as a consequence (a warning, not a lock, per the 2026-09-24 ruling): the
// operator can still reopen it separately to fight it. Fetched the same way
// RemainingMatchesPanel finds Tanaka/Kyoto's other matches.
describe('Clear withdrawal and reopen: a later default win from the same withdrawal (bc-cse)', () => {
  it('lists a later default win as a consequence, named by its scores-list label plus the pairing', async () => {
    const laterMatch = {
      id: 'Pool 2-0', compId: 'comp1', phase: 'pool', poolName: 'Pool 2', status: 'completed', decision: 'fusensho', decisionBy: 'shiro',
      sideA: { id: 'team-osaka', name: 'Osaka' }, sideB: { id: 'team-kyoto', name: 'Kyoto' },
    };
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ id: 'comp1', config: {} });
    window.compMatchesForCompetition = vi.fn().mockReturnValue([laterMatch]);
    await mount(teamWithdrawal());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-default-win-consequences')).toBeTruthy());
    const line = screen.getByTestId(`clear-withdrawal-default-win-${laterMatch.id}`);
    // bc-cse: named by scoreRowMatchLabel FIRST -- a pairing alone cannot be
    // found in the scores list, which is where the operator has to go to
    // reopen it -- joined to the pairing the same way ReopenFeedback's own
    // fetched blockerLabel does ("<lead> · <pairing>").
    expect(line.textContent).toBe('Pool 2 · Match 1 · Kyoto vs Osaka keeps its default win; reopen it to fight it.');
  });

  // bc-cse: a match carrying no number at all (drawn before numbering
  // existed, or a supplementary DH/TB bout) falls back to the bare pairing
  // scoreRowMatchLabel's own doc describes -- never a raw internal id.
  it('falls back to the bare pairing when the later match carries no number', async () => {
    const laterMatch = {
      id: 'Pool 2-DH-0', compId: 'comp1', phase: 'pool', poolName: 'Pool 2', status: 'completed', decision: 'fusensho', decisionBy: 'shiro',
      sideA: { id: 'team-osaka', name: 'Osaka' }, sideB: { id: 'team-kyoto', name: 'Kyoto' },
    };
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ id: 'comp1', config: {} });
    window.compMatchesForCompetition = vi.fn().mockReturnValue([laterMatch]);
    await mount(teamWithdrawal());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-default-win-consequences')).toBeTruthy());
    const line = screen.getByTestId(`clear-withdrawal-default-win-${laterMatch.id}`);
    expect(line.textContent).toBe('Kyoto vs Osaka keeps its default win; reopen it to fight it.');
    expect(line.textContent).not.toContain('Pool 2-DH-0');
  });

  it('a match already carrying a fusensho for a DIFFERENT competitor is not listed', async () => {
    const unrelated = {
      id: 'm-r2-1', compId: 'comp1', status: 'completed', decision: 'fusensho', decisionBy: 'aka',
      sideA: { id: 'team-nagoya', name: 'Nagoya' }, sideB: { id: 'team-sendai', name: 'Sendai' },
    };
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ id: 'comp1', config: {} });
    window.compMatchesForCompetition = vi.fn().mockReturnValue([unrelated]);
    await mount(teamWithdrawal());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-consequence')).toBeTruthy());
    expect(screen.queryByTestId('clear-withdrawal-default-win-consequences')).toBeNull();
  });

  it('omits the block when there is no such later match', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ id: 'comp1', config: {} });
    window.compMatchesForCompetition = vi.fn().mockReturnValue([]);
    await mount(teamWithdrawal());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-consequence')).toBeTruthy());
    expect(screen.queryByTestId('clear-withdrawal-default-win-consequences')).toBeNull();
  });
});

// bc-kfup: a fusenpai recorded on a remaining match of a competitor who had
// already withdrawn elsewhere chains onto that bar and records no status of
// its own. Clearing it restores nobody, so the server sends the match back
// to the queue while the bar holds (engine.reopenTargetStatus), the same as
// a fusensho. The editor tells the two fusenpai apart by the competitor's
// status record: an ordinary fusenpai's names THIS match.
describe('clearing a fusenpai chained onto an earlier withdrawal (bc-kfup)', () => {
  it('says the match goes back to the queue, not that the competitor can compete again', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: false, matchId: 'Pool A-0' },
    ]);
    await mount(individualWithdrawal());
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-reopen').textContent).toBe('Clear default win'));
    const text = screen.getByTestId('clear-withdrawal-consequence').textContent;
    expect(text).toBe('The match goes back to the queue. Tanaka is still withdrawn, so record the default win again.');
    expect(text).not.toContain('can compete again');
  });

  it('keeps the ordinary copy for a fusenpai that barred the competitor itself', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: false, matchId: 'm-r1-0' },
    ]);
    await mount(individualWithdrawal());
    await waitFor(() => expect(window.API.fetchCompetitorStatuses).toHaveBeenCalled());
    expect(screen.getByTestId('clear-withdrawal-reopen').textContent).toBe('Clear withdrawal and reopen');
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('can compete again');
  });

  // Review finding: a kiken whose competitor was reinstated and then withdrew
  // again elsewhere no longer names this match, so its reopen also goes to
  // the queue. The copy must not promise "back to running".
  it('a kiken whose competitor is barred by another match also says the match goes back to the queue', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([
      { playerId: 'p2', eligible: false, matchId: 'Pool A-5' },
    ]);
    await mount(individualWithdrawal({ decision: 'kiken-voluntary' }));
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-reopen').textContent).toBe('Clear withdrawal'));
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('goes back to the queue');
  });

  // Review finding: the clear is one tap, so it waits for the status that
  // decides its copy rather than acting under the wrong promise.
  it('holds the clear until the competitor status is in', async () => {
    let resolve;
    window.API.fetchCompetitorStatuses = vi.fn().mockImplementation(() => new Promise((r) => { resolve = r; }));
    await mount(individualWithdrawal());
    expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(true);
    await act(async () => { resolve([{ playerId: 'p2', eligible: false, matchId: 'm-r1-0' }]); });
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(false));
  });

  // The status fetch is best-effort with no timeout. A request that hangs
  // must not keep the correction disabled: the hold is capped.
  it('releases the clear after a short cap when the status request hangs', async () => {
    window.API.fetchCompetitorStatuses = vi.fn().mockImplementation(() => new Promise(() => {}));
    await mount(individualWithdrawal());
    expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(true);
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(false),
      { timeout: STATUS_HOLD_MS + 1500 });
  });

  it("lists a chained fusenpai among the original withdrawal's later default wins", async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ config: {} });
    window.compMatchesForCompetition = () => [{
      id: 'Pool A-3', compId: 'comp1', status: 'completed', phase: 'pool', poolName: 'Pool A', matchNumber: 3,
      decision: 'fusenpai', decisionBy: 'aka',
      sideA: { id: 'p2', name: 'Tanaka' }, sideB: { id: 'p3', name: 'Suzuki' },
    }];
    try {
      await mount(individualWithdrawal({ decision: 'kiken-voluntary' }));
      await waitFor(() => expect(screen.getByTestId('clear-withdrawal-default-win-Pool A-3')).toBeTruthy());
      expect(screen.getByTestId('clear-withdrawal-default-win-Pool A-3').textContent).toContain('keeps its default win');
      // Clearing this kiken moves the bar onto that no-show (the server's
      // standingWithdrawalOf), so the match goes to the queue, not the court.
      expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toBe(
        'The match goes back to the queue. Tanaka also did not appear for a later match, listed below, so they are still withdrawn because of it.');
      expect(screen.getByTestId('clear-withdrawal-reopen').textContent).toBe('Clear withdrawal');
    } finally {
      window.compMatchesForCompetition = STUBBED_GLOBALS.compMatchesForCompetition;
    }
  });

  // A chained no-show in the later list changes the copy too, so the clear
  // waits for that list as well as for the status.
  it('holds the clear until the later matches are in', async () => {
    // The editor fetches the competition from more than one place; release
    // every call.
    const pending = [];
    window.API.fetchCompetitorStatuses = vi.fn().mockResolvedValue([{ playerId: 'p2', eligible: false, matchId: 'm-r1-0' }]);
    window.API.fetchCompetitionDetails = vi.fn().mockImplementation(() => new Promise((r) => { pending.push(r); }));
    await mount(individualWithdrawal({ decision: 'kiken-voluntary' }));
    await waitFor(() => expect(window.API.fetchCompetitorStatuses).toHaveBeenCalled());
    expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(true);
    await act(async () => { pending.forEach((r) => r({ config: {} })); });
    await waitFor(() => expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(false));
  });
});
