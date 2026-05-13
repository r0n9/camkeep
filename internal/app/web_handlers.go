package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/service"
	"github.com/r0n9/camkeep/internal/task"
)

type recordFile struct {
	Name string `json:"name"`
	Url  string `json:"url"`
	Size string `json:"size"` // 文件大小字符串
	Path string `json:"path"` // 相对路径，用于删除文件
}

type recordEntry struct {
	file    recordFile
	date    time.Time
	dateKey string
}

type recordDateRange struct {
	start    time.Time
	end      time.Time
	explicit bool
}

type probeResult struct {
	Codec     string `json:"codec"`
	IsH265    bool   `json:"is_h265"`
	CanProbe  bool   `json:"can_probe"`
	ProbeNote string `json:"probe_note,omitempty"`
}

const (
	recordDateLayout    = "2006-01-02"
	maxRecordRangeDays  = 7
	defaultRecordDayMax = 7
)

var recordDatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

func handleIndex(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Version": version,
	})
}

func handleStatus(c *gin.Context) {
	service.StatusMux.RLock()
	defer service.StatusMux.RUnlock()
	c.JSON(200, service.StatusMap)
}

func handleGetConfig(c *gin.Context) {
	data, _ := os.ReadFile(constant.ConfigFilePath) // 【修改】
	c.String(200, string(data))
}

func handleSaveConfig(c *gin.Context) {
	yamlBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "读取请求失败"})
		return
	}
	newConfig, err := parseConfigYAML(yamlBytes)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if err := os.WriteFile(constant.ConfigFilePath, yamlBytes, 0644); err != nil {
		c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 异步重启任务，不阻塞前端请求
	go restartTasks(newConfig)
	c.JSON(200, gin.H{"msg": "配置已保存，系统正在热重启"})
}

func handleValidateConfig(c *gin.Context) {
	yamlBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "读取请求失败"})
		return
	}
	if _, err := parseConfigYAML(yamlBytes); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"msg": "配置格式检查通过"})
}

func handleCameraAction(c *gin.Context) {
	id := c.Param("id")
	action := c.Param("action") // start, stop, auto

	if !cameraExists(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该摄像头"})
		return
	}
	if err := task.SetOverride(id, action); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"msg": "指令已下发"})
}

func cameraExists(camID string) bool {
	constant.ConfigMux.RLock()
	defer constant.ConfigMux.RUnlock()
	return slices.ContainsFunc(currentConfig.Cameras, func(cam constant.Camera) bool {
		return cam.ID == camID
	})
}

func handleRecords(c *gin.Context) {
	camID := c.Param("id")
	dateRange, err := parseRecordDateRange(c.Query("start"), c.Query("end"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var entries []recordEntry
	baseDir := filepath.Join(constant.DefaultRecordBaseDir, camID)

	filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".mp4")) {
			relPath, _ := filepath.Rel(constant.DefaultRecordBaseDir, path)
			relPath = filepath.ToSlash(relPath)
			recordDate, ok := parseRecordDateFromPath(relPath)
			if !ok {
				return nil
			}

			// 2. 读取并格式化文件大小
			info, err := d.Info()
			if err != nil {
				return nil
			}
			sizeMB := float64(info.Size()) / (1024 * 1024)
			sizeStr := fmt.Sprintf("%.1f MB", sizeMB)

			entries = append(entries, recordEntry{
				date:    recordDate,
				dateKey: recordDate.Format(recordDateLayout),
				file: recordFile{
					Name: filepath.Base(path),
					Url:  "/play/" + relPath,
					Size: sizeStr,
					Path: relPath,
				},
			})
		}
		return nil
	})
	c.JSON(http.StatusOK, filterRecordEntries(entries, dateRange))
}

func parseRecordDateRange(startText, endText string) (recordDateRange, error) {
	startText = strings.TrimSpace(startText)
	endText = strings.TrimSpace(endText)
	if startText == "" && endText == "" {
		return recordDateRange{}, nil
	}
	if startText == "" || endText == "" {
		return recordDateRange{}, fmt.Errorf("开始日期和结束日期必须同时提供")
	}

	start, err := parseRecordDate(startText)
	if err != nil {
		return recordDateRange{}, fmt.Errorf("开始日期格式有误")
	}
	end, err := parseRecordDate(endText)
	if err != nil {
		return recordDateRange{}, fmt.Errorf("结束日期格式有误")
	}
	if end.Before(start) {
		return recordDateRange{}, fmt.Errorf("结束日期不能早于开始日期")
	}
	if recordDateSpanDays(start, end) > maxRecordRangeDays {
		return recordDateRange{}, fmt.Errorf("日期范围最多支持连续 %d 天", maxRecordRangeDays)
	}

	return recordDateRange{
		start:    start,
		end:      end,
		explicit: true,
	}, nil
}

func recordDateSpanDays(start, end time.Time) int {
	startUTC := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endUTC := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return int(endUTC.Sub(startUTC)/(24*time.Hour)) + 1
}

