package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MomentStatus 朋友圈状态
type MomentStatus string

const (
	MomentStatusPending    MomentStatus = "pending"    // 等待生图
	MomentStatusGenerating MomentStatus = "generating" // 生图中
	MomentStatusPublished  MomentStatus = "published"  // 已发布
	MomentStatusFailed     MomentStatus = "failed"     // 生图失败
)

// Moment 朋友圈动态
type Moment struct {
	ID          string       `json:"id"`
	PromptID    string       `json:"prompt_id"`           // 发布者（角色）ID
	PromptName  string       `json:"prompt_name"`         // 发布者名称
	Content     string       `json:"content"`             // 文案内容
	ImagePrompt string       `json:"image_prompt"`        // 生图提示词
	ImagePath   string       `json:"image_path"`          // 生成的图片路径（相对路径）
	Status      MomentStatus `json:"status"`              // 状态
	ErrorMsg    string       `json:"error_msg,omitempty"` // 错误信息
	CreatedAt   time.Time    `json:"created_at"`          // 创建时间
	UpdatedAt   time.Time    `json:"updated_at"`          // 更新时间
	Likes       []Like       `json:"likes"`               // 点赞列表
	Comments    []Comment    `json:"comments"`            // 评论列表
}

// Like 点赞
type Like struct {
	ID        string    `json:"id"`
	UserType  string    `json:"user_type"` // "user" 或 "prompt"
	UserID    string    `json:"user_id"`   // 用户ID或角色ID
	UserName  string    `json:"user_name"` // 显示名称
	CreatedAt time.Time `json:"created_at"`
}

// Comment 评论
type Comment struct {
	ID        string    `json:"id"`
	UserType  string    `json:"user_type"`          // "user" 或 "prompt"
	UserID    string    `json:"user_id"`            // 用户ID或角色ID
	UserName  string    `json:"user_name"`          // 显示名称
	Content   string    `json:"content"`            // 评论内容
	ReplyTo   string    `json:"reply_to,omitempty"` // 回复的评论ID
	CreatedAt time.Time `json:"created_at"`
}

// MomentsConfig 朋友圈配置
type MomentsConfig struct {
	BackgroundImage string `json:"background_image,omitempty"` // 背景图路径（相对路径）
}

// MomentManager 朋友圈管理器
type MomentManager struct {
	baseDir string

	moments map[string]Moment
	config  MomentsConfig

	mu sync.RWMutex
}

// NewMomentManager 创建管理器
func NewMomentManager(baseDir string) *MomentManager {
	mm := &MomentManager{
		baseDir: baseDir,
		moments: make(map[string]Moment),
	}
	_ = os.MkdirAll(baseDir, 0755)
	_ = os.MkdirAll(filepath.Join(baseDir, "images"), 0755)
	_ = os.MkdirAll(filepath.Join(baseDir, "backgrounds"), 0755)
	_ = mm.Load()
	return mm
}

func (mm *MomentManager) momentsFilePath() string {
	return filepath.Join(mm.baseDir, "moments.json")
}

func (mm *MomentManager) configFilePath() string {
	return filepath.Join(mm.baseDir, "config.json")
}

// Load 加载所有朋友圈与配置
func (mm *MomentManager) Load() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.moments = make(map[string]Moment)

	if errLoadMoments := mm.loadMomentsLocked(); errLoadMoments != nil {
		return errLoadMoments
	}
	if errLoadConfig := mm.loadConfigLocked(); errLoadConfig != nil {
		return errLoadConfig
	}
	return nil
}

func (mm *MomentManager) loadMomentsLocked() error {
	data, errRead := os.ReadFile(mm.momentsFilePath())
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil
		}
		return errRead
	}

	var moments []Moment
	if errUnmarshal := json.Unmarshal(data, &moments); errUnmarshal != nil {
		return errUnmarshal
	}

	for _, moment := range moments {
		if moment.ID == "" {
			continue
		}
		mm.moments[moment.ID] = normalizeMoment(moment)
	}
	return nil
}

