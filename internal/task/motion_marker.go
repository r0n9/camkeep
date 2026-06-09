package task

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
)

const motionMarkerDirName = ".markers"

var motionMarkerFileMux sync.Mutex

type MotionMarker struct {
	CameraID string    `json:"camera_id,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Source   string    `json:"source"`
	Topic    string    `json:"topic,omitempty"`
	Score    float64   `json:"score,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

func AppendMotionMarker(marker MotionMarker) error {
	marker.CameraID = normalizeMarkerText(marker.CameraID)
	if marker.CameraID == "" || !marker.End.After(marker.Start) {
		return nil
	}
	if marker.Source == "" {
		marker.Source = "unknown"
	}

	motionMarkerFileMux.Lock()
	defer motionMarkerFileMux.Unlock()

	for _, clip := range splitMotionMarkerByLocalDay(marker) {
		if err := appendMotionMarkerClip(clip); err != nil {
			return err
		}
	}
	return nil
}

func ReadMotionMarkers(cameraID string, startDay, endDay time.Time) ([]MotionMarker, error) {
	cameraID = normalizeMarkerText(cameraID)
	if cameraID == "" || startDay.IsZero() || endDay.IsZero() {
		return nil, nil
	}
	startDay = localDayStart(startDay)
	endDay = localDayStart(endDay)
	if endDay.Before(startDay) {
		return nil, nil
	}

	rangeStart := startDay
	rangeEnd := endDay.AddDate(0, 0, 1)
	var markers []MotionMarker

	motionMarkerFileMux.Lock()
	defer motionMarkerFileMux.Unlock()

	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		file, err := os.Open(motionMarkerFilePath(cameraID, day))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var marker MotionMarker
			if err := json.Unmarshal(scanner.Bytes(), &marker); err != nil {
				continue
			}
			if marker.CameraID == "" {
				marker.CameraID = cameraID
			}
			if marker.CameraID != cameraID || !marker.End.After(marker.Start) {
				continue
			}
			if marker.End.After(rangeStart) && marker.Start.Before(rangeEnd) {
				markers = append(markers, marker)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}

	sort.Slice(markers, func(i, j int) bool {
		if !markers[i].Start.Equal(markers[j].Start) {
			return markers[i].Start.Before(markers[j].Start)
		}
		return markers[i].End.Before(markers[j].End)
	})
	return markers, nil
}

func MotionMarkerDir(cameraID string) string {
	return filepath.Join(constant.DefaultRecordBaseDir, normalizeMarkerText(cameraID), motionMarkerDirName)
}

func appendMotionMarkerClip(marker MotionMarker) error {
	if !marker.End.After(marker.Start) {
		return nil
	}
	if err := os.MkdirAll(MotionMarkerDir(marker.CameraID), 0755); err != nil {
		return err
	}

	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	file, err := os.OpenFile(motionMarkerFilePath(marker.CameraID, marker.Start), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(data)
	return err
}

func splitMotionMarkerByLocalDay(marker MotionMarker) []MotionMarker {
	var clips []MotionMarker
	start := marker.Start
	for start.Before(marker.End) {
		nextDay := localDayStart(start).AddDate(0, 0, 1)
		end := minTime(marker.End, nextDay)
		if end.After(start) {
			clip := marker
			clip.Start = start
			clip.End = end
			clips = append(clips, clip)
		}
		start = end
	}
	return clips
}

func motionMarkerFilePath(cameraID string, day time.Time) string {
	return filepath.Join(MotionMarkerDir(cameraID), localDayStart(day).Format("2006-01-02")+".jsonl")
}

func localDayStart(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func normalizeMarkerText(value string) string {
	return strings.TrimSpace(value)
}
