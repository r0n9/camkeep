package onvif

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	onvifgo "github.com/0x524a/onvif-go"
)

const defaultProbeTimeout = 5 * time.Second

type Capabilities struct {
	DeviceXAddr       string
	MediaXAddr        string
	PTZXAddr          string
	EventXAddr        string
	PullPointSupport  bool
	RawEventSupported bool
	ProfileToken      string
	ProfileName       string
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
	}
}

func (c *Client) GetCapabilities(ctx context.Context) (Capabilities, error) {
	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return Capabilities{}, err
	}

	caps, err := backend.GetCapabilities(ctx)
	if err != nil {
		return Capabilities{}, err
	}

	result := mapCapabilities(c.Endpoint, caps)
	if result.DeviceXAddr == "" && result.MediaXAddr == "" && result.PTZXAddr == "" && result.EventXAddr == "" {
		return Capabilities{}, fmt.Errorf("ONVIF GetCapabilities 响应中没有可用服务地址")
	}
	return result, nil
}

func mapCapabilities(endpoint string, caps *onvifgo.Capabilities) Capabilities {
	if caps == nil {
		return Capabilities{}
	}

	var result Capabilities
	if caps.Device != nil {
		result.DeviceXAddr = normalizeServiceXAddr(endpoint, caps.Device.XAddr)
	}
	if caps.Media != nil {
		result.MediaXAddr = normalizeServiceXAddr(endpoint, caps.Media.XAddr)
	}
	if caps.PTZ != nil {
		result.PTZXAddr = normalizeServiceXAddr(endpoint, caps.PTZ.XAddr)
	}
	if caps.Events != nil {
		result.EventXAddr = normalizeServiceXAddr(endpoint, caps.Events.XAddr)
		result.PullPointSupport = caps.Events.WSPullPointSupport
		result.RawEventSupported = result.EventXAddr != ""
	}
	return result
}

type MediaProfile struct {
	Token  string
	Name   string
	HasPTZ bool
}

func (c *Client) GetProfiles(ctx context.Context, mediaXAddr string) ([]MediaProfile, error) {
	mediaXAddr = strings.TrimSpace(mediaXAddr)
	if mediaXAddr == "" {
		return nil, fmt.Errorf("ONVIF media xaddr 为空")
	}

	backend, err := c.newBackend(mediaXAddr)
	if err != nil {
		return nil, err
	}

	profiles, err := backend.GetProfiles(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]MediaProfile, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		result = append(result, MediaProfile{
			Token:  strings.TrimSpace(profile.Token),
			Name:   strings.TrimSpace(profile.Name),
			HasPTZ: profile.PTZConfiguration != nil,
		})
	}
	return result, nil
}

func SelectPTZProfile(profiles []MediaProfile) (MediaProfile, bool) {
	for _, profile := range profiles {
		if profile.Token != "" && profile.HasPTZ {
			return profile, true
		}
	}
	for _, profile := range profiles {
		if profile.Token != "" {
			return profile, true
		}
	}
	return MediaProfile{}, false
}

type PTZMove struct {
	PanTiltX  float64
	PanTiltY  float64
	ZoomX     float64
	TimeoutMS int
}

func (c *Client) ContinuousMove(ctx context.Context, ptzXAddr, profileToken string, move PTZMove) error {
	ptzXAddr = strings.TrimSpace(ptzXAddr)
	profileToken = strings.TrimSpace(profileToken)
	if ptzXAddr == "" {
		return fmt.Errorf("ONVIF PTZ xaddr 为空")
	}
	if profileToken == "" {
		return fmt.Errorf("ONVIF PTZ profile token 为空")
	}

	velocity := &onvifgo.PTZSpeed{}
	if move.PanTiltX != 0 || move.PanTiltY != 0 {
		velocity.PanTilt = &onvifgo.Vector2D{
			X: clampUnit(move.PanTiltX),
			Y: clampUnit(move.PanTiltY),
		}
	}
	if move.ZoomX != 0 {
		velocity.Zoom = &onvifgo.Vector1D{
			X: clampUnit(move.ZoomX),
		}
	}
	if velocity.PanTilt == nil && velocity.Zoom == nil {
		return fmt.Errorf("ONVIF PTZ 移动速度不能全为 0")
	}

	backend, err := c.initializedBackend(ctx, ptzXAddr)
	if err != nil {
		return err
	}

	timeout := formatPTZTimeout(move.TimeoutMS)
	return backend.ContinuousMove(ctx, profileToken, velocity, &timeout)
}

func (c *Client) Stop(ctx context.Context, ptzXAddr, profileToken string, panTilt, zoom bool) error {
	ptzXAddr = strings.TrimSpace(ptzXAddr)
	profileToken = strings.TrimSpace(profileToken)
	if ptzXAddr == "" {
		return fmt.Errorf("ONVIF PTZ xaddr 为空")
	}
	if profileToken == "" {
		return fmt.Errorf("ONVIF PTZ profile token 为空")
	}

	backend, err := c.initializedBackend(ctx, ptzXAddr)
	if err != nil {
		return err
	}
	return backend.Stop(ctx, profileToken, panTilt, zoom)
}

