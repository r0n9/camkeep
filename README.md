# <img src="./static/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

[![Repo](https://img.shields.io/badge/Docker-Repo-007EC6?labelColor-555555&color-007EC6&logo=docker&logoColor=fff&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Version](https://img.shields.io/docker/v/r0n9/camkeep/latest?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Size](https://img.shields.io/docker/image-size/r0n9/camkeep/latest?sort=semver&labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![Pulls](https://img.shields.io/docker/pulls/r0n9/camkeep?labelColor-555555&color-007EC6&style=flat-square)](https://hub.docker.com/r/r0n9/camkeep)
[![github: AlexxIT/go2rtc](https://img.shields.io/badge/Repo-AlexxIT/go2rtc-slategray?style=flat&logo=github&logoColor=white)](https://github.com/AlexxIT/go2rtc)
[![github: r0n9/camkeep](https://img.shields.io/badge/Repo-r0n9/camkeep-slategray?style=flat&logo=github&logoColor=white)](https://github.com/r0n9/camkeep)

[简体中文](./README.md) | [English](./README_en.md)

---

**专为家庭 NAS 设计的轻量级私有化监控录像机 (NVR)**

CamKeep 是一款基于 Go 语言开发，深度集成 **go2rtc** 和 **FFmpeg** 的流媒体监控与录制网关。它专为家庭 NAS (飞牛、群晖、威联通、Unraid 等) 和低功耗小主机设计，让你彻底告别昂贵的品牌硬盘录像机和隐私泄露风险。

![camkeep](camkeep_console.png)

## ✨ 功能亮点

CamKeep 的目标很明确：在你自己的内网里，用尽可能少的配置和资源，把 RTSP 摄像头稳定录到 NAS 上。

* 🐳 **单容器极简部署**：内置 go2rtc 与 FFmpeg，Docker 启动即可使用；配置文件简单，Web 控制台支持热更新。
* 🔒 **纯内网私有运行**：不依赖云端、不强制账号、不要求公网服务。视频流、录像文件和回放入口都留在你的局域网与 NAS 内。
* ⚡ **低功耗友好**：基于 go2rtc 做流代理，多终端观看也尽量只向摄像头拉取一路流；常规录制默认 `copy`，避免不必要的重编码。
* 📹 **有 RTSP 就能接入**：兼容海康、大华、TP-Link、刷机摄像头、旧手机等各类 RTSP 视频源，也可扫描并接管已有 go2rtc 流。
* 🎥 **轻量录像能力完整**：支持定时录像、手动启停、动检录像、延时摄影、TS/MP4 切片、按天回放和历史片段浏览。
* 🧠 **低成本动检录制**：动检模式采用低分辨率帧差检测，配合 Time-Shift 缓存，只在有画面变化时生成事件录像，减少空镜头占用。
* 🧹 **自动存储管理**：通过 `retention_days` 控制保留天数，后台自动滚动清理过期录像，适合长期放在 NAS 上运行。
* 🖥️ **实用监控面板**：支持 WebRTC 低延迟直播，异常场景可回退到 MSE / mpegts；提供 4/6 宫格预览、双击全屏、设备状态和日期回放。
* 🏗️ **适合 NAS 与边缘设备**：原生支持 x86-64 与 ARM64，适配群晖、威联通、Unraid、飞牛、树莓派、RK3588 等设备。

---

## 🚀 极速部署

得益于底层的全面整合，CamKeep 现在只需映射所需的端口和目录即可一键启动。你可以根据习惯选择 `Docker Run` 或 `Docker-Compose`。

### 1. 准备目录与配置文件

> 💡v1.1.0 之后版本支持通过 Web 控制台更新配置，但初次启动建议准备好配置目录。

在你的 NAS 或服务器上创建一个基础目录（例如 `/vol1/CamKeep`），并在其中新建配置文件 `config/conf.yaml`：

具体配置项说明，请阅览：[配置说明文档 (conf_usage.md)](https://github.com/r0n9/camkeep/blob/main/conf_usage.md)

```yaml
daily_merge:
  enabled: false          # 是否每天合并前一天碎片录像
  time: "03:30"           # 每日合并时间，建议放在低峰时段

cameras:
# 普通录制模式示例
  - id: "front-door"      # 摄像头唯一ID (英文/数字)
    rtsp_url: "rtsp://admin:123456@192.168.1.100:554/stream"
    retention_days: 7       # 录像保留 7 天
    segment_duration: 300   # 每 5 分钟切分一个录像文件
    format: "ts"            # 强烈推荐 ts 格式，支持边写边播
    record_time: "00:00-24:00" # 允许录制的时间段
    mode: "normal"
    motion_detect: false    # 默认关闭动检录制
    motionDetectRatioThreshold: 0.01 # 动检阈值，默认 1%
```

### 2. 启动服务 (二选一)

#### 方式一：Docker Run (单行命令，推荐极简部署)

在终端中执行以下命令（请将 `${PWD}` 替换为你的实际物理路径）：

```bash
docker run -d \
  --name camkeep \
  --restart unless-stopped \
  --network host \
  -e TZ=Asia/Shanghai \
  -v ${PWD}/config:/app/config \
  -v ${PWD}/records:/app/records \
  r0n9/camkeep:latest  # 若网络不佳，可替换为 ghcr.io/r0n9/camkeep:latest
```

#### 方式二：Docker-Compose

如果你习惯使用 Compose 管理，在同级目录下新建 `docker-compose.yaml`：

```yaml
services:
  camkeep:
    image: r0n9/camkeep:latest
    container_name: camkeep
    restart: unless-stopped
    network_mode: "host" # 建议使用 host 网络，否则WebRTC可能握手失败
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./config:/app/config
      - ./records:/app/records
#    ports:
#      - "9110:9110"      # CamKeep Web 控制台
#      - "1984:1984"      # go2rtc API 端口
#      - "8554:8554"      # RTSP 服务端口
#      - "8555:8555/tcp"  # WebRTC 端口 (必须暴露，否则无画面)
#      - "8555:8555/udp"
```

然后执行：
```bash
docker-compose up -d
```

### 3. 开始使用
启动成功后，在浏览器中访问 `http://<你的NAS IP>:9110` 即可进入监控中心。

## 📄 开源协议

本项目基于 **MIT License** 开源。欢迎大家提交 Issue 和 PR 共同完善这款属于个人的 NAS 监控系统。

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
