package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
)

const (
	envAuthPassword     = "CAMKEEP_AUTH_PASSWORD"
	envSessionSecret    = "CAMKEEP_SESSION_SECRET"
	envAuthCookieSecure = "CAMKEEP_AUTH_COOKIE_SECURE"

	authCookieName = "camkeep_session"
	authContextKey = "camkeep_user"
)

var (
	webAuth               authConfig
	constantUsersFilePath = constant.UsersFilePath
)

type authConfig struct {
	Username      string
	SessionSecret []byte
	CookieSecure  bool
	SessionTTL    time.Duration
	UserStore     *userStore
	Sessions      *sessionTracker
}

type sessionClaims struct {
	UID            string `json:"uid"`
	User           string `json:"u"`
	Role           string `json:"r"`
	Source         string `json:"src"`
	SessionVersion int64  `json:"ver"`
	ExpiresAt      int64  `json:"exp"`
	Nonce          string `json:"n"`
}

func loadAuthConfigFromEnv() authConfig {
	usersFileExists, err := fileExists(constantUsersFilePath)
	if err != nil {
		log.Fatalf("检查用户文件失败: %v", err)
	}

	store, err := newUserStore(constantUsersFilePath)
	if err != nil {
		log.Fatalf("加载用户文件失败: %v", err)
	}

	username := "admin"
	password := os.Getenv(envAuthPassword)
	if password != "" {
		user, created, err := store.ensureBootstrapAdmin(username, password)
		if err != nil {
			log.Fatalf("初始化本地管理员用户失败: %v", err)
		}
		if created {
			if usersFileExists {
				log.Printf("检测到 %s 且 %s 缺少 admin 用户，已追加本地管理员用户: %s；后续登录以用户文件为准", envAuthPassword, constantUsersFilePath, user.Username)
			} else {
				log.Printf("检测到 %s 且 %s 不存在，已初始化本地管理员用户: %s；后续登录以用户文件为准", envAuthPassword, constantUsersFilePath, user.Username)
			}
		}
	}

	cfg := authConfig{
		Username:     username,
		CookieSecure: parseBoolEnv(os.Getenv(envAuthCookieSecure)),
		SessionTTL:   7 * 24 * time.Hour,
		UserStore:    store,
		Sessions:     newSessionTracker(),
	}

	secret := os.Getenv(envSessionSecret)
	if secret == "" {
		cfg.SessionSecret = randomSessionSecret()
		if cfg.isEnabled() {
			log.Printf("登录鉴权已启用，但 %s 未设置；本次启动将使用临时会话密钥，重启后需要重新登录", envSessionSecret)
		}
	} else {
		cfg.SessionSecret = []byte(secret)
	}
	return cfg
}

func (auth authConfig) isEnabled() bool {
	return auth.UserStore != nil && auth.UserStore.hasUsers()
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
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
		if !auth.isEnabled() {
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
		if !auth.isEnabled() {
			c.Redirect(http.StatusFound, "/")
			return
		}

		next := sanitizeLoginNext(c.PostForm("next"))
		user, ok := auth.authenticateUser(c.PostForm("username"), c.PostForm("password"))
		if !ok {
			c.HTML(http.StatusUnauthorized, "login.html", gin.H{
				"Version": version,
				"Next":    next,
				"Error":   "用户名或密码错误",
				"User":    c.PostForm("username"),
			})
			return
		}

		now := time.Now()
		token, err := auth.newSessionTokenForUser(user, now)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "login.html", gin.H{
				"Version": version,
				"Next":    next,
				"Error":   "创建登录会话失败",
				"User":    c.PostForm("username"),
			})
			return
		}
		ip := clientIP(c)
		if auth.UserStore != nil {
			if _, err := auth.UserStore.recordLogin(user.ID, ip, now); err != nil {
				log.Printf("记录用户登录信息失败: %v", err)
			}
		}
		if auth.Sessions != nil {
			auth.Sessions.trackLogin(token, user, ip, now, now.Add(auth.SessionTTL))
		}
		auth.setSessionCookie(c, token)
		c.Redirect(http.StatusFound, next)
	}
}

func handleLogout(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := c.Cookie(authCookieName); err == nil && auth.Sessions != nil {
			auth.Sessions.remove(token)
		}
		auth.clearSessionCookie(c)
		if auth.isEnabled() {
			c.Redirect(http.StatusFound, "/login")
			return
		}
		c.Redirect(http.StatusFound, "/")
	}
}

