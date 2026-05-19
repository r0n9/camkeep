package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // SHA1 is required by ONVIF WS-Security digest
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	onvifgo "github.com/0x524a/onvif-go"
)

const (
	imagingNamespace     = "http://www.onvif.org/ver20/imaging/wsdl"
	onvifSchemaNamespace = "http://www.onvif.org/ver10/schema"

	defaultFocusNudgeDuration = 350 * time.Millisecond
)

type imagingMoveRequest struct {
	XMLName          xml.Name `xml:"timg:Move"`
	Xmlns            string   `xml:"xmlns:timg,attr"`
	XmlnsONVIF       string   `xml:"xmlns:onvif,attr,omitempty"`
	VideoSourceToken string   `xml:"timg:VideoSourceToken"`
	Focus            *imagingFocusMove
}

type imagingFocusMove struct {
	XMLName    xml.Name                `xml:"timg:Focus"`
	Relative   *imagingRelativeFocus   `xml:"onvif:Relative,omitempty"`
	Continuous *imagingContinuousFocus `xml:"onvif:Continuous,omitempty"`
}

type imagingRelativeFocus struct {
	Distance float64  `xml:"onvif:Distance"`
	Speed    *float64 `xml:"onvif:Speed,omitempty"`
}

type imagingContinuousFocus struct {
	Speed float64 `xml:"onvif:Speed"`
}

type imagingStopRequest struct {
	XMLName          xml.Name `xml:"timg:Stop"`
	Xmlns            string   `xml:"xmlns:timg,attr"`
	VideoSourceToken string   `xml:"timg:VideoSourceToken"`
}

type imagingGetMoveOptionsRequest struct {
	XMLName          xml.Name `xml:"timg:GetMoveOptions"`
	Xmlns            string   `xml:"xmlns:timg,attr"`
	VideoSourceToken string   `xml:"timg:VideoSourceToken"`
}

type imagingGetMoveOptionsResponse struct {
	XMLName     xml.Name `xml:"GetMoveOptionsResponse"`
	MoveOptions struct {
		Relative *struct {
			Distance struct {
				Min float64 `xml:"Min"`
				Max float64 `xml:"Max"`
			} `xml:"Distance"`
			Speed struct {
				Min float64 `xml:"Min"`
				Max float64 `xml:"Max"`
			} `xml:"Speed"`
		} `xml:"Relative"`
		Continuous *struct {
			Speed struct {
				Min float64 `xml:"Min"`
				Max float64 `xml:"Max"`
			} `xml:"Speed"`
		} `xml:"Continuous"`
	} `xml:"MoveOptions"`
}

type focusMoveOptions struct {
	Relative   *relativeFocusMoveOptions
	Continuous *continuousFocusMoveOptions
}

type relativeFocusMoveOptions struct {
	Distance floatRange
	Speed    floatRange
}

type continuousFocusMoveOptions struct {
	Speed floatRange
}

type floatRange struct {
	Min float64
	Max float64
}

type soapEnvelope struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  *soapHeader `xml:"http://www.w3.org/2003/05/soap-envelope Header,omitempty"`
	Body    soapBody    `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

type soapHeader struct {
	Security *soapSecurity `xml:"Security,omitempty"`
}

type soapBody struct {
	Content interface{} `xml:",omitempty"`
}

type soapSecurity struct {
	XMLName        xml.Name           `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Security"`
	MustUnderstand string             `xml:"http://www.w3.org/2003/05/soap-envelope mustUnderstand,attr,omitempty"`
	UsernameToken  *soapUsernameToken `xml:"UsernameToken,omitempty"`
}

type soapUsernameToken struct {
	XMLName  xml.Name     `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd UsernameToken"`
	Username string       `xml:"Username"`
	Password soapPassword `xml:"Password"`
	Nonce    soapNonce    `xml:"Nonce"`
	Created  string       `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd Created"`
}

type soapPassword struct {
	Type     string `xml:"Type,attr"`
	Password string `xml:",chardata"`
}

type soapNonce struct {
	Type  string `xml:"EncodingType,attr"`
	Nonce string `xml:",chardata"`
}

type soapFault struct {
	Code   string `xml:"Code>Value"`
	Reason string `xml:"Reason>Text"`
	Detail string `xml:"Detail,omitempty"`
}

