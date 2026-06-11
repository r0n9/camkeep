package task

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

const (
	onvifEventCapabilityCheckDelay  = time.Second
	onvifEventSubscribeTimeout      = 10 * time.Second
	onvifEventPullTimeout           = 25 * time.Second
	onvifEventHTTPTimeout           = onvifEventPullTimeout + 5*time.Second
	onvifEventMessageLimit          = 20
	onvifEventSubscriptionDuration  = time.Hour
	onvifEventRenewBefore           = 10 * time.Minute
	onvifEventReconnectInitial      = 3 * time.Second
	onvifEventReconnectMax          = time.Minute
	onvifEventInitialSnapshotWindow = 5 * time.Second
	onvifEventLogValueLimit         = 512
)

type onvifEventHandleOptions struct {
	SuppressMotionPublish bool
	SuppressReason        string
}

// OnvifEventTask listens for ONVIF PullPoint events and publishes matched motion
// notifications into the existing in-memory DetectionEvent model.
func OnvifEventTask(ctx context.Context, wg *sync.WaitGroup, cam constant.Camera, candidate onvif.Candidate) {
	defer wg.Done()
	runOnvifEventWatcher(ctx, cam, candidate, func() bool { return true })
}

func runOnvifEventWatcher(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, shouldPublishMotion func() bool) {
	service.UpdateOnvifEventListenerStarting(cam.ID)
	reconnectDelay := onvifEventReconnectInitial
	for {
		status, ok := waitOnvifPullPointReady(ctx, candidate)
		if !ok {
			service.UpdateOnvifEventListenerInactive(cam.ID)
			return
		}

		sessionStartedAt := time.Now()
		err := runOnvifPullPointSession(ctx, cam, candidate, status, shouldPublishMotion)
		if ctx.Err() != nil {
			service.UpdateOnvifEventListenerInactive(cam.ID)
			return
		}
		if err != nil {
			service.UpdateOnvifEventListenerError(cam.ID, err)
			log.Printf("[%s] ONVIF PullPoint 事件监听异常，%s 后重连: %v", cam.ID, reconnectDelay, err)
		}
		if time.Since(sessionStartedAt) >= onvifEventReconnectMax {
			reconnectDelay = onvifEventReconnectInitial
		}
		if !waitOnvifEventRetry(ctx, reconnectDelay) {
			return
		}
		reconnectDelay = nextOnvifEventReconnectDelay(reconnectDelay)
	}
}

func waitOnvifPullPointReady(ctx context.Context, candidate onvif.Candidate) (service.OnvifStatus, bool) {
	ticker := time.NewTicker(onvifEventCapabilityCheckDelay)
	defer ticker.Stop()

	probeRetryDelay := onvifEventReconnectInitial
	nextProbeAfter := time.Time{}
	for {
		now := time.Now()
		status, ok := service.GetOnvifStatus(candidate.ID)
		if ok {
			switch status.EventState {
			case service.OnvifStateAvailable:
				if status.EventXAddr == "" {
					log.Printf("[%s] ONVIF Event 可用但缺少 xaddr，跳过 PullPoint 监听", candidate.ID)
					return service.OnvifStatus{}, false
				}
				if !status.PullPointSupport {
					log.Printf("[%s] ONVIF Event 可用但设备不支持 PullPoint，跳过事件监听", candidate.ID)
					return service.OnvifStatus{}, false
				}
				return status, true
			case service.OnvifStateUnavailable:
				log.Printf("[%s] ONVIF Event 不可用，跳过 PullPoint 监听", candidate.ID)
				return service.OnvifStatus{}, false
			case service.OnvifStateError:
				if now.Before(nextProbeAfter) {
					break
				}
				refreshed, err := refreshOnvifPullPointStatus(ctx, candidate)
				if err != nil {
					log.Printf("[%s] ONVIF Event 能力重新探测失败，%s 后重试: %v", candidate.ID, probeRetryDelay, err)
					nextProbeAfter = now.Add(probeRetryDelay)
					probeRetryDelay = nextOnvifEventReconnectDelay(probeRetryDelay)
					break
				}
				status = refreshed
				ok = true
				probeRetryDelay = onvifEventReconnectInitial
				nextProbeAfter = time.Time{}
				continue
			}
		} else if !now.Before(nextProbeAfter) {
			if _, err := refreshOnvifPullPointStatus(ctx, candidate); err != nil {
				log.Printf("[%s] ONVIF Event 能力探测失败，%s 后重试: %v", candidate.ID, probeRetryDelay, err)
				nextProbeAfter = now.Add(probeRetryDelay)
				probeRetryDelay = nextOnvifEventReconnectDelay(probeRetryDelay)
			} else {
				probeRetryDelay = onvifEventReconnectInitial
				nextProbeAfter = time.Time{}
				continue
			}
		}

		select {
		case <-ctx.Done():
			return service.OnvifStatus{}, false
		case <-ticker.C:
		}
	}
}

