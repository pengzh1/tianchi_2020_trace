# Distributed Link Tracer with Statistics & Filtering

**English** | [中文](README.zh-CN.md)

![Rank](https://img.shields.io/badge/rank-9th%20of%204404-9cf)
![Contest](https://img.shields.io/badge/contest-1st%20Cloud%20Native%20Challenge%202020-blue)
![Language](https://img.shields.io/badge/language-Go-00ADD8)

9th-place solution (of 4404 teams) of the 1st Cloud Native Programming
Challenge (Alibaba Tianchi, 2020): *implement a distributed link tracer that
performs statistics and filtering*.

## Approach

A single-binary, multi-role Go service that ingests high-throughput trace
segments, aggregates them, and answers filtered statistical queries:

- **Multi-threaded reader** (`reader.go`) for parallel segment ingestion
- **Filter service** (`filter.go`) implementing the contest's filtering DSL
- **Backend service** (`backend.go`) — includes a panic-to-commit-time recovery mechanism
- **Buffered submission** (`retbuffer.go`) to accelerate result commits
  (turned out less effective than hoped, kept for reference)

## Project layout

| File | Role |
|---|---|
| `main.go` | entrypoint & role wiring |
| `reader.go` | multi-threaded segment reader |
| `filter.go` | filtering service |
| `backend.go` | backend service (with panic-to-commit-time handling) |
| `mockscoring.go` | local mock scoring service for faster iteration |
| `retbuffer.go` | buffered result submission |
| `config/` | docker-compose files for dev and official scoring (adjust image & volume paths) |

## Run

```bash
./build.sh
# local dev loop: docker-compose -f config/dev-go.yml up
# official scoring: docker-compose -f config/dev-score.yml up
```
