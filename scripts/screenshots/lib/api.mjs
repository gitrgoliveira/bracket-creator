// The API is allowed to do the SCAFFOLDING only.
//
// Operator ruling (bc-shot): a capture must show state the application itself
// produced, so anything carrying member numbers, bout rows or points is entered
// through the interface (see ui.mjs). Writing those over HTTP drops member ids
// and bout points, which silently removes number chips and zeroes PW/PL - it
// has shipped a misleading screenshot before. Creating a tournament, a
// competition, its participants and its draw is not that: those calls are the
// same ones the client makes, with no per-fighter detail to lose.
export const PASSWORD = 'testpassword';

export function client(base) {
  const call = async (method, p, body, auth = true) => {
    const res = await fetch(base + p, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(auth ? { 'X-Tournament-Password': PASSWORD } : {}),
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
        // A competition with no start time renders its subtitle as
        // "<date> at * <courts>" - a dangling "at" with nothing after it, which
        // shipped into four captures. Defaulted here rather than at the call
        // sites so a new recipe cannot reintroduce it; override per recipe when
        // the time itself matters.
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
    addMember: (id, tid, name) =>
      call('POST', `/api/competitions/${id}/teams/${tid}/members`, { name }),
  };
}

// Roster helpers. Dojos must be non-blank: a blank one is refused by the save
// floor (state.ErrBlankDojo) and again by the tree-aware draw.
export function roster(names, dojo) {
  return names.map((name) => ({ name, dojo }));
}
