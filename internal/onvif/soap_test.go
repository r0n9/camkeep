package onvif

import (
	"context"
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
	if !strings.Contains(gotBody, "UsernameToken") || !strings.Contains(gotBody, "<wsse:Username>admin</wsse:Username>") {
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

func TestParseCapabilitiesResponseHandlesSOAPFault(t *testing.T) {
	_, err := ParseCapabilitiesResponse([]byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <s:Fault>
      <s:Code><s:Value>s:Sender</s:Value></s:Code>
      <s:Reason><s:Text>bad credentials</s:Text></s:Reason>
    </s:Fault>
  </s:Body>
</s:Envelope>`))
	if err == nil {
		t.Fatal("expected SOAP fault to fail")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected fault reason in error, got %v", err)
	}
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
