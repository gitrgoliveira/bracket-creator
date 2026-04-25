---
name: testing
description: 'Write and maintain Go tests for bracket-creator. Use when creating tests, adding test cases, fixing test failures, or improving coverage. Covers table-driven tests, test helpers, mocking, and cleanup patterns.'
---

# Testing Skill

## When to Use
- Writing new tests for existing or new code
- Adding test cases to existing table-driven tests
- Fixing broken tests or improving coverage
- Setting up mocks for dependencies

## Test File Setup

### Package Naming
- **Domain/internal packages**: Use `_test` suffix for black-box testing (e.g., `package domain_test`)
- **cmd packages**: Use same package name (e.g., `package cmd`) for access to unexported options structs

### Imports
```go
import (
    "testing"

    "github.com/gitrgoliveira/bracket-creator/internal/domain"
    "github.com/gitrgoliveira/bracket-creator/internal/test"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

## Procedure

### 1. Use Table-Driven Tests
Structure tests as slices of structs with `t.Run()`:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected OutputType
        wantErr  bool
    }{
        {
            name:     "success case",
            input:    validInput,
            expected: expectedOutput,
        },
        {
            name:    "error case",
            input:   invalidInput,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := MyFunction(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### 2. Use Test Helpers from `internal/test`
Available factories in [internal/test/helpers.go](../../internal/test/helpers.go):

| Helper | Returns | Use For |
|--------|---------|---------|
| `CreateTestPlayers()` | `[]domain.Player` | Two test players with different dojos |
| `CreateTestPools()` | `[]domain.Pool` | One pool with two players and a match |
| `CreateTestTournament()` | `domain.Tournament` | Full tournament with pools and elimination |

### 3. Assertions
- `assert.*` — Non-fatal checks (test continues on failure)
- `require.*` — Fatal checks (test stops immediately on failure)
- Use `require` for preconditions, `assert` for actual test checks

### 4. Mocking
- Use manual mocks with `testify/mock` — no code generation tools
- Use `testing/fstest.MapFS` for filesystem mocking
- Use `net/http/httptest` for HTTP server/handler testing

### 5. Cleanup
Always use `defer` for cleanup in tests:
```go
func TestWithFile(t *testing.T) {
    tmpFile := createTempFile(t)
    defer os.Remove(tmpFile)
    // ... test logic
}
```

### 6. Run Tests
```bash
make go/test  # lint + race detection + coverage
```

## Checklist
- [ ] Both success and error paths tested
- [ ] Table-driven with descriptive subtest names
- [ ] Cleanup via `defer` for any created resources
- [ ] `require` for preconditions, `assert` for checks
- [ ] Leveraged `internal/test` helpers where applicable
