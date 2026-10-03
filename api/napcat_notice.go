package api

import "encoding/json"

// A double-tap poke is a notice, not a message segment. Only a private poke
// addressed to this bot is conversational input. Requests are deliberately not
// auto-approved, and group notices must never enter a private conversation.
func normalizeNapCatPrivateNotice(event napCatMessageEvent) napCatMessageEvent {
	if event.PostType != "notice" || event.NoticeType != "notify" || event.SubType != "poke" || event.GroupID != 0 || event.SelfID == 0 || event.TargetID != event.SelfID {
		return event
	}
	actor := event.SenderID
	if actor == 0 {
		actor = event.UserID
	}
	if actor == 0 || actor == event.SelfID {
		return event
	}
	event.PostType, event.MessageType, event.UserID = "message", "private", actor
	event.Message = json.RawMessage(`[{"type":"poke","data":{}}]`)
	return event
}
