package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/service"
)

var httpClient = &http.Client{
	Timeout: 3 * time.Second, // 3秒超时，防止任何请求卡死
}

// StartGo2rtcDaemon 负责启动并守护底层流媒体引擎
func StartGo2rtcDaemon() {
	go func() {
		for {
			log.Println("正在启动底层流媒体引擎 go2rtc...")

			// 调用同目录下的 go2rtc 二进制文件
			cmd := exec.Command("./go2rtc")

			// 如果你想在终端看到 go2rtc 的原生日志，可以取消下面两行的注释
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			// 阻塞运行，直到进程意外退出
			err := cmd.Run()

			log.Printf("go2rtc 进程退出: %v，3秒后尝试重启...", err)
			time.Sleep(3 * time.Second) // 缓冲时间，防止死循环狂刷日志
		}
	}()
}

// WaitForGo2rtcReady 轮询探测 go2rtc API 是否就绪
// timeout: 最大容忍的等待时间
func WaitForGo2rtcReady(timeout time.Duration) error {
	go2rtcAPI := fmt.Sprintf("http://%s:%d/api/streams", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)

	client := &http.Client{
		Timeout: 500 * time.Millisecond, // 单词请求超时设短一点，方便快速重试
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(go2rtcAPI)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil // 成功访问，确认彻底就绪
		}
		if resp != nil {
			resp.Body.Close()
		}

		// 间隔 200 毫秒再次探测，既不吃 CPU 也能毫秒级响应就绪状态
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("等待 go2rtc 启动超时 (%v)", timeout)
}

