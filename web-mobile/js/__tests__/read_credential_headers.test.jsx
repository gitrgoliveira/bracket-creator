// bc-hnam: every read a page makes goes as the page reads. The organiser's
// password (or a reveal key) rides on the team reads too, so a hidden-names
// competition is not masked for the signed-in organiser; and a stale password
// draws the one credential refusal from every read, never a plain failure.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

describe('reads made as the page reads', () => {
    let origFetch;
    beforeEach(() => { origFetch = global.fetch; });
    afterEach(() => {
        global.fetch = origFetch;
        delete window.credentialRefused;
        vi.resetModules();
    });

    async function load(kind, password, answer) {
        vi.resetModules();
        const fetchMock = vi.fn(async () => ({
            headers: { get: () => null }, ...answer,
        }));
        global.fetch = fetchMock;
        const requests = await import('../api_client_requests.jsx');
        const api = await import('../api_client.jsx');
        requests.setReadAudience(kind, password);
        return { requests, API: api.API, fetchMock };
    }

    const ok = (body) => ({ ok: true, status: 200, json: async () => body });
    const refused = { ok: false, status: 401, json: async () => ({ error: 'Invalid password' }) };

    const sentTo = (fetchMock, pattern) => fetchMock.mock.calls.find(([url]) => new RegExp(pattern).test(String(url)))[1];

    it('sends the organiser\'s password with the kachinuki roster read', async () => {
        const { API, fetchMock } = await load('admin', 'pw', ok({ sideA: [], sideB: [] }));
        await API.fetchKachinukiRoster('c1', 'm1');
        expect(sentTo(fetchMock, 'kachinuki-roster|fielded-lineup').headers).toMatchObject({ 'X-Tournament-Password': 'pw' });
    });

    it('sends a reveal key with the fielded lineup read, beside its content type', async () => {
        const { API, fetchMock } = await load('admin', 'pw', ok({ fieldedMemberIds: { 1: 'x' } }));
        await API.fetchFieldedLineup('c1', 't1', 'm1', { positions: {}, memberIds: {} });
        expect(sentTo(fetchMock, 'kachinuki-roster|fielded-lineup').headers).toMatchObject({ 'Content-Type': 'application/json', 'X-Tournament-Password': 'pw' });
    });

    it('sends no credential header from a display page', async () => {
        const { API, fetchMock } = await load('display', '', ok({ sideA: [], sideB: [] }));
        await API.fetchKachinukiRoster('c1', 'm1');
        expect(sentTo(fetchMock, 'kachinuki-roster|fielded-lineup').headers).toEqual({});
    });

    it('drops a stale password on every read that sends it', async () => {
        const reads = {
            courtMatches: (API) => API.fetchCourtMatches('A'),
            competitionDetails: (API) => API.fetchCompetitionDetails('c1'),
            swissStandings: (API) => API.swissStandings('c1'),
            leagueStandings: (API) => API.leagueStandings('c1'),
            competitorStatuses: (API) => API.fetchCompetitorStatuses('c1'),
            leagueTiebreakCandidates: (API) => API.leagueTiebreakCandidates('c1'),
            teamLineup: (API) => API.fetchTeamLineup('c1', 't1', 1),
        };
        for (const [name, read] of Object.entries(reads)) {
            const credentialRefused = vi.fn();
            window.credentialRefused = credentialRefused;
            const { requests, API } = await load('admin', 'stale', refused);
            await expect(read(API), name).rejects.toMatchObject({ code: requests.CREDENTIAL_REFUSED });
            expect(credentialRefused, name).toHaveBeenCalledTimes(1);
        }
    });

    it('keeps the status of a refused competition read, for the page\'s data-issue banner', async () => {
        const { API } = await load('viewer', '', { ok: false, status: 404, json: async () => ({ error: 'not found' }) });
        await expect(API.fetchCompetitionDetails('c1')).rejects.toMatchObject({ status: 404, message: 'not found' });
    });
});
