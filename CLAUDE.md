# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CamKeep is a Go-based, self-hosted NVR (Network Video Recorder) for home NAS / edge devices. It is go2rtc-compatible: go2rtc handles stream ingestion and live viewing, FFmpeg handles recording, and a Gin web server provides the console on port 9110. It runs as a single container/binary. Code comments and commit messages are predominantly in Chinese; UI strings are bilingual (zh/en).

## Common Commands

- `go test ./...` — run the full test suite (tests are colocated as `*_test.go`)
- `go test ./internal/app -run TestName` — run a single test in one package
- `go build -o camkeep .` — build the local binary
- `go run .` — start locally on `http://localhost:9110` (needs `config/` and writable `records/`)
- `docker compose up -d` — containerized setup; `--shm-size=512m` is required for motion-detection Time-Shift cache
- There is no Makefile — use direct Go/Docker commands.

## Architecture

Startup flow (`main.go` → `internal/app/app.go:Run`): start go2rtc as a subprocess daemon → wait for its API on `127.0.0.1:1984` (fatal if not ready in 10s) → load/init config → register go2rtc streams → start stream-status polling, camera cover task, web server, and per-camera background tasks. On shutdown, cancel the global context, wait for task group, then kill the go2rtc child process.

### Package roles

- `internal/app` — application wiring, Gin HTTP handlers, local auth (users.json, session cookies, `CAMKEEP_AUTH_PASSWORD` env bootstrap), config load/save, camera covers, update checker, config hot-reload (`restartTasks` cancels the per-task context, waits, re-starts — guarded by `restartMux`). `webAuth` is a package-level global.
- `internal/task` — all background work: per-camera FFmpeg recording (`camera.go`), go2rtc daemon management + stream state polling (`go2rtc.go`), motion detection (`motion*.go`, `motion_recording.go`), ONVIF event manager, daily merge (`merge.go`), retention cleanup (`cleanup.go`).
- `internal/service` — shared runtime status (camera online state, ONVIF status) consumed by both tasks and handlers.
- `internal/onvif` — ONVIF SOAP client (`soap.go`), candidate discovery, PullPoint events (`event.go`, `pull_messages.go`), PTZ/imaging controls (`imaging.go`).
- `constant` — config types (`Config`, `Camera`), all file paths and defaults, motion-event-source constants. Config is shared process-wide via `ConfigMux` RWLock.
- `util`, `slog` — helpers and the custom rotating log writer. Frontend: Go templates in `template/`, plain JS/CSS in `static/` (no build tooling).

### Config & runtime state

- `config/conf.yaml` — camera definitions (config surface documented in `conf_usage.md`). Legacy field `rtsp_url` still works; `stream_url` wins when set. `stream_url: "managed_by_go2rtc"` means go2rtc owns the stream.
- `config/go2rtc.yaml` — go2rtc stream definitions; CamKeep can also import/scan streams go2rtc already has.
- `config/overrides.json` — persisted manual record overrides (`start`/`stop`/`auto` per camera).
- `config/users.json` — local users (admin/readonly roles, per-user camera scope).
- The web server proxies go2rtc endpoints on the single 9110 port: `/api/ws`, `/api/webrtc`, `/stream.html`, `/api/stream.mp4`, `/api/stream.m3u8`, plus `/webrtc/:id`. These proxies (and record playback endpoints) are the auth boundary — keep them behind `authRequired`.

### Recording layout & naming (important invariant)

Recordings live in `records/<camID>/<YYYY-MM-DD>/`. Filename format: `CamID_YYYYMMDD_HHMMSS_HHMMSS.ext` (start_YYYYMMDD_HHMMSS, end_HHMMSS). Normal recordings: FFmpeg `-strftime` writes `CamID_YYYYMMDD_HHMMSS_unknown.ext`, and a polling task (`renameCompletedSegments`) renames to add the end time; motion-detection recordings write the full name directly. Per-camera covers also live under `records/`.

### Motion detection

Three event sources (`motion_event_source`): `frame_diff` (local low-res frame diff), `onvif` (PullPoint), `auto` (ONVIF when healthy, else frame diff). Motion recordings use a Time-Shift cache (pre-roll buffer) that writes to `/dev/shm` — too small a shm-size causes short/failed motion clips. `motion_mark_enabled` overlays activity markers on the 24H timeline without affecting record start/stop.

### Build / release

- Version injected at build: `-ldflags "-X main.Version=<tag>"`; `main.Version` defaults to `dev`.
- Release: push a `v*` tag → GitHub Actions (`release.yml`) builds linux/amd64+arm64 binaries bundling a pinned go2rtc release, then pushes multi-arch Docker images to GHCR and Docker Hub. `Dockerfile` downloads go2rtc per `TARGETARCH` (note: the release workflow pins go2rtc v1.9.2, the Dockerfile v1.9.14).
- `fnnas.camkeep/` holds fnOS (FNOS NAS) app packaging (`manifest`, `wizard`, `.fpk` bundles) — update these when release packaging changes.

## Conventions & Guardrails

- Run `gofmt` on changed Go files. Keep JSON/YAML field names stable — they are the web API and config surface (add new fields, don't rename).
- Frontend: plain JS/CSS following existing style in `static/`; no new build tooling.
- Tests: Go `testing` package, colocated, named `Test...` — e.g. `internal/app/auth_test.go`, `internal/task/motion_test.go`. Run `go test ./...` before committing.
- Commits: short imperative summaries, optionally prefixed (`feat:`, `fix:`).
- Never commit camera credentials, real RTSP/ONVIF URLs, session secrets, or private NAS paths — sanitize examples in `conf.yaml` and docs.
- Endpoints that read/delete/download/proxy files or streams must validate paths and preserve the auth boundary (see `internal/app/web_server.go`).
- For RTSP/FFmpeg recording design questions, the project defines a subagent: `.claude/agents/ffmpeg-rtsp-recording-expert.md`.
