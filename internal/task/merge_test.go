package task

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/r0n9/camkeep/constant"
)

func TestSkipDailyMergeOnlyForTimelapse(t *testing.T) {
	if !skipDailyMerge(constant.Camera{Mode: "timelapse"}) {
		t.Fatal("expected timelapse camera to skip daily merge")
	}
	if !skipDailyMerge(constant.Camera{Mode: " TIMELAPSE "}) {
		t.Fatal("expected timelapse mode check to ignore case and spaces")
	}
	if skipDailyMerge(constant.Camera{Mode: "normal"}) {
		t.Fatal("expected normal camera to run daily merge")
	}
	if skipDailyMerge(constant.Camera{}) {
		t.Fatal("expected empty mode to run daily merge as normal")
	}
}

func TestMergeFragmentsIncludesNormalAndMotionFiles(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_2026-05-12_09-00-00.ts")
	createMergeTestFile(t, dir, "2026-05-12_090500_motion.mp4")
	createMergeTestFile(t, dir, "cam1_2026-05-12_09-10-00.mp4")
	createMergeTestFile(t, dir, "manual.mp4")
	createMergeTestFile(t, dir, "cam1_2026-05-12_merged.mp4")
	createMergeTestFile(t, dir, "cam1_2026-05-12_09-15-00.tmp.mp4")
	createMergeTestFile(t, dir, "note.txt")
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}

	fragments, err := mergeFragments(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := mergeTestBaseNames(fragments)
	want := []string{
		"cam1_2026-05-12_09-00-00.ts",
		"2026-05-12_090500_motion.mp4",
		"cam1_2026-05-12_09-10-00.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSortMergeFragmentsUsesEmbeddedStartTime(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_2026-05-12_09-10-00.ts",
		"/records/cam1/2026-05-12/2026-05-12_090500_motion.mp4",
		"/records/cam1/2026-05-12/cam1_2026-05-12_09-00-00.ts",
	}

	sortMergeFragments(fragments)

	got := mergeTestBaseNames(fragments)
	want := []string{
		"cam1_2026-05-12_09-00-00.ts",
		"2026-05-12_090500_motion.mp4",
		"cam1_2026-05-12_09-10-00.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestGroupMergeFragmentsByHour(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_2026-05-12_10-00-00.ts",
		"/records/cam1/2026-05-12/cam1_2026-05-12_09-10-00.ts",
		"/records/cam1/2026-05-12/2026-05-12_095500_motion.mp4",
		"/records/cam1/2026-05-12/cam1_2026-05-12_10-05-00.ts",
	}

	groups := groupMergeFragmentsByHour(fragments)
	if len(groups) != 3 {
		t.Fatalf("expected 3 hourly groups, got %d", len(groups))
	}

	if groups[0].hourKey != "2026-05-12_09" || groups[0].kind != "motion" {
		t.Fatalf("expected first group 2026-05-12_09 motion, got %s %s", groups[0].hourKey, groups[0].kind)
	}
	if got := mergeTestBaseNames(groups[0].fragments); !reflect.DeepEqual(got, []string{
		"2026-05-12_095500_motion.mp4",
	}) {
		t.Fatalf("unexpected first group fragments: %v", got)
	}

	if groups[1].hourKey != "2026-05-12_09" || groups[1].kind != "normal" {
		t.Fatalf("expected second group 2026-05-12_09 normal, got %s %s", groups[1].hourKey, groups[1].kind)
	}
	if got := mergeTestBaseNames(groups[1].fragments); !reflect.DeepEqual(got, []string{
		"cam1_2026-05-12_09-10-00.ts",
	}) {
		t.Fatalf("unexpected second group fragments: %v", got)
	}

	if groups[2].hourKey != "2026-05-12_10" || groups[2].kind != "normal" {
		t.Fatalf("expected third group 2026-05-12_10 normal, got %s %s", groups[2].hourKey, groups[2].kind)
	}
	if got := mergeTestBaseNames(groups[2].fragments); !reflect.DeepEqual(got, []string{
		"cam1_2026-05-12_10-00-00.ts",
		"cam1_2026-05-12_10-05-00.ts",
	}) {
		t.Fatalf("unexpected third group fragments: %v", got)
	}
}

