package api

import (
	"context"
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"fmt"
	"strings"
	"time"
)

// napCatMessageChain is channel-local, ordered and deliberately not serialized
// into shared history. Attachment metadata must come from registered resources
// (generated media, validated tool intents, or successfully saved incoming media),
// never directly from wire paths or text. Wire segments have a separate type.
type napCatMessageChain []napCatChainSegment
type napCatChainSegment struct {
	Text       string
	Attachment *storage.ChatAttachment
}

func napCatTextChain(text, token string) napCatMessageChain {
	var chain napCatMessageChain
	for _, chunk := range splitAssistantReplyMessages(text, token, napCatReplyChunkMaxRune) {
		chain = append(chain, napCatChainSegment{Text: chunk})
	}
	return chain
}

// legacyNapCatReplyChain preserves the historical attachments-first behavior.
// StorageMessages has separate columns: original text/media interleaving is lost
// and cannot be recovered by this adapter.
func legacyNapCatReplyChain(reply *napCatGeneratedReply, token string) napCatMessageChain {
	var chain napCatMessageChain
	for _, a := range napCatReplyAttachments(reply.StorageMessages) {
		chain = append(chain, napCatChainSegment{Attachment: &a})
	}
	return append(chain, napCatTextChain(strings.TrimSpace(reply.Text), token)...)
}

// encodeNapCatSegment is the only outbound protocol boundary. Model CQ prose
// remains a structured text value; resource bytes use confined storage reads.
func (s *NapCatService) encodeNapCatSegment(userID int64, segment napCatChainSegment) (string, map[string]interface{}, error) {
	if segment.Attachment != nil {
		if segment.Text != "" {
			return "", nil, errors.New("mixed payload in a single NapCat segment")
		}
		return s.encodeNapCatAttachment(userID, *segment.Attachment)
	}
	if segment.Text == "" {
		return "", nil, errors.New("empty NapCat segment")
	}
	return "send_private_msg", map[string]interface{}{"user_id": userID, "message": []napCatMessageSegment{{Type: "text", Data: napCatMessageSegmentData{Text: segment.Text}}}}, nil
}

// deliverNapCatChain returns exactly the confirmed prefix, one receipt per
// segment. No batching, retries, or continuation after an uncertain action.
func (s *NapCatService) deliverNapCatChain(ctx context.Context, userID int64, chain napCatMessageChain) (napCatMessageChain, error) {
	if s == nil || userID <= 0 {
		return nil, errors.New("invalid private destination")
	}
	var confirmed napCatMessageChain
	for i, segment := range chain {
		started := time.Now()
		action, params, err := s.encodeNapCatSegment(userID, segment)
		if err == nil {
			err = s.sendConfirmedAction(ctx, action, params)
		}
		if err != nil {
			status := "unconfirmed"
			if len(confirmed) > 0 {
				status = "partial"
			}
			channelOutcome(ctx, status, err)
			logging.Event(ctx, "channel.delivery.segment.completed", "status", "unconfirmed", "segment", i, "duration_ms", time.Since(started).Milliseconds(), "error_kind", logging.ErrorKind(err))
			return confirmed, fmt.Errorf("NapCat segment %d delivery unconfirmed; do not automatically retry: %w", i, err)
		}
		logging.Event(ctx, "channel.delivery.segment.completed", "status", "confirmed", "segment", i, "duration_ms", time.Since(started).Milliseconds())
		confirmed = append(confirmed, segment)
	}
	return confirmed, nil
}

// legacyReceived projects resolved channel segments into the existing shared
// input columns. Only this boundary discards interleaving; placeholders stay in
// their original text position. It does not grant access to incoming wire paths.
func (chain napCatMessageChain) legacyReceived() (string, []string, []storage.ChatAttachment) {
	var text strings.Builder
	var images []string
	var attachments []storage.ChatAttachment
	for _, segment := range chain {
		if segment.Attachment == nil {
			text.WriteString(segment.Text)
			continue
		}
		a := *segment.Attachment
		if a.Kind == "image" {
			images = append(images, a.Path)
		} else {
			attachments = append(attachments, a)
		}
	}
	return strings.TrimSpace(text.String()), images, attachments
}
