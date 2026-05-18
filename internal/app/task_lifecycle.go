package app

import (
	"context"
	"log"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/service"
	"github.com/r0n9/camkeep/internal/task"
)

// startTasks 启动或重启所有的摄像头监控任务
func startTasks() {
	var ctx context.Context
	ctx, reloadCancel = context.WithCancel(ctxGlobal)

	constant.ConfigMux.RLock()
	cfg := currentConfig
	cams := cfg.Cameras
	constant.ConfigMux.RUnlock()

	taskWg.Add(1)
	go task.CleanupTask(ctx, &taskWg, cams)

	taskWg.Add(1)
	go task.DailyMergeTask(ctx, &taskWg, cfg)

	for _, cam := range cams {
		taskWg.Add(1)
		go task.CameraTask(ctx, &taskWg, cam)

		if cam.MotionDetect && (cam.Mode == "" || cam.Mode == "normal") {
			taskWg.Add(1)
			go task.MotionDetectTask(ctx, &taskWg, cam)
		}
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
	syncOnvifCandidates(ctxGlobal, newConfig)

	// 4. 【新增】清理内存中被删除的“幽灵”摄像头状态
	cleanGhostStatus(newConfig)
	task.PruneOverridesForCameras(newConfig.Cameras)

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
