package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
)

type StorageHealth struct {
	Path      string    `json:"path"`
	Writable  bool      `json:"writable"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

var (
	storageHealthCache StorageHealth
	storageHealthMux   sync.RWMutex
	storageHealthTTL   = 15 * time.Second
)

// CheckStorageHealth 检查录像存储目录的写权限（带缓存避免频繁磁盘 I/O）
func CheckStorageHealth() StorageHealth {
	storageHealthMux.RLock()
	if time.Since(storageHealthCache.CheckedAt) < storageHealthTTL && !storageHealthCache.CheckedAt.IsZero() {
		cached := storageHealthCache
		storageHealthMux.RUnlock()
		return cached
	}
	storageHealthMux.RUnlock()

	health := ProbeStorageWritable(constant.DefaultRecordBaseDir)

	storageHealthMux.Lock()
	storageHealthCache = health
	storageHealthMux.Unlock()

	return health
}

// ForceCheckStorageHealth 强制立即刷新检查
func ForceCheckStorageHealth() StorageHealth {
	health := ProbeStorageWritable(constant.DefaultRecordBaseDir)

	storageHealthMux.Lock()
	storageHealthCache = health
	storageHealthMux.Unlock()

	return health
}

// ResetStorageHealthCache 重置缓存（用于测试）
func ResetStorageHealthCache() {
	storageHealthMux.Lock()
	storageHealthCache = StorageHealth{}
	storageHealthMux.Unlock()
}

// ProbeStorageWritable 真实探测指定目录是否可写
func ProbeStorageWritable(baseDir string) StorageHealth {
	absPath, err := filepath.Abs(baseDir)
	if err != nil {
		absPath = baseDir
	}

	// 1. 尝试创建目录（如果不存在）
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return StorageHealth{
			Path:      absPath,
			Writable:  false,
			Error:     fmt.Sprintf("创建录像存储目录失败: %v", err),
			CheckedAt: time.Now(),
		}
	}

	// 2. 真实尝试写入并删除临时测试探针文件
	testFile := filepath.Join(absPath, fmt.Sprintf(".perm_test_%d", time.Now().UnixNano()))
	f, err := os.OpenFile(testFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return StorageHealth{
			Path:      absPath,
			Writable:  false,
			Error:     fmt.Sprintf("录像存储目录无写入权限 (Permission Denied): %v", err),
			CheckedAt: time.Now(),
		}
	}
	_ = f.Close()
	_ = os.Remove(testFile)

	return StorageHealth{
		Path:      absPath,
		Writable:  true,
		CheckedAt: time.Now(),
	}
}
