package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/service"
)

var httpClient = &http.Client{
	Timeout: 3 * time.Second, // 3秒超时，防止任何请求卡死
}

const (
	streamRecoveryProbeWindow = 15 * time.Second
	streamRecoveryBaseBackoff = 30 * time.Second
	streamRecoveryMaxBackoff  = 5 * time.Minute
	streamIdleProbeInterval   = 30 * time.Second
)

// streamRecoveryState 只用于处理“TCP 已恢复但 RTSP/媒体流仍不可用”的灰色状态。
// ProbeUntil 是允许业务侧重新拉流的短窗口；窗口结束仍未 online 时进入 NextRetryAfter 退避。
type streamRecoveryState struct {
	ProbeUntil     time.Time
	NextRetryAfter time.Time
	Failures       int
}

// streamIdleProbeState 记录无消费者 idle 态的轻量探活缓存。
// go2rtc 只返回 URL 且没有消费者时无法证明真实媒体流可用，这里只缓存 TCP 可达性，避免每轮轮询都拨号。
type streamIdleProbeState struct {
	LastProbeAt time.Time
	Reachable   bool
}

var (
	streamRecoveryMux  sync.Mutex
	streamRecoveries   = make(map[string]streamRecoveryState)
	streamIdleProbeMux sync.Mutex
	streamIdleProbes   = make(map[string]streamIdleProbeState)
	// go2rtcReinjectInFlight 自愈重注入流程的去重标志，保证同一时刻只有一个 InitGo2rtcStreams 在跑。
	go2rtcReinjectInFlight atomic.Bool
)

// StartGo2rtcDaemon 负责启动并守护底层流媒体引擎。
// 返回的 channel 会在守护 goroutine 退出后关闭。
func StartGo2rtcDaemon(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				log.Println("go2rtc 守护进程已停止")
				return
			default:
			}

			log.Println("正在启动底层流媒体引擎 go2rtc...")

			if err := prepareGo2rtcConfig(constant.LegacyGo2rtcConfigFilePath, constant.Go2rtcConfigFilePath); err != nil {
				log.Printf("go2rtc 配置文件准备失败: %v，3秒后尝试重试...", err)
				if !waitGo2rtcDaemonRetry(ctx, 3*time.Second) {
					log.Println("go2rtc 守护进程已停止")
					return
				}
				continue
			}

			// 调用同目录下的 go2rtc 二进制文件
			cmd := exec.CommandContext(ctx, "./go2rtc", "-config", constant.Go2rtcConfigFilePath)

			// 如果你想在终端看到 go2rtc 的原生日志，可以取消下面两行的注释
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			// 阻塞运行，直到进程意外退出
			err := cmd.Run()

			if ctx.Err() != nil {
				log.Println("go2rtc 进程已随 CamKeep 退出")
				return
			}

			log.Printf("go2rtc 进程退出: %v，3秒后尝试重启...", err)
			if !waitGo2rtcDaemonRetry(ctx, 3*time.Second) {
				log.Println("go2rtc 守护进程已停止")
				return
			}
		}
	}()
	return done
}

func waitGo2rtcDaemonRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func prepareGo2rtcConfig(legacyPath, configPath string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建 go2rtc 配置目录失败: %w", err)
	}

	if _, err := os.Stat(configPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查 go2rtc 配置文件失败: %w", err)
	}

	legacyInfo, err := os.Stat(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("检查旧 go2rtc 配置文件失败: %w", err)
	}
	if legacyInfo.IsDir() {
		return fmt.Errorf("旧 go2rtc 配置路径是目录: %s", legacyPath)
	}

	if err := copyFileExclusive(legacyPath, configPath, legacyInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("迁移旧 go2rtc 配置文件失败: %w", err)
	}

	log.Printf("已将旧 go2rtc 配置从 %s 迁移到 %s", legacyPath, configPath)
	return nil
}

