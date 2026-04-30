package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/service"
	"github.com/r0n9/camkeep/slog"
	"github.com/r0n9/camkeep/task"

	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
)

var (
	currentConfig constant.Config
	restartMux    sync.Mutex // 热重启专属防并发锁
	reloadCancel  context.CancelFunc
	taskWg        sync.WaitGroup
	ctxGlobal     context.Context
)

var Version string = "dev"

func main() {
	mime.AddExtensionType(".ts", "video/mp2t")
	slog.Init()

	log.Printf("CamKeep version=%s", Version)

	// 在 CamKeep 业务逻辑启动前，先拉起底座进程
	task.StartGo2rtcDaemon()
	// 动态轮询等待，确保 go2rtc API 彻底就绪（最大等待 10 秒）
	log.Println("等待底层流媒体引擎 go2rtc 启动...")
	if err := task.WaitForGo2rtcReady(10 * time.Second); err != nil {
		// 如果 10 秒了还没起来，说明底座进程严重故障，直接让主程序退出
		log.Fatalf("致命错误: 无法连接到底层引擎: %v", err)
	}
	log.Println("go2rtc 底座已完美就绪！")

	// 1. 读取或初始化配置 (如果不存在则创建空配置)
	currentConfig = loadOrInitConfig()

	os.MkdirAll(constant.DefaultRecordBaseDir, 0755)

	// 加载持久化的手动录像覆盖指令
	task.LoadOverrides()

	// 设置全局 Context
	var cancelGlobal context.CancelFunc
	ctxGlobal, cancelGlobal = context.WithCancel(context.Background())

	// 初始化流
	task.InitGo2rtcStreams(currentConfig)

	// 启动实时流状态轮询任务
	go task.PollGo2rtcStatus(&currentConfig)

	// 启动 Web 路由
	go startWebServer()

	// 启动后台录制与清理任务
	startTasks()

	// 监听退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("接收到退出信号，正在停止所有任务...")

	task.CleanupGo2rtcStreams(currentConfig)
	cancelGlobal() // 通知所有层级的 Context 退出
	taskWg.Wait()  // 等待所有任务完成
	log.Println("程序已安全退出。")
}

// loadOrInitConfig 如果配置文件不存在则生成一个带示例的默认配置
func loadOrInitConfig() constant.Config {
	os.MkdirAll(filepath.Dir(constant.ConfigFilePath), 0755)

	data, err := os.ReadFile(constant.ConfigFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("conf.yaml 不存在，自动创建默认模板配置...")

			// 定义默认配置模板
			defaultCfg := constant.Config{
				Cameras: []constant.Camera{
					{
						ID:              "摄像头1",
						RTSPUrl:         "rtsp://admin:password@192.168.1.100:554/live",
						RetentionDays:   7,
						SegmentDuration: 600,
						Format:          "ts",
						MinSizeKb:       1024,
						RecordTime:      "00:00-23:59",
						Mode:            "normal",
					},
				},
			}

			// 将默认配置序列化为 YAML
			out, err := yaml.Marshal(defaultCfg)
			if err != nil {
				log.Printf("生成默认配置失败: %v", err)
				return defaultCfg
			}

			// 可以在文件开头写入一些注释说明（可选）
			header := []byte("# CamKeep 配置文件\n# mode: normal (普通录制), timelapse (延时摄影)\n")
			finalOut := append(header, out...)

			if err := os.WriteFile(constant.ConfigFilePath, finalOut, 0644); err != nil {
				log.Printf("写入配置文件失败: %v", err)
			}

			return defaultCfg
		}
		log.Fatalf("读取配置文件失败: %v", err)
	}

	var c constant.Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	// 启动时如果发现文件里有重复 ID，自动去重
	c = validateAndFixConfig(c)
	return c
}

// startTasks 启动或重启所有的摄像头监控任务
func startTasks() {
	var ctx context.Context
	ctx, reloadCancel = context.WithCancel(ctxGlobal)

	constant.ConfigMux.RLock()
	cams := currentConfig.Cameras
	constant.ConfigMux.RUnlock()

	taskWg.Add(1)
	go task.CleanupTask(ctx, &taskWg, cams)

	for _, cam := range cams {
		taskWg.Add(1)
		go task.CameraTask(ctx, &taskWg, cam)
	}
}

// restartTasks 热重启任务 (用于保存配置后生效)
func restartTasks(newConfig constant.Config) {
	// restartMux 互斥锁，防并发重启
	restartMux.Lock()
	defer restartMux.Unlock()

	log.Println("检测到配置更改，正在重启底层任务...")
	if reloadCancel != nil {
		reloadCancel() // 取消旧的录像和清理任务
	}
	taskWg.Wait() // 阻塞等待旧任务全部安全退出 (此时录像已停)

	// 1. 获取旧配置的快照 (用于注销旧流)，极速释放锁
	constant.ConfigMux.RLock()
	oldConfig := currentConfig
	constant.ConfigMux.RUnlock()

	// 2. 执行耗时的网络请求操作 (千万不要在这里加锁！)
	task.CleanupGo2rtcStreams(oldConfig)
	task.InitGo2rtcStreams(newConfig)

	// 3. 极速替换新配置 (只锁赋值这一瞬间)
	constant.ConfigMux.Lock()
	currentConfig = newConfig
	constant.ConfigMux.Unlock()

	// 4. 【新增】清理内存中被删除的“幽灵”摄像头状态
	cleanGhostStatus(newConfig)

	// 5. 启动新任务
	startTasks()
	log.Println("任务热重启完成！")
}

