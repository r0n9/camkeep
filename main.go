package main

import (
	"bytes"
	"camkeep/constant"
	"camkeep/service"
	"camkeep/slog"
	"camkeep/task"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
)

var httpClient = &http.Client{
	Timeout: 3 * time.Second, // 3秒超时，防止任何请求卡死
}

const ConfigFilePath = "config/conf.yaml" // 定义统一的配置文件路径

// Config 对应 yaml 配置文件
type Config struct {
	Cameras []task.Camera `yaml:"cameras"`
}

var (
	currentConfig Config
	configMux     sync.RWMutex
	restartMux    sync.Mutex // 热重启专属防并发锁
	reloadCancel  context.CancelFunc
	taskWg        sync.WaitGroup
	ctxGlobal     context.Context
)

func main() {
	mime.AddExtensionType(".ts", "video/mp2t")
	slog.Init()

	// 1. 读取或初始化配置 (如果不存在则创建空配置)
	currentConfig = loadOrInitConfig()

	os.MkdirAll(constant.RecordBaseDir, 0755)

	// 设置全局 Context
	var cancelGlobal context.CancelFunc
	ctxGlobal, cancelGlobal = context.WithCancel(context.Background())

	// 初始化流
	initGo2rtcStreams(currentConfig)

	// 启动 Web 路由
	go startWebServer()

	// 启动后台录制与清理任务
	startTasks()

	// 启动实时流状态轮询任务
	go pollGo2rtcStatus()

	// 监听退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("接收到退出信号，正在停止所有任务...")

	cleanupGo2rtcStreams(currentConfig)
	cancelGlobal() // 通知所有层级的 Context 退出
	taskWg.Wait()  // 等待所有任务完成
	log.Println("程序已安全退出。")
}

