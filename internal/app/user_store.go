package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	userRoleAdmin  = "admin"
	userRoleViewer = "viewer"

	userSourceLocal = "local"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

type currentUser struct {
	ID             string   `json:"id"`
	Username       string   `json:"username"`
	DisplayName    string   `json:"display_name"`
	Role           string   `json:"role"`
	Source         string   `json:"source"`
	SessionVersion int64    `json:"session_version"`
	CameraIDs      []string `json:"camera_ids"`
}

type userView struct {
	ID              string            `json:"id"`
	Username        string            `json:"username"`
	DisplayName     string            `json:"display_name"`
	Role            string            `json:"role"`
	Enabled         bool              `json:"enabled"`
	Source          string            `json:"source"`
	SessionVersion  int64             `json:"session_version"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	LastLoginAt     *time.Time        `json:"last_login_at,omitempty"`
	LastLoginIP     string            `json:"last_login_ip,omitempty"`
	Online          bool              `json:"online"`
	IsCurrent       bool              `json:"is_current"`
	ActiveSessions  []userSessionView `json:"active_sessions,omitempty"`
	CameraIDs       []string          `json:"camera_ids"`
	CameraAccessAll bool              `json:"camera_access_all"`
	Editable        bool              `json:"editable"`
	CanDelete       bool              `json:"can_delete"`
	PasswordManaged bool              `json:"password_managed"`
}

type storedUser struct {
	ID             string     `json:"id"`
	Username       string     `json:"username"`
	DisplayName    string     `json:"display_name"`
	Role           string     `json:"role"`
	PasswordHash   string     `json:"password_hash"`
	Enabled        bool       `json:"enabled"`
	SessionVersion int64      `json:"session_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	LastLoginIP    string     `json:"last_login_ip,omitempty"`
	CameraIDs      []string   `json:"camera_ids"`
}

type usersFile struct {
	Users []storedUser `json:"users"`
}

type userStore struct {
	path    string
	mux     sync.RWMutex
	users   map[string]storedUser
	userIDs map[string]string
}

type createUserRequest struct {
	Username        string   `json:"username"`
	DisplayName     string   `json:"display_name"`
	Role            string   `json:"role"`
	Password        string   `json:"password"`
	Enabled         *bool    `json:"enabled"`
	CameraIDs       []string `json:"camera_ids"`
	CameraAccessAll *bool    `json:"camera_access_all"`
}

type updateUserRequest struct {
	DisplayName     *string  `json:"display_name"`
	Role            *string  `json:"role"`
	Enabled         *bool    `json:"enabled"`
	CameraIDs       []string `json:"camera_ids"`
	CameraAccessAll *bool    `json:"camera_access_all"`
}

type updatePasswordRequest struct {
	Password string `json:"password"`
}

func newUserStore(path string) (*userStore, error) {
	store := &userStore{
		path:    path,
		users:   make(map[string]storedUser),
		userIDs: make(map[string]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *userStore) load() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取用户文件失败: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	var file usersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("解析用户文件失败: %w", err)
	}

	users := make(map[string]storedUser, len(file.Users))
	userIDs := make(map[string]string, len(file.Users))
	for _, user := range file.Users {
		user.Username = strings.TrimSpace(user.Username)
		user.Role = normalizeUserRole(user.Role)
		if user.ID == "" || user.Username == "" {
			continue
		}
		users[user.ID] = user
		userIDs[strings.ToLower(user.Username)] = user.ID
	}
	s.users = users
	s.userIDs = userIDs
	return nil
}

func (s *userStore) hasUsers() bool {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return len(s.users) > 0
}

func (s *userStore) listUsers() []storedUser {
	s.mux.RLock()
	defer s.mux.RUnlock()

	users := make([]storedUser, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Username) < strings.ToLower(users[j].Username)
	})
	return users
}

func (s *userStore) userByID(id string) (storedUser, bool) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	user, ok := s.users[id]
	return user, ok
}

func (s *userStore) userByUsername(username string) (storedUser, bool) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	id, ok := s.userIDs[strings.ToLower(strings.TrimSpace(username))]
	if !ok {
		return storedUser{}, false
	}
	user, ok := s.users[id]
	return user, ok
}

