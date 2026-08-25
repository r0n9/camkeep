package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
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
	scheduledRecordBoundaryGuard    = 2 * time.Second
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

func scheduledRecordingWindowEnabled(control, recordTime string, now time.Time) bool {
	inTimeRange := util.IsTimeWithinTimeRange(now, recordTime)
	if !recordingWindowEnabled(control, inTimeRange) {
		return false
	}
	return control == "start" || !util.IsTimeRangeEndingSoon(now, recordTime, scheduledRecordBoundaryGuard)
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
			streamState := currentStreamState(cam.ID)

			shouldRun := scheduledRecordingWindowEnabled(control, cam.RecordTime, now)

			// idle 只表示近期 TCP 可达且等待拉流验证，仍允许录制任务发起一次真实拉流。
			// 只有明确的 offline 才拦截，避免设备恢复供电后没有消费者触发 go2rtc 刷新。
			if streamState == "offline" {
				shouldRun = false
			}
			if !shouldRun {
				recordErr = nil
				nextStartAfter = time.Time{}
			}

			// 进程是否退出完全由 ffmpegDone 驱动，不读 ffmpegCmd.ProcessState：
			// Wait() goroutine 会并发写入 ProcessState，主循环直接读取是数据竞争。
			isRunning := ffmpegCmd != nil
			if ffmpegCmd != nil && ffmpegDone != nil {
				select {
				case err := <-ffmpegDone:
					if err != nil && ctx.Err() == nil && shouldRun {
						recordErr = err
						nextStartAfter = now.Add(scheduledRecordRetryDelay)
						log.Printf("[%s] 录制进程异常退出，%s 后重试: %v", cam.ID, scheduledRecordRetryDelay, err)
					}
					if ffmpegCancel != nil {
						ffmpegCancel() // 进程已自然退出，仍需 cancel 释放子 context，避免挂在父 context 上泄漏
					}
					ffmpegCmd = nil
					ffmpegDone = nil
					ffmpegCancel = nil
					isRunning = false
				default:
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
			if eventSession != nil {
				// 任务取消（停机/保存配置热重启）时导出进行中的动检事件：
				// 下次启动 startMotionTimeShiftFFmpeg 会 RemoveAll 缓存目录，不导出就整体丢失。
				// ctx 已取消，内部 ffmpeg/ffprobe 必须使用独立的超时 context 才能执行。
				log.Printf("[%s] 任务退出，导出进行中的动检事件录像...", cam.ID)
				exportCtx, cancelExport := context.WithTimeout(context.Background(), motionExitExportTimeout)
				finishEventRecordSession(exportCtx, cam, camDir, eventSession, time.Now())
				cancelExport()
				eventSession = nil
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
				clearMotionAutoFrameDiffFollowUp(cam.ID)
			}

			if harvestCmd != nil && harvestDone != nil {
				select {
				case harvestErr := <-harvestDone:
					releaseOnvifMotionEventDemand("动检 Time-Shift 缓存引擎退出")
					if harvestCancel != nil {
						harvestCancel() // 进程已自然退出，仍需 cancel 释放子 context，避免挂在父 context 上泄漏
					}
					harvestCmd = nil
					harvestDone = nil
					harvestCancel = nil
					if eventSession != nil && ctx.Err() == nil {
						log.Printf("[%s] 动检 Time-Shift 缓存引擎退出，结束当前动检事件...", cam.ID)
						finishEventRecordSession(ctx, cam, camDir, eventSession, time.Now())
						eventSession = nil
					}
					if motionTimeShiftExitedNoSpace(harvestErr) {
						enableMotionTimeShiftTmpFallback(cam.ID)
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

// 延时录像参数推导
//
// 用户只需要配置 capture_interval，其余参数全部由代码推导，核心约束是：
//
//	加速倍率 = 采样间隔 × 逻辑播放帧率 = 5 × capture_interval
//
// 这与历史行为完全一致，所以成片的播放速度、分片播放时长都不会因为这套推导而改变。
//
// 顺滑度只由"相邻画面相隔多少现实时间"（即采样间隔）决定。历史实现取采样间隔 =
// capture_interval、逻辑播放帧率 = 5，每张抓拍要在屏幕上停留 0.2 秒，观感是一跳一跳的
// 幻灯片。现在把逻辑播放帧率提到与输出帧率相同的 25，并把采样间隔缩到
// capture_interval/5 来守住倍率：这等价于把连续录像用播放器按同样倍数快放，落盘的每
// 一帧都是真实抓拍，不再有复制帧。代价只有存储变大，CPU 几乎不变（延时模式的开销
// 八成在解码完整码流，抽帧滤镜本身免费，编码只占一小部分）。
const (
	// timelapseOutputFPS 同时是输出物理帧率和逻辑播放帧率。两者相等，FFmpeg 就不需要
	// 复帧补齐，写进文件的每一帧都是真实抓拍。
	timelapseOutputFPS = 25
	// timelapseDensityFactor 是抓帧密度相对 capture_interval 的提升倍数，也是逻辑播放
	// 帧率相对历史值(5)的提升倍数。两者相乘保持加速倍率不变。
	timelapseDensityFactor = 5
	// timelapseFilterTimebase 固定滤镜时基。fps 滤镜会把时基设成采样间隔（可能长达数十
	// 秒），此时 setpts 的运算结果会被截断成整数，导致大量抓帧挤在同一个时间戳上、成片
	// 时长和倍率都对不上。固定成 1/1000 后 PTS 落在整数刻度上。
	timelapseFilterTimebase = "1/1000"
	// timelapseFallbackFramesPerSegment 是 capture_interval 远大于 segment_duration
	// （单个分片装不下一帧）这种矛盾配置的兜底帧数，对应 5 秒播放时长。
	timelapseFallbackFramesPerSegment = 125
)

// timelapsePlan 是一路延时摄影摄像头推导出的全部 FFmpeg 参数。
type timelapsePlan struct {
	CaptureInterval  int     // 归一化后的 capture_interval
	SamplingFPS      string  // fps 滤镜参数，约分后的有理数，如 "1/12"
	SamplingInterval float64 // 相邻画面相隔的现实秒数，越小越顺滑
	SpeedupFactor    int     // 加速倍率
	FramesPerSegment int
	SegmentSeconds   float64 // 单个分片的播放时长
	GopSize          int
	VideoFilter      string
	SegmentDegraded  bool // 是否触发了分片帧数兜底
}

func newTimelapsePlan(captureInterval, segmentDuration int) timelapsePlan {
	if captureInterval <= 0 {
		captureInterval = 1
	}

	// 采样帧率 = densityFactor / capture_interval。用约分后的有理数表达，既避免浮点字符串
	// 的精度问题，也让日志和滤镜串更易读（capture_interval=60 -> "1/12"）。
	num, den := reduceFraction(timelapseDensityFactor, captureInterval)
	samplingInterval := float64(captureInterval) / float64(timelapseDensityFactor)

	// 用四舍五入而非整除，避免 capture_interval 不能整除时分片覆盖的现实时间偏短。
	framesPerSegment := 0
	if segmentDuration > 0 {
		framesPerSegment = int(math.Round(float64(segmentDuration) / samplingInterval))
	}
	degraded := false
	if framesPerSegment <= 0 {
		framesPerSegment = timelapseFallbackFramesPerSegment
		degraded = true
	}

	samplingFPS := fmt.Sprintf("%d/%d", num, den)
	return timelapsePlan{
		CaptureInterval:  captureInterval,
		SamplingFPS:      samplingFPS,
		SamplingInterval: samplingInterval,
		SpeedupFactor:    captureInterval * timelapseOutputFPS / timelapseDensityFactor,
		FramesPerSegment: framesPerSegment,
		SegmentSeconds:   float64(framesPerSegment) / float64(timelapseOutputFPS),
		// 逻辑播放帧率等于输出帧率，所以 GOP 帧数正好等于分片帧数，关键帧对齐分片边界。
		GopSize: framesPerSegment,
		VideoFilter: fmt.Sprintf("fps=%s,settb=%s,setpts=N/(%d*TB)",
			samplingFPS, timelapseFilterTimebase, timelapseOutputFPS),
		SegmentDegraded: degraded,
	}
}

func reduceFraction(num, den int) (int, int) {
	if g := gcd(num, den); g > 0 {
		return num / g, den / g
	}
	return num, den
}

func gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// segmentStartLayout 是 ffmpeg -strftime 生成的文件名中开始时间部分格式
const segmentStartLayout = "20060102_150405"

// renameCompletedSegments 将已完成的 normal 模式 segment 从 CamID_YYYYMMDD_HHMMSS_unknown.ext
// 重命名为 CamID_YYYYMMDD_HHMMSS_HHMMSS.ext
// 同时扫描今天和昨天目录，解决跨天时最后一个 segment 漏处理的问题
func renameCompletedSegments(ctx context.Context, cam constant.Camera, camDir string, now time.Time) {
	if isTimelapseMode(cam) {
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
		startTime, ok := parseUnknownSegmentStart(camID, name)
		if !ok {
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
		newName := fmt.Sprintf("%s_%s_%s%s", camID, startTime.Format(segmentStartLayout), endTime.Format("150405"), filepath.Ext(name))
		os.Rename(path, filepath.Join(dateDir, newName))
	}
}

// parseUnknownSegmentStart 从 CamID_YYYYMMDD_HHMMSS_unknown.ext 文件名中解析段开始时间。
// CamID 自身可能含下划线，因此从尾部定位时间与 unknown 标记，并校验前缀与 camID 一致。
func parseUnknownSegmentStart(camID, name string) (time.Time, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) < 4 || parts[len(parts)-1] != "unknown" {
		return time.Time{}, false
	}
	if strings.Join(parts[:len(parts)-3], "_") != camID {
		return time.Time{}, false
	}
	startTime, err := time.ParseInLocation(segmentStartLayout, parts[len(parts)-3]+"_"+parts[len(parts)-2], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return startTime, true
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
	if isNormalMode(cam) {
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
	} else if isTimelapseMode(cam) {
		plan := newTimelapsePlan(cam.CaptureInterval, cam.SegmentDuration)
		if plan.SegmentDegraded {
			log.Printf("[%s] capture_interval=%ds 远大于 segment_duration=%ds，单个分片装不下一帧，已按 %d 帧兜底",
				cam.ID, plan.CaptureInterval, cam.SegmentDuration, plan.FramesPerSegment)
		}
		log.Printf("[%s] 延时录像参数: 采样间隔=%.2fs 加速=%dx 每片=%.1fs(%d 帧)",
			cam.ID, plan.SamplingInterval, plan.SpeedupFactor, plan.SegmentSeconds, plan.FramesPerSegment)

		args = []string{
			"-loglevel", "error",
			"-rtsp_transport", "tcp",
			"-timeout", "5000000",
			"-i", safeRTSPUrl,
			"-an",
			"-vf", plan.VideoFilter,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "28",
			"-r", fmt.Sprintf("%d", timelapseOutputFPS), // 输出帧率与逻辑播放帧率相同，无需复帧
			"-g", fmt.Sprintf("%d", plan.GopSize), // 强制关键帧间隔对齐分片边界
			"-sc_threshold", "0",
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%.3f", plan.SegmentSeconds),
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
