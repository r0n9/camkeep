package task

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
)

// FileItem 定义用于排序的结构体
type FileItem struct {
	Path string
	Info fs.FileInfo
}

// isRecordVideoFile 判断是否为录像视频文件。camDir 下还混有 cover.jpg 等非录像文件，
// 清理规则只应作用在录像上。
func isRecordVideoFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".mp4":
		return true
	default:
		return false
	}
}

// isTimelapseMode 判断这路摄像头是否为延时摄影模式。
func isTimelapseMode(cam constant.Camera) bool {
	return strings.EqualFold(strings.TrimSpace(cam.Mode), "timelapse")
}

// CleanupTask 定期清理过期和过小的文件
func CleanupTask(ctx context.Context, wg *sync.WaitGroup, cameras []constant.Camera) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Hour) // 每小时执行一次清理
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Println("开始执行全局文件清理任务...")
			for _, cam := range cameras {
				cleanCameraFiles(cam)
			}
		}
	}
}

func cleanCameraFiles(cam constant.Camera) {
	camDir := filepath.Join(constant.DefaultRecordBaseDir, cam.ID)

	var items []FileItem

	// 使用 WalkDir 递归扫描所有日期子目录中的文件
	err := filepath.WalkDir(camDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == motionMarkerDirName {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				items = append(items, FileItem{Path: path, Info: info})
			}
		}
		return nil
	})

	if err != nil {
		return
	}

	// 按全局修改时间排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].Info.ModTime().Before(items[j].Info.ModTime())
	})

	now := time.Now()
	for i, item := range items {
		// 1. 校验保留天数
		if cam.RetentionDays > 0 {
			if now.Sub(item.Info.ModTime()).Hours() > float64(cam.RetentionDays*24) {
				os.Remove(item.Path)
				log.Printf("[%s] 删除过期文件: %s", cam.ID, item.Path)
				continue
			}
		}

		// 2. 校验文件大小 (必须排除全局最后一个正在写入的文件)
		//
		// 只对录像视频文件生效：camDir 下还放着 cover.jpg 等非录像文件，封面通常只有几十 KB，
		// 会被默认 1MB 的阈值当成碎片删掉。
		//
		// 延时录像同样跳过：min_size_kb 的默认值是按普通录像的流拷贝片段定的，而延时片是重
		// 编码的小文件，体积随 capture_interval 和 segment_duration 大幅变化，用固定 KB 阈值
		// 判断会误删正常成片。延时片仍然受保留天数约束。
		if cam.MinSizeKb > 0 && i < len(items)-1 && isRecordVideoFile(item.Path) && !isTimelapseMode(cam) {
			if item.Info.Size() < cam.MinSizeKb*1024 {
				os.Remove(item.Path)
				log.Printf("[%s] 删除过小碎片文件: %s", cam.ID, item.Path)
			}
		}
	}

	// 扫尾工作，如果日期目录里的文件都被清理空了，就把空目录也删掉
	dirs, _ := os.ReadDir(camDir)
	for _, d := range dirs {
		if d.IsDir() {
			dateDirPath := filepath.Join(camDir, d.Name())
			if d.Name() == motionMarkerDirName {
				cleanMotionMarkerFiles(cam, dateDirPath)
				continue
			}
			entries, _ := os.ReadDir(dateDirPath)
			if len(entries) == 0 {
				os.Remove(dateDirPath)
				log.Printf("[%s] 删除空的日期目录: %s", cam.ID, dateDirPath)
			}
		}
	}
}

func cleanMotionMarkerFiles(cam constant.Camera, markerDir string) {
	if cam.RetentionDays <= 0 {
		return
	}
	entries, err := os.ReadDir(markerDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -cam.RetentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), time.Local)
		if err != nil {
			continue
		}
		if day.Before(localDayStart(cutoff)) {
			path := filepath.Join(markerDir, entry.Name())
			if err := os.Remove(path); err == nil {
				log.Printf("[%s] 删除过期动检标记文件: %s", cam.ID, path)
			}
		}
	}
}
