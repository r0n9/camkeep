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
	deviceNamespace       = "http://www.onvif.org/ver10/device/wsdl"
	soap12Namespace       = "http://www.w3.org/2003/05/soap-envelope"
	getCapabilitiesAction = deviceNamespace + "/GetCapabilities"
	defaultProbeTimeout   = 5 * time.Second
)

type Capabilities struct {
	DeviceXAddr       string
	MediaXAddr        string
	PTZXAddr          string
	EventXAddr        string
	PullPointSupport  bool
	RawEventSupported bool
}

type Client struct {
	Endpoint   string
	Username   string
	Password   string
	HTTPClient *http.Client
}

func NewClient(candidate Candidate) *Client {
	return &Client{
		Endpoint: candidate.Endpoint,
		Username: candidate.Username,
		Password: candidate.Password,
		HTTPClient: &http.Client{
			Timeout: defaultProbeTimeout,
		},
	}
}

func (c *Client) GetCapabilities(ctx context.Context) (Capabilities, error) {
	if strings.TrimSpace(c.Endpoint) == "" {
		return Capabilities{}, fmt.Errorf("ONVIF endpoint 为空")
	}

	envelope, err := c.getCapabilitiesEnvelope()
	if err != nil {
		return Capabilities{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(envelope))
	if err != nil {
		return Capabilities{}, err
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+getCapabilitiesAction+`"`)
	req.Header.Set("SOAPAction", `"`+getCapabilitiesAction+`"`)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultProbeTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return Capabilities{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Capabilities{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Capabilities{}, fmt.Errorf("ONVIF GetCapabilities HTTP %d: %s", resp.StatusCode, compactBody(body))
	}

	return ParseCapabilitiesResponse(body)
}

func (c *Client) getCapabilitiesEnvelope() ([]byte, error) {
	securityHeader, err := c.wsSecurityHeader()
	if err != nil {
		return nil, err
	}

	body := `<s:Envelope xmlns:s="` + soap12Namespace + `" xmlns:tds="` + deviceNamespace + `">` +
		securityHeader +
		`<s:Body><tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities></s:Body></s:Envelope>`
	return []byte(body), nil
}

func (c *Client) wsSecurityHeader() (string, error) {
	if c.Username == "" {
		return "", nil
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	created := time.Now().UTC().Format(time.RFC3339)
	digest := passwordDigest(nonce, created, c.Password)

	return `<s:Header><wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` +
		`<wsse:UsernameToken>` +
		`<wsse:Username>` + escapeXML(c.Username) + `</wsse:Username>` +
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + digest + `</wsse:Password>` +
		`<wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce>` +
		`<wsu:Created>` + created + `</wsu:Created>` +
		`</wsse:UsernameToken></wsse:Security></s:Header>`, nil
}

func passwordDigest(nonce []byte, created, password string) string {
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func ParseCapabilitiesResponse(body []byte) (Capabilities, error) {
	var envelope capabilitiesEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return Capabilities{}, fmt.Errorf("解析 ONVIF GetCapabilities 响应失败: %w", err)
	}

	if envelope.Body.Fault != nil {
		return Capabilities{}, fmt.Errorf("ONVIF GetCapabilities SOAP Fault: %s", envelope.Body.Fault.Message())
	}

	caps := envelope.Body.Response.Capabilities
	result := Capabilities{
		DeviceXAddr:       strings.TrimSpace(caps.Device.XAddr),
		MediaXAddr:        strings.TrimSpace(caps.Media.XAddr),
		PTZXAddr:          strings.TrimSpace(caps.PTZ.XAddr),
		EventXAddr:        strings.TrimSpace(caps.Events.XAddr),
		PullPointSupport:  caps.Events.WSPullPointSupport,
		RawEventSupported: strings.TrimSpace(caps.Events.XAddr) != "",
	}
	if result.EventXAddr == "" {
		result.EventXAddr = strings.TrimSpace(caps.Event.XAddr)
		result.PullPointSupport = result.PullPointSupport || caps.Event.WSPullPointSupport
		result.RawEventSupported = result.EventXAddr != ""
	}

	if result.DeviceXAddr == "" && result.MediaXAddr == "" && result.PTZXAddr == "" && result.EventXAddr == "" {
		return Capabilities{}, fmt.Errorf("ONVIF GetCapabilities 响应中没有可用服务地址")
	}
	return result, nil
}

type capabilitiesEnvelope struct {
	Body capabilitiesBody `xml:"Body"`
}

type capabilitiesBody struct {
	Fault    *soapFault           `xml:"Fault"`
	Response capabilitiesResponse `xml:"GetCapabilitiesResponse"`
}

type capabilitiesResponse struct {
	Capabilities capabilitySet `xml:"Capabilities"`
}

type capabilitySet struct {
	Device capabilityXAddr `xml:"Device"`
	Media  capabilityXAddr `xml:"Media"`
	PTZ    capabilityXAddr `xml:"PTZ"`
	Events eventCapability `xml:"Events"`
	Event  eventCapability `xml:"Event"`
}

type capabilityXAddr struct {
	XAddr string `xml:"XAddr"`
}

type eventCapability struct {
	XAddr              string `xml:"XAddr"`
	WSPullPointSupport bool   `xml:"WSPullPointSupport"`
}

type soapFault struct {
	Code struct {
		Value string `xml:"Value"`
	} `xml:"Code"`
	Reason struct {
		Text string `xml:"Text"`
	} `xml:"Reason"`
	FaultCode   string `xml:"faultcode"`
	FaultString string `xml:"faultstring"`
}

func (f soapFault) Message() string {
	parts := []string{
		strings.TrimSpace(f.Code.Value),
		strings.TrimSpace(f.FaultCode),
		strings.TrimSpace(f.Reason.Text),
		strings.TrimSpace(f.FaultString),
	}
	var message []string
	for _, part := range parts {
		if part != "" {
			message = append(message, part)
		}
	}
	if len(message) == 0 {
		return "未知错误"
	}
	return strings.Join(message, ": ")
}

func compactBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 300 {
		return text[:300]
	}
	return text
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
