// A write that stays direct (not in the outbox) says ONE plain sentence when it
// gets no answer: write_result.jsx's WRITE_NOT_CONFIRMED, thrown by
// api_client.jsx's _directFetch for a fetch that rejects with a TypeError
// (whatever the browser's own words for it). It never claims the request did
// not land, because a TypeError also means only the answer was lost. The PDF
// export and the restore plan, which change nothing, say NOT_SENT_NO_CONNECTION
// instead. The test calls every direct write in API with fetch rejecting, and
// pins the list against the source so a new direct write cannot ship with the
// browser's raw "Failed to fetch".
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { API } from '../api_client.jsx';
import { NOT_SENT_NO_CONNECTION, WRITE_NOT_CONFIRMED, REGISTRATION_NOT_CONFIRMED, isNoConnection, noConnectionError, shownMessage } from '../write_result.jsx';

const PW = 'pw';

// Every direct write function in API, called the way a screen calls it. The
// seven doors the outbox takes over (check-in, bulk check-in, the three
// announcement doors, the tournament save, the awards save) are not here. A
// tournament save that changes the password stays direct (updateTournamentPassword).
const DIRECT_WRITES = {
  resetPassword: () => API.resetPassword('new-password', 'tab-1'),
  createTournament: () => API.createTournament({ name: 'T' }, PW),
  updateTournamentPassword: () => API.updateTournamentPassword({ name: 'T', password: 'new-secret', tournamentVersion: 'v0' }, PW),
  createCompetition: () => API.createCompetition({ name: 'C' }, PW),
  updateCompetition: () => API.updateCompetition('c1', { name: 'C' }, PW),
  exportPDFs: () => API.exportPDFs('all', PW),
  exportResults: () => API.exportResults('c1', PW),
  startCompetition: () => API.startCompetition('c1', PW),
  generateDraw: () => API.generateDraw('c1', PW),
  discardDraw: () => API.discardDraw('c1', PW),
  quarantineBracket: () => API.quarantineBracket('c1', PW),
  completeCompetition: () => API.completeCompetition('c1', PW),
  importCompetitions: () => API.importCompetitions(new FormData(), PW),
  deleteCompetition: () => API.deleteCompetition('c1', PW, PW),
  invalidateCompetition: () => API.invalidateCompetition('c1', PW),
  swissGenerateRound: () => API.swissGenerateRound('c1', PW),
  addParticipant: () => API.addParticipant('c1', { name: 'A', dojo: 'D' }, PW),
  replaceParticipant: () => API.replaceParticipant('c1', 'p1', { name: 'A', dojo: 'D' }, PW),
  uploadSponsor: () => API.uploadSponsor({ file: new Blob(['x']), name: 'S', link: '', password: PW }),
  deleteSponsor: () => API.deleteSponsor(0, PW),
  uploadBrandingLogo: () => API.uploadBrandingLogo({ file: new Blob(['x']), password: PW }),
  deleteBrandingLogo: () => API.deleteBrandingLogo(PW),
  leagueTiebreakGenerate: () => API.leagueTiebreakGenerate('c1', ['A', 'B'], PW, ['a', 'b']),
  leagueTiebreakRemove: () => API.leagueTiebreakRemove('c1', ['A', 'B'], PW, ['a', 'b']),
  leagueTiebreakFinalize: () => API.leagueTiebreakFinalize('c1', PW),
  planRestore: () => API.planRestore({ from: 'b.zip' }, PW),
  restore: () => API.restore({ from: 'b.zip' }, PW, PW),
  deleteBackup: () => API.deleteBackup('b.zip', PW, PW),
};

// The direct calls that change nothing: they say "Not sent", not "may have been saved".
const READ_ONLY = new Set(['exportPDFs', 'exportResults', 'planRestore']);

