import { describe, it, expect } from "vitest";
import {
    matchesPerPool,
    formatDuration,
    formatTime,
    estimateSchedule,
    parseStartTime
} from "../js/time_estimator.js";

describe("matchesPerPool", () => {
    it("returns 0 for fewer than 2 players", () => {
        expect(matchesPerPool(0, true)).toBe(0);
        expect(matchesPerPool(1, false)).toBe(0);
    });

    it("uses N*(N-1)/2 for round-robin or small pools", () => {
        // 4 players, round-robin: 6 matches.
        expect(matchesPerPool(4, true)).toBe(6);
        // 3 players (always treated as round-robin): 3 matches.
        expect(matchesPerPool(3, false)).toBe(3);
    });

    it("falls back to N matches for pools above 3 without round-robin", () => {
        expect(matchesPerPool(5, false)).toBe(5);
    });
});

describe("formatDuration", () => {
    it("returns '0m' for zero or negative input", () => {
        expect(formatDuration(0)).toBe("0m");
        expect(formatDuration(-5)).toBe("0m");
    });

    it("formats sub-hour durations as just minutes", () => {
        expect(formatDuration(45)).toBe("45m");
    });

    it("formats whole-hour durations as just hours", () => {
        expect(formatDuration(120)).toBe("2h");
    });

    it("formats mixed durations as 'Xh Ym'", () => {
        expect(formatDuration(95)).toBe("1h 35m");
    });

    it("rolls minutes into the next hour when rounding would yield 60", () => {
        // 119.6 → rounds to 120 → "2h", not "1h 60m".
        expect(formatDuration(119.6)).toBe("2h");
    });
});

describe("formatTime", () => {
    it("formats minutes-from-midnight as 24h HH:MM", () => {
        expect(formatTime(9 * 60 + 30)).toBe("09:30");
    });

    it("wraps past 24h", () => {
        expect(formatTime(26 * 60)).toBe("02:00");
    });

    it("rolls minutes into the next hour when rounding would yield 60", () => {
        // 59.8 minutes → rounds to 60 → "01:00", not "00:60".
        expect(formatTime(59.8)).toBe("01:00");
    });
});

describe("parseStartTime", () => {
    it("parses 'HH:MM' into minutes-from-midnight", () => {
        expect(parseStartTime("09:30")).toBe(570);
    });

    it("returns 0 on a malformed input", () => {
        expect(parseStartTime("not a time")).toBe(0);
    });

    it("uses 09:00 default semantics when input is empty", () => {
        // Empty input falls back to '09:00' inside parseStartTime → 540 minutes.
        expect(parseStartTime("")).toBe(9 * 60);
    });
});

describe("estimateSchedule", () => {
    it("returns null when totalPlayers < 2", () => {
        expect(estimateSchedule({ totalPlayers: 1, isPools: false, courts: 1, teamSize: 1 })).toBeNull();
    });

    it("computes a basic playoffs-only estimate", () => {
        // 8 players → 7 elimination matches; teamSize=1; 4-min match; 0 rotation,
        // 0 break, 1 court ⇒ totalElimMinutes = 7*1*4 = 28.
        const e = estimateSchedule({
            totalPlayers: 8,
            isPools: false,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            startTimeMinutes: 9 * 60
        });
        expect(e).not.toBeNull();
        expect(e.totalPoolMinutes).toBe(0);
        expect(e.totalElimMinutes).toBe(28);
        expect(e.totalParallelMinutes).toBe(28);
        expect(e.finishTotalMins).toBe(9 * 60 + 28);
    });

    it("respects the courts divisor for parallel-court timing", () => {
        const e = estimateSchedule({
            totalPlayers: 8,
            isPools: false,
            courts: 2,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            startTimeMinutes: 0
        });
        // 28 sequential / 2 courts = 14 parallel.
        expect(e.totalParallelMinutes).toBe(14);
    });

    it("computes a pools+playoffs estimate with round-robin", () => {
        // 12 players, 4-per-pool min mode, 3-min pool, 4-min elim, 1 court,
        // 0 rotation/break, round-robin on, 2 winners per pool.
        // numPools = floor(12/4) = 3; matches per pool = 4*3/2 = 6;
        // totalPoolMatches = 18; totalPoolMinutes = 18*1*3 = 54.
        // playoffParticipants = 3*2 = 6; numElimMatches = 5;
        // totalElimMinutes = 5*1*4 = 20.
        const e = estimateSchedule({
            totalPlayers: 12,
            isPools: true,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            playersPerPool: 4,
            winnersPerPool: 2,
            isMaxMode: false,
            isRoundRobin: true,
            startTimeMinutes: 0
        });
        expect(e.totalPoolMinutes).toBe(54);
        expect(e.totalElimMinutes).toBe(20);
        expect(e.totalSequentialMinutes).toBe(74);
        expect(e.playoffParticipants).toBe(6);
        expect(e.numElimMatches).toBe(5);
    });

    it("accounts for smaller remainder pool in max mode", () => {
        // 5 players, max 3 per pool → ceil(5/3) = 2 pools.
        // Pool 1: 3 players = 3 matches. Pool 2: 2 players = 1 match. Total = 4.
        const e = estimateSchedule({
            totalPlayers: 5,
            isPools: true,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            playersPerPool: 3,
            winnersPerPool: 2,
            isMaxMode: true,
            isRoundRobin: true,
            startTimeMinutes: 0
        });
        expect(e.numPoolMatches).toBe(4);
    });

    it("handles evenly divisible pools in max mode without remainder", () => {
        // 6 players, max 3 → 2 full pools. 2 * 3 = 6 matches.
        const e = estimateSchedule({
            totalPlayers: 6,
            isPools: true,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            playersPerPool: 3,
            winnersPerPool: 2,
            isMaxMode: true,
            isRoundRobin: true,
            startTimeMinutes: 0
        });
        expect(e.numPoolMatches).toBe(6);
    });

    it("accounts for remainder pool with round-robin and larger pools", () => {
        // 7 players, max 4, round-robin → ceil(7/4) = 2 pools.
        // Pool 1: 4 players = 6 matches. Pool 2: 3 players = 3 matches. Total = 9.
        const e = estimateSchedule({
            totalPlayers: 7,
            isPools: true,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            playersPerPool: 4,
            winnersPerPool: 2,
            isMaxMode: true,
            isRoundRobin: true,
            startTimeMinutes: 0
        });
        expect(e.numPoolMatches).toBe(9);
    });

    it("does not apply remainder logic in min mode", () => {
        // 5 players, min 3 → floor(5/3) = 1 pool of 3.
        // 1 * 3 = 3 matches. Remainder players excluded from pools.
        const e = estimateSchedule({
            totalPlayers: 5,
            isPools: true,
            courts: 1,
            teamSize: 1,
            poolMatchMins: 3,
            elimMatchMins: 4,
            rotationSecs: 0,
            breakMins: 0,
            playersPerPool: 3,
            winnersPerPool: 2,
            isMaxMode: false,
            isRoundRobin: true,
            startTimeMinutes: 0
        });
        expect(e.numPoolMatches).toBe(3);
    });
});
