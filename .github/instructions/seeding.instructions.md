---
description: "Use when working on seed assignments, bracket ordering, seed validation, or the ApplySeeds/StandardSeeding functions. Covers seeding edge cases and collision handling."
applyTo: "internal/helper/seed*.go"
---

# Seeding Guidelines

## How Seeding Works
- `generateBracketOrder(n)` produces bracket positions (e.g., for 8: `[1,8,4,5,2,7,3,6]`)
- `StandardSeeding()` places seeded players at bracket positions, fills remaining with unseeded
- `ApplySeeds()` resolves collisions by **swapping seed values** — never fails on collision

## Validation Rules (from `internal/domain/seed.go`)
- Seed ranks must be **positive integers**
- Seed ranks must be **unique** — duplicate ranks are rejected
- Names must **exactly match** participant names (**case-sensitive**)
- Empty seed rank = unseeded (placed in remaining pool)

## Edge Cases to Handle
- More seeds than bracket positions → extra seeds become unseeded
- Seed rank higher than player count → placed at nearest valid position
- All players unseeded → original order preserved (no bracket reordering)
- Single seeded player → placed at position 1

## When Modifying
1. Always test with 0, 1, 2, and many seeded players
2. Verify bracket order stays correct for powers of 2 (4, 8, 16, 32)
3. Check collision swapping doesn't lose any players
4. Run `make go/test` — seed_test.go has comprehensive table-driven cases
