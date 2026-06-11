# <img src="./static/image/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

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
* **内网安全**：默认本地优先，不依赖云端，不上传视频和设备信息；建议部署在可信局域网内，必要时通过本地用户鉴权或反向代理访问。

## ✨ 功能亮点

* 🧩 **go2rtc-native 接入**：支持 RTSP、ONVIF、FFmpeg、脚本等 go2rtc 兼容接入源，也可以直接导入已有 go2rtc 流。
* 🕹️ **ONVIF 控制与事件诊断**：自动识别 ONVIF 控制候选设备，支持 PTZ、变焦、对焦、光圈控制，并可诊断 Event 服务、PullPoint 支持和最近收到的事件。
* 🖼️ **实时封面**：每个实时节点展示持久化封面；优先使用 go2rtc 截图，失败后回退本地 FFmpeg。
* 📺 **紧凑实时看板**：实时节点卡片适配桌面和移动端，封面只在可视区域按需加载，后台轻量巡检，减少性能消耗和画面闪烁。
* 🕓 **24H 时间轴回放**：保留原卡片列表和旧时间轴，同时新增可吸附到播放器下方的 24H 时间轴，支持拖动、滚轮缩放、移动端双指缩放和按时间点播放。
* 🧰 **Web 配置管理**：单页面配置管理，支持表单/YAML 双模式、摄像头卡片折叠、恢复未保存修改、单个添加、批量添加和从 go2rtc 导入未接管流。
* 🎥 **完整录像能力**：支持定时录像、手动强制开始/停止、动检录像、延时摄影、TS/MP4 切片、历史回放、下载和删除。
* 🧠 **事件源可选的动检**：支持本地低分辨率帧差、ONVIF PullPoint，或自动组合两者，用 Time-Shift 缓存生成事件录像。
* 🧭 **普通录像动检标记**：连续录像时也能叠加活动区间，不影响录像启停，方便在 24H 时间轴上快速定位有人、车或画面变化的片段。
* 🧹 **自动存储管理**：支持过期清理、过小碎片过滤、每日按小时/连续区间合并录像；残留的普通录像片段会自动修复封装，动检片段也可按需合并。
* 🔒 **本地用户与权限**：不依赖云端、不强制账号、不上传摄像头数据；支持本地管理员/只读用户、在线会话状态和按摄像头限制普通用户可见范围。

## 接入源说明

CamKeep 直接沿用 go2rtc 的接入能力，不局限于 RTSP 地址。你可以填写 RTSP、ONVIF、FFmpeg 等接入方式，也可以在 Web 配置页扫描并导入已有 go2rtc 流。导入后，go2rtc 继续管理流定义，CamKeep 负责录制、回放、状态展示和设备控制。

## ONVIF 事件与动检标记

ONVIF 接入设备在能力探测成功后，CamKeep 会识别 Event 服务和 PullPoint 支持情况。配置页可以展开 ONVIF 事件诊断，查看监听状态、最近事件，并启动 30 秒 PullPoint 测试监听。

动检录像可以选择三种事件来源：

* 本地帧差：低分辨率检测画面变化，适合没有 ONVIF 事件的摄像头。
* ONVIF PullPoint：直接使用摄像头上报的事件，CPU 占用更低。
* 自动模式：ONVIF 健康时优先使用摄像头事件，并在事件后短时用帧差跟踪来延长或结束事件窗口；ONVIF 不可用时回退本地帧差。

普通连续录像还可以单独开启动检标记。它不会控制录像开始或停止，只会把活动区间叠加到 24H 时间轴上，方便回看时快速定位变化片段。

在 1 画面实时直播窗口中，ONVIF 设备会显示事件叠层开关。开启后只租用 PullPoint 监听并展示最近事件，鼠标悬停按钮时显示事件列表，不改变录像策略。

## 🚀 快速部署

CamKeep Docker 镜像内置 go2rtc 和 FFmpeg。推荐使用 host 网络，尤其是需要 WebRTC 低延迟直播时。

