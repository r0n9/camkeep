package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/util"
)

const (
	motionDetectFrameWidth       = 32
	motionDetectFrameHeight      = 24
	motionDetectFPS              = 2
	motionDetectPixelThreshold   = 15
	motionDetectRatioThreshold   = 0.01
	motionDetectStateCheckDelay  = 1 * time.Second
	motionDetectRestartDelay     = 3 * time.Second
	motionDetectIdleLogInterval  = 5 * time.Second
	motionDetectAlertLogInterval = 2 * time.Second
)

type motionFrameStats struct {
	DiffSum    int
	DiffPixels int
	DiffRatio  float64
	Motion     bool
}

// MotionDetectTask implements 方案二：低分辨率灰度帧差检测。
func MotionDetectTask(ctx context.Context, wg *sync.WaitGroup, cam constant.Camera) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !motionDetectionShouldRun(cam) {
			resetMotionDetected(cam.ID)
			if !waitMotionDetect(ctx, motionDetectStateCheckDelay) {
				return
			}
			continue
		}

		if err := runMotionDetectorUntilDisabled(ctx, cam); err != nil && ctx.Err() == nil && motionDetectionShouldRun(cam) {
			log.Printf("[%s] 动态检测进程退出: %v，%s 后重试", cam.ID, err, motionDetectRestartDelay)
			if !waitMotionDetect(ctx, motionDetectRestartDelay) {
				return
			}
			continue
		}

		if !waitMotionDetect(ctx, motionDetectStateCheckDelay) {
			return
		}
	}
}

func motionDetectionShouldRun(cam constant.Camera) bool {
	if !motionRecordingEnabled(cam) {
		return false
	}

	control := getOverride(cam.ID)
	inTimeRange := util.IsWithinTimeRange(cam.RecordTime)
	if !recordingWindowEnabled(control, inTimeRange) {
		return false
	}

	return currentStreamState(cam.ID) != "offline"
}

func runMotionDetectorUntilDisabled(ctx context.Context, cam constant.Camera) error {
	detectorCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)

		ticker := time.NewTicker(motionDetectStateCheckDelay)
		defer ticker.Stop()

		for {
			select {
			case <-detectorCtx.Done():
				return
			case <-ticker.C:
				if !motionDetectionShouldRun(cam) {
					resetMotionDetected(cam.ID)
					log.Printf("[%s] 动态检测条件不符，停止检测进程...", cam.ID)
					cancel()
					return
				}
			}
		}
	}()

	err := runMotionDetector(detectorCtx, cam)
	cancel()
	<-watcherDone
	return err
}

func waitMotionDetect(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func runMotionDetector(ctx context.Context, cam constant.Camera) error {
	ratioThreshold := motionRatioThreshold(cam)
	inputURL := motionDetectInputURL(cam)
	args := []string{
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-i", inputURL,
		"-an",
		"-vf", fmt.Sprintf("fps=%d,scale=%d:%d", motionDetectFPS, motionDetectFrameWidth, motionDetectFrameHeight),
		"-pix_fmt", "gray",
		"-f", "rawvideo",
		"-",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	log.Printf("[%s] 动态检测已启动: %dx%d gray @ %dfps, ratioThreshold=%.4f",
		cam.ID, motionDetectFrameWidth, motionDetectFrameHeight, motionDetectFPS, ratioThreshold)

	readErr := readMotionFrames(ctx, cam.ID, stdoutPipe, ratioThreshold)
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return readErr
	}
	if waitErr != nil {
		return waitErr
	}
	return readErr
}

func motionDetectInputURL(cam constant.Camera) string {
	if motionURL := strings.TrimSpace(cam.MotionURL); motionURL != "" {
		return motionURL
	}
	return fmt.Sprintf("rtsp://%s:8554/%s", constant.DefaultGo2rtcHost, cam.ID)
}

func readMotionFrames(ctx context.Context, camID string, reader io.Reader, ratioThreshold float64) error {
	frameSize := motionDetectFrameWidth * motionDetectFrameHeight
	buf := make([]byte, frameSize)
	prevFrame := make([]byte, frameSize)
	hasPrevFrame := false
	var lastLog time.Time

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if _, err := io.ReadFull(reader, buf); err != nil {
			return err
		}

		if !hasPrevFrame {
			copy(prevFrame, buf)
			hasPrevFrame = true
			log.Printf("[%s] 动态检测已获得首帧，开始帧差验证", camID)
			continue
		}

		stats := compareMotionFrames(prevFrame, buf, motionDetectPixelThreshold, ratioThreshold)
		now := time.Now()
		if stats.Motion {
			markMotionDetected(camID, now)
		}
		logInterval := motionDetectIdleLogInterval
		if stats.Motion {
			logInterval = motionDetectAlertLogInterval
		}
		if lastLog.IsZero() || now.Sub(lastLog) >= logInterval {
			if stats.Motion {
				log.Printf("[%s] 动态检测: motion=%t, diffPixels=%d/%d, diffRatio=%.2f%%, diffSum=%d",
					camID, stats.Motion, stats.DiffPixels, frameSize, stats.DiffRatio*100, stats.DiffSum)
			}
			lastLog = now
		}

		copy(prevFrame, buf)
	}
}

func compareMotionFrames(prevFrame, currentFrame []byte, pixelThreshold int, ratioThreshold float64) motionFrameStats {
	frameSize := len(prevFrame)
	if len(currentFrame) < frameSize {
		frameSize = len(currentFrame)
	}
	if frameSize == 0 {
		return motionFrameStats{}
	}

	stats := motionFrameStats{}
	for i := 0; i < frameSize; i++ {
		diff := int(currentFrame[i]) - int(prevFrame[i])
		if diff < 0 {
			diff = -diff
		}
		stats.DiffSum += diff
		if diff > pixelThreshold {
			stats.DiffPixels++
		}
	}

	stats.DiffRatio = float64(stats.DiffPixels) / float64(frameSize)
	stats.Motion = stats.DiffRatio > ratioThreshold
	return stats
}
