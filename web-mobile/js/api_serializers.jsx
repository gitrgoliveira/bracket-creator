// Request/response serializers for the mobile-app API client.
//
// These are pure helpers : no fetch, no DOM, no globals. Split out of
// api.jsx (T007 / NFR-006) so api_client.jsx can stay focused on HTTP.
// Importers (admin.jsx, viewer.jsx, score editor, etc.) consume these
// via the original api.jsx re-export shim or directly from this module.
//
// Status enum mapping: the backend uses "completed" / "running" /
// "scheduled". The UI carries shorter labels ("complete" / "in_progress")
// in some payloads : STATUS_MAP translates those at the boundary.
//
// Match shape conversion: the UI keeps sideA/sideB as objects ({id,name})
// and stores ippons as an array of arbitrary tokens (incl. "•" for
// no-strike placeholder). The Go backend keeps sides as strings (player
// names : names are unique within a competition) and stores hansoku as
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
        hansokuA: patch.hansokuA ?? fouls.a ?? 0,
        hansokuB: patch.hansokuB ?? fouls.b ?? 0,
        decision: isHikiwake(score.type) ? "hikiwake" : "",
        status: toBackendStatus(patch.status || "scheduled"),
    };
    // Carry the winner's participant id so a SAME-NAME head-to-head (the winner
    // NAME matches both sides) is unambiguous on the backend. Without it, when
    // the scoreline is tied (e.g. a hantei decision : equal ippon counts) the
    // backend can't infer the winning side and leaves WinnerID empty, and the
    // league matrix then marks BOTH same-name rows as winners. The scoring modal
    // sets patch.winner to the winning SIDE object (m.sideA/m.sideB), so its id
    // is available directly; fall back to deriving it from the match sides by
    // name only when the names are distinct (unambiguous).
    let winnerId = "";
    if (patch.winner && typeof patch.winner === "object" && patch.winner.id) {
        winnerId = patch.winner.id;
    } else if (winnerName) {
        const aId = (typeof match?.sideA === "object" ? match.sideA.id : null);
        const bId = (typeof match?.sideB === "object" ? match.sideB.id : null);
        if (winnerName === sideAName && winnerName !== sideBName) winnerId = aId || "";
        else if (winnerName === sideBName && winnerName !== sideAName) winnerId = bId || "";
    }
    if (winnerId) result.winnerId = winnerId;
    if (patch.subResults) {
        result.subResults = patch.subResults;
    }
    // mp-62vr: rep-player names for a team daihyosen/tiebreaker rep bout. Only
    // forward non-empty values : the engine preserves a prior pick on empty
    // (backfillMatchIdentity), so omitting an unset side never wipes it.
    if (patch.repPlayerA) result.repPlayerA = patch.repPlayerA;
    if (patch.repPlayerB) result.repPlayerB = patch.repPlayerB;
    // FR-033: encho metadata round-trips so the (E) suffix persists. The
    // backend in Slice 1 (T039) accepts the field passively : Slice 3 wires
    // the decision/kiken/fusenpai semantics, but we already keep the
    // periodCount alongside the score so re-edits and history retain it.
    if (patch.encho && patch.encho.periodCount > 0) {
        result.encho = { periodCount: patch.encho.periodCount };
    }
    // Include decidedByHantei when explicitly set in the patch, or when the
    // current match already has it true (to preserve it across re-edits).
    // Omit it otherwise so non-hantei payloads stay minimal.
    const explicitHantei = typeof patch.decidedByHantei === "boolean";
    if (explicitHantei) {
        result.decidedByHantei = patch.decidedByHantei;
    } else if (match?.decidedByHantei) {
        result.decidedByHantei = true;
    }
    return result;
}

