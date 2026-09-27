// The API is allowed to do the SCAFFOLDING only.
//
// Operator ruling (bc-shot): a capture must show state the application itself
// produced, so anything carrying member numbers, bout rows or points is entered
// through the interface (see ui.mjs). Writing those over HTTP drops member ids
// and bout points, which silently removes number chips and zeroes PW/PL - it
// has shipped a misleading screenshot before. Creating a tournament, a
// competition, its participants and its draw is not that: those calls are the
// same ones the client makes, with no per-fighter detail to lose. Nor is a
// lineup written through nameMembers + lineup below: it names the draw's own
// numbered members and carries their ids, exactly the request the client
// sends, and fixture.mjs's assertLineupIds re-reads it before a capture.
export const PASSWORD = 'testpassword';

// `headers` lets a caller add one: a self-run tournament gates roster and draw
// mutations behind a second X-Admin-Password header (middleware.go's
// RequireElevatedPassword), and the recipe that needs it configures this
// client rather than re-implementing the call.
export function client(base, headers = {}) {
  const call = async (method, p, body, auth = true) => {
    const res = await fetch(base + p, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(auth ? { 'X-Tournament-Password': PASSWORD } : {}),
        ...headers,
      },
      body: body != null ? JSON.stringify(body) : undefined,
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${p} -> ${res.status} ${text.slice(0, 200)}`);
    return text.trim() ? JSON.parse(text) : null;
  };

  return {
    call,
    get: (p) => call('GET', p),
    post: (p, b) => call('POST', p, b),
    put: (p, b) => call('PUT', p, b),

    async tournament(over = {}) {
      return call('POST', '/api/tournament', {
        name: 'Kendo Tournament',
        date: '10-05-2026',
        venue: 'London',
        durationDays: 1,
        courts: ['A', 'B', 'C'],
        password: PASSWORD,
        ...over,
      }, false);
    },

    // teamSize follows the API contract: 0 = individual, >0 = team. kind is
    // derived from it so the two can never disagree (setup_tournament.py makes
    // the same point: defaulting to 1 would read as a one-person team comp).
    async competition(id, name, over = {}) {
      const teamSize = over.teamSize ?? 0;
      await call('POST', '/api/competitions', {
        id,
        name,
        kind: teamSize > 0 ? 'team' : 'individual',
        format: 'mixed',
        poolSize: 3,
        poolWinners: 2,
        roundRobin: true,
        courts: ['A'],
        withZekkenName: false,
        numberPrefix: '',
        // Gives every captured competition a start time, as a real
        // tournament's would have. (Without one it used to render a dangling
        // "<date> at ·", which shipped into four captures; the application no
        // longer does that.) Override per recipe when the time itself matters.
        startTime: '09:00',
        date: '',
        status: 'setup',
        ...over,
        teamSize,
      });
      return id;
    },

    participants: (id, players) =>
      call('POST', `/api/competitions/${id}/participants`, { players }),

    generateDraw: (id) => call('POST', `/api/competitions/${id}/generate-draw`),
    start: (id) => call('POST', `/api/competitions/${id}/start`),
    viewer: (id) => call('GET', `/api/viewer/competitions/${id}`),

    // Name a drawn team's people the way the Lineups page does: by naming the
    // numbered blank members the draw seeded (teamSize + state.SquadReserveSlots
    // of them, internal/state/squad.go), in number order, so they read 1..n as
    // a real team's do. Adding members instead mints new ones after the blanks,
    // which is how the captures came to show "K2.8" for a first fighter.
    // Returns the named members, ids included, for a lineup written by id.
    async nameMembers(id, tid, names) {
      const { teamMembers } = await call('GET', `/api/competitions/${id}/team-members`);
      const blanks = teamMembers[tid].filter((m) => !m.name).sort((a, b) => a.index - b.index);
      const named = [];
      for (const [i, name] of names.entries()) {
        await call('PUT', `/api/competitions/${id}/teams/${tid}/members/${blanks[i].id}`, { name });
        named.push({ ...blanks[i], name });
      }
      return named;
    },

    // Write a round-0 lineup carrying BOTH each position's name and its member
    // id, the shape the client writes (LineupRequest.MemberIDs,
    // handlers_lineup.go) and the only one that renders a competitor-number
    // chip: the chip resolves through the member id, so a name-only lineup
    // shows no number. Lineups must land BEFORE any bout is recorded; a bout
    // freezes the names it was fought under.
    async lineup(id, tid, members) {
      const positions = {};
      const memberIds = {};
      members.slice(0, POSITIONS.length).forEach((m, i) => {
        positions[POSITIONS[i]] = m.name;
        memberIds[POSITIONS[i]] = m.id;
      });
      return call('PUT', `/api/competitions/${id}/teams/${tid}/lineups/0`, { positions, memberIds });
    },
  };
}

// The five FIK fighting-order positions, in sheet order.
export const POSITIONS = ['senpo', 'jiho', 'chuken', 'fukusho', 'taisho'];

// Roster helpers. Dojos must be non-blank: a blank one is refused by the save
// floor (state.ErrBlankDojo) and again by the tree-aware draw.
export function roster(names, dojo) {
  return names.map((name) => ({ name, dojo }));
}
