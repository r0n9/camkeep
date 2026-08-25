package task

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseUnknownSegmentStart(t *testing.T) {
	wantStart := time.Date(2026, 5, 12, 9, 0, 0, 0, time.Local)

	cases := []struct {
		name     string
		camID    string
		fileName string
		ok       bool
	}{
		{name: "simple id", camID: "cam1", fileName: "cam1_20260512_090000_unknown.mp4", ok: true},
		{name: "underscore id", camID: "front_door", fileName: "front_door_20260512_090000_unknown.mp4", ok: true},
		{name: "multi underscore id", camID: "front_door_2f", fileName: "front_door_2f_20260512_090000_unknown.ts", ok: true},
		{name: "prefix mismatch", camID: "cam1", fileName: "cam2_20260512_090000_unknown.mp4", ok: false},
		{name: "completed segment", camID: "cam1", fileName: "cam1_20260512_090000_091000.mp4", ok: false},
		{name: "temp file", camID: "cam1", fileName: "cam1_20260512_090000_unknown.tmp.mp4", ok: false},
		{name: "invalid time", camID: "cam1", fileName: "cam1_20269999_090000_unknown.mp4", ok: false},
		{name: "too few parts", camID: "cam1", fileName: "cam1_unknown.mp4", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, ok := parseUnknownSegmentStart(tc.camID, tc.fileName)
			if ok != tc.ok {
				t.Fatalf("parseUnknownSegmentStart(%q, %q) ok=%v, want %v", tc.camID, tc.fileName, ok, tc.ok)
			}
			if ok && !start.Equal(wantStart) {
				t.Fatalf("parseUnknownSegmentStart(%q, %q) start=%s, want %s", tc.camID, tc.fileName, start, wantStart)
			}
		})
	}
}

func TestRenameSegmentsInDirSkipsRecentSegments(t *testing.T) {
	dir := t.TempDir()
	// 段开始于 09:55，segDur=600s，宽限 10s：10:00 时仍可能在写入，不能重命名。
	fileName := "front_door_20260512_095500_unknown.mp4"
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	renameSegmentsInDir(context.Background(), "front_door", dir, 600*time.Second, now)

	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatalf("expected recent unknown segment to be left untouched: %v", err)
	}
}

