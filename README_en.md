# <img src="./static/image/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

[![Repo](https://img.shields.io/badge/Docker-Repo-007EC6?labelColor-555555&color-007EC6&logo=docker&logoColor=fff&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Version](https://img.shields.io/docker/v/r0n9/camkeep/latest?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Size](https://img.shields.io/docker/image-size/r0n9/camkeep/latest?sort=semver&labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Pulls](https://img.shields.io/docker/pulls/r0n9/camkeep?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![github: AlexxIT/go2rtc](https://img.shields.io/badge/Repo-AlexxIT/go2rtc-slategray?style=flat&logo=github&logoColor=white)](https://github.com/AlexxIT/go2rtc)
[![github: r0n9/camkeep](https://img.shields.io/badge/Repo-r0n9/camkeep-slategray?style=flat&logo=github&logoColor=white)](https://github.com/r0n9/camkeep)

[简体中文](./README.md) | [English](./README_en.md)

---

**A self-hosted NVR fully compatible with go2rtc, built for home NAS and edge devices.**

CamKeep is built with Go, go2rtc, and FFmpeg. It provides local-first camera ingest, recording, playback, and device control. It is no longer just a minimal RTSP recorder; it is a unified NVR gateway for go2rtc streams, ONVIF devices, and other go2rtc-compatible sources.

![camkeep](camkeep_console.png)

## Design Goals And Principles

CamKeep is not intended to replace large enterprise video security platforms. It is designed to be a practical, stable, and controllable NVR for home NAS, low-power mini servers, and self-hosted LAN deployments.

* **Minimal**: Single-container deployment, small configuration surface, and common operations available in the Web console instead of forcing users into complex video engineering details.
* **Low power**: Reuse go2rtc stream proxying, use stream copy by default for recording, and keep cover/status refresh frequency and concurrency under control for long-running NAS or ARM devices.
* **LAN-safe**: Local-first by default, no cloud dependency, and no video or device upload. Deploy it inside a trusted LAN, and enable local user authentication or a reverse proxy when needed.

## ✨ Feature Highlights

* 🧩 **go2rtc-native ingest**: `stream_url` accepts any go2rtc-compatible source, including `rtsp://`, `onvif://`, `ffmpeg:`, `exec:`, and existing go2rtc streams.
* 🕹️ **ONVIF PTZ control**: CamKeep discovers ONVIF control candidates and supports pan/tilt, zoom, stop, plus focus and iris controls when available.
* 🖼️ **Live covers**: Each live camera card can show a persisted cover image stored at `records/<camera>/cover.jpg`; CamKeep prefers go2rtc snapshots and falls back to local FFmpeg.
* 📺 **Compact live dashboard**: Camera cards are optimized for desktop and mobile. Covers are loaded only for visible cards, while the backend refreshes them periodically with low concurrency.
* 🕓 **24H timeline playback**: The original card list and timeline remain, and a new docked 24-hour timeline supports dragging, mouse-wheel zoom, mobile pinch zoom, and seeking by time.
* 🧰 **Web configuration management**: Single-page config management with form/YAML modes, collapsible camera cards, restore-before-save, single add, batch add, and importing unmanaged go2rtc streams.
* 🎥 **Practical recording modes**: Scheduled recording, manual start/stop, motion recording, timelapse, TS/MP4 segments, historical playback, download, and deletion.
* 🧠 **Selectable motion event source**: In normal mode, `motion_detect` can use local low-resolution frame differencing, ONVIF PullPoint, or automatic ONVIF-first fallback to frame differencing with a Time-Shift cache for event clips.
* 🧹 **Automatic storage management**: Retention cleanup, minimum-size filtering, and optional daily hourly merge keep long-running NAS deployments manageable.
* 🔒 **Local users and access control**: No cloud dependency, no required account, and no camera data upload. CamKeep supports local admin/viewer users, online session status, and per-camera visibility for viewers.

## Status And Modes

The `/api/status` field `mode` represents the runtime recording mode: `normal`, `motion`, or `timelapse`. `record_state` represents only the current state: `idle`, `recording`, `motion_detecting`, or `motion_recording`. Do not infer the recording mode from `record_state`.

## Source Configuration

`stream_url` is the camera source field. It means “go2rtc source”, not just an RTSP URL. The legacy `rtsp_url` field remains compatible, but new configs should use `stream_url`.

Common examples:

```yaml
stream_url: "rtsp://admin:password@192.168.1.10:554/Streaming/Channels/101"
stream_url: "onvif://admin:password@192.168.1.11"
stream_url: "ffmpeg:rtsp://admin:password@192.168.1.12/live#video=copy#audio=aac"
stream_url: "managed_by_go2rtc"
```

`managed_by_go2rtc` is usually written by the Web UI when importing an existing go2rtc stream. It means go2rtc owns the stream definition, while CamKeep handles recording, playback, and status.

## 🚀 Quick Deployment

The Docker image includes go2rtc and FFmpeg. Host networking is recommended, especially for low-latency WebRTC live view.

### 1. Prepare Directory And Optional Config

Create a base directory on your NAS or server, for example `/vol1/CamKeep`. For first-time deployment, you only need the `config/` and `records/` directories; if `config/conf.yaml` is missing, CamKeep will generate a default template on first start. You can still pre-create the file if you want a custom starting config. See [Configuration Usage](./conf_usage.md) for all fields.

The snippet below is only an optional starting example. You can also start the service first and then edit the generated template in Web Configuration.

```yaml
daily_merge:
  enabled: false
  time: "03:30"
  merge_motion_records: false

cameras:
  - id: "front-door"
    order: 0
    stream_url: "rtsp://admin:password@192.168.1.100:554/stream"
    motion_url: ""
    retention_days: 7
    segment_duration: 300
    format: "ts"
    min_size_kb: 1024
    record_time: "00:00-23:59"
    mode: "normal"
    motion_detect: false
    motion_event_source: "frame_diff"
    motionDetectRatioThreshold: 0.01
```

The `records` directory stores video files and the latest persisted cover image for each camera.

### 2. Start The Service

Login authentication is controlled by the local user file `config/users.json`. `CAMKEEP_AUTH_PASSWORD` is only used to bootstrap or add the built-in `admin` account; after the user file exists, login uses `users.json`, and changing the environment variable will not overwrite existing user passwords.

If `CAMKEEP_AUTH_PASSWORD` is not set and `users.json` is empty, the Web console starts with authentication disabled. You can create the first `admin` user from User Management, which enables login protection. `CAMKEEP_SESSION_SECRET` is an optional session signing key; if it is left empty, CamKeep generates a temporary key and users must log in again after restart. Set it only when you want sessions to survive restarts. When serving CamKeep through an HTTPS reverse proxy, you can set `CAMKEEP_AUTH_COOKIE_SECURE=true`.

#### Docker Run

```bash
docker run -d \
  --name camkeep \
  --restart unless-stopped \
  --network host \
  -e TZ=Asia/Shanghai \
  -e CAMKEEP_AUTH_PASSWORD=admin \
  -v ${PWD}/config:/app/config \
  -v ${PWD}/records:/app/records \
  ghcr.io/r0n9/camkeep:latest
```

#### Docker Compose

```yaml
services:
  camkeep:
    image: ghcr.io/r0n9/camkeep:latest
    container_name: camkeep
    restart: unless-stopped
    network_mode: "host" # Recommended for WebRTC
    environment:
      - TZ=Asia/Shanghai
      - CAMKEEP_AUTH_PASSWORD=admin
    volumes:
      - ./config:/app/config
      - ./records:/app/records
#    ports:
#      - "9110:9110"      # CamKeep Web UI
#      - "1984:1984"      # go2rtc API / console
#      - "8554:8554"      # go2rtc RTSP service
#      - "8555:8555/tcp"  # WebRTC
#      - "8555:8555/udp"
```

Then run:

```bash
docker-compose up -d
```

To keep existing login sessions after service restarts, optionally set a fixed `CAMKEEP_SESSION_SECRET`.

### 3. Open The Console

Visit `http://<Your-NAS-IP>:9110` in your browser. If `CAMKEEP_AUTH_PASSWORD` bootstrapped the admin account, log in as `admin` with that password. Manage later users, passwords, and permissions in Web User Management.

## Web Console

* **Live dashboard**: Cover image, online state, recording state, manual recording controls, and live preview.
* **History playback**: Camera/date based browsing with card list, classic timeline, and 24H timeline playback.
* **Configuration**: Form and YAML editors, batch camera add, and importing unmanaged go2rtc streams.
* **User management**: Local users, admin/viewer roles, enable/disable accounts, password resets, online session status, and per-camera access scope for viewers.
* **ONVIF controls**: PTZ, zoom, focus, and iris controls for supported devices.
* **Update check**: CamKeep checks GitHub Releases asynchronously after startup and then periodically. Stable builds show an update entry when a newer stable release exists. `dev`, `test`, and custom versions are not marked as stable upgrades.

## Privacy

CamKeep does not include telemetry by default and does not upload video, device lists, or usage behavior. The update checker only requests GitHub Releases metadata to determine whether a new version exists.

## 📄 License

This project is licensed under the **MIT License**. Issues and PRs are welcome.

This project uses:

- go2rtc — https://github.com/AlexxIT/go2rtc
  Licensed under the MIT License.

<a href="https://nextlaunch.io/projects/camkeep" target="_blank" title="Featured on Next Launch">
  <img src="https://nextlaunch.io/images/badges/nextlaunch-badge-light.svg" alt="Featured on Next Launch" style="width: 175px; height: auto;" />
</a>

---

<a href="https://www.star-history.com/?repos=r0n9%2Fcamkeep&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
 </picture>
</a>
