package task

import (
	"context"
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
