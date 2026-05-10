// Status enum mapping: backend uses "completed"/"running"/"scheduled"
const STATUS_MAP = { "complete": "completed", "in_progress": "running" };
const STATUS_REVERSE = { "completed": "complete", "in_progress": "running" };

function toBackendStatus(s) { return STATUS_MAP[s] || s; }

// Translate UI score patch into backend MatchResult shape.
// UI sends: { winner: {id,name,...}, status, score: {type,winnerPts,loserPts,ippons,fouls,...} }
// Backend expects: { winner: string, ipponsA: [], ipponsB: [], hansokuA: int, hansokuB: int, decision: "", status: "completed"|"running"|"scheduled" }
function toBackendMatchResult(patch, match) {
    const sideAName = typeof match?.sideA === "object" ? match.sideA?.name : match?.sideA;
    const sideBName = typeof match?.sideB === "object" ? match.sideB?.name : match?.sideB;
    const winnerName = patch.winner ? (typeof patch.winner === "object" ? patch.winner.name : patch.winner) : "";

    const score = patch.score || {};
    const ipponsA = (patch.ipponsA || []).filter(x => x !== "•");
    const ipponsB = (patch.ipponsB || []).filter(x => x !== "•");

    const fouls = score.fouls || {};
    const result = {
        sideA: sideAName || "",
        sideB: sideBName || "",
        winner: winnerName,
        ipponsA,
        ipponsB,
        hansokuA: fouls.a || 0,
        hansokuB: fouls.b || 0,
        decision: score.type === "hikiwake" || score.type === "hikewake" ? "hikewake" : "",
        status: toBackendStatus(patch.status || "scheduled"),
    };
    if (patch.subResults) {
        result.subResults = patch.subResults;
    }
    return result;
}

// Normalize a backend match (string sideA/sideB) into UI shape (object sideA/sideB).
// Also normalizes score fields so bracket.js MatchCard can display them.
function normalizeMatch(m, playerMap) {
    if (!m) return m;
    const norm = { ...m };
    // Normalize sideA/sideB from string to {id, name}
    if (typeof norm.sideA === "string" && norm.sideA) {
        const p = playerMap?.[norm.sideA];
        norm.sideA = p || { id: norm.sideA, name: norm.sideA };
    }
    if (typeof norm.sideB === "string" && norm.sideB) {
        const p = playerMap?.[norm.sideB];
        norm.sideB = p || { id: norm.sideB, name: norm.sideB };
    }
    // Normalize winner from string to object
    if (typeof norm.winner === "string" && norm.winner) {
        const p = playerMap?.[norm.winner];
        norm.winner = p || { id: norm.winner, name: norm.winner };
    }
    // Build score object from flat scoreA/scoreB if needed (bracket matches)
    if (!norm.score && (norm.scoreA || norm.scoreB) && norm.status === "completed") {
        const aLen = (norm.scoreA || "").replace(/\s*\(H\d+\)/g, "").length;
        const bLen = (norm.scoreB || "").replace(/\s*\(H\d+\)/g, "").length;
        const aWin = norm.winner && norm.sideA && (typeof norm.winner === "object" ? norm.winner.name : norm.winner) === (typeof norm.sideA === "object" ? norm.sideA.name : norm.sideA);
        norm.score = {
            type: "ippon",
            winnerPts: aWin ? aLen : bLen,
            loserPts: aWin ? bLen : aLen,
            ippons: aWin ? (norm.scoreA || "").split("") : (norm.scoreB || "").split(""),
        };
    }
    // Build score from ipponsA/ipponsB for pool matches
    if (!norm.score && (norm.ipponsA?.length || norm.ipponsB?.length) && norm.status === "completed") {
        const aWin = norm.winner && norm.sideA && (typeof norm.winner === "object" ? norm.winner.name : norm.winner) === (typeof norm.sideA === "object" ? norm.sideA.name : norm.sideA);
        norm.score = {
            type: norm.decision === "hikewake" ? "hikiwake" : "ippon",
            winnerPts: aWin ? (norm.ipponsA?.length || 0) : (norm.ipponsB?.length || 0),
            loserPts: aWin ? (norm.ipponsB?.length || 0) : (norm.ipponsA?.length || 0),
            ippons: aWin ? norm.ipponsA : norm.ipponsB,
        };
    }
    return norm;
}

