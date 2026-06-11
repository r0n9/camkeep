package onvif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPullMessagesParsesNestedHikvisionMessage(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(hikvisionPullMessagesResponse))
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL,
		Username:   "admin",
		Password:   "secret",
		HTTPClient: server.Client(),
	}
	notifications, err := client.PullMessages(context.Background(), server.URL, 25*time.Second, 20)
	if err != nil {
		t.Fatalf("expected PullMessages to pass, got %v", err)
	}
	if !strings.Contains(gotBody, "PullMessages") || !strings.Contains(gotBody, "UsernameToken") {
		t.Fatalf("expected PullMessages SOAP request with WS-Security, got %s", gotBody)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected one notification, got %+v", notifications)
	}

	notification := notifications[0]
	if notification.Topic != "tns1:RuleEngine/CellMotionDetector/Motion" {
		t.Fatalf("unexpected topic: %+v", notification)
	}
	if notification.Operation != "Changed" {
		t.Fatalf("unexpected operation: %+v", notification)
	}
	if notification.At.Format(time.RFC3339) != "2026-06-10T01:02:03Z" {
		t.Fatalf("unexpected event time: %+v", notification)
	}
	if got := eventItemsForTest(notification.Source); got != "VideoSourceConfigurationToken=VideoSourceConfig_1" {
		t.Fatalf("unexpected source: %s", got)
	}
	if got := eventItemsForTest(notification.Key); got != "Rule=MyRuleDetector" {
		t.Fatalf("unexpected key: %s", got)
	}
	if got := eventItemsForTest(notification.Data); got != "IsMotion=true" {
		t.Fatalf("unexpected data: %s", got)
	}
}

func TestPullMessagesParsesDirectMessageItems(t *testing.T) {
	notifications, err := parsePullMessagesSOAPResponse([]byte(directPullMessagesResponse))
	if err != nil {
		t.Fatalf("expected PullMessages response to parse, got %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected one notification, got %+v", notifications)
	}

	notification := notifications[0]
	if notification.Topic != "tns1:VideoSource/MotionAlarm" {
		t.Fatalf("unexpected topic: %+v", notification)
	}
	if got := eventItemsForTest(notification.Source); got != "VideoSourceToken=video_src_001" {
		t.Fatalf("unexpected source: %s", got)
	}
	if got := eventItemsForTest(notification.Data); got != "State=true" {
		t.Fatalf("unexpected data: %s", got)
	}
}

func eventItemsForTest(items []EventItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.Name+"="+item.Value)
	}
	return strings.Join(parts, ", ")
}

const hikvisionPullMessagesResponse = `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
  xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
  xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
  xmlns:wsa="http://www.w3.org/2005/08/addressing"
  xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tev:PullMessagesResponse>
      <tev:CurrentTime>2026-06-10T01:02:04Z</tev:CurrentTime>
      <tev:TerminationTime>2026-06-10T02:02:04Z</tev:TerminationTime>
      <wsnt:NotificationMessage>
        <wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:RuleEngine/CellMotionDetector/Motion</wsnt:Topic>
        <wsnt:ProducerReference>
          <wsa:Address>http://192.0.2.10/onvif/event_service</wsa:Address>
        </wsnt:ProducerReference>
        <wsnt:Message>
          <tt:Message UtcTime="2026-06-10T01:02:03Z" PropertyOperation="Changed">
            <tt:Source>
              <tt:SimpleItem Name="VideoSourceConfigurationToken" Value="VideoSourceConfig_1"/>
            </tt:Source>
            <tt:Key>
              <tt:SimpleItem Name="Rule" Value="MyRuleDetector"/>
            </tt:Key>
            <tt:Data>
              <tt:SimpleItem Name="IsMotion" Value="true"/>
            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </s:Body>
</s:Envelope>`

const directPullMessagesResponse = `
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope">
  <SOAP-ENV:Body>
    <tev:PullMessagesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl">
      <tev:CurrentTime>2026-06-10T01:02:04Z</tev:CurrentTime>
      <tev:TerminationTime>2026-06-10T02:02:04Z</tev:TerminationTime>
      <wsnt:NotificationMessage xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
        <wsnt:Topic>tns1:VideoSource/MotionAlarm</wsnt:Topic>
        <wsnt:Message PropertyOperation="Changed" UtcTime="2026-06-10T01:02:03Z">
          <tt:Source xmlns:tt="http://www.onvif.org/ver10/schema">
            <tt:SimpleItem Name="VideoSourceToken" Value="video_src_001"/>
          </tt:Source>
          <tt:Data xmlns:tt="http://www.onvif.org/ver10/schema">
            <tt:SimpleItem Name="State" Value="true"/>
          </tt:Data>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
