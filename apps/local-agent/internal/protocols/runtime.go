package protocols

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"robot/apps/local-agent/internal/store"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrRestartRequired = errors.New("代理配置发生变化，需要确认重启当前实例的协议服务")

type runtimeRecord struct {
	Port, PID    int
	Token, Proxy string
	Created      uint64
}
type Binding struct {
	Protocol string  `json:"protocol"`
	Account  Account `json:"account"`
	Status   string  `json:"status"`
	Paused   bool    `json:"paused"`
}
type Attempt struct {
	ID        string    `json:"id"`
	TaskID    int64     `json:"taskID"`
	Protocol  string    `json:"protocol"`
	Image     string    `json:"image"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expiresAt"`
	scanID    string
	cancel    context.CancelFunc
}
type Status struct {
	Kind     string  `json:"kind"`
	State    string  `json:"state"`
	Address  string  `json:"address"`
	Proxy    string  `json:"proxy"`
	Retries  int     `json:"retries"`
	Message  string  `json:"message"`
	Affected []int64 `json:"affected"`
}
type persisted struct {
	Runtime  map[string]runtimeRecord
	Bindings map[int64]Binding
}
type Manager struct {
	OnTaskLog      func(int64, string, string)
	mu             sync.Mutex
	control        sync.Mutex
	root, instance string
	db             *store.Store
	doc            persisted
	attempts       map[int64]*Attempt
	accountLocks   map[string]*sync.Mutex
	state          map[string]string
	messages       map[string]string
	retries        map[string]int
	manual         map[string]bool
	ctx            context.Context
	cancel         context.CancelFunc
	workCtx        context.Context
	workCancel     context.CancelFunc
	active         string
	wg             sync.WaitGroup
	wake           chan struct{}
	maintaining    bool
}

