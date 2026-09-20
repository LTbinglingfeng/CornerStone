package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"fmt"
	"strings"
)

// No binary attachment is silently presented to the text/image model as understood.
func (s *ClawBotService) receiveClawBotAttachments(ctx context.Context, msg client.ClawBotIncomingMessage) ([]storage.ChatAttachment, string) {
	var attachments []storage.ChatAttachment
	var notices []string
	var total int64
	count := 0
	primaryText := strings.TrimSpace(client.ExtractTextFromClawBotMessage(msg))
	for _, item := range msg.ItemList {
		if item.Type < 3 || item.Type > 5 {
			continue
		}
		count++
		if count > 16 || total >= storage.MaxAttachmentBytes {
			notices = append(notices, "[附件数量或总大小超限，未处理其余附件。]")
			break
		}
		if item.Type == 3 && item.VoiceItem != nil {
			transcript := strings.TrimSpace(item.VoiceItem.Text)
			if transcript != "" && transcript != primaryText {
				notices = append(notices, transcript)
			}
		}
		if item.Type == 3 && item.VoiceItem != nil && item.VoiceItem.Media == nil && strings.TrimSpace(item.VoiceItem.Text) != "" {
			continue
		}
		data, kind, name, err := s.client.DownloadMediaItem(ctx, item)
		if err == nil && total+int64(len(data)) > storage.MaxAttachmentBytes {
			err = fmt.Errorf("attachment batch size exceeded")
		}
		if err == nil {
			var attachment storage.ChatAttachment
			attachment, err = s.handler.attachmentStore().Save(data, name, kind)
			if err == nil {
				attachments = append(attachments, attachment)
				total += attachment.Size
				notices = append(notices, fmt.Sprintf("[附件 %s (%s, %d bytes) 已保存。当前模型无法读取此附件内容；仅可依据消息文字或语音转写回答，不要声称已听取/观看/读取。]", attachment.Name, kind, attachment.Size))
			}
		}
		if err != nil {
			notices = append(notices, "[附件下载、解密或校验失败，内容不可用；不要声称已读取。]")
		}
	}
	return attachments, strings.Join(notices, "\n")
}
