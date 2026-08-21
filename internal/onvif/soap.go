package onvif

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
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

const ptzNamespace = "http://www.onvif.org/ver20/ptz/wsdl"

type Capabilities struct {
	DeviceXAddr       string
	MediaXAddr        string
	PTZXAddr          string
	ImagingXAddr      string
	EventXAddr        string
	PullPointSupport  bool
	RawEventSupported bool
	ProfileToken      string
	ProfileName       string
	VideoSourceToken  string
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
	if result.DeviceXAddr == "" && result.MediaXAddr == "" && result.PTZXAddr == "" && result.ImagingXAddr == "" && result.EventXAddr == "" {
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
	if caps.Imaging != nil {
		result.ImagingXAddr = normalizeServiceXAddr(endpoint, caps.Imaging.XAddr)
	}
	if caps.Events != nil {
		result.EventXAddr = normalizeServiceXAddr(endpoint, caps.Events.XAddr)
		result.PullPointSupport = caps.Events.WSPullPointSupport
		result.RawEventSupported = result.EventXAddr != ""
	}
	return result
}

type MediaProfile struct {
	Token            string
	Name             string
	VideoSourceToken string
	HasPTZ           bool
}

func (c *Client) GetProfiles(ctx context.Context, mediaXAddr string) ([]MediaProfile, error) {
	mediaXAddr = strings.TrimSpace(mediaXAddr)
	if mediaXAddr == "" {
		return nil, fmt.Errorf("ONVIF media xaddr 为空")
	}

	profiles, err := c.getProfilesFromEndpoint(ctx, mediaXAddr)
	if err == nil {
		return profiles, nil
	}
	directErr := err

	backend, err := c.initializedBackend(ctx, mediaXAddr)
	if err != nil {
		return nil, fmt.Errorf("ONVIF GetProfiles 失败: %v; 初始化服务端点失败: %w", directErr, err)
	}
	profiles, err = c.getProfilesFromBackend(ctx, backend)
	if err != nil {
		return nil, fmt.Errorf("ONVIF GetProfiles 失败: %v; 初始化服务端点后仍失败: %w", directErr, err)
	}
	return profiles, nil
}

func (c *Client) getProfilesFromEndpoint(ctx context.Context, mediaXAddr string) ([]MediaProfile, error) {
	backend, err := c.newBackend(mediaXAddr)
	if err != nil {
		return nil, err
	}

	return c.getProfilesFromBackend(ctx, backend)
}

func (c *Client) getProfilesFromBackend(ctx context.Context, backend *onvifgo.Client) ([]MediaProfile, error) {
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
			Token:            strings.TrimSpace(profile.Token),
			Name:             strings.TrimSpace(profile.Name),
			VideoSourceToken: profileVideoSourceToken(profile),
			HasPTZ:           profile.PTZConfiguration != nil,
		})
	}
	return result, nil
}

func profileVideoSourceToken(profile *onvifgo.Profile) string {
	if profile == nil || profile.VideoSourceConfiguration == nil {
		return ""
	}
	return strings.TrimSpace(profile.VideoSourceConfiguration.SourceToken)
}

