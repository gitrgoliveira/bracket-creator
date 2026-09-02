<!--
SYNC IMPACT REPORT
==================
Version change: 1.3.0 → 1.3.1 (PATCH: terminology wording only)
Modified principles: N/A (no principle text changed)
Terminology: "playoffs" → "knockout" in Principle I (the CLI verb, now
  `create-knockout`) and Principle VI ("knockout brackets"). The European
  Kendo Championships rules call the elimination stage the "knockout system"
  and reserve "play-off" for a tie-break representative bout (daihyosen), so
  the old wording collided with the sport's own vocabulary. Tracks mp-sfi8.
Added sections:
  - Quality Gates: added "Connected UX" row enforcing browser-based verification
    (manual or agent-driven via browser MCP) for any PR touching web-mobile/.
    Closes the gap where Principle VII demanded browser testing but the
    Quality Gates table had no corresponding row to enforce it.
Removed sections: N/A
Templates requiring updates:
  - .specify/templates/plan-template.md  — no change required
  - .specify/templates/spec-template.md  — no change required
Deferred TODOs: N/A

Prior change (1.1.0 → 1.2.0):
Modified principles:
  - Principle II: Expanded "Simplicity over Cleverness" to cover DRY explicitly alongside YAGNI.
  - Principle V: Renamed to "Test-Driven Development (TDD)" and tightened Red-Green-Refactor rules.
Added sections:
  - Principle VIII: Domain-Driven Design (DDD).
-->

# Bracket Creator Constitution

## Core Principles

### I. Two First-Class Interfaces: CLI for Paper, Mobile App for Connected Tournaments

The project serves two distinct tournament delivery modes, each with its own
primary interface:

- **Excel / paper-only tournaments** — The CLI (`bracket-creator create-pools`,
  `create-knockout`, etc.) MUST remain the primary interface. Every core
  bracket-generation feature MUST be accessible via subcommands. The web server
  is a convenience layer that MUST NOT contain logic unavailable at the CLI level.

- **Connected tournaments (WiFi / internet available)** — The mobile app
  (`bracket-creator mobile-app`, exposed via `make run-mobile`) is a first-class
  interface. It MUST provide a complete live-tournament workflow: participant
  management, pool/bracket generation, real-time match scoring, and result
  broadcasting. Features that only make sense in a live, connected context MAY
  ship in the mobile app without a CLI equivalent.

**Rationale**: CLI interfaces are composable, scriptable, and testable without a
browser, making them ideal for offline/batch use. The mobile app, however, solves
a distinct problem — running a live event — where a real-time UI is the right
tool and headless use is not a requirement. Treating both as first-class prevents
either domain from being a second-class afterthought.

**Non-negotiable rules**:
- Bracket/pool generation logic MUST live in `internal/helper` or `internal/engine`
  and be shared by both the CLI and the mobile app — no duplicate implementations.
- CLI text I/O protocol: `stdin`/args → `stdout`, errors → `stderr`.
- JSON output MUST be supported alongside human-readable formats wherever
  structured output is useful.
- Mobile app features that modify tournament state MUST go through the
  `internal/state` store — never directly mutate files from handler code.

### II. Simplicity over Cleverness (YAGNI + DRY)

The codebase MUST favour the simplest solution that satisfies the requirement.
Complexity MUST be explicitly justified; no pattern, abstraction, or dependency
may be added without a documented rationale. Logic that appears in more than one
place MUST be extracted into a single canonical location.