func (mm *MomentManager) loadConfigLocked() error {
	data, errRead := os.ReadFile(mm.configFilePath())
	if errRead != nil {
		if os.IsNotExist(errRead) {
			mm.config = MomentsConfig{}
			return nil
		}
		return errRead
	}

	var cfg MomentsConfig
	if errUnmarshal := json.Unmarshal(data, &cfg); errUnmarshal != nil {
		return errUnmarshal
	}
	mm.config = cfg
	return nil
}

func normalizeMoment(moment Moment) Moment {
	normalized := moment
	if normalized.Likes == nil {
		normalized.Likes = []Like{}
	}
	if normalized.Comments == nil {
		normalized.Comments = []Comment{}
	}
	return normalized
}

func cloneMoment(moment Moment) Moment {
	cloned := moment
	if moment.Likes != nil {
		cloned.Likes = append([]Like(nil), moment.Likes...)
	} else {
		cloned.Likes = []Like{}
	}
	if moment.Comments != nil {
		cloned.Comments = append([]Comment(nil), moment.Comments...)
	} else {
		cloned.Comments = []Comment{}
	}
	return cloned
}

func (mm *MomentManager) saveMomentsLocked() error {
	moments := make([]Moment, 0, len(mm.moments))
	for _, moment := range mm.moments {
		moments = append(moments, moment)
	}

	data, errMarshal := json.MarshalIndent(moments, "", "  ")
	if errMarshal != nil {
		return errMarshal
	}

	return os.WriteFile(mm.momentsFilePath(), data, 0644)
}

func (mm *MomentManager) saveConfigLocked() error {
	data, errMarshal := json.MarshalIndent(mm.config, "", "  ")
	if errMarshal != nil {
		return errMarshal
	}
	return os.WriteFile(mm.configFilePath(), data, 0644)
}

// Create 创建新朋友圈
func (mm *MomentManager) Create(moment Moment) (Moment, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if errValidateID := ValidateID(moment.ID); errValidateID != nil {
		return Moment{}, errValidateID
	}

	if _, ok := mm.moments[moment.ID]; ok {
		return Moment{}, errors.New("moment already exists")
	}

	now := time.Now()
	if moment.CreatedAt.IsZero() {
		moment.CreatedAt = now
	}
	if moment.UpdatedAt.IsZero() {
		moment.UpdatedAt = now
	}
	moment = normalizeMoment(moment)

	mm.moments[moment.ID] = cloneMoment(moment)
	if errSave := mm.saveMomentsLocked(); errSave != nil {
		delete(mm.moments, moment.ID)
		return Moment{}, errSave
	}
	return cloneMoment(moment), nil
}

// Get 获取单个朋友圈
func (mm *MomentManager) Get(id string) (Moment, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	moment, ok := mm.moments[id]
	if !ok {
		return Moment{}, false
	}
	return cloneMoment(moment), true
}

// List 获取朋友圈列表（按创建时间倒序），支持分页
func (mm *MomentManager) List(limit, offset int) []Moment {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	moments := make([]Moment, 0, len(mm.moments))
	for _, moment := range mm.moments {
		moments = append(moments, cloneMoment(moment))
	}

	sort.Slice(moments, func(i, j int) bool {
		if moments[i].CreatedAt.Equal(moments[j].CreatedAt) {
			return moments[i].ID > moments[j].ID
		}
		return moments[i].CreatedAt.After(moments[j].CreatedAt)
	})

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if offset >= len(moments) {
		return []Moment{}
	}

	end := offset + limit
	if end > len(moments) {
		end = len(moments)
	}
	return moments[offset:end]
}

