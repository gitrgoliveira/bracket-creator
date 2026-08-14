import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
    escapeHtml,
    getIssueLineNumber,
    sanitizeNameForValidation,
    normalizeNameForValidation,
    getParticipantValidationState,
    validateCourtsValue,
    shiaijoCountError,
    VALID_SHIAIJO_COUNTS,
    validatePoolSettings
} from "../js/validation.js";

describe("escapeHtml", () => {
    it("escapes the five XML metacharacters", () => {
        expect(escapeHtml(`<a href="x">'b'&"c"</a>`))
            .toBe("&lt;a href=&quot;x&quot;&gt;&#039;b&#039;&amp;&quot;c&quot;&lt;/a&gt;");
    });

    it("returns plain text unchanged", () => {
        expect(escapeHtml("Jane Doe, Enzan Dojo")).toBe("Jane Doe, Enzan Dojo");
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
        const state = getParticipantValidationState("Jane Doe, Enzan Dojo", false);
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
            "Jane Doe, Enzan Dojo\nJane Doe, Enzan Dojo",
            false
        );
        expect(state.errors.length).toBe(1);
        expect(state.errors[0]).toMatch(/Line 2.*duplicate/i);
    });

    it("requires three columns when zekken mode is enabled", () => {
        const state = getParticipantValidationState("Jane Doe, Enzan Dojo", true);
        expect(state.errors.length).toBe(1);
        expect(state.errors[0]).toMatch(/Name, ZekkenName, Dojo/);
    });

    it("treats zekken duplicates as duplicates regardless of case", () => {
        const state = getParticipantValidationState(
            "John Smith, JOHN, Enzan Dojo\nJohn Smith, john, Enzan Dojo",
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
            "Jane Doe, Enzan Dojo, 4-dan",
            false
        );
        expect(state.infos.length).toBe(1);
        expect(state.infos[0]).toMatch(/Extra columns/);
    });
});

// Mirrors internal/helper/shiaijo_count_test.go and
// web-mobile/js/__tests__/shiaijo_count.test.jsx. A tournament's shiaijo
// allocation must be a POWER OF TWO: the draw gives each shiaijo its own block
// of the bracket and merges those blocks in PAIRS, so the count has to halve
// cleanly all the way down. 6 halves to 3 and stops.
//
// This form posts natively to /create and the server answers a bad count with
// a JSON body, so anything this validator lets through replaces the page with
// raw JSON and destroys the operator's pasted participant list and seeds.
describe("shiaijoCountError", () => {
    const cases = [
        { n: 1, valid: true },  // a single-shiaijo tournament is explicitly allowed
        { n: 2, valid: true },
        { n: 3, valid: false, below: 2, above: 4 },
        { n: 4, valid: true },
        // 5 and 6 are the regression pins for the rule change: 5 was accepted
        // by the old "1 to 26" range check and 6 by the retired parity rule.
        { n: 5, valid: false, below: 4, above: 8 },
        { n: 6, valid: false, below: 4, above: 8 },
        { n: 7, valid: false, below: 4, above: 8 },
        { n: 8, valid: true },
        { n: 10, valid: false, below: 8, above: 16 },
        { n: 12, valid: false, below: 8, above: 16 },
        { n: 16, valid: true },
    ];

    cases.forEach(({ n, valid, below, above }) => {
        it(`${valid ? "accepts" : "rejects"} ${n} shiaijo`, () => {
            const err = shiaijoCountError(n);
            if (valid) {
                expect(err).toBeNull();
                return;
            }
            expect(err).toContain(`${n} shiaijo cannot be paired down to a single bracket`);
            // Names the nearest valid counts either side, and always offers 1.
            expect(err).toContain(`Use ${below} or ${above}, or 1`);
            // The canonical reason, shared with the Go message and the console.
            expect(err).toContain("each shiaijo its own block of the bracket");
            expect(err).toContain("merge in pairs");
            expect(err).toContain("halve cleanly");
        });
    });

    it("never reads as \"at least 2 shiaijo\"", () => {
        const err = shiaijoCountError(6).toLowerCase();
        expect(err).toContain(", or 1");
        expect(err).not.toContain("at least 2");
        expect(err).not.toContain("at least two");
    });

    it("offers only the count below once past the ceiling", () => {
        // 32 shiaijo cannot be labelled (A-Z caps at 26), so there is no higher
        // valid count to suggest: the message must not invent one.
        const err = shiaijoCountError(20);
        expect(err).toContain("Use 16, or 1");
        expect(err).not.toContain("32");
    });

    it("stays silent for non-counts", () => {
        expect(shiaijoCountError(NaN)).toBeNull();
        expect(shiaijoCountError(undefined)).toBeNull();
    });

    it("derives the valid counts from the A-Z label cap", () => {
        expect(VALID_SHIAIJO_COUNTS).toEqual([1, 2, 4, 8, 16]);
        expect(VALID_SHIAIJO_COUNTS[VALID_SHIAIJO_COUNTS.length - 1] * 2).toBeGreaterThan(26);
    });
});

