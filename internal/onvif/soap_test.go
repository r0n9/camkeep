package onvif

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetCapabilitiesSendsSOAPAndParsesResponse(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/soap+xml") {
			t.Fatalf("expected SOAP content type, got %q", got)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(capabilitiesSOAPResponse))
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL,
		Username:   "admin",
		Password:   "secret",
		HTTPClient: server.Client(),
	}
	caps, err := client.GetCapabilities(context.Background())
	if err != nil {
		t.Fatalf("expected capability probe to pass, got %v", err)
	}

	if !strings.Contains(gotBody, "GetCapabilities") {
		t.Fatalf("expected GetCapabilities request, got %s", gotBody)
	}
	if !strings.Contains(gotBody, "UsernameToken") || !strings.Contains(gotBody, ">admin<") {
		t.Fatalf("expected WS-Security username token, got %s", gotBody)
	}
	if strings.Contains(gotBody, "secret") {
		t.Fatal("expected password digest request not to include plaintext password")
	}

	if caps.MediaXAddr != "http://camera/onvif/media" {
		t.Fatalf("unexpected media xaddr: %q", caps.MediaXAddr)
	}
	if caps.PTZXAddr != "http://camera/onvif/ptz" {
		t.Fatalf("unexpected PTZ xaddr: %q", caps.PTZXAddr)
	}
	if caps.ImagingXAddr != "http://camera/onvif/imaging" {
		t.Fatalf("unexpected imaging xaddr: %q", caps.ImagingXAddr)
	}
	if caps.EventXAddr != "http://camera/onvif/events" || !caps.PullPointSupport {
		t.Fatalf("unexpected event capabilities: %+v", caps)
	}
}

func TestGetCapabilitiesSendsHTTPBasicAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(capabilitiesSOAPResponse))
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL,
		Username:   "admin",
		Password:   "secret",
		HTTPClient: server.Client(),
	}
	if _, err := client.GetCapabilities(context.Background()); err != nil {
		t.Fatalf("expected capability probe to pass, got %v", err)
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	if gotAuth != want {
		t.Fatalf("unexpected basic auth header: got %q want %q", gotAuth, want)
	}
}

func TestGetCapabilitiesRetriesHTTPDigestAuth(t *testing.T) {
	var requestCount int
	var gotDigestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Digest ") {
			w.Header().Set("WWW-Authenticate", `Digest realm="camkeep-test", nonce="abcdef", qop="auth", opaque="opaque-token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		gotDigestBody = string(body)
		if !strings.Contains(auth, `username="admin"`) || !strings.Contains(auth, `qop=auth`) {
			t.Fatalf("unexpected digest auth header: %s", auth)
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(capabilitiesSOAPResponse))
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL,
		Username:   "admin",
		Password:   "secret",
		HTTPClient: server.Client(),
	}
	if _, err := client.GetCapabilities(context.Background()); err != nil {
		t.Fatalf("expected capability probe to pass, got %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected digest retry to use two requests, got %d", requestCount)
	}
	if !strings.Contains(gotDigestBody, "GetCapabilities") {
		t.Fatalf("expected retried request body to be preserved, got %s", gotDigestBody)
	}
}

func TestGetCapabilitiesReportsSOAPFaultHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <s:Fault>
      <s:Code><s:Value>s:Sender</s:Value></s:Code>
      <s:Reason><s:Text>bad credentials</s:Text></s:Reason>
    </s:Fault>
  </s:Body>
</s:Envelope>`))
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	_, err := client.GetCapabilities(context.Background())
	if err == nil {
		t.Fatal("expected SOAP fault to fail")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected fault reason in error, got %v", err)
	}
}

func TestGetProfilesSelectsPTZProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "GetProfiles") {
			t.Fatalf("expected GetProfiles request, got %s", body)
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <trt:GetProfilesResponse>
      <trt:Profiles token="plain">
        <tt:Name>Plain</tt:Name>
      </trt:Profiles>
      <trt:Profiles token="ptz-main">
        <tt:Name>Main</tt:Name>
        <tt:VideoSourceConfiguration>
          <tt:SourceToken>video_1</tt:SourceToken>
        </tt:VideoSourceConfiguration>
        <tt:PTZConfiguration></tt:PTZConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`))
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	profiles, err := client.GetProfiles(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("expected profiles parse to pass, got %v", err)
	}

	profile, ok := SelectPTZProfile(profiles)
	if !ok {
		t.Fatal("expected a PTZ profile to be selected")
	}
	if profile.Token != "ptz-main" || profile.Name != "Main" {
		t.Fatalf("unexpected selected profile: %+v", profile)
	}
	if profile.VideoSourceToken != "video_1" {
		t.Fatalf("unexpected selected profile source token: %+v", profile)
	}
}

func TestGetProfilesFallsBackToInitializedMediaEndpoint(t *testing.T) {
	var sawDirectMediaRequest bool
	var sawInitializedMediaRequest bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		w.Header().Set("Content-Type", "application/soap+xml")

		switch {
		case strings.Contains(bodyText, "GetProfiles") && r.URL.Path == "/media":
			sawDirectMediaRequest = true
			http.Error(w, "direct media endpoint rejected GetProfiles", http.StatusBadGateway)
		case strings.Contains(bodyText, "GetCapabilities"):
			_, _ = w.Write([]byte(capabilitiesSOAPResponseForURL(server.URL + "/media-discovered")))
		case strings.Contains(bodyText, "GetProfiles") && r.URL.Path == "/media-discovered":
			sawInitializedMediaRequest = true
			_, _ = w.Write([]byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <trt:GetProfilesResponse>
      <trt:Profiles token="initialized-ptz">
        <tt:Name>Initialized PTZ</tt:Name>
        <tt:VideoSourceConfiguration>
          <tt:SourceToken>video_1</tt:SourceToken>
        </tt:VideoSourceConfiguration>
        <tt:PTZConfiguration></tt:PTZConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`))
		default:
			t.Fatalf("unexpected ONVIF request path=%s body=%s", r.URL.Path, bodyText)
		}
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL + "/device_service", HTTPClient: server.Client()}
	profiles, err := client.GetProfiles(context.Background(), server.URL+"/media")
	if err != nil {
		t.Fatalf("expected initialized media fallback to pass, got %v", err)
	}
	if !sawDirectMediaRequest || !sawInitializedMediaRequest {
		t.Fatalf("expected both direct and initialized media requests, direct=%t initialized=%t", sawDirectMediaRequest, sawInitializedMediaRequest)
	}

	profile, ok := SelectPTZProfile(profiles)
	if !ok {
		t.Fatal("expected a PTZ profile to be selected")
	}
	if profile.Token != "initialized-ptz" || profile.VideoSourceToken != "video_1" {
		t.Fatalf("unexpected selected profile: %+v", profile)
	}
}

func TestSelectVideoSourceProfileFallsBackToProfileWithoutPTZ(t *testing.T) {
	profile, ok := SelectVideoSourceProfile([]MediaProfile{
		{Token: "ptz-only", HasPTZ: true},
		{Token: "video-only", VideoSourceToken: "video_1"},
	})
	if !ok {
		t.Fatal("expected a profile to be selected")
	}
	if profile.Token != "video-only" || profile.VideoSourceToken != "video_1" {
		t.Fatalf("expected profile with video source token, got %+v", profile)
	}
}

