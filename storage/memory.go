package storage

import (
	"bufio"
	"bytes"
	"cornerstone/logging"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Memory struct {
	ID        string    `json:"id"`         // UUID
	Subject   string    `json:"subject"`    // "user" 或 "self"
	Category  string    `json:"category"`   // 分类
	Content   string    `json:"content"`    // 记忆内容
	Strength  float64   `json:"strength"`   // 基础强度 0~1（持久化存储）
	LastSeen  time.Time `json:"last_seen"`  // 上次使用时间
	SeenCount int       `json:"seen_count"` // 累计使用次数
	CreatedAt time.Time `json:"created_at"` // 创建时间
}

type MemoryPatch struct {
	ID        string
	Subject   *string
	Category  *string
	Content   *string
	Strength  *float64
	LastSeen  *time.Time
	SeenCount *int
	CreatedAt *time.Time
}

type MemoryResponse struct {
	ID              string    `json:"id"`
	Subject         string    `json:"subject"`
	Category        string    `json:"category"`
	Content         string    `json:"content"`
	Strength        float64   `json:"strength"`         // 基础强度
	CurrentStrength float64   `json:"current_strength"` // 当前强度（实时计算）
	LastSeen        time.Time `json:"last_seen"`
	SeenCount       int       `json:"seen_count"`
	CreatedAt       time.Time `json:"created_at"`
}

func (m *Memory) ToResponse() MemoryResponse {
	return MemoryResponse{
		ID:              m.ID,
		Subject:         m.Subject,
		Category:        m.Category,
		Content:         m.Content,
		Strength:        m.Strength,
		CurrentStrength: m.CurrentStrength(),
		LastSeen:        m.LastSeen,
		SeenCount:       m.SeenCount,
		CreatedAt:       m.CreatedAt,
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (m *Memory) CurrentStrength() float64 {
	hours := time.Since(m.LastSeen).Hours()
	if hours < 0 {
		hours = 0
	}

	seenCount := m.SeenCount
	if seenCount < 0 {
		seenCount = 0
	}

	stability := 1 + 0.5*math.Log(float64(seenCount+1))
	if stability <= 0 || math.IsNaN(stability) || math.IsInf(stability, 0) {
		stability = 1
	}
	decay := math.Exp(-hours / (stability * 24))
	return clamp01(clamp01(m.Strength) * decay)
}

func (m *Memory) Reinforce() {
	m.SeenCount++
	m.LastSeen = time.Now()
	m.Strength = math.Min(1.0, m.Strength*1.2+0.15)
}

const (
	SubjectUser = "user"
	SubjectSelf = "self"
)

const (
	MaxMemoryContentRunes = 100
)

const (
	ThresholdActive   = 0.4
	ThresholdArchive  = 0.15
	MaxActiveMemories = 100
)

const (
	CategoryIdentity   = "identity"
	CategoryRelation   = "relation"
	CategoryFact       = "fact"
	CategoryPreference = "preference"
	CategoryEvent      = "event"
	CategoryEmotion    = "emotion"
)

const (
	CategoryPromise   = "promise"
	CategoryPlan      = "plan"
	CategoryStatement = "statement"
	CategoryOpinion   = "opinion"
)

var CategoryStrength = map[string]float64{
	CategoryIdentity:   0.95,
	CategoryRelation:   0.90,
	CategoryFact:       0.85,
	CategoryPreference: 0.70,
	CategoryEvent:      0.60,
	CategoryEmotion:    0.50,
	CategoryPromise:    0.90,
	CategoryPlan:       0.85,
	CategoryStatement:  0.70,
	CategoryOpinion:    0.60,
}

func DefaultStrengthForCategory(category string) float64 {
	defaultStrength, ok := CategoryStrength[category]
	if ok {
		return defaultStrength
	}
	return 0.5
}

type MemoryManager struct {
	baseDir string
	mu      sync.RWMutex
	cache   map[string][]Memory
}

func NewMemoryManager(baseDir string) *MemoryManager {
	_ = os.MkdirAll(baseDir, 0755)
	return &MemoryManager{
		baseDir: baseDir,
		cache:   make(map[string][]Memory),
	}
}

func (mm *MemoryManager) getMemoryPath(promptID string) string {
	return filepath.Join(mm.baseDir, promptID, "memories.jsonl")
}

func (mm *MemoryManager) ensureLoadedLocked(promptID string) error {
	if _, ok := mm.cache[promptID]; ok {
		return nil
	}
	_, errLoad := mm.loadLocked(promptID)
	return errLoad
}

func (mm *MemoryManager) loadLocked(promptID string) ([]Memory, error) {
	memoryPath := mm.getMemoryPath(promptID)
	file, errOpen := os.Open(memoryPath)
	if errOpen != nil {
		if os.IsNotExist(errOpen) {
			mm.cache[promptID] = []Memory{}
			return mm.cache[promptID], nil
		}
		logging.Errorf("memory load failed: prompt=%s path=%s err=%v", promptID, memoryPath, errOpen)
		return nil, errOpen
	}
	defer file.Close()

	var memories []Memory
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var memory Memory
		errUnmarshal := json.Unmarshal(line, &memory)
		if errUnmarshal != nil {
			logging.Warnf("memory entry parse failed: prompt=%s line=%s err=%v", promptID, logging.Truncate(string(line), 100), errUnmarshal)
			continue
		}
		memories = append(memories, memory)
	}
	errScan := scanner.Err()
	if errScan != nil {
		logging.Errorf("memory scan failed: prompt=%s err=%v", promptID, errScan)
		return nil, errScan
	}

	mm.cache[promptID] = memories
	return memories, nil
}

func (mm *MemoryManager) saveLocked(promptID string) error {
	promptDir := filepath.Join(mm.baseDir, promptID)
	errMkdirAll := os.MkdirAll(promptDir, 0755)
	if errMkdirAll != nil {
		logging.Errorf("memory dir create failed: path=%s err=%v", promptDir, errMkdirAll)
		return errMkdirAll
	}

	memoryPath := mm.getMemoryPath(promptID)
	file, errOpen := os.OpenFile(memoryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if errOpen != nil {
		logging.Errorf("memory file open failed: path=%s err=%v", memoryPath, errOpen)
		return errOpen
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, memory := range mm.cache[promptID] {
		errWrite := writeJSONLine(writer, memory)
		if errWrite != nil {
			logging.Errorf("memory write failed: prompt=%s err=%v", promptID, errWrite)
			return errWrite
		}
	}
	errFlush := writer.Flush()
	if errFlush != nil {
		logging.Errorf("memory flush failed: prompt=%s err=%v", promptID, errFlush)
		return errFlush
	}
	_ = os.Chmod(memoryPath, 0600)
	return nil
}

func (mm *MemoryManager) Load(promptID string) ([]Memory, error) {
	errValidateID := ValidateID(promptID)
	if errValidateID != nil {
		return nil, errValidateID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	memories, errLoad := mm.loadLocked(promptID)
	if errLoad != nil {
		return nil, errLoad
	}
	return append([]Memory(nil), memories...), nil
}

func (mm *MemoryManager) Save(promptID string) error {
	errValidateID := ValidateID(promptID)
	if errValidateID != nil {
		return errValidateID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return errEnsureLoaded
	}
	return mm.saveLocked(promptID)
}

func (mm *MemoryManager) Add(promptID string, memory Memory) error {
	errValidateID := ValidateID(promptID)
	if errValidateID != nil {
		return errValidateID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return errEnsureLoaded
	}

	if memory.ID == "" {
		memory.ID = uuid.New().String()
	}

	now := time.Now()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	if memory.LastSeen.IsZero() {
		memory.LastSeen = now
	}
	if memory.SeenCount < 0 {
		memory.SeenCount = 0
	}
	memory.Strength = clamp01(memory.Strength)

	mm.cache[promptID] = append(mm.cache[promptID], memory)
	return mm.saveLocked(promptID)
}

func (mm *MemoryManager) Delete(promptID, memoryID string) error {
	errValidatePromptID := ValidateID(promptID)
	if errValidatePromptID != nil {
		return errValidatePromptID
	}
	errValidateMemoryID := ValidateID(memoryID)
	if errValidateMemoryID != nil {
		return errValidateMemoryID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return errEnsureLoaded
	}

	memories := mm.cache[promptID]
	for i := range memories {
		if memories[i].ID != memoryID {
			continue
		}
		mm.cache[promptID] = append(memories[:i], memories[i+1:]...)
		return mm.saveLocked(promptID)
	}

	return os.ErrNotExist
}

// Update 完整替换记忆（保留原始 ID 和 CreatedAt）
// 如果只需要更新部分字段，请使用 Patch 方法
func (mm *MemoryManager) Update(promptID string, memory Memory) error {
	errValidatePromptID := ValidateID(promptID)
	if errValidatePromptID != nil {
		return errValidatePromptID
	}
	errValidateMemoryID := ValidateID(memory.ID)
	if errValidateMemoryID != nil {
		return errValidateMemoryID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return errEnsureLoaded
	}

	memories := mm.cache[promptID]
	for i := range memories {
		if memories[i].ID != memory.ID {
			continue
		}

		// 保留原始 CreatedAt
		if memory.CreatedAt.IsZero() {
			memory.CreatedAt = memories[i].CreatedAt
		}

		// 规范化字段
		memory.Strength = clamp01(memory.Strength)
		if memory.SeenCount < 0 {
			memory.SeenCount = 0
		}
		if memory.LastSeen.IsZero() {
			memory.LastSeen = time.Now()
		}

		memories[i] = memory
		mm.cache[promptID] = memories
		return mm.saveLocked(promptID)
	}

	return os.ErrNotExist
}

func (mm *MemoryManager) Patch(promptID string, patch MemoryPatch) error {
	errValidatePromptID := ValidateID(promptID)
	if errValidatePromptID != nil {
		return errValidatePromptID
	}
	errValidateMemoryID := ValidateID(patch.ID)
	if errValidateMemoryID != nil {
		return errValidateMemoryID
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return errEnsureLoaded
	}

	memories := mm.cache[promptID]
	for i := range memories {
		if memories[i].ID != patch.ID {
			continue
		}

		if patch.Subject != nil {
			memories[i].Subject = *patch.Subject
		}
		if patch.Category != nil {
			memories[i].Category = *patch.Category
		}
		if patch.Content != nil {
			memories[i].Content = *patch.Content
		}
		if patch.Strength != nil {
			memories[i].Strength = clamp01(*patch.Strength)
		}
		if patch.LastSeen != nil {
			memories[i].LastSeen = *patch.LastSeen
		}
		if patch.SeenCount != nil {
			if *patch.SeenCount < 0 {
				memories[i].SeenCount = 0
			} else {
				memories[i].SeenCount = *patch.SeenCount
			}
		}
		if patch.CreatedAt != nil {
			memories[i].CreatedAt = *patch.CreatedAt
		}

		mm.cache[promptID] = memories
		return mm.saveLocked(promptID)
	}

	return os.ErrNotExist
}

func (mm *MemoryManager) GetAll(promptID string) []Memory {
	errValidateID := ValidateID(promptID)
	if errValidateID != nil {
		return []Memory{}
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	errEnsureLoaded := mm.ensureLoadedLocked(promptID)
	if errEnsureLoaded != nil {
		return []Memory{}
	}

	memories := mm.cache[promptID]
	return append([]Memory(nil), memories...)
}

func (mm *MemoryManager) FindByID(promptID, memoryID string) *Memory {
	memories := mm.GetAll(promptID)
	for i := range memories {
		if memories[i].ID == memoryID {
			return &memories[i]
		}
	}
	return nil
}

func (mm *MemoryManager) GetAllResponses(promptID string) []MemoryResponse {
	memories := mm.GetAll(promptID)
	responses := make([]MemoryResponse, len(memories))
	for i := range memories {
		responses[i] = memories[i].ToResponse()
	}
	return responses
}

func (mm *MemoryManager) GetActiveMemories(promptID string) []Memory {
	all := mm.GetAll(promptID)

	active := make([]Memory, 0, len(all))
	for _, memory := range all {
		if memory.CurrentStrength() >= ThresholdActive {
			active = append(active, memory)
		}
	}

	sort.Slice(active, func(i, j int) bool {
		return active[i].CurrentStrength() > active[j].CurrentStrength()
	})

	if len(active) > MaxActiveMemories {
		active = active[:MaxActiveMemories]
	}

	return active
}
