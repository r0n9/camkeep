package onvif

import (
	"context"
	"fmt"
	"strings"
	"time"

	onvifgo "github.com/0x524a/onvif-go"
)

type PullPointSubscription struct {
	Reference       string
	CurrentTime     time.Time
	TerminationTime time.Time
}

type EventNotification struct {
	Topic           string
	Operation       string
	At              time.Time
	ProducerAddress string
	Source          []EventItem
	Key             []EventItem
	Data            []EventItem
}

type EventItem struct {
	Name  string
	Value string
}

func (c *Client) CreatePullPointSubscription(ctx context.Context, eventXAddr string, termination time.Duration) (PullPointSubscription, error) {
	eventXAddr = strings.TrimSpace(eventXAddr)
	if eventXAddr == "" {
		return PullPointSubscription{}, fmt.Errorf("ONVIF event xaddr 为空")
	}

	backend, err := c.eventBackend(eventXAddr)
	if err != nil {
		return PullPointSubscription{}, err
	}

	var terminationPtr *time.Duration
	if termination > 0 {
		terminationPtr = &termination
	}
	subscription, err := backend.CreatePullPointSubscription(ctx, "", terminationPtr, "")
	if err != nil {
		return PullPointSubscription{}, err
	}
	if subscription == nil || strings.TrimSpace(subscription.SubscriptionReference) == "" {
		return PullPointSubscription{}, fmt.Errorf("ONVIF PullPoint 订阅响应缺少 subscription reference")
	}

	return PullPointSubscription{
		Reference:       strings.TrimSpace(subscription.SubscriptionReference),
		CurrentTime:     subscription.CurrentTime,
		TerminationTime: subscription.TerminationTime,
	}, nil
}

func (c *Client) PullMessages(ctx context.Context, subscriptionReference string, timeout time.Duration, messageLimit int) ([]EventNotification, error) {
	subscriptionReference = strings.TrimSpace(subscriptionReference)
	if subscriptionReference == "" {
		return nil, fmt.Errorf("ONVIF PullPoint subscription reference 为空")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("ONVIF PullPoint timeout 必须大于 0")
	}
	if messageLimit <= 0 {
		return nil, fmt.Errorf("ONVIF PullPoint message limit 必须大于 0")
	}

	return c.pullMessages(ctx, subscriptionReference, timeout, messageLimit)
}

func (c *Client) RenewSubscription(ctx context.Context, subscriptionReference string, termination time.Duration) (time.Time, time.Time, error) {
	subscriptionReference = strings.TrimSpace(subscriptionReference)
	if subscriptionReference == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("ONVIF PullPoint subscription reference 为空")
	}

	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return backend.RenewSubscription(ctx, subscriptionReference, termination)
}

func (c *Client) Unsubscribe(ctx context.Context, subscriptionReference string) error {
	subscriptionReference = strings.TrimSpace(subscriptionReference)
	if subscriptionReference == "" {
		return fmt.Errorf("ONVIF PullPoint subscription reference 为空")
	}

	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return err
	}
	return backend.Unsubscribe(ctx, subscriptionReference)
}

func (c *Client) eventBackend(eventXAddr string) (*onvifgo.Client, error) {
	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return nil, err
	}
	backend.SetEventEndpoint(eventXAddr)
	return backend, nil
}
