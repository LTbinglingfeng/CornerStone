package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cornerstone/client"
	"cornerstone/logging"

	"github.com/google/uuid"
)

const (
	memoryBatchUpsertToolName = "memory_batch_upsert"
	memoryBatchUpsertMaxItems = 6
)

var errMemoryMatchingIDNotFound = errors.New("memory matching_id not found")

type TimeProvider interface {
	Now() time.Time
}

type memoryBatchUpsertArgs struct {
	Items []ExtractedMemory `json:"items"`
}

type memoryBatchUpsertParseResult struct {
	Items      []ExtractedMemory
	ValidCalls int
}

type memoryExtractionApplyStats struct {
	InputCount         int
	AddedCount         int
	UpdatedCount       int
	InvalidCount       int
	AddFailedCount     int
	UpdateFailedCount  int
	TruncatedItemCount int
	UnprocessedCount   int
}

var memoryExtractionTools = []client.Tool{
	{
		Type: "function",
		Function: client.ToolFunction{
			Name:        memoryBatchUpsertToolName,
			Description: "批量新增或更新长期记忆。若有多条记忆，必须一次性放入 items 数组统一提交。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "需要写入的记忆条目，最多 6 条。",
						"maxItems":    memoryBatchUpsertMaxItems,
						"items": map[string]interface{}{
							"oneOf": []interface{}{
								map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"matching_id": map[string]interface{}{
											"type":        "string",
											"description": "已有记忆 UUID，用于更新已有记忆。",
											"minLength":   1,
										},
										"subject": map[string]interface{}{
											"type": "string",
											"enum": []string{SubjectUser, SubjectSelf},
										},
										"category": map[string]interface{}{
											"type": "string",
											"enum": []string{
												CategoryIdentity,
												CategoryRelation,
												CategoryFact,
												CategoryPreference,
												CategoryEvent,
												CategoryEmotion,
												CategoryPromise,
												CategoryPlan,
												CategoryStatement,
												CategoryOpinion,
											},
										},
										"content": map[string]interface{}{
											"type":        "string",
											"description": "单行中文，100字内。",
											"maxLength":   MaxMemoryContentRunes,
										},
									},
									"required":             []string{"matching_id", "subject", "category", "content"},
									"additionalProperties": false,
								},
								map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"subject": map[string]interface{}{
											"type": "string",
											"enum": []string{SubjectUser, SubjectSelf},
										},
										"category": map[string]interface{}{
											"type": "string",
											"enum": []string{
												CategoryIdentity,
												CategoryRelation,
												CategoryFact,
												CategoryPreference,
												CategoryEvent,
												CategoryEmotion,
												CategoryPromise,
												CategoryPlan,
												CategoryStatement,
												CategoryOpinion,
											},
										},
										"content": map[string]interface{}{
											"type":        "string",
											"description": "单行中文，100字内。",
											"maxLength":   MaxMemoryContentRunes,
										},
									},
									"required":             []string{"subject", "category", "content"},
									"additionalProperties": false,
								},
							},
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		},
	},
}

func getMemoryExtractionTools() []client.Tool {
	tools := make([]client.Tool, len(memoryExtractionTools))
	for i, tool := range memoryExtractionTools {
		tools[i] = tool
		tools[i].Function.Parameters = deepCopyJSONSchemaMap(tool.Function.Parameters)
	}
	return tools
}

func deepCopyJSONSchemaMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}

	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = deepCopyJSONSchemaValue(v)
	}
	return dst
}

func deepCopyJSONSchemaValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return deepCopyJSONSchemaMap(typed)
	case []interface{}:
		copied := make([]interface{}, len(typed))
		for i, item := range typed {
			copied[i] = deepCopyJSONSchemaValue(item)
		}
		return copied
	case []string:
		copied := make([]string, len(typed))
		copy(copied, typed)
		return copied
	default:
		return value
	}
}

func parseMemoryBatchUpsertToolCalls(promptID string, toolCalls []client.ToolCall) memoryBatchUpsertParseResult {
	result := memoryBatchUpsertParseResult{
		Items: make([]ExtractedMemory, 0),
	}

	for _, toolCall := range toolCalls {
		if strings.TrimSpace(toolCall.Function.Name) != memoryBatchUpsertToolName {
			continue
		}

		var args memoryBatchUpsertArgs
		rawArgs := strings.TrimSpace(toolCall.Function.Arguments)
		if rawArgs == "" {
			rawArgs = "{}"
		}
		if errUnmarshal := json.Unmarshal([]byte(rawArgs), &args); errUnmarshal != nil {
			logging.Warnf(
				"memory extraction tool args parse failed: prompt=%q tool=%q id=%q error_kind=%q",
				promptID,
				memoryBatchUpsertToolName,
				strings.TrimSpace(toolCall.ID),
				logging.ErrorKind(errUnmarshal),
			)
			continue
		}

		result.ValidCalls++
		if len(args.Items) == 0 {
			continue
		}
		result.Items = append(result.Items, args.Items...)
	}

	return result
}

