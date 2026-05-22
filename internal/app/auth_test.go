package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAuthDisabledAllowsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := authConfig{}

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
	auth := testAuthConfig(t)

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
	auth := testAuthConfig(t)

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
	auth := testAuthConfig(t)
	current, ok := auth.authenticateUser("admin", "admin-secret")
	if !ok {
		t.Fatal("expected test admin credentials to pass")
	}
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)

	token, err := auth.newSessionTokenForUser(current, now)
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
	auth := testAuthConfig(t)

	if !auth.credentialsValid("admin", "admin-secret") {
		t.Fatal("expected valid credentials to pass")
	}
	if auth.credentialsValid("admin", "bad") {
		t.Fatal("expected bad password to fail")
	}
	if auth.credentialsValid("other", "secret") {
		t.Fatal("expected bad username to fail")
	}
}

func TestLocalUserSessionValidation(t *testing.T) {
	auth := testAuthConfig(t)
	store := testUserStoreAt(t, "")
	user, err := store.createUser(createUserRequest{
		Username: "viewer1",
		Password: "viewer-secret",
		Role:     userRoleViewer,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	auth.UserStore = store

	current, ok := auth.authenticateUser("viewer1", "viewer-secret")
	if !ok {
		t.Fatal("expected local user credentials to pass")
	}
	if current.Role != userRoleViewer || current.ID != user.ID {
		t.Fatalf("unexpected current user: %+v", current)
	}

	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	token, err := auth.newSessionTokenForUser(current, now)
	if err != nil {
		t.Fatal(err)
	}
	validated, ok := auth.validateSessionTokenUser(token, now.Add(time.Minute))
	if !ok || validated.Username != "viewer1" {
		t.Fatalf("expected local session to validate, got ok=%t user=%+v", ok, validated)
	}

	enabled := false
	if _, err := store.updateUser(user.ID, updateUserRequest{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if auth.validateSessionToken(token, now.Add(2*time.Minute)) {
		t.Fatal("expected disabled user session to fail")
	}
}

func TestAdminRequiredRejectsViewer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := testAuthConfig(t)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		setCurrentUser(c, currentUser{Username: "viewer", Role: userRoleViewer, Source: userSourceLocal})
		c.Next()
	})
	r.Use(adminRequired(auth))
	r.GET("/api/config", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestUserStoreProtectsAdminCredentials(t *testing.T) {
	auth := testAuthConfig(t)
	store := testUserStoreAt(t, "")
	localAdmin, err := store.createUser(createUserRequest{
		Username: "local-admin",
		Password: "local-secret",
		Role:     userRoleAdmin,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	auth.UserStore = store

	enabled := false
	err = auth.validateUserUpdate(testGinContextWithUser(currentUser{ID: localAdmin.ID, Role: userRoleAdmin, Source: userSourceLocal}), localAdmin, updateUserRequest{Enabled: &enabled})
	if err == nil {
		t.Fatal("expected disabling last admin to fail")
	}
}

func TestBootstrapCreateUserWithoutEnvPasswordCreatesAdminSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := testUserStore(t)
	auth := authConfig{
		Username:      "admin",
		SessionSecret: []byte("test-session-secret"),
		SessionTTL:    time.Hour,
		UserStore:     store,
	}
	if auth.isEnabled() {
		t.Fatal("expected auth to start disabled without env password or local users")
	}

	r := gin.New()
	r.Use(authRequired(auth))
	r.Use(adminRequired(auth))
	r.POST("/api/users", handleCreateUser(auth))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{
		"username": "viewer",
		"password": "bootstrap-secret",
		"role": "viewer",
		"enabled": false
	}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created userView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Username != "admin" || created.Role != userRoleAdmin || !created.Enabled {
		t.Fatalf("expected bootstrap user to be enabled admin, got %+v", created)
	}
	if !auth.isEnabled() {
		t.Fatal("expected local user creation to enable auth dynamically")
	}
	cookie := w.Result().Cookies()
	if len(cookie) == 0 || cookie[0].Name != authCookieName {
		t.Fatalf("expected bootstrap create to set session cookie, got %+v", cookie)
	}
	if !auth.validateSessionToken(cookie[0].Value, time.Now()) {
		t.Fatal("expected bootstrap session cookie to validate")
	}
}

func TestLoadAuthConfigSeedsAdminFromEnvPasswordWhenUsersFileMissing(t *testing.T) {
	t.Setenv(envAuthPassword, "env-secret")
	t.Setenv(envSessionSecret, "test-session-secret")

	path := filepath.Join(t.TempDir(), "config", "users.json")
	withUsersFilePath(t, path, func() {
		auth := loadAuthConfigFromEnv()
		if !auth.isEnabled() {
			t.Fatal("expected auth to be enabled after env password seeds users file")
		}
		if !auth.credentialsValid("admin", "env-secret") {
			t.Fatal("expected seeded admin to use CAMKEEP_AUTH_PASSWORD")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected users file to be created: %v", err)
		}
	})
}

func TestLoadAuthConfigAddsAdminWhenUsersFileExistsWithoutAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "users.json")
	store := testUserStoreAt(t, path)
	if _, err := store.createUser(createUserRequest{
		Username: "viewer1",
		Password: "viewer-secret",
		Role:     userRoleViewer,
	}, ""); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envAuthPassword, "env-secret")
	t.Setenv(envSessionSecret, "test-session-secret")
	withUsersFilePath(t, path, func() {
		auth := loadAuthConfigFromEnv()
		if !auth.credentialsValid("admin", "env-secret") {
			t.Fatal("expected missing admin to be added from CAMKEEP_AUTH_PASSWORD")
		}
		if !auth.credentialsValid("viewer1", "viewer-secret") {
			t.Fatal("expected existing users to be preserved")
		}
		users := auth.UserStore.listUsers()
		if len(users) != 2 {
			t.Fatalf("expected 2 users after adding admin, got %d", len(users))
		}
	})
}

func TestEnvPasswordIgnoredWhenUsersFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "users.json")
	store := testUserStoreAt(t, path)
	if _, err := store.createUser(createUserRequest{
		Username: "admin",
		Password: "local-secret",
		Role:     userRoleAdmin,
	}, ""); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envAuthPassword, "env-secret")
	t.Setenv(envSessionSecret, "test-session-secret")
	withUsersFilePath(t, path, func() {
		auth := loadAuthConfigFromEnv()
		if !auth.credentialsValid("admin", "local-secret") {
			t.Fatal("expected existing users file password to work")
		}
		if auth.credentialsValid("admin", "env-secret") {
			t.Fatal("expected CAMKEEP_AUTH_PASSWORD to be ignored when users file exists")
		}
	})
}

func testAuthConfig(t *testing.T) authConfig {
	t.Helper()
	store := testUserStoreAt(t, "")
	if _, err := store.createUser(createUserRequest{
		Username: "admin",
		Password: "admin-secret",
		Role:     userRoleAdmin,
	}, ""); err != nil {
		t.Fatal(err)
	}
	return authConfig{
		Username:      "admin",
		SessionSecret: []byte("test-session-secret"),
		SessionTTL:    time.Hour,
		UserStore:     store,
	}
}

func testUserStore(t *testing.T) *userStore {
	t.Helper()
	return testUserStoreAt(t, "")
}

func testUserStoreAt(t *testing.T, path string) *userStore {
	t.Helper()
	if path == "" {
		path = filepath.Join(t.TempDir(), "users.json")
	}
	store, err := newUserStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func withUsersFilePath(t *testing.T, path string, fn func()) {
	t.Helper()
	old := constantUsersFilePath
	constantUsersFilePath = path
	defer func() {
		constantUsersFilePath = old
	}()
	fn()
}

func testGinContextWithUser(user currentUser) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setCurrentUser(c, user)
	return c
}
