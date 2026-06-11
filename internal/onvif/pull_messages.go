package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	soapEnvelopeNamespace = "http://www.w3.org/2003/05/soap-envelope"
	eventsNamespace       = "http://www.onvif.org/ver10/events/wsdl"
	wsseNamespace         = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNamespace          = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	passwordDigestType    = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	nonceBase64Type       = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
)

type pullMessagesRequest struct {
	XMLName      xml.Name `xml:"tev:PullMessages"`
	Xmlns        string   `xml:"xmlns:tev,attr"`
	Timeout      string   `xml:"tev:Timeout"`
	MessageLimit int      `xml:"tev:MessageLimit"`
}

type pullMessagesSOAPEnvelope struct {
	XMLName xml.Name                `xml:"s:Envelope"`
	XmlnsS  string                  `xml:"xmlns:s,attr"`
	Header  *pullMessagesSOAPHeader `xml:"s:Header,omitempty"`
	Body    pullMessagesSOAPBody    `xml:"s:Body"`
}

type pullMessagesSOAPHeader struct {
	Security *pullMessagesSOAPSecurity `xml:"wsse:Security,omitempty"`
}

type pullMessagesSOAPSecurity struct {
	XmlnsWsse      string                         `xml:"xmlns:wsse,attr"`
	XmlnsWsu       string                         `xml:"xmlns:wsu,attr"`
	MustUnderstand string                         `xml:"s:mustUnderstand,attr,omitempty"`
	UsernameToken  *pullMessagesSOAPUsernameToken `xml:"wsse:UsernameToken,omitempty"`
}

type pullMessagesSOAPUsernameToken struct {
	Username string                   `xml:"wsse:Username"`
	Password pullMessagesSOAPPassword `xml:"wsse:Password"`
	Nonce    pullMessagesSOAPNonce    `xml:"wsse:Nonce"`
	Created  string                   `xml:"wsu:Created"`
}

type pullMessagesSOAPPassword struct {
	Type     string `xml:"Type,attr"`
	Password string `xml:",chardata"`
}

type pullMessagesSOAPNonce struct {
	Type  string `xml:"EncodingType,attr"`
	Nonce string `xml:",chardata"`
}

type pullMessagesSOAPBody struct {
	Content any `xml:",omitempty"`
}

type pullMessagesSOAPResponseEnvelope struct {
	Body struct {
		Content []byte `xml:",innerxml"`
	} `xml:"Body"`
}

type pullMessagesResponse struct {
	NotificationMessages []pullMessagesNotification `xml:"NotificationMessage"`
}

type pullMessagesNotification struct {
	Topic struct {
		Value string `xml:",chardata"`
	} `xml:"Topic"`
	ProducerReference struct {
		Address string `xml:"Address"`
	} `xml:"ProducerReference"`
	Message pullMessagesEventMessage `xml:"Message"`
}

type pullMessagesEventMessage struct {
	PropertyOperation string                     `xml:"PropertyOperation,attr"`
	UtcTime           string                     `xml:"UtcTime,attr"`
	Source            pullMessagesItemList       `xml:"Source"`
	Key               pullMessagesItemList       `xml:"Key"`
	Data              pullMessagesItemList       `xml:"Data"`
	NestedMessages    []pullMessagesEventMessage `xml:"Message"`
}

type pullMessagesItemList struct {
	SimpleItems []pullMessagesSimpleItem `xml:"SimpleItem"`
}

