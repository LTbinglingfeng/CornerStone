package storage

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"cornerstone/client"
	"cornerstone/logging"
)

const (
	memoryBatchUpsertToolName = "memory_batch_upsert"
	memoryBatchUpsertMaxItems = 6
)

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
				"memory extraction tool args parse failed: prompt=%s tool=%s id=%s err=%v",
				promptID,
				memoryBatchUpsertToolName,
				strings.TrimSpace(toolCall.ID),
				errUnmarshal,
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
		"memory extraction tool items exceed limit, truncating: prompt=%s tool=%s total=%d kept=%d dropped=%d",
		promptID,
		memoryBatchUpsertToolName,
		len(items),
		memoryBatchUpsertMaxItems,
		truncatedCount,
	)
	return items[:memoryBatchUpsertMaxItems], truncatedCount
}

func (e *MemoryExtractor) applyExtractedMemories(promptID string, items []ExtractedMemory, now time.Time) memoryExtractionApplyStats {
	stats := memoryExtractionApplyStats{
		InputCount: len(items),
	}

	for _, item := range items {
		subject, category, content, ok := NormalizeExtractedMemoryFields(item.Subject, item.Category, item.Content)
		if !ok {
			stats.InvalidCount++
			logging.Warnf(
				"memory extraction field invalid: prompt=%s subject=%s category=%s content=%s",
				promptID,
				item.Subject,
				item.Category,
				logging.Truncate(item.Content, 50),
			)
			continue
		}

		if item.MatchingID != nil && strings.TrimSpace(*item.MatchingID) != "" {
			matchingID := strings.TrimSpace(*item.MatchingID)
			old := e.mm.FindByID(promptID, matchingID)
			if old != nil {
				seenCount := old.SeenCount + 1
				if seenCount <= 0 {
					seenCount = 1
				}
				strength := clamp01(old.Strength)
				strength = math.Min(1.0, strength*1.2+0.15)
				stability := old.Stability
				if stability <= 0 {
					stability = DefaultStabilityForCategory(category)
				}
				stability = math.Min(10.0, stability+0.3)
				errUpdate := e.mm.Patch(promptID, MemoryPatch{
					ID:        matchingID,
					Subject:   &subject,
					Category:  &category,
					Content:   &content,
					Strength:  &strength,
					Stability: &stability,
					LastSeen:  &now,
					SeenCount: &seenCount,
				})
				if errUpdate != nil {
					stats.UpdateFailedCount++
					logging.Errorf("memory update failed: prompt=%s id=%s err=%v", promptID, matchingID, errUpdate)
				} else {
					stats.UpdatedCount++
				}
				continue
			}
			logging.Infof("memory matching id not found, adding: prompt=%s id=%s", promptID, matchingID)
		}

		errAdd := e.mm.Add(promptID, Memory{
			Subject:   subject,
			Category:  category,
			Content:   content,
			Strength:  DefaultStrengthForCategory(category),
			Stability: DefaultStabilityForCategory(category),
			LastSeen:  now,
			SeenCount: 1,
			CreatedAt: now,
		})
		if errAdd != nil {
			stats.AddFailedCount++
			logging.Errorf("memory add failed: prompt=%s err=%v", promptID, errAdd)
		} else {
			stats.AddedCount++
		}
	}

	return stats
}
