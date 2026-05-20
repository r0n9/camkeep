package service

import "testing"

func TestUpdateStatusSetsRecordingState(t *testing.T) {
	camID := "record-state-normal"
	deleteStatusForTest(t, camID)

	UpdateStatus(camID, true, "normal", "08:00-12:00,14:00-18:00")

	StatusMux.RLock()
	status := StatusMap[camID]
	StatusMux.RUnlock()

	if status.RecordState != RecordStateRecording {
		t.Fatalf("expected record state %q, got %q", RecordStateRecording, status.RecordState)
	}
	if status.Mode != ModeNormal {
		t.Fatalf("expected mode %q, got %q", ModeNormal, status.Mode)
	}
	if !status.IsRunning {
		t.Fatal("expected is_running to stay true")
	}
	if status.RecordTime != "08:00-12:00,14:00-18:00" {
		t.Fatalf("expected record_time to be stored, got %q", status.RecordTime)
	}
}

func TestUpdateRecordStateSetsMotionRecording(t *testing.T) {
	camID := "record-state-motion"
	deleteStatusForTest(t, camID)

	UpdateRecordState(camID, RecordStateMotionRecording, "motion", "22:00-06:00")

	StatusMux.RLock()
	status := StatusMap[camID]
	StatusMux.RUnlock()

	if status.RecordState != RecordStateMotionRecording {
		t.Fatalf("expected record state %q, got %q", RecordStateMotionRecording, status.RecordState)
	}
	if status.Mode != ModeMotion {
		t.Fatalf("expected mode %q, got %q", ModeMotion, status.Mode)
	}
	if !status.IsRunning {
		t.Fatal("expected motion recording to set is_running true")
	}
	if status.RecordTime != "22:00-06:00" {
		t.Fatalf("expected record_time to be stored, got %q", status.RecordTime)
	}
}

func TestUpdateRecordStateSetsMotionDetecting(t *testing.T) {
	camID := "record-state-motion-detecting"
	deleteStatusForTest(t, camID)

	UpdateRecordState(camID, RecordStateMotionDetecting, "motion", "00:00-23:59")

	StatusMux.RLock()
	status := StatusMap[camID]
	StatusMux.RUnlock()

	if status.RecordState != RecordStateMotionDetecting {
		t.Fatalf("expected record state %q, got %q", RecordStateMotionDetecting, status.RecordState)
	}
	if status.Mode != ModeMotion {
		t.Fatalf("expected mode %q, got %q", ModeMotion, status.Mode)
	}
	if !status.IsRunning {
		t.Fatal("expected motion detecting to set is_running true")
	}
}

func TestUpdateRecordStateNormalizesUnknownState(t *testing.T) {
	camID := "record-state-unknown"
	deleteStatusForTest(t, camID)

	UpdateRecordState(camID, "unknown", "normal", "00:00-23:59")

	StatusMux.RLock()
	status := StatusMap[camID]
	StatusMux.RUnlock()

	if status.RecordState != RecordStateIdle {
		t.Fatalf("expected record state %q, got %q", RecordStateIdle, status.RecordState)
	}
	if status.Mode != ModeNormal {
		t.Fatalf("expected mode %q, got %q", ModeNormal, status.Mode)
	}
	if status.IsRunning {
		t.Fatal("expected unknown record state to set is_running false")
	}
	if status.RecordTime != "00:00-23:59" {
		t.Fatalf("expected record_time to be stored, got %q", status.RecordTime)
	}
}

func TestUpdateStatusNormalizesMode(t *testing.T) {
	camID := "record-state-mode-normalize"
	deleteStatusForTest(t, camID)

	UpdateStatus(camID, false, "  TIMELAPSE  ", "00:00-23:59")

	StatusMux.RLock()
	status := StatusMap[camID]
	StatusMux.RUnlock()

	if status.Mode != ModeTimelapse {
		t.Fatalf("expected mode %q, got %q", ModeTimelapse, status.Mode)
	}
}

func deleteStatusForTest(t *testing.T, camID string) {
	t.Helper()

	StatusMux.Lock()
	oldStatus, hadStatus := StatusMap[camID]
	delete(StatusMap, camID)
	StatusMux.Unlock()

	t.Cleanup(func() {
		StatusMux.Lock()
		if hadStatus {
			StatusMap[camID] = oldStatus
		} else {
			delete(StatusMap, camID)
		}
		StatusMux.Unlock()
	})
}
