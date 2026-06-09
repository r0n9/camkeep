package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
	"github.com/r0n9/camkeep/util"
)

var (
	overrideMux     sync.RWMutex
	overrideSaveMux sync.Mutex
	overrides       = make(map[string]string) // key: 摄像头ID, value: "start", "stop", 或空("auto")
)

const (
	scheduledRecordStateCheckDelay  = 1 * time.Second
	scheduledRecordMaintenanceEvery = 10 * time.Second
	scheduledRecordRetryDelay       = 30 * time.Second
)

// SetOverride 设置手动录像指令
func SetOverride(camID, action string) error {
	if camID == "" {
		return fmt.Errorf("摄像头 ID 不能为空")
	}

	overrideMux.Lock()
	switch action {
	case "auto":
		delete(overrides, camID) // 恢复自动
	case "start":
		overrides[camID] = action
	case "stop":
		overrides[camID] = action
		service.UpdateRecordStatus(camID, false)
	default:
		overrideMux.Unlock()
		return fmt.Errorf("无效的手动录像指令: %s", action)
	}
	overrideMux.Unlock()

	// 异步保存到磁盘，不阻塞当前 API 请求
	go SaveOverrides()
	return nil
}

// getOverride 获取当前的手动指令
func getOverride(camID string) string {
	overrideMux.RLock()
	defer overrideMux.RUnlock()
	return overrides[camID]
}

func GetOverride(camID string) string {
	control := getOverride(camID)
	if control == "" {
		return "auto"
	}
	return control
}

func recordingWindowEnabled(control string, inTimeRange bool) bool {
	switch control {
	case "start":
		return true
	case "stop":
		return false
	default:
		return inTimeRange
	}
}

// CameraTask 负责单个摄像头的生命周期管理
func CameraTask(ctx context.Context, wg *sync.WaitGroup, cam constant.Camera, eventCandidates ...onvif.Candidate) {
	defer wg.Done()

	camDir := filepath.Join(constant.DefaultRecordBaseDir, cam.ID)
	os.MkdirAll(camDir, 0755)

	service.UpdateStatus(cam.ID, false, statusModeForCamera(cam), cam.RecordTime)

	if motionRecordingEnabled(cam) {
		runMotionCameraTask(ctx, cam, camDir, firstOnvifEventCandidate(eventCandidates))
		return
	}
	runScheduledCameraTask(ctx, cam, camDir, firstOnvifEventCandidate(eventCandidates))
}

func firstOnvifEventCandidate(candidates []onvif.Candidate) *onvif.Candidate {
	if len(candidates) == 0 {
		return nil
	}
	return &candidates[0]
}

