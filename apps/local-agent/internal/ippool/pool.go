package ippool

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"robot/apps/local-agent/internal/store"
	"strconv"
	"strings"
	"sync"
	"time"
)

type taskKey struct{}

func WithTask(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, taskKey{}, id)
}

type Entry struct {
	ID         string `json:"id"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	Expires    string `json:"expires"`
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	Bound      int    `json:"bound"`
	TestedAt   string `json:"testedAt"`
	TestMS     int64  `json:"testMS"`
	TestResult string `json:"testResult"`
}
type Pool struct {
	Direct        bool    `json:"direct"`
	IncludeDirect bool    `json:"includeDirect"`
	Threshold     int     `json:"threshold"`
	CooldownSec   int     `json:"cooldownSec"`
	Entries       []Entry `json:"entries"`
}
type Binding struct {
	Exit       string `json:"exit"`
	Failures   int    `json:"failures"`
	Generation uint64 `json:"-"`
}
type View struct {
	Pools    map[string]Pool              `json:"pools"`
	Bindings map[int64]map[string]Binding `json:"bindings"`
}
type Manager struct {
	mu         sync.Mutex
	db         *store.Store
	pools      map[string]Pool
	bindings   map[int64]map[string]Binding
	cool       map[string]time.Time
	transports map[string]*http.Transport
	generation uint64
}

func New(db *store.Store) (*Manager, error) {
	m := &Manager{db: db, pools: map[string]Pool{}, bindings: map[int64]map[string]Binding{}, cool: map[string]time.Time{}, transports: map[string]*http.Transport{}}
	for _, k := range []string{"N1", "N5"} {
		m.pools[k] = Pool{Threshold: 3, CooldownSec: 60, Entries: []Entry{}}
	}
	raw, err := db.Get("ip-pools.v1")
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &m.pools); err != nil {
			return nil, errors.New("IP池配置无法读取")
		}
	}
	return m, nil
}
func validKind(k string) bool { return k == "N1" || k == "N5" }
func (m *Manager) save(next map[string]Pool) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = m.db.Put("ip-pools.v1", raw); err != nil {
		return errors.New("IP池保存失败")
	}
	m.pools = next
	return nil
}
func (m *Manager) next() map[string]Pool {
	n := map[string]Pool{}
	for k, p := range m.pools {
		p.Entries = append([]Entry{}, p.Entries...)
		n[k] = p
	}
	return n
}
func expiry(e Entry) time.Time {
	d, _ := time.ParseInLocation("2006-01-02", e.Expires, time.FixedZone("CST", 8*3600))
	return d.AddDate(0, 0, 1)
}
func (m *Manager) usable(k string, e Entry) bool {
	return e.Enabled && time.Now().Before(expiry(e)) && !time.Now().Before(m.cool[k+e.ID])
}
func (m *Manager) candidates(k string) []string {
	p := m.pools[k]
	if p.Direct {
		return []string{"本机直连"}
	}
	out := []string{}
	for _, e := range p.Entries {
		if m.usable(k, e) {
			out = append(out, e.ID)
		}
	}
	if p.IncludeDirect || len(out) == 0 {
		out = append(out, "本机直连")
	}
	return out
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func random(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}
func (m *Manager) choose(id int64, k string, shuffle bool) Binding {
	if m.bindings[id] == nil {
		m.bindings[id] = map[string]Binding{}
	}
	old := m.bindings[id][k]
	cs := m.candidates(k)
	if !shuffle && contains(cs, old.Exit) {
		return old
	}
	if shuffle && len(cs) > 1 {
		filtered := []string{}
		for _, x := range cs {
			if x != old.Exit {
				filtered = append(filtered, x)
			}
		}
		cs = filtered
	}
	chosen := cs[0]
	if shuffle {
		chosen = cs[random(len(cs))]
	} else {
		best := int(^uint(0) >> 1)
		for _, x := range cs {
			count := 0
			for tid, bs := range m.bindings {
				if tid != id && bs[k].Exit == x {
					count++
				}
			}
			if count < best {
				best = count
				chosen = x
			}
		}
	}
	m.generation++
	b := Binding{Exit: chosen, Generation: m.generation}
	m.bindings[id][k] = b
	return b
}
func (m *Manager) Snapshot(ids []int64) View {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		for _, k := range []string{"N1", "N5"} {
			if _, ok := m.bindings[id][k]; ok {
				m.choose(id, k, false)
			}
		}
	}
	v := View{Pools: m.next(), Bindings: map[int64]map[string]Binding{}}
	for id, bs := range m.bindings {
		v.Bindings[id] = map[string]Binding{}
		for k, b := range bs {
			v.Bindings[id][k] = b
		}
	}
	for k, p := range v.Pools {
		for i := range p.Entries {
			e := &p.Entries[i]
			e.Password = ""
			e.Bound = 0
			e.Status = "可用"
			if !e.Enabled {
				e.Status = "停用"
			} else if !time.Now().Before(expiry(*e)) {
				e.Status = "过期"
			} else if time.Now().Before(m.cool[k+e.ID]) {
				e.Status = "冷却"
			}
			for _, bs := range m.bindings {
				if bs[k].Exit == e.ID {
					e.Bound++
				}
			}
		}
		v.Pools[k] = p
	}
	return v
}
func (m *Manager) Settings(k string, direct, include bool, threshold, cooldown int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validKind(k) || threshold < 1 || threshold > 100 || cooldown < 1 || cooldown > 86400 {
		return errors.New("阈值须为1–100，冷却须为1–86400秒")
	}
	n := m.next()
	p := n[k]
	p.Direct = direct
	p.IncludeDirect = include
	p.Threshold = threshold
	p.CooldownSec = cooldown
	n[k] = p
	return m.save(n)
}
func parse(line string) (Entry, error) {
	v := strings.Split(strings.TrimSpace(line), "|")
	if len(v) != 5 {
		return Entry{}, errors.New("须为 IP|端口|用户名|密码|到期日期")
	}
	for i := range v {
		v[i] = strings.TrimSpace(v[i])
	}
	port, err := strconv.Atoi(v[1])
	if net.ParseIP(v[0]) == nil || err != nil || port < 1 || port > 65535 {
		return Entry{}, errors.New("IP或端口无效")
	}
	if _, err = time.Parse("2006-01-02", v[4]); err != nil {
		return Entry{}, errors.New("日期须为 YYYY-MM-DD")
	}
	return Entry{ID: net.JoinHostPort(v[0], strconv.Itoa(port)), IP: v[0], Port: port, User: v[2], Password: v[3], Expires: v[4], Enabled: true}, nil
}
func (m *Manager) Import(k, raw string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validKind(k) {
		return nil, errors.New("接口无效")
	}
	n := m.next()
	p := n[k]
	seen := map[string]bool{}
	for _, e := range p.Entries {
		seen[e.ID] = true
	}
	notes := []string{}
	for i, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, err := parse(line)
		if err != nil {
			notes = append(notes, fmt.Sprintf("第%d行：%s", i+1, err))
			continue
		}
		if seen[e.ID] {
			notes = append(notes, fmt.Sprintf("第%d行：重复，已跳过", i+1))
			continue
		}
		seen[e.ID] = true
		p.Entries = append(p.Entries, e)
	}
	n[k] = p
	return notes, m.save(n)
}
func (m *Manager) Entry(k, id, action, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validKind(k) {
		return errors.New("接口无效")
	}
	n := m.next()
	p := n[k]
	for i, e := range p.Entries {
		if e.ID != id {
			continue
		}
		switch action {
		case "delete":
			p.Entries = append(p.Entries[:i], p.Entries[i+1:]...)
		case "toggle":
			p.Entries[i].Enabled = !e.Enabled
		case "edit":
			v, err := parse(line)
			if err != nil {
				return err
			}
			for _, other := range p.Entries {
				if other.ID == v.ID && other.ID != id {
					return errors.New("IP和端口重复")
				}
			}
			if v.Password == "" {
				v.Password = e.Password
			}
			v.Enabled = e.Enabled
			p.Entries[i] = v
		default:
			return errors.New("操作无效")
		}
		n[k] = p
		if err := m.save(n); err != nil {
			return err
		}
		for taskID, bs := range m.bindings {
			if bs[k].Exit == id {
				m.generation++
				b := bs[k]
				b.Generation = m.generation
				b.Failures = 0
				m.bindings[taskID][k] = b
			}
		}
		for key, t := range m.transports {
			if strings.HasPrefix(key, k+id) {
				t.CloseIdleConnections()
				delete(m.transports, key)
			}
		}
		return nil
	}
	return errors.New("代理不存在")
}
func (m *Manager) Shuffle(ids []int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := 0
	for _, id := range ids {
		hit := false
		for _, k := range []string{"N1", "N5"} {
			old := m.choose(id, k, false)
			b := m.choose(id, k, true)
			hit = hit || b.Exit != old.Exit
		}
		if hit {
			changed++
		}
	}
	return fmt.Sprintf("已切换 %d 个任务，%d 个任务无其他可用出口", changed, len(ids)-changed)
}
func (m *Manager) Release(id int64) { m.mu.Lock(); defer m.mu.Unlock(); delete(m.bindings, id) }
func proxyURL(e Entry) *url.URL {
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(e.IP, strconv.Itoa(e.Port)), User: url.UserPassword(e.User, e.Password)}
}
func transport(e Entry) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyURL(proxyURL(e))
	t.MaxIdleConnsPerHost = 8
	return t
}
func (m *Manager) RoundTrip(req *http.Request, base http.RoundTripper) (*http.Response, error) {
	k := ""
	switch req.URL.Hostname() {
	case "fccdn1.k4n.cc":
		k = "N1"
	case "fccdn5.k4n.cc":
		k = "N5"
	default:
		return base.RoundTrip(req)
	}
	id, _ := req.Context().Value(taskKey{}).(int64)
	if id <= 0 {
		return base.RoundTrip(req)
	}
	m.mu.Lock()
	b := m.choose(id, k, false)
	tr := base
	orderIP := "direct"
	for _, e := range m.pools[k].Entries {
		if e.ID == b.Exit {
			orderIP = e.IP
			key := k + e.ID + e.User + e.Password
			t := m.transports[key]
			if t == nil {
				t = transport(e)
				m.transports[key] = t
			}
			tr = t
			break
		}
	}
	m.mu.Unlock()
	captureExit(req.Context(), orderIP)
	res, err := tr.RoundTrip(req)
	if err != nil {
		m.record(id, k, b, 0, err)
		return res, safeError{err}
	}
	if res.Body == nil {
		m.record(id, k, b, res.StatusCode, nil)
	} else {
		res.Body = &trackedBody{ReadCloser: res.Body, done: func(e error) { m.record(id, k, b, res.StatusCode, e) }}
	}
	return res, nil
}

type safeError struct{ err error }

func (e safeError) Error() string { return "业务网络连接失败" }
func (e safeError) Unwrap() error { return e.err }
func (e safeError) Timeout() bool {
	var t interface{ Timeout() bool }
	return errors.As(e.err, &t) && t.Timeout()
}

type trackedBody struct {
	io.ReadCloser
	once sync.Once
	done func(error)
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil {
		result := e
		if e == io.EOF {
			result = nil
		}
		b.once.Do(func() { b.done(result) })
		if result != nil {
			return n, safeError{e}
		}
	}
	return n, e
}
func (b *trackedBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.done(e) })
	return e
}
func (m *Manager) record(id int64, k string, b Binding, status int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.bindings[id][k]
	if current.Generation != b.Generation {
		return
	}
	failed := err != nil && !errors.Is(err, context.Canceled)
	if k == "N5" {
		var timeout interface{ Timeout() bool }
		failed = errors.As(err, &timeout) && timeout.Timeout()
	}
	if k == "N1" && status == 407 {
		failed = true
	}
	if failed {
		current.Failures++
	} else if err == nil {
		current.Failures = 0
	}
	m.bindings[id][k] = current
	if failed && current.Failures >= m.pools[k].Threshold && b.Exit != "本机直连" {
		m.cool[k+b.Exit] = time.Now().Add(time.Duration(m.pools[k].CooldownSec) * time.Second)
	}
}
func (m *Manager) Test(ctx context.Context, k, id string) error {
	m.mu.Lock()
	var entry *Entry
	for _, e := range m.pools[k].Entries {
		if e.ID == id {
			copy := e
			entry = &copy
		}
	}
	m.mu.Unlock()
	if entry == nil {
		return errors.New("代理不存在")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	host := "fccdn1.k4n.cc"
	if k == "N5" {
		host = "fccdn5.k4n.cc"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+host+"/", nil)
	t := transport(*entry)
	defer t.CloseIdleConnections()
	started := time.Now()
	res, err := t.RoundTrip(req)
	result := "连接成功"
	if res != nil {
		res.Body.Close()
		if res.StatusCode == 407 {
			result = "代理认证失败"
		}
	}
	if err != nil {
		result = "连接失败"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pools[k]
	for i := range p.Entries {
		if p.Entries[i].ID == id {
			p.Entries[i].TestResult = result
			p.Entries[i].TestMS = time.Since(started).Milliseconds()
			p.Entries[i].TestedAt = time.Now().Format(time.RFC3339)
		}
	}
	m.pools[k] = p
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.transports {
		t.CloseIdleConnections()
	}
}
