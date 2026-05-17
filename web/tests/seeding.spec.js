import { describe, it, expect } from "vitest";
import { validateSeedAssignments } from "../js/seeding.js";

describe("validateSeedAssignments", () => {
    it("includes Dojo when provided", () => {
        const result = validateSeedAssignments([
            { name: "Alice", dojo: "DojoA", rawValue: "1" },
            { name: "Bob", dojo: "DojoB", rawValue: "2" },
        ]);
        expect(result.ok).toBe(true);
        expect(result.assignments).toEqual([
            { Name: "Alice", Dojo: "DojoA", SeedRank: 1 },
            { Name: "Bob", Dojo: "DojoB", SeedRank: 2 },
        ]);
    });

    it("omits Dojo when empty", () => {
        const result = validateSeedAssignments([
            { name: "Alice", dojo: "", rawValue: "1" },
        ]);
        expect(result.ok).toBe(true);
        expect(result.assignments).toEqual([
            { Name: "Alice", SeedRank: 1 },
        ]);
    });

    it("omits Dojo when not present in input", () => {
        const result = validateSeedAssignments([
            { name: "Alice", rawValue: "1" },
        ]);
        expect(result.ok).toBe(true);
        expect(result.assignments).toEqual([
            { Name: "Alice", SeedRank: 1 },
        ]);
    });

    it("detects duplicate seed ranks", () => {
        const result = validateSeedAssignments([
            { name: "Alice", dojo: "DojoA", rawValue: "1" },
            { name: "Bob", dojo: "DojoB", rawValue: "1" },
        ]);
        expect(result.ok).toBe(false);
        expect(result.errors[0]).toMatch(/Duplicate seed ranks/);
    });

    it("detects gaps in seed sequence", () => {
        const result = validateSeedAssignments([
            { name: "Alice", dojo: "DojoA", rawValue: "1" },
            { name: "Bob", dojo: "DojoB", rawValue: "3" },
        ]);
        expect(result.ok).toBe(false);
        expect(result.errors[0]).toMatch(/Missing seed ranks/);
    });
});