func parseRecordDate(dateText string) (time.Time, error) {
	parsed, err := time.ParseInLocation(recordDateLayout, dateText, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	if parsed.Format(recordDateLayout) != dateText {
		return time.Time{}, fmt.Errorf("invalid date")
	}
	return parsed, nil
}

func parseRecordDateFromPath(relPath string) (time.Time, bool) {
	pathParts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range pathParts[1:] {
		candidate := recordDatePattern.FindString(part)
		if candidate != part {
			continue
		}
		parsed, err := parseRecordDate(candidate)
		if err == nil {
			return parsed, true
		}
	}

	for _, candidate := range recordDatePattern.FindAllString(relPath, -1) {
		parsed, err := parseRecordDate(candidate)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func filterRecordEntries(entries []recordEntry, dateRange recordDateRange) []recordFile {
	sortRecordEntries(entries)

	var files []recordFile
	if dateRange.explicit {
		for _, entry := range entries {
			if entry.date.Before(dateRange.start) || entry.date.After(dateRange.end) {
				continue
			}
			files = append(files, entry.file)
		}
		return files
	}

	selectedDates := latestRecordDateKeys(entries, defaultRecordDayMax)
	for _, entry := range entries {
		if selectedDates[entry.dateKey] {
			files = append(files, entry.file)
		}
	}
	return files
}

func sortRecordEntries(entries []recordEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].date.Equal(entries[j].date) {
			return entries[i].date.After(entries[j].date)
		}
		return entries[i].file.Name > entries[j].file.Name
	})
}

func latestRecordDateKeys(entries []recordEntry, limit int) map[string]bool {
	dateSet := make(map[string]bool)
	for _, entry := range entries {
		dateSet[entry.dateKey] = true
	}

	dates := make([]string, 0, len(dateSet))
	for dateKey := range dateSet {
		dates = append(dates, dateKey)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	if len(dates) > limit {
		dates = dates[:limit]
	}

	selected := make(map[string]bool, len(dates))
	for _, dateKey := range dates {
		selected[dateKey] = true
	}
	return selected
}

func handleDeleteRecord(c *gin.Context) {
	fullPath, ok := safeRecordPath(c)
	if !ok {
		c.JSON(400, gin.H{"error": "非法的路径参数"})
		return
	}

	if err := os.Remove(fullPath); err != nil {
		c.JSON(500, gin.H{"error": "删除失败，文件可能已被清理"})
		return
	}

	c.JSON(200, gin.H{"msg": "录像删除成功"})
}

func handleDownloadRecord(c *gin.Context) {
	fullPath, ok := safeRecordPath(c)
	if !ok {
		c.JSON(400, gin.H{"error": "非法的路径参数"})
		return
	}

	if _, err := os.Stat(fullPath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "录像文件不存在"})
		return
	}

	c.FileAttachment(fullPath, filepath.Base(fullPath))
}

func handleProbeRecord(c *gin.Context) {
	fullPath, ok := safeRecordPath(c)
	if !ok {
		c.JSON(400, gin.H{"error": "非法的路径参数"})
		return
	}

	codec, err := probeVideoCodec(fullPath)
	if err != nil {
		c.JSON(http.StatusOK, probeResult{
			CanProbe:  false,
			ProbeNote: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, probeResult{
		Codec:    codec,
		IsH265:   isH265Codec(codec),
		CanProbe: true,
	})
}

func handleUnmanagedStreams(c *gin.Context) {
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	resp, err := http.Get(go2rtcHost + "/api/streams")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法连接到 go2rtc"})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析 go2rtc 响应失败"})
		return
	}

	var streamKeys []string
	if streamsObj, ok := result["streams"].(map[string]interface{}); ok {
		for k := range streamsObj {
			streamKeys = append(streamKeys, k)
		}
	} else {
		for k := range result {
			streamKeys = append(streamKeys, k)
		}
	}

	// 过滤掉已经在 conf.yaml 中被 CamKeep 管理的流
	constant.ConfigMux.RLock()
	managed := make(map[string]bool)
	for _, cam := range currentConfig.Cameras {
		managed[cam.ID] = true
	}
	constant.ConfigMux.RUnlock()

	var unmanaged []string
	for _, k := range streamKeys {
		if !managed[k] {
			unmanaged = append(unmanaged, k)
		}
	}

	c.JSON(http.StatusOK, unmanaged)
}

func handlePlayHLS(c *gin.Context) {
	tsPath := c.Param("filepath") // 获取路径，例如: /front-door/2026-04-27/12-00-00.ts
	if !strings.HasSuffix(tsPath, ".ts") {
		c.String(400, "仅支持 ts 格式转换为 HLS")
		return
	}

	// 构造一个只包含单一文件的虚拟 M3U8 列表，欺骗 iOS 原生播放器
	m3u8Content := fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3600\n#EXTINF:3600.0,\n/play%s\n#EXT-X-ENDLIST\n", tsPath)

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Cache-Control", "no-cache")
	c.String(200, m3u8Content)
}

