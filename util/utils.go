package util

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// IsWithinTimeRange 判断当前时间是否在录制区间内。
// 支持多个区间，例如 "08:00-12:00,14:00-18:00"；单个区间支持跨天，例如 "22:00-06:00"。
func IsWithinTimeRange(timeStr string) bool {
	return IsTimeWithinTimeRange(time.Now(), timeStr)
}

func IsTimeWithinTimeRange(now time.Time, timeStr string) bool {
	return IsClockWithinTimeRange(fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute()), timeStr)
}

func IsClockWithinTimeRange(clock, timeStr string) bool {
	if timeStr == "" {
		return true // 空表示 24 小时
	}

	nowMinutes, err := parseClockMinutes(clock)
	if err != nil {
		return true
	}

	hasValidRange := false
	for _, item := range splitTimeRanges(timeStr) {
		start, end, ok := parseTimeRange(item)
		if !ok {
			continue
		}
		hasValidRange = true
		if minutesInRange(nowMinutes, start, end) {
			return true
		}
	}

	return !hasValidRange
}

func splitTimeRanges(timeStr string) []string {
	return strings.FieldsFunc(timeStr, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；' || r == '\n' || r == '\r'
	})
}

func parseTimeRange(item string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(item), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := parseClockMinutes(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	end, err := parseClockMinutes(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

func parseClockMinutes(clock string) (int, error) {
	parts := strings.Split(strings.TrimSpace(clock), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid clock")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	if hour < 0 || hour > 24 || minute < 0 || minute > 59 || (hour == 24 && minute != 0) {
		return 0, fmt.Errorf("invalid clock")
	}
	return hour*60 + minute, nil
}

func minutesInRange(now, start, end int) bool {
	if start == end {
		return false
	}
	if start <= end {
		return now >= start && now <= end
	}
	// 处理跨天情况 (如 22:00 到 06:00)
	return now >= start || now <= end
}

func IsTimeRangeEndingSoon(now time.Time, timeStr string, lead time.Duration) bool {
	if timeStr == "" {
		return false
	}
	if lead < 0 {
		lead = 0
	}

	nowSecond := now.Hour()*3600 + now.Minute()*60 + now.Second()
	leadSeconds := int(lead.Seconds())
	for _, item := range splitTimeRanges(timeStr) {
		start, end, ok := parseTimeRange(item)
		if !ok || start == end {
			continue
		}
		if allDayTimeRange(start, end) {
			return false
		}
		endSecond := rangeEndBoundarySecond(end)
		if start <= end {
			if nearRangeEndBoundary(nowSecond, endSecond, leadSeconds) {
				return true
			}
			continue
		}
		if nowSecond >= start*60 {
			if nearRangeEndBoundary(nowSecond, endSecond+24*60*60, leadSeconds) {
				return true
			}
		} else if nearRangeEndBoundary(nowSecond, endSecond, leadSeconds) {
			return true
		}
	}
	return false
}

func allDayTimeRange(start, end int) bool {
	return start == 0 && (end == 24*60 || end == 23*60+59)
}

func rangeEndBoundarySecond(end int) int {
	if end == 23*60+59 {
		return 24 * 60 * 60
	}
	return end * 60
}

func nearRangeEndBoundary(nowSecond, boundarySecond, leadSeconds int) bool {
	if nowSecond <= boundarySecond {
		return boundarySecond-nowSecond <= leadSeconds
	}
	return nowSecond < boundarySecond+60
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