func SelectPTZProfile(profiles []MediaProfile) (MediaProfile, bool) {
	for _, profile := range profiles {
		if profile.Token != "" && profile.HasPTZ && profile.VideoSourceToken != "" {
			return profile, true
		}
	}
	for _, profile := range profiles {
		if profile.Token != "" && profile.HasPTZ {
			return profile, true
		}
	}
	for _, profile := range profiles {
		if profile.Token != "" && profile.VideoSourceToken != "" {
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

func SelectVideoSourceProfile(profiles []MediaProfile) (MediaProfile, bool) {
	for _, profile := range profiles {
		if profile.VideoSourceToken != "" {
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

// ONVIF 规定 Velocity 下的 PanTilt/Zoom 必须属于 ver10/schema 命名空间（惯例前缀 tt/onvif）。
// 这里手写请求体而不是走 onvif-go：该库把这两个元素序列化成无前缀的 <PanTilt>，
// 会被 SOAP 信封的默认命名空间吞掉，大华等严格设备解析出空 Velocity，
// 于是照常回 200 却不转动。
type ptzContinuousMoveRequest struct {
	XMLName      xml.Name     `xml:"tptz:ContinuousMove"`
	Xmlns        string       `xml:"xmlns:tptz,attr"`
	XmlnsONVIF   string       `xml:"xmlns:onvif,attr"`
	ProfileToken string       `xml:"tptz:ProfileToken"`
	Velocity     *ptzVelocity `xml:"tptz:Velocity"`
	Timeout      string       `xml:"tptz:Timeout,omitempty"`
}

type ptzVelocity struct {
	PanTilt *ptzVector2D `xml:"onvif:PanTilt,omitempty"`
	Zoom    *ptzVector1D `xml:"onvif:Zoom,omitempty"`
}

type ptzVector2D struct {
	X float64 `xml:"x,attr"`
	Y float64 `xml:"y,attr"`
}

type ptzVector1D struct {
	X float64 `xml:"x,attr"`
}

type ptzStopRequest struct {
	XMLName      xml.Name `xml:"tptz:Stop"`
	Xmlns        string   `xml:"xmlns:tptz,attr"`
	ProfileToken string   `xml:"tptz:ProfileToken"`
	PanTilt      *bool    `xml:"tptz:PanTilt,omitempty"`
	Zoom         *bool    `xml:"tptz:Zoom,omitempty"`
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

	velocity := &ptzVelocity{}
	if move.PanTiltX != 0 || move.PanTiltY != 0 {
		velocity.PanTilt = &ptzVector2D{
			X: clampUnit(move.PanTiltX),
			Y: clampUnit(move.PanTiltY),
		}
	}
	if move.ZoomX != 0 {
		velocity.Zoom = &ptzVector1D{
			X: clampUnit(move.ZoomX),
		}
	}
	if velocity.PanTilt == nil && velocity.Zoom == nil {
		return fmt.Errorf("ONVIF PTZ 移动速度不能全为 0")
	}

	req := ptzContinuousMoveRequest{
		Xmlns:        ptzNamespace,
		XmlnsONVIF:   onvifSchemaNamespace,
		ProfileToken: profileToken,
		Velocity:     velocity,
		Timeout:      formatPTZTimeout(move.TimeoutMS),
	}
	return c.soapCall(ctx, ptzXAddr, "ContinuousMove", req, nil)
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

	req := ptzStopRequest{
		Xmlns:        ptzNamespace,
		ProfileToken: profileToken,
	}
	if panTilt {
		req.PanTilt = &panTilt
	}
	if zoom {
		req.Zoom = &zoom
	}
	return c.soapCall(ctx, ptzXAddr, "Stop", req, nil)
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
	resp, err := t.roundTripOnce(req)
	if err != nil || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return resp, err
	}

	// 部分摄像头(如海康)URL 中使用的别名路径(如 /onvif/device)存在但不实现标准 ONVIF
	// 操作，会返回 ter:ActionNotSupported SOAP Fault。此时回退到默认设备服务路径
	// /onvif/device_service 重试一次，让能力探测、Initialize、PTZ/成像等操作都能工作。
	body, isFault := readActionNotSupportedFault(resp)
	if isFault && !isDefaultDeviceServicePath(req.URL.Path) {
		retryReq, cloneErr := cloneRequestWithBody(req)
		if cloneErr == nil {
			urlCopy := *req.URL
			urlCopy.Path = defaultDeviceServicePath
			urlCopy.RawPath = ""
			retryReq.URL = &urlCopy
			if retried, retryErr := t.roundTripOnce(retryReq); retryErr == nil {
				return retried, nil
			}
		}
	}
	// 恢复原始响应体(可能已被 Fault 检查消费)，按原样返回
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// roundTripOnce 发送一次请求：先带 HTTP Basic 认证，若收到 Digest 挑战则用 Digest 重试。
func (t *httpAuthTransport) roundTripOnce(req *http.Request) (*http.Response, error) {
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

// readActionNotSupportedFault 读取非 2xx 响应体，判断是否为 "服务路径存在但未实现该操作" 的
// SOAP Fault(ter:ActionNotSupported / ter:InvalidOperation 子码)。
func readActionNotSupportedFault(resp *http.Response) ([]byte, bool) {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return body, false
	}
	isFault := bytes.Contains(body, []byte("ActionNotSupported")) || bytes.Contains(body, []byte("InvalidOperation"))
	return body, isFault
}

// isDefaultDeviceServicePath 判断请求路径是否为标准 ONVIF 设备服务路径。
func isDefaultDeviceServicePath(path string) bool {
	return strings.TrimSpace(path) == defaultDeviceServicePath
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
	// Timeout 是 xs:duration。小数秒虽然合法，但部分设备（大华在内）会把 PT0.8S
	// 截断成 0 而立刻停转，这里统一向上取整到整秒。前端按 MOVE_RENEW_MS 续期、
	// 松手时显式 Stop，取整只影响这个兜底上限，不影响实际手感。
	seconds := (timeoutMS + 999) / 1000
	return fmt.Sprintf("PT%dS", seconds)
}