func TestContinuousMoveSendsPTZVelocity(t *testing.T) {
	var gotBody string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		w.Header().Set("Content-Type", "application/soap+xml")
		if strings.Contains(bodyText, "GetCapabilities") {
			_, _ = w.Write([]byte(capabilitiesSOAPResponseForURL(server.URL)))
			return
		}
		if strings.Contains(bodyText, "ContinuousMove") {
			gotBody = bodyText
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><ContinuousMoveResponse/></s:Body></s:Envelope>`))
			return
		}
		t.Fatalf("unexpected ONVIF request: %s", bodyText)
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	err := client.ContinuousMove(context.Background(), server.URL, "profile_1", PTZMove{PanTiltX: 0.5, PanTiltY: -0.25, ZoomX: 0.75, TimeoutMS: 900})
	if err != nil {
		t.Fatalf("expected PTZ move to pass, got %v", err)
	}
	for _, want := range []string{"ContinuousMove", "profile_1", `x="0.5"`, `y="-0.25"`, `x="0.75"`, "PT0.9S"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("expected PTZ request to contain %q, got %s", want, gotBody)
		}
	}
}

func TestRelativeFocusSendsImagingMove(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:MoveResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, Username: "admin", Password: "secret", HTTPClient: server.Client()}
	if err := client.RelativeFocus(context.Background(), server.URL, "video_1", -0.08, 0.5); err != nil {
		t.Fatalf("expected imaging move to pass, got %v", err)
	}
	for _, want := range []string{"Move", "video_1", "onvif:Relative", "onvif:Distance", "-0.08", "onvif:Speed", ">0.5<", "UsernameToken"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("expected imaging move request to contain %q, got %s", want, gotBody)
		}
	}
}

func TestAdjustFocusUsesRelativeMoveOptions(t *testing.T) {
	var gotBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		gotBodies = append(gotBodies, bodyText)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(bodyText, "GetMoveOptions"):
			_, _ = w.Write([]byte(focusRelativeMoveOptionsSOAPResponse()))
		case strings.Contains(bodyText, "Move"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:MoveResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		default:
			t.Fatalf("unexpected ONVIF request: %s", bodyText)
		}
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	if err := client.AdjustFocus(context.Background(), server.URL, "video_1", -1, 0.5, 0.8); err != nil {
		t.Fatalf("expected adaptive focus to pass, got %v", err)
	}

	joined := strings.Join(gotBodies, "\n")
	for _, want := range []string{"GetMoveOptions", "onvif:Relative", "onvif:Distance>-0.3<", "onvif:Speed>0.4<"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected relative focus flow to contain %q, got %s", want, joined)
		}
	}
}

func TestAdjustFocusFallsBackToContinuousMoveOptions(t *testing.T) {
	var gotBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		gotBodies = append(gotBodies, bodyText)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(bodyText, "GetMoveOptions"):
			_, _ = w.Write([]byte(focusContinuousMoveOptionsSOAPResponse()))
		case strings.Contains(bodyText, "Move"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:MoveResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		case strings.Contains(bodyText, "Stop"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:StopResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		default:
			t.Fatalf("unexpected ONVIF request: %s", bodyText)
		}
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	if err := client.AdjustFocus(context.Background(), server.URL, "video_1", 1, 0.08, 0.6); err != nil {
		t.Fatalf("expected adaptive continuous focus to pass, got %v", err)
	}

	joined := strings.Join(gotBodies, "\n")
	for _, want := range []string{"GetMoveOptions", "onvif:Continuous", "onvif:Speed>0.2<", "Stop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected continuous focus flow to contain %q, got %s", want, joined)
		}
	}
}

func TestAdjustFocusFallsBackToContinuousWhenRelativeFails(t *testing.T) {
	var gotBodies []string
	var relativeFailed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		gotBodies = append(gotBodies, bodyText)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(bodyText, "GetMoveOptions"):
			_, _ = w.Write([]byte(focusRelativeAndContinuousMoveOptionsSOAPResponse()))
		case strings.Contains(bodyText, "Move") && strings.Contains(bodyText, "Relative"):
			relativeFailed = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <s:Fault>
      <s:Code><s:Value>s:Sender</s:Value></s:Code>
      <s:Reason><s:Text>The requested settings are incorrect.</s:Text></s:Reason>
    </s:Fault>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(bodyText, "Move") && strings.Contains(bodyText, "Continuous"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:MoveResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		case strings.Contains(bodyText, "Stop"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:StopResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		default:
			t.Fatalf("unexpected ONVIF request: %s", bodyText)
		}
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	if err := client.AdjustFocus(context.Background(), server.URL, "video_1", 1, 0.08, 0.6); err != nil {
		t.Fatalf("expected continuous fallback to pass, got %v", err)
	}
	if !relativeFailed {
		t.Fatal("expected relative focus attempt to fail before fallback")
	}

	joined := strings.Join(gotBodies, "\n")
	for _, want := range []string{"onvif:Relative", "onvif:Continuous", "Stop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected fallback focus flow to contain %q, got %s", want, joined)
		}
	}
}

func TestAdjustIrisUpdatesExposureValue(t *testing.T) {
	var gotBodies []string
	var requestCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		gotBodies = append(gotBodies, bodyText)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(bodyText, "GetCapabilities"):
			_, _ = w.Write([]byte(capabilitiesSOAPResponseWithImaging(server.URL)))
		case strings.Contains(bodyText, "GetImagingSettings"):
			_, _ = w.Write([]byte(imagingSettingsSOAPResponse()))
		case strings.Contains(bodyText, "SetImagingSettings"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><timg:SetImagingSettingsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/></s:Body></s:Envelope>`))
		default:
			t.Fatalf("unexpected ONVIF request: %s", bodyText)
		}
	}))
	defer server.Close()

	client := &Client{Endpoint: server.URL, HTTPClient: server.Client()}
	if err := client.AdjustIris(context.Background(), server.URL, "video_1", 0.1); err != nil {
		t.Fatalf("expected iris adjustment to pass, got %v", err)
	}
	if requestCount != 3 {
		t.Fatalf("expected 3 imaging requests, got %d", requestCount)
	}
	joined := strings.Join(gotBodies, "\n")
	for _, want := range []string{"GetCapabilities", "GetImagingSettings", "SetImagingSettings", ">0.4<", "MANUAL"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected imaging request chain to contain %q, got %s", want, joined)
		}
	}
}