func truncateMemoryBatchItems(promptID string, items []ExtractedMemory) ([]ExtractedMemory, int) {
	if len(items) <= memoryBatchUpsertMaxItems {
		return items, 0
	}

	truncatedCount := len(items) - memoryBatchUpsertMaxItems
	logging.Warnf(
		"memory extraction tool items exceed limit, truncating: prompt=%q tool=%q total=%d kept=%d dropped=%d",
		promptID,
		memoryBatchUpsertToolName,
		len(items),
		memoryBatchUpsertMaxItems,
		truncatedCount,
	)
	return items[:memoryBatchUpsertMaxItems], truncatedCount
}

// applyExtractedMemories commits this extraction's changes together. Returning a
// save error must not leave half a batch applied before MemorySession retries it.
// This remains extraction-specific; the chat tool retains its own write policy.
func (e *MemoryExtractor) applyExtractedMemories(promptID string, items []ExtractedMemory, now time.Time) (stats memoryExtractionApplyStats, err error) {
	stats.InputCount = len(items)
	stats.UnprocessedCount = len(items)
	defer func() {
		if err != nil {
			stats.AddFailedCount += stats.AddedCount
			stats.UpdateFailedCount += stats.UpdatedCount
			stats.AddedCount = 0
			stats.UpdatedCount = 0
		}
	}()
	if err := ValidateID(promptID); err != nil {
		return stats, err
	}
	if now.IsZero() {
		now = time.Now()
	}

	e.mm.mu.Lock()
	defer e.mm.mu.Unlock()
	if err := e.mm.ensureLoadedLocked(promptID); err != nil {
		return stats, err
	}
	memories := append([]Memory(nil), e.mm.cache[promptID]...)
	type change struct {
		before Memory
		after  Memory
		added  bool
	}
	changes := make([]change, 0, len(items))

	for i, item := range items {
		stats.UnprocessedCount = len(items) - i - 1
		subject, category, content, ok := NormalizeExtractedMemoryFields(item.Subject, item.Category, item.Content)
		if !ok {
			stats.InvalidCount++
			logging.Warnf(
				"memory extraction field invalid: prompt=%q subject_bytes=%d category_bytes=%d content_bytes=%d",
				promptID, len(item.Subject), len(item.Category), len(item.Content),
			)
			continue
		}

		if item.MatchingID != nil {
			matchingID := strings.TrimSpace(*item.MatchingID)
			if err := ValidateID(matchingID); err != nil {
				stats.UpdateFailedCount++
				return stats, err
			}
			index := -1
			for i := range memories {
				if memories[i].ID == matchingID {
					index = i
					break
				}
			}
			if index < 0 {
				stats.UpdateFailedCount++
				return stats, fmt.Errorf("%w: %q", errMemoryMatchingIDNotFound, matchingID)
			}
			before := memories[index]
			updated := before
			updated.Subject = subject
			updated.Category = category
			updated.Content = content
			updated.ReinforceAt(now)
			memories[index] = updated
			changes = append(changes, change{before: before, after: updated})
			stats.UpdatedCount++
			continue
		}

		memory := Memory{
			ID:        uuid.New().String(),
			Subject:   subject,
			Category:  category,
			Content:   content,
			Strength:  DefaultStrengthForCategory(category),
			Stability: DefaultStabilityForCategory(category),
			LastSeen:  now,
			SeenCount: 1,
			CreatedAt: now,
		}
		memories = append(memories, memory)
		changes = append(changes, change{after: memory, added: true})
		stats.AddedCount++
	}
	if len(changes) == 0 {
		return stats, nil
	}
	if err := e.mm.commitLocked(promptID, memories); err != nil {
		return stats, err
	}
	for _, change := range changes {
		if change.added {
			logging.Infof("memory.added: persona_id=%q id=%q content_bytes=%d", promptID, change.after.ID, len(change.after.Content))
		} else if fields := changedFields(change.before, change.after); len(fields) > 0 {
			logging.Infof("memory.updated: persona_id=%q id=%q changed_fields=%v content_bytes=%d", promptID, change.after.ID, fields, len(change.after.Content))
		}
	}
	return stats, nil
}