// Build a player lookup map from competition data
function buildPlayerMap(comp) {
    const map = {};
    const add = (p) => {
        const norm = normalizePlayer(p);
        if (norm.name) map[norm.name] = { id: norm.name, name: norm.name, dojo: norm.dojo || "", seed: norm.seed ?? 0 };
    };
    if (comp?.config?.players) comp.config.players.forEach(add);
    if (comp?.players) comp.players.forEach(add);
    if (comp?.pools) {
        comp.pools.forEach(pool => {
            (pool.players || pool.Players || []).forEach(add);
        });
    }
    return map;
}

// Normalize a Go helper.Player (uppercase fields) to frontend shape (lowercase)
function normalizePlayer(p) {
    if (!p) return p;
    if (p.name !== undefined) return p;
    return { name: p.Name || "", displayName: p.DisplayName || "", dojo: p.Dojo || "", seed: p.Seed || 0, number: p.Number || "", tag: p.Tag || "" };
}

// Normalize an entire competition detail response from the viewer API.
// Returns a new object; the input is not mutated.
function normalizeCompetitionDetail(data) {
    if (!data) return data;

    const result = { ...data };

    // Normalize config.players (Go uses PascalCase, JS expects camelCase)
    if (result.config && result.config.players) {
        result.config = { ...result.config, players: result.config.players.map(p => {
            const norm = normalizePlayer(p);
            // Preserve id and seed null (normalizePlayer maps Seed:0 → seed:0, but JS uses null for "not seeded")
            return { ...norm, id: p.id || norm.id, seed: p.Seed || p.seed || null };
        })};
    }

    // Normalize pools (Go: PoolName, Players → poolName, players)
    if (result.pools) {
        result.pools = result.pools.map(p => ({
            poolName: p.PoolName || p.poolName || "",
            players: (p.Players || p.players || []).map(normalizePlayer),
            matches: p.Matches || p.matches || [],
        }));
    }

    // Normalize standings player field
    if (result.standings) {
        const standings = {};
        for (const key of Object.keys(result.standings)) {
            standings[key] = result.standings[key].map(s => ({
                ...s,
                player: normalizePlayer(s.player),
            }));
        }
        result.standings = standings;
    }

    const playerMap = buildPlayerMap(result);

    if (result.poolMatches) {
        result.poolMatches = result.poolMatches.map(m => normalizeMatch(m, playerMap));
    }
    if (result.bracket && result.bracket.rounds) {
        result.bracket = { ...result.bracket, rounds: result.bracket.rounds.map(round =>
            round.map(m => normalizeMatch(m, playerMap))
        )};
    }
    return result;
}

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        return res.json();
    },
    async fetchCompetitions() {
        const res = await fetch('/api/viewer/competitions');
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        return comps.map(c => ({
            ...c,
            players: (c.players || []).map(normalizePlayer),
        }));
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async createTournament(config) {
        const res = await fetch('/api/tournament', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json();
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
        return res.json();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: {
                'X-Tournament-Password': password
            }
        });
        return res.ok;
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
                source.close();
                source = null;
                if (!cancelled) retryTimer = setTimeout(connect, 5000);
            };
        };

        connect();

        return () => {
            cancelled = true;
            clearTimeout(retryTimer);
            source?.close();
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
        return res.ok;
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
        return res.ok;
    },
    async resetOverrides(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/overrides`, {
            method: 'DELETE',
            headers: {
                'X-Tournament-Password': password
            }
        });
        return res.ok;
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
        return res.ok;
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
        return res.ok;
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
        return res.ok;
    },
    async importCompetitions(formData, password) {
        const res = await fetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: formData
        });
        const body = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(body.error || 'Import failed');
        return body;
    }
};

export { toBackendMatchResult, normalizeMatch, buildPlayerMap, normalizePlayer, normalizeCompetitionDetail, API };

if (typeof window !== 'undefined') {
    window.API = API;
    window.normalizeMatch = normalizeMatch;
    window.normalizeCompetitionDetail = normalizeCompetitionDetail;
    window.buildPlayerMap = buildPlayerMap;
    window.toBackendStatus = toBackendStatus;
}
