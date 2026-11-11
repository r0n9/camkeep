package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	CtxKeyViaGateway      = "viaGateway"
	CtxKeyTrimUser        = "trimUser"
	CtxKeyIsAdmin         = "isAdmin"
	DefaultGatewayPrefix  = "/app/camkeep"
)

type serverContextKey string

const isUnixSocketKey serverContextKey = "is_unix_socket"

func unixConnContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, isUnixSocketKey, true)
}

func isUnixSocketRequest(r *http.Request) bool {
	v, ok := r.Context().Value(isUnixSocketKey).(bool)
	return ok && v
}

// resolveSocketPath 解析 Unix Domain Socket 路径
// 优先使用 CAMKEEP_SOCK，其次检查 TRIM_APPDEST/app.sock
func resolveSocketPath() string {
	if p := strings.TrimSpace(os.Getenv("CAMKEEP_SOCK")); p != "" {
		return p
	}
	if dest := strings.TrimSpace(os.Getenv("TRIM_APPDEST")); dest != "" {
		return filepath.Join(dest, "app.sock")
	}
	return ""
}

// stripPrefixHandler 在 Gin 路由匹配前剥离网关前缀，并执行安全头清洗
func stripPrefixHandler(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUnix := isUnixSocketRequest(r)
		if !isUnix {
			// 外部 TCP 连接：严格防伪造，清除任何 X-Trim-* 伪造头
			r.Header.Del("X-Trim-Username")
			r.Header.Del("X-Trim-Userid")
			r.Header.Del("X-Trim-Isadmin")
		}

		if prefix != "" {
			p := r.URL.Path
			if p == prefix {
				target := prefix + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			if strings.HasPrefix(p, prefix+"/") {
				r.URL.Path = strings.TrimPrefix(p, prefix)
				if r.URL.Path == "" {
					r.URL.Path = "/"
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// gatewayContextMiddleware 注入网关上下文到 Gin
func gatewayContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		isUnix := isUnixSocketRequest(c.Request)
		if isUnix {
			c.Set(CtxKeyViaGateway, true)
			username := strings.TrimSpace(c.GetHeader("X-Trim-Username"))
			if username == "" {
				username = "fnos-user"
			}
			// 飞牛系统注入的 X-Trim-Isadmin 常见值为 "true" 或 "1"
			adminVal := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Trim-Isadmin")))
			isAdmin := adminVal == "true" || adminVal == "1"

			c.Set(CtxKeyTrimUser, username)
			c.Set(CtxKeyIsAdmin, isAdmin)
		} else {
			c.Set(CtxKeyViaGateway, false)
			c.Set(CtxKeyIsAdmin, false)
		}
		c.Next()
	}
}
