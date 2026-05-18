package app

import (
	"context"
	"log"
	"mime"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/task"
	"github.com/r0n9/camkeep/slog"
)

var (
	currentConfig constant.Config
	restartMux    sync.Mutex // 热重启专属防并发锁
	reloadCancel  context.CancelFunc
	taskWg        sync.WaitGroup
	ctxGlobal     context.Context
	version       string
)

func Run(appVersion string) {
	version = appVersion

	_ = mime.AddExtensionType(".ts", "video/mp2t")
	slog.Init()

	log.Printf("CamKeep version=%s", version)

	// 设置全局 Context，确保 go2rtc 子进程和后台任务能在退出时一起停止。
	var cancelGlobal context.CancelFunc
	ctxGlobal, cancelGlobal = context.WithCancel(context.Background())

	// 在 CamKeep 业务逻辑启动前，先拉起底座进程
	go2rtcDone := task.StartGo2rtcDaemon(ctxGlobal)
	// 动态轮询等待，确保 go2rtc API 彻底就绪（最大等待 10 秒）
	log.Println("等待底层流媒体引擎 go2rtc 启动...")
	if err := task.WaitForGo2rtcReady(10 * time.Second); err != nil {
		cancelGlobal()
		<-go2rtcDone
		// 如果 10 秒了还没起来，说明底座进程严重故障，直接让主程序退出
		log.Fatalf("致命错误: 无法连接到底层引擎: %v", err)
	}
	log.Println("go2rtc 底座已完美就绪！")

	// 1. 读取或初始化配置 (如果不存在则创建空配置)
	currentConfig = loadOrInitConfig()

	err := os.MkdirAll(constant.DefaultRecordBaseDir, 0755)
	if err != nil {
		log.Fatalf("致命错误: 创建录像存储文件夹失败: %v", err)
	}

	// 加载持久化的手动录像覆盖指令
	task.LoadOverrides()
	task.PruneOverridesForCameras(currentConfig.Cameras)

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

	cancelGlobal() // 通知所有层级的 Context 退出
	taskWg.Wait()  // 等待所有任务完成
	<-go2rtcDone   // 等待 go2rtc 子进程退出，避免留下孤儿进程
	log.Println("程序已安全退出。")
}
