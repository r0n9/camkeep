package task

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

const (
	motionRecordIdleTimeout        = 10 * time.Second
	motionTimeShiftPreRecord       = 5 * time.Second
	motionTimeShiftSegmentDuration = 3 * time.Minute
	motionTimeShiftSegmentCount    = 3
	motionTimeShiftBufferBaseName  = "camkeep_motion"
	motionTimeShiftFilePrefix      = "loop_"
	motionTimeShiftTimeLayout      = "20060102_150405"
	motionTimeShiftSegmentExt      = ".ts" // 从 .mp4 改为 .ts
)

type eventRecordSession struct {
	EventType string
	StartTime time.Time
	EndTime   time.Time
}

type motionTimeShiftSegment struct {
	path  string
	start time.Time
	end   time.Time
	live  bool
}

type motionTimeShiftClip struct {
	source motionTimeShiftSegment
	start  time.Time
	end    time.Time
}

func isNormalMode(cam constant.Camera) bool {
	return cam.Mode == "" || cam.Mode == "normal"
}

func motionRecordingEnabled(cam constant.Camera) bool {
	return isNormalMode(cam) && cam.MotionDetect
}

func motionEventSource(cam constant.Camera) string {
	return constant.NormalizeMotionEventSource(cam.MotionEventSource)
}

func motionEventSourceUsesOnvif(cam constant.Camera, eventCandidate *onvif.Candidate) bool {
	if !motionRecordingEnabled(cam) || eventCandidate == nil {
		return false
	}
	switch motionEventSource(cam) {
	case constant.MotionEventSourceONVIF, constant.MotionEventSourceAuto:
		return true
	default:
		return false
	}
}

func motionEventSourceUsesFrameDiff(cam constant.Camera, now time.Time) bool {
	if !motionRecordingEnabled(cam) {
		return false
	}
	switch motionEventSource(cam) {
	case constant.MotionEventSourceONVIF:
		return false
	case constant.MotionEventSourceAuto:
		return !service.OnvifEventSourceUsable(cam.ID, now)
	default:
		return true
	}
}

func FrameDiffMotionDetectionEnabled(cam constant.Camera) bool {
	return motionRecordingFrameDiffDetectionEnabled(cam) || motionMarkerFrameDiffDetectionEnabled(cam)
}

func motionRecordingFrameDiffDetectionEnabled(cam constant.Camera) bool {
	return motionRecordingEnabled(cam) && motionEventSource(cam) != constant.MotionEventSourceONVIF
}

func motionMarkerFrameDiffDetectionEnabled(cam constant.Camera) bool {
	return motionMarkingEnabled(cam) && motionMarkEventSource(cam) != constant.MotionEventSourceONVIF
}

func statusModeForCamera(cam constant.Camera) string {
	switch {
	case cam.Mode == "timelapse":
		return service.ModeTimelapse
	case motionRecordingEnabled(cam):
		return service.ModeMotion
	default:
		return service.ModeNormal
	}
}

func motionRatioThreshold(cam constant.Camera) float64 {
	if cam.MotionDetectRatioThreshold > 0 && cam.MotionDetectRatioThreshold <= 1 {
		return cam.MotionDetectRatioThreshold
	}
	if cam.MotionDetectRatioThreshold > 1 {
		log.Printf("[%s] motionDetectRatioThreshold=%.4f 超出范围，已回退默认值 %.4f",
			cam.ID, cam.MotionDetectRatioThreshold, motionDetectRatioThreshold)
	}
	return motionDetectRatioThreshold
}

func markMotionDetected(camID string, at time.Time) {
	markMotionDetectedWithMetadata(camID, at, nil)
}

func markMotionDetectedWithStats(camID string, at time.Time, stats motionFrameStats) {
	markMotionDetectedWithMetadata(camID, at, map[string]string{
		"diff_pixels": strconv.Itoa(stats.DiffPixels),
		"diff_ratio":  strconv.FormatFloat(stats.DiffRatio, 'f', 6, 64),
		"diff_sum":    strconv.Itoa(stats.DiffSum),
	})
}

