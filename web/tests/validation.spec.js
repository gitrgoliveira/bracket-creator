import { describe, it, expect } from "vitest";
import {
    escapeHtml,
    getIssueLineNumber,
    sanitizeNameForValidation,
    normalizeNameForValidation,
    getParticipantValidationState,
    validateCourtsValue,
    validatePoolSettings
} from "../js/validation.js";

describe("escapeHtml", () => {
    it("escapes the five XML metacharacters", () => {
        expect(escapeHtml(`<a href="x">'b'&"c"</a>`))
            .toBe("&lt;a href=&quot;x&quot;&gt;&#039;b&#039;&amp;&quot;c&quot;&lt;/a&gt;");
    });

    it("returns plain text unchanged", () => {
        expect(escapeHtml("Jane Doe, Mushin Dojo")).toBe("Jane Doe, Mushin Dojo");
    });
});

describe("getIssueLineNumber", () => {
    it("extracts the leading line number", () => {
        expect(getIssueLineNumber("Line 7: missing dojo")).toBe(7);
    });

    it("returns null when the message has no leading line marker", () => {
        expect(getIssueLineNumber("missing dojo on line 4")).toBeNull();
    });
});

describe("sanitizeNameForValidation", () => {
    it("uppercases a single name token", () => {
        expect(sanitizeNameForValidation("kazuki")).toBe("KAZUKI");
    });

    it("returns 'F. LAST' style for two+ tokens", () => {
        expect(sanitizeNameForValidation("Jane Marie Doe")).toBe("J. DOE");
    });

    it("returns empty string for empty/whitespace", () => {
        expect(sanitizeNameForValidation("   ")).toBe("");
    });
});

describe("normalizeNameForValidation", () => {
    it("collapses internal whitespace and lowercases", () => {
        expect(normalizeNameForValidation("  Jane   Doe  ")).toBe("jane doe");
    });
});

describe("getParticipantValidationState", () => {
    it("flags a valid two-column entry as no-issues", () => {
        const state = getParticipantValidationState("Jane Doe, Mushin Dojo", false);
        expect(state.errors).toEqual([]);
        expect(state.warnings).toEqual([]);
        expect(state.participantCount).toBe(1);
        expect(state.isEmpty).toBe(false);
    });

    it("warns when the dojo column is missing in standard mode", () => {
        const state = getParticipantValidationState("Jane Doe", false);
        expect(state.warnings.length).toBe(1);
        expect(state.warnings[0]).toMatch(/Line 1/);
    });

    it("errors on duplicate participant entries", () => {
        const state = getParticipantValidationState(
            "Jane Doe, Mushin Dojo\nJane Doe, Mushin Dojo",
            false
        );
        expect(state.errors.length).toBe(1);
        expect(state.errors[0]).toMatch(/Line 2.*duplicate/i);
    });

    it("requires three columns when zekken mode is enabled", () => {
        const state = getParticipantValidationState("Jane Doe, Mushin Dojo", true);
        expect(state.errors.length).toBe(1);
        expect(state.errors[0]).toMatch(/Name, ZekkenName, Dojo/);
    });

    it("treats zekken duplicates as duplicates regardless of case", () => {
        const state = getParticipantValidationState(
            "John Smith, JOHN, Mushin Dojo\nJohn Smith, john, Mushin Dojo",
            true
        );
        expect(state.errors.length).toBe(1);
        expect(state.errors[0]).toMatch(/Line 2.*duplicate/i);
    });

    it("treats an empty input as empty (no participants, no errors)", () => {
        const state = getParticipantValidationState("\n\n   \n", false);
        expect(state.isEmpty).toBe(true);
        expect(state.participantCount).toBe(0);
        expect(state.errors).toEqual([]);
    });

    it("emits an info note when extra metadata columns are detected", () => {
        const state = getParticipantValidationState(
            "Jane Doe, Mushin Dojo, 4-dan",
            false
        );
        expect(state.infos.length).toBe(1);
        expect(state.infos[0]).toMatch(/Extra columns/);
    });
});

describe("validateCourtsValue", () => {
    it("accepts the in-range value 5", () => {
        expect(validateCourtsValue("5")).toEqual({ ok: true, value: 5 });
    });

    it("rejects values above the A-Z cap of 26", () => {
        const result = validateCourtsValue("27");
        expect(result.ok).toBe(false);
        expect(result.error).toMatch(/1 and 26/);
    });

    it("rejects non-numeric input", () => {
        const result = validateCourtsValue("abc");
        expect(result.ok).toBe(false);
    });

    it("rejects values below 1", () => {
        const result = validateCourtsValue("0");
        expect(result.ok).toBe(false);
    });
});

describe("validatePoolSettings", () => {
    it("accepts valid winners < players", () => {
        expect(validatePoolSettings(2, 4, false)).toEqual({ ok: true });
    });

    it("rejects winners >= players (min mode label)", () => {
        const r = validatePoolSettings(3, 3, false);
        expect(r.ok).toBe(false);
        expect(r.error).toMatch(/minimum players/i);
    });

    it("rejects winners >= players (max mode label)", () => {
        const r = validatePoolSettings(4, 3, true);
        expect(r.ok).toBe(false);
        expect(r.error).toMatch(/maximum players/i);
    });

    it("rejects winners <= 0", () => {
        const r = validatePoolSettings(0, 3, false);
        expect(r.ok).toBe(false);
        expect(r.error).toMatch(/Winners per pool must be at least 1/);
    });
});