// loadOrInitConfig 如果配置文件不存在则生成一个带示例的默认配置
func loadOrInitConfig() Config {
	os.MkdirAll(filepath.Dir(ConfigFilePath), 0755)

	data, err := os.ReadFile(ConfigFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("conf.yaml 不存在，自动创建默认模板配置...")

			// 定义默认配置模板
			defaultCfg := Config{
				Cameras: []task.Camera{
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

			if err := os.WriteFile(ConfigFilePath, finalOut, 0644); err != nil {
				log.Printf("写入配置文件失败: %v", err)
			}

			return defaultCfg
		}
		log.Fatalf("读取配置文件失败: %v", err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}
	return c
}

// startTasks 启动或重启所有的摄像头监控任务
func startTasks() {
	var ctx context.Context
	ctx, reloadCancel = context.WithCancel(ctxGlobal)

	configMux.RLock()
	cams := currentConfig.Cameras
	configMux.RUnlock()

	taskWg.Add(1)
	go task.CleanupTask(ctx, &taskWg, cams)

	for _, cam := range cams {
		taskWg.Add(1)
		go task.CameraTask(ctx, &taskWg, cam)
	}
}

// restartTasks 热重启任务 (用于保存配置后生效)
func restartTasks(newConfig Config) {
	// restartMux 互斥锁，防并发重启
	restartMux.Lock()
	defer restartMux.Unlock()

	log.Println("检测到配置更改，正在重启底层任务...")
	if reloadCancel != nil {
		reloadCancel() // 取消旧的录像和清理任务
	}
	taskWg.Wait() // 阻塞等待旧任务全部安全退出 (此时录像已停)

	// 1. 获取旧配置的快照 (用于注销旧流)，极速释放锁
	configMux.RLock()
	oldConfig := currentConfig
	configMux.RUnlock()

	// 2. 执行耗时的网络请求操作 (千万不要在这里加锁！)
	cleanupGo2rtcStreams(oldConfig)
	initGo2rtcStreams(newConfig)

	// 3. 极速替换新配置 (只锁赋值这一瞬间)
	configMux.Lock()
	currentConfig = newConfig
	configMux.Unlock()

	// 4. 【新增】清理内存中被删除的“幽灵”摄像头状态
	cleanGhostStatus(newConfig)

	// 5. 启动新任务
	startTasks()
	log.Println("任务热重启完成！")
}

// cleanGhostStatus 清理掉已经被移出配置的摄像头状态
func cleanGhostStatus(newConfig Config) {
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
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// 2. 获取所有摄像头状态
	r.GET("/api/status", func(c *gin.Context) {
		service.StatusMux.RLock()
		defer service.StatusMux.RUnlock()
		c.JSON(200, service.StatusMap)
	})

	// 获取配置文件内容
	r.GET("/api/config", func(c *gin.Context) {
		data, _ := os.ReadFile(ConfigFilePath) // 【修改】
		c.String(200, string(data))
	})

	// 保存并应用配置文件
	r.POST("/api/config", func(c *gin.Context) {
		yamlBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(400, gin.H{"error": "读取请求失败"})
			return
		}
		var newConfig Config
		if err := yaml.Unmarshal(yamlBytes, &newConfig); err != nil {
			c.JSON(400, gin.H{"error": "YAML 格式有误: " + err.Error()})
			return
		}
		os.WriteFile(ConfigFilePath, yamlBytes, 0644)

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
		type RecordFile struct {
			Name string `json:"name"`
			Url  string `json:"url"`
		}
		var files []RecordFile
		baseDir := filepath.Join(constant.RecordBaseDir, camID)

		// 【修改为 WalkDir】
		filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			// 使用 d.IsDir() 替代 info.IsDir()，大幅节省 I/O 资源
			if !d.IsDir() && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".mp4")) {
				relPath, _ := filepath.Rel(constant.RecordBaseDir, path)
				files = append(files, RecordFile{
					Name: filepath.Base(path), // filepath.Base 直接处理字符串，性能极高
					Url:  "/play/" + filepath.ToSlash(relPath),
				})
			}
			return nil
		})
		c.JSON(200, files)
	})

	r.StaticFS("/play", http.Dir(constant.RecordBaseDir))

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	r.POST("/webrtc/:id", func(c *gin.Context) {
		camID := c.Param("id")

		configMux.RLock()
		var targetCam *task.Camera
		for _, cam := range currentConfig.Cameras {
			if cam.ID == camID {
				targetCam = &cam
				break
			}
		}
		configMux.RUnlock()

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

		go2rtcHost := fmt.Sprintf("http://%s:1984", constant.Go2rtcHost)

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

// initGo2rtcStreams 负责在启动时清理 go2rtc 历史流，并注册当前配置的所有流
func initGo2rtcStreams(config Config) {
	go2rtcHost := fmt.Sprintf("http://%s:1984", constant.Go2rtcHost)
	log.Println("正在连接 go2rtc 并初始化视频流...")

	// 1. 等待 go2rtc 服务就绪 (最多等待 10 秒)
	for i := 0; i < 10; i++ {
		resp, err := httpClient.Get(go2rtcHost + "/api/streams")
		if err == nil && resp.StatusCode == http.StatusOK {
			// 2. 获取并清理所有存在的历史流
			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				var streamKeys []string

				// 兼容不同版本的 go2rtc API 数据结构
				if streamsObj, ok := result["streams"].(map[string]interface{}); ok {
					for k := range streamsObj {
						streamKeys = append(streamKeys, k)
					}
				} else {
					for k := range result {
						streamKeys = append(streamKeys, k)
					}
				}

				// 发送 DELETE 请求清理旧流
				for _, streamName := range streamKeys {
					reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/streams?src=%s", go2rtcHost, streamName), nil)
					if respDel, errDel := httpClient.Do(reqDel); errDel == nil {
						respDel.Body.Close()
					}
				}
				if len(streamKeys) > 0 {
					log.Printf("已清理 go2rtc 中的 %d 个历史流", len(streamKeys))
				}
			}
			resp.Body.Close()

			for _, cam := range config.Cameras {
				addStreamURL := fmt.Sprintf("%s/api/streams?name=%s&src=%s", go2rtcHost, cam.ID, url.QueryEscape(cam.RTSPUrl))
				reqAdd, _ := http.NewRequest("PUT", addStreamURL, nil)
				respAdd, errAdd := http.DefaultClient.Do(reqAdd)

				if errAdd != nil {
					log.Printf("[%s] 注册到 go2rtc 失败: %v", cam.ID, errAdd)
				} else if respAdd != nil {
					if respAdd.StatusCode >= 400 {
						log.Printf("[%s] 注册失败，状态码: %d", cam.ID, respAdd.StatusCode)
					} else {
						log.Printf("[%s] 已成功注册到 go2rtc", cam.ID)
					}
					respAdd.Body.Close()
				}
			}
			log.Println("go2rtc 视频流初始化完毕！")
			return // 初始化成功，退出循环
		}

		if i == 0 {
			log.Println("等待 go2rtc 服务启动...")
		}
		time.Sleep(1 * time.Second)
	}
	log.Println("警告：无法连接到 go2rtc，流初始化超时，请确保 go2rtc 已启动！")
}

// cleanupGo2rtcStreams 在程序退出前注销已注册的视频流
func cleanupGo2rtcStreams(config Config) {
	// 注意这里使用我们在上一步修改的 go2rtc 容器名地址
	go2rtcHost := fmt.Sprintf("http://%s:1984", constant.Go2rtcHost)
	log.Println("正在从 go2rtc 注销视频流...")

	for _, cam := range config.Cameras {
		deleteURL := fmt.Sprintf("%s/api/streams?src=%s", go2rtcHost, cam.ID)
		reqDel, err := http.NewRequest("DELETE", deleteURL, nil)
		if err != nil {
			continue
		}

		// 设置较短的超时时间，防止退出时卡死
		client := &http.Client{Timeout: 2 * time.Second}
		resp, errDel := client.Do(reqDel)
		if errDel == nil {
			resp.Body.Close()
			log.Printf("[%s] go2rtc 视频流已注销", cam.ID)
		} else {
			log.Printf("[%s] go2rtc 视频流注销失败: %v", cam.ID, errDel)
		}
	}
	log.Println("go2rtc 视频流清理完毕。")
}

// pollGo2rtcStatus 定期轮询 go2rtc 接口，深度判断流的真实健康度
func pollGo2rtcStatus() {
	go2rtcHost := fmt.Sprintf("http://%s:1984", constant.Go2rtcHost)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		resp, err := httpClient.Get(go2rtcHost + "/api/streams")
		if err != nil || resp.StatusCode != http.StatusOK { // 加入状态码判断
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			markAllStreamOffline()
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		var streams map[string]interface{}
		if s, ok := result["streams"].(map[string]interface{}); ok {
			streams = s
		} else {
			streams = result
		}

		configMux.RLock()
		var camIDs []string
		for _, cam := range currentConfig.Cameras {
			camIDs = append(camIDs, cam.ID)
		}
		configMux.RUnlock()

		for _, id := range camIDs {
			streamState := "offline" // 默认假设为离线

			if camData, exists := streams[id]; exists {
				if data, ok := camData.(map[string]interface{}); ok {
					producers, hasProducers := data["producers"].([]interface{})
					consumers, hasConsumers := data["consumers"].([]interface{})

					if hasProducers && len(producers) > 0 {
						// 1. 有 producer，深入检查其健康度
						for _, p := range producers {
							if prod, ok := p.(map[string]interface{}); ok {
								// 如果存在错误字段，说明连接正在报错 (如 i/o timeout)
								if errStr, hasErr := prod["error"].(string); hasErr && errStr != "" {
									continue
								}
								// 检查是否真正收到了数据(bytes_recv) 或 成功解析了媒体轨(medias)
								bytesRecv, _ := prod["bytes_recv"].(float64)
								medias, hasMedias := prod["medias"].([]interface{})

								if bytesRecv > 0 || (hasMedias && len(medias) > 0) {
									streamState = "online"
									break
								}
							}
						}
					} else if (!hasProducers || len(producers) == 0) && (!hasConsumers || len(consumers) == 0) {
						// 2. 没有生产者也没消费者：属于 go2rtc 的“按需休眠”状态，属于正常预期
						streamState = "idle"
					} else if (!hasProducers || len(producers) == 0) && (hasConsumers && len(consumers) > 0) {
						// 3. 有人在请求流(比如FFmpeg在跑)，但 producer 却没建起来，说明彻底连不上
						streamState = "offline"
					}
				}
			}
			service.UpdateOnlineStatus(id, streamState)
		}
	}
}

func markAllStreamOffline() {
	configMux.RLock()
	defer configMux.RUnlock()
	for _, cam := range currentConfig.Cameras {
		service.UpdateOnlineStatus(cam.ID, "offline")
	}
}