describe("validateCourtsValue", () => {
    it("accepts every power of two up to the cap", () => {
        VALID_SHIAIJO_COUNTS.forEach((n) => {
            expect(validateCourtsValue(String(n))).toEqual({ ok: true, value: n });
        });
    });

    it("rejects 3, which the field's 1-26 range used to accept", () => {
        const result = validateCourtsValue("3");
        expect(result.ok).toBe(false);
        expect(result.error).toBe(shiaijoCountError(3));
        expect(result.error).toContain("3 shiaijo cannot be paired down to a single bracket");
        expect(result.error).toContain("Use 2 or 4, or 1");
    });

    it("rejects 5 and 6, so the client refuses exactly what the server refuses", () => {
        // 5 was asserted VALID by this suite under the retired rule, and 6 was
        // valid under the "1 or an even number" rule before that. Both now hit
        // helper.ValidateShiaijoCount server-side, whose 400 is a JSON body.
        expect(validateCourtsValue("5").ok).toBe(false);
        expect(validateCourtsValue("6").ok).toBe(false);
    });

    it("rejects values above the A-Z cap of 26", () => {
        const result = validateCourtsValue("27");
        expect(result.ok).toBe(false);
        // The cap is checked before the power-of-two rule, matching the server's
        // helper.ValidateCourts -> helper.ValidateShiaijoCount order.
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

// The rule has to be visible at the field, not only after a rejection: this
// form is the operator's one shot at a workbook and a bad count costs them the
// whole pasted roster. index.html carries the counts as static copy, so pin
// that copy against the derived list rather than trusting it to stay in step.
describe("index.html courts field", () => {
    const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");
    // Derived from the legal set rather than written out, so dropping a count
    // from VALID_SHIAIJO_COUNTS fails here instead of leaving the form's copy
    // quietly offering a count the validator now rejects. The joiner lives in
    // the test because nothing the browser loads needs it: the rejection
    // message names only the nearest counts either side.
    const counts = VALID_SHIAIJO_COUNTS.length <= 1
        ? String(VALID_SHIAIJO_COUNTS[0] ?? "")
        : `${VALID_SHIAIJO_COUNTS.slice(0, -1).join(", ")} or ${VALID_SHIAIJO_COUNTS[VALID_SHIAIJO_COUNTS.length - 1]}`;

    it("names the valid counts in the field label", () => {
        expect(counts).toBe("1, 2, 4, 8 or 16");
        expect(html).toContain(`Number of Shiaijo (courts), ${counts}:`);
    });

    it("names the valid counts in the field tooltip", () => {
        expect(html).toContain(`use ${counts}.`);
    });

    it("states the reason under the field, so the rule is taught not just enforced", () => {
        expect(html).toContain('<div class="form-text" id="courtsHelp">The knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly.</div>');
        // Wired to the input for assistive tech rather than floating loose.
        expect(html).toMatch(/id="courts"[\s\S]{0,200}aria-describedby="courtsHelp"/);
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