func TestNewTimelapsePlan(t *testing.T) {
	cases := []struct {
		name            string
		captureInterval int
		segmentDuration int
		wantSamplingFPS string
		wantInterval    float64
		wantSpeedup     int
		wantFrames      int
		wantSegmentSecs float64
		wantFilter      string
		wantDegraded    bool
	}{
		{
			name: "5s 间隔按 1s 采样", captureInterval: 5, segmentDuration: 300,
			wantSamplingFPS: "1/1", wantInterval: 1, wantSpeedup: 25,
			wantFrames: 300, wantSegmentSecs: 12,
			wantFilter: "fps=1/1,settb=1/1000,setpts=N/(25*TB)",
		},
		{
			name: "60s 间隔按 12s 采样", captureInterval: 60, segmentDuration: 300,
			wantSamplingFPS: "1/12", wantInterval: 12, wantSpeedup: 300,
			wantFrames: 25, wantSegmentSecs: 1,
			wantFilter: "fps=1/12,settb=1/1000,setpts=N/(25*TB)",
		},
		{
			name: "1s 间隔按 0.2s 采样", captureInterval: 1, segmentDuration: 600,
			wantSamplingFPS: "5/1", wantInterval: 0.2, wantSpeedup: 5,
			wantFrames: 3000, wantSegmentSecs: 120,
			wantFilter: "fps=5/1,settb=1/1000,setpts=N/(25*TB)",
		},
		{
			// 7 不能整除：四舍五入到 214 帧，覆盖 214*1.4=299.6s，几乎不丢时间；
			// 历史的整除实现只有 42 帧、覆盖 294s。
			name: "无法整除时四舍五入", captureInterval: 7, segmentDuration: 300,
			wantSamplingFPS: "5/7", wantInterval: 1.4, wantSpeedup: 35,
			wantFrames: 214, wantSegmentSecs: 8.56,
			wantFilter: "fps=5/7,settb=1/1000,setpts=N/(25*TB)",
		},
		{
			name: "非法间隔归一化为 1", captureInterval: 0, segmentDuration: 600,
			wantSamplingFPS: "5/1", wantInterval: 0.2, wantSpeedup: 5,
			wantFrames: 3000, wantSegmentSecs: 120,
			wantFilter: "fps=5/1,settb=1/1000,setpts=N/(25*TB)",
		},
		{
			// capture_interval 远大于 segment_duration：一个分片装不下一帧，走兜底
			name: "矛盾配置走兜底", captureInterval: 6000, segmentDuration: 300,
			wantSamplingFPS: "1/1200", wantInterval: 1200, wantSpeedup: 30000,
			wantFrames: 125, wantSegmentSecs: 5,
			wantFilter:   "fps=1/1200,settb=1/1000,setpts=N/(25*TB)",
			wantDegraded: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := newTimelapsePlan(tc.captureInterval, tc.segmentDuration)

			if plan.SamplingFPS != tc.wantSamplingFPS {
				t.Errorf("SamplingFPS=%q, want %q", plan.SamplingFPS, tc.wantSamplingFPS)
			}
			if math.Abs(plan.SamplingInterval-tc.wantInterval) > 1e-9 {
				t.Errorf("SamplingInterval=%v, want %v", plan.SamplingInterval, tc.wantInterval)
			}
			if plan.SpeedupFactor != tc.wantSpeedup {
				t.Errorf("SpeedupFactor=%d, want %d", plan.SpeedupFactor, tc.wantSpeedup)
			}
			if plan.FramesPerSegment != tc.wantFrames {
				t.Errorf("FramesPerSegment=%d, want %d", plan.FramesPerSegment, tc.wantFrames)
			}
			if math.Abs(plan.SegmentSeconds-tc.wantSegmentSecs) > 1e-9 {
				t.Errorf("SegmentSeconds=%v, want %v", plan.SegmentSeconds, tc.wantSegmentSecs)
			}
			if plan.VideoFilter != tc.wantFilter {
				t.Errorf("VideoFilter=%q, want %q", plan.VideoFilter, tc.wantFilter)
			}
			if plan.SegmentDegraded != tc.wantDegraded {
				t.Errorf("SegmentDegraded=%v, want %v", plan.SegmentDegraded, tc.wantDegraded)
			}
			// 逻辑播放帧率等于输出帧率，关键帧必须正好落在分片边界上
			if plan.GopSize != plan.FramesPerSegment {
				t.Errorf("GopSize=%d, want %d (= FramesPerSegment)", plan.GopSize, plan.FramesPerSegment)
			}
		})
	}
}

// TestTimelapsePlanPreservesLegacySpeedAndSegmentLength 锁住这次改动的核心不变量：
// 加速倍率和分片播放时长必须与历史实现(采样间隔=capture_interval、逻辑帧率=5)一致，
// 升级后用户看到的成片速度和长度不变，只有画面密度提高。
func TestTimelapsePlanPreservesLegacySpeedAndSegmentLength(t *testing.T) {
	for _, captureInterval := range []int{1, 2, 5, 10, 30, 60, 300} {
		for _, segmentDuration := range []int{300, 600, 3600} {
			plan := newTimelapsePlan(captureInterval, segmentDuration)

			// 历史行为: 倍率 = 5 * capture_interval
			if want := captureInterval * 5; plan.SpeedupFactor != want {
				t.Errorf("ci=%d seg=%d: SpeedupFactor=%d, want %d",
					captureInterval, segmentDuration, plan.SpeedupFactor, want)
			}
			// 历史行为: 分片播放时长 = (segment_duration / capture_interval) / 5
			wantSegmentSecs := float64(segmentDuration/captureInterval) / 5
			if math.Abs(plan.SegmentSeconds-wantSegmentSecs) > 0.5 {
				t.Errorf("ci=%d seg=%d: SegmentSeconds=%v, want ~%v",
					captureInterval, segmentDuration, plan.SegmentSeconds, wantSegmentSecs)
			}
			// 采样间隔必须是 capture_interval 的 1/5，这是顺滑度的来源
			if want := float64(captureInterval) / 5; math.Abs(plan.SamplingInterval-want) > 1e-9 {
				t.Errorf("ci=%d: SamplingInterval=%v, want %v", captureInterval, plan.SamplingInterval, want)
			}
		}
	}
}
