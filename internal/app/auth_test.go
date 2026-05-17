package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAuthDisabledAllowsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := authConfig{Enabled: false}

	r := gin.New()
	r.Use(authRequired(auth))
	r.GET("/api/status", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthRequiredRedirectsHTMLRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := testAuthConfig()

	r := gin.New()
	r.Use(authRequired(auth))
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?camera=front", nil)
	req.Header.Set("Accept", "text/html")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?next=") || !strings.Contains(location, "%3Fcamera%3Dfront") {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}

func TestAuthRequiredRejectsAPIRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := testAuthConfig()

	r := gin.New()
	r.Use(authRequired(auth))
	r.GET("/api/status", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestSessionTokenValidation(t *testing.T) {
	auth := testAuthConfig()
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)

	token, err := auth.newSessionToken(now)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.validateSessionToken(token, now.Add(time.Minute)) {
		t.Fatal("expected valid token to pass")
	}
	if auth.validateSessionToken(token+"x", now.Add(time.Minute)) {
		t.Fatal("expected tampered token to fail")
	}
	if auth.validateSessionToken(token, now.Add(2*time.Hour)) {
		t.Fatal("expected expired token to fail")
	}
}

func TestCredentialsValidation(t *testing.T) {
	auth := testAuthConfig()

	if !auth.credentialsValid("admin", "secret") {
		t.Fatal("expected valid credentials to pass")
	}
	if auth.credentialsValid("admin", "bad") {
		t.Fatal("expected bad password to fail")
	}
	if auth.credentialsValid("other", "secret") {
		t.Fatal("expected bad username to fail")
	}
}

func testAuthConfig() authConfig {
	return authConfig{
		Enabled:       true,
		Username:      "admin",
		Password:      "secret",
		SessionSecret: []byte("test-session-secret"),
		SessionTTL:    time.Hour,
	}
}
