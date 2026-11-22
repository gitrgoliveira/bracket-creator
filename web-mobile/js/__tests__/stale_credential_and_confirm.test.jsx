// bc-hnam: a stale saved password and the request-only public-names
// confirmation.
//  - A read the server refuses for the organiser's password (401) tells the
//    page and throws an error the page never reads as "no tournament".
//  - confirmPublicNames rides a settings save but is never part of the
//    document: a queued save's overlay on a fetched tournament drops it.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

describe('a read refused for the organiser\'s password', () => {
    let origFetch;
    beforeEach(() => { origFetch = global.fetch; });
    afterEach(() => {
        global.fetch = origFetch;
        delete window.credentialRefused;
        vi.resetModules();
    });

    async function load(kind, password, status) {
        vi.resetModules();
        global.fetch = vi.fn(async () => ({
            ok: false, status, headers: { get: () => null }, json: async () => ({ error: 'Invalid password' }),
        }));
        const requests = await import('../api_client_requests.jsx');
        const api = await import('../api_client.jsx');
        requests.setReadAudience(kind, password);
        return { requests, API: api.API };
    }

    it('tells the page and throws the refusal, for the tournament and the competitions', async () => {
        const refused = vi.fn();
        window.credentialRefused = refused;
        const { requests, API } = await load('admin', 'stale', 401);
        await expect(API.fetchTournament()).rejects.toMatchObject({ code: requests.CREDENTIAL_REFUSED, message: 'Invalid password' });
        await expect(API.fetchCompetitions()).rejects.toMatchObject({ code: requests.CREDENTIAL_REFUSED });
        await expect(API.fetchCompetitions({ bounded: true })).rejects.toMatchObject({ code: requests.CREDENTIAL_REFUSED });
        expect(refused).toHaveBeenCalledTimes(3);
    });

    it('is an ordinary failure on a public page, which sends no password', async () => {
        const refused = vi.fn();
        window.credentialRefused = refused;
        const { requests, API } = await load('viewer', '', 401);
        await expect(API.fetchTournament()).rejects.toThrow('Invalid password');
        await expect(API.fetchTournament()).rejects.not.toMatchObject({ code: requests.CREDENTIAL_REFUSED });
        expect(refused).not.toHaveBeenCalled();
    });

    it('is an ordinary failure for a server error', async () => {
        const refused = vi.fn();
        window.credentialRefused = refused;
        const { API } = await load('admin', 'pw', 500);
        await expect(API.fetchTournament()).rejects.toThrow();
        expect(refused).not.toHaveBeenCalled();
    });
});

describe('a queued tournament save laid over the fetched copy', () => {
    it('never carries the request-only confirmation', async () => {
        const { createDocumentVersions } = await import('../document_versions.jsx');
        const payload = { name: 'New name', confirmPublicNames: true, defaultPublicNames: 'person', password: 'x', tournamentVersion: 'v1' };
        const { overlayPendingTournament } = createDocumentVersions({
            entriesFor: () => [['tournament\u001fa', { payload }]],
            isInFlight: () => false,
            persistQueue: () => {},
        });
        const out = overlayPendingTournament({ name: 'Old name', venue: 'Hall' });
        expect(out).toEqual({ name: 'New name', venue: 'Hall', defaultPublicNames: 'person' });
        expect('confirmPublicNames' in out).toBe(false);
    });
});
