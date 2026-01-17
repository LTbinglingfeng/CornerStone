package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

// MomentGenerator 朋友圈生成器（异步处理生图）
type MomentGenerator struct {
	momentManager *storage.MomentManager
	configManager *config.Manager

	tasks   map[string]context.CancelFunc // momentID -> cancel函数
	tasksMu sync.RWMutex
}

func NewMomentGenerator(mm *storage.MomentManager, cm *config.Manager) *MomentGenerator {
	return &MomentGenerator{
		momentManager: mm,
		configManager: cm,
		tasks:         make(map[string]context.CancelFunc),
	}
}

// StartGeneration 启动异步生图任务
func (mg *MomentGenerator) StartGeneration(momentID string) {
	momentID = strings.TrimSpace(momentID)
	if momentID == "" {
		return
	}

	mg.tasksMu.Lock()
	if _, ok := mg.tasks[momentID]; ok {
		mg.tasksMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	mg.tasks[momentID] = cancel
	mg.tasksMu.Unlock()

	go mg.generateImage(ctx, momentID)
}

// CancelGeneration 取消生图任务
func (mg *MomentGenerator) CancelGeneration(momentID string) {
	mg.tasksMu.Lock()
	defer mg.tasksMu.Unlock()

	if cancel, ok := mg.tasks[momentID]; ok {
		cancel()
		delete(mg.tasks, momentID)
	}
}

func (mg *MomentGenerator) generateImage(ctx context.Context, momentID string) {
	defer func() {
		mg.tasksMu.Lock()
		delete(mg.tasks, momentID)
		mg.tasksMu.Unlock()
	}()

	moment, ok := mg.momentManager.Get(momentID)
	if !ok {
		return
	}

	if _, errUpdate := mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
		m.Status = storage.MomentStatusGenerating
		m.ErrorMsg = ""
		return nil
	}); errUpdate != nil {
		logging.Errorf("moment update generating failed: id=%s err=%v", momentID, errUpdate)
		return
	}

	imageProvider := mg.configManager.GetImageProvider()
	if imageProvider == nil {
		_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
			m.Status = storage.MomentStatusFailed
			m.ErrorMsg = "未配置生图供应商"
			return nil
		})
		return
	}
	if strings.TrimSpace(imageProvider.APIKey) == "" {
		_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
			m.Status = storage.MomentStatusFailed
			m.ErrorMsg = "生图供应商未配置API Key"
			return nil
		})
		return
	}

	imageClient := client.NewGeminiImageClient(imageProvider.BaseURL, imageProvider.APIKey)

	startedAt := time.Now()
	modelLower := strings.ToLower(strings.TrimSpace(imageProvider.Model))
	mimeType := ""
	imageBytes := []byte(nil)

	if strings.Contains(modelLower, "imagen") {
		cfg := &genai.GenerateImagesConfig{
			NumberOfImages: 1,
		}
		if imageProvider.GeminiImageAspectRatio != nil {
			cfg.AspectRatio = *imageProvider.GeminiImageAspectRatio
		}
		if imageProvider.GeminiImageSize != nil {
			cfg.ImageSize = *imageProvider.GeminiImageSize
		}
		if imageProvider.GeminiImageOutputMIMEType != nil {
			cfg.OutputMIMEType = *imageProvider.GeminiImageOutputMIMEType
		}

		resp, errGenerate := imageClient.GenerateImages(ctx, imageProvider.Model, moment.ImagePrompt, cfg)
		if errGenerate != nil {
			logging.Errorf("moment image generation failed: id=%s err=%v", momentID, errGenerate)
			_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
				m.Status = storage.MomentStatusFailed
				m.ErrorMsg = errGenerate.Error()
				return nil
			})
			return
		}

		if resp == nil || len(resp.GeneratedImages) == 0 || resp.GeneratedImages[0] == nil || resp.GeneratedImages[0].Image == nil {
			_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
				m.Status = storage.MomentStatusFailed
				m.ErrorMsg = "生图返回为空"
				return nil
			})
			return
		}

		image := resp.GeneratedImages[0].Image
		if len(image.ImageBytes) == 0 {
			_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
				m.Status = storage.MomentStatusFailed
				m.ErrorMsg = "生图返回为空"
				return nil
			})
			return
		}

		imageBytes = image.ImageBytes
		mimeType = strings.ToLower(strings.TrimSpace(image.MIMEType))
		if mimeType == "" && imageProvider.GeminiImageOutputMIMEType != nil {
			mimeType = strings.ToLower(strings.TrimSpace(*imageProvider.GeminiImageOutputMIMEType))
		}
	} else {
		temp := float32(imageProvider.Temperature)
		if temp <= 0 {
			temp = 1
		}
		topP := float32(imageProvider.TopP)
		if topP <= 0 {
			topP = 1
		}
		budget32 := int32(16000)
		cfg := &genai.GenerateContentConfig{
			Temperature:        genai.Ptr(temp),
			TopP:               genai.Ptr(topP),
			ResponseModalities: []string{"TEXT", "IMAGE"},
			ThinkingConfig: &genai.ThinkingConfig{
				IncludeThoughts: true,
				ThinkingBudget:  &budget32,
			},
		}

		result, errGenerate := imageClient.GenerateContentImage(ctx, imageProvider.Model, moment.ImagePrompt, cfg)
		if errGenerate != nil {
			logging.Errorf("moment image generation failed: id=%s err=%v", momentID, errGenerate)
			_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
				m.Status = storage.MomentStatusFailed
				m.ErrorMsg = errGenerate.Error()
				return nil
			})
			return
		}

		imageBytes = result.Data
		mimeType = strings.ToLower(strings.TrimSpace(result.MIMEType))
	}

	if len(imageBytes) == 0 {
		_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
			m.Status = storage.MomentStatusFailed
			m.ErrorMsg = "生图返回为空"
			return nil
		})
		return
	}

	ext := ".png"
	switch mimeType {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}

	filename := fmt.Sprintf("%s%s", momentID, ext)
	imagePath := mg.momentManager.GetImagePath(filename)
	if errWrite := os.WriteFile(imagePath, imageBytes, 0644); errWrite != nil {
		logging.Errorf("failed to save moment image: id=%s path=%s err=%v", momentID, imagePath, errWrite)
		_, _ = mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
			m.Status = storage.MomentStatusFailed
			m.ErrorMsg = "保存图片失败"
			return nil
		})
		return
	}

	relPath := "moments/images/" + filename
	_, errPublish := mg.momentManager.UpdateByID(momentID, func(m *storage.Moment) error {
		m.ImagePath = relPath
		m.Status = storage.MomentStatusPublished
		m.ErrorMsg = ""
		return nil
	})
	if errPublish != nil {
		logging.Errorf("moment publish failed: id=%s err=%v", momentID, errPublish)
		return
	}

	logging.Infof("moment published: id=%s prompt_id=%s elapsed=%s", momentID, moment.PromptID, time.Since(startedAt).Truncate(10*time.Millisecond))
}