func (c *Client) newBackend(endpoint string) (*onvifgo.Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("ONVIF endpoint 为空")
	}

	options := make([]onvifgo.ClientOption, 0, 2)
	options = append(options, onvifgo.WithHTTPClient(c.httpClient()))
	if c.Username != "" || c.Password != "" {
		options = append(options, onvifgo.WithCredentials(c.Username, c.Password))
	}

	backend, err := onvifgo.NewClient(endpoint, options...)
	if err != nil {
		return nil, fmt.Errorf("创建 ONVIF client 失败: %w", err)
	}
	return backend, nil
}

func (c *Client) httpClient() *http.Client {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultProbeTimeout}
	}
	if c.Username == "" && c.Password == "" {
		return httpClient
	}

	clone := *httpClient
	clone.Transport = &httpAuthTransport{
		base:     roundTripperOrDefault(httpClient.Transport),
		username: c.Username,
		password: c.Password,
	}
	return &clone
}

func roundTripperOrDefault(base http.RoundTripper) http.RoundTripper {
	if base != nil {
		return base
	}
	return http.DefaultTransport
}

type httpAuthTransport struct {
	base     http.RoundTripper
	username string
	password string

	mu sync.Mutex
	nc int
}

func (t *httpAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	firstReq, err := cloneRequestWithBody(req)
	if err != nil {
		return nil, err
	}
	firstReq.SetBasicAuth(t.username, t.password)

	resp, err := t.base.RoundTrip(firstReq)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(strings.ToLower(challenge), "digest") {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	retryReq, err := cloneRequestWithBody(req)
	if err != nil {
		return nil, err
	}
	retryReq.Header.Set("Authorization", t.digestAuthorization(retryReq, challenge))
	return t.base.RoundTrip(retryReq)
}

func cloneRequestWithBody(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil || req.GetBody == nil {
		return clone, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("复制 ONVIF HTTP 请求体失败: %w", err)
	}
	clone.Body = body
	return clone, nil
}

func (t *httpAuthTransport) digestAuthorization(req *http.Request, challenge string) string {
	realm := authChallengeParam(challenge, "realm")
	nonce := authChallengeParam(challenge, "nonce")
	opaque := authChallengeParam(challenge, "opaque")
	qop := authChallengeParam(challenge, "qop")
	useQOPAuth := strings.Contains(qop, "auth")

	uri := req.URL.RequestURI()
	ha1 := md5Hex(t.username + ":" + realm + ":" + t.password)
	ha2 := md5Hex(req.Method + ":" + uri)

	t.mu.Lock()
	t.nc++
	nc := t.nc
	t.mu.Unlock()

	ncText := fmt.Sprintf("%08x", nc)
	cnonce := randomHex(16)

	var response string
	if useQOPAuth {
		response = md5Hex(ha1 + ":" + nonce + ":" + ncText + ":" + cnonce + ":auth:" + ha2)
	} else {
		response = md5Hex(ha1 + ":" + nonce + ":" + ha2)
	}

	header := fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`,
		t.username, realm, nonce, uri, response)
	if opaque != "" {
		header += fmt.Sprintf(`, opaque=%q`, opaque)
	}
	if useQOPAuth {
		header += fmt.Sprintf(`, qop=auth, nc=%s, cnonce=%q`, ncText, cnonce)
	}
	return header
}

func authChallengeParam(challenge, param string) string {
	prefix := param + `="`
	idx := strings.Index(challenge, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(challenge[start:], `"`)
	if end < 0 {
		return ""
	}
	return challenge[start : start+end]
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

type initializedClientEntry struct {
	client *onvifgo.Client
}

var initializedClientCache sync.Map

func (c *Client) initializedBackend(ctx context.Context, serviceXAddr string) (*onvifgo.Client, error) {
	key := c.initializedClientKey(serviceXAddr)
	if value, ok := initializedClientCache.Load(key); ok {
		return value.(initializedClientEntry).client, nil
	}

	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return nil, err
	}
	if err := backend.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("初始化 ONVIF 服务端点失败: %w", err)
	}

	entry := initializedClientEntry{client: backend}
	actual, _ := initializedClientCache.LoadOrStore(key, entry)
	return actual.(initializedClientEntry).client, nil
}

func (c *Client) initializedClientKey(serviceXAddr string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(c.Endpoint),
		strings.TrimSpace(c.Username),
		c.Password,
		strings.TrimSpace(serviceXAddr),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func normalizeServiceXAddr(endpoint, serviceXAddr string) string {
	serviceXAddr = strings.TrimSpace(serviceXAddr)
	if serviceXAddr == "" {
		return ""
	}

	serviceURL, err := url.Parse(serviceXAddr)
	if err != nil || serviceURL.Host == "" {
		return serviceXAddr
	}
	if !isLoopbackHost(serviceURL.Hostname()) {
		return serviceXAddr
	}

	endpointURL, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || endpointURL.Hostname() == "" {
		return serviceXAddr
	}

	port := serviceURL.Port()
	if port == "" {
		port = endpointURL.Port()
	}
	serviceURL.Host = hostWithOptionalPort(endpointURL.Hostname(), port)
	return serviceURL.String()
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0" || host == "::1"
}

func hostWithOptionalPort(host, port string) string {
	if port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func clampUnit(value float64) float64 {
	if value > 1 {
		return 1
	}
	if value < -1 {
		return -1
	}
	return value
}

func formatPTZTimeout(timeoutMS int) string {
	if timeoutMS <= 0 {
		timeoutMS = 800
	}
	if timeoutMS < 100 {
		timeoutMS = 100
	}
	return fmt.Sprintf("PT%.1fS", float64(timeoutMS)/1000)
}