func runScheduledCameraTask(ctx context.Context, cam constant.Camera, camDir string, eventCandidate *onvif.Candidate) {
	var ffmpegCmd *exec.Cmd
	var ffmpegCancel context.CancelFunc
	var ffmpegDone chan error
	var recordErr error
	var nextStartAfter time.Time
	var markerSession *motionMarkerSession
	var releaseOnvifMarkerEvents func()

	acquireOnvifMarkerEvents := func() {
		if !motionMarkSourceUsesOnvif(cam, eventCandidate) || releaseOnvifMarkerEvents != nil {
			return
		}
		releaseOnvifMarkerEvents = RequireOnvifMotionEvents(ctx, cam, *eventCandidate, "normal-record-motion-marker")
	}

	releaseOnvifMarkerEventDemand := func(reason string) {
		if releaseOnvifMarkerEvents == nil {
			return
		}
		if reason != "" {
			log.Printf("[%s] %s，释放普通录制动检标记 ONVIF PullPoint 需求...", cam.ID, reason)
		}
		releaseOnvifMarkerEvents()
		releaseOnvifMarkerEvents = nil
	}

	ticker := time.NewTicker(scheduledRecordStateCheckDelay)
	defer ticker.Stop()
	var lastMaintenanceAt time.Time

	for {
		select {
		case <-ctx.Done():
			releaseOnvifMarkerEventDemand("")
			if markerSession != nil {
				finishMotionMarkerSession(cam, markerSession, time.Now())
				markerSession = nil
			}
			// 全局退出时，如果 ffmpeg 还在运行，杀掉它
			if ffmpegCancel != nil {
				ffmpegCancel()
				if ffmpegDone != nil {
					<-ffmpegDone
				}
			}
			service.UpdateStatus(cam.ID, false, statusModeForCamera(cam), cam.RecordTime)
			return
		case <-ticker.C:
			now := time.Now()
			if lastMaintenanceAt.IsZero() || now.Sub(lastMaintenanceAt) >= scheduledRecordMaintenanceEvery {
				ensureCameraDateDirs(camDir, now)
				renameCompletedSegments(ctx, cam, camDir, now)
				lastMaintenanceAt = now
			}

			// 判断逻辑接入覆写
			control := getOverride(cam.ID)
			inTimeRange := util.IsWithinTimeRange(cam.RecordTime)
			streamState := currentStreamState(cam.ID)

			shouldRun := recordingWindowEnabled(control, inTimeRange)

			// idle 只表示近期 TCP 可达且等待拉流验证，仍允许录制任务发起一次真实拉流。
			// 只有明确的 offline 才拦截，避免设备恢复供电后没有消费者触发 go2rtc 刷新。
			if streamState == "offline" {
				shouldRun = false
			}
			if !shouldRun {
				recordErr = nil
				nextStartAfter = time.Time{}
			}

			isRunning := ffmpegCmd != nil && ffmpegCmd.ProcessState == nil
			if ffmpegCmd != nil && !isRunning && ffmpegDone != nil {
				select {
				case err := <-ffmpegDone:
					if err != nil && ctx.Err() == nil && shouldRun {
						recordErr = err
						nextStartAfter = now.Add(scheduledRecordRetryDelay)
						log.Printf("[%s] 录制进程异常退出，%s 后重试: %v", cam.ID, scheduledRecordRetryDelay, err)
					}
					ffmpegCmd = nil
					ffmpegDone = nil
					ffmpegCancel = nil
					isRunning = false
				default:
					isRunning = true
				}
			}

			retryBlocked := shouldRun && !isRunning && recordErr != nil && now.Before(nextStartAfter)

			if shouldRun && !isRunning && !retryBlocked {
				log.Printf("[%s] 启动录制...", cam.ID)
				var fCtx context.Context
				fCtx, ffmpegCancel = context.WithCancel(ctx)
				var err error
				ffmpegCmd, err = startFFmpeg(fCtx, cam, camDir)
				if err != nil {
					recordErr = err
					nextStartAfter = now.Add(scheduledRecordRetryDelay)
					ffmpegCancel()
					ffmpegCancel = nil
					ffmpegCmd = nil
					log.Printf("[%s] 启动 FFmpeg 失败，%s 后重试: %v", cam.ID, scheduledRecordRetryDelay, err)
				} else {
					recordErr = nil
					nextStartAfter = time.Time{}
					ffmpegDone = make(chan error, 1)

					go func(c *exec.Cmd) {
						err := c.Wait()
						log.Printf("[%s] FFmpeg 进程已退出, err: %v", cam.ID, err)
						ffmpegDone <- err
					}(ffmpegCmd)

					isRunning = true
				}

			} else if !shouldRun && isRunning {
				// 细化日志输出，方便排查是时间到了还是流断了
				if streamState == "offline" {
					log.Printf("[%s] 检测到流状态离线 (Offline)，已强制中断录制...", cam.ID)
				} else if control == "stop" {
					log.Printf("[%s] 已被手动停止，停止录制...", cam.ID)
				} else {
					log.Printf("[%s] 录制条件不符，停止录制...", cam.ID)
				}
				ffmpegCancel()
				if ffmpegDone != nil {
					<-ffmpegDone
				}
				ffmpegCmd = nil
				ffmpegDone = nil
				ffmpegCancel = nil

				isRunning = false
			}

			markerShouldRun := isRunning && shouldRun && motionMarkingEnabled(cam)
			if markerShouldRun {
				acquireOnvifMarkerEvents()
			} else {
				releaseOnvifMarkerEventDemand("")
			}

			if markerShouldRun {
				if event, ok := motionMarkerRecentEvent(cam, now); ok {
					if markerSession == nil {
						markerSession = startMotionMarkerSession(cam, event, now)
						log.Printf("[%s] 动检时间轴标记窗口已开始: source=%s start=%s",
							cam.ID, markerSession.marker.Source, markerSession.marker.Start.Format(time.RFC3339))
					} else if !updateMotionMarkerSession(cam, markerSession, event, now) {
						finishMotionMarkerSession(cam, markerSession, event.At)
						markerSession = startMotionMarkerSession(cam, event, now)
						log.Printf("[%s] 动检时间轴标记窗口已切换: source=%s start=%s",
							cam.ID, markerSession.marker.Source, markerSession.marker.Start.Format(time.RFC3339))
					}
				} else if markerSession != nil {
					finishMotionMarkerSession(cam, markerSession, now)
					markerSession = nil
				}
			} else if markerSession != nil {
				finishMotionMarkerSession(cam, markerSession, now)
				markerSession = nil
			}

			recordState := service.RecordStateIdle
			switch {
			case isRunning:
				recordState = service.RecordStateRecording
			case shouldRun && recordErr != nil:
				// 录制条件满足但 FFmpeg 无法建立有效录制时，明确暴露失败状态，
				// 避免前端把退避等待误显示成“录制中”。
				recordState = service.RecordStateError
			}
			service.UpdateRecordState(cam.ID, recordState, statusModeForCamera(cam), cam.RecordTime)
		}
	}
}

