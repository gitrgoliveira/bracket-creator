// Pure-logic time-estimator helpers. The renderer in app.js wires these to
// the DOM; the math itself is side-effect free and safe to unit-test.

export function matchesPerPool(n, isRoundRobin) {
    if (n <= 1) return 0;
    if (n <= 3 || isRoundRobin) return n * (n - 1) / 2;
    return n;
}

export function formatDuration(totalMinutes) {
    if (totalMinutes <= 0) return '0m';
    const h = Math.floor(totalMinutes / 60);
    const m = Math.round(totalMinutes % 60);
    if (h > 0 && m > 0) return `${h}h ${m}m`;
    if (h > 0) return `${h}h`;
    return `${m}m`;
}

export function formatTime(totalMinutes) {
    const h = Math.floor(totalMinutes / 60) % 24;
    const m = Math.round(totalMinutes % 60);
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

// Estimate the schedule duration in parallel-court minutes given the
// configuration. Returns null if there isn't enough input to compute, and an
// object with the computed quantities otherwise.
//
// Inputs:
//   totalPlayers         — number
//   isPools              — bool (true = "Pools and Playoffs", false = "Playoffs")
//   courts               — int >= 1
//   teamSize             — int >= 1
//   poolMatchMins        — minutes per pool match (per bout)
//   elimMatchMins        — minutes per elimination match (per bout)
//   rotationSecs         — rotation padding seconds applied per match
//   breakMins            — break minutes added per phase
//   playersPerPool       — int (only used when isPools)
//   winnersPerPool       — int (only used when isPools)
//   isMaxMode            — bool (max-players-per-pool vs min)
//   isRoundRobin         — bool (only used when isPools)
//   startTimeMinutes     — start time as minutes-from-midnight (number)
export function estimateSchedule(opts) {
    const totalPlayers = Math.max(parseInt(opts.totalPlayers, 10) || 0, 0);
    if (totalPlayers < 2) {
        return null;
    }

    const courts = Math.max(parseInt(opts.courts, 10) || 1, 1);
    const teamSize = Math.max(parseInt(opts.teamSize, 10) || 0, 1);
    const poolMatchMins = parseFloat(opts.poolMatchMins) || 3;
    const elimMatchMins = parseFloat(opts.elimMatchMins) || 4;
    const rotationSecs = parseFloat(opts.rotationSecs) || 0;
    const breakMins = parseFloat(opts.breakMins) || 0;
    const isPools = !!opts.isPools;

    let totalPoolMinutes = 0;
    let playoffParticipants = totalPlayers;

    if (isPools) {
        const playersPerPool = parseInt(opts.playersPerPool, 10) || 3;
        const winnersPerPool = parseInt(opts.winnersPerPool, 10) || 2;
        const isMaxMode = !!opts.isMaxMode;
        const isRoundRobin = !!opts.isRoundRobin;

        const numPools = isMaxMode
            ? Math.ceil(totalPlayers / playersPerPool)
            : Math.floor(totalPlayers / playersPerPool);

        if (numPools <= 0) {
            return null;
        }

        const mpp = matchesPerPool(playersPerPool, isRoundRobin);
        const totalPoolMatches = numPools * mpp;

        totalPoolMinutes = (totalPoolMatches * teamSize * poolMatchMins)
            + (totalPoolMatches * rotationSecs / 60)
            + breakMins;

        playoffParticipants = numPools * winnersPerPool;
    }

    const numElimMatches = Math.max(playoffParticipants - 1, 0);
    const totalElimMinutes = (numElimMatches * teamSize * elimMatchMins)
        + (numElimMatches * rotationSecs / 60)
        + breakMins;

    const totalSequentialMinutes = totalPoolMinutes + totalElimMinutes;
    const totalParallelMinutes = totalSequentialMinutes / courts;

    const startTimeMinutes = (typeof opts.startTimeMinutes === 'number')
        ? opts.startTimeMinutes
        : 0;
    const finishTotalMins = startTimeMinutes + totalParallelMinutes;

    return {
        isPools,
        courts,
        totalPoolMinutes,
        totalElimMinutes,
        totalSequentialMinutes,
        totalParallelMinutes,
        finishTotalMins,
        playoffParticipants,
        numElimMatches
    };
}

// Parse a "HH:MM" string into minutes-from-midnight; returns 0 on bad input.
export function parseStartTime(startTimeStr) {
    const s = String(startTimeStr || '09:00');
    const parts = s.split(':');
    if (parts.length < 2) return 0;
    const h = parseInt(parts[0], 10);
    const m = parseInt(parts[1], 10);
    if (Number.isNaN(h) || Number.isNaN(m)) return 0;
    return h * 60 + m;
}

// fetchScheduleEstimate calls GET /api/schedule/estimate and returns the
// server's ScheduleEstimate { totalDurationMinutes, perCourtMinutes,
// ceremonyMinutes } shape, or null on any error/abort.
//
// The server endpoint (T147a, T152a) is the canonical source of truth
// for FR-055/057/058 calculation; the synchronous estimateSchedule()
// above remains for renderers that need a richer pool/elim breakdown
// (the server returns aggregate totals only).
//
// params: { matchDuration, multiplier, numMatches, courts, teamSize,
//           boutsPerTeamMatch, buffer, ceremonyMinutes }
export async function fetchScheduleEstimate(params, fetchImpl = (typeof fetch !== 'undefined' ? fetch : null)) {
    if (!fetchImpl) return null;
    const qs = new URLSearchParams();
    Object.entries(params || {}).forEach(([k, v]) => {
        if (v === undefined || v === null || v === '') return;
        qs.set(k, String(v));
    });
    try {
        const resp = await fetchImpl(`/api/schedule/estimate?${qs.toString()}`);
        if (!resp.ok) return null;
        return await resp.json();
    } catch (err) {
        return null;
    }
}
