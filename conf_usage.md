# CamKeep 配置说明

CamKeep 的配置文件默认位于 `config/conf.yaml`。建议优先通过 Web 配置管理页面编辑；如果需要手写 YAML，请参考本说明。

`stream_url` 现在表示 **go2rtc 接入源**，不是单纯的 RTSP 地址。CamKeep 会把每路摄像头注册到内置 go2rtc，再从本机 go2rtc 流录制、预览、截图和执行 ONVIF 识别。

## 基础结构

```yaml
daily_merge:
  enabled: false
  time: "03:30"
  merge_motion_records: false

cameras:
  - id: "front-door"
    order: 0
    stream_url: "rtsp://admin:password@192.168.1.100:554/Streaming/Channels/101"
    motion_url: ""
    retention_days: 7
    segment_duration: 300
    format: "ts"
    min_size_kb: 1024
    record_time: "00:00-23:59"
    mode: "normal"
    motion_detect: false
    motion_event_source: "frame_diff"
    motion_mark_enabled: false
    motion_mark_event_source: "auto"
    motionDetectRatioThreshold: 0.01
```

## 接入源示例

### RTSP 摄像头

```yaml
- id: "front-door"
  stream_url: "rtsp://admin:password@192.168.1.10:554/Streaming/Channels/101"
  mode: "normal"
```

### ONVIF 摄像头

```yaml
- id: "garage"
  stream_url: "onvif://admin:password@192.168.1.11"
  mode: "normal"
```

使用 `onvif://` 时，CamKeep 会把该设备识别为 ONVIF 控制候选设备。设备能力探测成功后，Web 页面会展示 PTZ、变焦、对焦、光圈等可用控制，也会显示 Event 服务、PullPoint 支持和最近事件等诊断信息。

### go2rtc 托管流

```yaml
- id: "yard"
  stream_url: "managed_by_go2rtc"
  auto_discovered: true
  mode: "normal"
```

这种配置通常由 Web 配置页“从 go2rtc 导入”自动生成。它表示这路流已经在 go2rtc 里存在，CamKeep 不覆盖它的 source，只负责录制、回放、状态和控制集成。

### 动检录像

```yaml
- id: "driveway"
  stream_url: "rtsp://admin:password@192.168.1.12:554/Streaming/Channels/101"
  motion_url: "rtsp://admin:password@192.168.1.12:554/Streaming/Channels/102"
  mode: "normal"
  motion_detect: true
  motion_event_source: "frame_diff"
  motionDetectRatioThreshold: 0.01
  record_time: "00:00-23:59"
```

`motion_event_source` 控制事件来源。事件录像仍使用该摄像头注册到 go2rtc 的默认录制源。

ONVIF 摄像头可使用自动事件源，Event/PullPoint 健康时优先使用设备事件，异常时回退本地帧差：

```yaml
- id: "garage"
  stream_url: "onvif://admin:password@192.168.1.11"
  mode: "normal"
  motion_detect: true
  motion_event_source: "auto"
  record_time: "00:00-23:59"
```

也可以使用 `motion_event_source: "onvif"` 做 ONVIF-only 动检录像。此模式只接受 PullPoint motion 事件，Event 不可用时不会回退帧差。

### 普通录像动检标记

普通连续录像可以单独生成动检时间轴标记，不影响录像启停：

```yaml
- id: "garage"
  stream_url: "onvif://admin:password@192.168.1.11"
  mode: "normal"
  motion_detect: false
  motion_mark_enabled: true
  motion_mark_event_source: "auto"
  motion_url: "rtsp://admin:password@192.168.1.11/substream"
  motionDetectRatioThreshold: 0.01
  record_time: "00:00-23:59"
```

说明：

* `motion_detect: false` 时才会使用普通录像动检标记；如果开启动检录像，系统会进入事件录像模式，不再生成普通录像标记。
* `motion_mark_event_source` 支持 `frame_diff`、`onvif`、`auto`，含义与动检录像事件源一致。
* 当标记来源为 `frame_diff`，或 `auto` 回退到帧差时，会读取 `motion_url` 和 `motionDetectRatioThreshold`。
* 标记文件写入 `records/<camera>/.markers/YYYY-MM-DD.jsonl`，按 `retention_days` 一起清理。
* 24H 时间轴会把这些标记显示为 ONVIF 动检或帧差动检活动区间。

