// bc-evts stage 5: the team editor scores a RUNNING match by taps, one render
// test per event kind. Every scoring gesture is one event handed to the outbox
// (API.recordTap) in the tick it is made, nothing waits on it and nothing else
// is written while the match runs; the sheet shows the board, the server's
// copy with this device's pending taps folded on (tapBoardOf with the team
// fold), so a tap shows at once and a push carrying it never counts it twice.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, teamBout, kachinukiBout, boutRow,
  marks, markSlots, ipponBtn, fusenshoBtn, tieBtn, foulBtn, byTestId,
} from '../helpers/team_editor.js';
import { serverFold } from '../helpers/fake_outbox.js';
import { bridge, applyPatchToTree } from '../../court_bridge.jsx';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn(pending)} password="pw" variant="inline" {...props} />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const events = () => API.recordTap.mock.calls.map((c) => c[2]);
const TEAM = { team: true };
// The outbox's answer to a rule-bound write: at once, with the server's to come.
const pending = () => Promise.resolve({ applied: true, pending: true, settled: Promise.resolve({ applied: true }) });

describe('a running team match is scored by taps', () => {
  it('an ippon is one ippon.add on its bout and side, shown at once, and nothing else is written', async () => {
    const onSubmit = vi.fn();
    const r = render(el(teamBout(), { onSubmit }));
    await tap(ipponBtn(r.container, 1, 'shiro', 'M'));
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 2, side: 'b', letter: 'M' }]);
    const call = API.recordTap.mock.calls[0];
    expect(call.slice(0, 2)).toEqual(['c1', 'tm1']);
    expect(call[3]).toBe('pw');
    expect(call[4]).toMatchObject({ id: 'tm1', scoreVersion: 4 });
    expect(marks(r.container, 1, 'shiro')).toEqual(['M']);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('tapping a scored mark is one ippon.remove naming the mark by its id', async () => {
    const r = render(el(teamBout({ subResults: [boutRow(1, { ipponsA: ['K'], winner: 'Kyoto', markIds: { a: ['srv-k'] } }), boutRow(2), boutRow(3)] })));
    const slot = markSlots(r.container, 0, 'aka').find((b) => b.textContent === 'K');
    await tap(slot);
    expect(events()).toEqual([{ kind: 'ippon.remove', pos: 1, side: 'a', target: 'srv-k' }]);
    expect(marks(r.container, 0, 'aka')).toEqual([]);
  });

  it('once a side has two the bout is decided: its buttons close and no third strike is sent', async () => {
    const r = render(el(teamBout()));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(ipponBtn(r.container, 0, 'aka', 'K'));
    const third = ipponBtn(r.container, 0, 'aka', 'D');
    expect(third.disabled, 'the decided bout takes no more strikes').toBe(true);
    await tap(third);
    expect(events().map((e) => e.letter)).toEqual(['M', 'K']);
  });

  it('a match started from the queue holds no bout rows: the first tap is a tap alone, the server makes the row', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(teamBout({ subResults: [] }), { onSubmit }));
    await tap(ipponBtn(r.container, 1, 'aka', 'K'));
    expect(onSubmit, 'no score write ahead of the tap (engine.createsMissingRow)').not.toHaveBeenCalled();
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 2, side: 'a', letter: 'K' }]);
    expect(marks(r.container, 1, 'aka'), 'the tap shows before the server holds the row').toEqual(['K']);
    await tap(ipponBtn(r.container, 0, 'shiro', 'M'));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(events().length).toBe(2);
    expect(marks(r.container, 0, 'shiro')).toEqual(['M']);
  });

  it('a foul is one foul.add, and its removal one foul.remove; the second foul lands the H in the fold', async () => {
    const r = render(el(teamBout()));
    await tap(foulBtn(r.container, 0, 'aka', true));
    await tap(foulBtn(r.container, 0, 'aka', true));
    expect(events()).toEqual([
      { kind: 'foul.add', pos: 1, side: 'a' },
      { kind: 'foul.add', pos: 1, side: 'a' },
    ]);
    expect(marks(r.container, 0, 'shiro')).toEqual(['H']);
    await tap(foulBtn(r.container, 0, 'shiro', true));
    await tap(foulBtn(r.container, 0, 'shiro', false));
    expect(events().slice(2)).toEqual([
      { kind: 'foul.add', pos: 1, side: 'b' },
      { kind: 'foul.remove', pos: 1, side: 'b' },
    ]);
  });

  it('Fusensho is one bout.decision for the side, and tapped again a clear; the other side keeps its point', async () => {
    const r = render(el(teamBout({ phase: 'pool' })));
    await tap(ipponBtn(r.container, 0, 'shiro', 'K'));
    await tap(fusenshoBtn(r.container, 0, 'aka'));
    expect(events().slice(1)).toEqual([{ kind: 'bout.decision', pos: 1, side: 'a', value: 'fusensho' }]);
    expect(marks(r.container, 0, 'aka')).toEqual(['○', '○']);
    expect(marks(r.container, 0, 'shiro')).toEqual(['K']);
    await tap(fusenshoBtn(r.container, 0, 'aka'));
    expect(events().slice(2)).toEqual([{ kind: 'bout.decision', pos: 1, value: 'clear' }]);
    expect(marks(r.container, 0, 'aka')).toEqual([]);
    expect(marks(r.container, 0, 'shiro')).toEqual(['K']);
  });

  it('Tie is one bout.decision hikiwake, and on a bout the tie alone made played a clear', async () => {
    const r = render(el(teamBout({ phase: 'pool' })));
    await tap(tieBtn(r.container, 2));
    expect(events()).toEqual([{ kind: 'bout.decision', pos: 3, value: 'hikiwake' }]);
    await tap(tieBtn(r.container, 2));
    expect(events().slice(1)).toEqual([{ kind: 'bout.decision', pos: 3, value: 'clear' }]);
  });

  it('the encounter overtime moves one encho tap per period on the match', async () => {
    const r = render(el(teamBout({ phase: 'pool' })));
    await tap(byTestId(r.container, 'scoring-modal-encho-pill'));
    await tap(byTestId(r.container, 'scoring-modal-encho-checkbox'));
    await tap(r.container.querySelector('button[aria-label="Increase overtime period count"]'));
    await tap(r.container.querySelector('button[aria-label="Decrease overtime period count"]'));
    expect(events()).toEqual([
      { kind: 'encho.add', pos: 0 },
      { kind: 'encho.add', pos: 0 },
      { kind: 'encho.remove', pos: 0 },
    ]);
  });

  it('with a representative bout the overtime is that bout\'s, and a hantei pick and its undo are verdict taps', async () => {
    const dh = boutRow(-1, { sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' });
    const tied = [boutRow(1, { ipponsA: ['M'], winner: 'Kyoto' }), boutRow(2, { ipponsB: ['M'], winner: 'Osaka' }), boutRow(3, { decision: 'hikiwake' }), dh];
    const r = render(el(teamBout({ subResults: tied })));
    await tap(byTestId(r.container, 'scoring-modal-encho-pill'));
    await tap(byTestId(r.container, 'scoring-modal-encho-checkbox'));
    expect(events()).toEqual([{ kind: 'encho.add', pos: -1 }]);
    await tap(byTestId(r.container, 'team-daihyosen-hantei-arm'));
    expect(events().length, 'the arm alone is a mode and sends nothing').toBe(1);
    await tap(byTestId(r.container, 'team-daihyosen-hantei-shiro'));
    expect(events().slice(1)).toEqual([{ kind: 'daihyosen.verdict', pos: -1, side: 'b' }]);
    expect(byTestId(r.container, 'team-daihyosen-ht-shiro')).toBeTruthy();
    await tap(byTestId(r.container, 'team-daihyosen-ht-shiro'));
    expect(events().slice(2)).toEqual([{ kind: 'daihyosen.verdict', pos: -1, value: 'clear' }]);
  });

  it('a pending tap and another device\'s pushed point show together; the push carrying the tap counts it once', async () => {
    const m0 = teamBout();
    const r = render(el(m0));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    // Another device scores Shiro's K on bout 2; the push holds no tap of ours.
    const other = serverFold(m0, [{ id: 'dev2.1', session: 'dev2', seq: 1, stamp: m0.modifiedAt + 5, kind: 'ippon.add', pos: 2, side: 'b', letter: 'K' }], TEAM).match;
    await act(async () => { r.rerender(el(other)); });
    expect(marks(r.container, 0, 'aka')).toEqual(['M']);
    expect(marks(r.container, 1, 'shiro')).toEqual(['K']);
    // The server logs ours: its push carries the mark and the seq.
    const both = serverFold(other, outbox.taps('c1', 'tm1'), TEAM).match;
    await act(async () => { r.rerender(el(both)); });
    expect(marks(r.container, 0, 'aka')).toEqual(['M']);
    await act(async () => { outbox.ack('c1', 'tm1', both); });
    expect(marks(r.container, 0, 'aka')).toEqual(['M']);
  });

  it('a tap the server did not apply is said beside the board', async () => {
    const r = render(el(teamBout()));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    const [sent] = outbox.taps('c1', 'tm1');
    await act(async () => { outbox.ack('c1', 'tm1', null, { [sent.id]: { outcome: 'capped' } }); });
    expect(byTestId(r.container, 'tap-not-applied-note')).toBeTruthy();
  });

  it('the public self-run sheet taps with the empty password', async () => {
    const r = render(el(teamBout(), { password: '', selfReport: true }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    expect(API.recordTap.mock.calls[0][3]).toBe('');
  });

  it('Finish is the rule-bound write over the board, naming the copy the board showed', async () => {
    const onSubmit = vi.fn(pending);
    const done = [boutRow(1, { ipponsA: ['M'], winner: 'Kyoto' }), boutRow(2, { ipponsA: ['K'], winner: 'Kyoto' }), boutRow(3)];
    const r = render(el(teamBout({ phase: 'pool', subResults: done }), { onSubmit }));
    await tap(ipponBtn(r.container, 2, 'shiro', 'D'));
    const finish = [...r.container.querySelectorAll('button')].find((b) => /Finish/.test(b.textContent));
    await tap(finish);
    const confirm = [...r.container.querySelectorAll('button')].find((b) => /Tap again/.test(b.textContent));
    await new Promise((res) => setTimeout(res, 450));
    await tap(confirm);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.status).toBe('completed');
    expect(patch.seenScoreVersion).toBe(4);
    expect(patch.subResults.find((s) => s.position === 3).ipponsB).toEqual(['D']);
  });

  it('a match that is not running keeps a local draft, sent by Start', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(teamBout({ status: 'scheduled' }), { onSubmit }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(marks(r.container, 0, 'aka')).toEqual(['M']);
  });
});

describe('a pick on a running team match is a tap', () => {
  const SQUADS = {
    ta: [{ id: 'k1', name: 'Kenji', index: 1 }, { id: 'k2', name: 'Kaito', index: 2 }],
    tb: [{ id: 'o1', name: 'Osamu', index: 1 }, { id: 'o2', name: 'Oda', index: 2 }],
  };
  beforeEach(() => {
    API.fetchSquads = vi.fn().mockResolvedValue(SQUADS);
    API.fetchLineupInForce = vi.fn().mockResolvedValue({ saved: false, positions: {} });
    API.fetchTeamLineup = vi.fn().mockResolvedValue({ saved: false, positions: {} });
  });
  const pickFrom = async (input, name) => {
    await act(async () => { fireEvent.focus(input); fireEvent.click(input); });
    const opt = [...document.querySelectorAll('.pmf__opt-name')].find((n) => n.textContent === name);
    expect(opt, `${name} is offered`).toBeTruthy();
    await act(async () => { fireEvent.click(opt.closest('button')); });
    await act(async () => { await new Promise((res) => setTimeout(res, 0)); });
  };

  it('a representative picked from the list is one rep.pick naming the member', async () => {
    const dh = boutRow(-1, { sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' });
    const r = render(el(teamBout({ subResults: [boutRow(1, { decision: 'hikiwake' }), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' }), dh] })));
    await act(async () => { await new Promise((res) => setTimeout(res, 0)); });
    const input = r.getByLabelText('Daihyosen SHIRO player');
    await pickFrom(input, 'Oda');
    expect(events()).toEqual([{ kind: 'rep.pick', pos: -1, side: 'b', memberId: 'o2' }]);
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('a kachinuki fighter picked on a later bout is one bout.fighter with the member\'s name and id', async () => {
    const m0 = kachinukiBout({ subResults: [
      boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', ipponsA: ['M', 'K'], winner: 'K1', winnerMemberId: 'k1', recorded: true }),
      boutRow(2, { sideA: 'Kenji', sideAMemberId: 'k1' }),
    ] });
    const r = render(el(m0));
    await act(async () => { await new Promise((res) => setTimeout(res, 0)); });
    const boxes = r.getAllByLabelText(/SHIRO player$/);
    await pickFrom(boxes[boxes.length - 1], 'Oda');
    expect(events()).toEqual([{ kind: 'bout.fighter', pos: 2, side: 'b', name: 'Oda', memberId: 'o2' }]);
  });

  // Stage 6: two devices striking on a kachinuki first bout at once both count.
  // The server makes the first bout's row at its first tap, so the sheet writes
  // no row ahead of it (a row write stamped after the other device's tap turned
  // that tap away): the lineup's fighters go as its first taps on the bout.
  it('a kachinuki first bout the server holds no row for: the lineup\'s fighters go as the first taps, and no row is written', async () => {
    API.fetchLineupInForce = vi.fn().mockImplementation(async (_comp, teamId) => (teamId === 'ta'
      ? { saved: true, positions: { 1: 'Kenji' }, memberIds: { 1: 'k1' } }
      : { saved: true, positions: { 1: 'Osamu' }, memberIds: { 1: 'o1' } }));
    const onSubmit = vi.fn(pending);
    const r = render(el(kachinukiBout({ subResults: [] }), { onSubmit }));
    await act(async () => { await new Promise((res) => setTimeout(res, 0)); });
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    expect(onSubmit, 'no row write ahead of the tap').not.toHaveBeenCalled();
    expect(events()).toEqual([
      { kind: 'bout.fighter', pos: 1, side: 'a', name: 'Kenji', memberId: 'k1' },
      { kind: 'bout.fighter', pos: 1, side: 'b', name: 'Osamu', memberId: 'o1' },
      { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' },
    ]);
    await tap(ipponBtn(r.container, 0, 'shiro', 'K'));
    expect(events().slice(3), 'the fighters are seated once').toEqual([{ kind: 'ippon.add', pos: 1, side: 'b', letter: 'K' }]);
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('a running kachinuki encounter is scored by taps', () => {
  it('Encho on the tied live bout is one encho.add on it, and Undo encho one encho.remove', async () => {
    const tiedBout = kachinukiBout({ subResults: [boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', decision: 'hikiwake' })] });
    const r = render(el(tiedBout));
    await tap(byTestId(r.container, 'kachinuki-encho-button'));
    expect(events()).toEqual([{ kind: 'encho.add', pos: 1 }]);
    await tap(byTestId(r.container, 'kachinuki-encho-undo-button'));
    expect(events().slice(1)).toEqual([{ kind: 'encho.remove', pos: 1 }]);
  });

  it('+ Add next bout manually writes the empty pairing at once, naming its group alone', async () => {
    const onSubmit = vi.fn(pending);
    const won = kachinukiBout({ subResults: [boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', ipponsA: ['M', 'K'], winner: 'K1', winnerMemberId: 'k1', recorded: true })] });
    const r = render(el(won, { onSubmit }));
    const add = byTestId(r.container, 'kachinuki-add-bout-button');
    expect(add, 'the manual add is offered').toBeTruthy();
    await tap(add);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.status).toBe('running');
    expect(patch.changed).toEqual(['bout:2']);
    expect(patch.subResults).toEqual([expect.objectContaining({ position: 2, ipponsA: [], ipponsB: [] })]);
    expect(API.recordTap).not.toHaveBeenCalled();
  });
});

describe('a same-device display shows the team board', () => {
  let publish;
  beforeEach(() => { publish = vi.spyOn(bridge, 'publish').mockImplementation(() => {}); });
  afterEach(() => { publish.mockRestore(); delete API.hasPendingTerminalWrite; });
  const patches = () => publish.mock.calls.filter((c) => c[0] === 'patch').map((c) => c[3].result);

  it('each tap publishes the board, its bout rows with the pending tap, and IV and PW to work out again', async () => {
    const r = render(el(teamBout()));
    publish.mockClear();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    const last = patches().at(-1);
    expect(last).toMatchObject({ id: 'tm1', status: 'running', teamResult: null });
    expect(last.subResults.find((s) => s.position === 1)).toMatchObject({ ipponsA: ['M'], winner: 'Kyoto' });
    // A display holding the server's earlier copy takes it whole.
    const comp = { id: 'c1', poolMatches: [{ ...teamBout(), phase: 'pool' }] };
    const payload = publish.mock.calls.filter((c) => c[0] === 'patch').at(-1)[3];
    const shown = applyPatchToTree({ competitions: [comp] }, { type: 'patch', court: 'A', compId: 'c1', payload }).competitions[0].poolMatches[0];
    expect(shown.subResults.find((s) => s.position === 1).ipponsA).toEqual(['M']);
  });

  it('nothing is published once a finished result of the match is queued', async () => {
    API.hasPendingTerminalWrite = () => true;
    const r = render(el(teamBout()));
    publish.mockClear();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    expect(patches()).toEqual([]);
  });

  it('a match that is not running publishes nothing', async () => {
    const r = render(el(teamBout({ status: 'scheduled' })));
    publish.mockClear();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    expect(patches()).toEqual([]);
  });
});

describe('Record bout carries the copy the board showed', () => {
  it('its running write names the board\'s scoreVersion, after the taps on the bout', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(kachinukiBout({ scoreVersion: 9 }), { onSubmit }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap([...r.container.querySelectorAll('button')].find((b) => b.textContent === 'Record bout'));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch).toMatchObject({ status: 'running', kachinukiBoutFinal: true, seenScoreVersion: 9 });
    expect(patch.subResults.find((s) => s.position === 1).ipponsA).toEqual(['M']);
    expect(API.recordTap.mock.invocationCallOrder[0]).toBeLessThan(onSubmit.mock.invocationCallOrder[0]);
  });
});