// AdjustFocus moves focus using the focus operation supported by the camera.
func (c *Client) AdjustFocus(ctx context.Context, imagingXAddr, videoSourceToken string, direction, step, speed float64) error {
	imagingXAddr = strings.TrimSpace(imagingXAddr)
	videoSourceToken = strings.TrimSpace(videoSourceToken)
	if imagingXAddr == "" {
		return fmt.Errorf("ONVIF Imaging xaddr 为空")
	}
	if videoSourceToken == "" {
		return fmt.Errorf("ONVIF video source token 为空")
	}
	direction = clampUnit(direction)
	if direction == 0 {
		return fmt.Errorf("ONVIF 对焦方向不能为 0")
	}
	step = clampAbsUnit(step)
	if step == 0 {
		step = 0.08
	}
	speed = clampAbsUnit(speed)
	if speed == 0 {
		speed = 0.5
	}

	options, err := c.getFocusMoveOptions(ctx, imagingXAddr, videoSourceToken)
	if err == nil {
		var relativeErr error
		if options.Relative != nil {
			distance := signedFocusValue(direction, step, options.Relative.Distance)
			relativeSpeed := clampFocusValue(speed, options.Relative.Speed)
			if err := c.relativeFocus(ctx, imagingXAddr, videoSourceToken, distance, &relativeSpeed); err == nil {
				return nil
			} else {
				relativeErr = err
			}
		}
		if options.Continuous != nil {
			continuousSpeed := signedFocusValue(direction, speed, options.Continuous.Speed)
			if err := c.continuousFocusNudge(ctx, imagingXAddr, videoSourceToken, continuousSpeed, defaultFocusNudgeDuration); err == nil {
				return nil
			} else if relativeErr != nil {
				return fmt.Errorf("ONVIF 聚焦失败: Relative=%v; Continuous=%w", relativeErr, err)
			} else {
				return err
			}
		}
		if relativeErr != nil {
			return relativeErr
		}
		return fmt.Errorf("ONVIF 聚焦移动能力不可用")
	}

	distance := direction * step
	if err := c.relativeFocus(ctx, imagingXAddr, videoSourceToken, distance, nil); err == nil {
		return nil
	} else {
		continuousSpeed := direction * speed
		if continuousErr := c.continuousFocusNudge(ctx, imagingXAddr, videoSourceToken, continuousSpeed, defaultFocusNudgeDuration); continuousErr == nil {
			return nil
		} else {
			return fmt.Errorf("ONVIF 聚焦失败: Relative=%v; Continuous=%w", err, continuousErr)
		}
	}
}

// RelativeFocus moves focus relatively by the requested delta.
func (c *Client) RelativeFocus(ctx context.Context, imagingXAddr, videoSourceToken string, distance, speed float64) error {
	imagingXAddr = strings.TrimSpace(imagingXAddr)
	videoSourceToken = strings.TrimSpace(videoSourceToken)
	if imagingXAddr == "" {
		return fmt.Errorf("ONVIF Imaging xaddr 为空")
	}
	if videoSourceToken == "" {
		return fmt.Errorf("ONVIF video source token 为空")
	}
	if distance == 0 {
		return fmt.Errorf("ONVIF 对焦距离不能为 0")
	}
	speed = clampAbsUnit(speed)
	var speedPtr *float64
	if speed != 0 {
		speedPtr = &speed
	}
	return c.relativeFocus(ctx, imagingXAddr, videoSourceToken, distance, speedPtr)
}

func (c *Client) relativeFocus(ctx context.Context, imagingXAddr, videoSourceToken string, distance float64, speed *float64) error {
	req := imagingMoveRequest{
		Xmlns:            imagingNamespace,
		XmlnsONVIF:       onvifSchemaNamespace,
		VideoSourceToken: videoSourceToken,
		Focus: &imagingFocusMove{
			Relative: &imagingRelativeFocus{
				Distance: distance,
				Speed:    speed,
			},
		},
	}

	return c.soapCall(ctx, imagingXAddr, "Move", req, nil)
}

