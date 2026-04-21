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
    - `helper/`: Implementation of bracket generation, Excel file creation, and business logic.
    - `excel/`: Lower-level Excel client and styling logic.
    - `resources/`: Management of embedded assets.
- `web/`: Frontend assets (HTML, CSS, JS) embedded into the binary using `go:embed`.
- `tests/`: Integration tests for the Web API and CLI.

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
    - Ensure tests pass with the `-race` flag: `go test -race ./...`.
- **Embedded Assets:** Static files in `web/` and the Excel `template.xlsx` are embedded. After modifying these, run `go generate ./...` if needed (though `go build` usually handles `embed`).
- **CI/CD:** GitHub Actions are used for validation (`.github/workflows/validate.yaml`), including security scans (`gosec`), linting, and coverage reporting via Codecov.
- **Git:** Never commit changes directly to `main` without a PR. Ensure the build and tests pass before requesting a review.
