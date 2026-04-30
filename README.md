# <img src="./static/camkeep_w80.png" width="42" align="center" alt="CamKeep Logo" /> CamKeep

**专为家庭 NAS 设计的轻量级私有化监控录像机 (NVR)**

CamKeep 是一款基于 Go 语言开发，深度集成 **go2rtc** 和 **FFmpeg** 的流媒体监控与录制网关。它专为家庭 NAS (飞牛、群晖、威联通、Unraid 等) 和低功耗小主机设计，让你彻底告别昂贵的品牌硬盘录像机和隐私泄露风险。

## 🔥 核心亮点

* 🐳 **All-in-One 极简部署**：深度集成底层流媒体引擎，**单一 Docker 容器**即可拉起整个安防系统，无需复杂的网络和环境配置。
* 🔒 **100% 纯内网运行**：**不出公网！不连云端！不强制注册账号！** 彻底杜绝监控画面泄露风险，只要你的局域网通，监控就在。
* 📹 **打破品牌壁垒 (有 RTSP 就能录)**：无论你是海康、大华、TP-Link 这种传统安防厂，还是刷了机的智能摄像头，甚至是闲置的旧手机，**只要能输出 RTSP 视频流，CamKeep 就能帮你全天候存储**。
* ⚡ **极低资源占用**：针对 NAS 普遍 CPU 较弱的痛点，采用“流代理（Stream Proxy）”架构。多终端同时观看也只向摄像头拉取 **1路流**，并且录制过程默认采用 `Copy` 模式零重编码，对低功耗设备极度友好。

## 🌟 更多特性

* **超低延迟与全网兼容直播**：前端优先采用原生 WebRTC 协议实现毫秒级实时画面，告别传统 HLS 的数秒延迟。针对复杂的家庭网络环境，**内置智能协议降级机制 (MSE / mpegts)**，即使在 UDP 穿透受限、严格防火墙拦截或特定浏览器下，系统也会自动无缝回退至 HTTP 兼容流，确保监控画面在任何设备上都能“秒开”且稳定流畅。
* **智能录像与存储管理**：
  * 原生支持 **MPEG-TS (.ts)** 录像切片，哪怕断电重启录像也不会损坏，随录随播。
  * 内置 **延时摄影 (Timelapse)** 模式，将漫长的监控画面浓缩为高倍速视频，极大地节省 NAS 硬盘空间。
  * **全自动空间管家**：配置 `retention_days`（保留天数），系统会自动在后台滚动清理过期文件，NAS 硬盘永远不会被塞满。
* **现代化控制面板**：自带清爽的 Web UI，支持设备状态实时监测，内置 DPlayer + mpegts.js，按日期瀑布流回放历史录像。

---

## 🚀 极速部署

得益于底层的全面整合，CamKeep 现在只需映射所需的端口和目录即可一键启动。你可以根据习惯选择 `Docker Run` 或 `Docker-Compose`。

### 1. 准备目录与配置文件

> 💡v1.1.0 之后版本支持通过 Web 控制台更新配置，但初次启动建议准备好配置目录。

在你的 NAS 或服务器上创建一个基础目录（例如 `/vol1/CamKeep`），并在其中新建配置文件 `config/conf.yaml`：

具体配置项说明，请阅览：[配置说明文档 (conf_usage.md)](https://github.com/r0n9/camkeep/blob/main/conf_usage.md)

```yaml
cameras:
# 普通录制模式示例
  - id: "front-door"      # 摄像头唯一ID (英文/数字)
    rtsp_url: "rtsp://admin:123456@192.168.1.100:554/stream"
    retention_days: 7       # 录像保留 7 天
    segment_duration: 300   # 每 5 分钟切分一个录像文件
    format: "ts"            # 强烈推荐 ts 格式，支持边写边播
    record_time: "00:00-24:00" # 允许录制的时间段
    mode: "normal"
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

---

<a href="https://www.star-history.com/?repos=r0n9%2Fcamkeep&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=r0n9/camkeep&type=date&legend=top-left" />
 </picture>
</a>