func runMotionCameraTask(ctx context.Context, cam constant.Camera, camDir string, eventCandidate *onvif.Candidate) {
	var harvestCmd *exec.Cmd
	var harvestCancel context.CancelFunc
	var harvestDone chan error
	var eventSession *eventRecordSession
	var releaseOnvifMotionEvents func()

	acquireOnvifMotionEvents := func() {
		if !motionEventSourceUsesOnvif(cam, eventCandidate) || releaseOnvifMotionEvents != nil {
			return
		}
		releaseOnvifMotionEvents = RequireOnvifMotionEvents(ctx, cam, *eventCandidate, "motion-recording")
	}

	releaseOnvifMotionEventDemand := func(reason string) {
		if releaseOnvifMotionEvents == nil {
			return
		}
		if reason != "" {
			log.Printf("[%s] %s，释放 ONVIF PullPoint motion 事件需求...", cam.ID, reason)
		}
		releaseOnvifMotionEvents()
		releaseOnvifMotionEvents = nil
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			releaseOnvifMotionEventDemand("")
			if harvestCancel != nil {
				harvestCancel()
				if harvestDone != nil {
					<-harvestDone
				}
			}
			service.UpdateStatus(cam.ID, false, statusModeForCamera(cam), cam.RecordTime)
			return
		case <-ticker.C:
			now := time.Now()
			ensureCameraDateDirs(camDir, now)

			control := getOverride(cam.ID)
			inTimeRange := util.IsWithinTimeRange(cam.RecordTime)
			streamState := currentStreamState(cam.ID)
			recordingWindow := recordingWindowEnabled(control, inTimeRange)
			harvestShouldRun := recordingWindow && streamState != "offline"
			if !harvestShouldRun {
				resetMotionDetected(cam.ID)
			}

			if harvestCmd != nil && harvestDone != nil {
				select {
				case <-harvestDone:
					releaseOnvifMotionEventDemand("动检 Time-Shift 缓存引擎退出")
					harvestCmd = nil
					harvestDone = nil
					harvestCancel = nil
					if eventSession != nil && ctx.Err() == nil {
						log.Printf("[%s] 动检 Time-Shift 缓存引擎退出，结束当前动检事件...", cam.ID)
						finishEventRecordSession(ctx, cam, camDir, eventSession, time.Now())
						eventSession = nil
					}
				default:
				}
			}

			harvestRunning := harvestCmd != nil
			if harvestShouldRun && !harvestRunning {
				log.Printf("[%s] 启动动检 Time-Shift 大块缓存引擎...", cam.ID)
				var hCtx context.Context
				hCtx, harvestCancel = context.WithCancel(ctx)
				var err error
				harvestCmd, err = startMotionTimeShiftFFmpeg(hCtx, cam)
				if err != nil {
					log.Printf("[%s] 启动动检 Time-Shift 缓存引擎失败: %v", cam.ID, err)
					harvestCancel()
					harvestCancel = nil
					harvestCmd = nil
				} else {
					harvestDone = make(chan error, 1)
					go func(c *exec.Cmd) {
						err := c.Wait()
						log.Printf("[%s] 动检 Time-Shift 缓存引擎已退出, err: %v", cam.ID, err)
						harvestDone <- err
					}(harvestCmd)
					acquireOnvifMotionEvents()
				}
			} else if !harvestShouldRun && harvestRunning {
				if streamState == "offline" {
					log.Printf("[%s] 检测到流状态离线 (Offline)，停止动检 Time-Shift 缓存引擎...", cam.ID)
				} else if control == "stop" {
					log.Printf("[%s] 已被手动停止，停止动检 Time-Shift 缓存引擎...", cam.ID)
				} else {
					log.Printf("[%s] 录制时间窗结束，停止动检 Time-Shift 缓存引擎...", cam.ID)
				}
				harvestCancel()
				releaseOnvifMotionEventDemand("")
				if harvestDone != nil {
					<-harvestDone
				}
				harvestCmd = nil
				harvestDone = nil
				harvestCancel = nil
			}

			eventShouldRecord := harvestShouldRun && harvestCmd != nil && motionDetectedRecently(cam.ID, now)
			if eventShouldRecord && eventSession == nil {
				eventSession = newEventRecordSession(EventTypeMotion, now)
				log.Printf("[%s] 检测到移动，记录动检事件窗口: start=%s",
					cam.ID, eventSession.StartTime.Format(time.RFC3339))
			}

			if harvestCmd != nil {
				protectAfter := time.Time{}
				if eventSession != nil {
					protectAfter = eventSession.StartTime
				}
				pruneMotionTimeShiftSegments(cam.ID, protectAfter)
			}

			if !eventShouldRecord && eventSession != nil {
				if streamState == "offline" {
					log.Printf("[%s] 检测到流状态离线 (Offline)，结束动检事件录制...", cam.ID)
				} else if control == "stop" {
					log.Printf("[%s] 已被手动停止，结束动检事件录制...", cam.ID)
				} else if !recordingWindow {
					log.Printf("[%s] 录制时间窗结束，结束动检事件录制...", cam.ID)
				} else {
					log.Printf("[%s] 画面连续 %s 无变化，结束动检事件录制...", cam.ID, motionRecordIdleTimeout)
				}
				finishEventRecordSession(ctx, cam, camDir, eventSession, now)
				eventSession = nil
			}

			recordState := service.RecordStateIdle
			if eventSession != nil {
				recordState = service.RecordStateMotionRecording
			} else if harvestCmd != nil {
				recordState = service.RecordStateMotionDetecting
			}
			service.UpdateRecordState(cam.ID, recordState, statusModeForCamera(cam), cam.RecordTime)
		}
	}
}