func authRequired(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.isEnabled() {
			setCurrentUser(c, disabledAdminUser())
			c.Next()
			return
		}

		if user, claims, token, ok := auth.currentUserClaimsFromRequest(c); ok {
			setCurrentUser(c, user)
			if auth.Sessions != nil {
				auth.Sessions.touch(token, user, clientIP(c), time.Now(), time.Unix(claims.ExpiresAt, 0))
			}
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

func clientIP(c *gin.Context) string {
	forwarded := c.GetHeader("X-Forwarded-For")
	if forwarded != "" {
		ip, _, _ := strings.Cut(forwarded, ",")
		if ip = strings.TrimSpace(ip); ip != "" {
			return ip
		}
	}
	if ip := strings.TrimSpace(c.GetHeader("X-Real-IP")); ip != "" {
		return ip
	}
	return c.ClientIP()
}

func (auth authConfig) credentialsValid(username, password string) bool {
	_, ok := auth.authenticateUser(username, password)
	return ok
}

func (auth authConfig) authenticateUser(username, password string) (currentUser, bool) {
	if !auth.isEnabled() {
		return disabledAdminUser(), true
	}

	if auth.UserStore == nil {
		return currentUser{}, false
	}
	return auth.UserStore.authenticate(username, password)
}

func (auth authConfig) isRequestAuthenticated(c *gin.Context) bool {
	_, ok := auth.currentUserFromRequest(c)
	return ok
}

func (auth authConfig) currentUserFromRequest(c *gin.Context) (currentUser, bool) {
	user, _, _, ok := auth.currentUserClaimsFromRequest(c)
	return user, ok
}

func (auth authConfig) currentUserClaimsFromRequest(c *gin.Context) (currentUser, sessionClaims, string, bool) {
	token, err := c.Cookie(authCookieName)
	if err != nil {
		return currentUser{}, sessionClaims{}, "", false
	}
	user, claims, ok := auth.validateSessionTokenUserClaims(token, time.Now())
	return user, claims, token, ok
}

func (auth authConfig) newSessionToken(now time.Time) (string, error) {
	return auth.newSessionTokenForUser(disabledAdminUser(), now)
}

func (auth authConfig) newSessionTokenForUser(user currentUser, now time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	claims := sessionClaims{
		UID:            user.ID,
		User:           user.Username,
		Role:           user.Role,
		Source:         user.Source,
		SessionVersion: user.SessionVersion,
		ExpiresAt:      now.Add(auth.SessionTTL).Unix(),
		Nonce:          base64.RawURLEncoding.EncodeToString(nonce),
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return payload + "." + auth.signPayload(payload), nil
}

func (auth authConfig) validateSessionToken(token string, now time.Time) bool {
	_, ok := auth.validateSessionTokenUser(token, now)
	return ok
}

func (auth authConfig) validateSessionTokenUser(token string, now time.Time) (currentUser, bool) {
	user, _, ok := auth.validateSessionTokenUserClaims(token, now)
	return user, ok
}

func (auth authConfig) validateSessionTokenUserClaims(token string, now time.Time) (currentUser, sessionClaims, bool) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" {
		return currentUser{}, sessionClaims{}, false
	}

	if subtle.ConstantTimeCompare([]byte(signature), []byte(auth.signPayload(payload))) != 1 {
		return currentUser{}, sessionClaims{}, false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return currentUser{}, sessionClaims{}, false
	}

	var claims sessionClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return currentUser{}, sessionClaims{}, false
	}
	if claims.ExpiresAt <= now.Unix() {
		return currentUser{}, sessionClaims{}, false
	}

	if claims.Source != userSourceLocal || auth.UserStore == nil || claims.UID == "" {
		return currentUser{}, sessionClaims{}, false
	}
	user, ok := auth.UserStore.userByID(claims.UID)
	if !ok || !user.Enabled || user.Username != claims.User || user.SessionVersion != claims.SessionVersion {
		return currentUser{}, sessionClaims{}, false
	}
	return currentUserFromStored(user), claims, true
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

func disabledAdminUser() currentUser {
	return currentUser{
		ID:             "auth-disabled",
		Username:       "admin",
		DisplayName:    "Admin",
		Role:           userRoleAdmin,
		Source:         "disabled",
		SessionVersion: 1,
	}
}

func setCurrentUser(c *gin.Context, user currentUser) {
	c.Set(authContextKey, user)
}

func getCurrentUser(c *gin.Context) (currentUser, bool) {
	value, ok := c.Get(authContextKey)
	if !ok {
		return currentUser{}, false
	}
	user, ok := value.(currentUser)
	return user, ok
}

func currentUserCanAdmin(c *gin.Context) bool {
	user, ok := getCurrentUser(c)
	return ok && user.Role == userRoleAdmin
}

func adminRequired(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.isEnabled() || currentUserCanAdmin(c) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
	}
}
