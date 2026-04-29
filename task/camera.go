package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/service"
	"github.com/r0n9/camkeep/util"
)

var (
	overrideMux sync.RWMutex
	overrides   = make(map[string]string) // key: 摄像头ID, value: "start", "stop", 或空("auto")
)

// SetOverride 设置手动录像指令
func SetOverride(camID, action string) {
	overrideMux.Lock()
	if action == "auto" {
		delete(overrides, camID) // 恢复自动
	} else if action == "start" {
		overrides[camID] = action
		service.UpdateRecordStatus(camID, true) // 立刻更新当前status
	} else if action == "stop" {
		overrides[camID] = action
		service.UpdateRecordStatus(camID, false)
	}

	overrideMux.Unlock()

	// 异步保存到磁盘，不阻塞当前 API 请求
	go SaveOverrides()
}

// getOverride 获取当前的手动指令
func getOverride(camID string) string {
	overrideMux.RLock()
	defer overrideMux.RUnlock()
	return overrides[camID]
}

// CameraTask 负责单个摄像头的生命周期管理
func CameraTask(ctx context.Context, wg *sync.WaitGroup, cam constant.Camera) {
	defer wg.Done()

	camDir := filepath.Join(constant.DefaultRecordBaseDir, cam.ID)
	os.MkdirAll(camDir, 0755)

	var ffmpegCmd *exec.Cmd
	var ffmpegCancel context.CancelFunc

	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次状态
	defer ticker.Stop()

	service.UpdateStatus(cam.ID, false, cam.Mode)

	for {
		select {
		case <-ctx.Done():
			// 全局退出时，如果 ffmpeg 还在运行，杀掉它
			if ffmpegCancel != nil {
				ffmpegCancel()
			}
			return
		case <-ticker.C:
			// 提前铺路，创建今天和明天的日期目录
			// 防止跨天时 (00:00:00) FFmpeg 因为目标文件夹不存在而报错崩溃
			now := time.Now()
			todayDir := filepath.Join(camDir, now.Format("2006-01-02"))
			tomorrowDir := filepath.Join(camDir, now.AddDate(0, 0, 1).Format("2006-01-02"))
			os.MkdirAll(todayDir, 0755)
			os.MkdirAll(tomorrowDir, 0755)

			// 判断逻辑接入覆写
			control := getOverride(cam.ID)
			inTimeRange := util.IsWithinTimeRange(cam.RecordTime)

			service.StatusMux.RLock()
			streamState := "offline" // 默认为断开
			if st, ok := service.StatusMap[cam.ID]; ok {
				streamState = st.StreamState
			}
			service.StatusMux.RUnlock()

			shouldRun := false
			if control == "start" {
				shouldRun = true
			} else if control == "stop" {
				shouldRun = false
			} else {
				shouldRun = inTimeRange
			}

			// 注意：如果状态是 "idle" (按需休眠) 是可以启动的，只有明确的 "offline" 才拦截
			if streamState == "offline" {
				shouldRun = false
			}

			isRunning := ffmpegCmd != nil && ffmpegCmd.ProcessState == nil

			if shouldRun && !isRunning {
				log.Printf("[%s] 启动录制...", cam.ID)
				var fCtx context.Context
				fCtx, ffmpegCancel = context.WithCancel(ctx)
				ffmpegCmd = startFFmpeg(fCtx, cam, camDir)

				go func(c *exec.Cmd) {
					err := c.Wait()
					log.Printf("[%s] FFmpeg 进程已退出, err: %v", cam.ID, err)
				}(ffmpegCmd)

				isRunning = true

			} else if !shouldRun && isRunning {
				// 细化日志输出，方便排查是时间到了还是流断了
				if streamState == "offline" {
					log.Printf("[%s] 检测到流状态离线 (Offline)，已强制中断录制...", cam.ID)
				} else {
					log.Printf("[%s] 录制条件不符 (或已被手动停止)，停止录制...", cam.ID)
				}
				ffmpegCancel()
				ffmpegCmd = nil

				isRunning = false
			}

			service.UpdateStatus(cam.ID, isRunning, cam.Mode)
		}
	}
}

// startFFmpeg 构建并启动 FFmpeg 进程
func startFFmpeg(ctx context.Context, cam constant.Camera, camDir string) *exec.Cmd {
	fileNamePattern := fmt.Sprintf("%s/%%Y-%%m-%%d/%s_%%Y-%%m-%%d_%%H-%%M-%%S.%s", filepath.ToSlash(camDir), cam.ID, cam.Format)

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
			"-i", safeRTSPUrl,
			"-c:v", "copy", // 视频流保持直接拷贝，不消耗 CPU
			"-c:a", "aac", // 把摄像头的 pcm_alaw 实时转成 MP4 兼容的 AAC 音频
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%d", cam.SegmentDuration),
			"-segment_format", cam.Format,
			"-reset_timestamps", "1",
			"-strftime", "1",
		}

		// 如果普通模式是 mp4，加上 fMP4 标记，让浏览器可以流式播放
		if cam.Format == "mp4" {
			args = append(args, "-segment_format_options", "movflags=frag_keyframe+empty_moov")
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

		args = append(args, fileNamePattern)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("[%s] 启动 FFmpeg 失败: %v", cam.ID, err)
	}

	return cmd
}

// LoadOverrides 在程序启动时调用（例如 main 函数开头）
func LoadOverrides() {
	overrideMux.Lock()
	defer overrideMux.Unlock()

	data, err := os.ReadFile(constant.OverridesFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取手动录像指令文件失败: %v", err)
		}
		// 如果文件不存在，属于正常情况（初次启动），静默跳过
		return
	}

	if err := json.Unmarshal(data, &overrides); err != nil {
		log.Printf("解析手动录像指令文件失败: %v", err)
	} else {
		log.Printf("成功加载了 %d 个摄像头的覆盖指令", len(overrides))
	}
}

// SaveOverrides 将当前的 overrides 写入文件
func SaveOverrides() {
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
