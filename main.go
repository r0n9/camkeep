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

// Config 对应 yaml 配置文件
type Config struct {
	Cameras []task.Camera `yaml:"cameras"`
}

func main() {

	// 注册 .ts 格式，确保后端返回正确的 Content-Type
	mime.AddExtensionType(".ts", "video/mp2t")

	slog.Init() // 日志初始化

	// 1. 读取配置
	data, err := os.ReadFile("conf.yaml")
	if err != nil {
		log.Fatalf("读取配置文件失败: %v", err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	// 2. 创建基础录像目录
	os.MkdirAll(constant.RecordBaseDir, 0755)

	// 3. 设置全局 Context，用于平滑退出
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 启动前统一初始化 go2rtc 流
	initGo2rtcStreams(config)

	// 启动 Web 服务器后台协程
	go startWebServer(config)

	// 4. 启动全局清理守护进程
	wg.Add(1)
	go task.CleanupTask(ctx, &wg, config.Cameras)

	// 5. 启动各个摄像头的录制调度任务
	for _, cam := range config.Cameras {
		wg.Add(1)
		go task.CameraTask(ctx, &wg, cam)
	}

	// 6. 监听系统中断信号 (Ctrl+C, Docker stop)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("接收到退出信号，正在停止所有录制任务...")

	cleanupGo2rtcStreams(config)

	cancel()  // 通知所有 Goroutine 退出
	wg.Wait() // 等待所有资源清理完毕
	log.Println("程序已安全退出。")
}

func startWebServer(config Config) {
	// 设置 Gin 为发布模式，减少控制台日志干扰
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

	// 3. 录像文件列表 API (修正路径返回，方便前端直接播放)
	r.GET("/api/records/:id", func(c *gin.Context) {
		camID := c.Param("id")
		type RecordFile struct {
			Name string `json:"name"`
			Url  string `json:"url"`
		}
		var files []RecordFile
		baseDir := filepath.Join(constant.RecordBaseDir, camID)

		filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".mp4")) {
				// 将本地路径转换为网络 URL 路径 (例如: records/110/... -> /play/110/...)
				relPath, _ := filepath.Rel(constant.RecordBaseDir, path)
				files = append(files, RecordFile{
					Name: filepath.Base(path),
					Url:  "/play/" + filepath.ToSlash(relPath),
				})
			}
			return nil
		})
		c.JSON(200, files)
	})

	// 4. 静态资源服务 (录像播放)
	r.StaticFS("/play", http.Dir(constant.RecordBaseDir))

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	r.POST("/webrtc/:id", func(c *gin.Context) {
		camID := c.Param("id")

		// 检查配置中是否存在该摄像头
		var targetCam *task.Camera
		for _, cam := range config.Cameras {
			if cam.ID == camID {
				targetCam = &cam
				break
			}
		}
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
		resp, err := http.Get(go2rtcHost + "/api/streams")
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
					if respDel, errDel := http.DefaultClient.Do(reqDel); errDel == nil {
						respDel.Body.Close()
					}
				}
				if len(streamKeys) > 0 {
					log.Printf("已清理 go2rtc 中的 %d 个历史流", len(streamKeys))
				}
			}
			resp.Body.Close()

			// 3. 将 yaml 配置中的流批量注册到 go2rtc
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