// Delete 删除朋友圈
func (mm *MomentManager) Delete(id string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if errValidateID := ValidateID(id); errValidateID != nil {
		return errValidateID
	}

	moment, ok := mm.moments[id]
	if !ok {
		return os.ErrNotExist
	}

	delete(mm.moments, id)
	if errSave := mm.saveMomentsLocked(); errSave != nil {
		mm.moments[id] = moment
		return errSave
	}

	// 清理图片文件（失败不影响主流程）
	if moment.ImagePath != "" {
		filename := filepath.Base(moment.ImagePath)
		if filename != "" && filename != "." && filename != ".." {
			_ = os.Remove(mm.GetImagePath(filename))
		}
	}
	return nil
}

// UpdateByID 原子更新单条朋友圈
func (mm *MomentManager) UpdateByID(id string, updater func(moment *Moment) error) (Moment, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if errValidateID := ValidateID(id); errValidateID != nil {
		return Moment{}, errValidateID
	}

	current, ok := mm.moments[id]
	if !ok {
		return Moment{}, os.ErrNotExist
	}

	next := cloneMoment(current)
	if errUpdate := updater(&next); errUpdate != nil {
		return Moment{}, errUpdate
	}
	next = normalizeMoment(next)
	next.UpdatedAt = time.Now()

	mm.moments[id] = cloneMoment(next)
	if errSave := mm.saveMomentsLocked(); errSave != nil {
		mm.moments[id] = current
		return Moment{}, errSave
	}
	return cloneMoment(next), nil
}

// AddLike 添加点赞（幂等）
func (mm *MomentManager) AddLike(momentID string, like Like) (Moment, error) {
	return mm.UpdateByID(momentID, func(moment *Moment) error {
		for _, existing := range moment.Likes {
			if existing.UserType == like.UserType && existing.UserID == like.UserID {
				return nil
			}
		}
		moment.Likes = append(moment.Likes, like)
		return nil
	})
}

// RemoveLike 取消点赞（幂等）
func (mm *MomentManager) RemoveLike(momentID, userType, userID string) (Moment, error) {
	return mm.UpdateByID(momentID, func(moment *Moment) error {
		next := moment.Likes[:0]
		for _, existing := range moment.Likes {
			if existing.UserType == userType && existing.UserID == userID {
				continue
			}
			next = append(next, existing)
		}
		moment.Likes = next
		return nil
	})
}

// AddComment 添加评论
func (mm *MomentManager) AddComment(momentID string, comment Comment) (Moment, error) {
	return mm.UpdateByID(momentID, func(moment *Moment) error {
		moment.Comments = append(moment.Comments, comment)
		return nil
	})
}

// RemoveComment 删除评论
func (mm *MomentManager) RemoveComment(momentID, commentID string) (Moment, error) {
	return mm.UpdateByID(momentID, func(moment *Moment) error {
		next := moment.Comments[:0]
		for _, existing := range moment.Comments {
			if existing.ID == commentID {
				continue
			}
			next = append(next, existing)
		}
		moment.Comments = next
		return nil
	})
}

// GetConfig 获取朋友圈配置
func (mm *MomentManager) GetConfig() MomentsConfig {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.config
}

// UpdateConfig 更新朋友圈配置
func (mm *MomentManager) UpdateConfig(cfg MomentsConfig) (MomentsConfig, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.config = cfg
	if errSave := mm.saveConfigLocked(); errSave != nil {
		return MomentsConfig{}, errSave
	}
	return mm.config, nil
}

// SetBackgroundImage 设置背景图路径
func (mm *MomentManager) SetBackgroundImage(path string) (MomentsConfig, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.config.BackgroundImage = path
	if errSave := mm.saveConfigLocked(); errSave != nil {
		return MomentsConfig{}, errSave
	}
	return mm.config, nil
}

// GetImagePath 获取图片存储路径
func (mm *MomentManager) GetImagePath(filename string) string {
	return filepath.Join(mm.baseDir, "images", filename)
}

// GetBackgroundPath 获取背景图存储路径
func (mm *MomentManager) GetBackgroundPath(filename string) string {
	return filepath.Join(mm.baseDir, "backgrounds", filename)
}
