import React from 'react';
import { render, fireEvent, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// OPERATOR RULING: a scoring result must read the SAME on every surface, and
// reopening a match to edit changes only that match's result, for every
// surface asking for it.
//
// The score editor is one of those surfaces. While it is open, the viewer
// card, the bracket, the TV board, the lobby and the Excel export are all
// already showing whatever the server holds — so an editor showing something
// else is a divergence, whatever its reason.
//
// That makes a verdict recorded on ANOTHER device the interesting case. The
// editor reads its match from a live prop, so SSE delivers it mid-edit. Two
// wrong answers were tried before this one:
//
//   1. Recompute from the live prop but keep the local armed state frozen.
//      The panel still showed "Decide by hantei…", and `hanteiClear` turned
//      the live true into an authoritative `decidedByHantei: false` on the
//      next write — deleting a verdict the operator was never shown.
//   2. Freeze the flag at mount so the write goes silent. That stopped the
//      deletion by accepting the divergence: the editor sat showing no
//      verdict while every other screen showed the Ht.
//
// The answer is to ADOPT: follow the server's verdict, so there is one result
// and one reading of it. An explicit `false` from this editor is then always
// the operator ruling on something in front of them.

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
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
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

// A running 1-1 match: tied, so it is exactly the scoreline a hantei is taken
// from, and scored, so Finish is enabled.
function tiedRunningMatch(overrides = {}) {
  return {
    id: 'm1',
    status: 'running',
    phase: 'knockout',
    round: 'Semi-final',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },   // AKA
    sideB: { id: 'p2', name: 'Tanaka' },   // SHIRO
    ipponsA: ['M'],
    ipponsB: ['K'],
    hansokuA: 0,
    hansokuB: 0,
    ...overrides,
  };
}

// Yamada (sideA / AKA) wins by hantei, as another device recorded it.
const withVerdict = () => tiedRunningMatch({
  decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' },
});


// Finish is a two-tap arm-then-confirm on a non-complete match. Both taps go
// through act(): doSubmit is async and lands setState after the await, which
// the render harness fails on as an unwrapped update.
async function finish(container) {
  const btn = [...container.querySelectorAll('button')]
    .find(b => /Finish|Tap again/.test(b.textContent));
  expect(btn).toBeTruthy();
  await act(async () => { fireEvent.click(btn); });
  await act(async () => { fireEvent.click(btn); });
  return btn;
}