func New(root, instance string, db *store.Store) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{root: root, instance: instance, db: db, ctx: ctx, cancel: cancel, attempts: map[int64]*Attempt{}, state: map[string]string{}, messages: map[string]string{}, retries: map[string]int{}, manual: map[string]bool{}}
	m.doc = persisted{Runtime: map[string]runtimeRecord{}, Bindings: map[int64]Binding{}}
	m.accountLocks = map[string]*sync.Mutex{}
	m.wake = make(chan struct{}, 1)
	b, err := db.Get("protocols.v1")
	if err != nil {
		cancel()
		return nil, err
	}
	if len(b) > 0 {
		if json.Unmarshal(b, &m.doc) != nil || m.doc.Runtime == nil || m.doc.Bindings == nil {
			cancel()
			return nil, errors.New("协议配置无法读取")
		}
	}
	for id, v := range m.doc.Bindings {
		v.Status = "unknown"
		m.doc.Bindings[id] = v
	}
	m.wg.Add(1)
	go m.monitor()
	return m, nil
}
func validKind(k string) bool { return k == "wechat" || k == "yyb" }
func ValidateProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("代理地址格式无效")
	}
	if (u.Scheme != "http" && u.Scheme != "socks5") || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("请填写 http://[用户名:密码@]主机:端口 或 socks5://主机:端口")
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 {
		return "", errors.New("代理端口须为 1–65535")
	}
	if u.Scheme == "socks5" && u.User != nil {
		return "", errors.New("第一版 SOCKS5 仅支持无认证代理，请使用 HTTP 认证代理")
	}
	if u.User != nil && strings.Contains(u.User.Username(), ":") {
		return "", errors.New("HTTP 代理用户名不能包含冒号")
	}
	return u.String(), nil
}
func (m *Manager) saveLocked() error {
	b, e := json.Marshal(m.doc)
	if e != nil {
		return e
	}
	if m.db.Put("protocols.v1", b) != nil {
		return errors.New("协议配置保存失败")
	}
	return nil
}
func (m *Manager) client(k string) *adapter {
	m.mu.Lock()
	r := m.doc.Runtime[k]
	m.mu.Unlock()
	return &adapter{kind: k, base: fmt.Sprintf("http://127.0.0.1:%d", r.Port), token: r.Token, proxy: r.Proxy, client: localClient(45 * time.Second)}
}
func (m *Manager) AvatarURL(k string, acc Account) string {
	p := adapt(m.client(k))
	provider, ok := p.(AvatarURLProvider)
	if !ok {
		return ""
	}
	return provider.AvatarURL(acc)
}
func (m *Manager) Proxy(k string) (string, error) {
	if !validKind(k) {
		return "", errors.New("无效协议")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.doc.Runtime[k].Proxy, nil
}
func (m *Manager) identity(ctx context.Context, k string) bool {
	a := m.client(k)
	req, _ := http.NewRequestWithContext(ctx, "GET", a.base+"/robot/health", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	res, e := localClient(time.Second).Do(req)
	if e != nil {
		return false
	}
	defer res.Body.Close()
	var d struct {
		Instance, Service string
		PID               int
	}
	return res.StatusCode == 200 && json.NewDecoder(res.Body).Decode(&d) == nil && d.Instance == m.instance && d.Service == k && d.PID > 0 && func() bool { m.mu.Lock(); defer m.mu.Unlock(); return d.PID == m.doc.Runtime[k].PID }()
}
func (m *Manager) Status(k string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.doc.Runtime[k]
	p := "直连"
	if r.Proxy != "" {
		u, _ := url.Parse(r.Proxy)
		if u != nil {
			p = u.Scheme + "://" + u.Host
		}
	}
	state := m.state[k]
	if state == "" {
		state = "stopped"
	}
	ids := []int64{}
	for id, b := range m.doc.Bindings {
		if b.Protocol == k {
			ids = append(ids, id)
		}
	}
	port := r.Port
	if k == "yyb" {
		port = 8088
	}
	return Status{k, state, fmt.Sprintf("127.0.0.1:%d", port), p, m.retries[k], m.messages[k], ids}
}
func (m *Manager) Ensure(ctx context.Context, k, proxy string, confirmed bool) error {
	if !validKind(k) {
		return errors.New("无效协议")
	}
	proxy, e := ValidateProxy(proxy)
	if e != nil {
		return e
	}
	m.control.Lock()
	defer m.control.Unlock()
	m.mu.Lock()
	old := m.doc.Runtime[k]
	m.mu.Unlock()
	alive := m.processAlive(k, old)
	if alive && old.Proxy != proxy && !confirmed {
		return ErrRestartRequired
	}
	if alive && old.Proxy == proxy && (k != "yyb" || old.Port == 8088) && m.identity(ctx, k) {
		m.mu.Lock()
		m.state[k] = "running"
		m.manual[k] = false
		m.mu.Unlock()
		return nil
	}
	if alive {
		if e = m.stop(k); e != nil {
			return e
		}
	}
	return m.start(ctx, k, proxy)
}
func (m *Manager) processAlive(k string, r runtimeRecord) bool {
	if r.PID <= 0 {
		return false
	}
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(r.PID))
	if e != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil || r.Created == 0 || (uint64(created.HighDateTime)<<32|uint64(created.LowDateTime)) != r.Created {
		return false
	}
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 {
		return false
	}
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return false
	}
	exe := "wechat_win.exe"
	if k == "yyb" {
		exe = "yyb-go.exe"
	}
	expected := filepath.Join(m.root, "protocols", k, exe)
	return strings.EqualFold(windows.UTF16ToString(buf[:n]), expected)
}
func (m *Manager) start(ctx context.Context, k, proxy string) error {
	m.mu.Lock()
	m.state[k] = "starting"
	m.messages[k] = ""
	m.mu.Unlock()
	fail := func(msg string) error {
		m.mu.Lock()
		m.state[k] = "failed"
		m.messages[k] = msg
		m.mu.Unlock()
		return errors.New(msg)
	}
	exe := "wechat_win.exe"
	port := 8062
	if k == "yyb" {
		exe = "yyb-go.exe"
		port = 8088
	}
	dir := filepath.Join(m.root, "protocols", k)
	exePath := filepath.Join(dir, exe)
	if _, e := os.Stat(exePath); e != nil {
		return fail("协议程序缺失，请重新打包 protocols/" + k)
	}
	// YYB always uses 8088, including upgrades from a previously saved random port.
	// Preserve the existing WeChat port allocation policy.
	m.mu.Lock()
	old := m.doc.Runtime[k]
	m.mu.Unlock()
	if k != "yyb" && old.Port > 0 {
		port = old.Port
	}
	listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil && k != "yyb" {
		listener, e = net.Listen("tcp", "127.0.0.1:0")
	}
	if e != nil {
		if k == "yyb" {
			return fail("YYB 协议端口 127.0.0.1:8088 已被占用或无法监听，请先关闭占用该端口的程序后重试")
		}
		return fail("无法分配协议端口")
	}
	port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	tokenBytes := make([]byte, 32)
	if _, e = rand.Read(tokenBytes); e != nil {
		return fail("无法生成协议服务身份")
	}
	token := hex.EncodeToString(tokenBytes)
	data := filepath.Join(m.root, "data", "protocols", k)
	if os.MkdirAll(data, 0700) != nil {
		return fail("协议数据目录不可写")
	}
	args := []string{}
	if k == "yyb" {
		args = []string{"-host", "127.0.0.1", "-port", strconv.Itoa(port), "-resource-root", data}
	}
	cmd := exec.Command(exePath, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200}
	// Pass credentials via the child environment, never the process command line or log.
	env := []string{}
	for _, v := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if strings.HasPrefix(name, "ROBOT_PROTOCOL_") || name == "HTTP_PROXY" || name == "HTTPS_PROXY" || name == "ALL_PROXY" || name == "NO_PROXY" {
			continue
		}
		env = append(env, v)
	}
	env = append(env, "ROBOT_PROTOCOL_TOKEN="+token, "ROBOT_PROTOCOL_INSTANCE="+m.instance, "ROBOT_PROTOCOL_PORT="+strconv.Itoa(port), "ROBOT_PROTOCOL_PROXY="+proxy, "ROBOT_PROTOCOL_MANAGED=1", "NO_PROXY=127.0.0.1,localhost")
	if proxy != "" {
		env = append(env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy)
	}
	cmd.Env = env
	// Protocol sources log raw upstream objects; discard output rather than persist account secrets.
	// nil attaches NUL rather than parent-owned pipes, so output stays valid after client exit.
	cmd.Stdout = nil
	cmd.Stderr = nil
	if e = cmd.Start(); e != nil {
		return fail("协议服务启动失败")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	var created, exited, kernel, user windows.Filetime
	if err == nil {
		err = windows.GetProcessTimes(h, &created, &exited, &kernel, &user)
		windows.CloseHandle(h)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fail("无法确认协议进程身份")
	}
	m.mu.Lock()
	m.doc.Runtime[k] = runtimeRecord{Port: port, PID: cmd.Process.Pid, Token: token, Proxy: proxy, Created: uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)}
	e = m.saveLocked()
	m.mu.Unlock()
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fail("无法保存协议进程身份，已取消启动")
	}
	go func() { _ = cmd.Wait() }()
	timeout, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if m.identity(timeout, k) {
			m.mu.Lock()
			m.state[k] = "running"
			m.manual[k] = false
			m.messages[k] = ""
			m.mu.Unlock()
			m.signal()
			return nil
		}
		select {
		case <-timeout.Done():
			_ = m.stop(k)
			return fail("协议服务未就绪，请检查依赖、Redis 配置及端口")
		case <-tick.C:
		}
	}
}
func (m *Manager) stop(k string) error {
	m.mu.Lock()
	if m.active == k && m.workCancel != nil {
		m.workCancel()
		m.workCtx, m.workCancel = context.WithCancel(m.ctx)
	}
	r := m.doc.Runtime[k]
	for id, b := range m.doc.Bindings {
		if b.Protocol == k {
			if !b.Paused {
				m.taskLog(id, "warning", "协议服务关闭或重启，任务已暂停")
			}
			b.Paused = true
			b.Status = "unknown"
			m.doc.Bindings[id] = b
		}
	}
	for _, a := range m.attempts {
		if a.Protocol == k && a.cancel != nil {
			a.cancel()
			a.Status = "cancelled"
			a.Image = ""
			a.Message = "协议服务已停止"
		}
	}
	m.mu.Unlock()
	if m.processAlive(k, r) {
		p, e := os.FindProcess(r.PID)
		if e != nil {
			return errors.New("无法定位协议进程")
		}
		if e = p.Kill(); e != nil {
			return errors.New("无法停止协议进程")
		}
		for i := 0; i < 30 && m.processAlive(k, r); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if m.processAlive(k, r) {
			return errors.New("协议进程尚未退出")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state[k] = "stopped"
	return m.saveLocked()
}
func (m *Manager) Control(ctx context.Context, k, action string, confirmed bool) error {
	if !validKind(k) {
		return errors.New("无效协议")
	}
	if action == "start" {
		p, _ := m.Proxy(k)
		m.mu.Lock()
		m.retries[k] = 0
		m.mu.Unlock()
		return m.Ensure(ctx, k, p, true)
	}
	if action != "stop" && action != "restart" {
		return errors.New("无效操作")
	}
	if !confirmed {
		return errors.New("请确认受影响任务后再操作")
	}
	m.control.Lock()
	defer m.control.Unlock()
	m.mu.Lock()
	m.manual[k] = true
	m.retries[k] = 0
	p := m.doc.Runtime[k].Proxy
	m.mu.Unlock()
	if e := m.stop(k); e != nil {
		return e
	}
	if action == "restart" {
		return m.start(ctx, k, p)
	}
	return nil
}
func (m *Manager) Activate(k string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workCancel != nil {
		m.workCancel()
	}
	m.workCtx, m.workCancel = context.WithCancel(m.ctx)
	m.active = k
	m.signal()
}
func (m *Manager) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = ""
	if m.workCancel != nil {
		m.workCancel()
	}
	for _, a := range m.attempts {
		if a.cancel != nil {
			a.cancel()
		}
		if !terminal(a.Status) {
			a.Status = "cancelled"
			a.Image = ""
		}
	}
}
func (m *Manager) Close() {
	m.Deactivate()
	m.cancel()
	m.wg.Wait() /* Child services intentionally survive. */
}
func (m *Manager) monitor() {
	defer m.wg.Done()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	next := time.Time{}
	var lastScope context.Context
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
		case <-m.wake:
		}
		m.mu.Lock()
		k, ctx := m.active, m.workCtx
		manual := m.manual[k]
		m.mu.Unlock()
		if k == "" || manual {
			continue
		}
		if ctx != lastScope {
			next = time.Time{}
			lastScope = ctx
		}
		if !m.identity(ctx, k) {
			if ctx.Err() != nil {
				continue
			}
			m.mu.Lock()
			m.retries[k]++
			count := m.retries[k]
			m.mu.Unlock()
			if count > 3 {
				m.mu.Lock()
				m.state[k] = "failed"
				m.messages[k] = "连续重启失败，已停止自动重试，请手动启动"
				m.manual[k] = true
				m.mu.Unlock()
				continue
			}
			_ = m.recoverService(ctx, k)
			next = time.Time{}
			continue
		}
		m.mu.Lock()
		m.retries[k] = 0
		m.mu.Unlock()
		if time.Now().Before(next) {
			continue
		}
		m.mu.Lock()
		if m.maintaining {
			m.mu.Unlock()
			continue
		}
		m.maintaining = true
		m.mu.Unlock()
		next = time.Now().Add(time.Minute)
		m.wg.Add(1)
		go func(scope context.Context, kind string) {
			defer m.wg.Done()
			defer func() { m.mu.Lock(); m.maintaining = false; m.mu.Unlock() }()
			m.checkAccounts(scope, kind)
		}(ctx, k)
	}
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) recoverService(observed context.Context, k string) error {
	m.control.Lock()
	defer m.control.Unlock()
	m.mu.Lock()
	if m.active != k || m.manual[k] || m.workCtx != observed || observed.Err() != nil {
		m.mu.Unlock()
		return nil
	}
	r := m.doc.Runtime[k]
	m.mu.Unlock()
	if err := m.stop(k); err != nil {
		return err
	}
	return m.start(m.ctx, k, r.Proxy)
}

func (m *Manager) accountLock(k, ref string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := k + "\x00" + ref
	if m.accountLocks[key] == nil {
		m.accountLocks[key] = &sync.Mutex{}
	}
	return m.accountLocks[key]
}

// The sink must not call back into Manager. Installed before starting any workers.
func (m *Manager) taskLog(id int64, level, text string) {
	if m.OnTaskLog != nil {
		m.OnTaskLog(id, level, text)
	}
}
