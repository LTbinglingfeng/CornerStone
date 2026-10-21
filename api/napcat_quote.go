package api

import (
	"errors"
	"strings"
)

// Validate provenance before parsing or downloading any quoted media. An action
// response is trusted to describe the message, not to authorize another chat's
// content. Bot-authored messages require an explicit peer: the bot alone cannot
// identify which private conversation a message belongs to.
func validateNapCatQuoteSource(source napCatChatSource, requestedID int64, data napCatGetMsgData) error {
	if !source.isPrivate() || source.SelfID <= 0 || source.UserID <= 0 || source.SelfID == source.UserID {
		return errors.New("quote source identity unavailable")
	}
	if requestedID <= 0 || data.MessageID != requestedID {
		return errors.New("quote message ID mismatch")
	}
	if strings.TrimSpace(data.MessageType) != "private" || data.GroupID != 0 {
		return errors.New("quote is not a private message")
	}
	if data.SelfID != 0 && data.SelfID != source.SelfID {
		return errors.New("quote bot identity mismatch")
	}
	sender := data.Sender.UserID
	if sender == 0 {
		sender = data.UserID
	}
	// OneBot user_id describes the sender. Reject inconsistent metadata rather
	// than interpreting a conflicting user_id as proof of the destination.
	if data.UserID != 0 && data.UserID != sender {
		return errors.New("quote sender metadata mismatch")
	}
	switch sender {
	case source.UserID:
		if data.TargetID != 0 && data.TargetID != source.SelfID {
			return errors.New("quote recipient mismatch")
		}
	case source.SelfID:
		if data.TargetID != source.UserID {
			return errors.New("bot quote peer unavailable or mismatched")
		}
	default:
		return errors.New("quote belongs to another sender")
	}
	return nil
}
