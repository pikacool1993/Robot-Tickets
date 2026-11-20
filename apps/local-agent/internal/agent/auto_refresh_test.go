package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/userkey"
	"sync/atomic"
	"testing"
	"time"
)

type refreshProtocolStub struct {
	*orderProtocolStub
	key   userkey.Material
	codes atomic.Int32
}

func (p *refreshProtocolStub) MiniProgramCode(context.Context, int64, protocols.Binding, string) (string, error) {
	return fmt.Sprintf("code-%d", p.codes.Add(1)), nil
}
func (p *refreshProtocolStub) UserEncryptionKey(context.Context, int64, protocols.Binding, string) (userkey.Material, error) {
	return p.key, nil
}

type refreshAPIStub struct {
	*stockStub
	call func(context.Context, business.Session, func(context.Context) (string, error), func(business.Session)) (business.Session, []business.Person, error)
}

func (a *refreshAPIStub) RefreshWithLogin(ctx context.Context, _ business.Config, _ string, session business.Session, get func(context.Context) (string, error), _ func(string, string), login func(business.Session)) (business.Session, []business.Person, error) {
	return a.call(ctx, session, get, login)
}

func autoRefreshFixture(t *testing.T, n int) (*Service, []*inventoryRun, *refreshProtocolStub) {
	t.Helper()
	s, runs, key := orderFixture(t, n)
	s.autoRefresh = AutoRefreshSettings{Enabled: true, IntervalMinutes: 30}
	s.maintenanceResume = map[int64]bool{}
	s.session.BusinessConfig = business.Config{Ready: true, AppID: business.AppID, DeviceType: "ios", Template: &business.Template{UserAgent: "iPhone", Referer: "https://servicewechat.com/" + business.AppID + "/78/page-frame.html", E: map[string]any{}, S: map[string]any{"platform": "ios"}, V: "240222"}}
	s.state.AuthProtocol = "wechat"
	p := &refreshProtocolStub{orderProtocolStub: s.protocols.(*orderProtocolStub), key: key}
	s.protocols = p
	for _, r := range runs {
		r.status = "stopped"
		r.plan = nil
		r.business.attempted = true
		r.business.refresh.lastSuccess = time.Now().Add(-time.Hour)
		if err := s.tasks.SaveBusiness(r.task.ID, tasks.BusinessData{Account: r.business.binding.Protocol + ":" + r.business.binding.Account.Ref, People: r.people, Selected: r.task.PeopleSelected, Status: "ready", Usable: true}); err != nil {
			t.Fatal(err)
		}
	}
	return s, runs, p
}
func waitRefresh(t *testing.T, s *Service, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		done := condition()
		s.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
func successRefresh(t *testing.T, people []business.Person) func(context.Context, business.Session, func(context.Context) (string, error), func(business.Session)) (business.Session, []business.Person, error) {
	return func(ctx context.Context, session business.Session, get func(context.Context) (string, error), login func(business.Session)) (business.Session, []business.Person, error) {
		if session.Token != "" || session.AuthCode != "" {
			t.Error("automatic refresh reused existing login")
		}
		code, err := get(ctx)
		if err != nil {
			return session, nil, err
		}
		session = business.Session{Token: "fresh-token", UserID: "99", AuthCode: code}
		login(session)
		return session, people, nil
	}
}

func TestAutomaticRefreshFullFlowAndSingleSlot(t *testing.T) {
	s, runs, p := autoRefreshFixture(t, 2)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	s.businessClient = &refreshAPIStub{call: func(ctx context.Context, session business.Session, get func(context.Context) (string, error), login func(business.Session)) (business.Session, []business.Person, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return session, nil, ctx.Err()
		}
		return successRefresh(t, runs[0].people)(ctx, session, get, login)
	}}
	s.mu.Lock()
	s.tickAutoRefreshLocked(time.Now())
	s.tickAutoRefreshLocked(time.Now().Add(time.Minute))
	s.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	select {
	case <-entered:
		t.Fatal("automatic refreshes overlapped")
	default:
	}
	if err := s.RefreshTaskPeople(runs[0].task.ID, true); err == nil {
		t.Fatal("manual refresh overlapped automatic refresh")
	}
	if err := s.StopInventory(runs[0].task.ID); err != nil {
		t.Fatal(err)
	}
	// Release through cancellation-safe channel without waiting for a real timer.
	release <- struct{}{}
	waitRefresh(t, s, func() bool {
		return !runs[0].business.refresh.active && !runs[0].business.refresh.lastSuccess.Before(time.Now().Add(-time.Minute))
	})
	s.mu.Lock()
	if s.maintenanceResume[runs[0].task.ID] || runs[0].status != "stopped" {
		t.Error("idle/stopped task auto-started")
	}
	raw, err := s.store.Get(fmt.Sprintf("business-session.v1.%d", runs[0].task.ID))
	var saved persistedBusinessSession
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.LastFullRefresh.IsZero() || saved.AuthCode != "code-1" || saved.Token != "fresh-token" {
		t.Error("full success not persisted", err)
	}
	s.tickAutoRefreshLocked(s.nextAutoRefresh.Add(-time.Millisecond))
	s.mu.Unlock()
	select {
	case <-entered:
		t.Fatal("stagger not enforced")
	default:
	}
	if p.codes.Load() != 1 {
		t.Fatal("duplicate login code fetch")
	}
}

