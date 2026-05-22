package app

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
)

type cameraOption struct {
	ID string `json:"id"`
}

func currentCameraOptions() []cameraOption {
	constant.ConfigMux.RLock()
	cameras := append([]constant.Camera(nil), currentConfig.Cameras...)
	constant.ConfigMux.RUnlock()

	options := make([]cameraOption, 0, len(cameras))
	for _, cam := range cameras {
		id := strings.TrimSpace(cam.ID)
		if id == "" {
			continue
		}
		options = append(options, cameraOption{ID: id})
	}
	return options
}

func normalizeCameraScopeFromRequest(role string, cameraIDs []string, accessAll *bool) []string {
	if normalizeUserRole(role) == userRoleAdmin {
		return nil
	}
	if accessAll != nil && *accessAll {
		return nil
	}
	if accessAll == nil && cameraIDs == nil {
		return nil
	}

	valid := currentCameraIDOrder()
	seen := make(map[string]bool, len(cameraIDs))
	filtered := make([]string, 0, len(cameraIDs))
	for _, id := range cameraIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := valid[id]; !ok {
			continue
		}
		seen[id] = true
		filtered = append(filtered, id)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return valid[filtered[i]] < valid[filtered[j]]
	})
	return filtered
}

func currentCameraIDOrder() map[string]int {
	constant.ConfigMux.RLock()
	defer constant.ConfigMux.RUnlock()

	order := make(map[string]int, len(currentConfig.Cameras))
	for index, cam := range currentConfig.Cameras {
		id := strings.TrimSpace(cam.ID)
		if id == "" {
			continue
		}
		if _, exists := order[id]; !exists {
			order[id] = index
		}
	}
	return order
}

func userCanAccessAllCameras(user currentUser) bool {
	return user.Role == userRoleAdmin || user.CameraIDs == nil
}

func userCanAccessCamera(user currentUser, camID string) bool {
	if userCanAccessAllCameras(user) {
		return true
	}
	for _, allowedID := range user.CameraIDs {
		if allowedID == camID {
			return true
		}
	}
	return false
}

func requestCanAccessCamera(c *gin.Context, camID string) bool {
	user, ok := getCurrentUser(c)
	if !ok {
		return true
	}
	return userCanAccessCamera(user, camID)
}

func requireCameraAccess(c *gin.Context, camID string) bool {
	if _, ok := getCurrentUser(c); !ok {
		return true
	}
	if !cameraExists(camID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该摄像头"})
		return false
	}
	if !requestCanAccessCamera(c, camID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该摄像头"})
		return false
	}
	return true
}

func requireQueryCameraAccess(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		camID := strings.TrimSpace(c.Query(param))
		if camID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "缺少摄像头参数"})
			return
		}
		if !cameraExists(camID) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "找不到该摄像头"})
			return
		}
		if !requestCanAccessCamera(c, camID) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权访问该摄像头"})
			return
		}
		c.Next()
	}
}

func filterStatusSnapshotForUser(c *gin.Context, snapshot map[string]statusResponseEntry) map[string]statusResponseEntry {
	user, ok := getCurrentUser(c)
	if !ok || userCanAccessAllCameras(user) {
		return snapshot
	}
	for id := range snapshot {
		if !userCanAccessCamera(user, id) {
			delete(snapshot, id)
		}
	}
	return snapshot
}

func cameraIDFromRecordPath(targetPath string) (string, bool) {
	targetPath = strings.ReplaceAll(strings.TrimSpace(targetPath), "\\", "/")
	targetPath = strings.TrimLeft(targetPath, "/")
	if targetPath == "" {
		return "", false
	}
	cleanTarget := filepath.ToSlash(filepath.Clean(filepath.FromSlash(targetPath)))
	if cleanTarget == "." || strings.HasPrefix(cleanTarget, "../") || cleanTarget == ".." {
		return "", false
	}

	constant.ConfigMux.RLock()
	cameras := append([]constant.Camera(nil), currentConfig.Cameras...)
	constant.ConfigMux.RUnlock()

	sort.SliceStable(cameras, func(i, j int) bool {
		return len(cameras[i].ID) > len(cameras[j].ID)
	})
	for _, cam := range cameras {
		id := strings.Trim(strings.TrimSpace(cam.ID), "/")
		if id == "" {
			continue
		}
		if cleanTarget == id || strings.HasPrefix(cleanTarget, id+"/") {
			return cam.ID, true
		}
	}
	return "", false
}