type pullMessagesSimpleItem struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:"Value,attr"`
}

type flattenedPullMessage struct {
	PropertyOperation string
	UtcTime           string
	Source            []pullMessagesSimpleItem
	Key               []pullMessagesSimpleItem
	Data              []pullMessagesSimpleItem
}

func (c *Client) pullMessages(ctx context.Context, subscriptionReference string, timeout time.Duration, messageLimit int) ([]EventNotification, error) {
	request := pullMessagesRequest{
		Xmlns:        eventsNamespace,
		Timeout:      formatONVIFDuration(timeout),
		MessageLimit: messageLimit,
	}

	xmlBody, err := marshalPullMessagesSOAPEnvelope(request, c.Username, c.Password)
	if err != nil {
		return nil, err
	}

	responseBody, err := c.callPullMessagesSOAP(ctx, subscriptionReference, xmlBody)
	if err != nil {
		return nil, err
	}

	notifications, err := parsePullMessagesSOAPResponse(responseBody)
	if err != nil {
		return nil, err
	}
	return notifications, nil
}

func marshalPullMessagesSOAPEnvelope(request pullMessagesRequest, username, password string) ([]byte, error) {
	envelope := pullMessagesSOAPEnvelope{
		XmlnsS: soapEnvelopeNamespace,
		Body: pullMessagesSOAPBody{
			Content: request,
		},
	}
	if username != "" || password != "" {
		envelope.Header = &pullMessagesSOAPHeader{
			Security: newPullMessagesSOAPSecurity(username, password),
		}
	}

	body, err := xml.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 ONVIF PullMessages SOAP 请求失败: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

func newPullMessagesSOAPSecurity(username, password string) *pullMessagesSOAPSecurity {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		nonceBytes = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	created := time.Now().UTC().Format(time.RFC3339)

	hash := sha1.New() //nolint:gosec // ONVIF WS-Security UsernameToken requires SHA1.
	hash.Write(nonceBytes)
	hash.Write([]byte(created))
	hash.Write([]byte(password))

	return &pullMessagesSOAPSecurity{
		XmlnsWsse:      wsseNamespace,
		XmlnsWsu:       wsuNamespace,
		MustUnderstand: "1",
		UsernameToken: &pullMessagesSOAPUsernameToken{
			Username: username,
			Password: pullMessagesSOAPPassword{
				Type:     passwordDigestType,
				Password: base64.StdEncoding.EncodeToString(hash.Sum(nil)),
			},
			Nonce: pullMessagesSOAPNonce{
				Type:  nonceBase64Type,
				Nonce: nonce,
			},
			Created: created,
		},
	}
}

func (c *Client) callPullMessagesSOAP(ctx context.Context, endpoint string, xmlBody []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(xmlBody))
	if err != nil {
		return nil, fmt.Errorf("创建 ONVIF PullMessages HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送 ONVIF PullMessages 请求失败: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 ONVIF PullMessages 响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ONVIF PullMessages HTTP %d: %s", resp.StatusCode, limitONVIFErrorBody(responseBody))
	}
	if len(responseBody) == 0 {
		return nil, fmt.Errorf("ONVIF PullMessages 响应为空")
	}
	return responseBody, nil
}

func parsePullMessagesSOAPResponse(responseBody []byte) ([]EventNotification, error) {
	var envelope pullMessagesSOAPResponseEnvelope
	if err := xml.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("解析 ONVIF PullMessages SOAP envelope 失败: %w", err)
	}
	if len(envelope.Body.Content) == 0 {
		return nil, fmt.Errorf("ONVIF PullMessages SOAP body 为空")
	}

	var response pullMessagesResponse
	if err := xml.Unmarshal(envelope.Body.Content, &response); err != nil {
		return nil, fmt.Errorf("解析 ONVIF PullMessages 响应失败: %w", err)
	}

	notifications := make([]EventNotification, 0, len(response.NotificationMessages))
	for _, notification := range response.NotificationMessages {
		message := flattenPullMessagesEventMessage(notification.Message)
		notifications = append(notifications, EventNotification{
			Topic:           strings.TrimSpace(notification.Topic.Value),
			Operation:       strings.TrimSpace(message.PropertyOperation),
			At:              parseONVIFEventTime(message.UtcTime),
			ProducerAddress: strings.TrimSpace(notification.ProducerReference.Address),
			Source:          mapPullMessageItems(message.Source),
			Key:             mapPullMessageItems(message.Key),
			Data:            mapPullMessageItems(message.Data),
		})
	}
	return notifications, nil
}

func flattenPullMessagesEventMessage(message pullMessagesEventMessage) flattenedPullMessage {
	result := flattenedPullMessage{
		PropertyOperation: strings.TrimSpace(message.PropertyOperation),
		UtcTime:           strings.TrimSpace(message.UtcTime),
		Source:            message.Source.SimpleItems,
		Key:               message.Key.SimpleItems,
		Data:              message.Data.SimpleItems,
	}

	for _, nested := range message.NestedMessages {
		flattened := flattenPullMessagesEventMessage(nested)
		if result.PropertyOperation == "" {
			result.PropertyOperation = flattened.PropertyOperation
		}
		if result.UtcTime == "" {
			result.UtcTime = flattened.UtcTime
		}
		if len(result.Source) == 0 {
			result.Source = flattened.Source
		}
		if len(result.Key) == 0 {
			result.Key = flattened.Key
		}
		if len(result.Data) == 0 {
			result.Data = flattened.Data
		}
	}
	return result
}

func mapPullMessageItems(items []pullMessagesSimpleItem) []EventItem {
	result := make([]EventItem, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		value := strings.TrimSpace(item.Value)
		if name == "" && value == "" {
			continue
		}
		result = append(result, EventItem{
			Name:  name,
			Value: value,
		})
	}
	return result
}

func parseONVIFEventTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Time{}
}

func formatONVIFDuration(value time.Duration) string {
	seconds := int(value.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("PT%dS", seconds)
	}

	minutes := seconds / 60
	seconds %= 60
	if seconds == 0 {
		return fmt.Sprintf("PT%dM", minutes)
	}
	return fmt.Sprintf("PT%dM%dS", minutes, seconds)
}

func limitONVIFErrorBody(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) <= 1024 {
		return value
	}
	return value[:1024] + "..."
}