// Normalize a backend match (string sideA/sideB) into UI shape (object sideA/sideB).
// Also normalizes score fields so bracket.js MatchCard can display them.
function normalizeMatch(m, playerMap) {
    if (!m) return m;
    const norm = { ...m };
    // Normalize sideA/sideB/winner from name-string to {id, name}.
    //
    // playerMap is keyed by NAME, which collapses same-name participants
    // (e.g. two "Tanaka Kenji" from different dojos : the duplicate check
    // only rejects same-name AND same-dojo) onto a single id. When the
    // server provides an explicit per-side id (m.sideAId / m.sideBId /
    // m.winnerId : populated from pool-matches.csv), it is the authoritative
    // identity and overrides the name-collapsed lookup. We clone the
    // playerMap entry before stamping the id so the shared map object isn't
    // mutated across matches.
    const resolveSide = (name, flatId) => {
        // Prefer the id-keyed entry: it carries the CORRECT dojo/number even for
        // same-name participants. The name-keyed entry collapses same-name
        // players onto one identity, so only trust it when its id matches the
        // server's flat id : otherwise we'd attach the wrong dojo to this side.
        let p = flatId ? playerMap?.[flatId] : null;
        if (!p) {
            const byName = playerMap?.[name];
            if (byName && (!flatId || byName.id === flatId)) p = byName;
        }
        const base = p ? { ...p } : { id: flatId || name, name };
        if (flatId) base.id = flatId;
        return base;
    };
    if (typeof norm.sideA === "string" && norm.sideA) {
        norm.sideA = resolveSide(norm.sideA, m.sideAId);
    } else if (!norm.sideA) {
        norm.sideA = { id: "", name: "" };
    }
    if (typeof norm.sideB === "string" && norm.sideB) {
        norm.sideB = resolveSide(norm.sideB, m.sideBId);
    } else if (!norm.sideB) {
        norm.sideB = { id: "", name: "" };
    }
    if (typeof norm.winner === "string" && norm.winner) {
        norm.winner = resolveSide(norm.winner, m.winnerId);
    }
    // Build score object from flat scoreA/scoreB if needed (bracket matches)
    if (!norm.score && (norm.scoreA || norm.scoreB) && norm.status === "completed") {
        // Strip the trailing "(HN)" hansoku suffix (with optional separator
        // space : see engine/scoring.go::formatScore) before measuring length
        // or splitting into ippon chars. Without this, scoreA="MK (H1)" would
        // count length 7 and split to ["M","K"," ","(","H","1",")"], polluting
        // both the displayed score and the modal's ippon-slot seeding (which
        // falls back to score.ippons when ipponsA/B are absent for bracket
        // matches). Mirrors web-mobile/js/bracket.jsx::ipponsFromScore : 
        // kept inline to avoid load-order coupling with bracket.js (which
        // window-registers its helper LATER in the script order).
        const stripHansoku = (s) => (s || "").replace(/\s*\(H\d+\)$/, "");
        const cleanA = stripHansoku(norm.scoreA);
        const cleanB = stripHansoku(norm.scoreB);
        const aWin = norm.winner && norm.sideA && (typeof norm.winner === "object" ? norm.winner.name : norm.winner) === (typeof norm.sideA === "object" ? norm.sideA.name : norm.sideA);
        // Recover BOTH sides' waza letters into the per-side ippon arrays (when
        // the server didn't send them for bracket matches). scoreA/scoreB are
        // each formatScore(IpponsA/B) on the server : i.e. both sides' letters : 
        // so this is loss-free, unlike score.ippons which keeps only the
        // winner's. Populating these means formatIpponsScore renders technique
        // letters for BOTH competitors ("MK–D"), never the numeric fallback.
        // Only fill when absent so server-provided arrays always win.
        if (!norm.ipponsA?.length && cleanA) norm.ipponsA = cleanA.split("");
        if (!norm.ipponsB?.length && cleanB) norm.ipponsB = cleanB.split("");
        norm.score = {
            type: "ippon",
            winnerPts: aWin ? cleanA.length : cleanB.length,
            loserPts: aWin ? cleanB.length : cleanA.length,
            ippons: (aWin ? cleanA : cleanB).split(""),
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
        if (!norm.name) return;
        // Carry the FULL competitor identity so bracket/match sides resolved by
        // name (e.g. a pool finisher seeded into the knockout) show the same
        // details : dojo, zekken display name, and assigned number (e.g. "K1") : 
        // as the pool/schedule cards. Previously only {id,name,dojo,seed} were
        // carried, so a qualifier lost their number and zekken in the bracket.
        const entry = {
            id: norm.id || norm.name,
            name: norm.name,
            dojo: norm.dojo || "",
            seed: norm.seed ?? 0,
            displayName: norm.displayName || "",
            number: norm.number || "",
            source: norm.source || "",
            danGrade: norm.danGrade || "",
        };
        map[norm.name] = entry;
        // ALSO key by participant id. The name key collapses same-name
        // participants (two "Tanaka Kenji" from different dojos) onto whichever
        // was added last, so a name-only lookup can attach the WRONG dojo/number
        // to a side. A distinct id key preserves each one's correct metadata,
        // letting normalizeMatch resolve by the server's authoritative side id.
        // (UUID id keys never collide with display-name keys.)
        if (norm.id) map[norm.id] = entry;
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

// buildPlayerMetadata composes the canonical metadata array sent to the
// backend from a (danGrade, existingMeta) pair. Three-way logic:
//   - grade present → [grade, ...rest]
//   - no grade + rest exists → ["", ...rest] (preserves slot 1+ alignment)
//   - no grade + no rest → undefined (caller should omit the field entirely
//     so participants.csv doesn't gain a stray blank column)
// Shared by updateCompetition and the replace-participant flow so the column
// layout stays consistent across both write paths.
function buildPlayerMetadata(danGrade, existingMeta) {
    const rest = (existingMeta || []).slice(1);
    if (danGrade) return [danGrade, ...rest];
    if (rest.length > 0) return ["", ...rest];
    return undefined;
}

// Normalize a Go helper.Player (uppercase fields) to frontend shape (lowercase)
function normalizePlayer(p) {
    if (!p) return p;
    if (p.name !== undefined) {
        // Already camelCase : backfill danGrade from metadata if the field is absent.
        if (p.danGrade === undefined) {
            const danGrade = (p.metadata && p.metadata[0]) || "";
            return { ...p, danGrade };
        }
        return p;
    }
    const danGrade = (p.Metadata && p.Metadata[0]) || "";
    // Include the full metadata array so updateCompetition/replaceParticipant
    // can preserve metadata[1+] slots (e.g. a second dan-grade notation or
    // other extra CSV columns beyond the grade) when the player round-trips
    // through the JS layer. Note: "registered"/"manual"/"transfer" are registration
    // sources, not metadata : they are mapped to p.Source above.
    return { name: p.Name || "", displayName: p.DisplayName || "", dojo: p.Dojo || "", seed: p.Seed || 0, number: p.Number || "", source: p.Source || "", danGrade, metadata: p.Metadata || [] };
}

// Normalize an entire competition detail response from the viewer API.
// Returns a new object; the input is not mutated.
function normalizeCompetitionDetail(data) {
    if (!data) return data;

    const result = { ...data };

    // Go ships nil slices as JSON null, so a competition created via the
    // API/import flow (which don't force a courts list the way the admin
    // form does) arrives with `courts: null`. Render sites across admin
    // and viewer read `c.courts.join(...)` / `c.courts.length` directly,
    // which crash on null. Normalize to [] at this single fetch boundary
    // so no consumer has to guard individually. No code distinguishes
    // null from empty courts, so this is behavior-preserving.
    result.courts = result.courts || [];

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

export { toBackendStatus, isHikiwake, isKikenDecision, toBackendMatchResult, normalizeMatch, buildPlayerMap, normalizePlayer, normalizeCompetitionDetail, buildPlayerMetadata };

if (typeof window !== 'undefined') {
    window.toBackendStatus = toBackendStatus;
    window.isHikiwake = isHikiwake;
    window.isKikenDecision = isKikenDecision;
    window.normalizeMatch = normalizeMatch;
    window.normalizeCompetitionDetail = normalizeCompetitionDetail;
    window.buildPlayerMap = buildPlayerMap;
    window.buildPlayerMetadata = buildPlayerMetadata;
}