func markMotionDetectedWithMetadata(camID string, at time.Time, metadata map[string]string) {
	PublishDetectionEvent(DetectionEvent{
		CameraID: camID,
		Type:     EventTypeMotion,
		Source:   "builtin-motion",
		At:       at,
		Metadata: metadata,
	})
}

func resetMotionDetected(camID string) {
	ResetCameraDetectionEvents(camID)
}

func motionDetectedRecently(camID string, now time.Time) bool {
	return motionEventRecordAction.shouldRecord(camID, now)
}

type eventRecordAction struct {
	eventType   string
	idleTimeout time.Duration
}

var motionEventRecordAction = eventRecordAction{
	eventType:   EventTypeMotion,
	idleTimeout: motionRecordIdleTimeout,
}

func (action eventRecordAction) shouldRecord(camID string, now time.Time) bool {
	_, ok := RecentDetectionEvent(camID, action.eventType, now, action.idleTimeout)
	return ok
}

func startMotionTimeShiftFFmpeg(ctx context.Context, cam constant.Camera) (*exec.Cmd, error) {
	bufferDir := motionTimeShiftDir(cam.ID)
	if err := os.RemoveAll(bufferDir); err != nil {
		log.Printf("[%s] 清理动检 Time-Shift 缓存失败: %v", cam.ID, err)
	}
	if err := os.MkdirAll(bufferDir, 0755); err != nil {
		return nil, err
	}

	filePattern := filepath.Join(bufferDir, motionTimeShiftFilePrefix+"%Y%m%d_%H%M%S"+motionTimeShiftSegmentExt)
	safeRTSPURL := fmt.Sprintf("rtsp://%s:8554/%s", constant.DefaultGo2rtcHost, cam.ID)

	args := []string{
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-max_delay", "500000",
		"-reorder_queue_size", "1024",
		"-use_wallclock_as_timestamps", "1",
		"-i", safeRTSPURL,
		"-c:v", "copy",
		"-c:a", "aac",
		"-f", "segment",
		"-segment_time", formatSeconds(motionTimeShiftSegmentDuration),
		"-segment_format", "mpegts",
		"-reset_timestamps", "1",
		"-strftime", "1",
		filePattern,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	log.Printf("[%s] 动检 Time-Shift 大块缓存已启动: %s, segment=%s, keep=%d, preRecord=%s",
		cam.ID, bufferDir, motionTimeShiftSegmentDuration, motionTimeShiftSegmentCount, motionTimeShiftPreRecord)
	return cmd, nil
}

func newEventRecordSession(eventType string, detectedAt time.Time) *eventRecordSession {
	if eventType == "" {
		eventType = EventTypeMotion
	}
	return &eventRecordSession{
		EventType: eventType,
		StartTime: detectedAt.Add(-motionTimeShiftPreRecord),
	}
}

func finishEventRecordSession(ctx context.Context, cam constant.Camera, camDir string, session *eventRecordSession, endTime time.Time) {
	if session == nil {
		return
	}
	if session.EndTime.IsZero() {
		session.EndTime = endTime
	}
	if session.EndTime.Before(session.StartTime) {
		session.EndTime = session.StartTime
	}
	exportTimeShiftEvent(ctx, cam, camDir, *session)
}

func exportTimeShiftEvent(ctx context.Context, cam constant.Camera, camDir string, session eventRecordSession) {
	if !session.EndTime.After(session.StartTime) {
		log.Printf("[%s] 动检事件时长不足，跳过裁剪: start=%s end=%s", cam.ID, session.StartTime, session.EndTime)
		return
	}

	clips, err := motionTimeShiftClips(cam.ID, session.StartTime, session.EndTime, time.Now())
	if err != nil {
		log.Printf("[%s] 收集动检 Time-Shift 大块失败: %v", cam.ID, err)
		return
	}
	if len(clips) == 0 {
		log.Printf("[%s] 未找到覆盖动检事件的 Time-Shift 大块: start=%s end=%s",
			cam.ID, session.StartTime, session.EndTime)
		return
	}

	dateDir := filepath.Join(camDir, session.StartTime.Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		log.Printf("[%s] 创建动检录像目录失败: %v", cam.ID, err)
		return
	}

	outputPath := filepath.Join(dateDir, fmt.Sprintf("%s_%s_%s_motion.mp4",
		cam.ID,
		session.StartTime.Format("20060102_150405"),
		session.EndTime.Format("150405")))
	tempOutput := outputPath + ".tmp.mp4"
	defer os.Remove(tempOutput)

	tempDir, err := os.MkdirTemp("", "camkeep-motion-trim-*")
	if err != nil {
		log.Printf("[%s] 创建动检裁剪临时目录失败: %v", cam.ID, err)
		return
	}
	defer os.RemoveAll(tempDir)

	clips, err = prepareMotionTimeShiftClips(clips)
	if err != nil {
		log.Printf("[%s] 预处理动检 Time-Shift 片段失败: %v", cam.ID, err)
		return
	}

	isHEVC, err := probeMotionTimeShiftClipsHEVC(ctx, clips)
	if err != nil {
		log.Printf("[%s] 探测动检片段编码失败，按 H.264 路径继续: %v", cam.ID, err)
	}

	parts := make([]string, 0, len(clips))
	for i, clip := range clips {
		partPath := filepath.Join(tempDir, fmt.Sprintf("part_%03d.ts", i))
		if err := trimMotionTimeShiftClip(ctx, clip, partPath, isHEVC); err != nil {
			log.Printf("[%s] 动检事件裁剪到 TS 片段失败: %v", cam.ID, err)
			return
		}
		parts = append(parts, partPath)
	}
	if err := concatMotionTimeShiftParts(ctx, parts, tempOutput, isHEVC); err != nil {
		log.Printf("[%s] 动检事件 TS 片段封装为 MP4 失败: %v", cam.ID, err)
		return
	}

	if err := os.Rename(tempOutput, outputPath); err != nil {
		log.Printf("[%s] 保存动检事件录像失败: %v", cam.ID, err)
		return
	}
	log.Printf("[%s] 动检事件已裁剪落盘: %s - %s -> %s",
		cam.ID, session.StartTime.Format(time.RFC3339), session.EndTime.Format(time.RFC3339), outputPath)
}

func trimMotionTimeShiftClip(ctx context.Context, clip motionTimeShiftClip, outputPath string, isHEVC bool) error {
	seek := clip.start.Sub(clip.source.start)
	if seek < 0 {
		seek = 0
	}
	duration := clip.end.Sub(clip.start)
	if duration <= 0 {
		return fmt.Errorf("无效裁剪时长: %s", duration)
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", formatSeconds(seek),
		"-i", clip.source.path,
		"-t", formatSeconds(duration),
		"-map", "0:v:0", "-map", "0:a?",
		"-c:v", "copy",
		"-c:a", "copy",
		"-avoid_negative_ts", "make_zero",
	}

	if !strings.HasSuffix(strings.ToLower(outputPath), ".ts") && isHEVC {
		args = append(args, "-tag:v", "hvc1")
	}

	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 裁剪失败: %v, output=%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func prepareMotionTimeShiftClips(clips []motionTimeShiftClip) ([]motionTimeShiftClip, error) {
	prepared := make([]motionTimeShiftClip, 0, len(clips))
	for _, clip := range clips {
		if !clip.end.After(clip.start) {
			log.Printf("警告: 动检片段时长不足，忽略此段: source=%s start=%s end=%s",
				clip.source.path, clip.start.Format(time.RFC3339), clip.end.Format(time.RFC3339))
			continue
		}
		prepared = append(prepared, clip)
	}

	if len(prepared) == 0 {
		return nil, fmt.Errorf("所有动检片段时长均无效")
	}

	return prepared, nil
}

func probeMotionTimeShiftClipsHEVC(ctx context.Context, clips []motionTimeShiftClip) (bool, error) {
	if len(clips) == 0 {
		return false, fmt.Errorf("无可探测的动检片段")
	}
	codec, err := probeVideoCodecName(ctx, clips[0].source.path)
	if err != nil {
		return false, err
	}
	return isHEVCCodec(codec), nil
}

func concatMotionTimeShiftParts(ctx context.Context, parts []string, outputPath string, isHEVC bool) error {
	listPath, err := writeConcatList(parts)
	if err != nil {
		return err
	}
	defer os.Remove(listPath)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c:v", "copy",
		"-c:a", "copy",
		"-bsf:a", "aac_adtstoasc", // 从 TS 转回 MP4 时，修复音频 ADTS 头部
		"-movflags", "+faststart", // 让生成的 MP4 支持网页秒开和拖拽
	}
	if isHEVC {
		args = append(args, "-tag:v", "hvc1")
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 拼接裁剪片段失败: %v, output=%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func motionTimeShiftClips(camID string, start, end, now time.Time) ([]motionTimeShiftClip, error) {
	segments, err := motionTimeShiftSegments(camID, now)
	if err != nil {
		return nil, err
	}

	var clips []motionTimeShiftClip
	for _, segment := range segments {
		if !segment.end.After(start) || !segment.start.Before(end) {
			continue
		}
		clipStart := maxTime(start, segment.start)
		clipEnd := minTime(end, segment.end)
		if clipEnd.After(clipStart) {
			clips = append(clips, motionTimeShiftClip{
				source: segment,
				start:  clipStart,
				end:    clipEnd,
			})
		}
	}
	return clips, nil
}

func motionTimeShiftSegments(camID string, now time.Time) ([]motionTimeShiftSegment, error) {
	bufferDir := motionTimeShiftDir(camID)
	entries, err := os.ReadDir(bufferDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	segments := make([]motionTimeShiftSegment, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != motionTimeShiftSegmentExt {
			continue
		}
		start, ok := parseMotionTimeShiftSegmentStart(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(bufferDir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		segments = append(segments, motionTimeShiftSegment{
			path:  path,
			start: start,
			end:   start.Add(motionTimeShiftSegmentDuration),
		})
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].start.Before(segments[j].start)
	})
	for i := range segments {
		if i+1 < len(segments) && segments[i+1].start.After(segments[i].start) {
			segments[i].end = segments[i+1].start
			continue
		}
		if now.After(segments[i].start) && now.Before(segments[i].end) {
			segments[i].end = now
			segments[i].live = true
		}
	}
	return segments, nil
}

func pruneMotionTimeShiftSegments(camID string, protectAfter time.Time) {
	segments, err := motionTimeShiftSegments(camID, time.Now())
	if err != nil {
		log.Printf("[%s] 清理动检 Time-Shift 缓存失败: %v", camID, err)
		return
	}
	if len(segments) <= motionTimeShiftSegmentCount {
		return
	}

	keepFrom := len(segments) - motionTimeShiftSegmentCount
	for i, segment := range segments {
		if i >= keepFrom {
			continue
		}
		if !protectAfter.IsZero() && segment.end.After(protectAfter) {
			continue
		}
		if err := os.Remove(segment.path); err != nil {
			log.Printf("[%s] 删除过期动检 Time-Shift 缓存失败: %s, err=%v", camID, segment.path, err)
		}
	}
}

func motionTimeShiftDir(camID string) string {
	base := filepath.Join(os.TempDir(), motionTimeShiftBufferBaseName)
	if info, err := os.Stat("/dev/shm"); err == nil && info.IsDir() {
		base = filepath.Join("/dev/shm", motionTimeShiftBufferBaseName)
	}
	return filepath.Join(base, camID)
}

func parseMotionTimeShiftSegmentStart(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, motionTimeShiftFilePrefix) || filepath.Ext(name) != motionTimeShiftSegmentExt {
		return time.Time{}, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, motionTimeShiftFilePrefix), motionTimeShiftSegmentExt)
	start, err := time.ParseInLocation(motionTimeShiftTimeLayout, raw, time.Local)
	return start, err == nil
}

func formatSeconds(duration time.Duration) string {
	seconds := duration.Seconds()
	if seconds == float64(int64(seconds)) {
		return fmt.Sprintf("%.0f", seconds)
	}
	return fmt.Sprintf("%.3f", seconds)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