func TestAutoRefreshEligibilityBoundaries(t *testing.T) {
	for _, status := range []string{"ready", "pending_order", "submitting", "stopping", "order_created", "order_verifying", "order_blocked", "order_review"} {
		t.Run(status, func(t *testing.T) {
			s, runs, _ := autoRefreshFixture(t, 1)
			r := runs[0]
			r.status = status
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.autoRefreshDeferralLocked(r.task.ID, r.business) == "" {
				t.Fatal("protected order state allowed automatic refresh")
			}
		})
	}
	s, runs, p := autoRefreshFixture(t, 1)
	r := runs[0]
	s.inventory.synced = true
	s.inventory.anchor = time.Now()
	s.inventory.wall = time.Now()
	r.status = "waiting"
	r.early = true
	r.match.SaleAt = s.calibratedNowLocked().Add(29 * time.Second)
	if s.autoRefreshDeferralLocked(r.task.ID, r.business) == "" {
		t.Fatal("opening guard missing")
	}
	r.match.SaleAt = s.calibratedNowLocked().Add(time.Minute)
	if reason := s.autoRefreshDeferralLocked(r.task.ID, r.business); reason != "" {
		t.Fatal("far opening incorrectly blocked", reason)
	}
	r.early = false
	r.status = "order_cooldown"
	if reason := s.autoRefreshDeferralLocked(r.task.ID, r.business); reason != "" {
		t.Fatal("cooldown should admit maintenance", reason)
	}
	r.business.refresh.authorizationPending = true
	if s.autoRefreshDeferralLocked(r.task.ID, r.business) == "" {
		t.Fatal("authorization overlaps")
	}
	r.business.refresh.authorizationPending = false
	binding := p.bindings[r.task.ID]
	binding.Status = "expired"
	p.bindings[r.task.ID] = binding
	if s.autoRefreshDeferralLocked(r.task.ID, r.business) == "" {
		t.Fatal("offline grace triggered a destructive refresh")
	}
}

func TestAutoRefreshRetryDisableAndSettingsPersistence(t *testing.T) {
	s, runs, _ := autoRefreshFixture(t, 1)
	b := runs[0].business
	now := time.Now()
	last := b.refresh.lastSuccess
	s.businessClient = &refreshAPIStub{call: func(context.Context, business.Session, func(context.Context) (string, error), func(business.Session)) (business.Session, []business.Person, error) {
		return business.Session{}, nil, errors.New("offline")
	}}
	s.mu.Lock()
	s.tickAutoRefreshLocked(now)
	s.mu.Unlock()
	waitRefresh(t, s, func() bool { return b.refresh.failures == 1 })
	s.mu.Lock()
	retry := b.refresh.retryAt
	if !b.refresh.lastSuccess.Equal(last) || retry.IsZero() || runs[0].status != "maintenance_failed" {
		t.Error("failure advanced success or lost failure state")
	}
	s.tickAutoRefreshLocked(retry.Add(-time.Millisecond))
	if b.refresh.active {
		t.Error("retried before deadline")
	}
	s.mu.Unlock()
	if err := s.SaveAutoRefreshSettings(AutoRefreshSettings{Enabled: false, IntervalMinutes: 10}); err != nil {
		t.Fatal(err)
	}
	next := &Service{store: s.store}
	if err := next.initAutoRefresh(); err != nil || next.autoRefresh.Enabled || next.autoRefresh.IntervalMinutes != 10 {
		t.Fatal("settings did not survive reload", err)
	}
	s.mu.Lock()
	s.tickAutoRefreshLocked(retry.Add(time.Hour))
	if b.refresh.active {
		t.Error("disabled scheduler retried")
	}
	s.mu.Unlock()
	if err := s.SaveAutoRefreshSettings(AutoRefreshSettings{IntervalMinutes: 0}); err == nil {
		t.Fatal("invalid interval accepted")
	}
}

