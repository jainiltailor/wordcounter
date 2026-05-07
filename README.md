# wordcounter

> A production-grade **parallel file word counter** written in Go —
> demonstrating goroutines, channels, context cancellation, and clean package design.

---

## Concurrency Architecture

The tool uses a classic **dispatcher → worker pool → aggregator** pipeline.
Files flow through buffered channels so no stage ever blocks another.

```
walker.Walk()
    │
    ▼
Dispatcher goroutine ──[jobCh, buffered]──▶ Worker-1 ──┐
                                        ──▶ Worker-2 ──┤──[resultCh, buffered]──▶ Aggregator ──▶ Stats
                                        ──▶ Worker-N ──┘
                                                              ▲
                                                    Closer goroutine
                                                    (wg.Wait → close resultCh)
```

| Step | What happens |
|---|---|
| `close(jobCh)` | Dispatcher signals workers — no more files |
| `wg.Wait()` | Closer waits for every worker to finish |
| `close(resultCh)` | Closer signals aggregator — no more results |
| Ordered index map | Results land at their original input position |

---

## Features

- **Parallel processing** — configurable worker goroutine count (default: CPU cores)
- **Streaming I/O** — `bufio.Scanner` reads one line at a time; memory is O(line), not O(file)
- **Zero-alloc word counting** — state machine instead of `strings.Fields`, zero heap allocations per line
- **Deterministic output order** — results always match input order despite goroutines finishing randomly
- **Multiple output formats** — coloured table, JSON, CSV
- **Flexible file discovery** — include/exclude glob patterns, optional recursive traversal
- **Graceful shutdown** — `SIGINT`/`SIGTERM` triggers context cancellation; partial results are printed
- **Race-safe** — passes `go test -race`

---

## Folder Structure

```
wordcounter/
├── cmd/
│   └── wordcounter/
│       └── main.go              ← CLI entry point: flags, wiring, signal handling
│
├── internal/
│   ├── counter/
│   │   ├── counter.go           ← Parallel engine: dispatcher + workers + aggregator
│   │   └── counter_test.go      ← Table-driven tests + benchmarks
│   │
│   ├── walker/
│   │   ├── walker.go            ← File discovery: recursive walk, glob filters, dedup
│   │   └── walker_test.go       ← Walker unit tests
│   │
│   └── reporter/
│       └── reporter.go          ← Output formatter: table / JSON / CSV + sort
│
├── .github/
│   └── workflows/
│       └── ci.yml               ← GitHub Actions: test-race + cross-platform build
│
├── WorkFlow.png                 ← Architecture diagram
├── go.mod                       ← Module: github.com/jainiltailor/wordcounter
├── Makefile                     ← build / test / bench / cover targets
└── README.md
```

**Package boundary rule:** `walker` → `counter` → `reporter` — no reverse imports.
`internal/` is Go-enforced: nothing outside this module can import these packages.

---

## Installation

```bash
go install github.com/jainiltailor/wordcounter/cmd/wordcounter@latest
```

Or build from source:

```bash
git clone https://github.com/jainiltailor/wordcounter
cd wordcounter
make build       # → ./bin/wordcounter
```

---

## Usage

```bash
# Count all .go files recursively, sorted by word count (descending)
wordcounter -r -include="*.go" -sort=words -desc ./

# JSON output — pipe into jq
wordcounter -format=json *.md | jq '.total.words'

# Exclude generated and test files, show byte counts
wordcounter -exclude="*.pb.go,*_test.go" -bytes -r ./internal

# Use 8 parallel workers (default: number of CPU cores)
wordcounter -w 8 ./large-corpus/

# Disable colour for piped output
wordcounter -no-color *.txt > report.txt
```

### All flags

| Flag | Default | Description |
|---|---|---|
| `-w` | `NumCPU` | Number of parallel worker goroutines |
| `-r` | `false` | Recursively traverse directories |
| `-include` | `""` | Comma-separated glob patterns to include (e.g. `*.go,*.md`) |
| `-exclude` | `""` | Comma-separated glob patterns to exclude (e.g. `*_test.go`) |
| `-format` | `table` | Output format: `table` \| `json` \| `csv` |
| `-sort` | `""` | Sort by: `words` \| `lines` \| `chars` \| `bytes` \| `name` |
| `-desc` | `false` | Sort in descending order |
| `-bytes` | `false` | Show byte counts in output |
| `-no-color` | `false` | Disable ANSI colour (auto-detected when piped) |
| `-version` | `false` | Print version and exit |

---

## Sample Output

```
LINES   WORDS   CHARS   FILE
──────  ──────  ──────  ────────────────────────────
312     1 840   11 203  internal/counter/counter.go
198     1 021    6 890  internal/reporter/reporter.go
154       872    5 431  internal/walker/walker.go
 87       401    2 980  cmd/wordcounter/main.go
──────  ──────  ──────  ────────────────────────────
751     4 134   26 504  TOTAL (4 files, 8 workers)
```

---

## Testing

```bash
make test        # all tests
make test-race   # with race detector — always run in CI
make bench       # benchmarks with memory allocation stats
make cover       # HTML coverage report → coverage.html
```

### Benchmark results (M2 MacBook Pro, 20 files × 50 KB)

```
BenchmarkCount_Workers/workers=1-10    100    11.2 ms/op    48 KB/op    3 allocs/op
BenchmarkCount_Workers/workers=2-10    100     6.1 ms/op    49 KB/op    3 allocs/op
BenchmarkCount_Workers/workers=4-10    100     3.8 ms/op    51 KB/op    3 allocs/op
BenchmarkCount_Workers/workers=8-10    100     2.9 ms/op    54 KB/op    3 allocs/op
```

Word counting itself achieves **zero heap allocations per line** — the state-machine
implementation avoids the `[]string` allocation that `strings.Fields` would create.

---

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | All files processed successfully |
| `1` | Usage error — no files matched or bad flags |
| `2` | Interrupted by `SIGINT`/`SIGTERM` — partial results shown |
| `3` | Completed but one or more files could not be read |

---

## Key Go Concepts Demonstrated

| Concept | Where |
|---|---|
| Worker pool with buffered channels | `counter.Count()` |
| `sync.WaitGroup` + Closer goroutine | `counter.go` |
| Context cancellation propagation | `main.go` → `counter.Count()` |
| Ordered results from nondeterministic goroutines | aggregator index map |
| Zero-alloc hot path | `countWords()` state machine |
| Table-driven tests with `t.Parallel()` | `counter_test.go` |
| Graceful HTTP-style shutdown with `signal.NotifyContext` | `main.go` |
| `internal/` package boundary enforcement | project layout |

---

## Author

**Jainil Tailor** — [github.com/jainiltailor](https://github.com/jainiltailor)

Built as part of a Master Engineering learning path: Go Fundamentals → Distributed Systems → Cloud.
