# GEMINI.md

## Project Overview

`bracket-creator` is a specialized CLI and Web application designed to generate tournament brackets for Kendo competitions. It supports both straight knockout (Playoffs) and round-robin (Pools) formats, accommodating both individual and team matches.

### Key Technologies
- **Language:** Go (Golang)
- **CLI Framework:** [Cobra](https://github.com/spf13/cobra)
- **Web Framework:** [Gin](https://github.com/gin-gonic/gin)
- **Excel Manipulation:** [Excelize](https://github.com/xuri/excelize/v2)
- **Frontend:** Vanilla HTML/JS (embedded in the Go binary)
- **Containerization:** Docker & Docker Compose

### Architecture
- `cmd/`: CLI command definitions (serve, create-playoffs, create-pools, version).
- `internal/`: Core logic and domain models.
    - `domain/`: Domain entities (Tournament, Pool, Match, Player).
    - `helper/`: Implementation of bracket generation, pool creation, Excel file creation, and business logic.
    - `excel/`: Lower-level Excel client and styling logic.
    - `resources/`: Management of embedded assets.
- `web/`: Frontend assets (HTML, CSS, JS) embedded into the binary using `go:embed`.
- `tests/`: Integration tests for the Web API and CLI.
- `specs/`: Contains the OpenAPI specification (`openapi.yaml`) for the web API.

### Seeding Logic
- **Playoffs (`StandardSeeding`)**: Uses a power-of-2 bracket distribution (e.g., seeds 1 and 2 on opposite halves). Includes displaced seed placement using a furthest-distance heuristic for out-of-range seeds.
- **Pools (`PoolSeeding`)**: Distributes seeds across pools using an "extremes and middle" balanced priority distribution (e.g., for 12 pools: Pool 1, Pool 12, Pool 6, Pool 7), with cyclic priority for additional seeds.

### Pool Scoring Rules
Rankings within pools are determined by the following criteria:

**Individual Tournaments:**
1. Higher number of fights won (Matches Won).
2. Lower number of fights lost (Matches Lost).
3. Higher number of hikiwake (Matches Tied).
4. Higher number of points scored (Points Won).
5. Lower number of points lost (Points Against).

**Team Tournaments:**
1. Higher number of team matches won (W).
2. Lower number of team matches lost (L).
3. Higher number of draws in team matches (T).
4. Higher number of individual winners (IV).
5. Lower number of individual losses (IL).
6. Higher number of individual draws (IT).
7. Higher number of points scored (PW).
8. Lower number of points lost (PL).

### Team Match Winning Criteria
Individual encounters between teams are decided by:
1. Highest number of individual winners (Victories).
2. Highest number of points scored.
3. If still tied, the match is a draw in pool play, or proceeds to a play-off in elimination rounds.

### Tie-marking Rule
A match (individual or sub-match) is ONLY considered a tie if the operator enters an **'X'** (or 'x') in the "vs" column between the players. Equal scores without an 'X' are NOT treated as ties. The "vs" column is unlocked on all sheets to facilitate this.

### Match Colors
On tree and playoff brackets, the player/team on the top of the bracket is always assigned the color **Red (Aka)** and the player/team on the bottom is assigned **White (Shiro)**.

## Building and Running

### Prerequisites
- Go 1.26.2+
- Make

### Key Commands
- **Build the application:** `make go/build` (outputs to `./bin/bracket-creator`)
- **Run the Web UI:** `make run` or `./bin/bracket-creator serve`
- **Run Tests (fast):** `make go/test`
- **Run Tests (with race detection):** `make go/test-race`
- **Run Linters:** `make go/lint`
- **Generate Examples:** `make examples`
- **Build Docker Image:** `make docker/build`

### CLI Usage Examples
- **Create Playoffs:**
  ```bash
  ./bin/bracket-creator create-playoffs -f players.csv -o bracket.xlsx
  ```
- **Create Pools:**
  ```bash
  ./bin/bracket-creator create-pools -p 3 -w 2 -f players.csv -o pools.xlsx
  ```

## Development Conventions

- **Code Style:** Follow standard Go idioms. Use `go fmt` and `golangci-lint` for consistency.
- **Dependency Management:** Use `go mod tidy` to manage `go.mod` and `go.sum`.
- **Testing:**
    - New features should include unit tests in the same directory as the implementation.
    - Integration tests should be added to the `tests/` directory.
    - Ensure tests pass with: `make go/test`.
- **Embedded Assets:** Static files in `web/` are embedded via `//go:embed`. The Excel workbook is built entirely from code in `internal/excel/template.go`. After modifying embedded web assets, rebuild with `go build` (or `make go/build`).
- **CI/CD:** GitHub Actions are used for validation (`.github/workflows/validate.yaml`), including security scans (`gosec`), linting, and coverage reporting via Codecov.
- **Git:** Never commit changes directly to `main` without a PR. Ensure the build and tests pass before requesting a review.
