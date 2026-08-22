// Request/response serializers for the mobile-app API client.
//
// These are pure helpers: no fetch, no DOM, no globals. Split out of
// api.jsx (T007 / NFR-006) so api_client.jsx can stay focused on HTTP.
// Importers (admin.jsx, viewer.jsx, score editor, etc.) consume these
// via the original api.jsx re-export shim or directly from this module.
//
// Status enum mapping: the backend uses "completed" / "running" /
// "scheduled". The UI carries shorter labels ("complete" / "in_progress")
// in some payloads: STATUS_MAP translates those at the boundary.
//
// Match shape conversion: the UI keeps sideA/sideB as objects ({id,name})
// and stores ippons as an array of arbitrary tokens (incl. "•" for
// no-strike placeholder). The Go backend keeps sides as strings (player
// names: names are unique within a competition) and stores hansoku as
// integers. toBackendMatchResult() bridges those representations.
//
// normalizeMatch() goes the other way: take a match as the Go server
// emits it (string sides, ipponsA/ipponsB arrays — pool and bracket
// matches share this one shape; scoreA/scoreB strings never appear on
// the wire) and produce the UI-friendly shape with object sides and a
// unified `score` object the bracket card renderer can consume.

import { realIppons, hanteiDecided, placeHtForWinner, stripHt } from './result_slot.jsx';
const STATUS_MAP = { "complete": "completed", "in_progress": "running" };

function toBackendStatus(s) { return STATUS_MAP[s] || s; }

// Canonical draw value is "hikiwake". See specs/openapi.yaml for details.
function isHikiwake(v) { return v === "hikiwake"; }
function isKikenDecision(v) { return v === "kiken" || v === "kiken-voluntary" || v === "kiken-injury"; }

// Translate UI score patch into backend MatchResult shape.
// UI sends: { winner: {id,name,...}, status, score: {type,winnerPts,loserPts,ippons,fouls,...} }
// The judges'-decision verdict is recorded as the "Ht" entry in the WINNER's
// ippon list (the mark IS the record; Go: domain.HanteiMark). hanteiDecided
// is the read predicate (does this match/sub carry the mark on either
// side); placeHtForWinner is the write placement - given a winner (name, and
// optionally id) and the two sides (names, and optionally ids), it attributes
// the mark to whichever side matches (attributeWinnerSide: id-first when a
// winnerId/sideAId/sideBId triple is available, mirroring domain.
// AttributeWinnerSide - ids win over names when they disagree, since a
// same-name/different-dojo pair is exactly the case ids exist to
// disambiguate; name fallback, sideA-first, otherwise) and places it there
// (mirroring domain.AppendHantei so the mark lands in the winner's next free
// slot), or leaves both arrays untouched when the winner matches neither
// side; stripHt drops a stored mark without touching any other letter. All
// five now live in result_slot.jsx (the declared owner of the Ht slot rule)
// alongside realIppons - this file was a fourth consumer with its own
// copies, which is exactly the drift that primitive is meant to prevent.
// The MATCH-level call site below threads winnerId/sideAId/sideBId (already
// in scope); the SUB-BOUT call site does not - a numbered team bout names
// the two individual PLAYERS fielded, and player names are not unique (only
// (name, dojo) is), so sub rows carry no ids at all and that path stays on
// the name fallback exactly as before.