func (c *Client) continuousFocusNudge(ctx context.Context, imagingXAddr, videoSourceToken string, speed float64, duration time.Duration) error {
	if speed == 0 {
		return fmt.Errorf("ONVIF 连续聚焦速度不能为 0")
	}
	if duration <= 0 {
		duration = defaultFocusNudgeDuration
	}

	req := imagingMoveRequest{
		Xmlns:            imagingNamespace,
		XmlnsONVIF:       onvifSchemaNamespace,
		VideoSourceToken: videoSourceToken,
		Focus: &imagingFocusMove{
			Continuous: &imagingContinuousFocus{Speed: speed},
		},
	}
	if err := c.soapCall(ctx, imagingXAddr, "Move", req, nil); err != nil {
		return err
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return c.stopFocus(ctx, imagingXAddr, videoSourceToken)
}

func (c *Client) stopFocus(ctx context.Context, imagingXAddr, videoSourceToken string) error {
	req := imagingStopRequest{
		Xmlns:            imagingNamespace,
		VideoSourceToken: videoSourceToken,
	}
	return c.soapCall(ctx, imagingXAddr, "Stop", req, nil)
}

func (c *Client) getFocusMoveOptions(ctx context.Context, imagingXAddr, videoSourceToken string) (focusMoveOptions, error) {
	req := imagingGetMoveOptionsRequest{
		Xmlns:            imagingNamespace,
		VideoSourceToken: videoSourceToken,
	}

	var resp imagingGetMoveOptionsResponse
	if err := c.soapCall(ctx, imagingXAddr, "GetMoveOptions", req, &resp); err != nil {
		return focusMoveOptions{}, err
	}

	var options focusMoveOptions
	if resp.MoveOptions.Relative != nil {
		options.Relative = &relativeFocusMoveOptions{
			Distance: floatRange{
				Min: resp.MoveOptions.Relative.Distance.Min,
				Max: resp.MoveOptions.Relative.Distance.Max,
			},
			Speed: floatRange{
				Min: resp.MoveOptions.Relative.Speed.Min,
				Max: resp.MoveOptions.Relative.Speed.Max,
			},
		}
	}
	if resp.MoveOptions.Continuous != nil {
		options.Continuous = &continuousFocusMoveOptions{
			Speed: floatRange{
				Min: resp.MoveOptions.Continuous.Speed.Min,
				Max: resp.MoveOptions.Continuous.Speed.Max,
			},
		}
	}
	return options, nil
}

func signedFocusValue(direction, magnitude float64, valueRange floatRange) float64 {
	magnitude = clampFocusSignedMagnitude(direction, magnitude, valueRange)
	if direction < 0 {
		return -magnitude
	}
	return magnitude
}

func clampFocusSignedMagnitude(direction, magnitude float64, valueRange floatRange) float64 {
	magnitude = clampAbsUnit(magnitude)
	if valueRange.Max <= valueRange.Min {
		return magnitude
	}
	if direction < 0 && valueRange.Min < 0 {
		maxMagnitude := absFloat(valueRange.Min)
		minMagnitude := 0.0
		if valueRange.Max < 0 {
			minMagnitude = absFloat(valueRange.Max)
		}
		return clampFocusValue(magnitude, floatRange{Min: minMagnitude, Max: maxMagnitude})
	}
	if direction >= 0 && valueRange.Max > 0 {
		minMagnitude := 0.0
		if valueRange.Min > 0 {
			minMagnitude = valueRange.Min
		}
		return clampFocusValue(magnitude, floatRange{Min: minMagnitude, Max: valueRange.Max})
	}
	return clampFocusValue(magnitude, absRange(valueRange))
}

func clampFocusValue(value float64, valueRange floatRange) float64 {
	value = clampAbsUnit(value)
	minValue := valueRange.Min
	maxValue := valueRange.Max
	if maxValue <= minValue {
		return value
	}

	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func absRange(valueRange floatRange) floatRange {
	minValue := absFloat(valueRange.Min)
	maxValue := absFloat(valueRange.Max)
	if minValue > maxValue {
		minValue, maxValue = maxValue, minValue
	}
	if minValue == maxValue {
		minValue = 0
	}
	return floatRange{Min: minValue, Max: maxValue}
}

func clampAbsUnit(value float64) float64 {
	if value < 0 {
		value = -value
	}
	if value > 1 {
		return 1
	}
	return value
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

// AdjustIris adjusts iris/exposure by a relative delta and persists it only for the current session.
func (c *Client) AdjustIris(ctx context.Context, imagingXAddr, videoSourceToken string, delta float64) error {
	imagingXAddr = strings.TrimSpace(imagingXAddr)
	videoSourceToken = strings.TrimSpace(videoSourceToken)
	if imagingXAddr == "" {
		return fmt.Errorf("ONVIF Imaging xaddr 为空")
	}
	if videoSourceToken == "" {
		return fmt.Errorf("ONVIF video source token 为空")
	}
	if delta == 0 {
		return fmt.Errorf("ONVIF 光圈调整量不能为 0")
	}

	backend, err := c.initializedBackend(ctx, imagingXAddr)
	if err != nil {
		return err
	}

	settings, err := backend.GetImagingSettings(ctx, videoSourceToken)
	if err != nil {
		return fmt.Errorf("获取 ONVIF 图像设置失败: %w", err)
	}
	if settings == nil || settings.Exposure == nil {
		return fmt.Errorf("ONVIF 光圈配置不可用")
	}

	next := settings.Exposure.Iris + delta
	minIris := settings.Exposure.MinIris
	maxIris := settings.Exposure.MaxIris
	if maxIris > minIris {
		if next < minIris {
			next = minIris
		}
		if next > maxIris {
			next = maxIris
		}
	} else {
		if next < 0 {
			next = 0
		}
		if next > 1 {
			next = 1
		}
	}

	exposureMode := strings.TrimSpace(settings.Exposure.Mode)
	if exposureMode == "" || strings.EqualFold(exposureMode, "auto") {
		exposureMode = "MANUAL"
	}

	nextSettings := &onvifgo.ImagingSettings{
		Exposure: &onvifgo.Exposure{
			Mode: exposureMode,
			Iris: next,
		},
	}

	if err := backend.SetImagingSettings(ctx, videoSourceToken, nextSettings, false); err != nil {
		return fmt.Errorf("调整 ONVIF 光圈失败: %w", err)
	}
	return nil
}

func (c *Client) soapCall(ctx context.Context, endpoint string, operation string, request interface{}, response interface{}) error {
	body, err := c.soapEnvelopeBody(request)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 ONVIF SOAP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	if operation != "" {
		req.Header.Set("SOAPAction", operation)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("发送 ONVIF SOAP 请求失败: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 ONVIF SOAP 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if fault := parseSOAPFault(respBody); fault != "" {
			return fmt.Errorf("%s failed: %s", operation, fault)
		}
		return fmt.Errorf("%s failed with status %d: %s", operation, resp.StatusCode, string(respBody))
	}

	if response == nil {
		return nil
	}

	var envelope struct {
		Body struct {
			Content []byte `xml:",innerxml"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("解析 ONVIF SOAP 响应失败: %w", err)
	}
	if err := xml.Unmarshal(envelope.Body.Content, response); err != nil {
		return fmt.Errorf("解析 ONVIF SOAP 响应失败: %w", err)
	}
	return nil
}

func (c *Client) soapEnvelopeBody(request interface{}) ([]byte, error) {
	envelope := soapEnvelope{
		Body: soapBody{Content: request},
	}
	if c.Username != "" || c.Password != "" {
		envelope.Header = &soapHeader{Security: c.soapSecurityHeader()}
	}

	body, err := xml.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 ONVIF SOAP 请求失败: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

func (c *Client) soapSecurityHeader() *soapSecurity {
	const nonceSize = 16
	nonceBytes := make([]byte, nonceSize)
	_, _ = rand.Read(nonceBytes)
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	created := time.Now().UTC().Format(time.RFC3339)

	hash := sha1.New() //nolint:gosec // ONVIF WS-Security digest requires SHA1
	hash.Write(nonceBytes)
	hash.Write([]byte(created))
	hash.Write([]byte(c.Password))
	digest := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	return &soapSecurity{
		MustUnderstand: "1",
		UsernameToken: &soapUsernameToken{
			Username: c.Username,
			Password: soapPassword{
				Type:     "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest",
				Password: digest,
			},
			Nonce: soapNonce{
				Type:  "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary",
				Nonce: nonce,
			},
			Created: created,
		},
	}
}

func parseSOAPFault(body []byte) string {
	var envelope struct {
		Body struct {
			Fault *soapFault `xml:"Fault"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	if envelope.Body.Fault == nil {
		return ""
	}
	if reason := strings.TrimSpace(envelope.Body.Fault.Reason); reason != "" {
		return reason
	}
	if code := strings.TrimSpace(envelope.Body.Fault.Code); code != "" {
		return code
	}
	return strings.TrimSpace(envelope.Body.Fault.Detail)
}
