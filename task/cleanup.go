package task

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
)

// FileItem 定义用于排序的结构体
type FileItem struct {
	Path string
	Info fs.FileInfo
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
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				items = append(items, FileItem{Path: path, Info: info})
			}
		}
		return nil
	})

	if err != nil || len(items) == 0 {
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
		if cam.MinSizeKb > 0 && i < len(items)-1 {
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
			entries, _ := os.ReadDir(dateDirPath)
			if len(entries) == 0 {
				os.Remove(dateDirPath)
				log.Printf("[%s] 删除空的日期目录: %s", cam.ID, dateDirPath)
			}
		}
	}
}