**YAGNI (You Aren't Gonna Need It)**: Do not add functionality until it is
required. Speculative generality is a liability, not an asset.

**DRY (Don't Repeat Yourself)**: Every piece of knowledge — algorithm, constant,
business rule, schema — MUST have a single, unambiguous representation in the
codebase. Duplication between Go and JS (e.g., validation caps, CSV parsing) is
technical debt that MUST be tracked and resolved.

**Rationale**: Bracket creation for kendo tournaments is a well-scoped domain.
Over-engineering risks maintenance burden and contributor friction. Duplication
diverges silently under maintenance, leading to inconsistent behaviour between
the CLI and the mobile app.

**Non-negotiable rules**:
- New abstractions require a documented reason in the relevant RFC or PR.
- Dependencies MUST be evaluated against build/binary-size impact.
- If a simpler alternative exists and was rejected, the reason MUST be recorded
  (see Abandoned Ideas in RFC guidance).
- Constants, validation rules, and business logic shared across Go and JS MUST
  have the Go implementation as the source of truth; JS mirrors it and MUST NOT
  diverge silently.

### III. Evidence-Based Decision Making

All technical and product decisions MUST be grounded in evidence, not
assumptions. Claims about performance, correctness, or user need require
supporting data or explicit acknowledgement that they are unvalidated hypotheses.

**Rationale**: Derived from the Critical Thinking skill. Blind agreement,
rubber-stamping, and "trust me" justifications produce unmaintainable systems
and incorrect features. Decisions made without evidence are risks that MUST be
surfaced, not hidden.

**Non-negotiable rules**:
- Performance claims MUST cite benchmarks or be marked "UNVALIDATED HYPOTHESIS".
- Architectural proposals MUST document alternatives considered and why they
  were rejected.
- Bug reports MUST include reproduction steps before a fix is implemented.
- Cost/effort estimates are assumptions until validated; treat them as such.

### IV. Document-Driven Development

Significant changes MUST be preceded by a written artifact (RFC, spec, plan,
or memo) before implementation begins. The document drives alignment, not
the code.

**Rationale**: HashiCorp's document-driven culture enables async review,
preserves institutional context, and prevents costly re-work. Documents
create a decision audit trail that code alone cannot provide.

**Non-negotiable rules**:
- Features beyond single-component, < 1-week scope MUST have a spec and plan.
- Technical architecture changes MUST have an RFC.
- All decisions MUST document abandoned alternatives.
- Documents follow the lifecycle: WIP → In Review → Approved → Obsolete.
  Obsolete documents are archived, never deleted.

### V. Test-Driven Development (TDD)

All new behaviour MUST be driven by tests written **before** the production
code. Tests are the primary specification mechanism; implementation fills in
what tests require.

**TDD cycle (mandatory)**:
1. **Red** — Write a failing test that describes the desired behaviour.
2. **Green** — Write the minimum code to make the test pass.
3. **Refactor** — Clean up without breaking the green state.

Skipping the Red step (writing tests after) is not TDD — it is retrofitting
and MUST NOT be presented as test-driven.

**Rationale**: The project generates Excel output for live tournament use.
Incorrect bracket generation or pool assignment has real-world consequences
(wrong opponents, disqualified dojo groupings). Writing tests first forces
explicit specification of acceptance criteria and prevents implementation bias
from shaping what is tested.

**Non-negotiable rules**:
- Unit tests MUST cover core bracket/pool logic.
- Integration tests MUST cover end-to-end CLI invocations.
- Tests MUST exist in a failing state before production code is written.
- No PR may remove test coverage without documented justification.
- Test file naming follows the existing convention: same package for `helper`
  and `cmd`; `_test` suffix package for `domain`.

### VI. Correctness of Output (Bracket Integrity)

The output files (`.xlsx`) MUST correctly implement the tournament rules
they represent: pools MUST avoid same-dojo conflicts when grouping,
knockout brackets MUST be seeded correctly, and round-robin constraints
MUST be honoured when enabled.

**Rationale**: Tournament administrators rely on this tool for fair competition
outcomes. An incorrect bracket is not a UX issue — it undermines the integrity
of the event. Output correctness is the product's core value proposition.

**Non-negotiable rules**:
- Same-dojo constraint MUST be enforced in pool assignment when dojo data
  is present.
- Bracket seeding logic changes MUST be reviewed by the maintainer.
- Output file generation MUST be validated against known-good fixtures in CI.

### VII. Live Tournament Management (Mobile App Domain)

The mobile app operates under constraints that differ fundamentally from the
CLI/Excel domain: it runs during a live event, data loss is unrecoverable in the
moment, and participants and administrators depend on it in real time.

**Rationale**: A bug in bracket generation can be fixed and re-run before the
event starts. A bug in live match scoring discovered mid-tournament cannot be
rolled back — results may already have been acted on. This domain requires its
own quality bar and architectural rules.

**Non-negotiable rules**:
- **State durability**: Tournament and match state MUST be persisted to disk
  (via `internal/state`) before the server responds with success. An in-memory
  shortcut that risks data loss on restart is not acceptable.
- **Real-time push**: Score updates, pool results, and bracket transitions MUST
  be broadcast to all connected viewers via SSE (`internal/mobileapp/hub.go`)
  without requiring a page reload.
- **Availability over features**: During a live event the app MUST remain
  operational if non-critical subsystems fail. Degrade gracefully; never crash
  the server on a recoverable error.
- **Auth boundary**: All state-mutating API endpoints MUST require the
  `X-Tournament-Password` header. Read-only viewer routes MAY be unauthenticated.
- **No regressions on connected UX**: Changes to `web-mobile/` MUST be tested in
  a running browser against `make run-mobile` before merge — type checking alone
  is insufficient because the frontend is served embedded and compiled at build
  time.
- **Mobile app independence**: The mobile app MUST function as a self-contained
  binary. It MUST NOT require an external database, message broker, or cloud
  service to run a tournament.

### VIII. Domain-Driven Design (DDD)

The project's business logic MUST be expressed in terms of the domain — kendo
tournament management — not in terms of infrastructure concerns (Excel cells,
HTTP handlers, file paths). The domain model is the single authoritative source
of tournament rules and vocabulary.

**Ubiquitous Language**: All code, tests, comments, and documentation MUST use
the tournament domain vocabulary consistently: *pool*, *match*, *participant*,
*court (shiaijo)*, *seed*, *ippon*, *waza*, *dojo*, *hikiwake*. Synonyms that
leak implementation details (e.g. "row", "slot", "entry") MUST be confined to
the infrastructure layer.

**Bounded Contexts**:
- **Bracket Generation** — `internal/helper`, `internal/engine`: pool creation,
  seeding, elimination bracket construction. Output: domain objects, not Excel.
- **Excel Rendering** — `internal/excel`, `internal/helper` (render methods):
  translates domain objects to `.xlsx`. MUST NOT contain business rules.
- **Live Tournament** — `internal/mobileapp`, `internal/state`: match scoring,
  real-time broadcast, participant management. Depends on Bracket Generation;
  MUST NOT contain bracket/seeding logic.
- **CLI** — `cmd/`: thin shell that wires domain services to I/O. MUST NOT
  contain business logic.

**Domain Model Migration**: The project is transitioning from `internal/helper`
types (Excel-coordinate-bearing) to clean domain types in `internal/domain`.
New features MUST prefer `internal/domain` types. Migration of existing helper
types is incremental and MUST NOT break existing behaviour.

**Rationale**: Coupling business rules to Excel coordinates or HTTP payloads
makes them untestable in isolation and brittle to output format changes. A clean
domain layer allows bracket logic to be validated without generating a file, and
lets the CLI and mobile app share a single implementation of tournament rules.

**Non-negotiable rules**:
- Business rules (scoring, seeding, dojo constraints) MUST live in
  `internal/helper` or `internal/domain`, never in handlers or CLI commands.
- Domain types MUST NOT import `excelize`, `gin`, or any infrastructure package.
- New domain concepts MUST be named using the ubiquitous language defined above.
- When a helper type is migrated to `internal/domain`, the old type MUST be
  removed or aliased — no parallel implementations of the same concept.

## Development Workflow

- Features start with a user-facing description of the problem to solve
  (Working Backwards from the user experience).
- For features > 1 week: write a PRFAQ sketch (why now, who benefits) →
  spec (`spec.md`) → implementation plan (`plan.md`) → tasks (`tasks.md`).
- For features < 1 week / single component: a brief memo or PR description
  with rationale suffices.
- All work targets the `main` branch via Pull Requests. PRs MUST reference
  the associated spec or memo where applicable.
- The CI pipeline (CodeQL, tests, linting) MUST pass before merge. No
  exceptions without documented, time-bound justification.
- Commit messages MUST be descriptive. Prefer: `<type>: <summary>` format
  (e.g., `feat: add round-robin flag to create-pools`).

## Quality Gates

Every PR MUST satisfy the following before merge:

| Gate | Requirement |
|------|-------------|
| Tests | `go test ./...` passes with no failures |
| Lint | `go vet` and project linter pass |
| Security | CodeQL scan shows no new critical/high findings |
| Bracket Integrity | Output fixtures match expected results for affected commands |
| Connected UX | For any PR touching `web-mobile/`, the author certifies in the PR description that they (or an agent on their behalf) exercised `make run-mobile` and walked through at least one quickstart scenario covering the change. For agent-driven verification, the PR description lists the screenshot paths captured during the browser-tool playbook (see `specs/<feature>/screenshots/`). Type checking alone is insufficient because the frontend is served embedded and compiled at build time. |
| Documentation | README and CLI `--help` text updated if behaviour changes |
| Complexity Justified | Any new abstraction, dependency, or pattern is explained |

## Governance

This constitution supersedes all other implicit or informal practices for
the `bracket-creator` project. Amendments require:

1. A written proposal (memo or PR description) describing the change and its
   rationale.
2. Review by the project maintainer (`gitrgoliveira`).
3. A version bump per semantic versioning rules:
   - **MAJOR**: Backward-incompatible governance changes, principle removals,
     or fundamental redefinitions.
   - **MINOR**: New principle or section added, or materially expanded guidance.
   - **PATCH**: Clarifications, wording, or typo fixes.
4. Update of `Last Amended` date on ratification.

All PRs and feature reviews MUST verify compliance with the principles above.
Complexity violations MUST be documented in the plan's Complexity Tracking
table before the PR is opened.

For AI-agent runtime guidance, refer to `.specify/` templates and skill
files in `.agents/skills/`.

**Version**: 1.3.1 | **Ratified**: 2026-03-02 | **Last Amended**: 2026-09-02
