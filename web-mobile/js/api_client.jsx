// HTTP client for the mobile-app API.
//
// Split out of api.jsx (T006 / NFR-006). The serializer helpers
// (normalizeMatch, normalizeCompetitionDetail, etc.) live in
// api_serializers.jsx and are imported here for use on responses.
//
// Public function signatures intentionally mirror the original API
// object so importers (admin.jsx, viewer.jsx) keep working unchanged
// when they go through the api.jsx re-export shim.
//
// Auth: mutations send an `X-Tournament-Password` header. Read endpoints
// (/api/viewer/*) are unauthenticated.
//
// Error contract: every method that throws does so with `new Error(msg)`
// where `msg` is the server-reported `error` field if present, else a
// per-method default string. Callers decide whether to .catch and toast.
//
// Empty-body methods (overridePoolRank, overrideBracketWinner,
// resetOverrides, updateMatchTime, moveMatchCourt, updateSchedule,
// deleteReservedSlot, deleteCompetition) deliberately return `true`
// rather than `res.json()` — calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.

import { normalizeCompetitionDetail, normalizePlayer, toBackendMatchResult } from './api_serializers.jsx';

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        return res.json();
    },
    async fetchCompetitions() {
        const res = await fetch('/api/viewer/competitions');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competitions (Status ${res.status})`);
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        return comps.map(item => {
            const c = item.config || item;
            const norm = {
                ...c,
                poolMatches: item.poolMatches,
                bracket: item.bracket,
                players: (c.players || []).map(normalizePlayer),
            };
            // Run normalization on the matches as well
            return normalizeCompetitionDetail(norm);
        });
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competition details (Status ${res.status})`);
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async createTournament(config) {
        const res = await fetch('/api/tournament', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create tournament");
        }
        return res.json();
    },
    async updateTournament(config, password) {
        const res = await fetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update tournament");
        }
        return res.json();
    },
    async updateCompetition(id, config, password) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition");
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await fetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create competition");
        }
        return res.json();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to start competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    subscribeToEvents(callback) {
        let source = null;
        let retryTimer = null;
        let cancelled = false;

        const connect = () => {
            if (cancelled) return;
            source = new EventSource('/api/events');
            source.onmessage = (event) => {
                try {
                    callback(JSON.parse(event.data));
                } catch (err) {
                    console.error("Error parsing SSE event:", err);
                }
            };
            source.onerror = () => {
                if (source) source.close();
                source = null;
                if (!cancelled) retryTimer = setTimeout(connect, 5000);
            };
        };

        connect();

        return () => {
            cancelled = true;
            clearTimeout(retryTimer);
            if (source) source.close();
        };
    },
    async recordScore(compID, matchID, result, password, match) {
        const payload = toBackendMatchResult(result, match || result);
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/score`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to record score");
        }
        return res.json();
    },
    async overridePoolRank(compID, poolID, playerName, rank, password) {
        const res = await fetch(`/api/competitions/${compID}/pools/${poolID}/override-rank`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ playerName, rank })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override rank");
        }
        // Backend returns 200 with empty body. Calling .json() on an
        // empty body throws SyntaxError per the Fetch spec, which would
        // surface to the user as an alert("Failed: Unexpected end of
        // JSON input") right after a successful save.
        return true;
    },
    async overrideBracketWinner(compID, matchID, winnerName, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/override-winner`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ winnerName })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override winner");
        }
        // Backend returns 200 with empty body — see overridePoolRank.
        return true;
    },
    async resetOverrides(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/overrides`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to reset overrides");
        }
        // Backend returns 204 No Content — .json() would reject.
        return true;
    },
    async updateMatchTime(compID, matchID, scheduledAt, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/time`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ scheduledAt })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update match time");
        }
        return true;
    },
    async moveMatchCourt(compID, matchID, court, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/court`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ court })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to move match court");
        }
        return true;
    },
    async updateSchedule(compID, entries, password) {
        const res = await fetch(`/api/competitions/${compID}/schedule`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(entries)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update schedule");
        }
        return true;
    },
    async addReservedSlot(compID, sourceCompID, sourceRank, password) {
        const res = await fetch(`/api/competitions/${compID}/reserved-slots`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ sourceCompID, sourceRank })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add reserved slot');
        }
        return res.json();
    },
    async deleteReservedSlot(compID, slotID, password) {
        const res = await fetch(`/api/competitions/${compID}/reserved-slots/${slotID}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete reserved slot");
        }
        return true;
    },
    async importCompetitions(formData, password) {
        const res = await fetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        return res.json();
    },
    async createPlayoff(sourceId, password) {
        const res = await fetch(`/api/competitions/${sourceId}/playoffs`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create playoff");
        }
        return res.json();
    },
    async deleteCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete competition");
        }
        return true;
    }
};

export { API };

if (typeof window !== 'undefined') {
    window.API = API;
}
