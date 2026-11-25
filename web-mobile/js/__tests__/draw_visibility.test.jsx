import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  drawHiddenFromPublic, drawPublishedDraft, withoutHiddenDrafts, isWithheldMatchEvent,
  drawVisibilityIcon, drawVisibilityCardLine, DRAW_HIDDEN_CARD_LINE, DRAW_PUBLIC_CARD_LINE,
  publicDrawPreparingMessage, startHiddenDrawMessage, discardPublishedDrawMessage,
  startAllHiddenDrawsNote, callHiddenDrawMessage, confirmStartHiddenDraw,
} from '../draw_visibility.jsx';
import { maskSnapshotForBridge } from '../court_bridge.jsx';
import { publishDraw, unpublishDraw } from '../api_client_requests.jsx';
import { competitionsApi } from '../api_client_competitions.jsx';

// The server's Competition.DrawHiddenIn reads the same table
// (TestDrawHiddenIn_SharedTable, internal/state), so the twin cannot drift.
const casesPath = resolve(__dirname, '../../../internal/mobileapp/testdata/draw_visibility_cases.json');
const { cases } = JSON.parse(readFileSync(casesPath, 'utf8'));

const hidden = { id: 'a', name: 'Kyu A', status: 'draw-ready', drawHidden: true };
const published = { id: 'b', name: 'Kyu B', status: 'draw-ready' };
const running = { id: 'c', name: 'Dan C', status: 'pools', drawHidden: true };
const officiated = { mode: 'officiated' };
const selfRun = { mode: 'self-run' };

describe('drawHiddenFromPublic (shared draw_visibility_cases.json)', () => {
  it('reads a table of cases', () => { expect(cases.length).toBeGreaterThan(5); });
  for (const c of cases) {
    it(c.name, () => {
      const comp = { status: c.status };
      if ('drawHidden' in c) comp.drawHidden = c.drawHidden;
      expect(drawHiddenFromPublic({ mode: c.mode }, comp)).toBe(c.hidden);
    });
  }

  it('reads a competition with no tournament as one of an officiated tournament', () => {
    expect(drawHiddenFromPublic(null, hidden)).toBe(true);
    expect(drawHiddenFromPublic(null, null)).toBe(false);
    expect(drawHiddenFromPublic(officiated, undefined)).toBe(false);
  });
});

describe('drawPublishedDraft', () => {
  it('is a draft the public sees', () => {
    expect(drawPublishedDraft(officiated, published)).toBe(true);
    expect(drawPublishedDraft(officiated, hidden)).toBe(false);
    expect(drawPublishedDraft(selfRun, hidden)).toBe(true);
    expect(drawPublishedDraft(officiated, running)).toBe(false);
  });
});

describe('withoutHiddenDrafts', () => {
  it('leaves out the drafts the public may not see', () => {
    expect(withoutHiddenDrafts([hidden, published, running], officiated)).toEqual([published, running]);
  });
  it('hands the same array back when it holds none', () => {
    const comps = [published, running];
    expect(withoutHiddenDrafts(comps, officiated)).toBe(comps);
    const all = [hidden, published];
    expect(withoutHiddenDrafts(all, selfRun)).toBe(all);
  });
  it('passes a value that is not a list through', () => {
    expect(withoutHiddenDrafts(null, officiated)).toBe(null);
  });
});

describe('isWithheldMatchEvent', () => {
  it('is a match_updated sent with no data', () => {
    expect(isWithheldMatchEvent({ type: 'match_updated', data: null })).toBe(true);
    expect(isWithheldMatchEvent({ type: 'match_updated' })).toBe(true);
    expect(isWithheldMatchEvent({ type: 'match_updated', data: { competitionId: 'a' } })).toBe(false);
    expect(isWithheldMatchEvent({ type: 'lineup_updated', data: null })).toBe(false);
    expect(isWithheldMatchEvent(null)).toBe(false);
  });
});

