# Python → Go Migration Tracker

**Migration complete.** All Python sources, tests, and infrastructure have been removed. The codebase is now pure Go.

## Final state

| Module | Go LOC | Tests |
|--------|--------|-------|
| API client | 502 | — |
| Config | 358 | ✅ |
| Options | 211 | ✅ |
| Rebalance | 2292 | ✅ |
| TUI app | 1020 | ✅ |
| TUI components | 773 | — |
| TUI modals | 1422 | — |
| TUI table | 468 | — |
| TUI theme | 100 | — |

## Completed tasks

### P0 — Critical functional gaps
All 6 completed.

### P1 — Rebalance edge cases + TUI behavior
All 17 completed.

### P2 — Config + platform
All 4 completed.

### P2 — Tests
All 6 completed:
- [x] `internal/options/options_test.go` — OCC parsing, OptionPosition, extraction
- [x] `internal/config/config_test.go` — migration, account CRUD, credentials
- [x] `internal/rebalance/rebalance_test.go` — delta computation, stock weights, top-N
- [x] `internal/tui/app_test.go` — model init, pane navigation, height splitting
- [x] CI — GitHub Actions runs `go build`, `go vet`, `go test`

### P3 — Cleanup
All completed:
- [x] Deleted Python sources
- [x] Deleted Python tests and fixtures
- [x] Removed `pyproject.toml`, `uv.lock`
- [x] Updated `.github/workflows/ci.yml` for Go
- [x] Updated `.github/workflows/release.yml` for Go binaries
- [x] Updated `README.md` for Go build + run
- [x] Removed Python install hints from Go code

## Verification

```bash
go build ./...     # clean
go vet ./...       # clean
go test ./...      # 50 tests pass
```

## Key file references

- Entry: `cmd/public-terminal/main.go`
- TUI: `internal/tui/app.go`, `internal/tui/keymap.go`, `internal/tui/modals/*.go`, `internal/tui/components/*.go`
- API: `internal/api/client.go`, `internal/api/types.go`
- Config: `internal/config/config.go`, `internal/config/migrate.go`, `internal/config/systemd.go`
- Rebalance: `internal/rebalance/{index,marketcap,orders,rebalance,xlsx}.go`
- Options: `internal/options/options.go`
