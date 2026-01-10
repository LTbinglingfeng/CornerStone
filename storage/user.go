package storage

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserInfo 用户信息
type UserInfo struct {
	Username    string    `json:"username"`
	Description string    `json:"description"`
	Avatar      string    `json:"avatar,omitempty"` // 头像文件名
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserManager 用户信息管理器
type UserManager struct {
	baseDir  string
	userInfo *UserInfo
	mu       sync.RWMutex
}

// NewUserManager 创建用户管理器
func NewUserManager(baseDir string) *UserManager {
	um := &UserManager{
		baseDir:  baseDir,
		userInfo: &UserInfo{},
	}
	os.MkdirAll(baseDir, 0755)
	um.Load()
	return um
}

// getUserInfoPath 获取用户信息文件路径
func (um *UserManager) getUserInfoPath() string {
	return filepath.Join(um.baseDir, "user_info.json")
}

// Load 加载用户信息
func (um *UserManager) Load() error {
	um.mu.Lock()
	defer um.mu.Unlock()

	data, err := os.ReadFile(um.getUserInfoPath())
	if err != nil {
		if os.IsNotExist(err) {
			// 初始化默认用户信息
			um.userInfo = &UserInfo{
				Username:    "User",
				Description: "",
				UpdatedAt:   time.Now(),
			}
			return nil
		}
		return err
	}

	var info UserInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	um.userInfo = &info
	return nil
}

// save 保存用户信息
func (um *UserManager) save() error {
	data, err := json.MarshalIndent(um.userInfo, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(um.getUserInfoPath(), data, 0644)
}

// Get 获取用户信息
func (um *UserManager) Get() *UserInfo {
	um.mu.RLock()
	defer um.mu.RUnlock()

	// 返回副本
	info := *um.userInfo
	return &info
}

// Update 更新用户信息
func (um *UserManager) Update(username, description string) (*UserInfo, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	if username != "" {
		um.userInfo.Username = username
	}
	if description != "" {
		um.userInfo.Description = description
	}
	um.userInfo.UpdatedAt = time.Now()

	if err := um.save(); err != nil {
		return nil, err
	}

	info := *um.userInfo
	return &info, nil
}

// SaveAvatar 保存用户头像
func (um *UserManager) SaveAvatar(filename string, data io.Reader) (string, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	// 删除旧的头像文件
	if um.userInfo.Avatar != "" {
		oldAvatarPath := filepath.Join(um.baseDir, um.userInfo.Avatar)
		os.Remove(oldAvatarPath)
	}

	// 保存新头像
	avatarPath := filepath.Join(um.baseDir, filename)
	file, err := os.Create(avatarPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, data); err != nil {
		return "", err
	}

	// 更新用户信息
	um.userInfo.Avatar = filename
	um.userInfo.UpdatedAt = time.Now()

	if err := um.save(); err != nil {
		return "", err
	}

	return filename, nil
}

// GetAvatarPath 获取头像文件路径
func (um *UserManager) GetAvatarPath() (string, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	if um.userInfo.Avatar == "" {
		return "", os.ErrNotExist
	}

	avatarPath := filepath.Join(um.baseDir, um.userInfo.Avatar)
	if _, err := os.Stat(avatarPath); err != nil {
		return "", err
	}

	return avatarPath, nil
}

// DeleteAvatar 删除用户头像
func (um *UserManager) DeleteAvatar() error {
	um.mu.Lock()
	defer um.mu.Unlock()

	if um.userInfo.Avatar == "" {
		return nil
	}

	// 删除头像文件
	avatarPath := filepath.Join(um.baseDir, um.userInfo.Avatar)
	os.Remove(avatarPath)

	// 更新用户信息
	um.userInfo.Avatar = ""
	um.userInfo.UpdatedAt = time.Now()

	return um.save()
}

// GetBaseDir 获取基础目录
func (um *UserManager) GetBaseDir() string {
	return um.baseDir
}