describe('the editor shows the verdict the server holds', () => {
  it('adopts one recorded on another device, mid-edit', async () => {
    const onSubmit = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    // Before: no verdict anywhere, so the editor offers to record one.
    expect(screen.queryByTestId('scoring-modal-hantei-arm')).toBeTruthy();

    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    ); });

    // After: the arm button is gone and the verdict row is showing, with the
    // recorded side marked — the same result the viewer and the board show.
    expect(screen.queryByTestId('scoring-modal-hantei-arm')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-hantei-aka').className).toContain('btn--primary');
    expect(screen.queryByTestId('scoring-modal-hantei-shiro').className).not.toContain('btn--primary');
    // And the Ht rides in the winner's slot, as it does on every other surface.
    expect(container.textContent).toContain('Ht');
  });

  it('adopts the SCORE too, so the verdict lands in the right slot', async () => {
    // Every ippon slot is local state seeded at mount, so a stale scoreline
    // outlived an SSE update just as the verdict did — and the two together
    // were worse than either alone: adopting a verdict onto a 0-0 local board
    // put the Ht in the slot the OLD score left free, so the editor showed
    // `Ht` at 0-0 against a stored 1-1. Verified in the browser before the fix.
    const { rerender, container } = render(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: [], ipponsB: [] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    );
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent))
      .toEqual(['\u00b7', '\u00b7', '\u00b7', '\u00b7']);

    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={vi.fn()} password="" />
    ); });

    // SHIRO's two cells then AKA's, in DOM order. Yamada (AKA) holds the M and
    // the Ht, Tanaka (SHIRO) the K — the same result the board shows, which is
    // the whole point. (DOM order, not reading order: AKA's pair is reversed
    // visually by CSS, so on screen this reads `[K][ ] vs [Ht][M]`. Confirmed
    // against the running app, which produces exactly this array.)
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent))
      .toEqual(['K', '\u00b7', 'M', 'Ht']);
  });

  it('keeps UNSAVED operator edits rather than overwriting them', async () => {
    // The limit of the rule: an editor with work in it is not refreshed out
    // from under the operator. Their edits are not ours to discard — and note
    // the server ORDERS stamped writes but does not ARBITRATE a two-operator
    // conflict: whoever saves last wins, deliberately, because more than one
    // person may legitimately be scoring a court. So this keeps a REAL conflict
    // visible to the human who owns it rather than resolving it silently.
    const { rerender, container } = render(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: [], ipponsB: [] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    );
    // The operator awards AKA a men.
    const akaMen = [...container.querySelectorAll('button')].filter(b => b.textContent === 'M')[1];
    await act(async () => { fireEvent.click(akaMen); });
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent)).toContain('M');

    await act(async () => { rerender(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: ['K', 'K'], ipponsB: ['D'] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    ); });
    const cells = [...container.querySelectorAll('.sb-slot')].map(s => s.textContent);
    expect(cells, 'the operator\u2019s unsaved men must survive').toContain('M');
    expect(cells).not.toContain('D');
  });

  it('cannot be finished past a verdict without ruling on it', async () => {
    // The adopted verdict locks scoring: Finish is disabled while it stands,
    // so no write can quietly land a different result over it.
    const onSubmit = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    ); });

    const btn = await finish(container);
    expect(btn.disabled).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('adopting is not an unsaved change of the operators', async () => {
    // isDirty compares the verdict against what the SERVER holds, so adoption
    // moves both sides together. Closing an untouched editor must not prompt:
    // a spurious "discard unsaved scoring changes?" trains operators to
    // dismiss the one prompt that protects real work.
    const onClose = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={onClose} onSubmit={vi.fn()} password="" />
    );
    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={onClose} onSubmit={vi.fn()} password="" />
    ); });

    const cancel = [...container.querySelectorAll('button')]
      .find(b => b.textContent.trim() === 'Cancel' && !b.dataset.testid);
    await act(async () => { fireEvent.click(cancel); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('an operator who cancels the verdict on screen does clear it', async () => {
    // The other half, so adoption is not a blanket mute: the explicit false
    // still travels, because now it is always a ruling on a displayed result.
    const onSubmit = vi.fn();
    const { container } = render(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    await act(async () => {
      fireEvent.click(screen.queryByTestId('scoring-modal-hantei-cancel'));
    });
    await finish(container);
    expect(onSubmit.mock.calls.at(-1)[0].decidedByHantei).toBe(false);
  });
});

// The same ruling, one level down: the TEAM editor's daihyosen verdict.
//
// It had the identical divergence, from an identical mount-frozen pair
// (initialDaihyosenHantei / initialDaihyosenHanteiArmed), and the freeze was
// what buildPatch's `hanteiKnown` guard existed to compensate for. Adopting
// removes the divergence and the guard together, so this pins the display
// half: the rep-bout panel must follow the stored verdict.
describe('the team editor shows the daihyosen verdict the server holds', () => {
  const DH = -1;
  function teamMatch(subs) {
    return {
      id: 'tm1',
      status: 'running',
      phase: 'knockout',
      round: 'Final',
      court: 'A',
      compKind: 'team',
      teamSize: 3,
      sideA: { id: 'teamA', name: 'Kyoto' },
      sideB: { id: 'teamB', name: 'Osaka' },
      subResults: subs,
    };
  }
  // A tied rep bout, first with no verdict and then decided for Kyoto.
  const bout = extra => ({
    position: DH, sideA: 'Kyoto', sideB: 'Osaka',
    ipponsA: ['M'], ipponsB: ['K'], decision: 'daihyosen', ...extra,
  });

  it('adopts a verdict recorded on another device', async () => {
    const { rerender, container } = render(
      <ScoreEditorModal match={teamMatch([bout()])} onClose={vi.fn()} onSubmit={vi.fn()} password="" />
    );
    const armed = c => /Cancel hantei|SHIRO wins|AKA wins/.test(c.textContent);
    expect(armed(container)).toBe(false);

    await act(async () => { rerender(
      <ScoreEditorModal
        match={teamMatch([bout({ decidedByHantei: true, winner: 'Kyoto' })])}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    ); });

    expect(armed(container), 'the rep-bout panel must show the stored verdict').toBe(true);
  });
});

// bc-tsub. The same rule again, for the half of the team editor the daihyosen
// work did not reach: the NUMBERED bouts. Their rows were seeded once at mount
// and the only path that revisited them appended kachinuki growth, so a bout
// scored on another device never appeared — and the damage is not only on
// screen. buildPatch sends the FULL subResults snapshot, so the operator's next
// write carries the stale row out over the newer one.
//
// The seed baseline was a ref frozen at mount, which is why "just gate a
// re-seed on isDirty" does not work and is pinned below: nothing ever re-based
// it, so a single tap left the editor dirty for the rest of its life, even once
// its own autosave had landed and the server agreed. The gate would have been
// shut from the operator's first tap onward — which is precisely when another
// device's result turns up.
describe('the team editor shows the bout scoreline the server holds', () => {
  const KYOTO = 'Kyoto', OSAKA = 'Osaka';
  // A fixed-order (non-kachinuki) 3-person encounter: `positions` is pinned at
  // teamSize, so scoring a bout does NOT change the row count. That keeps these
  // tests on the re-seed itself, away from the growth path and away from the
  // subResults.length remount key two mount sites carry.
  function teamMatch(subs, extra = {}) {
    return {
      id: 'tm2',
      status: 'running',
      phase: 'pool',
      court: 'A',
      compKind: 'team',
      teamSize: 3,
      sideA: { id: 'teamA', name: KYOTO },
      sideB: { id: 'teamB', name: OSAKA },
      subResults: subs,
      ...extra,
    };
  }
  // One decided bout: `winnerSide` takes both ippons, so it is worth IV 1, PW 2.
  const bout = (position, winnerSide) => ({
    position,
    sideA: KYOTO,
    sideB: OSAKA,
    ipponsA: winnerSide === 'a' ? ['M', 'K'] : [],
    ipponsB: winnerSide === 'b' ? ['M', 'K'] : [],
  });
  const open = (match, onSubmit = vi.fn()) =>
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
  // onSubmit is threaded so a test can keep ONE spy across the re-render and
  // then read the patch the editor actually emits; the default keeps the
  // display-only tests terse.
  const reopenWith = async (rerender, match, onSubmit = vi.fn()) => {
    await act(async () => { rerender(
      <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    ); });
  };
  // Read the IV/PW strip STRUCTURALLY, one entry per side. A whole-container
  // substring check cannot say which side a total belongs to, passes for any
  // reason the string is absent (including a render that threw the strip away),
  // and matches a prefix of a larger number — so `not.toContain('PW: 2')` was
  // three different ways of not testing this.
  const AKA = 'AKA (Red)', SHIRO = 'SHIRO (White)';
  const totalsBySide = (container) => Object.fromEntries(
    [...container.querySelectorAll('.team-summary__side')]
      .map(el => [
        el.querySelector('.team-summary__label')?.textContent?.trim(),
        el.querySelector('.team-summary__stats')?.textContent?.trim(),
      ])
      .filter(([label, stats]) => label && stats)
  );
  // Award one ippon to Kyoto on bout 2, through the real control. Bout rows
  // render Shiro's buttons then Aka's; Kyoto is sideA (AKA), so its buttons are
  // the second group in the row.
  const scoreOneForKyoto = async (container) => {
    const rows = [...container.querySelectorAll('.team-sub-match')];
    const akaButtons = [...rows[1].querySelectorAll('button')].filter(b => b.textContent.trim() === 'M');
    expect(akaButtons.length, 'each side offers an M button').toBe(2);
    await act(async () => { fireEvent.click(akaButtons[1]); });
  };

  it('adopts a bout scored on another device', async () => {
    const { rerender, container } = open(teamMatch([]));
    expect(totalsBySide(container)).toEqual({ [AKA]: 'IV: 0 · PW: 0', [SHIRO]: 'IV: 0 · PW: 0' });

    await reopenWith(rerender, teamMatch([bout(1, 'a')]));

    expect(totalsBySide(container), "Kyoto's bout must appear on Kyoto's side")
      .toEqual({ [AKA]: 'IV: 1 · PW: 2', [SHIRO]: 'IV: 0 · PW: 0' });
  });

  it('keeps the operator\'s UNSAVED row and still takes the one they never touched', async () => {
    const { rerender, container } = open(teamMatch([]));
    await scoreOneForKyoto(container);
    expect(totalsBySide(container), "the operator's own ippon shows").toEqual({
      [AKA]: 'IV: 1 · PW: 1', [SHIRO]: 'IV: 0 · PW: 0',
    });

    // Device B records bout 1 — a DIFFERENT row from the one being edited.
    await reopenWith(rerender, teamMatch([bout(1, 'b')]));

    // Both must hold. Their bout-2 edit is not ours to discard, and bout 1 is
    // not theirs to overwrite: an all-or-nothing gate keeps the first and
    // loses the second, and because the next write is a full snapshot, losing
    // it means blanking it on the server.
    expect(totalsBySide(container)).toEqual({
      [AKA]: 'IV: 1 · PW: 1',     // the operator's unsaved bout 2
      [SHIRO]: 'IV: 1 · PW: 2',   // Osaka's bout 1, adopted
    });
  });

  it('does not blank a bout recorded elsewhere when the operator saves', async () => {
    const onSubmit = vi.fn();
    const { rerender, container } = open(teamMatch([]), onSubmit);
    await scoreOneForKyoto(container);
    await reopenWith(rerender, teamMatch([bout(1, 'b')]), onSubmit);

    const finish = () => [...container.querySelectorAll('button')].find(b => /finish/i.test(b.textContent));
    for (let i = 0; i < 2 && finish(); i++) { await act(async () => { fireEvent.click(finish()); }); }

    // The display half is not the whole rule: buildPatch sends the FULL board,
    // so a row the editor dropped is a row it overwrites.
    const b1 = (onSubmit.mock.calls[0]?.[0]?.subResults || []).find(s => s.position === 1);
    expect(b1 && { ipponsA: b1.ipponsA, ipponsB: b1.ipponsB, decision: b1.decision })
      .toEqual({ ipponsA: [], ipponsB: ['M', 'K'], decision: '' });
  });

  it('adopting is not an unsaved change of the operators', async () => {
    window.confirmDialog.mockClear();
    const { rerender } = open(teamMatch([]));
    await reopenWith(rerender, teamMatch([bout(1, 'a')]));

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-root')); });

    // A discard prompt on an editor nobody touched trains operators to dismiss
    // the one prompt that protects real work.
    expect(window.confirmDialog, 'no discard prompt for a result we merely adopted').not.toHaveBeenCalled();
  });

  it('still adopts after the operator has saved an edit of their own', async () => {
    const { rerender, container } = open(teamMatch([]));
    await scoreOneForKyoto(container);

    // Their autosave lands: the server now holds exactly what they typed, so
    // nothing is unsaved any more. With a mount-frozen baseline the editor
    // stays dirty here forever, and every later adopt is skipped.
    await reopenWith(rerender, teamMatch([{ ...bout(2, 'a'), ipponsA: ['M'] }]));

    // Device B then scores bout 1. This must still arrive.
    await reopenWith(rerender, teamMatch([
      { ...bout(2, 'a'), ipponsA: ['M'] },
      bout(1, 'b'),
    ]));

    expect(totalsBySide(container), "Osaka's bout must arrive after the operator's own save")
      .toEqual({ [AKA]: 'IV: 1 · PW: 1', [SHIRO]: 'IV: 1 · PW: 2' });
  });

  it('adopts overtime recorded on another device', async () => {
    const { rerender, container } = open(teamMatch([]));
    // "Overtime" alone is the encho control's own label and is always present;
    // the header eyebrow carries the COUNT, which is the server value.
    expect(container.textContent).not.toContain('Overtime ×');

    await reopenWith(rerender, teamMatch([], { encho: { periodCount: 1 } }));

    expect(container.textContent, 'the overtime count is a server value too').toContain('Overtime ×1');
  });

  it('follows a daihyosen DELETED on another device, without stranding its row', async () => {
    // The mirror of the growth case, and the one that used to go wrong in a way
    // no display test could see. `positions` shrinks, but the local board only
    // ever grew, so it kept a fourth row — and buildPatch derives each row's
    // wire position from its INDEX, so the stranded rep bout went out as
    // `position: 4`, a fourth numbered bout in a three-person team.
    const onSubmit = vi.fn();
    const dh = { position: -1, sideA: KYOTO, sideB: OSAKA, ipponsA: ['M'], ipponsB: ['K'], decision: 'daihyosen' };
    const { rerender, container } = open(teamMatch([bout(1, 'a'), dh]), onSubmit);
    await scoreOneForKyoto(container);   // dirty, so the adopt cannot paper over it

    await reopenWith(rerender, teamMatch([bout(1, 'a')]), onSubmit);

    const finishBtn = () => [...container.querySelectorAll('button')].find(b => /finish/i.test(b.textContent));
    for (let i = 0; i < 2 && finishBtn(); i++) { await act(async () => { fireEvent.click(finishBtn()); }); }
    expect((onSubmit.mock.calls[0]?.[0]?.subResults || []).map(s => s.position),
      'a 3-person team never has a bout 4').toEqual([1, 2, 3]);
  });

  it('keeps following the server after a shrink arrives mid-edit', async () => {
    // The same stranded row also wedged adoption: a board one row longer than
    // `positions` can never equal it, so isDirty was structurally true forever
    // and every later adopt was skipped for the life of the editor.
    const dh = { position: -1, sideA: KYOTO, sideB: OSAKA, ipponsA: ['M'], ipponsB: ['K'], decision: 'daihyosen' };
    const { rerender, container } = open(teamMatch([bout(1, 'a'), dh]));
    await scoreOneForKyoto(container);
    await reopenWith(rerender, teamMatch([bout(1, 'a')]));          // remote DH delete

    await reopenWith(rerender, teamMatch([bout(1, 'a'), bout(3, 'b')])); // later remote bout

    expect(totalsBySide(container)[SHIRO], "Osaka's later bout must still arrive")
      .toBe('IV: 1 · PW: 2');
  });

  it('lets the operator score a row appended while they were mid-edit', async () => {
    // Covering the growth in the RENDER is only half of it: the row also has to
    // be COMMITTED to state, or updateSub writes to an index the committed
    // array does not have and the operator's tap does nothing. Asserted on the
    // emitted patch, since the rep bout is excluded from IV/PW by design and so
    // leaves no trace in the summary strip.
    const onSubmit = vi.fn();
    const { rerender, container } = open(teamMatch([bout(1, 'a'), bout(2, 'b')]), onSubmit);
    await reopenWith(rerender, teamMatch([
      bout(1, 'a'), bout(2, 'b'),
      { position: -1, sideA: KYOTO, sideB: OSAKA, ipponsA: [], ipponsB: [], decision: 'daihyosen' },
    ]), onSubmit);

    const rows = [...container.querySelectorAll('.team-sub-match')];
    const akaM = [...rows[rows.length - 1].querySelectorAll('button')].filter(b => b.textContent.trim() === 'M');
    await act(async () => { fireEvent.click(akaM[1]); });

    const finishBtn = () => [...container.querySelectorAll('button')].find(b => /finish/i.test(b.textContent));
    for (let i = 0; i < 2 && finishBtn(); i++) { await act(async () => { fireEvent.click(finishBtn()); }); }
    const dhRow = (onSubmit.mock.calls[0]?.[0]?.subResults || []).find(s => s.position === -1);
    expect(dhRow?.ipponsA, 'the appended rep bout must accept a score').toEqual(['M']);
  });

  it('does not close an open correction prompt when another device scores', async () => {
    // ReasonPrompt holds the half-typed note in its OWN state, so closing it
    // discards what the operator wrote. The disarm effect used to key on the
    // bout array, which the editor now changes itself whenever it follows the
    // server — turning every remote score into a discarded audit note.
    const completed = teamMatch([bout(1, 'a'), bout(2, 'a'), bout(3, 'b')], { status: 'completed', winner: KYOTO });
    const { rerender, container } = open(completed);
    // On a completed match the primary action is "Save correction", and it
    // opens the prompt in ONE tap: the prompt's own Confirm is the commit.
    const saveBtn = [...container.querySelectorAll('button')].find(b => /save correction/i.test(b.textContent));
    await act(async () => { fireEvent.click(saveBtn); });
    expect(container.querySelector('.reason-prompt'), 'the prompt must be open to start').toBeTruthy();

    await reopenWith(rerender, teamMatch(
      [bout(1, 'a'), bout(2, 'a'), bout(3, 'b'), { position: 4, sideA: KYOTO, sideB: OSAKA, ipponsA: [], ipponsB: ['M'] }],
      { status: 'completed', winner: KYOTO },
    ));

    expect(container.querySelector('.reason-prompt'), 'a remote score is not the operator changing their mind').toBeTruthy();
  });

  it('covers a daihyosen added on another device without being remounted', async () => {
    const { rerender } = open(teamMatch([bout(1, 'a'), bout(2, 'b')]));

    // Adding a rep bout grows `positions` by one. The row list has to cover it
    // in the SAME render that first indexes it: subTotals[daihyosenIdx] is read
    // unconditionally once hasDaihyosen flips. Two mount sites paper over this
    // by keying the editor on subResults.length so it remounts, which throws
    // away whatever the operator had not saved.
    await reopenWith(rerender, teamMatch([
      bout(1, 'a'), bout(2, 'b'),
      { position: -1, sideA: KYOTO, sideB: OSAKA, ipponsA: ['M'], ipponsB: ['K'], decision: 'daihyosen' },
    ]));

    expect(screen.queryByTestId('team-daihyosen-hantei-row'), 'the rep bout must render').toBeTruthy();
  });
});