// cleanGhostStatus 清理掉已经被移出配置的摄像头状态
func cleanGhostStatus(newConfig constant.Config) {
	// 建立新配置的 ID 索引
	validIDs := make(map[string]bool)
	for _, cam := range newConfig.Cameras {
		validIDs[cam.ID] = true
	}

	// 遍历内存状态，如果不在新配置中，则直接删除
	service.StatusMux.Lock()
	defer service.StatusMux.Unlock()
	for id := range service.StatusMap {
		if !validIDs[id] {
			delete(service.StatusMap, id)
			log.Printf("已清理移除的摄像头状态: %s", id)
		}
	}
}

func startWebServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.Static("/static", "./static")

	// 1. 渲染前端页面
	r.LoadHTMLGlob("template/*")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"Version": Version,
		})
	})

	// 2. 获取所有摄像头状态
	r.GET("/api/status", func(c *gin.Context) {
		service.StatusMux.RLock()
		defer service.StatusMux.RUnlock()
		c.JSON(200, service.StatusMap)
	})

	// 获取配置文件内容
	r.GET("/api/config", func(c *gin.Context) {
		data, _ := os.ReadFile(constant.ConfigFilePath) // 【修改】
		c.String(200, string(data))
	})

	// 保存并应用配置文件
	r.POST("/api/config", func(c *gin.Context) {
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
	})

	// 摄像头手动控制启停
	r.POST("/api/camera/:id/:action", func(c *gin.Context) {
		id := c.Param("id")
		action := c.Param("action") // start, stop, auto
		task.SetOverride(id, action)
		c.JSON(200, gin.H{"msg": "指令已下发"})
	})

	r.GET("/api/records/:id", func(c *gin.Context) {
		camID := c.Param("id")

		// 1. 结构体
		type RecordFile struct {
			Name string `json:"name"`
			Url  string `json:"url"`
			Size string `json:"size"` // 文件大小字符串
			Path string `json:"path"` // 相对路径，用于删除文件
		}
		var files []RecordFile
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

				files = append(files, RecordFile{
					Name: filepath.Base(path),
					Url:  "/play/" + filepath.ToSlash(relPath),
					Size: sizeStr,
					Path: filepath.ToSlash(relPath),
				})
			}
			return nil
		})
		c.JSON(200, files)
	})

	r.DELETE("/api/record", func(c *gin.Context) {
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
	})

	r.GET("/api/go2rtc/unmanaged", func(c *gin.Context) {
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
	})

	r.StaticFS("/play", http.Dir(constant.DefaultRecordBaseDir))

	r.GET("/play_hls/*filepath", func(c *gin.Context) {
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
	})

	// 让 CamKeep 作为统一网关，直接代理 go2rtc 的全能自适应直播功能
	go2rtcURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort))
	go2rtcProxy := httputil.NewSingleHostReverseProxy(go2rtcURL)

	// 1. 代理 go2rtc 的自适应播放器页面及内部依赖的 JS (解决跨域和单端口问题)
	r.GET("/stream.html", gin.WrapH(go2rtcProxy))
	r.GET("/video-stream.js", gin.WrapH(go2rtcProxy))
	r.GET("/video-rtc.js", gin.WrapH(go2rtcProxy))
	r.GET("/webrtc.html", gin.WrapH(go2rtcProxy))

	// 2. 代理流媒体协商相关的 API (含 WebSocket 支持，自动兼容不同浏览器的降级)
	r.Any("/api/ws", gin.WrapH(go2rtcProxy))
	r.Any("/api/webrtc", gin.WrapH(go2rtcProxy))
	r.Any("/api/stream.mp4", gin.WrapH(go2rtcProxy))
	r.Any("/api/stream.m3u8", gin.WrapH(go2rtcProxy))

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	r.POST("/webrtc/:id", func(c *gin.Context) {
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
	})

	log.Println("Web 管理后台已启动: http://localhost:9110")
	r.Run(":9110")
}

// checkDuplicateIDs 检查配置中是否有重复的摄像头ID (用于 API 严格校验)
func checkDuplicateIDs(cfg constant.Config) error {
	seen := make(map[string]bool)
	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			return fmt.Errorf("摄像头 ID 不能为空")
		}
		if seen[cam.ID] {
			return fmt.Errorf("发现重复的摄像头 ID: %s", cam.ID)
		}
		seen[cam.ID] = true
	}
	return nil
}

// validateAndFixConfig 修复文件读取时的配置 (用于启动时静默去重防崩溃)
func validateAndFixConfig(cfg constant.Config) constant.Config {
	seen := make(map[string]bool)
	var uniqueCams []constant.Camera

	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			log.Println("警告: 发现空 ID 的摄像头配置，已跳过")
			continue
		}
		if seen[cam.ID] {
			log.Printf("警告: 发现重复的摄像头 ID [%s]，已自动去重", cam.ID)
			continue
		}
		seen[cam.ID] = true

		// 预置默认录像策略。如果用户在 conf.yaml 中没写，就走这里的兜底。
		if cam.RetentionDays == 0 {
			cam.RetentionDays = 7
		}
		if cam.SegmentDuration == 0 {
			cam.SegmentDuration = 600
		}
		if cam.Format == "" {
			cam.Format = "ts"
		}
		if cam.MinSizeKb == 0 {
			cam.MinSizeKb = 1024
		}
		if cam.RecordTime == "" {
			cam.RecordTime = "00:00-23:59"
		}
		if cam.Mode == "" {
			cam.Mode = "normal" // 普通录制模式
		}

		uniqueCams = append(uniqueCams, cam)
	}
	cfg.Cameras = uniqueCams
	return cfg
}
