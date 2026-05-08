package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/service"
	"github.com/r0n9/camkeep/internal/task"
	"gopkg.in/yaml.v3"
)

type recordFile struct {
	Name string `json:"name"`
	Url  string `json:"url"`
	Size string `json:"size"` // 文件大小字符串
	Path string `json:"path"` // 相对路径，用于删除文件
}

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
	var newConfig constant.Config
	if err := yaml.Unmarshal(yamlBytes, &newConfig); err != nil {
		c.JSON(400, gin.H{"error": "YAML 格式有误: " + err.Error()})
		return
	}

	if err := checkDuplicateIDs(newConfig); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	os.WriteFile(constant.ConfigFilePath, yamlBytes, 0644)

	// 异步重启任务，不阻塞前端请求
	go restartTasks(newConfig)
	c.JSON(200, gin.H{"msg": "配置已保存，系统正在热重启"})
}

func handleCameraAction(c *gin.Context) {
	id := c.Param("id")
	action := c.Param("action") // start, stop, auto
	task.SetOverride(id, action)
	c.JSON(200, gin.H{"msg": "指令已下发"})
}

func handleRecords(c *gin.Context) {
	camID := c.Param("id")

	var files []recordFile
	baseDir := filepath.Join(constant.DefaultRecordBaseDir, camID)

	filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".mp4")) {
			relPath, _ := filepath.Rel(constant.DefaultRecordBaseDir, path)

			// 2. 读取并格式化文件大小
			info, _ := d.Info()
			sizeMB := float64(info.Size()) / (1024 * 1024)
			sizeStr := fmt.Sprintf("%.1f MB", sizeMB)

			files = append(files, recordFile{
				Name: filepath.Base(path),
				Url:  "/play/" + filepath.ToSlash(relPath),
				Size: sizeStr,
				Path: filepath.ToSlash(relPath),
			})
		}
		return nil
	})
	c.JSON(200, files)
}

func handleDeleteRecord(c *gin.Context) {
	targetPath := c.Query("path")

	// 基础安全校验，防止路径穿越攻击 (防范 ../../../etc/passwd 这类请求)
	if targetPath == "" || strings.Contains(targetPath, "..") {
		c.JSON(400, gin.H{"error": "非法的路径参数"})
		return
	}

	fullPath := filepath.Join(constant.DefaultRecordBaseDir, targetPath)

	if err := os.Remove(fullPath); err != nil {
		c.JSON(500, gin.H{"error": "删除失败，文件可能已被清理"})
		return
	}

	c.JSON(200, gin.H{"msg": "录像删除成功"})
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
