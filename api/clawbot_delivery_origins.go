package api

import (
	"cornerstone/storage"
	"fmt"
)

// validateDeliveryOrigin is deliberately repeated before generation and every
// chunk. Queue persistence precedes binding, so this also repairs the handoff
// after a crash without trusting a stale producer snapshot.
func (s *ClawBotService) validateDeliveryOrigin(job storage.ClawBotDeliveryJob) (bool, error) {
	if job.Kind != "reminder" && job.Kind != "greeting" {
		return true, nil
	}
	if s == nil || s.handler == nil {
		return false, fmt.Errorf("delivery producer unavailable")
	}
	if job.Kind == "reminder" {
		if job.Reminder == nil {
			return false, fmt.Errorf("missing reminder snapshot")
		}
		service := s.handler.reminderService
		if service == nil || service.manager == nil {
			return false, fmt.Errorf("reminder manager unavailable")
		}
		return service.manager.BindDelivery(*job.Reminder, job.ID, service.now())
	}
	if job.Greeting == nil {
		return false, fmt.Errorf("missing greeting snapshot")
	}
	service := s.handler.idleGreetingService
	if service == nil || service.manager == nil || s.handler.configManager == nil || s.handler.chatManager == nil {
		return false, fmt.Errorf("greeting dependencies unavailable")
	}
	task := job.Greeting
	cfg := s.handler.configManager.Get()
	now := service.now()
	if !cfg.IdleGreeting.Enabled || (service.maxFireLag > 0 && now.After(task.DueAt.Add(service.maxFireLag))) ||
		!isIdleGreetingFireTimeAllowed(now, cfg.IdleGreeting, loadIdleGreetingLocation(cfg.TimeZone)) {
		return false, nil
	}
	session, ok := s.handler.chatManager.GetSession(task.SessionID)
	if !ok || session == nil || hasNewerUserMessage(session, task.LastUserAt) {
		return false, nil
	}
	// Inputs are durable before their history projection. Do not let a greeting
	// jump ahead of a newly received message merely because generation is queued.
	s.stateMu.Lock()
	newer := false
	if s.state.Delivery != nil {
		for _, incoming := range s.state.Delivery.Jobs {
			if incoming.ID == job.ID || (incoming.SessionID != task.SessionID && incoming.UserID != task.Target.UserID) {
				continue
			}
			// UpdatedAt also advances on replies/receipts; it is not user activity.
			lastInput := incoming.CreatedAt
			for _, at := range incoming.InputTimes {
				if at.After(lastInput) {
					lastInput = at
				}
			}
			if len(incoming.Inputs) > 0 && lastInput.After(task.LastUserAt) {
				newer = true
				break
			}
			for _, message := range incoming.InputMessages {
				if message.Role == "user" && message.Timestamp.After(task.LastUserAt) {
					newer = true
					break
				}
			}
			if newer {
				break
			}
		}
	}
	s.stateMu.Unlock()
	if newer {
		return false, nil
	}
	return service.manager.BindDelivery(*task, job.ID, now)
}

func (s *ClawBotService) finishDeliveryOrigin(job storage.ClawBotDeliveryJob, cancelled bool) error {
	if job.Kind != "reminder" && job.Kind != "greeting" {
		return nil
	}
	if s == nil || s.handler == nil {
		return fmt.Errorf("delivery producer unavailable")
	}
	if job.Kind == "reminder" {
		service := s.handler.reminderService
		if job.Reminder == nil || service == nil || service.manager == nil {
			return fmt.Errorf("reminder producer unavailable")
		}
		return service.manager.CompleteDelivery(*job.Reminder, job.ID, cancelled, service.now())
	}
	service := s.handler.idleGreetingService
	if job.Greeting == nil || service == nil || service.manager == nil {
		return fmt.Errorf("greeting producer unavailable")
	}
	return service.manager.CompleteDelivery(*job.Greeting, job.ID)
}

func deliveryOriginKey(kind, id string, revision uint64) string {
	if revision == 0 {
		return kind + ":" + id
	}
	return fmt.Sprintf("%s:%s:r%d", kind, id, revision)
}
