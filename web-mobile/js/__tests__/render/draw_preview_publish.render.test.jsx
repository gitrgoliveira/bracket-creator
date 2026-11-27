import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import {
  DRAW_HIDDEN_HINT, DRAW_PUBLISHED_HINT, DRAW_PUBLISHED_TOAST, DRAW_UNPUBLISHED_TOAST,
  DRAW_HIDDEN_CARD_LINE, DRAW_PUBLIC_CARD_LINE, OPEN_PUBLIC_PAGE_LABEL,
  startHiddenDrawMessage, discardPublishedDrawMessage,
} from '../../draw_visibility.jsx';

// bc-drpb FR9, FR11, FR12, FR15: the draw preview header of a draw-ready
// competition. In an officiated tournament it holds Discard draw, Publish draw
// (Unpublish once published) and Start competition, in that order, with the
// hint line saying who can see the draw. Start on a hidden draft asks first;
// Discard on a published draft warns that the public saw it. Every button is
// disabled while a draw mutation is out. A self-run tournament shows none of
// the visibility controls.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  AdminTopbar: () => <div data-stub="AdminTopbar" />,
  Breadcrumbs: () => null,
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: () => null,
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({ matches: false, addEventListener: noop, removeEventListener: noop, addListener: noop, removeListener: noop }),
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    subscribeToEvents: () => () => {},
    discardDraw: vi.fn().mockResolvedValue({}),
    startCompetition: vi.fn().mockResolvedValue({}),
    publishDraw: vi.fn().mockResolvedValue({}),
    unpublishDraw: vi.fn().mockResolvedValue({}),
  },
};

let restoreGlobals;
let AdminCompetition;
beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_competition.jsx');
  AdminCompetition = window.AdminCompetition;
});
afterAll(() => restoreGlobals());
beforeEach(() => {
  window.confirmDialog = vi.fn().mockResolvedValue(false);
  for (const k of ['discardDraw', 'startCompetition', 'publishDraw', 'unpublishDraw']) window.API[k] = vi.fn().mockResolvedValue({});
});

async function mount({ hidden = true, mode = 'officiated', status = 'draw-ready' } = {}) {
  const comp = { id: 'c1', name: 'Mudansha', status, drawHidden: hidden, format: 'pools', kind: 'individual', players: [], courts: ['A'], startTime: '09:00', date: '', fightingSpiritAwards: [] };
  const t = { name: 'Spring Taikai', mode, courts: ['A'], competitions: [comp] };
  const calls = { showToast: vi.fn(), onRefreshCompetition: vi.fn().mockResolvedValue({ ok: true }), onSection: vi.fn() };
  let utils;
  await act(async () => {
    utils = render(
      <AdminCompetition tournament={t} competition={comp} pools={[]} poolMatches={[]} standings={[]} bracket={null}
        section="participants" onBack={noop} onOpenCompetition={noop} onCreateCompetition={noop} onLogout={noop} onViewerMode={noop}
        onUpdate={noop} onMoveCourt={noop} onEditScore={noop} tweaks={{}} password="pw" {...calls} />
    );
  });
  return { ...utils, calls };
}

const headerButtons = (container) => [...container.querySelectorAll('.page-head__actions button')];
const labels = (container) => headerButtons(container).map((b) => b.textContent.trim());
const button = (container, label) => headerButtons(container).find((b) => b.textContent.trim() === label);
const hint = () => screen.queryByTestId('draw-visibility-hint');
const state = () => screen.queryByTestId('draw-visibility-state');
const publicLink = () => screen.queryByTestId('draw-public-page-link');