// handlePlayRemux 实时重封装：零损耗、零转码、极低CPU，用于浏览器直接硬解 H.265
func handlePlayRemux(c *gin.Context) {
	fullPath, ok := safeRecordPathFromParam(c.Param("filepath"))
	if !ok {
		c.String(http.StatusBadRequest, "非法的路径参数")
		return
	}

	// 核心魔法参数：-c:v copy 彻底跳过视频转码
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", fullPath,
		"-map", "0:v:0", // 显式映射第一个视频流
		"-map", "0:a?", // 显式映射音频流（如果有的话）
		"-c:v", "copy", // 直接复制 H.265 原始数据
		"-tag:v", "hvc1", // 强制将 HEVC 标签设为 hvc1，满足苹果 Safari 的苛刻要求
		"-c:a", "aac", // 音频由于监控多为 G711，浏览器不支持，需要转码 AAC（极低开销）
		"-f", "mp4", // 封装为 MP4
		"-movflags", "frag_keyframe+empty_moov", // 让 MP4 变成流式结构 (Fragmented MP4)，不需要等文件全部处理完就能播放
		"pipe:1", // 输出到标准流
	}

	cmd := exec.CommandContext(c.Request.Context(), "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.String(http.StatusInternalServerError, "重封装初始化失败")
		return
	}
	if err := cmd.Start(); err != nil {
		c.String(http.StatusInternalServerError, "重封装启动失败")
		return
	}

	// 告诉浏览器这直接就是一个 MP4 视频流
	c.Header("Content-Type", "video/mp4")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	// 将 fMP4 数据流直接打给前端
	c.DataFromReader(http.StatusOK, -1, "video/mp4", stdout, nil)

	if err := cmd.Wait(); err != nil && c.Request.Context().Err() == nil {
		log.Printf("实时重封装进程退出异常: %v", err)
	}
}

func handlePlayTranscode(c *gin.Context) {
	fullPath, ok := safeRecordPathFromParam(c.Param("filepath"))
	if !ok {
		c.String(http.StatusBadRequest, "非法的路径参数")
		return
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", fullPath,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-f", "mpegts",
		"pipe:1",
	}

	cmd := exec.CommandContext(c.Request.Context(), "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.String(http.StatusInternalServerError, "转码初始化失败")
		return
	}
	if err := cmd.Start(); err != nil {
		c.String(http.StatusInternalServerError, "转码启动失败")
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.DataFromReader(http.StatusOK, -1, "video/mp2t", stdout, nil)

	if err := cmd.Wait(); err != nil && c.Request.Context().Err() == nil {
		log.Printf("按需转码进程退出异常: %v", err)
	}
}

func handleWebRTCProxy(c *gin.Context) {
	camID := c.Param("id")

	constant.ConfigMux.RLock()
	var targetCam *constant.Camera
	for _, cam := range currentConfig.Cameras {
		if cam.ID == camID {
			targetCam = &cam
			break
		}
	}
	constant.ConfigMux.RUnlock()

	if targetCam == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该摄像头"})
		return
	}

	// 读取前端发来的 WebRTC SDP Offer
	sdpOffer, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取 SDP offer"})
		return
	}

	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)

	// 接口被调用时不再需要发送 PUT 注册流，因为启动时已经统一注册好了！
	// 直接发起 WebRTC 握手：
	go2rtcWebRTCURL := fmt.Sprintf("%s/api/webrtc?src=%s", go2rtcHost, camID)

	resp, err := http.Post(go2rtcWebRTCURL, "application/sdp", bytes.NewReader(sdpOffer))
	if err != nil {
		log.Printf("[%s] 连接 go2rtc 失败: %v", camID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "视频流网关连接失败"})
		return
	}
	defer resp.Body.Close()

	// 判断包含 200 和 201 状态码 (创建成功)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[%s] go2rtc 拒绝了 WebRTC 请求，状态码: %d, 返回内容: %s", camID, resp.StatusCode, string(bodyBytes))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "视频网关握手拒绝"})
		return
	}

	// 将 go2rtc 返回的 SDP Answer 原封不动返回给前端
	c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), resp.Body, nil)
}

func safeRecordPath(c *gin.Context) (string, bool) {
	return safeRecordPathFromParam(c.Query("path"))
}

func safeRecordPathFromParam(targetPath string) (string, bool) {
	targetPath = strings.TrimPrefix(targetPath, "/")
	if targetPath == "" || strings.Contains(targetPath, "..") {
		return "", false
	}
	if !strings.HasSuffix(targetPath, ".ts") && !strings.HasSuffix(targetPath, ".mp4") {
		return "", false
	}
	return filepath.Join(constant.DefaultRecordBaseDir, targetPath), true
}

func probeVideoCodec(fullPath string) (string, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "json",
		fullPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var result struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", err
	}
	if len(result.Streams) == 0 {
		return "", fmt.Errorf("未找到视频流")
	}
	return result.Streams[0].CodecName, nil
}

func isH265Codec(codec string) bool {
	codec = strings.ToLower(codec)
	return codec == "hevc" || codec == "h265" || codec == "h.265"
}