### 延时摄影

```yaml
- id: "construction"
  stream_url: "rtsp://admin:password@192.168.1.13:554/live"
  mode: "timelapse"
  capture_interval: 5
  segment_duration: 3600
  format: "mp4"
  record_time: "07:00-19:00"
```

## 全局配置

### `daily_merge`

每日按自然小时合并前一天的碎片录像，输出 MP4。默认关闭。

* `enabled`：是否启用每日合并。
* `time`：每日执行时间，格式为 `HH:mm`，建议放在 `03:00-05:00` 等低峰时段。
* `merge_motion_records`：是否同时合并动检录像片段。默认 `false`，动检录像片段会保留原文件；设为 `true` 后才会按现有逻辑按小时合并动检录像。

说明：

* `mode: "timelapse"` 的摄像头会跳过每日合并。
* 普通录像会先按自然小时分组，再根据片段文件名中的起止时间拆分连续区间；中间断开超过约 2 秒会分开输出，避免把时间表断开的片段拼成一个文件。
* 合并文件名会尽量使用实际连续区间，例如 `cam1_20260512_090000_091000_merged.mp4`。
* `merge_motion_records` 只影响每日合并任务，不影响动检录像的触发、录制和保留天数。
* 动检录像片段只在 `merge_motion_records: true` 时参与合并，并会与普通录像分组处理。
* 合并成功后会删除对应原碎片。
* 视频使用 FFmpeg 流拷贝，音频会处理为浏览器更兼容的封装。

## 摄像头字段

### `id`

摄像头唯一标识。该值会作为 go2rtc 流名称、Web 展示名称和录像目录名。

建议使用英文、数字、短横线或下划线，例如 `front-door`、`cam_01`。不要包含 `/`、`\`、`:` 等会破坏文件路径的字符。

### `order`

摄像头排序字段，纯数字，默认 `0`。Web 实时节点、配置表单和 `/api/status` 返回顺序会优先按 `order` 升序排列；`order` 相同时保持配置文件顺序。

### `stream_url`

摄像头接入源，支持任意 go2rtc 兼容 source。

常见值：

* `rtsp://...`
* `onvif://...`
* `ffmpeg:...`
* `exec:...`
* `managed_by_go2rtc`

兼容说明：

* 旧字段 `rtsp_url` 仍可使用。
* 如果 `stream_url` 和 `rtsp_url` 同时存在，优先使用 `stream_url`。
* 新配置建议统一使用 `stream_url`。

### `rtsp_url`

旧版兼容字段。仅为兼容旧配置保留，不建议新配置继续使用。

### `motion_url`

本地帧差识别专用接入源，可选。只要当前功能需要本地帧差，就会使用它：

* `motion_detect: true` 且 `motion_event_source` 为 `frame_diff`。
* `motion_detect: true` 且 `motion_event_source` 为 `auto`，并且 ONVIF Event 通道不可用时回退帧差。
* `motion_mark_enabled: true` 且 `motion_mark_event_source` 为 `frame_diff`。
* `motion_mark_enabled: true` 且 `motion_mark_event_source` 为 `auto`，并且 ONVIF Event 通道不可用时回退帧差。

如果留空，帧差检测会使用该摄像头的默认 go2rtc 流。建议填写低分辨率子码流以降低 CPU 和带宽消耗。`motion_event_source: "onvif"` 或 `motion_mark_event_source: "onvif"` 不使用该字段。

### `retention_days`

录像保留天数。后台清理任务每小时扫描一次。

* `> 0`：保留指定天数，超过后自动删除。
* `0`：配置读取时会被默认值逻辑补为 `7`。
* `-1`：不按保留天数自动清理。

### `segment_duration`

切片时长，单位秒。默认值逻辑为 `600`。

建议：

* 普通录像：`300`、`600` 或 `1800`。
* 延时摄影：`3600` 或更长。

### `format`

录像文件格式，支持 `ts` 或 `mp4`，默认 `ts`。

建议：

* 普通录像优先使用 `ts`，异常中断时更不容易损坏，适合边写边播。
* 手机浏览器兼容优先时可使用 `mp4`。
* 延时摄影更适合使用 `mp4`，便于下载和分享。

