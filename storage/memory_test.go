package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestMemoryManager(t *testing.T) *MemoryManager {
	t.Helper()
	dir := t.TempDir()
	return NewMemoryManager(dir)
}

func TestCurrentStrength_Pinned(t *testing.T) {
	// 固定记忆不衰减，即使 LastSeen 很久之前
	m := Memory{
		Strength:  0.8,
		Stability: 1.0,
		LastSeen:  time.Now().Add(-720 * time.Hour), // 30天前
		Pinned:    true,
	}
	cs := m.CurrentStrength()
	if cs != 0.8 {
		t.Errorf("pinned memory CurrentStrength = %v, want 0.8", cs)
	}

	// 非固定记忆应该衰减
	m2 := Memory{
		Strength:  0.8,
		Stability: 1.0,
		LastSeen:  time.Now().Add(-720 * time.Hour),
		Pinned:    false,
	}
	cs2 := m2.CurrentStrength()
	if cs2 >= 0.8 {
		t.Errorf("non-pinned memory should have decayed, got %v", cs2)
	}
}

func TestCurrentStrength_PinnedClamp(t *testing.T) {
	// 固定记忆强度也应该被 clamp 到 [0,1]
	m := Memory{
		Strength: 1.5,
		Pinned:   true,
	}
	cs := m.CurrentStrength()
	if cs != 1.0 {
		t.Errorf("pinned memory with strength 1.5 should clamp to 1.0, got %v", cs)
	}

	m2 := Memory{
		Strength: -0.5,
		Pinned:   true,
	}
	cs2 := m2.CurrentStrength()
	if cs2 != 0 {
		t.Errorf("pinned memory with negative strength should clamp to 0, got %v", cs2)
	}
}

func TestGetActiveMemories_IncludesPinned(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	// 创建 prompt 目录
	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	// 添加一个固定记忆（低强度，应该仍然出现在活跃列表中）
	pinnedMem := Memory{
		Subject:   SubjectUser,
		Category:  CategoryIdentity,
		Content:   "pinned memory",
		Strength:  0.1, // 低于 ThresholdActive
		Stability: 1.0,
		Pinned:    true,
	}
	if err := mm.Add(promptID, pinnedMem); err != nil {
		t.Fatalf("failed to add pinned memory: %v", err)
	}

	// 添加一个普通活跃记忆
	activeMem := Memory{
		Subject:   SubjectUser,
		Category:  CategoryFact,
		Content:   "active memory",
		Strength:  0.9,
		Stability: 2.0,
	}
	if err := mm.Add(promptID, activeMem); err != nil {
		t.Fatalf("failed to add active memory: %v", err)
	}

	// 添加一个低强度非固定记忆（应该不出现在活跃列表中）
	weakMem := Memory{
		Subject:   SubjectUser,
		Category:  CategoryEvent,
		Content:   "weak memory",
		Strength:  0.05,
		Stability: 1.0,
		LastSeen:  time.Now().Add(-720 * time.Hour),
	}
	if err := mm.Add(promptID, weakMem); err != nil {
		t.Fatalf("failed to add weak memory: %v", err)
	}

	active := mm.GetActiveMemories(promptID)

	// 应该包含固定记忆和活跃记忆，不包含弱记忆
	if len(active) != 2 {
		t.Errorf("expected 2 active memories, got %d", len(active))
	}

	// 固定记忆应该排在前面
	if len(active) > 0 && !active[0].Pinned {
		t.Errorf("expected pinned memory first, got pinned=%v", active[0].Pinned)
	}
}

func TestPatch_Pinned(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	mem := Memory{
		Subject:  SubjectUser,
		Category: CategoryIdentity,
		Content:  "test memory",
		Strength: 0.8,
	}
	if err := mm.Add(promptID, mem); err != nil {
		t.Fatalf("failed to add memory: %v", err)
	}

	memories := mm.GetAll(promptID)
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}

	memID := memories[0].ID
	if memories[0].Pinned {
		t.Errorf("memory should not be pinned initially")
	}

	// Pin the memory
	pinTrue := true
	err := mm.Patch(promptID, MemoryPatch{
		ID:     memID,
		Pinned: &pinTrue,
	})
	if err != nil {
		t.Fatalf("failed to patch pinned: %v", err)
	}

	memories = mm.GetAll(promptID)
	if !memories[0].Pinned {
		t.Errorf("memory should be pinned after patch")
	}

	// Unpin the memory
	pinFalse := false
	err = mm.Patch(promptID, MemoryPatch{
		ID:     memID,
		Pinned: &pinFalse,
	})
	if err != nil {
		t.Fatalf("failed to patch unpinned: %v", err)
	}

	memories = mm.GetAll(promptID)
	if memories[0].Pinned {
		t.Errorf("memory should not be pinned after unpin")
	}
}

