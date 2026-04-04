package exacttime

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceNowUsesOffsets(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	service := newService(
		Config{
			Server:       DefaultServer,
			Enabled:      true,
			SyncInterval: DefaultSyncInterval,
			Timeout:      DefaultTimeout,
			ManualOffset: 500 * time.Millisecond,
		},
		nil,
		func() time.Time { return base },
	)

	service.mu.Lock()
	service.ntpOffset = 2 * time.Second
	service.mu.Unlock()

	got := service.Now()
	want := base.Add(2500 * time.Millisecond)
	if !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestServiceSyncFailureKeepsPreviousOffset(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	service := newService(
		Config{
			Server:       DefaultServer,
			Enabled:      true,
			SyncInterval: DefaultSyncInterval,
			Timeout:      DefaultTimeout,
		},
		func(server string, timeout time.Duration) (time.Duration, time.Duration, error) {
			return 0, 0, errors.New("network down")
		},
		func() time.Time { return base },
	)

	service.mu.Lock()
	service.ntpOffset = 3 * time.Second
	service.rtt = 120 * time.Millisecond
	service.lastSuccess = true
	service.message = "ntp sync succeeded"
	service.mu.Unlock()

	err := service.Sync()
	if err == nil {
		t.Fatal("Sync() error = nil, want error")
	}

	status := service.Status()
	if status.Offset != 3*time.Second {
		t.Fatalf("Status().Offset = %v, want %v", status.Offset, 3*time.Second)
	}
	if status.RTT != 120*time.Millisecond {
		t.Fatalf("Status().RTT = %v, want %v", status.RTT, 120*time.Millisecond)
	}
	if status.LastSuccess {
		t.Fatal("Status().LastSuccess = true, want false")
	}
	if !status.LastSyncAt.Equal(base) {
		t.Fatalf("Status().LastSyncAt = %v, want %v", status.LastSyncAt, base)
	}
	if !strings.Contains(status.Message, "network down") {
		t.Fatalf("Status().Message = %q, want contains %q", status.Message, "network down")
	}

	gotNow := service.Now()
	wantNow := base.Add(3 * time.Second)
	if !gotNow.Equal(wantNow) {
		t.Fatalf("Now() = %v, want %v", gotNow, wantNow)
	}
}

func TestServiceSyncFailureWithoutPreviousSuccessFallsBackSafely(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	service := newService(
		Config{
			Server:       DefaultServer,
			Enabled:      true,
			SyncInterval: DefaultSyncInterval,
			Timeout:      DefaultTimeout,
			ManualOffset: 750 * time.Millisecond,
		},
		func(server string, timeout time.Duration) (time.Duration, time.Duration, error) {
			return 0, 0, errors.New("timeout")
		},
		func() time.Time { return base },
	)

	err := service.Sync()
	if err == nil {
		t.Fatal("Sync() error = nil, want error")
	}

	status := service.Status()
	if status.Offset != 750*time.Millisecond {
		t.Fatalf("Status().Offset = %v, want %v", status.Offset, 750*time.Millisecond)
	}
	if status.LastSuccess {
		t.Fatal("Status().LastSuccess = true, want false")
	}

	got := service.Now()
	want := base.Add(750 * time.Millisecond)
	if !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}
