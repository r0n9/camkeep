
# <img src="./static/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

[简体中文](./README.md) | [English](./README_en.md)

**A lightweight, self-hosted Network Video Recorder (NVR) designed for Home NAS.**


CamKeep is a streaming and recording gateway written in Go, deeply integrated with **go2rtc** and **FFmpeg**. Designed for home NAS (FnOS, Synology, QNAP, Unraid, etc.) and low-power micro-servers, it allows you to say goodbye to expensive proprietary NVRs and privacy risks.

## 🔥 Key Highlights

* 🐳 **All-in-One Simple Deployment**: Deeply integrated streaming engine. A **single Docker container** is all you need to launch the entire security system, no complex network or environment configuration required.
* 🔒 **100% Local Operation**: **No public cloud! No cloud connection! No mandatory accounts!** Completely eliminate the risk of monitoring footage leakage. As long as your local network is up, your surveillance is secure.
* 📹 **Break Brand Barriers (Records anything with RTSP)**: Whether it's a traditional security camera (Hikvision, Dahua, TP-Link), a flashed smart camera, or even an old smartphone—**as long as it outputs an RTSP stream, CamKeep can store it 24/7**.
* ⚡  **Ultra-Low Resource Usage**: Specifically optimized for the generally weak CPUs of NAS devices using a "Stream Proxy" architecture. Even with multiple viewers, it only pulls **one stream** from the camera. Recording uses `Copy` mode by default (zero re-encoding), making it extremely friendly to low-power devices.
* 🔄 **Seamless go2rtc Integration**: Versions v1.3.0+ support automatic scanning and management of external video streams running on the underlying go2rtc. Synchronize your existing smart home streams with just one click, without redundant configurations.
* 🖥️ **Professional Multi-Grid Matrix**: Say goodbye to single-screen switching. Features a professional-grade **4/6-grid playback matrix** with double-click fullscreen, smart focus switching, and background idle sleep mechanisms for an immersive monitoring experience.
* 🏗️ **Multi-Arch Compatibility**: Native support for x86-64 and ARM64 (Raspberry Pi, Rockchip, etc.), perfectly suited for professional servers, home NAS, and low-power edge devices.

## 🌟 More Features

* **Low Latency & Universal Live Viewing**: Front-end prioritizes native WebRTC for millisecond-level real-time video, avoiding the multi-second delay of traditional HLS. Built-in **intelligent protocol fallback (MSE / mpegts)** ensures smooth viewing even in strict firewall environments or restricted browsers.
* **Smart Recording & Storage Management**:
  * Native support for **MPEG-TS (.ts)** segments. Recording remains intact even after power failures, allowing for instant playback.
  * Built-in **Timelapse** mode: Condense long footage into high-speed videos to save massive NAS disk space.
  * **Automatic Storage Manager**: Configure `retention_days` and the system will automatically recycle expired files in the background.
* **Modern Control Panel**: Clean Web UI with real-time status monitoring. Built-in DPlayer + mpegts.js for waterfall-style playback by date.

---

## 🚀 Quick Deployment

Thanks to full integration, CamKeep only requires mapping ports and directories to start. Choose between `Docker Run` or `Docker-Compose`.

### 1. Prepare Directory & Configuration

> 💡 Versions v1.1.0 and later support updating configuration via the Web UI, but it is recommended to prepare the config directory for the first launch.

Create a base directory on your NAS (e.g., `/vol1/CamKeep`) and create `config/conf.yaml`:

For detailed configuration options, see: [Configuration Usage (conf_usage.md)](https://github.com/r0n9/camkeep/blob/main/conf_usage.md)

```yaml
cameras:
# Normal recording mode example
  - id: "front-door"      # Unique ID (Alphanumeric)
    rtsp_url: "rtsp://admin:123456@192.168.1.100:554/stream"
    retention_days: 7       # Keep recordings for 7 days
    segment_duration: 300   # 5-minute segments
    format: "ts"            # TS is highly recommended for instant playback
    record_time: "00:00-24:00" # Allowed recording schedule
    mode: "normal"
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

---

<a href="https://www.star-history.com/?repos=r0n9%2Fcamkeep&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
 </picture>
</a>
```