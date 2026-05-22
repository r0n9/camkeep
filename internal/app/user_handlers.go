package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type meResponse struct {
	AuthEnabled bool        `json:"auth_enabled"`
	User        currentUser `json:"user"`
	CanAdmin    bool        `json:"can_admin"`
	Permissions []string    `json:"permissions"`
}

type usersResponse struct {
	Users         []userView     `json:"users"`
	CameraOptions []cameraOption `json:"camera_options"`
}

func handleMe(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := getCurrentUser(c)
		if !ok {
			user = disabledAdminUser()
		}
		c.JSON(http.StatusOK, meResponse{
			AuthEnabled: auth.isEnabled(),
			User:        user,
			CanAdmin:    user.Role == userRoleAdmin,
			Permissions: permissionsForUser(user),
		})
	}
}

func handleListUsers(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, usersResponse{
			Users:         auth.userViews(c),
			CameraOptions: currentCameraOptions(),
		})
	}
}

func handleCreateUser(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth.UserStore == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "用户存储未初始化"})
			return
		}

		var req createUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求内容不合法"})
			return
		}

		bootstrap := !auth.UserStore.hasUsers()
		if bootstrap {
			enabled := true
			req.Username = "admin"
			if strings.TrimSpace(req.DisplayName) == "" {
				req.DisplayName = "admin"
			}
			req.Role = userRoleAdmin
			req.Enabled = &enabled
			req.CameraAccessAll = &enabled
			req.CameraIDs = nil
		}
		req.CameraIDs = normalizeCameraScopeFromRequest(req.Role, req.CameraIDs, req.CameraAccessAll)
		user, err := auth.UserStore.createUser(req, "")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if bootstrap {
			current := currentUserFromStored(user)
			if token, err := auth.newSessionTokenForUser(current, time.Now()); err == nil {
				setCurrentUser(c, current)
				now := time.Now()
				ip := clientIP(c)
				if _, err := auth.UserStore.recordLogin(current.ID, ip, now); err == nil {
					user, _ = auth.UserStore.userByID(current.ID)
				}
				if auth.Sessions != nil {
					auth.Sessions.trackLogin(token, current, ip, now, now.Add(auth.SessionTTL))
				}
				auth.setSessionCookie(c, token)
			}
		}
		c.JSON(http.StatusCreated, auth.enrichUserView(c, userViewFromStored(user)))
	}
}

func handleUpdateUser(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if !isLocalUserID(id) || auth.UserStore == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该用户不可编辑"})
			return
		}

		var req updateUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求内容不合法"})
			return
		}

		existing, ok := auth.UserStore.userByID(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		if err := auth.validateUserUpdate(c, existing, req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		nextRole := existing.Role
		if req.Role != nil {
			nextRole = normalizeUserRole(*req.Role)
		}
		req.CameraIDs = normalizeCameraScopeFromRequest(nextRole, req.CameraIDs, req.CameraAccessAll)

		user, err := auth.UserStore.updateUser(id, req)
		if err != nil {
			status := http.StatusInternalServerError
			if isNotFound(err) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		if auth.Sessions != nil && user.SessionVersion != existing.SessionVersion {
			auth.Sessions.removeByUserID(id)
		}
		c.JSON(http.StatusOK, auth.enrichUserView(c, userViewFromStored(user)))
	}
}

func handleResetUserPassword(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if !isLocalUserID(id) || auth.UserStore == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该用户密码不可在此修改"})
			return
		}

		var req updatePasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求内容不合法"})
			return
		}
		if err := auth.UserStore.updatePassword(id, req.Password); err != nil {
			status := http.StatusBadRequest
			if isNotFound(err) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		if auth.Sessions != nil {
			auth.Sessions.removeByUserID(id)
		}
		c.JSON(http.StatusOK, gin.H{"msg": "密码已更新"})
	}
}

func handleDeleteUser(auth authConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if !isLocalUserID(id) || auth.UserStore == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该用户不可删除"})
			return
		}

		user, ok := auth.UserStore.userByID(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		if current, ok := getCurrentUser(c); ok && current.Source == userSourceLocal && current.ID == id {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除当前登录用户"})
			return
		}
		if user.Enabled && user.Role == userRoleAdmin && auth.UserStore.enabledAdminCount() <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "至少需要保留一个启用的管理员"})
			return
		}

		if err := auth.UserStore.deleteUser(id); err != nil {
			status := http.StatusInternalServerError
			if isNotFound(err) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		if auth.Sessions != nil {
			auth.Sessions.removeByUserID(id)
		}
		c.JSON(http.StatusOK, gin.H{"msg": "用户已删除"})
	}
}

func (auth authConfig) userViews(c *gin.Context) []userView {
	current, _ := getCurrentUser(c)
	currentToken, _ := c.Cookie(authCookieName)
	activeByUser := map[string][]userSessionView{}
	if auth.Sessions != nil {
		activeByUser = auth.Sessions.activeSessionsByUser(time.Now(), currentToken)
	}
	localUsers := []storedUser(nil)
	if auth.UserStore != nil {
		localUsers = auth.UserStore.listUsers()
	}
	views := make([]userView, 0, 1+len(localUsers))
	for _, user := range localUsers {
		view := userViewFromStored(user)
		if current.Source == userSourceLocal && current.ID == user.ID {
			view.CanDelete = false
		}
		view.IsCurrent = current.Source == userSourceLocal && current.ID == user.ID
		view.ActiveSessions = activeByUser[user.ID]
		view.Online = len(view.ActiveSessions) > 0
		views = append(views, view)
	}
	return views
}

func (auth authConfig) enrichUserView(c *gin.Context, view userView) userView {
	current, _ := getCurrentUser(c)
	view.IsCurrent = current.Source == userSourceLocal && current.ID == view.ID
	currentToken, _ := c.Cookie(authCookieName)
	if auth.Sessions != nil {
		view.ActiveSessions = auth.Sessions.activeSessionsByUser(time.Now(), currentToken)[view.ID]
		view.Online = len(view.ActiveSessions) > 0
	}
	if view.IsCurrent {
		view.CanDelete = false
	}
	return view
}

func (auth authConfig) validateUserUpdate(c *gin.Context, existing storedUser, req updateUserRequest) error {
	nextRole := existing.Role
	nextEnabled := existing.Enabled
	if req.Role != nil {
		nextRole = normalizeUserRole(*req.Role)
	}
	if req.Enabled != nil {
		nextEnabled = *req.Enabled
	}

	if current, ok := getCurrentUser(c); ok && current.Source == userSourceLocal && current.ID == existing.ID {
		if existing.Enabled && !nextEnabled {
			return errUserChange("不能停用当前登录用户")
		}
		if existing.Role == userRoleAdmin && nextRole != userRoleAdmin {
			return errUserChange("不能降低当前登录用户的管理员权限")
		}
	}

	removesAdmin := existing.Enabled && existing.Role == userRoleAdmin && (!nextEnabled || nextRole != userRoleAdmin)
	if removesAdmin && auth.UserStore.enabledAdminCount() <= 1 {
		return errUserChange("至少需要保留一个启用的管理员")
	}
	return nil
}

func isLocalUserID(id string) bool {
	return strings.HasPrefix(id, "u_")
}

func permissionsForUser(user currentUser) []string {
	if user.Role == userRoleAdmin {
		return []string{"view", "admin"}
	}
	return []string{"view"}
}

type errUserChange string

func (e errUserChange) Error() string {
	return string(e)
}
