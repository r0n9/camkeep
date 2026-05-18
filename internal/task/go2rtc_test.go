package task

import "testing"

func TestDefaultPortForSchemeSupportsONVIF(t *testing.T) {
	if got := defaultPortForScheme("onvif"); got != "80" {
		t.Fatalf("expected ONVIF default port 80, got %q", got)
	}
}
