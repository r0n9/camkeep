# <img src="./static/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

[![Repo](https://img.shields.io/badge/Docker-Repo-007EC6?labelColor-555555&color-007EC6&logo=docker&logoColor=fff&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Version](https://img.shields.io/docker/v/r0n9/camkeep/latest?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Size](https://img.shields.io/docker/image-size/r0n9/camkeep/latest?sort=semver&labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Pulls](https://img.shields.io/docker/pulls/r0n9/camkeep?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![github: AlexxIT/go2rtc](https://img.shields.io/badge/Repo-AlexxIT/go2rtc-slategray?style=flat&logo=github&logoColor=white)](https://github.com/AlexxIT/go2rtc)
[![github: r0n9/camkeep](https://img.shields.io/badge/Repo-r0n9/camkeep-slategray?style=flat&logo=github&logoColor=white)](https://github.com/r0n9/camkeep)

[简体中文](./README.md) | [English](./README_en.md)

---

**全面兼容 go2rtc 的自托管 NVR，面向家庭 NAS 与边缘设备。**

CamKeep 基于 Go、go2rtc 和 FFmpeg，提供本地优先的视频接入、录制、回放和设备管理能力。它已经不再只是 RTSP 极简录像机，而是一个可以接入 go2rtc 现有流、ONVIF 设备以及其他 go2rtc source 的统一 NVR 网关。

![camkeep](camkeep_console.png)

## 设计初衷与原则

CamKeep 的初衷不是替代大型企业级安防平台，而是为家庭 NAS、低功耗小主机和内网自托管场景提供一套够用、稳定、可控的 NVR。

* **极简**：单容器运行，配置尽量少，常用能力优先在 Web 控制台完成，不把用户拖进复杂的视频工程配置里。
* **低功耗**：优先复用 go2rtc 流代理，录像默认流拷贝，封面与状态刷新控制频率和并发，尽量适合长期运行在 NAS、软路由、ARM 小主机上。
* **内网安全**：默认本地优先，不依赖云端，不上传视频和设备信息；建议部署在可信局域网内，必要时通过内置登录鉴权或反向代理访问。

## ✨ 功能亮点

* 🧩 **go2rtc-native 接入**：`stream_url` 支持任意 go2rtc 兼容接入源，例如 `rtsp://`、`onvif://`、`ffmpeg:`、`exec:`，也支持导入已有 go2rtc 流。
* 🕹️ **ONVIF PTZ 控制**：自动识别 ONVIF 控制候选设备，支持云台移动、变焦、停止，以及设备支持时的对焦和光圈控制。
* 🖼️ **实时封面**：每个实时节点展示持久化封面，默认保存到 `records/<camera>/cover.jpg`；优先使用 go2rtc 截图，失败后回退本地 FFmpeg。
* 📺 **紧凑实时看板**：实时节点卡片适配桌面和移动端，封面只在可视区域按需加载，后台每 10 分钟轻量巡检，减少性能消耗和画面闪烁。
* 🕓 **24H 时间轴回放**：保留原卡片列表和旧时间轴，同时新增可吸附到播放器下方的 24H 时间轴，支持拖动、滚轮缩放、移动端双指缩放和按时间点播放。
* 🧰 **Web 配置管理**：单页面配置管理，支持表单/YAML 双模式、摄像头卡片折叠、恢复未保存修改、单个添加、批量添加和从 go2rtc 导入未接管流。
* 🎥 **完整录像能力**：支持定时录像、手动强制开始/停止、动检录像、延时摄影、TS/MP4 切片、历史回放、下载和删除。
* 🧠 **低成本动检**：普通模式下可启用 `motion_detect`，使用低分辨率帧差检测和 Time-Shift 缓存，只在画面变化时生成事件录像。
* 🧹 **自动存储管理**：支持 `retention_days` 过期清理、过小碎片过滤、每日按小时合并录像，适合长期运行在 NAS 上。
* 🔒 **本地优先**：不依赖云端、不强制账号、不上传摄像头数据；登录鉴权可通过环境变量开启。

## 状态与模式

`/api/status` 中的 `mode` 表示运行态录制模式，取值为 `normal`、`motion`、`timelapse`。`record_state` 只表示当前录制状态，取值为 `idle`、`recording`、`motion_detecting`、`motion_recording`，不要用它判断录制模式。

## 接入源说明

`stream_url` 是 CamKeep 的接入源字段，语义是“go2rtc source”，不是单纯的 RTSP 地址。旧字段 `rtsp_url` 仍兼容，但推荐迁移到 `stream_url`。

常见写法：

```yaml
stream_url: "rtsp://admin:password@192.168.1.10:554/Streaming/Channels/101"
stream_url: "onvif://admin:password@192.168.1.11"
stream_url: "ffmpeg:rtsp://admin:password@192.168.1.12/live#video=copy#audio=aac"
stream_url: "managed_by_go2rtc"
```

`managed_by_go2rtc` 通常由 Web 配置页导入已有 go2rtc 流时自动写入，表示该流由 go2rtc 管理，CamKeep 只负责录制、回放和状态展示。

## 🚀 快速部署

CamKeep Docker 镜像内置 go2rtc 和 FFmpeg。推荐使用 host 网络，尤其是需要 WebRTC 低延迟直播时。

### 1. 准备目录与配置文件

在 NAS 或服务器上创建基础目录，例如 `/vol1/CamKeep`，并在其中准备 `config/conf.yaml`。详细字段见 [配置说明文档](./conf_usage.md)。

```yaml
daily_merge:
  enabled: false
  time: "03:30"

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
    motionDetectRatioThreshold: 0.01
```

说明：`records` 目录会保存录像文件，也会保存每个摄像头的最新封面截图。

### 2. 启动服务

`CAMKEEP_AUTH_PASSWORD` 用来开启登录鉴权；如果不设置它，Web 控制台不会启用登录鉴权，也不需要配置 `CAMKEEP_AUTH_USER` 和 `CAMKEEP_SESSION_SECRET`。

#### Docker Run

```bash
docker run -d \
  --name camkeep \
  --restart unless-stopped \
  --network host \
  -e TZ=Asia/Shanghai \
  -e CAMKEEP_AUTH_USER=admin \
  -e CAMKEEP_AUTH_PASSWORD=admin \
  -e CAMKEEP_SESSION_SECRET=B1JM12wvPLHL9bturc2DfiFFvHjtntl2+OG+V/2yXjg= \
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
    network_mode: "host" # 推荐 host 网络，否则 WebRTC 可能握手失败
    environment:
      - TZ=Asia/Shanghai
      - CAMKEEP_AUTH_USER=admin
      - CAMKEEP_AUTH_PASSWORD=admin
      - CAMKEEP_SESSION_SECRET=B1JM12wvPLHL9bturc2DfiFFvHjtntl2+OG+V/2yXjg=
    volumes:
      - ./config:/app/config
      - ./records:/app/records
#    ports:
#      - "9110:9110"      # CamKeep Web 控制台
#      - "1984:1984"      # go2rtc API / 控制台
#      - "8554:8554"      # go2rtc RTSP 服务
#      - "8555:8555/tcp"  # WebRTC
#      - "8555:8555/udp"
```

然后执行：

```bash
docker-compose up -d
```

### 3. 进入控制台

浏览器访问 `http://<你的NAS IP>:9110`。如果启用了鉴权，使用环境变量中的用户名和密码登录。

## Web 控制台

* **实时节点**：展示封面、在线状态、录制状态、手动录制控制和实时预览入口。
* **历史录像**：按摄像头和日期查看录像，支持卡片列表、传统时间轴和 24H 时间轴回放。
* **配置管理**：支持表单编辑和 YAML 编辑，批量添加摄像头，从 go2rtc 扫描并导入未接管流。
* **ONVIF 控制**：对支持的设备显示 PTZ、变焦、对焦和光圈控制。
* **版本更新**：启动后异步检查 GitHub Releases，之后按周期缓存刷新；稳定版发现更新时会在版本号旁展示入口。`dev`、`test` 或自定义版本不会被标记为稳定版升级。

## 隐私说明

CamKeep 默认不包含遥测，不上传视频、设备列表或使用行为。版本检查只请求 GitHub Releases 元数据，用于判断是否有新版本。

## 📄 开源协议

本项目基于 **MIT License** 开源。欢迎提交 Issue 和 PR。

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