func refreshOnvifPullPointStatus(ctx context.Context, candidate onvif.Candidate) (service.OnvifStatus, error) {
	service.MarkOnvifProbeStarted(candidate)

	probeCtx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()

	caps, err := probeOnvifCapabilities(probeCtx, candidate)
	if err != nil {
		service.UpdateOnvifProbeError(candidate, err)
		return service.OnvifStatus{}, err
	}

	service.UpdateOnvifProbeResult(candidate, caps)
	status, ok := service.GetOnvifStatus(candidate.ID)
	if !ok {
		return service.OnvifStatus{}, fmt.Errorf("ONVIF status 不存在")
	}
	return status, nil
}

func runOnvifPullPointSession(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, status service.OnvifStatus, shouldPublishMotion func() bool) error {
	client := onvif.NewClient(candidate)
	client.HTTPClient = &http.Client{Timeout: onvifEventHTTPTimeout}

	subscribeCtx, cancel := context.WithTimeout(ctx, onvifEventSubscribeTimeout)
	subscription, err := client.CreatePullPointSubscription(subscribeCtx, status.EventXAddr, onvifEventSubscriptionDuration)
	cancel()
	if err != nil {
		return fmt.Errorf("创建 PullPoint 订阅失败: %w", err)
	}

	log.Printf("[%s] ONVIF PullPoint 事件监听已启动: event_xaddr=%s subscription=%s termination=%s",
		cam.ID, status.EventXAddr, subscription.Reference, formatEventTime(subscription.TerminationTime))
	service.UpdateOnvifEventListenerListening(cam.ID, time.Time{})
	defer unsubscribeOnvifPullPoint(client, cam.ID, subscription.Reference)

	firstPull := true
	subscriptionStartedAt := time.Now()
	nextRenew := nextOnvifSubscriptionRenewTime(subscription.TerminationTime, time.Now())
	for {
		if !nextRenew.IsZero() && !time.Now().Before(nextRenew) {
			renewCtx, cancel := context.WithTimeout(ctx, onvifEventSubscribeTimeout)
			_, terminationTime, err := client.RenewSubscription(renewCtx, subscription.Reference, onvifEventSubscriptionDuration)
			cancel()
			if err != nil {
				return fmt.Errorf("续订 PullPoint 订阅失败: %w", err)
			}
			nextRenew = nextOnvifSubscriptionRenewTime(terminationTime, time.Now())
			log.Printf("[%s] ONVIF PullPoint 订阅已续订: termination=%s next_renew=%s",
				cam.ID, formatEventTime(terminationTime), formatEventTime(nextRenew))
		}

		pullCtx, cancel := context.WithTimeout(ctx, onvifEventHTTPTimeout)
		notifications, err := client.PullMessages(pullCtx, subscription.Reference, onvifEventPullTimeout, onvifEventMessageLimit)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("拉取 PullPoint 事件失败: %w", err)
		}
		pullCompletedAt := time.Now()
		service.UpdateOnvifEventListenerListening(cam.ID, pullCompletedAt)
		for _, notification := range notifications {
			options := onvifEventHandleOptions{}
			publishMotion := shouldPublishMotion == nil || shouldPublishMotion()
			if !publishMotion {
				options.SuppressMotionPublish = true
				options.SuppressReason = "当前监听租约不发布动检事件"
			} else if shouldSuppressInitialOnvifMotionPublish(notification, firstPull, subscriptionStartedAt, pullCompletedAt) {
				options.SuppressMotionPublish = true
				options.SuppressReason = "订阅启动后的初始化状态事件"
			}
			handleOnvifEventNotificationWithOptions(cam.ID, notification, options)
		}
		firstPull = false
	}
}

func handleOnvifEventNotification(camID string, notification onvif.EventNotification) {
	handleOnvifEventNotificationWithOptions(camID, notification, onvifEventHandleOptions{})
}

func handleOnvifEventNotificationWithOptions(camID string, notification onvif.EventNotification, options onvifEventHandleOptions) {
	eventAt := effectiveOnvifEventTime(notification.At, time.Now())
	sourceText := formatOnvifEventItems(notification.Source)
	keyText := formatOnvifEventItems(notification.Key)
	dataText := formatOnvifEventItems(notification.Data)
	motionTopic := isOnvifMotionTopic(notification.Topic)
	motionEvent := isOnvifMotionNotification(notification)
	log.Printf("[%s] ONVIF PullPoint 事件: topic=%q op=%q at=%s source=%s key=%s data=%s",
		camID,
		notification.Topic,
		notification.Operation,
		eventAt.Format(time.RFC3339),
		sourceText,
		keyText,
		dataText,
	)

	service.UpdateOnvifLastEvent(camID, service.OnvifEventSnapshot{
		Topic:           notification.Topic,
		Operation:       notification.Operation,
		At:              eventAt,
		ReceivedAt:      time.Now(),
		ProducerAddress: notification.ProducerAddress,
		Source:          sourceText,
		Key:             keyText,
		Data:            dataText,
		Motion:          motionEvent,
		MotionTopic:     motionTopic,
	})

	if !motionEvent {
		return
	}
	if options.SuppressMotionPublish {
		reason := strings.TrimSpace(options.SuppressReason)
		if reason == "" {
			reason = "事件被过滤"
		}
		log.Printf("[%s] ONVIF PullPoint motion 事件已忽略: topic=%q at=%s reason=%s data=%s",
			camID, notification.Topic, eventAt.Format(time.RFC3339), reason, dataText)
		return
	}

	PublishDetectionEvent(DetectionEvent{
		CameraID: camID,
		Type:     EventTypeMotion,
		Source:   "onvif-pullpoint",
		At:       eventAt,
		Metadata: map[string]string{
			"topic":     notification.Topic,
			"operation": notification.Operation,
			"data":      dataText,
		},
	})
	startMotionAutoFrameDiffFollowUp(camID, eventAt)
	log.Printf("[%s] ONVIF PullPoint motion 事件已发布: topic=%q at=%s data=%s",
		camID, notification.Topic, eventAt.Format(time.RFC3339), dataText)
}

