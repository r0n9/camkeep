
# <img src="./static/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

[![Repo](https://img.shields.io/badge/Docker-Repo-007EC6?labelColor-555555&color-007EC6&logo=docker&logoColor=fff&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Version](https://img.shields.io/docker/v/r0n9/camkeep/latest?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Size](https://img.shields.io/docker/image-size/r0n9/camkeep/latest?sort=semver&labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Pulls](https://img.shields.io/docker/pulls/r0n9/camkeep?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![github: AlexxIT/go2rtc](https://img.shields.io/badge/Repo-AlexxIT/go2rtc-slategray?style=flat&logo=github&logoColor=white)](https://github.com/AlexxIT/go2rtc)
[![github: r0n9/camkeep](https://img.shields.io/badge/Repo-r0n9/camkeep-slategray?style=flat&logo=github&logoColor=white)](https://github.com/r0n9/camkeep)

[简体中文](./README.md) | [English](./README_en.md)

---

**A lightweight, self-hosted Network Video Recorder (NVR) designed for Home NAS.**


CamKeep is a streaming and recording gateway written in Go, deeply integrated with **go2rtc** and **FFmpeg**. Designed for home NAS (FnOS, Synology, QNAP, Unraid, etc.) and low-power micro-servers, it allows you to say goodbye to expensive proprietary NVRs and privacy risks.

## ✨ Feature Highlights

CamKeep has one clear goal: keep RTSP cameras recording reliably to your own NAS, inside your local network, with minimal setup and low resource usage.

* 🐳 **Simple single-container deployment**: go2rtc and FFmpeg are bundled in one Docker image. Start it, mount config and records, then manage updates from the Web UI.
* 🔒 **Private local-network operation**: No cloud dependency, no required account, no public service required. Streams, recordings, and playback stay on your LAN and NAS.
* ⚡ **Low-power friendly**: go2rtc acts as a stream proxy, so multiple viewers can share one upstream camera connection. Normal recording uses `copy` by default to avoid unnecessary re-encoding.
* 📹 **Works with any RTSP source**: Hikvision, Dahua, TP-Link, flashed smart cameras, old phones, and existing go2rtc streams can all be brought into CamKeep.
* 🎥 **Practical recording modes**: Scheduled recording, manual start/stop, motion recording, timelapse, TS/MP4 segments, date-based playback, and historical clip browsing.
* 🧠 **Efficient motion recording**: Motion detection uses low-resolution frame differencing with a Time-Shift buffer, creating event clips only when the scene changes.
* 🧹 **Automatic retention**: Set `retention_days` and CamKeep will clean expired recordings in the background, making it suitable for long-running NAS deployments.
* 🖥️ **Useful monitoring dashboard**: Low-latency WebRTC live view with MSE / mpegts fallback, 4/6-grid preview, double-click fullscreen, device status, and date-based playback.
* 🏗️ **Built for NAS and edge devices**: Native x86-64 and ARM64 images for Synology, QNAP, Unraid, FnOS, Raspberry Pi, RK3588, and similar low-power systems.

---

## 🚀 Quick Deployment

Thanks to full integration, CamKeep only requires mapping ports and directories to start. Choose between `Docker Run` or `Docker-Compose`.

### 1. Prepare Directory & Configuration

> 💡 Versions v1.1.0 and later support updating configuration via the Web UI, but it is recommended to prepare the config directory for the first launch.

Create a base directory on your NAS (e.g., `/vol1/CamKeep`) and create `config/conf.yaml`:

For detailed configuration options, see: [Configuration Usage (conf_usage.md)](https://github.com/r0n9/camkeep/blob/main/conf_usage.md)

```yaml
daily_merge:
  enabled: false          # Merge yesterday's video segments every day
  time: "03:30"           # Daily merge time, preferably during off-peak hours

cameras:
# Normal recording mode example
  - id: "front-door"      # Unique ID (Alphanumeric)
    rtsp_url: "rtsp://admin:123456@192.168.1.100:554/stream"
    retention_days: 7       # Keep recordings for 7 days
    segment_duration: 300   # 5-minute segments
    format: "ts"            # TS is highly recommended for instant playback
    record_time: "00:00-24:00" # Allowed recording schedule
    mode: "normal"
    motion_detect: false    # Motion recording is disabled by default
    motionDetectRatioThreshold: 0.01 # Motion threshold, default 1%
```

### 2. Start Service (Choose One)

#### Method 1: Docker Run (Recommended for simple deployment)

Run this command in your terminal (replace `${PWD}` with your actual path):

```bash
docker run -d \
  --name camkeep \
  --restart unless-stopped \
  --network host \
  -e TZ=Asia/Shanghai \
  -v ${PWD}/config:/app/config \
  -v ${PWD}/records:/app/records \
  r0n9/camkeep:latest  # Use ghcr.io/r0n9/camkeep:latest if network is slow
```

#### Method 2: Docker-Compose

Create `docker-compose.yaml` in the same directory:

```yaml
services:
  camkeep:
    image: r0n9/camkeep:latest
    container_name: camkeep
    restart: unless-stopped
    network_mode: "host" # Host mode is recommended for WebRTC
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./config:/app/config
      - ./records:/app/records
#    ports:
#      - "9110:9110"      # CamKeep Web UI
#      - "1984:1984"      # go2rtc API Port
#      - "8554:8554"      # RTSP Service Port
#      - "8555:8555/tcp"  # WebRTC Port (Required for live viewing)
#      - "8555:8555/udp"
```

Then execute:
```bash
docker-compose up -d
```

### 3. Get Started
Access `http://<Your-NAS-IP>:9110` in your browser to enter the monitoring center.

## 📄 License

This project is licensed under the **MIT License**. Issues and PRs are welcome.

This project uses:

- go2rtc — https://github.com/AlexxIT/go2rtc
  Licensed under the MIT License.

---

<a href="https://www.star-history.com/?repos=r0n9%2Fcamkeep&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
 </picture>
</a>
