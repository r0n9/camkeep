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

* 🧩 **go2rtc-native ingest**: Supports RTSP, ONVIF, FFmpeg, scripts, and other go2rtc-compatible sources, including existing go2rtc streams.
* 🕹️ **ONVIF control and event diagnostics**: CamKeep discovers ONVIF control candidates, supports PTZ, zoom, focus, and iris controls, and can diagnose Event service, PullPoint support, and recently received events.
* 🖼️ **Live covers**: Each live camera card can show a persisted cover image; CamKeep prefers go2rtc snapshots and falls back to local FFmpeg.
* 📺 **Compact live dashboard**: Camera cards are optimized for desktop and mobile. Covers are loaded only for visible cards, while the backend refreshes them periodically with low concurrency.
* 🕓 **24H timeline playback**: The original card list and timeline remain, and a new docked 24-hour timeline supports dragging, mouse-wheel zoom, mobile pinch zoom, and seeking by time.
* 🧰 **Web configuration management**: Single-page config management with form/YAML modes, collapsible camera cards, restore-before-save, single add, batch add, and importing unmanaged go2rtc streams.
* 🎥 **Practical recording modes**: Scheduled recording, manual start/stop, motion recording, timelapse, TS/MP4 segments, historical playback, download, and deletion.
* 🧠 **Selectable motion event source**: Use local low-resolution frame differencing, ONVIF PullPoint, or an automatic combination of both with a Time-Shift cache for event clips.
* 🧭 **Motion markers for continuous recording**: Continuous recording can overlay activity ranges on the 24H timeline without changing when recording starts or stops.
* 🧹 **Automatic storage management**: Retention cleanup, minimum-size filtering, daily hourly/continuous-range merging, and automatic repair for leftover recording fragments keep long-running NAS deployments manageable. Motion clips can also be merged when desired.
* 🔒 **Local users and access control**: No cloud dependency, no required account, and no camera data upload. CamKeep supports local admin/viewer users, online session status, and per-camera visibility for viewers.

## Source Configuration

CamKeep reuses go2rtc's ingest capabilities, so camera sources are not limited to plain RTSP URLs. You can use RTSP, ONVIF, FFmpeg, and other source types, or scan and import existing go2rtc streams from the Web configuration page. After import, go2rtc still owns the stream definition while CamKeep handles recording, playback, status, and device controls.

## ONVIF Events And Motion Markers

After ONVIF capability probing succeeds, CamKeep detects Event service and PullPoint support. The configuration page can expand ONVIF event diagnostics to show listener state, recent events, and start a 30-second PullPoint test listener.

Motion recording can use three event sources:

* Local frame differencing: watches low-resolution frame changes, useful for cameras without ONVIF events.
* ONVIF PullPoint: uses motion events reported by the camera and usually costs less CPU.
* Auto mode: prefers ONVIF events when healthy, then briefly follows up with frame differencing to extend or end the event window; falls back to local detection when ONVIF is unavailable.

Continuous recording can also enable motion markers. They do not control recording start or stop; they only add activity ranges to the 24H timeline so you can jump to moments with people, vehicles, or visible scene changes.

In the single-camera live view, ONVIF devices show an event overlay switch. When enabled, CamKeep only leases a PullPoint listener and displays recent events. The event list is shown when hovering over the button, and the recording strategy is unchanged.

## 🚀 Quick Deployment

The Docker image includes go2rtc and FFmpeg. Host networking is recommended, especially for low-latency WebRTC live view.

### 1. Prepare Directory And Optional Config

Create a base directory on your NAS or server, for example `/vol1/CamKeep`. For first-time deployment, you only need the `config/` and `records/` directories; if the config file is missing, CamKeep will generate a default template on first start. The easiest path is to start the service first and then add cameras in the Web configuration page. See [Configuration Usage](./conf_usage.md) for the full YAML reference.

The Web configuration page supports form editing, batch add, go2rtc import, ONVIF event diagnostics, and restore-before-save:

![CamKeep camera configuration page](camkeep_cam_config.png)

If you prefer a hand-written starting file, a minimal camera entry only needs an ID, a source, and a recording schedule. The rest can be tuned later in the Web UI.

```yaml
cameras:
  - id: "front-door"
    stream_url: "rtsp://admin:password@192.168.1.100:554/stream"
    record_time: "00:00-23:59"
```

The `records` directory stores video files and the latest persisted cover image for each camera.

### 2. Start The Service

Login is managed by local users. On first start, you can use the admin password shown in the examples below to bootstrap the built-in `admin` account; after that, manage accounts, passwords, and permissions in Web User Management. Changing the startup environment variable will not overwrite existing passwords.

If no initial admin password is provided and no users exist yet, the Web console starts with authentication disabled. Create the first `admin` user from User Management to enable login protection. Persistent sessions and HTTPS-only cookies can be configured later for more advanced deployments.

#### Docker Run

```bash
docker run -d \
  --name camkeep \
  --restart unless-stopped \
  --network host \
  --shm-size=512m \
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
    shm_size: "512m"
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

Keeping `--shm-size=512m` or `shm_size: "512m"` is recommended. CamKeep stores the motion-recording Time-Shift buffer in the container `/dev/shm` first, while Docker usually defaults it to 64MB. High-bitrate streams or multiple motion-recording cameras can otherwise make FFmpeg fail to write the buffer, which may show up as short motion clips or a stopped Time-Shift engine. 512MB is suitable for typical 1-2 camera setups; use `1g` or more for multiple cameras or high-bitrate main streams. If motion recording is not used, this can be lowered or omitted.

### 3. Open The Console

Visit `http://<Your-NAS-IP>:9110` in your browser. If the example environment variable bootstrapped the admin account, log in as `admin` with the password you set. Manage later users, passwords, and permissions in Web User Management.

## Web Console

* **Live dashboard**: Cover image, online state, recording state, manual recording controls, and live preview; ONVIF live windows can enable an event overlay.
* **History playback**: Camera/date based browsing with card list, classic timeline, and 24H timeline playback with motion marker overlays.
* **Configuration**: Form and YAML editors, batch camera add, importing unmanaged go2rtc streams, and ONVIF Event/PullPoint diagnostics.
* **User management**: Local users, admin/viewer roles, enable/disable accounts, password resets, online session status, and per-camera access scope for viewers.
* **ONVIF controls**: PTZ, zoom, focus, iris controls, and event test entry for supported devices.
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
