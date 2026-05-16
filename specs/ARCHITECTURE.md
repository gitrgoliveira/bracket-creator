# Architecture: Bracket Creator

This document describes the architectural design and key components of the `bracket-creator` application.

## 1. High-Level Architecture

`bracket-creator` follows a layered architecture, cleanly separating the presentation layer (CLI and Web API), service coordination, business domain logic, and file I/O operations.

```text
CLI / Web Layer (cmd/)
        │
   Service Layer (internal/service/)
        │
   Business Logic (internal/helper/)
        │
   Domain Models (internal/domain/)
        │
   Excel / IO Layer (internal/excel/)
```

### Core Responsibilities
- **Presentation:** Parses inputs (flags, HTTP requests), orchestrates the overall flow, and presents outputs or errors to the user.
- **Service Layer:** Acts as a facade over complex business operations, reducing the coupling between the presentation layer and the underlying algorithms.
- **Business Logic:** Implements the core rules for kendo tournaments: pool generation, seeding logic, bracket/tree construction, and dojo conflict resolution. 
- **Domain Models:** Defines the core entities (e.g., `Player`, `Pool`, `Match`, `Tournament`, `Seed`) that represent the application's vocabulary, completely decoupled from presentation or I/O concerns.
- **Excel/IO Layer:** Handles low-level file operations, specifically the generation and formatting of the complex Excel `.xlsx` output files.

## 2. Key Components

### 2.1. The `cmd/` Package (Presentation Layer)
- Utilizes the **Cobra framework** for CLI subcommands (`create-pools`, `create-playoffs`, `serve`).
- Shared logic across commands (e.g., CSV parsing, input validation) is centralized in `cmd/shared.go`.
- The `serve` command spins up a **Gin HTTP server** serving both a REST API and embedded web assets.

### 2.2. The `internal/helper/` Package (Business Logic)
*Note: This package contains legacy code transitioning toward the newer `domain` and `service` models.*
- **Seeding Algorithms (`seed.go`):** 
  - `StandardSeeding`: Uses a power-of-2 distribution with a furthest-distance heuristic for displaced seeds.
  - `PoolSeeding`: Distributes seeds across pools using an "extremes and middle" balanced priority distribution.
- **Bracket Generation (`tree.go`):**
  - Represents the tournament bracket as a recursive binary tree (`Node` struct).
  - Supports subdividing large brackets across multiple Excel sheets (max 16 players per tree/page).
- **Tournament Organization (`tournament.go`):**
  - Implements greedy pool creation.
  - Ensures dojo-conflict avoidance during early match stages.

### 2.3. The `internal/domain/` Package
Contains pure structural definitions of tournament concepts, ensuring that business entities do not leak implementation details from the `excel` or `helper` packages.

### 2.4. The `internal/excel/` Package
- Wraps the `xuri/excelize/v2` library.
- The entire workbook is built dynamically from scratch (e.g., via `template.go`).
- Controls complex spreadsheet operations: cell merging, formula linking across sheets (e.g., automatically populating the knockout bracket with winners from the pool stage), and applying visual styles.

### 2.5. Resources & Embedding (`internal/resources/`)
- Uses Go's `//go:embed` functionality to bake frontend assets (`web/`) into the final binary.
- Exposes resources through an `fs.FS` interface to allow seamless dependency injection for both production runs (using `embed.FS`) and testing environments (using `fstest.MapFS`).

## 3. Data Flow

**Example Flow: Generating a Pools Tournament via the Web API (`/create`)**
1. **Input:** The Gin router receives a POST request containing tournament configuration and a CSV list of participants.
2. **Validation:** The `cmd` layer validates the request, checks for duplicate players, and parses any provided seed assignments.
3. **Initialization:** The input is handed off to the Service/Helper layer. Player objects are instantiated.
4. **Pool Assignment:** `PoolSeeding` distributes top players to prevent early clashes. The remaining players are assigned to pools, strictly respecting dojo-conflict avoidance rules.
5. **Bracket Construction:** Winners from the generated pools are mapped to a binary tree to represent the final knockout stage.
6. **Excel Generation:** The `excel` layer creates a new workbook in memory. It generates sheets for Pool Draws, Pool Matches, and Elimination brackets, drawing borders and inserting formulas to link pool winners to the playoff tree.
7. **Output:** The complete Excel file is streamed back to the client as a binary response payload.

## 4. Design Patterns & Principles

- **Command Pattern:** Employed via Cobra to encapsulate execution logic for discrete CLI functions.
- **Dependency Injection:** System resources (like embedded HTML/CSS) are injected, making the system highly testable without relying on physical files.
- **Fail-Fast Error Handling:** The application leverages strict linter enforcement (`errcheck`) and comprehensive input validation to catch configuration or formatting errors before engaging the heavy Excel generation logic.
- **Immutable Output:** The application does not edit existing Excel templates on disk. It produces a fresh, deterministic workbook for every run based on hardcoded template structures and constants (found in `internal/helper/constants.go`).