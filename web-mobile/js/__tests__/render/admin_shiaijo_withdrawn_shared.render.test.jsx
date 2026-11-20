import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, probe, makeMinimalTournament, renderPage, mountCourt, courtSide, courtMatch } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

// Recording a withdrawal advances the court like any other decision (operator
// ruling 2026-09-26). This court's list does not show the withdrawn
// competitor's matches barred until it is refetched, so the advance passes
// over them itself rather than starting one the server then refuses.
describe('the court moves on past the withdrawn competitor', () => {
  it('starts the next match the withdrawn competitor is not in', async () => {
    const side = (id, name) => ({ id, name });
    const row = (id, a, b, status, at) => ({
      id, compId: 'c1', compName: 'Cup', status, phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: at, sideA: a, sideB: b,
    });
    const aoki = side('p1', 'Aoki');
    const run = row('m1', aoki, side('p2', 'Baba'), 'running', '09:00');
    window.tournamentMatches = () => [run, row('m2', side('p3', 'Endo'), aoki, 'scheduled', '09:05'), row('m3', side('p4', 'Doi'), side('p5', 'Fujii'), 'scheduled', '09:10')];
    window.filterMatchesByCourt = (matches) => matches;
    const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
    await act(async () => { renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m1');
    await act(async () => {
      // What /decision answers with: the stored match, sides as bare names.
      await probe.props.onAfterDecision({
        id: 'm1', sideA: 'Aoki', sideB: 'Baba', sideAId: 'p1', sideBId: 'p2', winner: 'Baba', winnerId: 'p2',
        status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka',
      });
    });
    expect(onEditScore).toHaveBeenCalledTimes(1);
    expect(onEditScore.mock.calls[0][1], "Aoki's next match is passed over").toBe('m3');
  });
});

// Operator request 2026-10-03: a match moved onto this court from another one,
// for a competition other than the one shown, was visible only through the
// competition picker. The court names the other competition's waiting matches
// quietly while its own still has work, keeps the amber "Switch to" for when it
// has none, and never switches by itself. The move confirm names the MATCH,
// both competitors, not one person.
describe('a court shared with another competition', () => {
  const shared = () => [
    courtMatch('m1', 'running'),
    courtMatch('m2', 'scheduled'),
    courtMatch('m3', 'scheduled', { compId: 'c2', compName: 'League' }),
  ];
  const nudge = (c) => c.utils.queryByTestId('shiaijo-nudge');

  it("names the other competition's waiting match quietly while this one still has work", async () => {
    const c = await mountCourt(shared());
    try {
      const n = nudge(c);
      expect(n, 'the other competition is named on the court').toBeTruthy();
      expect(n.textContent).toContain('1 League match also waiting on this court.');
      expect(n.classList.contains('shiaijo-nudge--also')).toBe(true);
      expect(n.classList.contains('alert--warn')).toBe(false);
      expect(c.editorMatch(), 'nothing switched by itself').toBe('m1');
    } finally { c.restore(); }
  });

  it('switches only when tapped', async () => {
    const c = await mountCourt(shared());
    try {
      const picker = () => c.utils.container.querySelector('select[aria-label="Select competition to officiate"]');
      expect(picker().value).toBe('c1');
      await act(async () => { nudge(c).click(); });
      expect(picker().value).toBe('c2');
    } finally { c.restore(); }
  });

  // One operator per court: the competition shown is held, so the bout that
  // finishes does not swap the court to the other competition. It turns
  // amber and waits for the tap instead.
  it('holds the competition when its last bout here finishes, and turns amber, "Switch to"', async () => {
    const c = await mountCourt([
      courtMatch('m1', 'running'),
      courtMatch('m3', 'scheduled', { compId: 'c2', compName: 'League' }),
    ]);
    try {
      const picker = () => c.utils.container.querySelector('select[aria-label="Select competition to officiate"]');
      expect(picker().value).toBe('c1');
      c.feed.current = [
        courtMatch('m1', 'completed', { winner: courtSide('m1-a', 'Aka m1') }),
        courtMatch('m3', 'scheduled', { compId: 'c2', compName: 'League' }),
      ];
      await c.refresh();
      expect(picker().value, 'the console did not switch by itself').toBe('c1');
      const n = nudge(c);
      expect(n.textContent).toContain('Switch to League: 1 match waiting on this court.');
      expect(n.classList.contains('alert--warn')).toBe(true);
      expect(n.classList.contains('shiaijo-nudge--also')).toBe(false);
      await act(async () => { n.click(); });
      expect(picker().value).toBe('c2');
    } finally { c.restore(); }
  });

  it('names every other competition waiting here, each with its own switch', async () => {
    const c = await mountCourt([
      courtMatch('m1', 'running'),
      courtMatch('m3', 'scheduled', { compId: 'c2', compName: 'League' }),
      courtMatch('m4', 'scheduled', { compId: 'c3', compName: 'Teams' }),
      courtMatch('m5', 'scheduled', { compId: 'c3', compName: 'Teams' }),
    ]);
    try {
      const lines = c.utils.getAllByTestId('shiaijo-nudge');
      expect(lines.map((l) => l.querySelector('.shiaijo-nudge__text').textContent)).toEqual([
        '2 Teams matches also waiting on this court.',
        '1 League match also waiting on this court.',
      ]);
      // The accessible name carries the visible text and what the tap does.
      expect(lines[1].getAttribute('aria-label')).toBe('1 League match also waiting on this court. Switch to League');
      await act(async () => { lines[1].click(); });
      expect(c.utils.container.querySelector('select[aria-label="Select competition to officiate"]').value).toBe('c2');
    } finally { c.restore(); }
  });

  it('the move confirm names both competitors', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')]);
    try {
      await act(async () => { c.utils.getAllByTestId('move-to-B')[0].click(); });
      const dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog.textContent).toMatch(/Shiro m\d vs Aka m\d leaves Shiaijo A and goes last in the queue on Shiaijo B\./);
    } finally { c.restore(); }
  });
});
