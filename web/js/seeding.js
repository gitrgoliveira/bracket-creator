// Pure-logic helpers for seed assignment validation. The DOM wiring (modal,
// table population, button highlighting) lives in app.js.

// Validate the rank assignments captured from the seeds modal. Returns
// { ok, assignments, errors }. Each assignment is { Name, SeedRank }.
//
// rawInputs is an array of { name, rawValue } where rawValue is the raw
// textbox string (so we can ignore blanks and reject negatives uniformly).
export function validateSeedAssignments(rawInputs) {
    const assignments = [];
    const duplicates = new Set();
    const seenRanks = new Set();
    let maxRank = 0;

    for (const input of rawInputs) {
        const rank = parseInt(input.rawValue, 10);
        if (rank > 0) {
            if (seenRanks.has(rank)) {
                duplicates.add(rank);
            }
            seenRanks.add(rank);
            if (rank > maxRank) {
                maxRank = rank;
            }
            assignments.push({
                Name: input.name,
                SeedRank: rank
            });
        }
        // Non-positive or blank rawValues are ignored — caller is responsible
        // for clearing the textbox if it wants to reflect that.
    }

    const errors = [];
    if (duplicates.size > 0) {
        const dupList = Array.from(duplicates).sort((a, b) => a - b).join(', ');
        errors.push(`Duplicate seed ranks found: ${dupList}. All ranks must be unique.`);
    }

    const missingRanks = [];
    for (let i = 1; i <= maxRank; i++) {
        if (!seenRanks.has(i)) {
            missingRanks.push(i);
        }
    }
    if (missingRanks.length > 0) {
        const missingList = missingRanks.join(', ');
        errors.push(`Missing seed ranks in the sequence: ${missingList}. Ranks must be continuous from 1 to ${maxRank}.`);
    }

    return {
        ok: errors.length === 0,
        assignments,
        errors
    };
}