### 1. 准备目录与可选配置文件

在 NAS 或服务器上创建基础目录，例如 `/vol1/CamKeep`。首次部署时只要准备 `config/` 和 `records/` 目录即可，配置文件不存在时会在首次启动时自动生成默认模板。推荐先启动服务，再在 Web 配置页添加摄像头；完整 YAML 字段见 [配置说明文档](./conf_usage.md)。

Web 配置页支持表单编辑、批量添加、从 go2rtc 导入、ONVIF 事件诊断和未保存修改恢复：

![CamKeep 摄像头配置页](camkeep_cam_config.png)

如果你想手写一个最小初始配置，只需要先写清楚摄像头 ID、接入源和录制时间即可，其他常用参数可以之后在 Web 页面里调整。

```yaml
cameras:
  - id: "front-door"
    stream_url: "rtsp://admin:password@192.168.1.100:554/stream"
    record_time: "00:00-23:59"
```

说明：`records` 目录会保存录像文件，也会保存每个摄像头的最新封面截图。

### 2. 启动服务

登录鉴权由本地用户文件管理。首次启动时可以用下面示例里的管理员密码初始化内置 `admin` 账号；之后账号、密码和权限都在 Web“用户管理”里维护，修改启动环境变量不会覆盖已有密码。

如果没有设置初始管理员密码，且当前没有任何用户，Web 控制台会先以未启用鉴权的状态启动。此时可以在“用户管理”里创建第一个 `admin` 用户，创建后会自动启用登录保护。固定会话密钥和 HTTPS Cookie 可以在高级部署时再按需配置。

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
    network_mode: "host" # 推荐 host 网络，否则 WebRTC 可能握手失败
    shm_size: "512m"
    environment:
      - TZ=Asia/Shanghai
      - CAMKEEP_AUTH_PASSWORD=admin
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

建议保留 `--shm-size=512m` 或 `shm_size: "512m"`。CamKeep 的动检录像 Time-Shift 缓存会优先写入容器 `/dev/shm`，Docker 默认通常只有 64MB，高码率或多路动检时可能导致 FFmpeg 写入失败，表现为缓存引擎退出或动检片段过短。512MB 适合一般 1-2 路摄像头；多路或高码率主码流建议调整到 `1g` 或更高。不使用动检录像时可以按需降低或省略。

### 3. 进入控制台

浏览器访问 `http://<你的NAS IP>:9110`。如果通过示例环境变量初始化了管理员账号，使用用户名 `admin` 和设置的密码登录；后续账号、密码和权限在 Web“用户管理”中维护。

## Web 控制台

* **实时节点**：展示封面、在线状态、录制状态、手动录制控制和实时预览入口；ONVIF 直播窗口可开启事件叠层。
* **历史录像**：按摄像头和日期查看录像，支持卡片列表、传统时间轴和 24H 时间轴回放，24H 时间轴可叠加动检标记。
* **配置管理**：支持表单编辑和 YAML 编辑，批量添加摄像头，从 go2rtc 扫描并导入未接管流，ONVIF 设备可查看 Event/PullPoint 诊断。
* **用户管理**：支持本地用户、管理员/只读角色、账号启停、密码重置、在线会话展示，以及普通用户可访问摄像头范围设置。
* **ONVIF 控制**：对支持的设备显示 PTZ、变焦、对焦、光圈控制和事件测试入口。
* **版本更新**：启动后异步检查 GitHub Releases，之后按周期缓存刷新；稳定版发现更新时会在版本号旁展示入口。`dev`、`test` 或自定义版本不会被标记为稳定版升级。

## 隐私说明

CamKeep 默认不包含遥测，不上传视频、设备列表或使用行为。版本检查只请求 GitHub Releases 元数据，用于判断是否有新版本。

## 📄 开源协议

本项目基于 **MIT License** 开源。欢迎提交 Issue 和 PR。

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