describe('the words', () => {
  it('names the card line and its icon', () => {
    expect(drawVisibilityCardLine(officiated, hidden)).toBe(DRAW_HIDDEN_CARD_LINE);
    expect(drawVisibilityIcon(officiated, hidden)).toBe('eye-off');
    expect(drawVisibilityCardLine(officiated, published)).toBe(DRAW_PUBLIC_CARD_LINE);
    expect(drawVisibilityIcon(officiated, published)).toBe('eye');
  });

  it('words the public card with and without a start time', () => {
    expect(publicDrawPreparingMessage({ startTime: '10:30', players: [{}, {}, {}] }))
      .toBe('Starts at 10:30. 3 competitors entered. The draw shows here once it is published.');
    expect(publicDrawPreparingMessage({ players: [{}] }))
      .toBe('1 competitor entered. The draw shows here once it is published.');
    expect(publicDrawPreparingMessage({ kind: 'team', startTime: ' ', players: [{}, {}] }))
      .toBe('2 teams entered. The draw shows here once it is published.');
    expect(publicDrawPreparingMessage({ teamSize: 3 }, 1))
      .toBe('1 team entered. The draw shows here once it is published.');
  });

  it('words the confirms', () => {
    expect(startHiddenDrawMessage('Kyu A')).toBe('The draw for "Kyu A" is still hidden from the public. Starting the competition makes it public at once.');
    expect(discardPublishedDrawMessage('Kyu B')).toBe('The public has already seen this draw. Discard the draw for "Kyu B"? The pools and bracket are removed so you can edit participants and settings, and the new draw starts hidden.');
    expect(callHiddenDrawMessage('Kyu A')).toBe('The draw for Kyu A is still hidden from the public. Calling this match shows its competitors on the public screens.');
  });

  it('names the hidden draws Start all makes public, and nothing when there are none', () => {
    const other = { ...hidden, id: 'd', name: 'Kyu D' };
    expect(startAllHiddenDrawsNote(officiated, [hidden, published, other])).toBe('Starting makes these hidden draws public: Kyu A, Kyu D.');
    expect(startAllHiddenDrawsNote(officiated, [published])).toBe(null);
    expect(startAllHiddenDrawsNote(selfRun, [hidden])).toBe(null);
  });
});

describe('the same-browser court bridge', () => {
  it('leaves a hidden draft out of a snapshot reply, as the public court feeds do', () => {
    const comps = [hidden, published, running];
    expect(maskSnapshotForBridge(comps, officiated).map((c) => c.id)).toEqual(['b', 'c']);
  });
  it('hands a slice with no hidden draft back as it is', () => {
    const comps = [published, running];
    expect(maskSnapshotForBridge(comps, officiated)).toBe(comps);
  });
  it('keeps a self-run tournament\'s draft, which is public as soon as it is made', () => {
    const comps = [hidden];
    expect(maskSnapshotForBridge(comps, selfRun)).toBe(comps);
  });
});

describe('publishDraw and unpublishDraw', () => {
  let origFetch;
  beforeEach(() => { origFetch = global.fetch; });
  afterEach(() => { global.fetch = origFetch; });
  const answer = (status, body) => vi.fn(() => Promise.resolve({ ok: status < 300, status, json: () => Promise.resolve(body) }));

  it('posts to each route with the password and answers the competition as stored', async () => {
    global.fetch = answer(200, { id: 'kyu a', name: 'Kyu A', status: 'draw-ready', courts: null });
    const comp = await publishDraw('kyu a', 'pw');
    expect(global.fetch).toHaveBeenCalledWith('/api/competitions/kyu%20a/publish-draw', { method: 'POST', headers: { 'X-Tournament-Password': 'pw' } });
    expect(comp.courts).toEqual([]);
    global.fetch = answer(200, { id: 'a', status: 'draw-ready', drawHidden: true });
    await unpublishDraw('a', 'pw');
    expect(global.fetch.mock.calls[0][0]).toBe('/api/competitions/a/unpublish-draw');
  });

  it('throws the server\'s sentence with its code', async () => {
    global.fetch = answer(409, { code: 'draw_not_ready', error: 'The draw can be published or hidden only while it waits to start.' });
    await expect(publishDraw('a', 'pw')).rejects.toMatchObject({ message: 'The draw can be published or hidden only while it waits to start.', code: 'draw_not_ready' });
    global.fetch = answer(500, null);
    await expect(unpublishDraw('a', 'pw')).rejects.toThrow('The draw was not hidden');
  });

  it('are doors of window.API, through the competitions API', () => {
    expect(competitionsApi.publishDraw).toBe(publishDraw);
    expect(competitionsApi.unpublishDraw).toBe(unpublishDraw);
  });
});

describe('confirmStartHiddenDraw', () => {
  afterEach(() => { delete window.confirmDialog; });

  it('starts a published draft without asking', async () => {
    window.confirmDialog = vi.fn();
    await expect(confirmStartHiddenDraw(officiated, published)).resolves.toBe(true);
    await expect(confirmStartHiddenDraw(selfRun, hidden)).resolves.toBe(true);
    expect(window.confirmDialog).not.toHaveBeenCalled();
  });

  it('asks before starting a hidden draft and answers what the operator chose', async () => {
    window.confirmDialog = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    await expect(confirmStartHiddenDraw(officiated, hidden)).resolves.toBe(false);
    await expect(confirmStartHiddenDraw(officiated, hidden)).resolves.toBe(true);
    expect(window.confirmDialog).toHaveBeenCalledWith({
      message: startHiddenDrawMessage('Kyu A'),
      confirmLabel: 'Start competition',
      cancelLabel: 'Cancel',
    });
  });
});
