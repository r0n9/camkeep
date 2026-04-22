package util

import (
	"net/url"
	"strings"
	"time"
)

// IsWithinTimeRange 判断当前时间是否在录制区间内 (支持跨天，如 22:00-06:00)
func IsWithinTimeRange(timeStr string) bool {
	if timeStr == "" {
		return true // 空表示 24 小时
	}
	parts := strings.Split(timeStr, "-")
	if len(parts) != 2 {
		return true // 格式错误默认放行
	}

	now := time.Now().Format("15:04")
	start, end := parts[0], parts[1]

	if start <= end {
		return now >= start && now <= end
	}
	// 处理跨天情况 (如 22:00 到 06:00)
	return now >= start || now <= end
}

// EscapeRTSPAuth 专门用于安全地转义 RTSP URL 中的账号和密码
func EscapeRTSPAuth(rawURL string) string {
	// 1. 提取协议前缀
	prefix := ""
	if strings.HasPrefix(rawURL, "rtsp://") {
		prefix = "rtsp://"
	} else if strings.HasPrefix(rawURL, "rtsps://") {
		prefix = "rtsps://"
	} else {
		return rawURL // 非标准格式，原样返回
	}

	// 2. 分离 authority (user:pass@host:port) 和 path (/stream...)
	body := rawURL[len(prefix):]
	pathIdx := strings.Index(body, "/")

	authority := body
	path := ""
	if pathIdx != -1 {
		authority = body[:pathIdx]
		path = body[pathIdx:]
	}

	// 3. 寻找最后一个 @，分离 账号密码 和 主机地址
	atIdx := strings.LastIndex(authority, "@")
	if atIdx == -1 {
		return rawURL // 没有账号密码信息，不需要转义，原样返回
	}

	authPart := authority[:atIdx]
	hostPart := authority[atIdx+1:]

	// 4. 分离 Username 和 Password
	colonIdx := strings.Index(authPart, ":")
	user := authPart
	pass := ""
	if colonIdx != -1 {
		user = authPart[:colonIdx]
		pass = authPart[colonIdx+1:]
	}

	// 5. 仅对账号和密码进行 URL 编码
	safeUser := url.QueryEscape(user)
	safePass := url.QueryEscape(pass)

	// 6. 重新组装安全的 Auth 字符串
	safeAuth := safeUser
	if colonIdx != -1 {
		safeAuth += ":" + safePass
	}

	// 7. 拼接最终的 URL
	return prefix + safeAuth + "@" + hostPart + path
}
