# 🛡️ CamKeep

**专为家庭 NAS 设计的轻量级私有化监控录像机 (NVR)**

CamKeep 是一款基于 Go 语言开发，深度集成 **go2rtc** 和 **FFmpeg** 的流媒体监控与录制网关。它专为家庭 NAS (飞牛、群晖、威联通、Unraid 等) 和低功耗小主机设计，让你彻底告别昂贵的品牌硬盘录像机和隐私泄露风险。

## 🔥 核心亮点

* 🐳 **NAS 极简部署**：无需繁琐的环境配置，提供标准 `docker-compose` 方案，一键拉起整个安防系统。
* 🔒 **100% 纯内网运行**：**不出公网！不连云端！不强制注册账号！** 彻底杜绝监控画面泄露风险，只要你的局域网通，监控就在。
* 📹 **打破品牌壁垒 (有 RTSP 就能录)**：无论你是海康、大华、TP-Link 这种传统安防厂，还是刷了机的智能摄像头，甚至是闲置的旧手机，**只要能输出 RTSP 视频流，CamKeep 就能帮你全天候存储**。
* ⚡ **极低资源占用**：针对 NAS 普遍 CPU 较弱的痛点，采用“流代理（Stream Proxy）”架构。多终端同时观看也只向摄像头拉取 **1路流**，并且录制过程默认采用 `Copy` 模式零重编码，对低功耗设备极度友好。

## 🌟 更多特性

* **低延迟直播**：Web 前端采用原生 WebRTC 协议，告别传统 HLS/FLV 的数秒延迟，实现毫秒级实时画面。
* **智能录像与存储管理**：
    * 原生支持 **MPEG-TS (.ts)** 录像切片，哪怕断电重启录像也不会损坏，随录随播。
    * 内置 **延时摄影 (Timelapse)** 模式，将漫长的监控画面浓缩为高倍速视频，极大地节省 NAS 硬盘空间。
    * **全自动空间管家**：配置 `retention_days`（保留天数），系统会自动在后台滚动清理过期文件，NAS 硬盘永远不会被塞满。
* **现代化控制面板**：自带清爽的 Web UI，支持设备状态实时监测，内置 DPlayer + mpegts.js，按日期瀑布流回放历史录像。

---

## 🚀 极速部署 (Docker-Compose)

### 1. 准备目录与配置文件
在你的 NAS 或服务器上创建一个目录，并新建配置文件 `conf.yaml`：

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

# 延时录像模式示例 (省空间神器)
  - id: "balcony"
    rtsp_url: "rtsp://admin:123456@192.168.1.101:554/stream"
    retention_days: 30      # 延时录像很小，可以保留很久
    segment_duration: 3600  # 1小时一个切片
    format: "ts"           
    record_time: "08:00-18:00"
    mode: "timelapse"
    capture_interval: 2     # 每 2 秒抓取一帧
```

### 2. 创建 docker-compose.yaml
在同级目录下新建 `docker-compose.yaml`：

https://raw.githubusercontent.com/r0n9/camkeep/refs/heads/main/docker-compose.yaml

### 3. 一键启动
在终端中执行：
```bash
docker-compose up -d
```
启动成功后，在浏览器中访问 `http://<你的NAS IP>:9110` 即可进入监控中心。

---

## 🛠️ 架构简析

CamKeep 充当了摄像头的“中枢大脑”：
1. **统一网关拉流**：由底层性能极佳的 `go2rtc` 充当网关，统一向真实摄像头拉取 RTSP 流，保护 IPC（网络摄像机）的连接数上限。
2. **WebRTC 分发**：当前端用户在网页上点击预览时，通过网关将 RTSP 秒转 WebRTC 进行极低延迟渲染。
3. **录像守护进程**：后端的 Go 程序根据配置的时间表和规则，调度内置的 FFmpeg 从同网段的 go2rtc 网关无损抓取码流，写入 NAS 磁盘。

## 📄 开源协议

本项目基于 **MIT License** 开源。欢迎大家提交 Issue 和 PR 共同完善这款属于个人的 NAS 监控系统。