### `min_size_kb`

过小碎片清理阈值，单位 KB，默认值逻辑为 `1024`。清理任务会跳过全局最新文件，避免误删正在写入的切片。

### `record_time`

自动录制时间段。支持单个或多个区间：

```yaml
record_time: "00:00-23:59"
record_time: "08:00-12:00,14:00-18:00"
record_time: "22:00-06:00"
```

区间支持跨天，例如 `22:00-06:00`。如果开始时间和结束时间相同，例如 `08:00-08:00` 或 `00:00-00:00`，该区间会被视为合法但零长度，运行时不会触发录制。

手动录制控制会覆盖该时间段：

* `start`：强制录制。
* `stop`：强制停止。
* `auto`：恢复按 `record_time` 自动判断。

手动控制由 Web UI 写入覆盖状态，不需要在 `conf.yaml` 中配置。

### `mode`

配置态录制模式，只支持：

* `normal`：普通录像模式，默认值。
* `timelapse`：延时摄影模式。

注意：运行态 `/api/status.mode` 还可能返回 `motion`，表示这路摄像头正在按动检录像逻辑运行。配置文件里不要写 `mode: "motion"`；动检通过 `mode: "normal"` 加 `motion_detect: true` 开启。

### `capture_interval`

延时摄影抓帧间隔，单位秒，只在 `mode: "timelapse"` 时生效。小于等于 0 时运行时按 `1` 秒处理。

示例：

* `5`：每 5 秒取 1 帧。
* `60`：每 60 秒取 1 帧。

### `motion_detect`

是否启用动检录像，只在 `mode: "normal"` 时生效。开启后，这路摄像头不再按时间段持续落盘，而是在录制时间段内检测到画面变化时生成事件录像。

### `motion_event_source`

动检录像事件源，只在 `mode: "normal"` 且 `motion_detect: true` 时生效。

支持：

* `frame_diff`：使用本地低分辨率帧差检测。旧配置未写该字段时按此行为处理。
* `onvif`：只使用 ONVIF PullPoint motion 事件。ONVIF Event 不可用时不会回退本地帧差。
* `auto`：优先使用 ONVIF PullPoint；当 ONVIF Event 不可用、PullPoint 不支持、订阅/PullMessages 失败或最近 Pull 成功超时时，自动回退本地帧差。

`auto` 判断的是 ONVIF Event 通道健康，不要求已经收到过 motion 触发。安静场景长时间没有 motion 事件不会导致回退；但 PullPoint 监听异常会回退。

### `motion_mark_enabled`

普通连续录像下是否生成动检时间轴标记，只在 `mode: "normal"` 且 `motion_detect: false` 时生效。

开启后，摄像头仍按 `record_time` 连续录像；动检事件只用于在历史回放的 24H 时间轴上叠加活动区间。适合既要完整保存录像，又希望快速定位有人/车/画面变化的时间点。

### `motion_mark_event_source`

普通录制动检标记事件源，只在 `motion_mark_enabled: true` 且 `motion_detect: false` 时生效。

支持：

* `frame_diff`：使用本地低分辨率帧差检测生成时间轴标记。
* `onvif`：只使用 ONVIF PullPoint motion 事件生成时间轴标记。
* `auto`：默认值。ONVIF Event 通道健康时使用 PullPoint；不可用时回退本地帧差。

当来源为 `frame_diff`，或 `auto` 回退帧差时，帧差检测会读取 `motion_url` 和 `motionDetectRatioThreshold`。当来源为 `onvif` 时，这两个字段在配置页会隐藏但保留原值。

### `motionDetectRatioThreshold`

帧差检测的变化像素比例阈值，范围 `0` 到 `1`。默认示例为 `0.01`，即约 1% 的低分辨率检测像素变化时判定为运动。

数值越小越敏感，误触发可能越多；数值越大越不敏感，可能漏掉小范围移动。该字段同时用于动检录像帧差检测和普通录像动检标记的帧差检测；ONVIF-only 来源不使用它。

### `auto_discovered`

标记该摄像头是否来自 go2rtc 自动发现或导入。通常由 Web 配置页维护，不建议手写。

