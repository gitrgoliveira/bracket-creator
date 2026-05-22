// Request/response serializers for the mobile-app API client.
//
// These are pure helpers — no fetch, no DOM, no globals. Split out of
// api.jsx (T007 / NFR-006) so api_client.jsx can stay focused on HTTP.
// Importers (admin.jsx, viewer.jsx, score editor, etc.) consume these
// via the original api.jsx re-export shim or directly from this module.
//
// Status enum mapping: the backend uses "completed" / "running" /
// "scheduled". The UI carries shorter labels ("complete" / "in_progress")
// in some payloads — STATUS_MAP translates those at the boundary.
//
// Match shape conversion: the UI keeps sideA/sideB as objects ({id,name})
// and stores ippons as an array of arbitrary tokens (incl. "•" for
// no-strike placeholder). The Go backend keeps sides as strings (player
// names — names are unique within a competition) and stores hansoku as
// integers. toBackendMatchResult() bridges those representations.
//
// normalizeMatch() goes the other way: take a match as the Go server
// emits it (string sides, flat scoreA/scoreB or ipponsA/ipponsB arrays)
// and produce the UI-friendly shape with object sides and a unified
// `score` object the bracket card renderer can consume.

const STATUS_MAP = { "complete": "completed", "in_progress": "running" };

function toBackendStatus(s) { return STATUS_MAP[s] || s; }

// Canonical draw value is "hikiwake". See specs/openapi.yaml for details.
function isHikiwake(v) { return v === "hikiwake"; }
function isKikenDecision(v) { return v === "kiken" || v === "kiken-voluntary" || v === "kiken-injury"; }

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
        decision: isHikiwake(score.type) ? "hikiwake" : "",
        status: toBackendStatus(patch.status || "scheduled"),
    };
    if (patch.subResults) {
        result.subResults = patch.subResults;
    }
    // FR-033: encho metadata round-trips so the (E) suffix persists. The
    // backend in Slice 1 (T039) accepts the field passively — Slice 3 wires
    // the decision/kiken/fusenpai semantics, but we already keep the
    // periodCount alongside the score so re-edits and history retain it.
    if (patch.encho && patch.encho.periodCount > 0) {
        result.encho = { periodCount: patch.encho.periodCount };
    }
    // mp-6di: judges' decision (hantei) is a top-level flag on
    // MatchResult.DecidedByHantei. Forward it so the backend round-trips
    // the HT suffix in the viewer and the bracket DecidedByHantei mirror.
    // Explicitly forward `false` too so a re-edit can clear a previously
    // hantei-decided match.
    if (typeof patch.decidedByHantei === "boolean") {
        result.decidedByHantei = patch.decidedByHantei;
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
    } else if (!norm.sideA) {
        norm.sideA = { id: "", name: "" };
    }
    if (typeof norm.sideB === "string" && norm.sideB) {
        const p = playerMap?.[norm.sideB];
        norm.sideB = p || { id: norm.sideB, name: norm.sideB };
    } else if (!norm.sideB) {
        norm.sideB = { id: "", name: "" };
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
            type: isHikiwake(norm.decision) ? "hikiwake" : "ippon",
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

export { toBackendStatus, isHikiwake, isKikenDecision, toBackendMatchResult, normalizeMatch, buildPlayerMap, normalizePlayer, normalizeCompetitionDetail };

if (typeof window !== 'undefined') {
    window.toBackendStatus = toBackendStatus;
    window.isHikiwake = isHikiwake;
    window.isKikenDecision = isKikenDecision;
    window.normalizeMatch = normalizeMatch;
    window.normalizeCompetitionDetail = normalizeCompetitionDetail;
    window.buildPlayerMap = buildPlayerMap;
}
