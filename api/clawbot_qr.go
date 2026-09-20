package api

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func validateClawBotBaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid ClawBot base URL")
	}
	return nil
}

func sameClawBotOrigin(left, right string) bool {
	a, errA := url.Parse(strings.TrimSpace(left))
	b, errB := url.Parse(strings.TrimSpace(right))
	return errA == nil && errB == nil && a.Host != "" && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// Redirects are data from the login server, not administrator-supplied URLs.
// Never forward QR values or verification codes to arbitrary hosts.
func trustedClawBotRedirect(raw string) bool {
	if validateClawBotBaseURL(raw) != nil {
		return false
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && u.Port() == "" && (host == "weixin.qq.com" || strings.HasSuffix(host, ".weixin.qq.com"))
}

func validClawBotVerifyCode(code string) bool {
	if len(code) > 32 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *ClawBotService) PollQRCode(ctx context.Context, sessionID string, verifyCodes ...string) (*ClawBotQRCodePollResponse, error) {
	// Serialize polls so concurrent submissions cannot bypass attempt limits or
	// overwrite a newly confirmed credential with an older response.
	s.qrMu.Lock()
	defer s.qrMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.cleanupState()
	s.mu.RLock()
	session, ok := s.qrSessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return &ClawBotQRCodePollResponse{Status: "expired"}, nil
	}

	code := ""
	if len(verifyCodes) > 0 {
		code = strings.TrimSpace(verifyCodes[0])
	}
	if !validClawBotVerifyCode(code) {
		return nil, fmt.Errorf("invalid verification code")
	}
	if code != "" {
		session.VerifyAttempts++
		if session.VerifyAttempts > 5 {
			s.deleteQRCodeSession(sessionID)
			return &ClawBotQRCodePollResponse{Status: "verify_code_blocked"}, nil
		}
		session.VerifyCode = code
		// Record attempts before the request, including failed requests.
		s.mu.Lock()
		s.qrSessions[sessionID] = session
		s.mu.Unlock()
	}

	resp, err := s.client.GetQRCodeStatus(ctx, session.BaseURL, session.QRCode, session.VerifyCode)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &ClawBotQRCodePollResponse{Status: strings.TrimSpace(resp.Status)}
	switch result.Status {
	case "expired", "verify_code_blocked":
		s.deleteQRCodeSession(sessionID)
		return result, nil
	case "scaned_but_redirect":
		redirect := "https://" + strings.TrimSpace(resp.RedirectHost)
		u, _ := url.Parse(redirect)
		if !trustedClawBotRedirect(redirect) || u.Path != "" || session.Redirects >= 3 {
			s.deleteQRCodeSession(sessionID)
			return nil, fmt.Errorf("invalid or excessive ClawBot QR redirect")
		}
		session.BaseURL = redirect
		session.Redirects++
	case "need_verifycode", "scaned":
		session.VerifyCode = ""
	case "binded_redirect":
		cfg := s.handler.configManager.GetClawBotConfig()
		s.mu.RLock()
		stale := s.staleToken != "" && s.staleToken == strings.TrimSpace(cfg.BotToken)
		s.mu.RUnlock()
		if stale || session.LocalBotToken == "" || session.LocalBotToken != strings.TrimSpace(cfg.BotToken) {
			s.deleteQRCodeSession(sessionID)
			return nil, fmt.Errorf("ClawBot binding has no reusable local credential; start a new QR login")
		}
		s.deleteQRCodeSession(sessionID)
		result.Settings, err = s.GetSettings()
		return result, err
	case "confirmed":
		if strings.TrimSpace(resp.BotToken) == "" {
			return nil, fmt.Errorf("ClawBot login returned an empty bot token")
		}
		baseURL := strings.TrimSpace(resp.BaseURL)
		if baseURL == "" {
			baseURL = session.BaseURL
		}
		if validateClawBotBaseURL(baseURL) != nil || (!sameClawBotOrigin(baseURL, session.BaseURL) && !trustedClawBotRedirect(baseURL)) {
			return nil, fmt.Errorf("untrusted ClawBot login base URL")
		}
		cfg := s.handler.configManager.GetClawBotConfig()
		cfg.BaseURL = baseURL
		cfg.BotToken = strings.TrimSpace(resp.BotToken)
		cfg.ILinkUserID = strings.TrimSpace(resp.ILinkUserID)
		cfg.ILinkBotID = strings.TrimSpace(resp.ILinkBotID)
		cfg.GetUpdatesBuf = ""
		if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.staleToken = ""
		s.lastError = ""
		// Other pending logins must not later replace this confirmed account.
		s.qrSessions = make(map[string]clawBotQRCodeSession)
		s.mu.Unlock()
		s.ApplyCurrentConfig()
		result.Settings, err = s.GetSettings()
		return result, err
	}
	s.mu.Lock()
	s.qrSessions[sessionID] = session
	s.mu.Unlock()
	return result, nil
}

func (s *ClawBotService) deleteQRCodeSession(sessionID string) {
	s.mu.Lock()
	delete(s.qrSessions, sessionID)
	s.mu.Unlock()
}
