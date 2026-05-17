package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	envAuthUser         = "CAMKEEP_AUTH_USER"
	envAuthPassword     = "CAMKEEP_AUTH_PASSWORD"
	envSessionSecret    = "CAMKEEP_SESSION_SECRET"
	envAuthCookieSecure = "CAMKEEP_AUTH_COOKIE_SECURE"

	authCookieName = "camkeep_session"
)

var webAuth authConfig

type authConfig struct {
	Enabled       bool
	Username      string
	Password      string
	SessionSecret []byte
	CookieSecure  bool
	SessionTTL    time.Duration
}

type sessionClaims struct {
	User      string `json:"u"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"n"`
}

func loadAuthConfigFromEnv() authConfig {
	username := strings.TrimSpace(os.Getenv(envAuthUser))
	if username == "" {
		username = "admin"
	}

	password := os.Getenv(envAuthPassword)
	cfg := authConfig{
		Enabled:      password != "",
		Username:     username,
		Password:     password,
		CookieSecure: parseBoolEnv(os.Getenv(envAuthCookieSecure)),
		SessionTTL:   7 * 24 * time.Hour,
	}
	if !cfg.Enabled {
		return cfg
	}

	secret := os.Getenv(envSessionSecret)
	if secret == "" {
		cfg.SessionSecret = randomSessionSecret()
		log.Printf("登录鉴权已启用，但 %s 未设置；本次启动将使用临时会话密钥，重启后需要重新登录", envSessionSecret)
	} else {
		cfg.SessionSecret = []byte(secret)
	}
	return cfg
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func randomSessionSecret() []byte {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Printf("生成临时会话密钥失败，将退回到密码派生密钥: %v", err)
		sum := sha256.Sum256([]byte(time.Now().String()))
		return sum[:]
	}
	return secret
}

func handleLoginPage(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.Enabled {
			c.Redirect(http.StatusFound, "/")
			return
		}

		next := sanitizeLoginNext(c.Query("next"))
		if auth.isRequestAuthenticated(c) {
			c.Redirect(http.StatusFound, next)
			return
		}

		c.HTML(http.StatusOK, "login.html", gin.H{
			"Version": version,
			"Next":    next,
			"User":    auth.Username,
		})
	}
}

func handleLoginPost(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.Enabled {
			c.Redirect(http.StatusFound, "/")
			return
		}

		next := sanitizeLoginNext(c.PostForm("next"))
		if !auth.credentialsValid(c.PostForm("username"), c.PostForm("password")) {
			c.HTML(http.StatusUnauthorized, "login.html", gin.H{
				"Version": version,
				"Next":    next,
				"Error":   "用户名或密码错误",
				"User":    c.PostForm("username"),
			})
			return
		}

		token, err := auth.newSessionToken(time.Now())
		if err != nil {
			c.HTML(http.StatusInternalServerError, "login.html", gin.H{
				"Version": version,
				"Next":    next,
				"Error":   "创建登录会话失败",
				"User":    c.PostForm("username"),
			})
			return
		}
		auth.setSessionCookie(c, token)
		c.Redirect(http.StatusFound, next)
	}
}

func handleLogout(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth.clearSessionCookie(c)
		if auth.Enabled {
			c.Redirect(http.StatusFound, "/login")
			return
		}
		c.Redirect(http.StatusFound, "/")
	}
}

func authRequired(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.Enabled || auth.isRequestAuthenticated(c) {
			c.Next()
			return
		}

		if wantsHTML(c.Request) {
			c.Redirect(http.StatusFound, "/login?next="+url.QueryEscape(c.Request.URL.RequestURI()))
			c.Abort()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已过期"})
	}
}

func wantsHTML(r *http.Request) bool {
	if r.Method == http.MethodGet {
		path := r.URL.Path
		if path == "/" || strings.HasSuffix(path, ".html") {
			return true
		}
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func sanitizeLoginNext(next string) string {
	if next == "" {
		return "/"
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	if strings.HasPrefix(next, "/login") {
		return "/"
	}
	return next
}

func (auth authConfig) credentialsValid(username, password string) bool {
	if !auth.Enabled {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(auth.Username)) != 1 {
		return false
	}

	got := sha256.Sum256([]byte(password))
	want := sha256.Sum256([]byte(auth.Password))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

func (auth authConfig) isRequestAuthenticated(c *gin.Context) bool {
	token, err := c.Cookie(authCookieName)
	if err != nil {
		return false
	}
	return auth.validateSessionToken(token, time.Now())
}

func (auth authConfig) newSessionToken(now time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	claims := sessionClaims{
		User:      auth.Username,
		ExpiresAt: now.Add(auth.SessionTTL).Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return payload + "." + auth.signPayload(payload), nil
}

func (auth authConfig) validateSessionToken(token string, now time.Time) bool {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(signature), []byte(auth.signPayload(payload))) != 1 {
		return false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return false
	}

	var claims sessionClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return false
	}
	if claims.User != auth.Username || claims.ExpiresAt <= now.Unix() {
		return false
	}
	return true
}

func (auth authConfig) signPayload(payload string) string {
	mac := hmac.New(sha256.New, auth.SessionSecret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (auth authConfig) setSessionCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.SessionTTL),
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   auth.CookieSecure || c.Request.TLS != nil,
	})
}

func (auth authConfig) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   auth.CookieSecure || c.Request.TLS != nil,
	})
}