func TestDeleteBatch(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	// 添加 5 条记忆
	for i := 0; i < 5; i++ {
		mem := Memory{
			Subject:  SubjectUser,
			Category: CategoryFact,
			Content:  "memory content",
			Strength: 0.8,
		}
		if err := mm.Add(promptID, mem); err != nil {
			t.Fatalf("failed to add memory %d: %v", i, err)
		}
	}

	memories := mm.GetAll(promptID)
	if len(memories) != 5 {
		t.Fatalf("expected 5 memories, got %d", len(memories))
	}

	// 批量删除前 3 条
	idsToDelete := []string{memories[0].ID, memories[1].ID, memories[2].ID}
	deleted, err := mm.DeleteBatch(promptID, idsToDelete)
	if err != nil {
		t.Fatalf("failed to batch delete: %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}

	remaining := mm.GetAll(promptID)
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(remaining))
	}

	// 验证剩余的是正确的
	for _, r := range remaining {
		if r.ID == memories[0].ID || r.ID == memories[1].ID || r.ID == memories[2].ID {
			t.Errorf("deleted memory %s should not be in remaining list", r.ID)
		}
	}
}

func TestDeleteBatch_Empty(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	deleted, err := mm.DeleteBatch(promptID, []string{})
	if err != nil {
		t.Fatalf("empty batch delete should not error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

func TestDeleteBatch_NonexistentIDs(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	mem := Memory{
		Subject:  SubjectUser,
		Category: CategoryFact,
		Content:  "test",
		Strength: 0.8,
	}
	if err := mm.Add(promptID, mem); err != nil {
		t.Fatalf("failed to add memory: %v", err)
	}

	deleted, err := mm.DeleteBatch(promptID, []string{"nonexistent-id"})
	if err != nil {
		t.Fatalf("batch delete with nonexistent ID should not error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}

	// 确保原记忆未被影响
	remaining := mm.GetAll(promptID)
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(remaining))
	}
}

func TestDeleteArchived(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	// 添加活跃记忆
	activeMem := Memory{
		Subject:  SubjectUser,
		Category: CategoryIdentity,
		Content:  "active",
		Strength: 0.9,
	}
	if err := mm.Add(promptID, activeMem); err != nil {
		t.Fatalf("failed to add active memory: %v", err)
	}

	// 添加归档记忆（低强度 + 很久之前的 LastSeen）
	archivedMem := Memory{
		Subject:  SubjectUser,
		Category: CategoryEvent,
		Content:  "archived",
		Strength: 0.1,
		LastSeen: time.Now().Add(-2160 * time.Hour), // 90天前
	}
	if err := mm.Add(promptID, archivedMem); err != nil {
		t.Fatalf("failed to add archived memory: %v", err)
	}

	// 添加固定记忆（低强度但不应被删除）
	pinnedMem := Memory{
		Subject:  SubjectUser,
		Category: CategoryIdentity,
		Content:  "pinned low strength",
		Strength: 0.05,
		LastSeen: time.Now().Add(-2160 * time.Hour),
		Pinned:   true,
	}
	if err := mm.Add(promptID, pinnedMem); err != nil {
		t.Fatalf("failed to add pinned memory: %v", err)
	}

	// 清空归档
	deleted, err := mm.DeleteArchived(promptID)
	if err != nil {
		t.Fatalf("failed to delete archived: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	remaining := mm.GetAll(promptID)
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining (active + pinned), got %d", len(remaining))
	}

	// 验证固定记忆和活跃记忆仍然存在
	hasPinned := false
	hasActive := false
	for _, m := range remaining {
		if m.Pinned {
			hasPinned = true
		}
		if m.Content == "active" {
			hasActive = true
		}
	}
	if !hasPinned {
		t.Errorf("pinned memory should not be deleted")
	}
	if !hasActive {
		t.Errorf("active memory should not be deleted")
	}
}

func TestDeleteArchived_NoneToDelete(t *testing.T) {
	mm := newTestMemoryManager(t)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(mm.baseDir, promptID), 0755)

	// 只添加活跃记忆
	mem := Memory{
		Subject:  SubjectUser,
		Category: CategoryIdentity,
		Content:  "active",
		Strength: 0.9,
	}
	if err := mm.Add(promptID, mem); err != nil {
		t.Fatalf("failed to add memory: %v", err)
	}

	deleted, err := mm.DeleteArchived(promptID)
	if err != nil {
		t.Fatalf("delete archived should not error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

func TestPinnedPersistence(t *testing.T) {
	dir := t.TempDir()
	mm := NewMemoryManager(dir)
	promptID := "test-prompt"

	_ = os.MkdirAll(filepath.Join(dir, promptID), 0755)

	mem := Memory{
		Subject:  SubjectUser,
		Category: CategoryIdentity,
		Content:  "persistent pinned",
		Strength: 0.8,
		Pinned:   true,
	}
	if err := mm.Add(promptID, mem); err != nil {
		t.Fatalf("failed to add memory: %v", err)
	}

	// 创建新的 manager 来模拟重启
	mm2 := NewMemoryManager(dir)
	memories, err := mm2.Load(promptID)
	if err != nil {
		t.Fatalf("failed to load memories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if !memories[0].Pinned {
		t.Errorf("pinned state should persist across restarts")
	}
}