func TestGroupMergeFragmentsByHourSeparatesNormalAndMotion(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_2026-05-12_09-20-00.ts",
		"/records/cam1/2026-05-12/2026-05-12_092500_motion.mp4",
		"/records/cam1/2026-05-12/cam1_2026-05-12_09-30-00.ts",
	}

	groups := groupMergeFragmentsByHour(fragments)
	if len(groups) != 2 {
		t.Fatalf("expected 2 hourly groups, got %d", len(groups))
	}
	if groups[0].kind != "motion" || groups[0].outputNameSuffix() != "_motion" {
		t.Fatalf("expected first group to be motion, got kind=%q suffix=%q", groups[0].kind, groups[0].outputNameSuffix())
	}
	if got := mergeTestBaseNames(groups[0].fragments); !reflect.DeepEqual(got, []string{"2026-05-12_092500_motion.mp4"}) {
		t.Fatalf("unexpected motion fragments: %v", got)
	}
	if groups[1].kind != "normal" || groups[1].outputNameSuffix() != "" {
		t.Fatalf("expected second group to be normal, got kind=%q suffix=%q", groups[1].kind, groups[1].outputNameSuffix())
	}
	if got := mergeTestBaseNames(groups[1].fragments); !reflect.DeepEqual(got, []string{
		"cam1_2026-05-12_09-20-00.ts",
		"cam1_2026-05-12_09-30-00.ts",
	}) {
		t.Fatalf("unexpected normal fragments: %v", got)
	}
}

func TestMergeFragmentStartTimeParsesNormalAndMotionNames(t *testing.T) {
	normal, ok := mergeFragmentStartTime("cam1_2026-05-12_09-10-00.ts")
	if !ok {
		t.Fatal("expected normal segment start time to parse")
	}
	wantNormal := time.Date(2026, 5, 12, 9, 10, 0, 0, time.Local)
	if !normal.Equal(wantNormal) {
		t.Fatalf("expected %s, got %s", wantNormal, normal)
	}

	motion, ok := mergeFragmentStartTime("2026-05-12_091025_motion.mp4")
	if !ok {
		t.Fatal("expected motion recording start time to parse")
	}
	wantMotion := time.Date(2026, 5, 12, 9, 10, 25, 0, time.Local)
	if !motion.Equal(wantMotion) {
		t.Fatalf("expected %s, got %s", wantMotion, motion)
	}
}

func TestValidateMergedDurationAllowsSingleFragment(t *testing.T) {
	fragments := []string{"source.mp4"}
	probe := func(_ context.Context, path string) (time.Duration, error) {
		switch path {
		case "source.mp4", "merged.mp4":
			return 10 * time.Second, nil
		default:
			return 0, os.ErrNotExist
		}
	}

	if err := validateMergedDurationWithProbe(context.Background(), fragments, "merged.mp4", probe); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMergedDurationRejectsShortOutput(t *testing.T) {
	fragments := []string{"source-a.mp4", "source-b.mp4"}
	probe := func(_ context.Context, path string) (time.Duration, error) {
		switch path {
		case "merged.mp4":
			return 5 * time.Second, nil
		case "source-a.mp4", "source-b.mp4":
			return 10 * time.Second, nil
		default:
			return 0, os.ErrNotExist
		}
	}

	if err := validateMergedDurationWithProbe(context.Background(), fragments, "merged.mp4", probe); err == nil {
		t.Fatal("expected short merged output to fail validation")
	}
}

func TestIsCorruptFragmentFFmpegOutput(t *testing.T) {
	output := `[h264 @ 0x1546049f0] Invalid NAL unit size (2277 > 986).
[h264 @ 0x1546049f0] missing picture in access unit with size 990
[concat @ 0x154705d10] h264_mp4toannexb filter failed to receive output packet
[in#0/concat @ 0x600000a18300] Error during demuxing: Invalid data found when processing input`
	if !isCorruptFragmentFFmpegOutput(output) {
		t.Fatal("expected corrupt fragment output to be detected")
	}
	if isCorruptFragmentFFmpegOutput("Non-monotonous DTS in output stream") {
		t.Fatal("expected unrelated ffmpeg warning to stay non-corrupt")
	}
}

func createMergeTestFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("fragment"), 0644); err != nil {
		t.Fatal(err)
	}
}

func mergeTestBaseNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}