func copyFileExclusive(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
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
			_ = resp.Body.Close() // 服务已就绪，不再需要解析历史流

			// 2. 遍历当前配置文件中的摄像头
			for _, cam := range config.Cameras {
				streamURL := cam.EffectiveStreamURL()
				if constant.CameraManagedByGo2rtc(cam) {
					log.Printf("[%s] 识别为 go2rtc 原生流，已接管", cam.ID)
					continue
				}
				if streamURL == "" {
					log.Printf("[%s] 未配置主码流地址，跳过注册到 go2rtc", cam.ID)
					continue
				}

				// 只针对当前 conf.yaml 里存在的流，先删后加
				// 这一步确保了如果该流被修改了主码流地址，旧地址会被彻底顶替掉
				reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/streams?src=%s", go2rtcHost, url.QueryEscape(cam.ID)), nil)
				if respDel, errDel := httpClient.Do(reqDel); errDel == nil && respDel != nil {
					_ = respDel.Body.Close()
				}

				// 注册最新的流配置
				addStreamURL := fmt.Sprintf("%s/api/streams?name=%s&src=%s", go2rtcHost, url.QueryEscape(cam.ID), url.QueryEscape(streamURL))
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
					_ = respAdd.Body.Close()
				}
			}
			log.Println("go2rtc 视频流初始化完毕！")
			return // 初始化成功，退出循环
		}

		if resp != nil {
			_ = resp.Body.Close()
		}

		if i == 0 {
			log.Println("等待 go2rtc 服务启动...")
		}
		time.Sleep(1 * time.Second)
	}
	log.Println("警告：无法连接到 go2rtc，流初始化超时，请确保 go2rtc 已启动！")
}

// CleanupGo2rtcStreams 在配置热重启时注销旧配置注册的视频流。
func CleanupGo2rtcStreams(config constant.Config) {
	// 注意这里使用我们在上一步修改的 go2rtc 容器名地址
	go2rtcHost := fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	log.Println("正在从 go2rtc 注销视频流...")

	for _, cam := range config.Cameras {
		if constant.CameraManagedByGo2rtc(cam) {
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
			_ = resp.Body.Close()
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
				_ = resp.Body.Close()
			}
			markAllStreamOffline(cfg)
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			_ = resp.Body.Close()
			continue
		}
		_ = resp.Body.Close()

		var streams map[string]interface{}
		if s, ok := result["streams"].(map[string]interface{}); ok {
			streams = s
		} else {
			streams = result
		}

		// 统一在读锁下做配置快照（Camera 全为值类型字段，拷贝 slice 即深拷贝），
		// 后续逻辑不再直接触碰 cfg 指向的共享配置，避免与热重载的整体替换产生数据竞争。
		constant.ConfigMux.RLock()
		cfgSnapshot := *cfg
		cfgSnapshot.Cameras = append([]constant.Camera(nil), cfg.Cameras...)
		constant.ConfigMux.RUnlock()

		// === 自愈逻辑开始 ===
		// 如果我们配置了摄像头，但 go2rtc 里一条流都没有，说明 go2rtc 重启失忆了
		if len(cfgSnapshot.Cameras) > 0 && len(streams) == 0 {
			// CompareAndSwap 确保同一时刻只有一个重注入流程在跑：
			// InitGo2rtcStreams 内部最长可执行 10 秒，而本轮询每 3 秒一次，
			// 不去重会堆积多个并发注入互相覆盖 DELETE/PUT。
			if go2rtcReinjectInFlight.CompareAndSwap(false, true) {
				log.Println("检测到 go2rtc 丢失所有流配置(可能已重启)，正在重新注入...")
				// 异步重新注入，避免阻塞轮询
				go func(cfgCopy constant.Config) {
					defer go2rtcReinjectInFlight.Store(false)
					InitGo2rtcStreams(cfgCopy)
				}(cfgSnapshot)
			}
			continue // 跳过本次状态更新，等下一轮
		}
		// === 自愈逻辑结束 ===

		for _, cam := range cfgSnapshot.Cameras {
			service.StatusMux.RLock()
			prevState := "offline"
			if status, exists := service.StatusMap[cam.ID]; exists {
				prevState = status.StreamState
			}
			service.StatusMux.RUnlock()

			streamState := go2rtcStreamState(cam.ID, streams[cam.ID], cam.EffectiveStreamURL(), prevState, time.Now(), checkCameraTCPAlive)
			service.UpdateOnlineStatus(cam.ID, streamState)
		}
	}
}