// segmentStartLayout 是 ffmpeg -strftime 生成的文件名中开始时间部分格式
const segmentStartLayout = "20060102_150405"

// renameCompletedSegments 将已完成的 normal 模式 segment 从 CamID_YYYYMMDD_HHMMSS_unknown.ext
// 重命名为 CamID_YYYYMMDD_HHMMSS_HHMMSS.ext
// 同时扫描今天和昨天目录，解决跨天时最后一个 segment 漏处理的问题
func renameCompletedSegments(ctx context.Context, cam constant.Camera, camDir string, now time.Time) {
	if cam.Mode == "timelapse" {
		return
	}
	segDur := time.Duration(cam.SegmentDuration) * time.Second
	renameSegmentsInDir(ctx, cam.ID, filepath.Join(camDir, now.Format("2006-01-02")), segDur, now)
	renameSegmentsInDir(ctx, cam.ID, filepath.Join(camDir, now.AddDate(0, 0, -1).Format("2006-01-02")), segDur, now)
}

func renameSegmentsInDir(ctx context.Context, camID, dateDir string, segDur time.Duration, now time.Time) {
	entries, err := os.ReadDir(dateDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		parts := strings.Split(base, "_")

		// 格式: CamID_YYYYMMDD_HHMMSS_unknown 有4段，第4段必须是 "unknown" 才处理
		if len(parts) != 4 || parts[3] != "unknown" {
			continue
		}
		startTime, err := time.ParseInLocation(segmentStartLayout, parts[1]+"_"+parts[2], time.Local)
		if err != nil {
			continue
		}
		if now.Before(startTime.Add(segDur).Add(10 * time.Second)) {
			continue
		}
		path := filepath.Join(dateDir, name)
		dur, err := probeVideoDuration(ctx, path)
		if err != nil {
			continue
		}
		endTime := startTime.Add(dur)
		newName := fmt.Sprintf("%s_%s_%s%s", camID, startTime.Format(segmentStartLayout), endTime.Format("150405"), ext)
		os.Rename(path, filepath.Join(dateDir, newName))
	}
}

