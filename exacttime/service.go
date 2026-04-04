package exacttime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/beevik/ntp"
)

const (
	DefaultServer       = "ntp.aliyun.com"
	DefaultSyncInterval = 10 * time.Minute
	DefaultTimeout      = 5 * time.Second
)

type Config struct {
	Server       string
	Enabled      bool
	SyncInterval time.Duration
	Timeout      time.Duration
	ManualOffset time.Duration
}

func DefaultConfig() Config {
	return Config{
		Server:       DefaultServer,
		Enabled:      true,
		SyncInterval: DefaultSyncInterval,
		Timeout:      DefaultTimeout,
		ManualOffset: 0,
	}
}

type Status struct {
	Server      string        `json:"server"`
	Enabled     bool          `json:"enabled"`
	LastSyncAt  time.Time     `json:"last_sync_at"`
	LastSuccess bool          `json:"last_success"`
	Message     string        `json:"message"`
	RTT         time.Duration `json:"rtt"`
	Offset      time.Duration `json:"offset"`
}

type ntpQueryFunc func(server string, timeout time.Duration) (time.Duration, time.Duration, error)
type nowFunc func() time.Time

type Service struct {
	mu sync.RWMutex

	server       string
	enabled      bool
	syncInterval time.Duration
	timeout      time.Duration
	manualOffset time.Duration

	ntpOffset   time.Duration
	lastSyncAt  time.Time
	lastSuccess bool
	message     string
	rtt         time.Duration

	query ntpQueryFunc
	now   nowFunc
}

func New(cfg Config) *Service {
	return newService(cfg, defaultNTPQuery, time.Now)
}

func newService(cfg Config, query ntpQueryFunc, now nowFunc) *Service {
	zeroConfig := cfg == (Config{})
	defaults := DefaultConfig()

	server := strings.TrimSpace(cfg.Server)
	if server == "" {
		server = defaults.Server
	}

	enabled := cfg.Enabled
	if zeroConfig {
		enabled = defaults.Enabled
	}

	syncInterval := cfg.SyncInterval
	if syncInterval <= 0 {
		syncInterval = defaults.SyncInterval
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaults.Timeout
	}

	if query == nil {
		query = defaultNTPQuery
	}
	if now == nil {
		now = time.Now
	}

	message := "ntp sync not completed yet"
	if !enabled {
		message = "ntp sync disabled"
	}

	return &Service{
		server:       server,
		enabled:      enabled,
		syncInterval: syncInterval,
		timeout:      timeout,
		manualOffset: cfg.ManualOffset,
		message:      message,
		query:        query,
		now:          now,
	}
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}

	if s.isEnabled() {
		_ = s.Sync()
	}

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.isEnabled() {
				_ = s.Sync()
			}
		}
	}
}

func (s *Service) Sync() error {
	if s == nil {
		return fmt.Errorf("service is nil")
	}

	s.mu.RLock()
	server := s.server
	timeout := s.timeout
	s.mu.RUnlock()

	offset, rtt, err := s.query(server, timeout)
	attemptAt := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastSyncAt = attemptAt
	if err != nil {
		s.lastSuccess = false
		s.message = "ntp sync failed: " + err.Error()
		return err
	}

	s.ntpOffset = offset
	s.rtt = rtt
	s.lastSuccess = true
	s.message = "ntp sync succeeded"
	return nil
}

func (s *Service) Now() time.Time {
	if s == nil {
		return time.Now()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	base := s.now()
	offset := s.manualOffset
	if s.enabled {
		offset += s.ntpOffset
	}
	return base.Add(offset)
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{
			Server:      DefaultServer,
			Enabled:     false,
			LastSuccess: false,
			Message:     "service is nil",
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	offset := s.manualOffset
	if s.enabled {
		offset += s.ntpOffset
	}

	return Status{
		Server:      s.server,
		Enabled:     s.enabled,
		LastSyncAt:  s.lastSyncAt,
		LastSuccess: s.lastSuccess,
		Message:     s.message,
		RTT:         s.rtt,
		Offset:      offset,
	}
}

func (s *Service) isEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func defaultNTPQuery(server string, timeout time.Duration) (time.Duration, time.Duration, error) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return 0, 0, err
	}
	if err := resp.Validate(); err != nil {
		return 0, 0, err
	}
	return resp.ClockOffset, resp.RTT, nil
}
