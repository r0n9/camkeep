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

func TestMergeFragmentsSkipsMotionFilesByDefault(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500.ts")
	createMergeTestFile(t, dir, "cam1_20260512_090500_090800_motion.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_091000_091500.mp4")
	createMergeTestFile(t, dir, "manual.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_090000_normal_merged.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_091500_unknown.tmp.mp4")
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
		"cam1_20260512_090000_090500.ts",
		"cam1_20260512_091000_091500.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestScanMergeFragmentsIncludesMotionFilesWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500.ts")
	createMergeTestFile(t, dir, "cam1_20260512_090500_090800_motion.mp4")

	scanResult, err := scanMergeFragments(dir, true)
	if err != nil {
		t.Fatal(err)
	}

	got := mergeTestBaseNames(scanResult.fragments)
	want := []string{
		"cam1_20260512_090000_090500.ts",
		"cam1_20260512_090500_090800_motion.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if scanResult.skippedMotion != 0 {
		t.Fatalf("expected no skipped motion files when enabled, got %d", scanResult.skippedMotion)
	}
}

func TestScanMergeFragmentsCountsSkippedMotionFiles(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500.ts")
	createMergeTestFile(t, dir, "cam1_20260512_090500_090800_motion.mp4")

	scanResult, err := scanMergeFragments(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if scanResult.skippedMotion != 1 {
		t.Fatalf("expected one skipped motion file, got %d", scanResult.skippedMotion)
	}
	if got := mergeTestBaseNames(scanResult.fragments); !reflect.DeepEqual(got, []string{"cam1_20260512_090000_090500.ts"}) {
		t.Fatalf("unexpected selected fragments: %v", got)
	}
}

func TestScanMergeFragmentsCollectsUnknownForSingleFileRepair(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_unknown.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_090500_091000.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_091500_unknown.tmp.mp4")
	createMergeTestFile(t, dir, "manual_unknown.mp4")

	scanResult, err := scanMergeFragments(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := mergeTestBaseNames(scanResult.fragments); !reflect.DeepEqual(got, []string{"cam1_20260512_090500_091000.mp4"}) {
		t.Fatalf("unexpected merge fragments: %v", got)
	}
	if got := mergeTestBaseNames(scanResult.singleFileRepairs); !reflect.DeepEqual(got, []string{"cam1_20260512_090000_unknown.mp4"}) {
		t.Fatalf("unexpected single file repairs: %v", got)
	}
	if scanResult.skippedTemp != 1 {
		t.Fatalf("expected one temp file, got %d", scanResult.skippedTemp)
	}
	if scanResult.skippedNoTime != 1 {
		t.Fatalf("expected one unknown file without start time, got %d", scanResult.skippedNoTime)
	}
}

func TestScanMergeFragmentsSkipsRepairedOutputs(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500_repaired.mp4")
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500_repaired_2.mp4")
	createMergeTestFile(t, dir, "cam1_repaired_20260512_090500_091000.mp4")

	scanResult, err := scanMergeFragments(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := mergeTestBaseNames(scanResult.fragments); !reflect.DeepEqual(got, []string{"cam1_repaired_20260512_090500_091000.mp4"}) {
		t.Fatalf("unexpected merge fragments: %v", got)
	}
	if scanResult.skippedRepaired != 2 {
		t.Fatalf("expected two repaired files to be skipped, got %d", scanResult.skippedRepaired)
	}
}

func TestSortMergeFragmentsUsesEmbeddedStartTime(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_20260512_091000_091500.ts",
		"/records/cam1/2026-05-12/cam1_20260512_090500_090800_motion.mp4",
		"/records/cam1/2026-05-12/cam1_20260512_090000_090500.ts",
	}

	sortMergeFragments(fragments)

	got := mergeTestBaseNames(fragments)
	want := []string{
		"cam1_20260512_090000_090500.ts",
		"cam1_20260512_090500_090800_motion.mp4",
		"cam1_20260512_091000_091500.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestGroupMergeFragmentsByHour(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_20260512_100000_100500.ts",
		"/records/cam1/2026-05-12/cam1_20260512_091000_091500.ts",
		"/records/cam1/2026-05-12/cam1_20260512_095500_095800_motion.mp4",
		"/records/cam1/2026-05-12/cam1_20260512_100500_101000.ts",
	}

	groups := groupMergeFragmentsByHour(fragments)
	if len(groups) != 3 {
		t.Fatalf("expected 3 hourly groups, got %d", len(groups))
	}

	if groups[0].hourKey != "20260512_090000" || groups[0].kind != "motion" {
		t.Fatalf("expected first group 20260512_090000 motion, got %s %s", groups[0].hourKey, groups[0].kind)
	}
	if got := mergeTestBaseNames(groups[0].fragments); !reflect.DeepEqual(got, []string{
		"cam1_20260512_095500_095800_motion.mp4",
	}) {
		t.Fatalf("unexpected first group fragments: %v", got)
	}

	if groups[1].hourKey != "20260512_090000" || groups[1].kind != "normal" {
		t.Fatalf("expected second group 20260512_090000 normal, got %s %s", groups[1].hourKey, groups[1].kind)
	}
	if got := mergeTestBaseNames(groups[1].fragments); !reflect.DeepEqual(got, []string{
		"cam1_20260512_091000_091500.ts",
	}) {
		t.Fatalf("unexpected second group fragments: %v", got)
	}

	if groups[2].hourKey != "20260512_100000" || groups[2].kind != "normal" {
		t.Fatalf("expected third group 20260512_100000 normal, got %s %s", groups[2].hourKey, groups[2].kind)
	}
	if got := mergeTestBaseNames(groups[2].fragments); !reflect.DeepEqual(got, []string{
		"cam1_20260512_100000_100500.ts",
		"cam1_20260512_100500_101000.ts",
	}) {
		t.Fatalf("unexpected third group fragments: %v", got)
	}
	if !groups[2].rangeStart.Equal(time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)) ||
		!groups[2].rangeEnd.Equal(time.Date(2026, 5, 12, 10, 10, 0, 0, time.Local)) {
		t.Fatalf("expected third group range 10:00-10:10, got %s-%s", groups[2].rangeStart, groups[2].rangeEnd)
	}
}

func TestGroupMergeFragmentsByHourSeparatesNormalAndMotion(t *testing.T) {
	fragments := []string{
		"/records/cam1/2026-05-12/cam1_20260512_092000_092500.ts",
		"/records/cam1/2026-05-12/cam1_20260512_092500_092800_motion.mp4",
		"/records/cam1/2026-05-12/cam1_20260512_093000_093500.ts",
	}

	groups := groupMergeFragmentsByHour(fragments)
	if len(groups) != 3 {
		t.Fatalf("expected 3 hourly groups, got %d", len(groups))
	}
	if groups[0].kind != "motion" {
		t.Fatalf("expected first group to be motion, got kind=%q", groups[0].kind)
	}
	if got := mergeTestBaseNames(groups[0].fragments); !reflect.DeepEqual(got, []string{"cam1_20260512_092500_092800_motion.mp4"}) {
		t.Fatalf("unexpected motion fragments: %v", got)
	}
	if groups[1].kind != "normal" {
		t.Fatalf("expected second group to be normal, got kind=%q", groups[1].kind)
	}
	if got := mergeTestBaseNames(groups[1].fragments); !reflect.DeepEqual(got, []string{"cam1_20260512_092000_092500.ts"}) {
		t.Fatalf("unexpected first normal fragments: %v", got)
	}
	if groups[2].kind != "normal" {
		t.Fatalf("expected third group to be normal, got kind=%q", groups[2].kind)
	}
	if got := mergeTestBaseNames(groups[2].fragments); !reflect.DeepEqual(got, []string{"cam1_20260512_093000_093500.ts"}) {
		t.Fatalf("unexpected second normal fragments: %v", got)
	}
}

func TestMergeFragmentTimeRangeParsesStartAndEnd(t *testing.T) {
	start, end, ok := mergeFragmentTimeRange("cam1_20260512_235500_000000.mp4")
	if !ok {
		t.Fatal("expected normal segment range to parse")
	}
	wantStart := time.Date(2026, 5, 12, 23, 55, 0, 0, time.Local)
	wantEnd := time.Date(2026, 5, 13, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("expected %s-%s, got %s-%s", wantStart, wantEnd, start, end)
	}
}

func TestMergeOutputNameUsesNormalRange(t *testing.T) {
	group := mergeHourGroup{
		hourKey:    "20260512_090000",
		start:      time.Date(2026, 5, 12, 9, 0, 0, 0, time.Local),
		kind:       "normal",
		rangeStart: time.Date(2026, 5, 12, 9, 0, 0, 0, time.Local),
		rangeEnd:   time.Date(2026, 5, 12, 9, 10, 0, 0, time.Local),
	}
	if got := mergeOutputName("cam1", group, ".mp4"); got != "cam1_20260512_090000_091000_merged.mp4" {
		t.Fatalf("unexpected ranged merge output name: %s", got)
	}

	group.kind = "motion"
	if got := mergeOutputName("cam1", group, ".mp4"); got != "cam1_20260512_090000_motion_merged.mp4" {
		t.Fatalf("unexpected motion merge output name: %s", got)
	}
}

func TestRepairedFragmentOutputNameIncludesRangeAndSuffix(t *testing.T) {
	start := time.Date(2026, 5, 12, 9, 0, 0, 0, time.Local)

	if got := repairedFragmentOutputName("cam1", start, 5*time.Minute, 0); got != "cam1_20260512_090000_090500_repaired.mp4" {
		t.Fatalf("unexpected repaired output name: %s", got)
	}
	if got := repairedFragmentOutputName("cam1", start, 5*time.Minute, 1); got != "cam1_20260512_090000_090500_repaired_2.mp4" {
		t.Fatalf("unexpected repaired output name with index: %s", got)
	}
}

func TestNextRepairedFragmentPathAvoidsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	createMergeTestFile(t, dir, "cam1_20260512_090000_090500_repaired.mp4")
	start := time.Date(2026, 5, 12, 9, 0, 0, 0, time.Local)

	path, err := nextRepairedFragmentPath(dir, "cam1", start, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(path); got != "cam1_20260512_090000_090500_repaired_2.mp4" {
		t.Fatalf("unexpected repaired output path: %s", got)
	}
}

func TestMergeFragmentStartTimeParsesNormalAndMotionNames(t *testing.T) {
	normal, ok := mergeFragmentStartTime("cam1_20260512_091000_091500.ts")
	if !ok {
		t.Fatal("expected normal segment start time to parse")
	}
	wantNormal := time.Date(2026, 5, 12, 9, 10, 0, 0, time.Local)
	if !normal.Equal(wantNormal) {
		t.Fatalf("expected %s, got %s", wantNormal, normal)
	}

	motion, ok := mergeFragmentStartTime("cam1_20260512_091025_091500_motion.mp4")
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
