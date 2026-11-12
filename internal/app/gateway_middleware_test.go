package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStripPrefixHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var recordedPath string
	var recordedHeaders http.Header

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordedPath = r.URL.Path
		recordedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	handler := stripPrefixHandler("/app/camkeep", nextHandler)

	t.Run("Rewrite prefix without trailing slash directly to root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if recordedPath != "/" {
			t.Fatalf("expected path /, got %s", recordedPath)
		}
	})

	t.Run("Rewrite prefix with trailing slash directly to root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if recordedPath != "/" {
			t.Fatalf("expected path /, got %s", recordedPath)
		}
	})

	t.Run("Rewrite prefix with query params without redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep?foo=bar", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if recordedPath != "/" {
			t.Fatalf("expected path /, got %s", recordedPath)
		}
	})

	t.Run("Strip prefix with trailing path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep/api/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if recordedPath != "/api/status" {
			t.Fatalf("expected path /api/status, got %s", recordedPath)
		}
	})

	t.Run("TCP request strips forged X-Trim headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Header.Set("X-Trim-Username", "hacker")
		req.Header.Set("X-Trim-Isadmin", "true")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if recordedHeaders.Get("X-Trim-Username") != "" {
			t.Fatalf("expected X-Trim-Username to be removed on TCP request")
		}
		if recordedHeaders.Get("X-Trim-Isadmin") != "" {
			t.Fatalf("expected X-Trim-Isadmin to be removed on TCP request")
		}
	})

	t.Run("Unix socket request keeps X-Trim headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep/api/status", nil)
		req = req.WithContext(unixConnContext(req.Context(), &net.UnixConn{}))
		req.Header.Set("X-Trim-Username", "fnos_admin")
		req.Header.Set("X-Trim-Isadmin", "true")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if recordedPath != "/api/status" {
			t.Fatalf("expected path /api/status, got %s", recordedPath)
		}
		if recordedHeaders.Get("X-Trim-Username") != "fnos_admin" {
			t.Fatalf("expected X-Trim-Username fnos_admin, got %s", recordedHeaders.Get("X-Trim-Username"))
		}
		if recordedHeaders.Get("X-Trim-Isadmin") != "true" {
			t.Fatalf("expected X-Trim-Isadmin true, got %s", recordedHeaders.Get("X-Trim-Isadmin"))
		}
	})
}

func TestGatewayAuthFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(gatewayContextMiddleware())

	auth := authConfig{
		Username:     "admin",
		UserStore:    &userStore{users: map[string]storedUser{"u1": {ID: "u1", Username: "admin", Enabled: true, Role: userRoleAdmin}}},
		CookieSecure: false,
	}

	protected := r.Group("/")
	protected.Use(authRequired(auth))
	protected.GET("/test-auth", func(c *gin.Context) {
		user, _ := getCurrentUser(c)
		c.JSON(http.StatusOK, gin.H{
			"user":     user.Username,
			"role":     user.Role,
			"canAdmin": currentUserCanAdmin(c),
		})
	})

	handler := stripPrefixHandler("/app/camkeep", r)

	t.Run("TCP access without cookie is blocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("TCP access with spoofed X-Trim headers is still blocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
		req.Header.Set("X-Trim-Username", "admin")
		req.Header.Set("X-Trim-Isadmin", "true")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("Unix socket access as admin via gateway is granted without password", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep/test-auth", nil)
		req = req.WithContext(unixConnContext(req.Context(), &net.UnixConn{}))
		req.Header.Set("X-Trim-Username", "fn_master")
		req.Header.Set("X-Trim-Isadmin", "true")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		expectedBody := `{"canAdmin":true,"role":"admin","user":"fn_master"}`
		if rec.Body.String() != expectedBody {
			t.Fatalf("expected body %s, got %s", expectedBody, rec.Body.String())
		}
	})

	t.Run("Unix socket access as non-admin via gateway is granted viewer role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/camkeep/test-auth", nil)
		req = req.WithContext(unixConnContext(req.Context(), &net.UnixConn{}))
		req.Header.Set("X-Trim-Username", "family_member")
		req.Header.Set("X-Trim-Isadmin", "false")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		expectedBody := `{"canAdmin":false,"role":"viewer","user":"family_member"}`
		if rec.Body.String() != expectedBody {
			t.Fatalf("expected body %s, got %s", expectedBody, rec.Body.String())
		}
	})
}