// Backend expects: { winner: string, ipponsA: [], ipponsB: [], hansokuA: int, hansokuB: int, decision: "", status: "completed"|"running"|"scheduled" }
function toBackendMatchResult(patch, match) {
    const sideAName = typeof match?.sideA === "object" ? match.sideA?.name : match?.sideA;
    const sideBName = typeof match?.sideB === "object" ? match.sideB?.name : match?.sideB;
    const winnerName = patch.winner ? (typeof patch.winner === "object" ? patch.winner.name : patch.winner) : "";
    // Side ids, when the match carries object-shaped sides (post-normalizeMatch
    // the common case). Used both to derive winnerId below and, further down,
    // to attribute the Ht mark's placement by id rather than name (bc-dmsr
    // follow-up: a same-name/different-dojo pair previously placed the mark by
    // NAME, sideA-first, even when the id-carrying winnerId named the other
    // side).
    const sideAId = (typeof match?.sideA === "object" ? match.sideA?.id : null) || "";
    const sideBId = (typeof match?.sideB === "object" ? match.sideB?.id : null) || "";

    const score = patch.score || {};
    const ipponsA = realIppons(patch.ipponsA);
    const ipponsB = realIppons(patch.ipponsB);

    const fouls = score.fouls || {};
    const result = {
        sideA: sideAName || "",
        sideB: sideBName || "",
        winner: winnerName,
        ipponsA,
        ipponsB,
        hansokuA: patch.hansokuA ?? fouls.a ?? 0,
        hansokuB: patch.hansokuB ?? fouls.b ?? 0,
        // mp-gmcg: an explicit patch-level decision wins (kachinuki [End
        // match] sends "kachinuki-exhaustion" for a win and "hikiwake" for a
        // drawn pool/league encounter); otherwise keep the legacy mapping
        // from score.type. The score endpoint accepts and persists a
        // top-level decision (ScoreRequest = state.MatchResult;
        // validateDecision allowlists kachinuki-exhaustion).
        decision: patch.decision || (isHikiwake(score.type) ? "hikiwake" : ""),
        status: toBackendStatus(patch.status || "scheduled"),
    };
    // Carry the winner's participant id so a SAME-NAME head-to-head (the winner
    // NAME matches both sides) is unambiguous on the backend. Without it, when
    // the scoreline is tied (e.g. a hantei decision: equal ippon counts) the
    // backend can't infer the winning side and leaves WinnerID empty, and the
    // league matrix then marks BOTH same-name rows as winners. The scoring modal
    // sets patch.winner to the winning SIDE object (m.sideA/m.sideB), so its id
    // is available directly; fall back to deriving it from the match sides by
    // name only when the names are distinct (unambiguous).
    let winnerId = "";
    if (patch.winner && typeof patch.winner === "object" && patch.winner.id) {
        winnerId = patch.winner.id;
    } else if (winnerName) {
        if (winnerName === sideAName && winnerName !== sideBName) winnerId = sideAId || "";
        else if (winnerName === sideBName && winnerName !== sideAName) winnerId = sideBId || "";
    }
    if (winnerId) result.winnerId = winnerId;
    // Belt and braces (bc-dmsr review): emit the side ids alongside
    // winnerId so the server's validateHanteiMarkPlacement sees the SAME
    // triple this function just used to place the mark above, rather than
    // relying solely on the server backfilling them from the stored match.
    // Without this the server never learned the ids on the wire at all -
    // sideAId/sideBId are computed above purely for this function's own
    // placement and were otherwise discarded - so a same-name pair's
    // correctly id-attributed mark could be rejected by the server's
    // name-only fallback. Omitted (not sent as "") when a side carries no
    // id at all, matching every other optional field in this payload.
    //
    // Send back ONLY an id the server itself supplied. The locals above are
    // not always participant UUIDs: normalizeMatch's resolveSide falls back to
    // `{ id: flatId || name }`, so a match the server sends WITHOUT flat side
    // ids - a bracket match, which persists none, or a legacy pool row written
    // before the id columns existed - yields sides whose id IS the display
    // name. That is fine for this function's own placement (the fallback
    // applies to all three values at once, so domain.AttributeWinnerSide's id
    // branch compares name against name and answers exactly as its name branch
    // would), but it must not go on the wire: a pool write persists the ids
    // verbatim, so an invented one would be stored as though it were a real
    // participant UUID, and buildPlayerMap collapses a same-name pair onto one
    // entry, so BOTH sides would be stored under the SAME invented id.
    //
    // Gating on the flat id the server sent, rather than on the resolved side
    // object, keeps the case this was added for (a real same-name pair, whose
    // mark can only be attributed by id) and drops exactly the invented case.
    // With nothing sent, the server backfills from the stored match and falls
    // back to name attribution, which is what it did before these were added.
    if (match?.sideAId && sideAId) result.sideAId = sideAId;
    if (match?.sideBId && sideBId) result.sideBId = sideBId;
    // Engi (kata) matches score by referee flag count, not ippons: carry
    // flagsA/flagsB through when the patch sets them (EngiScoreEditorModal's
    // submit payload). Omitted otherwise so non-engi payloads stay minimal.
    if (patch.flagsA != null) result.flagsA = patch.flagsA;
    if (patch.flagsB != null) result.flagsB = patch.flagsB;
    // Audit reason captured by ReasonPrompt when correcting a completed
    // match (admin_scoring_shared.jsx CORRECTION_PRESETS). Without this the
    // operator's typed/selected reason never reached the wire and the audit
    // trail silently stayed empty on every correction, kendo and team alike.
    if (patch.correctionReason) result.correctionReason = patch.correctionReason;
    if (patch.subResults) {
        // Same conversion per bout: a sub carrying the editor's boolean (the
        // daihyosen editor states it unconditionally) has it folded into the
        // mark on the sub winner's side; the field itself never reaches the
        // wire. Subs echoed verbatim from the server already carry the mark
        // inside their ippons and pass through untouched.
        result.subResults = patch.subResults.map((sub) => {
            if (typeof sub.decidedByHantei !== "boolean") return sub;
            const { decidedByHantei, ...rest } = sub;
            let a = stripHt(rest.ipponsA), b = stripHt(rest.ipponsB);
            if (decidedByHantei) {
                // If the winner names NEITHER side (no winner, or a
                // rename-drifted name), placeHtForWinner leaves both arrays
                // untouched: the mark has no side to ride on, so both stay
                // markless. This is CLAUDE.md's accepted no-mark class (ii),
                // and it is also the only shape the server accepts -
                // validateHanteiMarkPlacement rejects a mark whose winner is
                // not that side's name, so echoing one back would 400 every
                // later save of the match.
                [a, b] = placeHtForWinner(rest.winner, rest.sideA, rest.sideB, a, b);
            }
            return { ...rest, ipponsA: a, ipponsB: b };
        });
    }
    // Kachinuki: transient request-only flag marking an explicit operator
    // "record bout" submit. The server advances the winner-stays sequence
    // only on flagged writes (handlers_match.go scoreRequestBody); it is
    // never persisted on the match.
    if (patch.kachinukiBoutFinal) result.kachinukiBoutFinal = true;
    // mp-62vr: rep-player names for a team daihyosen/tiebreaker rep bout. Only
    // forward non-empty values: the engine preserves a prior pick on empty
    // (backfillMatchIdentity), so omitting an unset side never wipes it.
    if (patch.repPlayerA) result.repPlayerA = patch.repPlayerA;
    if (patch.repPlayerB) result.repPlayerB = patch.repPlayerB;
    // FR-033: encho metadata round-trips so the (E) suffix persists. The
    // backend in Slice 1 (T039) accepts the field passively: Slice 3 wires
    // the decision/kiken/fusenpai semantics, but we already keep the
    // periodCount alongside the score so re-edits and history retain it.
    if (patch.encho && patch.encho.periodCount > 0) {
        result.encho = { periodCount: patch.encho.periodCount };
    }
    // The editors keep an armed decidedByHantei BOOLEAN locally (their UX is
    // unchanged); the wire carries the verdict as the "Ht" mark in the
    // winner's ippon list instead of a flag. realIppons above already
    // stripped any echoed mark from the outgoing arrays, so placement here is
    // never doubled: an armed verdict (or a re-edit of a match that holds
    // one) re-places the mark on the winner's side; an explicit false simply
    // leaves the arrays markless — under the mark model a markless scoreline
    // IS the withdrawal, no separate signal exists or is needed.
    const wantHantei = typeof patch.decidedByHantei === "boolean"
        ? patch.decidedByHantei
        : !!match?.decidedByHantei;
    if (wantHantei) {
        // Same placeHtForWinner rule as the sub-bout branch above, but this
        // call site DOES have ids in scope (winnerId/sideAId/sideBId, derived
        // above), so it threads them through: placeHtForWinner attributes by
        // id whenever all three are present, falling back to the name
        // comparison only when one is missing. A winner naming/matching
        // neither side leaves both arrays untouched, so no mark is placed.
        [result.ipponsA, result.ipponsB] = placeHtForWinner(
            winnerName, sideAName, sideBName, result.ipponsA, result.ipponsB,
            winnerId, sideAId, sideBId);
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
    // (e.g. two "Tanaka Kenji" from different dojos: the duplicate check
    // only rejects same-name AND same-dojo) onto a single id. When the
    // server provides an explicit per-side id (m.sideAId / m.sideBId /
    // m.winnerId: populated from pool-matches.csv), it is the authoritative
    // identity and overrides the name-collapsed lookup. We clone the
    // playerMap entry before stamping the id so the shared map object isn't
    // mutated across matches.
    const resolveSide = (name, flatId) => {
        // Prefer the id-keyed entry: it carries the CORRECT dojo/number even for
        // same-name participants. The name-keyed entry collapses same-name
        // players onto one identity, so only trust it when its id matches the
        // server's flat id: otherwise we'd attach the wrong dojo to this side.
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
        // Same-name pair with no winnerId (scoring.go deliberately leaves
        // WinnerID empty on an equal-count hantei): the name lookup stamps an
        // ARBITRARY twin's uuid, wrong ~half the time. Do NOT "fix" this by
        // blanking the id — that was tried and reverted. A blanked id is not
        // honoured uniformly: winnerSideLR and sideAWon name-fall-back to a
        // deterministic side, MatchCard/league cells drop or double the loss,
        // and the editor's score.ippons seeding (which keys winner.id ===
        // side.id) matches NEITHER side, so reopening the match seeded empty
        // slots and the next save wiped the recorded point — the 4d602de2
        // regression class. Arbitrary-but-CONSISTENT attribution is the
        // codebase-wide status quo (Go's SideMarksLR also picks the first
        // name match); the truth is simply not in the data, and every
        // surface agreeing on one side beats surfaces disagreeing.
        norm.winner = resolveSide(norm.winner, m.winnerId);
    }
    // Did sideA win? Prefer matching by stable id (sideA/winner are resolved to
    // {id,name} above with the server's authoritative flat ids), so same-name /
    // different-dojo finalists don't collide onto the wrong side and swap the
    // displayed winner/loser tallies. Fall back to name only when an id isn't
    // present on both.
    const sideAWon = (w, a) => {
        if (!w || !a) return false;
        const wId = typeof w === "object" ? w.id || "" : "";
        const aId = typeof a === "object" ? a.id || "" : "";
        if (wId && aId) return wId === aId;
        const wn = typeof w === "object" ? w.name : w;
        const an = typeof a === "object" ? a.name : a;
        return wn === an;
    };
    // Build score from ipponsA/ipponsB. Pool and bracket matches converge on
    // this one shape (both carry ipponsA/ipponsB arrays; scoreA/scoreB
    // strings never appear on the wire), so one branch covers both.
    if (!norm.score && (norm.ipponsA?.length || norm.ipponsB?.length) && norm.status === "completed") {
        const aWin = sideAWon(norm.winner, norm.sideA);
        // realIppons discipline, hoisted once: the "Ht" mark occupies a slot
        // but is never a point, so a 1-1 hantei must read 1-1 here, not 2-1.
        // The winner's stripped array is otherwise filtered twice per match
        // (once for winnerPts.length, once for the ippons array itself).
        const winnerIppons = realIppons(aWin ? norm.ipponsA : norm.ipponsB);
        norm.score = {
            type: isHikiwake(norm.decision) ? "hikiwake" : "ippon",
            winnerPts: winnerIppons.length,
            loserPts: realIppons(aWin ? norm.ipponsB : norm.ipponsA).length,
            ippons: winnerIppons,
        };
    }
    // Carry engi flag counts through (additive, no kendo code reads these).
    if (m.flagsA != null) norm.flagsA = m.flagsA;
    if (m.flagsB != null) norm.flagsB = m.flagsB;
    // decidedByHantei is a DERIVED view property now: the server records the
    // verdict as the "Ht" entry in the winner's ippon list and sends no flag.
    // Deriving it here keeps every display surface and editor reading the
    // property they always read, off the one place the verdict actually
    // lives. Per-sub likewise.
    norm.decidedByHantei = hanteiDecided(norm);
    // Pre-check with .some() before mapping: most matches carry no hantei
    // sub at all (a daihyosen/hantei bout is the exception, not the rule),
    // so allocating a fresh array on every normalizeMatch call would be a
    // needless copy on the common path. When no sub carries the mark, the
    // original array identity is preserved rather than an unmodified clone.
    if (Array.isArray(norm.subResults) && norm.subResults.some((sub) => sub && hanteiDecided(sub))) {
        norm.subResults = norm.subResults.map((sub) =>
            sub && hanteiDecided(sub)
                ? { ...sub, decidedByHantei: true }
                : sub);
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
        // details: dojo, zekken display name, and assigned number (e.g. "K1"): 
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
        // Already camelCase: backfill danGrade from metadata if the field is absent.
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
    // sources, not metadata: they are mapped to p.Source above.
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

    // ExtraQualifiers (bc-qual LP-5a): `json:"extraQualifiers,omitempty"` on
    // the Go side drops the key entirely for the default/standard value
    // (""), so a competition without a non-standard selection arrives with
    // `extraQualifiers: undefined` rather than "". Normalize to "" here so
    // every consumer (admin_setup.jsx's radio, viewer_standings.jsx's
    // per-pool qualifier count) can compare against "" directly without an
    // `|| ""` of its own. This top-level field covers the bare-Competition
    // responses (start/generate-draw/complete); the nested `result.config`
    // case (GET /viewer/competitions/:id) is normalized separately below.
    result.extraQualifiers = result.extraQualifiers || "";

    // ...and again on the NESTED record, which is the shape the operator
    // console actually renders: GET /api/viewer/competitions/:id answers
    // {config, pools, poolMatches, bracket, standings}, and admin.jsx passes
    // `detail.config` straight to AdminCompetition. Normalizing only the top
    // level left `config.courts` null and made the claim above ("no consumer
    // has to guard individually") false for the one path that matters. The
    // render sites are defended too; this keeps the boundary honest so the
    // next consumer of config.* inherits the guarantee.
    // One copy, both fields. config.players is PascalCase on the Go side and
    // camelCase here; config.courts must never arrive null.
    if (result.config) {
        const config = { ...result.config, courts: result.config.courts || [], extraQualifiers: result.config.extraQualifiers || "" };
        if (config.players) {
            config.players = config.players.map(p => {
                const norm = normalizePlayer(p);
                // Preserve id and seed null (normalizePlayer maps Seed:0 → seed:0, but JS uses null for "not seeded")
                return { ...norm, id: p.id || norm.id, seed: p.Seed || p.seed || null };
            });
        }
        result.config = config;
    }

    // Normalize pools (Go: PoolName, Players → poolName, players)
    if (result.pools) {
        result.pools = result.pools.map(p => ({
            poolName: p.PoolName || p.poolName || "",
            players: (p.Players || p.players || []).map(normalizePlayer),
            matches: p.Matches || p.matches || [],
        }));
    }

    // Normalize standings player field (carry flags for engi standings)
    if (result.standings) {
        const standings = {};
        for (const key of Object.keys(result.standings)) {
            standings[key] = result.standings[key].map(s => ({
                ...s,
                player: normalizePlayer(s.player),
                flags: s.flags || 0,
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
    // Normalize bronze/3rd-place match when present (naginata competitions)
    if (result.bracket && result.bracket.thirdPlaceMatch) {
        result.bracket = { ...result.bracket, thirdPlaceMatch: normalizeMatch(result.bracket.thirdPlaceMatch, playerMap) };
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
