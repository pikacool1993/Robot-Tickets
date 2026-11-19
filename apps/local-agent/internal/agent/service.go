package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/userkey"
	"strings"
	"sync"
	"time"
)

type Form struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	ClientNumber   string `json:"client_number"`
	Remember       bool   `json:"remember"`
	AuthProtocol   string `json:"auth_protocol"`
	Proxy          string `json:"proxy"`
	ConfirmRestart bool   `json:"confirmRestart"`
	ResetTaskIDs   bool   `json:"resetTaskIDs"`
}
type State struct {
	Configured   bool      `json:"configured"`
	ServerURL    string    `json:"serverURL"`
	InstanceID   string    `json:"instanceID"`
	LoggedIn     bool      `json:"loggedIn"`
	AgentName    string    `json:"agentName"`
	ClientNumber string    `json:"clientNumber"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Connection   string    `json:"connection"`
	Notice       string    `json:"notice"`
	AuthProtocol string    `json:"authProtocol"`
}
type Service struct {
	inventory         inventoryState
	businessWG        sync.WaitGroup
	businessClient    businessAPI
	orderClient       orderSender
	orderActions      map[string]bool
	orderDetailWG     sync.WaitGroup
	businessRuns      map[int64]*businessRun
	orderKeys         map[string]*userkey.Cache
	businessDone      chan struct{}
	mu                sync.Mutex
	store             *store.Store
	root              string
	enc               map[string][]byte
	sign              map[string]ed25519.PublicKey
	config            *protocol.Config
	client            *cloud.Client
	session           *cloud.Session
	state             State
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	log               *slog.Logger
	tasks             *tasks.Service
	protocols         protocolRuntime
	maintenanceResume map[int64]bool
	debugMode         bool
	simulation        SimulationSettings
}

func New(root string, db *store.Store, enc map[string][]byte, sign map[string]ed25519.PublicKey, logger *slog.Logger) (*Service, error) {
	taskService, err := tasks.New(root, db)
	if err != nil {
		return nil, err
	}
	id, err := db.InstanceID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{store: db, root: root, enc: enc, sign: sign, state: State{InstanceID: id, Connection: "未登录"}, ctx: ctx, cancel: cancel, done: make(chan struct{}), log: logger, maintenanceResume: map[int64]bool{}}
	s.tasks = taskService
	if err := s.initInventory(); err != nil {
		cancel()
		return nil, err
	}
	s.businessClient = business.NewClient()
	s.orderClient = s.businessClient
	s.businessRuns = map[int64]*businessRun{}
	s.businessDone = make(chan struct{})
	if err := s.tasks.DisableBusiness(); err != nil {
		cancel()
		return nil, err
	}
	data, err := db.Get("config")
	if err != nil {
		s.state.Notice = "本地通信配置无法读取，请重新导入 .cfg 文件"
	} else if len(data) > 0 {
		var c protocol.Config
		if json.Unmarshal(data, &c) != nil || c.Validate() != nil {
			s.state.Notice = "本地通信配置无效，请重新导入"
		} else {
			s.useConfig(c)
		}
	}
	if s.config == nil {
		path := filepath.Join(root, "config", "bootstrap.cfg")
		if _, err := os.Stat(path); err == nil {
			if err := s.importFile(path); err != nil {
				s.state.Notice = err.Error()
			}
		}
	}
	if protocolData, getErr := db.Get("auth-protocol"); getErr == nil {
		selected := string(protocolData)
		if selected == "wechat" || selected == "yyb" {
			s.state.AuthProtocol = selected
		}
	}
	s.inventory.wg.Add(1)
	go s.inventoryClockLoop()
	go s.monitor()
	go s.businessLoop()
	return s, nil
}
func (s *Service) useConfig(c protocol.Config) {
	client, err := cloud.New(c)
	if err != nil {
		return
	}
	if s.client != nil {
		s.client.Close()
	}
	s.config = &c
	s.client = client
	s.state.Configured = true
	s.state.ServerURL = c.ServerURL
}
func (s *Service) importFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("无法打开配置文件，请重新选择")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, protocol.MaxConfigSize+1))
	if err != nil || len(data) > protocol.MaxConfigSize {
		return errors.New("配置文件过大或无法读取")
	}
	if len(s.enc) == 0 || len(s.sign) == 0 {
		return errors.New("此软件包尚未配置可信密钥，请联系管理员重新打包")
	}
	c, err := protocol.OpenConfig(data, s.enc, s.sign)
	if err != nil {
		return errors.New("配置文件校验失败：文件可能已损坏、被修改或与软件版本不匹配")
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := s.store.ReplaceConfig(plain); err != nil {
		return errors.New("无法保存配置，请确认软件文件夹可写")
	}
	s.useConfig(c)
	s.state.Notice = "通信配置已导入，请登录；原记住的登录信息已清除"
	s.log.Info("communication configuration imported")
	return nil
}
func (s *Service) Import(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		return errors.New("请先退出登录再更换通信配置")
	}
	return s.importFile(path)
}
func (s *Service) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state }
func (s *Service) Remembered() (Form, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.store.Get("form")
	if err != nil {
		return Form{}, errors.New("无法读取已记住的登录信息，请重新输入")
	}
	var f Form
	if len(data) > 0 && json.Unmarshal(data, &f) != nil {
		return Form{}, errors.New("已记住的登录信息无效，请重新输入")
	}
	if protocol, protocolErr := s.store.Get("auth-protocol"); protocolErr == nil && (string(protocol) == "wechat" || string(protocol) == "yyb") {
		f.AuthProtocol = string(protocol)
	}
	return f, nil
}
func (s *Service) Login(f Form) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config == nil {
		return errors.New("请先导入通信配置")
	}
	if s.session != nil {
		return errors.New("当前已登录")
	}
	if f.AuthProtocol != "wechat" && f.AuthProtocol != "yyb" {
		return errors.New("请选择当前客户端使用的微信或 YYB 授权协议")
	}
	f.Username = strings.ToLower(strings.TrimSpace(f.Username))
	f.ClientNumber = strings.TrimSpace(f.ClientNumber)
	if f.Username == "" || f.Password == "" || f.ClientNumber == "" || len(f.ClientNumber) > 128 {
		return errors.New("请输入用户名、密码和有效的客户端编号（最多 128 字节）")
	}
	if s.protocols != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		err := s.protocols.Ensure(ctx, f.AuthProtocol, f.Proxy, f.ConfirmRestart)
		cancel()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	session, err := s.client.Login(ctx, cloud.LoginInput{Username: f.Username, Password: f.Password, ClientNumber: f.ClientNumber, InstanceID: s.state.InstanceID})
	if err != nil {
		return err
	}
	f.Proxy = ""
	f.ConfirmRestart = false
	data, _ := json.Marshal(f)
	err = s.store.SaveLoginPreferences(f.AuthProtocol, data, f.Remember)
	if err != nil {
		cleanup, cancel := context.WithTimeout(s.ctx, 2*time.Second)
		defer cancel()
		_ = s.client.Call(cleanup, "POST", "/auth/logout", session.Token, struct{}{}, nil)
		return errors.New("无法保存登录偏好，已取消本地登录，请检查文件夹写入权限后重试")
	}
	if f.ResetTaskIDs {
		if err := s.tasks.ResetIDs(); err != nil {
			cleanup, cancel := context.WithTimeout(s.ctx, 2*time.Second)
			defer cancel()
			_ = s.client.Call(cleanup, "POST", "/auth/logout", session.Token, struct{}{}, nil)
			return err
		}
	}
	if err := s.tasks.DisableBusiness(); err != nil {
		cleanup, stop := context.WithTimeout(s.ctx, 2*time.Second)
		defer stop()
		_ = s.client.Call(cleanup, "POST", "/auth/logout", session.Token, struct{}{}, nil)
		return err
	}
	s.session = &session
	s.state.AuthProtocol = f.AuthProtocol
	s.state.LoggedIn = true
	s.state.AgentName = session.Agent.Name
	s.state.ClientNumber = session.ClientNumber
	s.state.ExpiresAt = session.ExpiresAt
	s.state.Connection = "已连接"
	s.state.Notice = ""
	if s.protocols != nil {
		s.protocols.Activate(f.AuthProtocol)
	}
	s.log.Info("agent login succeeded")
	return nil
}
func (s *Service) clearSession(notice string) {
	s.maintenanceResume = map[int64]bool{}
	s.cancelInventoryLocked()
	s.cancelBusinessLocked()
	if s.tasks != nil {
		if err := s.tasks.DisableBusiness(); err != nil {
			s.log.Warn("business state persistence failed")
		}
	}
	if s.protocols != nil {
		s.protocols.Deactivate()
	}
	s.session = nil
	s.state.LoggedIn = false
	s.state.AgentName = ""
	s.state.ClientNumber = ""
	s.state.ExpiresAt = time.Time{}
	s.state.Connection = "未登录"
	s.state.Notice = notice
}
func (s *Service) Logout() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	err := s.client.Call(ctx, "POST", "/auth/logout", s.session.Token, struct{}{}, nil)
	notice := "已退出登录"
	if err != nil {
		notice = "已退出本地登录；服务端会话撤销未确认，同一实例下次登录会替换旧会话"
	}
	s.clearSession(notice)
	s.log.Info("agent logged out")
	return nil
}
func (s *Service) monitor() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextCheck := time.Now()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			session, client := s.session, s.client
			if session == nil {
				s.mu.Unlock()
				continue
			}
			if !now.Before(session.ExpiresAt) {
				s.clearSession("登录已过期，请重新登录")
				s.mu.Unlock()
				continue
			}
			if now.Before(nextCheck) {
				s.mu.Unlock()
				continue
			}
			nextCheck = now.Add(30 * time.Second)
			s.mu.Unlock()
			ctx, cancel := context.WithDeadline(s.ctx, session.ExpiresAt)
			err := client.Call(ctx, "GET", "/auth/me", session.Token, nil, nil)
			cancel()
			s.mu.Lock()
			if s.session == session {
				var apiErr *cloud.APIError
				if !time.Now().Before(session.ExpiresAt) {
					s.clearSession("登录已过期，请重新登录")
				} else if errors.As(err, &apiErr) && apiErr.Status == 401 {
					s.clearSession("账号授权或登录会话已失效，请重新登录")
				} else if err != nil {
					s.state.Connection = "连接中断"
				} else {
					s.state.Connection = "已连接"
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *Service) Close() {
	s.cancel()
	<-s.done
	if s.businessDone != nil {
		<-s.businessDone
	}
	s.businessWG.Wait()
	s.inventory.wg.Wait()
	s.orderDetailWG.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.client.Call(ctx, "POST", "/auth/logout", s.session.Token, struct{}{}, nil)
	}
	s.clearSession("")
	if s.protocols != nil {
		s.protocols.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
	if s.businessClient != nil {
		s.businessClient.Close()
	}
}