func ensureCameraDateDirs(camDir string, now time.Time) {
	// 提前铺路，创建今天和明天的日期目录，防止跨天时目标文件夹不存在。
	todayDir := filepath.Join(camDir, now.Format("2006-01-02"))
	tomorrowDir := filepath.Join(camDir, now.AddDate(0, 0, 1).Format("2006-01-02"))
	os.MkdirAll(todayDir, 0755)
	os.MkdirAll(tomorrowDir, 0755)
}

func currentStreamState(camID string) string {
	service.StatusMux.RLock()
	defer service.StatusMux.RUnlock()
	streamState := "offline"
	if st, ok := service.StatusMap[camID]; ok {
		streamState = st.StreamState
	}
	return streamState
}

// startFFmpeg 构建并启动 FFmpeg 进程
func startFFmpeg(ctx context.Context, cam constant.Camera, camDir string) (*exec.Cmd, error) {
	fileNamePattern := fmt.Sprintf("%s/%%Y-%%m-%%d/%s_%%Y%%m%%d_%%H%%M%%S_unknown.%s", filepath.ToSlash(camDir), cam.ID, cam.Format)

	var args []string

	// 获取安全转义后的 RTSP URL
	// safeRTSPUrl := util.EscapeRTSPAuth(cam.RTSPUrl)
	safeRTSPUrl := fmt.Sprintf("rtsp://%s:8554/%s", constant.DefaultGo2rtcHost, cam.ID)

	// 如果未设置模式，默认按 normal 处理
	if cam.Mode == "" || cam.Mode == "normal" {
		args = []string{
			"-loglevel", "error",
			"-rtsp_transport", "tcp",
			"-timeout", "5000000",
			"-max_delay", "500000",
			"-reorder_queue_size", "1024",
			"-use_wallclock_as_timestamps", "1", // 强制使用本地系统时钟作为视频帧的时间戳基准
			"-i", safeRTSPUrl,
			"-c:v", "copy", // 视频流保持直接拷贝，不消耗 CPU
			"-c:a", "aac", // 把摄像头的 pcm_alaw 实时转成 MP4 兼容的 AAC 音频
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%d", cam.SegmentDuration),
			"-segment_atclocktime", "1", // 开启按自然时钟对齐切割（如 00:00, 00:05）
			"-segment_format", cam.Format,
			"-reset_timestamps", "1",
			"-strftime", "1",
		}

		// 普通模式的 MP4 碎片落盘后作为点播文件使用，优先生成标准 faststart MP4，
		// 让浏览器原生播放器更容易秒开、识别时长和拖拽。
		if cam.Format == "mp4" {
			args = append(args, "-segment_format_options", "movflags=+faststart")
		}

		// 最后统一追加文件名
		args = append(args, fileNamePattern)
	} else if cam.Mode == "timelapse" {
		if cam.CaptureInterval <= 0 {
			cam.CaptureInterval = 1
		}

		// 定义"逻辑播放帧率"（即一秒钟你想看几张抓拍的图片）
		// 之前等效于 25，导致画面狂闪。
		// 推荐值：5 (每张停留 0.2秒，适合非常慢的变化) 或 10 (每张停留 0.1秒，适合常规监控快放)
		// 建议后续可将此参数暴露到 conf.yaml 中，这里先设为 10
		logicalPlaybackFPS := 5.0

		// 组装延时录像的视频滤镜：
		// fps=1/N : 每 N 秒截取一帧
		// setpts=N/(逻辑帧率*TB) : 告诉 FFmpeg 这些帧应该以多快的速度播放
		vfFilter := fmt.Sprintf("fps=1/%d,setpts=N/(%.1f*TB)", cam.CaptureInterval, logicalPlaybackFPS)

		// 先通过整数除法，确定这段视频绝对包含的原始抓拍帧数
		framesPerSegment := cam.SegmentDuration / cam.CaptureInterval
		if framesPerSegment <= 0 {
			framesPerSegment = 25
		}

		// 用逻辑播放帧率反推 FFmpeg 内部的实际切片时间
		actualFFmpegSegmentTime := float64(framesPerSegment) / logicalPlaybackFPS

		// 输出物理帧率强制保持 25，确保浏览器 DPlayer/WebRTC 的完美兼容
		outputFPS := 25

		// GOP 大小 = 切片总秒数 * 物理输出帧率
		gopSize := int(actualFFmpegSegmentTime * float64(outputFPS))

		args = []string{
			"-loglevel", "error",
			"-rtsp_transport", "tcp",
			"-timeout", "5000000",
			"-i", safeRTSPUrl,
			"-an",
			"-vf", vfFilter,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "28",
			"-r", fmt.Sprintf("%d", outputFPS), // 强制输出物理帧率为 25，FFmpeg 会自动复制帧补齐
			"-g", fmt.Sprintf("%d", gopSize), // 强制关键帧间隔对齐
			"-sc_threshold", "0",
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%.3f", actualFFmpegSegmentTime),
			"-segment_format", cam.Format,
			"-reset_timestamps", "1",
			"-strftime", "1",
		}

		if cam.Format == "mp4" {
			args = append(args, "-segment_format_options", "movflags=frag_keyframe+empty_moov")
		}

		args = append(args, fmt.Sprintf("%s/%%Y-%%m-%%d/%s_%%Y%%m%%d_%%H%%M%%S_timelapse.%s", filepath.ToSlash(camDir), cam.ID, cam.Format))
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

// LoadOverrides 在程序启动时调用（例如 main 函数开头）
func LoadOverrides() {
	data, err := os.ReadFile(constant.OverridesFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取手动录像指令文件失败: %v", err)
		}
		// 如果文件不存在，属于正常情况（初次启动），静默跳过
		return
	}

	loaded := make(map[string]string)
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("解析手动录像指令文件失败: %v", err)
		return
	}
	if loaded == nil {
		loaded = make(map[string]string)
	}

	cleaned := make(map[string]string, len(loaded))
	for camID, action := range loaded {
		if camID == "" {
			continue
		}
		switch action {
		case "start", "stop":
			cleaned[camID] = action
		case "", "auto":
			// auto 不需要持久化。
		default:
			log.Printf("[%s] 忽略无效的手动录像指令: %s", camID, action)
		}
	}

	overrideMux.Lock()
	overrides = cleaned
	overrideMux.Unlock()
	log.Printf("成功加载了 %d 个摄像头的覆盖指令", len(cleaned))
}

