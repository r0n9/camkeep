package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/r0n9/camkeep/constant"
)

func TestAppendMotionMarkerSplitsAndReadsByLocalDay(t *testing.T) {
	t.Chdir(t.TempDir())

	start := time.Date(2026, 5, 12, 23, 59, 50, 0, time.Local)
	end := start.Add(20 * time.Second)
	if err := AppendMotionMarker(MotionMarker{
		CameraID: "cam1",
		Start:    start,
		End:      end,
		Source:   "auto_onvif",
		Topic:    "tns1:VideoSource/MotionAlarm",
		Score:    1,
	}); err != nil {
		t.Fatal(err)
	}

	dayOneMarkers, err := ReadMotionMarkers("cam1", start, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(dayOneMarkers) != 1 {
		t.Fatalf("expected one marker on first day, got %d", len(dayOneMarkers))
	}
	if !dayOneMarkers[0].Start.Equal(start) {
		t.Fatalf("expected first clip to start at %s, got %s", start, dayOneMarkers[0].Start)
	}
	if !dayOneMarkers[0].End.Equal(time.Date(2026, 5, 13, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("expected first clip to end at midnight, got %s", dayOneMarkers[0].End)
	}

	dayTwoMarkers, err := ReadMotionMarkers("cam1", end, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(dayTwoMarkers) != 1 {
		t.Fatalf("expected one marker on second day, got %d", len(dayTwoMarkers))
	}
	if !dayTwoMarkers[0].Start.Equal(time.Date(2026, 5, 13, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("expected second clip to start at midnight, got %s", dayTwoMarkers[0].Start)
	}
	if !dayTwoMarkers[0].End.Equal(end) {
		t.Fatalf("expected second clip to end at %s, got %s", end, dayTwoMarkers[0].End)
	}
}

func TestCleanCameraFilesCleansMarkersWhenNoRecordFiles(t *testing.T) {
	t.Chdir(t.TempDir())

	markerDir := MotionMarkerDir("cam1")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldMarker := filepath.Join(markerDir, time.Now().AddDate(0, 0, -10).Format("2006-01-02")+".jsonl")
	currentMarker := filepath.Join(markerDir, time.Now().Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(oldMarker, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentMarker, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cleanCameraFiles(constant.Camera{ID: "cam1", RetentionDays: 7})

	if _, err := os.Stat(oldMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected expired marker file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(currentMarker); err != nil {
		t.Fatalf("expected current marker file to remain, got %v", err)
	}
}
