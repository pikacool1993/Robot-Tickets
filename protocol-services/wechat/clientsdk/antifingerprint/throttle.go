package antifingerprint

import (
	"sync"
	"time"

	srand "wechatdll/clientsdk/securerandom"
)

type RequestThrottler struct {
	mu             sync.Mutex
	lastRequest    time.Time
	minInterval    time.Duration
	baseInterval   time.Duration
	maxInterval    time.Duration
	consecutiveErr int
	backoffUntil   time.Time
	sessionStart   time.Time
	requestCount   int
}

func NewRequestThrottler(minMs, baseMs, maxMs int) *RequestThrottler {
	return &RequestThrottler{
		minInterval:  time.Duration(minMs) * time.Millisecond,
		baseInterval: time.Duration(baseMs) * time.Millisecond,
		maxInterval:  time.Duration(maxMs) * time.Millisecond,
		sessionStart: time.Now(),
	}
}

func (t *RequestThrottler) Wait() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.backoffUntil.IsZero() && time.Now().Before(t.backoffUntil) {
		wait := time.Until(t.backoffUntil)
		time.Sleep(wait)
		t.backoffUntil = time.Time{}
	}

	elapsed := time.Since(t.lastRequest)
	delay := t.calculateDelay()

	if elapsed < delay {
		time.Sleep(delay - elapsed)
	}

	t.lastRequest = time.Now()
	t.requestCount++
}

func (t *RequestThrottler) calculateDelay() time.Duration {
	base := t.baseInterval

	if t.consecutiveErr > 0 {
		backoff := base * time.Duration(1<<uint(min(t.consecutiveErr, 5)))
		if backoff > t.maxInterval {
			backoff = t.maxInterval
		}
		base = backoff
	}

	jitter := srand.JitterMs(int(base.Milliseconds()), 15)
	if jitter < t.minInterval {
		jitter = t.minInterval
	}
	return jitter
}

func (t *RequestThrottler) OnSuccess() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.consecutiveErr = 0
}

func (t *RequestThrottler) OnError() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.consecutiveErr++
}

func (t *RequestThrottler) OnRiskDetected() {
	t.mu.Lock()
	defer t.mu.Unlock()
	cooldown := srand.HumanDelay(30000, 120000)
	t.backoffUntil = time.Now().Add(cooldown)
}

func (t *RequestThrottler) RequestCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requestCount
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type SessionManager struct {
	mu             sync.Mutex
	loginTime      time.Time
	lastHeartbeat  time.Time
	lastSyncTime   time.Time
	heartbeatCount int
	syncCount      int
	isActive       bool
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		loginTime: time.Now(),
		isActive:  true,
	}
}

func (s *SessionManager) NextHeartbeatInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	baseInterval := 300 * time.Second

	s.heartbeatCount++

	jitter := srand.JitterMs(int(baseInterval.Milliseconds()), 10)
	s.lastHeartbeat = time.Now()
	return jitter
}

func (s *SessionManager) NextSyncInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	baseInterval := 30 * time.Second
	s.syncCount++

	if s.syncCount > 10 {
		baseInterval = 60 * time.Second
	}
	if s.syncCount > 50 {
		baseInterval = 120 * time.Second
	}

	jitter := srand.JitterMs(int(baseInterval.Milliseconds()), 20)
	s.lastSyncTime = time.Now()
	return jitter
}

func (s *SessionManager) ShouldSendHeartbeat() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastHeartbeat.IsZero() {
		return true
	}

	elapsed := time.Since(s.lastHeartbeat)
	interval := s.NextHeartbeatInterval()
	return elapsed >= interval
}

func (s *SessionManager) OnLogin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginTime = time.Now()
	s.heartbeatCount = 0
	s.syncCount = 0
	s.isActive = true
}

func (s *SessionManager) OnDisconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isActive = false
}

func (s *SessionManager) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isActive
}

func (s *SessionManager) SessionDuration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.loginTime)
}

type RiskDetector struct {
	mu             sync.Mutex
	errorCount     int
	lastErrorTime  time.Time
	banRiskLevel   int
	consecutiveOK  int
	totalRequests  int
	totalErrors    int
}

func NewRiskDetector() *RiskDetector {
	return &RiskDetector{}
}

func (r *RiskDetector) OnResponse(retCode int32) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalRequests++

	switch {
	case retCode == -2008:
		r.banRiskLevel = 100
		r.errorCount++
	case retCode == -13:
		r.banRiskLevel = min(r.banRiskLevel+30, 100)
		r.errorCount++
	case retCode == -102:
		r.banRiskLevel = min(r.banRiskLevel+20, 100)
		r.errorCount++
	case retCode == -301:
		r.banRiskLevel = min(r.banRiskLevel+10, 100)
		r.errorCount++
	case retCode < 0:
		r.errorCount++
		r.banRiskLevel = min(r.banRiskLevel+5, 100)
	default:
		r.consecutiveOK++
		r.banRiskLevel = max(r.banRiskLevel-5, 0)
	}

	r.lastErrorTime = time.Now()
}

func (r *RiskDetector) RiskLevel() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.banRiskLevel
}

func (r *RiskDetector) ShouldBackoff() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.banRiskLevel > 50
}

func (r *RiskDetector) ShouldSwitchStrategy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.banRiskLevel > 70
}

func (r *RiskDetector) ErrorRate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totalRequests == 0 {
		return 0
	}
	return float64(r.totalErrors) / float64(r.totalRequests)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	GlobalThrottler    *RequestThrottler
	GlobalSession      *SessionManager
	GlobalRiskDetector *RiskDetector
	onceInit           sync.Once
)

func InitGlobal() {
	onceInit.Do(func() {
		GlobalThrottler = NewRequestThrottler(500, 2000, 30000)
		GlobalSession = NewSessionManager()
		GlobalRiskDetector = NewRiskDetector()
	})
}