func shouldSuppressInitialOnvifMotionPublish(notification onvif.EventNotification, firstPull bool, subscriptionStartedAt, pullCompletedAt time.Time) bool {
	if !firstPull || !isOnvifMotionTopic(notification.Topic) {
		return false
	}
	if subscriptionStartedAt.IsZero() || pullCompletedAt.IsZero() {
		return false
	}
	if pullCompletedAt.Sub(subscriptionStartedAt) > onvifEventInitialSnapshotWindow {
		return false
	}
	// PullPoint 新订阅后，设备常会先返回当前状态快照。只有明确 Changed 的事件
	// 才在首批消息中参与动检触发，避免 MotionAlarm 初始化状态误拉起录像。
	return strings.ToLower(strings.TrimSpace(notification.Operation)) != "changed"
}

func isOnvifMotionNotification(notification onvif.EventNotification) bool {
	if !isOnvifMotionTopic(notification.Topic) {
		return false
	}
	if state, ok := onvifTopicMotionState(notification.Topic, notification.Data); ok {
		return state
	}
	if state, ok := onvifMotionState(notification.Data); ok {
		return state
	}
	return true
}

func isOnvifMotionTopic(topic string) bool {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if strings.Contains(topic, "ruleengine/countaggregation/counter") {
		return false
	}
	return strings.Contains(topic, "videosource/motionalarm") ||
		strings.Contains(topic, "ruleengine")
}

func onvifTopicMotionState(topic string, items []onvif.EventItem) (bool, bool) {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if strings.Contains(topic, "ruleengine/fielddetector/objectsinside") {
		return onvifEventItemBool(items, "isinside")
	}
	return false, false
}

func onvifMotionState(items []onvif.EventItem) (bool, bool) {
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Name)) {
		case "state", "ismotion", "motion", "logicalstate":
			if value, ok := parseOnvifEventBool(item.Value); ok {
				return value, true
			}
		}
	}
	return false, false
}

func onvifEventItemBool(items []onvif.EventItem, name string) (bool, bool) {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) {
			return parseOnvifEventBool(item.Value)
		}
	}
	return false, false
}

func parseOnvifEventBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on", "active":
		return true, true
	case "false", "0", "no", "off", "inactive":
		return false, true
	default:
		return false, false
	}
}

func effectiveOnvifEventTime(eventAt, receivedAt time.Time) time.Time {
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	if eventAt.IsZero() || eventAt.After(receivedAt.Add(5*time.Minute)) {
		return receivedAt
	}
	return eventAt
}

func nextOnvifSubscriptionRenewTime(terminationTime, now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	if terminationTime.IsZero() {
		return now.Add(onvifEventSubscriptionDuration - onvifEventRenewBefore)
	}
	renewAt := terminationTime.Add(-onvifEventRenewBefore)
	minRenewAt := now.Add(time.Minute)
	if renewAt.Before(minRenewAt) {
		return minRenewAt
	}
	return renewAt
}

func unsubscribeOnvifPullPoint(client *onvif.Client, camID, subscriptionReference string) {
	ctx, cancel := context.WithTimeout(context.Background(), onvifEventSubscribeTimeout)
	defer cancel()

	if err := client.Unsubscribe(ctx, subscriptionReference); err != nil {
		log.Printf("[%s] 取消 ONVIF PullPoint 订阅失败: %v", camID, err)
		return
	}
	log.Printf("[%s] ONVIF PullPoint 订阅已取消", camID)
}

func waitOnvifEventRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextOnvifEventReconnectDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return onvifEventReconnectInitial
	}
	next := current * 2
	if next > onvifEventReconnectMax {
		return onvifEventReconnectMax
	}
	return next
}

func formatOnvifEventItems(items []onvif.EventItem) string {
	if len(items) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		value := strings.TrimSpace(item.Value)
		if name == "" && value == "" {
			continue
		}
		parts = append(parts, name+"="+value)
	}
	if len(parts) == 0 {
		return "-"
	}
	return limitOnvifEventLogText(strings.Join(parts, ", "))
}

func limitOnvifEventLogText(value string) string {
	if len(value) <= onvifEventLogValueLimit {
		return value
	}
	return value[:onvifEventLogValueLimit] + "..."
}

func formatEventTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339)
}