当 `stream_url: "managed_by_go2rtc"` 时，CamKeep 会把该摄像头视为 go2rtc 托管流。

## 运行态状态字段

`/api/status` 会返回每个摄像头的运行态信息，常用字段包括：

* `mode`：运行态录制模式，`normal`、`motion`、`timelapse`。
* `record_state`：当前录像状态，`idle`、`recording`、`motion_detecting`、`motion_recording`。
* `stream_state`：实时流状态，`online`、`offline`、`idle`。
* `record_override`：手动录制覆盖状态，`auto`、`start`、`stop`。
* `cover_ready` / `cover_version`：实时封面是否可用及版本。
* `onvif_enabled` / `ptz_state` / `imaging_state`：ONVIF 能力状态。

判断录制模式请使用 `mode`，不要根据 `record_state` 反推。

## ONVIF 事件与 PullPoint

ONVIF 摄像头完成能力探测后，状态中会记录：

* `event_state`：Event 服务能力状态，`available` 表示可用。
* `pull_point_support`：设备是否支持 PullPoint。
* `event_listener_state`：事件监听状态，`inactive`、`starting`、`listening` 或 `error`。
* `last_event` / `recent_events`：最近收到的 ONVIF 事件快照。

Web 配置页的 ONVIF 事件诊断可以读取这些状态，并提供 30 秒 PullPoint 测试监听。测试时触发摄像头事件，如果设备正常推送，最近事件会更新。

ONVIF 事件会被以下功能按需租用监听：

* `motion_detect: true` 且 `motion_event_source` 为 `onvif` 或 `auto`。
* `motion_mark_enabled: true` 且 `motion_mark_event_source` 为 `onvif` 或 `auto`。
* 直播窗口开启 ONVIF 事件叠层。
* 配置页启动 PullPoint 测试监听。

监听只发布 motion 事件给录像/标记逻辑；实时事件叠层和诊断会展示最近事件，但不会改变录像策略。

## 动检时间轴标记

普通录像动检标记会保存为 JSONL 文件：

```text
records/<camera>/.markers/YYYY-MM-DD.jsonl
```

每条记录包含 `start`、`end`、`source`、`topic`、`score`、`reason` 等字段。`source` 常见取值：

* `onvif`：ONVIF-only 标记。
* `frame_diff`：本地帧差标记。
* `auto_onvif`：自动来源当前使用 ONVIF。
* `auto_frame_diff`：自动来源当前回退帧差。

标记文件按本地自然日拆分，跨天标记会拆成多条。清理任务会按摄像头的 `retention_days` 删除过期标记文件。

## 实时封面

CamKeep 会为每路摄像头维护一张最新成功获取的封面图：

```text
records/<camera>/cover.jpg
```

刷新策略：

* 应用启动后会立即扫描一次。
* 后台每 10 分钟周期刷新一次。
* 即使设备处于按需休眠状态，如果没有封面，也会尝试获取一次。
* 优先通过 go2rtc `/api/frame.jpeg` 获取截图。
* go2rtc 截图失败时，回退到本地 FFmpeg 从 `rtsp://127.0.0.1:8554/<camera>` 获取。

前端默认只在首次打开或刷新页面时请求封面，并且只对当前可视区域的节点触发加载，避免实时状态轮询导致封面闪烁或额外消耗。

## Web 批量添加

配置管理页支持一次添加多路摄像头。每行一条，支持两种格式：

```text
front-door rtsp://user:pass@192.168.1.10/stream1
garage rtsp://user:pass@192.168.1.11/stream1
onvif://user:pass@192.168.1.12
```

说明：

* `id source`：显式指定摄像头 ID。
* `source`：只写接入源时，系统会根据 host 自动生成 ID。
* 如果 ID 重复，Web UI 会自动追加后缀。
* 批量添加后仍需保存并应用配置才会生效。

## 版本检查与隐私

后端启动后会异步检查一次 GitHub Releases，之后按周期缓存刷新。稳定版发现新版本时，Web 顶部版本号旁会显示更新入口。

`dev`、`test` 或自定义版本不会被标记为“有新稳定版可升级”，但仍可能展示最新稳定版本参考。

CamKeep 默认没有遥测，不上传视频、设备列表或使用行为。版本检查只请求 GitHub Releases 元数据。