// SaveOverrides 将当前的 overrides 写入文件
func SaveOverrides() {
	overrideSaveMux.Lock()
	defer overrideSaveMux.Unlock()

	overrideMux.RLock()
	// 注意：这里用深拷贝把数据拿出来再写入，避免在进行磁盘 I/O 时长时间阻塞其他 goroutine 获取读写锁
	mapCopy := make(map[string]string, len(overrides))
	for k, v := range overrides {
		mapCopy[k] = v
	}
	overrideMux.RUnlock()

	data, err := json.MarshalIndent(mapCopy, "", "  ")
	if err != nil {
		log.Printf("序列化手动录像指令失败: %v", err)
		return
	}

	// 确保目录存在
	os.MkdirAll(filepath.Dir(constant.OverridesFilePath), 0755)

	// 写入文件
	if err := os.WriteFile(constant.OverridesFilePath, data, 0644); err != nil {
		log.Printf("保存手动录像指令到文件失败: %v", err)
	}
}

func PruneOverridesForCameras(cameras []constant.Camera) {
	validIDs := make(map[string]bool, len(cameras))
	for _, cam := range cameras {
		validIDs[cam.ID] = true
	}

	changed := false
	overrideMux.Lock()
	for camID := range overrides {
		if !validIDs[camID] {
			delete(overrides, camID)
			changed = true
			log.Printf("[%s] 已清理无效的手动录像指令", camID)
		}
	}
	overrideMux.Unlock()

	if changed {
		SaveOverrides()
	}
}