// InitGo2rtcStreams 负责在启动时注册和更新当前配置中的流
func InitGo2rtcStreams(config constant.Config) {
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	log.Println("正在连接 go2rtc 并初始化视频流...")

	// 1. 等待 go2rtc 服务就绪 (最多等待 10 秒)
	for i := 0; i < 10; i++ {
		resp, err := httpClient.Get(go2rtcHost + "/api/streams")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close() // 服务已就绪，不再需要解析历史流

			// 2. 遍历当前配置文件中的摄像头
			for _, cam := range config.Cameras {
				if cam.AutoDiscovered {
					log.Printf("[%s] 识别为 go2rtc 原生流，已接管", cam.ID)
					continue
				}

				// 只针对当前 conf.yaml 里存在的流，先删后加
				// 这一步确保了如果该流被修改了 RTSP 地址，旧地址会被彻底顶替掉
				reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/streams?src=%s", go2rtcHost, url.QueryEscape(cam.ID)), nil)
				if respDel, errDel := httpClient.Do(reqDel); errDel == nil && respDel != nil {
					respDel.Body.Close()
				}

				// 注册最新的流配置
				addStreamURL := fmt.Sprintf("%s/api/streams?name=%s&src=%s", go2rtcHost, url.QueryEscape(cam.ID), url.QueryEscape(cam.RTSPUrl))
				reqAdd, _ := http.NewRequest("PUT", addStreamURL, nil)
				respAdd, errAdd := httpClient.Do(reqAdd)

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

// CleanupGo2rtcStreams 在程序退出前注销已注册的视频流
func CleanupGo2rtcStreams(config constant.Config) {
	// 注意这里使用我们在上一步修改的 go2rtc 容器名地址
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	log.Println("正在从 go2rtc 注销视频流...")

	for _, cam := range config.Cameras {
		if cam.AutoDiscovered {
			// go2rtc 上注册的流，不能注销
			log.Printf("[%s] go2rtc 上注册的流，不注销", cam.ID)
			continue
		}

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

// PollGo2rtcStatus 定期轮询 go2rtc 接口，深度判断流的真实健康度
func PollGo2rtcStatus(cfg *constant.Config) {
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		resp, err := httpClient.Get(go2rtcHost + "/api/streams")
		if err != nil || resp.StatusCode != http.StatusOK { // 加入状态码判断
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			markAllStreamOffline(cfg)
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

		// === 自愈逻辑开始 ===
		constant.ConfigMux.RLock()
		cams := cfg.Cameras
		constant.ConfigMux.RUnlock()

		// 如果我们配置了摄像头，但 go2rtc 里一条流都没有，说明 go2rtc 重启失忆了
		if len(cams) > 0 && len(streams) == 0 {
			log.Println("检测到 go2rtc 丢失所有流配置(可能已重启)，正在重新注入...")
			// 异步重新注入，避免阻塞轮询
			go InitGo2rtcStreams(*cfg)
			continue // 跳过本次状态更新，等下一轮
		}
		// === 自愈逻辑结束 ===

		constant.ConfigMux.RLock()
		var camIDs []string
		for _, cam := range cfg.Cameras {
			camIDs = append(camIDs, cam.ID)
		}
		constant.ConfigMux.RUnlock()

		for _, id := range camIDs {
			streamState := "offline" // 默认假设为离线
			isIdle := false          // 标记是否进入了“薛定谔”的待机状态
			var probeURL string      // 存放真实的探活物理地址

			if camData, exists := streams[id]; exists {
				if data, ok := camData.(map[string]interface{}); ok {
					producers, hasProducers := data["producers"].([]interface{})
					consumers, hasConsumers := data["consumers"].([]interface{})

					consumerCount := 0
					if hasConsumers && consumers != nil {
						consumerCount = len(consumers)
					}

					// go2rtc 只要注册了流，hasProducers 就是 true，且至少包含一条 {"url": "..."}
					if hasProducers && len(producers) > 0 {
						isActive := false // 是否有真实数据在流动
						hasError := false // 是否有明确的拉流报错 (如 i/o timeout)

						for _, p := range producers {
							if prod, ok := p.(map[string]interface{}); ok {
								// 优先从 go2rtc 底层状态中提取真实物理 URL
								if u, hasU := prod["url"].(string); hasU && u != "" {
									probeURL = u
								}

								// 1. 检查是否存在明确报错（只有在有人看，且拉流失败时才会出现）
								if errStr, hasErr := prod["error"].(string); hasErr && errStr != "" {
									hasError = true
									continue
								}

								// 2. 检查是否正在真实收发数据
								bytesRecv, _ := prod["bytes_recv"].(float64)
								medias, hasMedias := prod["medias"].([]interface{})

								if bytesRecv > 0 || (hasMedias && len(medias) > 0) {
									isActive = true
									break
								}
							}
						}

						// 状态仲裁
						if isActive {
							streamState = "online" // 数据流转中，绝对健康
						} else if hasError {
							streamState = "offline" // 有明确报错，离线
						} else {
							// 既没报错也没数据，说明 `producer` 里只剩下一个干瘪的 {"url": "..."}
							if consumerCount > 0 {
								// 有人在请求，但还没拿到数据/也没报错，说明“正在握手建联中”
								streamState = "online"
							} else {
								// 没人请求，也没数据。进入了 go2rtc 无法分辨的盲区
								isIdle = true
							}
						}
					}
				}
			}
			if isIdle {
				// 1. 安全地读取该摄像头上一次的状态
				service.StatusMux.RLock()
				prevState := "offline"
				if status, exists := service.StatusMap[id]; exists {
					prevState = status.StreamState
				}
				service.StatusMux.RUnlock()

				// 2. 根据历史状态决定是否需要发起真实探活
				if prevState == "idle" {
					// 如果之前已经是休眠状态，直接继承，跳过 TCP 探活。
					// 如果它真的在此期间断电了，等到下次有业务拉流时，
					// go2rtc 会报错产生 error 进而被上面的逻辑打回 offline。
					streamState = "idle"
				} else {
					// 如果 go2rtc 里真没给 URL，才去 conf.yaml 兜底
					if probeURL == "" {
						constant.ConfigMux.RLock()
						for _, c := range cfg.Cameras {
							if c.ID == id {
								probeURL = c.RTSPUrl
								break
							}
						}
						constant.ConfigMux.RUnlock()
					}

					// 发起毫秒级轻量探活
					if checkCameraTCPAlive(probeURL) {
						streamState = "idle" // 端口通，才是真休眠
					} else {
						streamState = "offline" // 端口不通（如断网/断电），伪装成休眠也没用，标记为离线
					}
				}
			}

			service.UpdateOnlineStatus(id, streamState)
		}
	}
}

// checkCameraTCPAlive 极低损耗的旁路探活：仅验证摄像头的 RTSP 端口是否存活
func checkCameraTCPAlive(rawURL string) bool {
	if rawURL == "" {
		return false
	}

	if rawURL == "managed_by_go2rtc" {
		return true
	}

	// 如果配置了 ffmpeg 实时转码 (例如 ffmpeg:rtsp://...)，需要剥离前缀
	cleanURL := rawURL
	if strings.HasPrefix(cleanURL, "ffmpeg:") {
		cleanURL = strings.TrimPrefix(cleanURL, "ffmpeg:")
	}

	u, err := url.Parse(cleanURL)
	if err != nil {
		return false
	}

	host := u.Hostname()
	if host == "" {
		return false
	}

	port := u.Port()
	if port == "" {
		port = "554" // RTSP 协议默认端口
	}

	// 1秒超时，不占用 CPU，只进行 TCP 握手
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		return false
	}

	conn.Close()
	return true
}

func markAllStreamOffline(currentConfig *constant.Config) {
	constant.ConfigMux.RLock()
	defer constant.ConfigMux.RUnlock()
	for _, cam := range currentConfig.Cameras {
		service.UpdateOnlineStatus(cam.ID, "offline")
	}
}