func go2rtcStreamState(camID string, camData interface{}, configuredURL string, prevState string, now time.Time, tcpAlive func(string) bool) string {
	if now.IsZero() {
		now = time.Now()
	}

	streamState := "offline" // 默认假设为离线
	isIdle := false          // 标记是否进入了 go2rtc 无法分辨的待验证状态
	var producerURL string   // 存放 go2rtc 返回的物理地址

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
					if u, hasU := prod["url"].(string); hasU && u != "" {
						producerURL = u
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
				clearStreamRecovery(camID)
				clearStreamIdleProbe(camID)
				return "online" // 数据流转中，绝对健康
			}
			if hasError {
				clearStreamIdleProbe(camID)
				// go2rtc 可能保留旧的 producer error。这里先用配置里的真实流地址做轻量 TCP 探活，
				// 但 TCP 通只代表设备端口恢复，不代表 RTSP/媒体流可用，因此后面还会套恢复窗口和退避。
				probeURL := strings.TrimSpace(configuredURL)
				if probeURL == "" {
					probeURL = producerURL
				}
				if streamProbeAlive(probeURL, tcpAlive) {
					return streamStateForReachableProducerError(camID, now)
				}
				clearStreamRecovery(camID)
				return "offline"
			}
			// 既没报错也没数据，说明 `producer` 里只剩下一个干瘪的 {"url": "..."}
			if consumerCount > 0 {
				clearStreamRecovery(camID)
				clearStreamIdleProbe(camID)
				return "online" // 有人在请求，但还没拿到数据/也没报错，说明“正在握手建联中”
			}
			clearStreamRecovery(camID)
			isIdle = true // 没人请求，也没数据。进入了 go2rtc 无法分辨的盲区
		}
	}

	if isIdle {
		// 优先沿用 go2rtc 给出的物理地址，没有才回退到配置地址。
		probeURL := producerURL
		if probeURL == "" {
			probeURL = strings.TrimSpace(configuredURL)
		}
		return streamStateForIdleProducer(camID, probeURL, prevState, now, tcpAlive)
	}

	return streamState
}

func streamStateForIdleProducer(camID string, probeURL string, prevState string, now time.Time, tcpAlive func(string) bool) string {
	if now.IsZero() {
		now = time.Now()
	}

	// 无消费者时 go2rtc 没有真实媒体状态；idle 只表示“近期 TCP 探活可达、等待业务拉流验证”。
	// 上一次已经是 idle 时复用短期缓存，避免大量摄像头每 3 秒都做 TCP dial。
	if prevState == "idle" {
		streamIdleProbeMux.Lock()
		cached, ok := streamIdleProbes[camID]
		streamIdleProbeMux.Unlock()
		if ok && now.Sub(cached.LastProbeAt) < streamIdleProbeInterval {
			if cached.Reachable {
				return "idle"
			}
			return "offline"
		}
	}

	reachable := streamProbeAlive(probeURL, tcpAlive)
	streamIdleProbeMux.Lock()
	streamIdleProbes[camID] = streamIdleProbeState{
		LastProbeAt: now,
		Reachable:   reachable,
	}
	streamIdleProbeMux.Unlock()

	if reachable {
		return "idle"
	}
	return "offline"
}

func streamStateForReachableProducerError(camID string, now time.Time) string {
	streamRecoveryMux.Lock()
	defer streamRecoveryMux.Unlock()

	state := streamRecoveries[camID]

	// 退避期内继续保持 offline，避免 FFmpeg 因为 TCP 端口已通而高频重启。
	if !state.NextRetryAfter.IsZero() && now.Before(state.NextRetryAfter) {
		return "offline"
	}

	// 到达退避截止时间后，再给一个短暂的 idle 窗口，让业务侧有机会重新拉流验证。
	if state.ProbeUntil.IsZero() || (!state.NextRetryAfter.IsZero() && !now.Before(state.NextRetryAfter)) {
		state.ProbeUntil = now.Add(streamRecoveryProbeWindow)
		state.NextRetryAfter = time.Time{}
		streamRecoveries[camID] = state
		return "idle"
	}

	// 窗口期内返回 idle，录制任务可以发起一次或少量重试来刷新 go2rtc 的真实流状态。
	if now.Before(state.ProbeUntil) {
		return "idle"
	}

	// 恢复窗口结束后仍然只有 producer.error，说明端口通但媒体流不可用，进入指数退避。
	state.Failures++
	state.NextRetryAfter = now.Add(streamRecoveryBackoff(state.Failures))
	state.ProbeUntil = time.Time{}
	streamRecoveries[camID] = state
	return "offline"
}

