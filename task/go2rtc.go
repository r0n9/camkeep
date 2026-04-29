package task

import (
	"camkeep/constant"
	"camkeep/service"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
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

// InitGo2rtcStreams 负责在启动时清理 go2rtc 历史流，并注册当前配置的所有流
func InitGo2rtcStreams(config constant.Config) {
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
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

// CleanupGo2rtcStreams 在程序退出前注销已注册的视频流
func CleanupGo2rtcStreams(config constant.Config) {
	// 注意这里使用我们在上一步修改的 go2rtc 容器名地址
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
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
			go initGo2rtcStreams(*cfg)
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

			if camData, exists := streams[id]; exists {
				if data, ok := camData.(map[string]interface{}); ok {
					producers, hasProducers := data["producers"].([]interface{})
					consumers, hasConsumers := data["consumers"].([]interface{})

					if hasProducers && len(producers) > 0 {
						// 1. 有 producer，深入检查其健康度
						isConnecting := false
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
								} else {
									// 没报错且没数据，说明正在握手连接中
									isConnecting = true
								}
							}
						}

						if streamState != "online" && isConnecting {
							streamState = "online"
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

// initGo2rtcStreams 负责在启动时清理 go2rtc 历史流，并注册当前配置的所有流
func initGo2rtcStreams(config constant.Config) {
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
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

func markAllStreamOffline(currentConfig *constant.Config) {
	constant.ConfigMux.RLock()
	defer constant.ConfigMux.RUnlock()
	for _, cam := range currentConfig.Cameras {
		service.UpdateOnlineStatus(cam.ID, "offline")
	}
}
