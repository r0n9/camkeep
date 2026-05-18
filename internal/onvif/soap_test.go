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
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`
