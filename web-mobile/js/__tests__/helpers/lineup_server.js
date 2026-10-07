// lineup_server.js: a server that follows the lineup save contract (operator
// decision 2026-10-07, "Only changed positions"), for the tests that run the
// real API client, or an editor over a stubbed one, against it.
//
// PUT .../match-lineups/:matchId and PUT .../lineups/:round take
// { positions, memberIds?, changed? }:
//  - `changed` absent: the body is the whole lineup and replaces the stored one.
//  - `changed` present (a non-empty array of position keys): only those keys of
//    positions and memberIds are read, whatever else the body carries. positions[p]
//    must be there (else 400); name "" with no id clears the position, anything else
//    sets it (the id is dropped when memberIds has none for p). The changes land on
//    the lineup the server holds when the write arrives: a match's own lineup, else the
//    lineup in force there (the carry rule), else an empty one.
//  - one member at two positions of the composed lineup is a 400 naming both.
// The answer is the whole lineup as stored after the write.
import { vi } from 'vitest';

const has = (object, key) => Object.prototype.hasOwnProperty.call(object || {}, key);

// applyLineupPut: the contract's composition, on its own: `base` is `{ positions,
// memberIds }` as the server holds it, `body` is what was sent. Answers
// { status, body }: the composed lineup on 200, `{ error }` on 400.
export function applyLineupPut(base, body) {
  const positions = { ...(base && base.positions) };
  const memberIds = { ...(base && base.memberIds) };
  if (body.changed === undefined) {
    Object.keys(positions).forEach((key) => delete positions[key]);
    Object.keys(memberIds).forEach((key) => delete memberIds[key]);
    Object.assign(positions, body.positions || {});
    Object.assign(memberIds, body.memberIds || {});
  } else {
    if (!Array.isArray(body.changed) || body.changed.length === 0) {
      return { status: 400, body: { error: 'a save that names changed positions names at least one' } };
    }
    for (const key of body.changed) {
      if (!has(body.positions, key)) return { status: 400, body: { error: `positions holds nothing for the changed position ${key}` } };
    }
    for (const key of body.changed) {
      const name = body.positions[key];
      const id = (body.memberIds && body.memberIds[key]) || '';
      if (name === '' && id === '') {
        delete positions[key];
        delete memberIds[key];
      } else {
        positions[key] = name;
        if (id) memberIds[key] = id; else delete memberIds[key];
      }
    }
  }
  const at = {};
  for (const [key, id] of Object.entries(memberIds)) {
    if (!id) continue;
    if (at[id]) return { status: 400, body: { error: `member ${id} is at positions ${at[id]} and ${key}` } };
    at[id] = key;
  }
  return { status: 200, body: { positions, memberIds } };
}

// lineupPutStub: a stand-in for API.putMatchLineup / API.putTeamLineup, for the tests
// that drive an editor over a stubbed API. It follows the contract over `held`, the
// lineup the server holds ({ positions, memberIds }): a save with `changed` lands its
// changed positions on it, one without replaces it. `held` is updated by every save
// that lands, which is what the next save then composes on, and a test sets it to what
// another device saved. The answer is the whole lineup the server then holds; a save
// the server refuses (one member at two positions) rejects with its message, as the
// API does.
export function lineupPutStub(held) {
  return vi.fn((_comp, _team, _scope, positions, _password, memberIds, changed) => {
    const done = applyLineupPut(held, { positions, memberIds, changed });
    if (done.status !== 200) return Promise.reject(new Error(done.body.error));
    held.positions = done.body.positions;
    held.memberIds = done.body.memberIds;
    return Promise.resolve({ positions: { ...held.positions }, memberIds: { ...held.memberIds } });
  });
}

// lineupPutStubByTeam: the same for a sheet that writes the lineups of both its teams.
// `lineups` is a function that answers the map of what the server holds, by team id,
// each `{ positions, memberIds }` (a function, so a test that starts a new map in
// its beforeEach is followed). A team holding nothing yet is empty.
export function lineupPutStubByTeam(lineups) {
  return vi.fn((_comp, teamId, _match, positions, _password, memberIds, changed) => {
    const map = lineups();
    const done = applyLineupPut(map[teamId], { positions, memberIds, changed });
    if (done.status !== 200) return Promise.reject(new Error(done.body.error));
    map[teamId] = done.body;
    return Promise.resolve({ positions: { ...done.body.positions }, memberIds: { ...done.body.memberIds } });
  });
}

const reply = (status, body) => ({ ok: status >= 200 && status < 300, status, json: () => Promise.resolve(body) });

// makeLineupServer: the routes of one team in one competition over a fetch stub.
//   own      the lineups saved for a match, by match id (a match's own lineup)
//   starting the team's starting lineup (round 0), or null
//   carried  the lineup in force at a match that has none of its own, by match id
//   members  the answer of GET team-members
// `offline` makes every request reject like a connection that is down. `puts` lists
// the bodies of the PUTs that reached it, with their urls.
export function makeLineupServer({ comp = 'c1', team = 't1', own = {}, starting = null, carried = {}, members = {} } = {}) {
  const server = {
    comp, team, own: { ...own }, starting, carried: { ...carried }, members, offline: false, puts: [],
  };
  const base = `/api/competitions/${comp}/teams/${team}`;
  const withIds = (lineup) => ({ positions: { ...lineup.positions }, memberIds: { ...lineup.memberIds } });
  const whole = (lineup, extra) => ({ teamId: team, competitionId: comp, ...withIds(lineup), saved: true, ...extra });
  const inForce = (matchId) => server.own[matchId] || server.carried[matchId] || null;

  server.fetch = vi.fn((url, opts = {}) => {
    if (server.offline) return Promise.reject(new TypeError('Failed to fetch'));
    const method = opts.method || 'GET';
    const path = String(url).split('?')[0];
    if (path === '/api/time') return Promise.resolve(reply(200, { nowMs: Date.now() }));
    if (path === `/api/competitions/${comp}/team-members`) return Promise.resolve(reply(200, { teamMembers: server.members }));
    let m = path.match(new RegExp(`^${base}/lineup-in-force/([^/]+)$`));
    if (m && method === 'GET') {
      const lineup = inForce(decodeURIComponent(m[1]));
      return Promise.resolve(reply(200, lineup ? whole(lineup, { matchId: m[1] }) : { teamId: team, positions: {}, saved: false }));
    }
    m = path.match(new RegExp(`^${base}/lineups/(\\d+)$`));
    if (m && method === 'GET') {
      return Promise.resolve(reply(200, server.starting ? whole(server.starting, { round: Number(m[1]) }) : { teamId: team, positions: {}, saved: false }));
    }
    if (m && method === 'PUT') {
      const body = JSON.parse(opts.body);
      server.puts.push({ url: path, body });
      const done = applyLineupPut(server.starting, body);
      if (done.status === 200) server.starting = done.body;
      return Promise.resolve(reply(done.status, done.status === 200 ? whole(done.body, { round: Number(m[1]) }) : done.body));
    }
    m = path.match(new RegExp(`^${base}/match-lineups/([^/]+)$`));
    if (m && method === 'PUT') {
      const matchId = decodeURIComponent(m[1]);
      const body = JSON.parse(opts.body);
      server.puts.push({ url: path, body });
      const done = applyLineupPut(inForce(matchId), body);
      if (done.status === 200) server.own[matchId] = done.body;
      return Promise.resolve(reply(done.status, done.status === 200 ? whole(done.body, { matchId }) : done.body));
    }
    return Promise.resolve(reply(404, { error: `no route for ${method} ${path}` }));
  });
  return server;
}
