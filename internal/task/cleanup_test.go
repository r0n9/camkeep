package task

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/r0n9/camkeep/constant"
)

// writeFileWithSize 写一个指定大小的文件，并把修改时间设为 modTime。
// 清理任务按修改时间全局排序，只豁免最后一个（正在写入的）文件，所以测试必须精确控制时间。
func writeFileWithSize(t *testing.T, path string, size int, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

// TestCleanCameraFilesKeepsTimelapseSegments 锁住：延时录像片段不受 min_size_kb 约束。
// min_size_kb 的默认值(1MB)是按普通录像的流拷贝片段定的，延时片是重编码的小文件，
// 用固定 KB 阈值判断会误删正常成片。
func TestCleanCameraFilesKeepsTimelapseSegments(t *testing.T) {
	t.Chdir(t.TempDir())

	camID := "tl-cam"
	camDir := filepath.Join(constant.DefaultRecordBaseDir, camID)
	dateDir := filepath.Join(camDir, "2026-05-12")
	now := time.Now()

	// 一个远小于 min_size_kb 的延时片，且不是最新文件（拿不到"最后一个"的豁免）
	small := filepath.Join(dateDir, camID+"_20260512_090000_timelapse.mp4")
	newest := filepath.Join(dateDir, camID+"_20260512_100000_timelapse.mp4")
	writeFileWithSize(t, small, 4096, now.Add(-2*time.Hour))
	writeFileWithSize(t, newest, 4096, now.Add(-1*time.Hour))

	cleanCameraFiles(constant.Camera{ID: camID, Mode: "timelapse", MinSizeKb: 1024, RetentionDays: 7})

	if _, err := os.Stat(small); err != nil {
		t.Fatalf("延时片不应被当成过小碎片删除: %v", err)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("最新的延时片不应被删除: %v", err)
	}
}

// TestCleanCameraFilesKeepsCoverImage 锁住：camDir 下的封面等非录像文件不受 min_size_kb 约束。
// 封面通常只有几十 KB，会被默认 1MB 的阈值当成碎片删掉。
func TestCleanCameraFilesKeepsCoverImage(t *testing.T) {
	t.Chdir(t.TempDir())

	camID := "normal-cam"
	camDir := filepath.Join(constant.DefaultRecordBaseDir, camID)
	dateDir := filepath.Join(camDir, "2026-05-12")
	now := time.Now()

	cover := filepath.Join(camDir, "cover.jpg")
	smallSegment := filepath.Join(dateDir, camID+"_20260512_090000_090500.mp4")
	newest := filepath.Join(dateDir, camID+"_20260512_100000_unknown.mp4")
	writeFileWithSize(t, cover, 8192, now.Add(-3*time.Hour))
	writeFileWithSize(t, smallSegment, 4096, now.Add(-2*time.Hour))
	writeFileWithSize(t, newest, 4096, now.Add(-1*time.Hour))

	cleanCameraFiles(constant.Camera{ID: camID, Mode: "normal", MinSizeKb: 1024, RetentionDays: 7})

	if _, err := os.Stat(cover); err != nil {
		t.Fatalf("封面不是录像文件，不应被当成过小碎片删除: %v", err)
	}
	// 普通录像的过小碎片仍然要照常清理，避免这次修复顺手放宽了原有行为
	if _, err := os.Stat(smallSegment); !os.IsNotExist(err) {
		t.Fatalf("普通模式的过小录像碎片仍应被删除, err=%v", err)
	}
}

// TestCleanCameraFilesRespectsRetentionForTimelapse 延时片跳过体积检查，但仍受保留天数约束。
func TestCleanCameraFilesRespectsRetentionForTimelapse(t *testing.T) {
	t.Chdir(t.TempDir())

	camID := "tl-cam"
	camDir := filepath.Join(constant.DefaultRecordBaseDir, camID)
	dateDir := filepath.Join(camDir, "2026-05-01")
	now := time.Now()

	expired := filepath.Join(dateDir, camID+"_20260501_090000_timelapse.mp4")
	fresh := filepath.Join(dateDir, camID+"_20260512_090000_timelapse.mp4")
	writeFileWithSize(t, expired, 4096, now.AddDate(0, 0, -10))
	writeFileWithSize(t, fresh, 4096, now.Add(-1*time.Hour))

	cleanCameraFiles(constant.Camera{ID: camID, Mode: "timelapse", MinSizeKb: 1024, RetentionDays: 7})

	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("超过保留天数的延时片仍应被删除, err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("未过期的延时片不应被删除: %v", err)
	}
}

func TestIsTimelapseMode(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{mode: "timelapse", want: true},
		{mode: " timelapse ", want: true},
		{mode: "Timelapse", want: true}, // 手改配置文件时的大小写差异
		{mode: "normal", want: false},
		{mode: "", want: false},
	}
	for _, tc := range cases {
		if got := isTimelapseMode(constant.Camera{Mode: tc.mode}); got != tc.want {
			t.Errorf("isTimelapseMode(%q)=%v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestIsRecordVideoFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "records/cam/2026-05-12/cam_20260512_090000_090500.mp4", want: true},
		{path: "records/cam/2026-05-12/cam_20260512_090000_090500.ts", want: true},
		{path: "records/cam/2026-05-12/cam_20260512_090000_timelapse.MP4", want: true},
		{path: "records/cam/cover.jpg", want: false},
		{path: "records/cam/.markers/2026-05-12.jsonl", want: false},
	}
	for _, tc := range cases {
		if got := isRecordVideoFile(tc.path); got != tc.want {
			t.Errorf("isRecordVideoFile(%q)=%v, want %v", tc.path, got, tc.want)
		}
	}
}
