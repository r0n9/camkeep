package task

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

const motionMarkerIdleTimeout = motionRecordIdleTimeout

type motionMarkerSession struct {
	marker      MotionMarker
	lastEventAt time.Time
}

func motionMarkingEnabled(cam constant.Camera) bool {
	return isNormalMode(cam) && !cam.MotionDetect && cam.MotionMarkEnabled
}

func motionMarkEventSource(cam constant.Camera) string {
	return constant.NormalizeMotionMarkEventSource(cam.MotionMarkEventSource)
}

func motionMarkSourceUsesOnvif(cam constant.Camera, eventCandidate *onvif.Candidate) bool {
	if !motionMarkingEnabled(cam) || eventCandidate == nil {
		return false
	}
	switch motionMarkEventSource(cam) {
	case constant.MotionEventSourceONVIF, constant.MotionEventSourceAuto:
		return true
	default:
		return false
	}
}

func motionMarkSourceUsesFrameDiff(cam constant.Camera, now time.Time) bool {
	if !motionMarkingEnabled(cam) {
		return false
	}
	switch motionMarkEventSource(cam) {
	case constant.MotionEventSourceONVIF:
		return false
	case constant.MotionEventSourceAuto:
		return !service.OnvifEventSourceUsable(cam.ID, now)
	default:
		return true
	}
}

func motionMarkerRecentEvent(cam constant.Camera, now time.Time) (DetectionEvent, bool) {
	event, ok := RecentDetectionEvent(cam.ID, EventTypeMotion, now, motionMarkerIdleTimeout)
	if !ok {
		return DetectionEvent{}, false
	}

	switch motionMarkEventSource(cam) {
	case constant.MotionEventSourceONVIF:
		return event, detectionEventIsOnvif(event)
	case constant.MotionEventSourceAuto:
		if service.OnvifEventSourceUsable(cam.ID, now) {
			return event, detectionEventIsOnvif(event)
		}
		return event, detectionEventIsFrameDiff(event)
	default:
		return event, detectionEventIsFrameDiff(event)
	}
}

func startMotionMarkerSession(cam constant.Camera, event DetectionEvent, now time.Time) *motionMarkerSession {
	marker := motionMarkerFromEvent(cam, event, now)
	return &motionMarkerSession{
		marker:      marker,
		lastEventAt: marker.Start,
	}
}

func updateMotionMarkerSession(cam constant.Camera, session *motionMarkerSession, event DetectionEvent, now time.Time) bool {
	if session == nil {
		return false
	}
	marker := motionMarkerFromEvent(cam, event, now)
	if motionMarkerSessionShouldRotate(session, marker) {
		return false
	}
	if marker.Start.After(session.lastEventAt) {
		session.lastEventAt = marker.Start
	}
	if marker.Score > 0 {
		session.marker.Score = marker.Score
	}
	return true
}

func finishMotionMarkerSession(cam constant.Camera, session *motionMarkerSession, endTime time.Time) {
	if session == nil {
		return
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}
	if endTime.Before(session.lastEventAt) {
		endTime = session.lastEventAt
	}
	session.marker.End = endTime
	if !session.marker.End.After(session.marker.Start) {
		return
	}
	if err := AppendMotionMarker(session.marker); err != nil {
		log.Printf("[%s] 保存动检时间轴标记失败: %v", cam.ID, err)
		return
	}
	log.Printf("[%s] 动检时间轴标记已保存: source=%s start=%s end=%s",
		cam.ID, session.marker.Source, session.marker.Start.Format(time.RFC3339), session.marker.End.Format(time.RFC3339))
}

func motionMarkerFromEvent(cam constant.Camera, event DetectionEvent, now time.Time) MotionMarker {
	at := event.At
	if at.IsZero() {
		at = now
	}
	source := motionMarkerSourceName(cam, event, now)
	marker := MotionMarker{
		CameraID: cam.ID,
		Start:    at,
		End:      at,
		Source:   source,
		Topic:    strings.TrimSpace(event.Metadata["topic"]),
		Reason:   motionMarkerReason(event),
		Score:    motionMarkerScore(event),
	}
	if marker.Score == 0 && detectionEventIsOnvif(event) {
		marker.Score = 1
	}
	return marker
}

func motionMarkerSourceName(cam constant.Camera, event DetectionEvent, now time.Time) string {
	source := motionMarkEventSource(cam)
	if source == constant.MotionEventSourceAuto {
		if detectionEventIsOnvif(event) {
			return "auto_onvif"
		}
		return "auto_frame_diff"
	}
	if detectionEventIsOnvif(event) {
		return "onvif"
	}
	return "frame_diff"
}

func motionMarkerReason(event DetectionEvent) string {
	if detectionEventIsOnvif(event) {
		return "motion_topic"
	}
	return "frame_diff_threshold"
}

func motionMarkerScore(event DetectionEvent) float64 {
	if event.Metadata == nil {
		return 0
	}
	score, _ := strconv.ParseFloat(strings.TrimSpace(event.Metadata["diff_ratio"]), 64)
	return score
}

func motionMarkerSessionShouldRotate(session *motionMarkerSession, marker MotionMarker) bool {
	if session == nil {
		return false
	}
	return session.marker.Source != marker.Source ||
		session.marker.Topic != marker.Topic ||
		session.marker.Reason != marker.Reason
}

func detectionEventIsOnvif(event DetectionEvent) bool {
	return strings.EqualFold(strings.TrimSpace(event.Source), "onvif-pullpoint")
}

func detectionEventIsFrameDiff(event DetectionEvent) bool {
	return strings.EqualFold(strings.TrimSpace(event.Source), "builtin-motion")
}
