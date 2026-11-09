// A write that stays direct (not in the outbox) says ONE plain sentence when it
// cannot be sent: write_result.jsx's NOT_SENT_NO_CONNECTION, thrown by
// api_client.jsx's _directFetch for a fetch that rejects with a TypeError
// (whatever the browser's own words for it). The test calls every direct write
// in API with fetch rejecting, and pins the list against the source so a new
// direct write cannot ship with the browser's raw "Failed to fetch".
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { API } from '../api_client.jsx';
import { NOT_SENT_NO_CONNECTION, isNoConnection } from '../write_result.jsx';

const PW = 'pw';

// Every direct write function in API, called the way a screen calls it. The
// seven doors the outbox takes over (check-in, bulk check-in, the three
// announcement doors, the tournament save, the awards save) are not here.
const DIRECT_WRITES = {
  resetPassword: () => API.resetPassword('new-password', 'tab-1'),
  createTournament: () => API.createTournament({ name: 'T' }, PW),
  createCompetition: () => API.createCompetition({ name: 'C' }, PW),
  updateCompetition: () => API.updateCompetition('c1', { name: 'C' }, PW),
  exportPDFs: () => API.exportPDFs('all', PW),
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

describe('a direct write that cannot be sent says one sentence', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('names the sentence the operator reads', () => {
    expect(NOT_SENT_NO_CONNECTION).toBe('Not sent: no connection. Nothing was changed; try again once connected.');
  });

  for (const [name, call] of Object.entries(DIRECT_WRITES)) {
    it(`${name} rejects with exactly that sentence when fetch rejects with a TypeError`, async () => {
      // Chrome's, Firefox's and Safari's own words for the same failure.
      for (const raw of ['Failed to fetch', 'NetworkError when attempting to fetch resource.', 'Load failed']) {
        global.fetch = vi.fn(() => Promise.reject(new TypeError(raw)));
        const err = await Promise.resolve().then(call).then(() => null, (e) => e);
        expect(global.fetch).toHaveBeenCalledTimes(1);
        expect(err, `${name} (${raw})`).toBeInstanceOf(Error);
        expect(err.message).toBe(NOT_SENT_NO_CONNECTION);
        expect(err.noConnection).toBe(true);
        expect(isNoConnection(err)).toBe(true);
      }
    });
  }

  it('leaves a rejection that is not a lost connection as it was', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' });
    global.fetch = vi.fn(() => Promise.reject(abort));
    await expect(API.startCompetition('c1', PW)).rejects.toBe(abort);
  });

  it('does not change what an answered refusal says', async () => {
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false, status: 409, json: () => Promise.resolve({ error: 'Competition already started' }),
    }));
    await expect(API.startCompetition('c1', PW)).rejects.toThrow('Competition already started');
  });

  it('every POST, PUT or DELETE in API goes through _directFetch, but the seven outbox doors', () => {
    const src = readFileSync(resolve(__dirname, '..', 'api_client.jsx'), 'utf8').split('\n');
    const start = src.findIndex((l) => l.startsWith('const API = {'));
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
      const write = /method:\s*'(POST|PUT|DELETE)'/.exec(near);
      if (!write) continue;
      const isDoor = doors.some((d) => src[i].includes(d))
        && !(src[i].includes("'/api/tournament', {") && write[1] === 'POST');
      if (!isDoor) bare.push(`${i + 1}: ${src[i].trim()}`);
    }
    expect(bare).toEqual([]);
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