func capabilitiesSOAPResponseForURL(serviceURL string) string {
	return fmt.Sprintf(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tds:GetCapabilitiesResponse>
      <tds:Capabilities>
        <tt:Device>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:Device>
        <tt:Media>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:Media>
        <tt:Events>
          <tt:XAddr>%[1]s</tt:XAddr>
          <tt:WSPullPointSupport>true</tt:WSPullPointSupport>
        </tt:Events>
        <tt:PTZ>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:PTZ>
        <tt:Imaging>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:Imaging>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`, serviceURL)
}

const capabilitiesSOAPResponse = `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tds:GetCapabilitiesResponse>
      <tds:Capabilities>
        <tt:Device>
          <tt:XAddr>http://camera/onvif/device_service</tt:XAddr>
        </tt:Device>
        <tt:Media>
          <tt:XAddr>http://camera/onvif/media</tt:XAddr>
        </tt:Media>
        <tt:Events>
          <tt:XAddr>http://camera/onvif/events</tt:XAddr>
          <tt:WSPullPointSupport>true</tt:WSPullPointSupport>
        </tt:Events>
        <tt:PTZ>
          <tt:XAddr>http://camera/onvif/ptz</tt:XAddr>
        </tt:PTZ>
        <tt:Imaging>
          <tt:XAddr>http://camera/onvif/imaging</tt:XAddr>
        </tt:Imaging>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`

func capabilitiesSOAPResponseWithImaging(serviceURL string) string {
	return fmt.Sprintf(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tds:GetCapabilitiesResponse>
      <tds:Capabilities>
        <tt:Media>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:Media>
        <tt:Imaging>
          <tt:XAddr>%[1]s</tt:XAddr>
        </tt:Imaging>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`, serviceURL)
}

func focusRelativeMoveOptionsSOAPResponse() string {
	return `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <timg:GetMoveOptionsResponse>
      <timg:MoveOptions>
        <tt:Relative>
          <tt:Distance>
            <tt:Min>0.1</tt:Min>
            <tt:Max>0.3</tt:Max>
          </tt:Distance>
          <tt:Speed>
            <tt:Min>0.2</tt:Min>
            <tt:Max>0.4</tt:Max>
          </tt:Speed>
        </tt:Relative>
      </timg:MoveOptions>
    </timg:GetMoveOptionsResponse>
  </s:Body>
</s:Envelope>`
}

func focusContinuousMoveOptionsSOAPResponse() string {
	return `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <timg:GetMoveOptionsResponse>
      <timg:MoveOptions>
        <tt:Continuous>
          <tt:Speed>
            <tt:Min>0.1</tt:Min>
            <tt:Max>0.2</tt:Max>
          </tt:Speed>
        </tt:Continuous>
      </timg:MoveOptions>
    </timg:GetMoveOptionsResponse>
  </s:Body>
</s:Envelope>`
}

func focusRelativeAndContinuousMoveOptionsSOAPResponse() string {
	return `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <timg:GetMoveOptionsResponse>
      <timg:MoveOptions>
        <tt:Relative>
          <tt:Distance>
            <tt:Min>-1</tt:Min>
            <tt:Max>1</tt:Max>
          </tt:Distance>
          <tt:Speed>
            <tt:Min>0.1</tt:Min>
            <tt:Max>1</tt:Max>
          </tt:Speed>
        </tt:Relative>
        <tt:Continuous>
          <tt:Speed>
            <tt:Min>-1</tt:Min>
            <tt:Max>1</tt:Max>
          </tt:Speed>
        </tt:Continuous>
      </timg:MoveOptions>
    </timg:GetMoveOptionsResponse>
  </s:Body>
</s:Envelope>`
}

func imagingSettingsSOAPResponse() string {
	return `
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <timg:GetImagingSettingsResponse>
      <timg:ImagingSettings>
        <tt:Exposure>
          <tt:Mode>AUTO</tt:Mode>
          <tt:MinIris>0</tt:MinIris>
          <tt:MaxIris>1</tt:MaxIris>
          <tt:Iris>0.3</tt:Iris>
        </tt:Exposure>
      </timg:ImagingSettings>
    </timg:GetImagingSettingsResponse>
  </s:Body>
</s:Envelope>`
}