func clearStreamRecovery(camID string) {
	streamRecoveryMux.Lock()
	delete(streamRecoveries, camID)
	streamRecoveryMux.Unlock()
}

func clearStreamIdleProbe(camID string) {
	streamIdleProbeMux.Lock()
	delete(streamIdleProbes, camID)
	streamIdleProbeMux.Unlock()
}

func streamRecoveryBackoff(failures int) time.Duration {
	if failures <= 0 {
		return streamRecoveryBaseBackoff
	}
	backoff := streamRecoveryBaseBackoff
	for i := 1; i < failures; i++ {
		backoff *= 2
		if backoff >= streamRecoveryMaxBackoff {
			return streamRecoveryMaxBackoff
		}
	}
	return backoff
}

func streamProbeAlive(probeURL string, tcpAlive func(string) bool) bool {
	probeURL = strings.TrimSpace(probeURL)
	if probeURL == "" {
		return false
	}
	if strings.HasPrefix(probeURL, "exec") || strings.HasPrefix(probeURL, "ffmpeg") {
		return true
	}
	if tcpAlive == nil {
		tcpAlive = checkCameraTCPAlive
	}
	return tcpAlive(probeURL)
}

// checkCameraTCPAlive 极低损耗的旁路探活：仅验证主码流地址对应的 TCP 端口是否存活
func checkCameraTCPAlive(rawURL string) bool {
	if rawURL == "" {
		return false
	}

	if constant.IsManagedByGo2rtcURL(rawURL) {
		return true
	}

	u, err := url.Parse(unwrapGo2rtcNetworkURL(rawURL))
	if err != nil {
		return false
	}

	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}
	if port == "" {
		// xiaomi:// / tapo:// / gb28181:// 等 go2rtc 原生应用层 scheme 没有可拨的标准
		// TCP 端口，无法用端口探活证明设备是否可达。这里若直接判 false，会让流在设备
		// 断电恢复后永久卡在 offline：探活失败 → 不录制 → go2rtc 无消费者 → 不去连设备
		// → producer 始终干瘪 → 探活仍失败……形成死锁，普通录制与动检录制都无法自动恢复。
		// 与上面的 exec/ffmpeg 前缀保持一致：对无法 TCP 探活的 scheme 视为可达，
		// 交给业务层用真实拉流去裁决（拉流失败时 go2rtc 会暴露 producer error，再走退避）。
		if u.Scheme != "" {
			return true
		}
		return false
	}

	host := u.Hostname()
	if host == "" {
		return false
	}

	// 1秒超时，不占用 CPU，只进行 TCP 握手
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

func unwrapGo2rtcNetworkURL(rawURL string) string {
	cleanURL := strings.TrimSpace(rawURL)
	schemeEnd := strings.Index(cleanURL, "://")
	if schemeEnd <= 0 {
		return cleanURL
	}

	schemeStart := schemeEnd - 1
	for schemeStart >= 0 && isURLSchemeChar(cleanURL[schemeStart]) {
		schemeStart--
	}
	return cleanURL[schemeStart+1:]
}

func isURLSchemeChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '+' || ch == '-' || ch == '.'
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "rtsp":
		return "554"
	case "rtsps":
		return "322"
	case "http":
		return "80"
	case "https":
		return "443"
	case "onvif":
		return "80"
	default:
		return ""
	}
}

func markAllStreamOffline(currentConfig *constant.Config) {
	constant.ConfigMux.RLock()
	defer constant.ConfigMux.RUnlock()
	for _, cam := range currentConfig.Cameras {
		service.UpdateOnlineStatus(cam.ID, "offline")
	}
}