describe('the draw preview header (bc-drpb)', () => {
  it('a hidden draft offers Discard draw, Publish draw, Start competition in that order, and says it is hidden', async () => {
    const { container } = await mount({ hidden: true });
    expect(labels(container)).toEqual(['Discard draw', 'Publish draw', 'Start competition']);
    expect(hint().textContent).toBe(`${DRAW_HIDDEN_HINT} ${OPEN_PUBLIC_PAGE_LABEL}`);
    expect(hint().className).toContain('draw-visibility-hint--hidden');
    // The title row says it too, beside the Draw ready badge.
    expect(state().textContent).toBe(DRAW_HIDDEN_CARD_LINE);
    expect(state().className).toContain('draw-visibility-line--hidden');
  });

  it('Publish draw is the bordered secondary button, apart from the ghost Discard draw (round critique-2 D2)', async () => {
    const { container } = await mount({ hidden: true });
    const toggle = container.querySelector('[data-testid="draw-visibility-toggle"]');
    expect(toggle.className.split(' ')).toEqual(['btn']);
  });

  it('the hint ends with a link to this competition\'s public page, in a new tab', async () => {
    await mount({ hidden: true });
    expect(publicLink().getAttribute('href')).toBe('/competition/c1');
    expect(publicLink().getAttribute('target')).toBe('_blank');
    expect(publicLink().getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('a published draft offers Unpublish in the same place and says it is published', async () => {
    const { container } = await mount({ hidden: false });
    expect(labels(container)).toEqual(['Discard draw', 'Unpublish', 'Start competition']);
    expect(hint().textContent).toBe(`${DRAW_PUBLISHED_HINT} ${OPEN_PUBLIC_PAGE_LABEL}`);
    expect(hint().className).toContain('draw-visibility-hint--published');
    expect(state().textContent).toBe(DRAW_PUBLIC_CARD_LINE);
    expect(state().className).toContain('draw-visibility-line--public');
  });

  it('Publish draw publishes, refetches and says so; nothing asks first', async () => {
    const { container, calls } = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(container, 'Publish draw')); });
    expect(window.API.publishDraw).toHaveBeenCalledWith('c1', 'pw');
    expect(window.API.unpublishDraw).not.toHaveBeenCalled();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(calls.onRefreshCompetition).toHaveBeenCalledTimes(1);
    expect(calls.showToast).toHaveBeenCalledWith(DRAW_PUBLISHED_TOAST);
  });

  it('Unpublish hides the draw again and says what it cannot take back', async () => {
    const { container, calls } = await mount({ hidden: false });
    await act(async () => { fireEvent.click(button(container, 'Unpublish')); });
    expect(window.API.unpublishDraw).toHaveBeenCalledWith('c1', 'pw');
    expect(window.API.publishDraw).not.toHaveBeenCalled();
    expect(calls.showToast).toHaveBeenCalledWith(DRAW_UNPUBLISHED_TOAST);
  });

  it('a refused publish says the server sentence and refetches the competition', async () => {
    const err = Object.assign(new Error('The draw can be published or hidden only while it waits to start.'), { code: 'draw_not_ready' });
    window.API.publishDraw = vi.fn().mockRejectedValue(err);
    const spy = vi.spyOn(console, 'error').mockImplementation(noop);
    const { container, calls } = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(container, 'Publish draw')); });
    spy.mockRestore();
    expect(calls.showToast).toHaveBeenCalledWith(err.message, 'error');
    expect(calls.showToast).not.toHaveBeenCalledWith(DRAW_PUBLISHED_TOAST);
    expect(calls.onRefreshCompetition).toHaveBeenCalledTimes(1);
    expect(labels(container)).toEqual(['Discard draw', 'Publish draw', 'Start competition']);
  });

  it('every header button is disabled while a publish is out, and comes back after', async () => {
    let finish;
    window.API.publishDraw = vi.fn(() => new Promise((r) => { finish = r; }));
    const { container } = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(container, 'Publish draw')); });
    expect(labels(container)).toEqual(['Discard draw', 'Publishing…', 'Start competition']);
    expect(headerButtons(container).every((b) => b.disabled)).toBe(true);
    await act(async () => { finish({}); });
    expect(headerButtons(container).some((b) => b.disabled)).toBe(false);
  });

  it('the publish button is disabled while a discard is out', async () => {
    let finish;
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.API.discardDraw = vi.fn(() => new Promise((r) => { finish = r; }));
    const { container } = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(container, 'Discard draw')); });
    expect(button(container, 'Publish draw').disabled).toBe(true);
    await act(async () => { finish({}); });
  });

  it('Start on a hidden draft asks first; Cancel starts nothing, Start competition starts it', async () => {
    const { container } = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(container, 'Start competition')); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    const [ask] = window.confirmDialog.mock.calls[0];
    expect(ask).toMatchObject({ title: 'Start Mudansha?', message: startHiddenDrawMessage('Mudansha'), confirmLabel: 'Start competition', cancelLabel: 'Cancel' });
    expect(window.API.startCompetition).not.toHaveBeenCalled();

    window.confirmDialog = vi.fn().mockResolvedValue(true);
    await act(async () => { fireEvent.click(button(container, 'Start competition')); });
    expect(window.API.startCompetition).toHaveBeenCalledWith('c1', 'pw');
  });

  it('Start on a published draft starts in the same tick with no question', async () => {
    const { container } = await mount({ hidden: false });
    fireEvent.click(button(container, 'Start competition'));
    expect(window.API.startCompetition).toHaveBeenCalledWith('c1', 'pw');
    expect(window.confirmDialog).not.toHaveBeenCalled();
    await act(async () => {});
  });

  it('Discard on a published draft warns that the public saw it; a hidden draft keeps the plain confirm', async () => {
    const pub = await mount({ hidden: false });
    await act(async () => { fireEvent.click(button(pub.container, 'Discard draw')); });
    expect(window.confirmDialog.mock.calls[0][0]).toMatchObject({ title: 'Discard the draw for Mudansha?', message: discardPublishedDrawMessage('Mudansha'), confirmLabel: 'Discard draw', cancelLabel: 'Cancel', danger: true });
    expect(window.API.discardDraw).not.toHaveBeenCalled();
    pub.unmount();

    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const hid = await mount({ hidden: true });
    await act(async () => { fireEvent.click(button(hid.container, 'Discard draw')); });
    expect(window.confirmDialog.mock.calls[0][0].message).toMatch(/^Discard the generated draw for "Mudansha"\?/);
    expect(window.confirmDialog.mock.calls[0][0].title).toBe('Discard the draw for Mudansha?');
  });

  it('a self-run tournament shows no visibility control, hint or confirm', async () => {
    const { container } = await mount({ hidden: true, mode: 'self-run' });
    expect(labels(container)).toEqual(['Discard draw', 'Start competition']);
    expect(hint()).toBeNull();
    expect(state()).toBeNull();
    expect(publicLink()).toBeNull();
    expect(container.textContent).toContain('Draw generated. Preview below, then start.');
    fireEvent.click(button(container, 'Start competition'));
    expect(window.API.startCompetition).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    await act(async () => {});
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await act(async () => { fireEvent.click(button(container, 'Discard draw')); });
    expect(window.confirmDialog.mock.calls[0][0].message).toMatch(/^Discard the generated draw for "Mudansha"\?/);
  });

  it('a started competition shows none of the draft header', async () => {
    const { container } = await mount({ hidden: true, status: 'pools' });
    expect(labels(container)).not.toContain('Publish draw');
    expect(labels(container)).not.toContain('Unpublish');
    expect(hint()).toBeNull();
  });
});