func TestRefreshCompletionNeedsKeyAndValidPeople(t *testing.T) {
	s, runs, p := autoRefreshFixture(t, 1)
	r := runs[0]
	b := r.business
	last := b.refresh.lastSuccess
	b.refresh.full = true
	b.refresh.active = true
	b.refreshReady = true
	b.keys.Clear()
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	s.mu.Unlock()
	if !b.refresh.lastSuccess.Equal(last) || !b.refresh.active {
		t.Fatal("incomplete refresh counted as success")
	}
	data, _ := s.tasks.Business(r.task.ID)
	data.People = nil
	if err := s.tasks.SaveBusiness(r.task.ID, data); err != nil {
		t.Fatal(err)
	}
	b.keys.Set(p.key)
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	s.mu.Unlock()
	if !b.refresh.blocked || b.refresh.active || !b.refresh.lastSuccess.Equal(last) || s.maintenanceResume[r.task.ID] {
		t.Fatal("invalid refreshed people resumed purchase or advanced success")
	}
}

func TestRefreshCooldownDoesNotResumeEarlyAndStopCancelsIntent(t *testing.T) {
	s, runs, p := autoRefreshFixture(t, 1)
	r := runs[0]
	b := r.business
	r.status = "order_cooldown"
	r.retryAt = s.calibratedNowLocked().Add(250 * time.Millisecond)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	s.businessClient = &refreshAPIStub{call: func(ctx context.Context, session business.Session, get func(context.Context) (string, error), login func(business.Session)) (business.Session, []business.Person, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return session, nil, ctx.Err()
		}
		return successRefresh(t, r.people)(ctx, session, get, login)
	}}
	s.mu.Lock()
	s.tickAutoRefreshLocked(time.Now())
	notBefore := b.resumeNotBefore
	s.mu.Unlock()
	<-entered
	if notBefore.IsZero() {
		t.Fatal("maintenance forgot cooldown")
	}
	release <- struct{}{}
	waitRefresh(t, s, func() bool { return b.resumePending })
	s.mu.Lock()
	if r.status != "stopped" || !s.maintenanceResume[r.task.ID] || s.autoRefreshDeferralLocked(r.task.ID, b) == "" {
		t.Error("cooldown was shortened")
	}
	s.mu.Unlock()
	if err := s.StopInventory(r.task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(notBefore) + 10*time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maintenanceResume[r.task.ID] || b.resumePending || r.status != "stopped" {
		t.Fatal("stop did not cancel delayed resume")
	}
	if p.codes.Load() != 1 {
		t.Fatal("unexpected extra login")
	}
}

func TestOnlyFullRefreshResetsScheduleAndIntervalRecomputes(t *testing.T) {
	s, runs, _ := autoRefreshFixture(t, 1)
	r := runs[0]
	b := r.business
	last := b.refresh.lastSuccess
	b.refreshReady = true
	b.refresh.full = false
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	s.mu.Unlock()
	if !b.refresh.lastSuccess.Equal(last) {
		t.Fatal("people-only refresh reset full-refresh timer")
	}
	b.refreshReady = true
	b.refresh.full = true
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	updated := b.refresh.lastSuccess
	s.mu.Unlock()
	if !updated.After(last) {
		t.Fatal("manual full refresh did not reset timer")
	}
	if err := s.SaveAutoRefreshSettings(AutoRefreshSettings{Enabled: true, IntervalMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refreshDueLocked(b).Equal(updated.Add(time.Hour)) {
		t.Fatal("interval change did not recompute from last success")
	}
}