describe('a direct write that cannot be sent says one sentence', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('names the sentences the operator reads', () => {
    expect(WRITE_NOT_CONFIRMED).toBe('No answer: the connection dropped. The change may or may not have been saved. Check, then try again if it is missing.');
    expect(NOT_SENT_NO_CONNECTION).toBe('Not sent: no connection. Nothing was changed; try again once connected.');
    expect(REGISTRATION_NOT_CONFIRMED).toBe('No answer: the connection dropped. Try again; if you were already registered it will say so.');
  });

  for (const [name, call] of Object.entries(DIRECT_WRITES)) {
    it(`${name} rejects with the no-answer sentence and the original error as its cause when fetch rejects with a TypeError`, async () => {
      // Chrome's, Firefox's and Safari's own words for the same failure.
      for (const raw of ['Failed to fetch', 'NetworkError when attempting to fetch resource.', 'Load failed']) {
        global.fetch = vi.fn(() => Promise.reject(new TypeError(raw)));
        const err = await Promise.resolve().then(call).then(() => null, (e) => e);
        expect(global.fetch).toHaveBeenCalledTimes(1);
        expect(err, `${name} (${raw})`).toBeInstanceOf(Error);
        expect(err.message).toBe(READ_ONLY.has(name) ? NOT_SENT_NO_CONNECTION : WRITE_NOT_CONFIRMED);
        expect(err.noConnection).toBe(true);
        expect(err.cause).toBeInstanceOf(TypeError);
        expect(err.cause.message).toBe(raw);
        expect(isNoConnection(err)).toBe(true);
      }
    });
  }

  it('leaves a rejection that is not a lost connection as it was', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' });
    global.fetch = vi.fn(() => Promise.reject(abort));
    await expect(API.startCompetition('c1', PW)).rejects.toBe(abort);
  });

  it('turns a connection that drops while the answer body is read into the same sentence', async () => {
    const dropped = new TypeError('network error');
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(dropped) }));
    const err = await API.startCompetition('c1', PW).then(() => null, (e) => e);
    expect(err.message).toBe(WRITE_NOT_CONFIRMED);
    expect(err.noConnection).toBe(true);
    expect(err.cause).toBe(dropped);
    // The PDF export and the restore plan read only, so a dropped body says "Not sent".
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, headers: new Headers(), blob: () => Promise.reject(dropped) }));
    await expect(API.exportPDFs('all', PW)).rejects.toThrow(NOT_SENT_NO_CONNECTION);
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(dropped) }));
    await expect(API.planRestore({ from: 'b.zip' }, PW)).rejects.toThrow(NOT_SENT_NO_CONNECTION);
  });

  it('leaves a body that reads, or fails for another reason, as it was', async () => {
    const other = new Error('bad json');
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, headers: new Headers(), blob: () => Promise.resolve('B') }));
    expect((await API.exportPDFs('all', PW)).blob).toBe('B');
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.reject(other) }));
    await expect(API.startCompetition('c1', PW)).rejects.toBe(other);
  });

  it('does not change what an answered refusal says', async () => {
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false, status: 409, json: () => Promise.resolve({ error: 'Competition already started' }),
    }));
    await expect(API.startCompetition('c1', PW)).rejects.toThrow('Competition already started');
  });

  it('every POST, PUT or DELETE in API goes through _directFetch, but the seven outbox doors', () => {
    const src = readFileSync(resolve(__dirname, '..', 'api_client.jsx'), 'utf8').split('\n');
    // The whole file: the helpers above API (_backupRequest and the like) send too.
    const start = 0;
    const writeMethod = /\bmethod\s*(?::\s*(['"`])(POST|PUT|PATCH|DELETE)\1|[,}])/;
    // The doors phase 3 moves to the outbox keep their own fetch until then.
    const doors = [
      "'/api/tournament', {",                // updateTournament (PUT; createTournament's POST is direct)
      '/awards`',                            // updateCompetitionAwards
      '/checkin`',                           // toggleCheckIn
      '/checkin-bulk`',                      // bulkCheckIn
      "'/api/tournament/announce'",          // announce
      '/api/announcements/${',               // withdraw one
      "'/api/announcements', {",             // clear all
    ];
    const bare = [];
    for (let i = start; i < src.length; i++) {
      // Any bare fetch( call (awaited or returned), never _directFetch(.
      if (!/(^|[^\w.])fetch\(/.test(src[i])) continue;
      const near = src.slice(i, i + 4).join(' ');
      const write = writeMethod.exec(near);
      if (!write) continue;
      const isDoor = doors.some((d) => src[i].includes(d))
        && !(src[i].includes("'/api/tournament', {") && write[2] === 'POST');
      if (!isDoor) bare.push(`${i + 1}: ${src[i].trim()}`);
    }
    expect(bare).toEqual([]);
  });

  it('DIRECT_WRITES lists exactly the API methods that send through _directFetch', () => {
    const src = readFileSync(resolve(__dirname, '..', 'api_client.jsx'), 'utf8').split('\n');
    const start = src.findIndex((l) => l.startsWith('const API = {'));
    const sends = new Set();
    let method = null;
    for (let i = start; i < src.length; i++) {
      method = /^    (?:async )?(\w+)\(/.exec(src[i])?.[1] ?? method;
      const near = src.slice(i, i + 4).join(' ');
      // _backupRequest sends a GET bare and any other method through _directFetch.
      if (src[i].includes('_directFetch(') || (src[i].includes('_backupRequest(') && /method:\s*'(POST|PUT|PATCH|DELETE)'/.test(near))) sends.add(method);
    }
    expect([...sends].sort()).toEqual(Object.keys(DIRECT_WRITES).sort());
  });
});

describe('noConnectionError and shownMessage', () => {
  it('builds the marked error with the original failure as its cause', () => {
    const raw = new TypeError('Failed to fetch');
    const err = noConnectionError(NOT_SENT_NO_CONNECTION, raw);
    expect(err.message).toBe(NOT_SENT_NO_CONNECTION);
    expect(err.noConnection).toBe(true);
    expect(err.cause).toBe(raw);
    expect(isNoConnection(err)).toBe(true);
  });
  it('shows the sentence as it is and any other failure after the prefix', () => {
    expect(shownMessage(noConnectionError(WRITE_NOT_CONFIRMED, new TypeError('x')), 'PDF export failed: ')).toBe(WRITE_NOT_CONFIRMED);
    expect(shownMessage(new Error('bad roster'), 'Export failed: ')).toBe('Export failed: bad roster');
    expect(shownMessage('plain text', 'Export failed: ')).toBe('Export failed: plain text');
  });
});

describe('isNoConnection', () => {
  it('is a fetch TypeError, a deadline, or an error _directFetch already turned', () => {
    expect(isNoConnection(new TypeError('Failed to fetch'))).toBe(true);
    expect(isNoConnection(Object.assign(new Error('x'), { timedOut: true }))).toBe(true);
    expect(isNoConnection(Object.assign(new Error('x'), { noConnection: true }))).toBe(true);
  });
  it('is not an answered refusal, an abort or nothing', () => {
    expect(isNoConnection(Object.assign(new Error('Refused'), { status: 409 }))).toBe(false);
    expect(isNoConnection(Object.assign(new Error('aborted'), { name: 'AbortError' }))).toBe(false);
    expect(isNoConnection(null)).toBe(false);
    expect(isNoConnection(undefined)).toBe(false);
  });
});