func (s *userStore) authenticate(username, password string) (currentUser, bool) {
	user, ok := s.userByUsername(username)
	if !ok || !user.Enabled {
		return currentUser{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return currentUser{}, false
	}
	return currentUserFromStored(user), true
}

func (s *userStore) createUser(req createUserRequest, reservedUsername string) (storedUser, error) {
	username := strings.TrimSpace(req.Username)
	displayName := strings.TrimSpace(req.DisplayName)
	role := normalizeUserRole(req.Role)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := validateUsername(username); err != nil {
		return storedUser{}, err
	}
	if strings.EqualFold(username, reservedUsername) {
		return storedUser{}, fmt.Errorf("用户名已存在")
	}
	if role == "" {
		return storedUser{}, fmt.Errorf("无效用户角色")
	}
	if len(req.Password) < 8 {
		return storedUser{}, fmt.Errorf("密码至少需要 8 位")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return storedUser{}, fmt.Errorf("生成密码哈希失败: %w", err)
	}

	now := time.Now()
	user := storedUser{
		ID:             newUserID(),
		Username:       username,
		DisplayName:    displayName,
		Role:           role,
		PasswordHash:   string(hash),
		Enabled:        enabled,
		SessionVersion: 1,
		CreatedAt:      now,
		UpdatedAt:      now,
		CameraIDs:      normalizeStoredCameraIDsForRole(role, req.CameraIDs),
	}

	s.mux.Lock()
	defer s.mux.Unlock()
	if _, exists := s.userIDs[strings.ToLower(username)]; exists {
		return storedUser{}, fmt.Errorf("用户名已存在")
	}
	s.users[user.ID] = user
	s.userIDs[strings.ToLower(user.Username)] = user.ID
	if err := s.saveLocked(); err != nil {
		delete(s.users, user.ID)
		delete(s.userIDs, strings.ToLower(user.Username))
		return storedUser{}, err
	}
	return user, nil
}

func (s *userStore) ensureBootstrapAdmin(username, password string) (storedUser, bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if err := validateUsername(username); err != nil {
		return storedUser{}, false, err
	}
	if password == "" {
		return storedUser{}, false, fmt.Errorf("初始化管理员密码不能为空")
	}

	s.mux.RLock()
	if id, exists := s.userIDs[strings.ToLower(username)]; exists {
		user := s.users[id]
		s.mux.RUnlock()
		return user, false, nil
	}
	s.mux.RUnlock()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return storedUser{}, false, fmt.Errorf("生成密码哈希失败: %w", err)
	}

	now := time.Now()
	user := storedUser{
		ID:             newUserID(),
		Username:       username,
		DisplayName:    username,
		Role:           userRoleAdmin,
		PasswordHash:   string(hash),
		Enabled:        true,
		SessionVersion: 1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	s.mux.Lock()
	defer s.mux.Unlock()
	if id, exists := s.userIDs[strings.ToLower(username)]; exists {
		return s.users[id], false, nil
	}
	s.users[user.ID] = user
	s.userIDs[strings.ToLower(user.Username)] = user.ID
	if err := s.saveLocked(); err != nil {
		delete(s.users, user.ID)
		delete(s.userIDs, strings.ToLower(user.Username))
		return storedUser{}, false, err
	}
	return user, true, nil
}

func (s *userStore) updateUser(id string, req updateUserRequest) (storedUser, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	user, ok := s.users[id]
	if !ok {
		return storedUser{}, os.ErrNotExist
	}
	next := user
	if req.DisplayName != nil {
		next.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Role != nil {
		role := normalizeUserRole(*req.Role)
		if role == "" {
			return storedUser{}, fmt.Errorf("无效用户角色")
		}
		next.Role = role
	}
	if req.Enabled != nil {
		next.Enabled = *req.Enabled
	}
	if next.Role == userRoleAdmin {
		next.CameraIDs = nil
	} else if req.CameraAccessAll != nil && *req.CameraAccessAll {
		next.CameraIDs = nil
	} else if req.CameraAccessAll != nil || req.CameraIDs != nil {
		next.CameraIDs = normalizeStoredCameraIDs(req.CameraIDs)
	}
	if next.Role != user.Role || next.Enabled != user.Enabled {
		next.SessionVersion++
	}
	next.UpdatedAt = time.Now()
	s.users[id] = next
	if err := s.saveLocked(); err != nil {
		s.users[id] = user
		return storedUser{}, err
	}
	return next, nil
}

func (s *userStore) updatePassword(id, password string) error {
	if len(password) < 8 {
		return fmt.Errorf("密码至少需要 8 位")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	user, ok := s.users[id]
	if !ok {
		return os.ErrNotExist
	}
	prev := user
	user.PasswordHash = string(hash)
	user.SessionVersion++
	user.UpdatedAt = time.Now()
	s.users[id] = user
	if err := s.saveLocked(); err != nil {
		s.users[id] = prev
		return err
	}
	return nil
}

func (s *userStore) recordLogin(id, ip string, now time.Time) (storedUser, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	user, ok := s.users[id]
	if !ok {
		return storedUser{}, os.ErrNotExist
	}
	prev := user
	loginAt := now
	user.LastLoginAt = &loginAt
	user.LastLoginIP = strings.TrimSpace(ip)
	s.users[id] = user
	if err := s.saveLocked(); err != nil {
		s.users[id] = prev
		return storedUser{}, err
	}
	return user, nil
}

func (s *userStore) deleteUser(id string) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	user, ok := s.users[id]
	if !ok {
		return os.ErrNotExist
	}
	delete(s.users, id)
	delete(s.userIDs, strings.ToLower(user.Username))
	if err := s.saveLocked(); err != nil {
		s.users[id] = user
		s.userIDs[strings.ToLower(user.Username)] = id
		return err
	}
	return nil
}

func (s *userStore) enabledAdminCountLocked() int {
	count := 0
	for _, user := range s.users {
		if user.Enabled && user.Role == userRoleAdmin {
			count++
		}
	}
	return count
}

func (s *userStore) enabledAdminCount() int {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return s.enabledAdminCountLocked()
}

func (s *userStore) saveLocked() error {
	users := make([]storedUser, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Username) < strings.ToLower(users[j].Username)
	})

	data, err := json.MarshalIndent(usersFile{Users: users}, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化用户文件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("创建用户目录失败: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".users-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时用户文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入用户文件失败: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("设置用户文件权限失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭用户文件失败: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("替换用户文件失败: %w", err)
	}
	return nil
}

func currentUserFromStored(user storedUser) currentUser {
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = user.Username
	}
	return currentUser{
		ID:             user.ID,
		Username:       user.Username,
		DisplayName:    displayName,
		Role:           user.Role,
		Source:         userSourceLocal,
		SessionVersion: user.SessionVersion,
		CameraIDs:      cloneStringSlice(user.CameraIDs),
	}
}

func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("用户名需为 3-32 位字母、数字、点、下划线或短横线")
	}
	return nil
}

func normalizeUserRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", userRoleViewer:
		return userRoleViewer
	case userRoleAdmin:
		return userRoleAdmin
	default:
		return ""
	}
}

func newUserID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("u_%d", time.Now().UnixNano())
	}
	return "u_" + base64.RawURLEncoding.EncodeToString(buf)
}

func userViewFromStored(user storedUser) userView {
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = user.Username
	}
	return userView{
		ID:              user.ID,
		Username:        user.Username,
		DisplayName:     displayName,
		Role:            user.Role,
		Enabled:         user.Enabled,
		Source:          userSourceLocal,
		SessionVersion:  user.SessionVersion,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
		LastLoginAt:     user.LastLoginAt,
		LastLoginIP:     user.LastLoginIP,
		CameraIDs:       cloneStringSlice(user.CameraIDs),
		CameraAccessAll: storedUserCanAccessAllCameras(user),
		Editable:        true,
		CanDelete:       true,
		PasswordManaged: true,
	}
}

func normalizeStoredCameraIDsForRole(role string, cameraIDs []string) []string {
	if role == userRoleAdmin {
		return nil
	}
	return normalizeStoredCameraIDs(cameraIDs)
}

func normalizeStoredCameraIDs(cameraIDs []string) []string {
	if cameraIDs == nil {
		return nil
	}
	seen := make(map[string]bool, len(cameraIDs))
	result := make([]string, 0, len(cameraIDs))
	for _, id := range cameraIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func storedUserCanAccessAllCameras(user storedUser) bool {
	return user.Role == userRoleAdmin || user.CameraIDs == nil
}

func isNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
