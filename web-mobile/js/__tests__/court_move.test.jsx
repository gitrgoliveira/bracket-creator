import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
    confirmMatchLabel,
    SCORE_KEPT_SENTENCE,
    runningCourtMoveText,
    confirmRunningCourtMove,
} from "../court_move.jsx";
import { UNSAVED_ENTRIES_DISCARDED } from "../unsaved_entries.jsx";

describe("confirmMatchLabel", () => {
    it("formats both names in 'Shiro vs Aka' order with sideB first", () => {
        const m = {
            sideA: { name: "Quinn Walker" },
            sideB: { name: "Jane Austen" },
        };
        expect(confirmMatchLabel(m)).toBe("Jane Austen vs Quinn Walker");
    });

    it("formats one side only when sideB is present", () => {
        const m = {
            sideB: { name: "Jane Austen" },
        };
        expect(confirmMatchLabel(m)).toBe("Jane Austen");
    });

    it("formats one side only when sideA is present", () => {
        const m = {
            sideA: { name: "Quinn Walker" },
        };
        expect(confirmMatchLabel(m)).toBe("Quinn Walker");
    });

    it("returns 'this match' when neither side has a name", () => {
        expect(confirmMatchLabel({})).toBe("this match");
        expect(confirmMatchLabel({ sideA: { name: "" }, sideB: { name: "" } })).toBe("this match");
        expect(confirmMatchLabel({ sideA: null, sideB: null })).toBe("this match");
    });
});

describe("SCORE_KEPT_SENTENCE", () => {
    it("has the exact expected wording", () => {
        expect(SCORE_KEPT_SENTENCE).toBe(
            "The score already saved is kept: starting it again carries on from there."
        );
    });
});

describe("runningCourtMoveText", () => {
    const exampleOpts = {
        label: "Jane Austen vs Quinn Walker",
        from: "B",
        to: "A",
    };

    it("returns exact title, message, and button labels without unsaved entries", () => {
        const res = runningCourtMoveText({ ...exampleOpts, unsaved: false });
        expect(res).toEqual({
            title: "Move to Shiaijo A?",
            message:
                "Jane Austen vs Quinn Walker is running on Shiaijo B. It stops there and goes first in the queue on Shiaijo A. The score already saved is kept: starting it again carries on from there.",
            confirmLabel: "Move to Shiaijo A",
            cancelLabel: "Cancel",
        });
    });

    it("returns the exact message with unsaved entries included", () => {
        const res = runningCourtMoveText({ ...exampleOpts, unsaved: true });
        expect(res.message).toBe(
            "Jane Austen vs Quinn Walker is running on Shiaijo B. It stops there and goes first in the queue on Shiaijo A. Your unsaved entries will be discarded. The score already saved is kept: starting it again carries on from there."
        );
        expect(res.message).toContain(UNSAVED_ENTRIES_DISCARDED);
    });

    it("confirmLabel matches /Move/ and cancelLabel does not", () => {
        const res = runningCourtMoveText(exampleOpts);
        expect(res.confirmLabel).toMatch(/Move/);
        expect(res.cancelLabel).not.toMatch(/Move/);
    });

    it("handles unassigned from ('')", () => {
        const res = runningCourtMoveText({
            label: "Jane Austen vs Quinn Walker",
            from: "",
            to: "A",
        });
        expect(res.title).toBe("Move to Shiaijo A?");
        expect(res.confirmLabel).toBe("Move to Shiaijo A");
        expect(res.cancelLabel).toBe("Cancel");
        expect(res.message).toBe(
            "Jane Austen vs Quinn Walker is running. It stops there and goes first in the queue on Shiaijo A. The score already saved is kept: starting it again carries on from there."
        );
    });

    it("handles unassigned to ('')", () => {
        const res = runningCourtMoveText({
            label: "Jane Austen vs Quinn Walker",
            from: "B",
            to: "",
        });
        expect(res.title).toBe("Take it off its shiaijo?");
        expect(res.confirmLabel).toBe("Take off its shiaijo");
        expect(res.cancelLabel).toBe("Cancel");
        expect(res.message).toBe(
            "Jane Austen vs Quinn Walker is running on Shiaijo B. It stops there and is taken off its shiaijo. The score already saved is kept: starting it again carries on from there."
        );
    });

    it("never prints 'Shiaijo ' followed by nothing, 'undefined', or 'null'", () => {
        const variations = [
            { from: "", to: "" },
            { from: "   ", to: "   " },
            { from: null, to: null },
            { from: undefined, to: undefined },
        ];
        for (const v of variations) {
            const res = runningCourtMoveText({
                label: "Jane Austen vs Quinn Walker",
                ...v,
            });
            expect(res.message).not.toContain("undefined");
            expect(res.message).not.toContain("null");
            expect(res.message).not.toMatch(/Shiaijo\s*[.?]/);
            expect(res.title).not.toContain("undefined");
            expect(res.title).not.toContain("null");
            expect(res.confirmLabel).not.toContain("undefined");
            expect(res.confirmLabel).not.toContain("null");
        }
    });
});

describe("confirmRunningCourtMove", () => {
    let prevConfirm;

    beforeEach(() => {
        prevConfirm = window.confirmDialog;
    });

    afterEach(() => {
        window.confirmDialog = prevConfirm;
    });

    it("passes the built text as options and resolves true when confirmDialog resolves true", async () => {
        window.confirmDialog = vi.fn().mockResolvedValue(true);
        const opts = {
            label: "Jane Austen vs Quinn Walker",
            from: "B",
            to: "A",
            unsaved: true,
        };
        const expectedOptions = runningCourtMoveText(opts);

        const result = await confirmRunningCourtMove(opts);

        expect(window.confirmDialog).toHaveBeenCalledTimes(1);
        expect(window.confirmDialog).toHaveBeenCalledWith(expectedOptions);
        expect(result).toBe(true);
    });

    it("resolves false when confirmDialog resolves false", async () => {
        window.confirmDialog = vi.fn().mockResolvedValue(false);
        const opts = {
            label: "Jane Austen vs Quinn Walker",
            from: "B",
            to: "A",
            unsaved: false,
        };

        const result = await confirmRunningCourtMove(opts);

        expect(window.confirmDialog).toHaveBeenCalledTimes(1);
        expect(result).toBe(false);
    });
});
