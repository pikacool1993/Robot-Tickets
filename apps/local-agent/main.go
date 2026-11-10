package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	win "golang.org/x/sys/windows"
	"log/slog"
	"os"
	"path/filepath"
	"robot/apps/local-agent/internal/agent"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/trust"
	"strings"
	"sync"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	service    *agent.Service
	mu         sync.Mutex
	ctx        context.Context
	closing    bool
	allowClose bool
}

func (a *App) startup(ctx context.Context) { a.mu.Lock(); a.ctx = ctx; a.mu.Unlock() }
func (a *App) focus() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		wruntime.WindowUnminimise(ctx)
		wruntime.WindowShow(ctx)
	}
}
func (a *App) GetState() agent.State                  { return a.service.Snapshot() }
func (a *App) GetRememberedForm() (agent.Form, error) { return a.service.Remembered() }
func (a *App) Login(form agent.Form) error            { return a.service.Login(form) }
func (a *App) Logout() error                          { return a.service.Logout() }
func (a *App) GetTasks() (tasks.View, error)          { return a.service.TaskView() }
func (a *App) CreateTask() (tasks.Task, error)        { return a.service.CreateTask() }
func (a *App) SaveTaskSeats(id int64, mode string, selected []string) error {
	return a.service.SaveTaskSeats(id, mode, selected)
}
func (a *App) DeleteTask(id int64) error                    { return a.service.DeleteTask(id) }
func (a *App) GetProtocolProxy(kind string) (string, error) { return a.service.ProtocolProxy(kind) }
func (a *App) GetProtocolStatus() (protocols.Status, error) { return a.service.ProtocolStatus() }
func (a *App) ControlProtocol(action string, confirmed bool) error {
	return a.service.ControlProtocol(action, confirmed)
}
func (a *App) BeginAuthorization(id int64) (protocols.Attempt, error) {
	return a.service.BeginAuthorization(id)
}
func (a *App) GetAuthorization(id int64) (protocols.Attempt, error) {
	return a.service.Authorization(id)
}
func (a *App) ImportConfig() error {
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "选择客户端通信配置", Filters: []wruntime.FileFilter{{DisplayName: "Robot 配置文件 (*.cfg)", Pattern: "*.cfg"}}})
	if err != nil {
		return fmt.Errorf("无法打开文件选择窗口")
	}
	if path == "" {
		return nil
	}
	return a.service.Import(path)
}
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	if a.allowClose {
		a.mu.Unlock()
		return false
	}
	if a.closing {
		a.mu.Unlock()
		return true
	}
	a.closing = true
	a.mu.Unlock()
	// Return to the Windows message loop before opening a native dialog.
	go func() {
		answer, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "退出 Robot", Message: "确定退出程序吗？客户端登录、扫码等待和账号维护将结束；已启动的微信或 YYB 协议服务继续在后台运行。", Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No"})
		a.mu.Lock()
		a.closing = false
		a.allowClose = err == nil && answer == "Yes"
		closeNow := a.allowClose
		a.mu.Unlock()
		if closeNow {
			wruntime.Quit(ctx)
		}
	}()
	return true
}
func fatal(message string) {
	text, _ := win.UTF16PtrFromString(message)
	title, _ := win.UTF16PtrFromString("Robot 启动失败")
	win.MessageBox(0, text, title, 0x10)
	os.Exit(1)
}
func main() {
	exe, err := os.Executable()
	if err != nil {
		fatal("无法确定程序目录")
	}
	root := filepath.Dir(exe)
	// Explicit development override; packaged releases always use the executable folder by default.
	if value := os.Getenv("ROBOT_AGENT_ROOT"); value != "" {
		root = value
	}
	root, err = filepath.Abs(root)
	if err != nil {
		fatal("无效的程序目录")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		fatal("无法读取程序目录")
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0700); err != nil {
		fatal("软件目录不可写，请将软件解压到可写文件夹后运行")
	}
	file, err := os.OpenFile(filepath.Join(dataDir, "logs", time.Now().Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fatal("无法创建运行日志")
	}
	defer file.Close()
	logger := slog.New(slog.NewJSONHandler(file, nil))
	db, err := store.Open(dataDir)
	if err != nil {
		fatal("本地数据库无法打开，请检查软件文件夹权限")
	}
	defer db.Close()
	enc, sign, err := trust.Load()
	if err != nil {
		fatal("软件内置配置密钥无效，请联系管理员重新打包")
	}
	service, err := agent.New(root, db, enc, sign, logger)
	if err != nil {
		fatal("客户端初始化失败")
	}
	defer service.Close()
	if err := service.EnableProtocols(); err != nil {
		fatal("协议服务配置无法读取，请检查本地数据")
	}
	app := &App{service: service}
	lockID := fmt.Sprintf("robot-agent-%x", sha256.Sum256([]byte(strings.ToLower(root))))
	err = wails.Run(&options.App{Title: "Robot · 本地客户端", Width: 1440, Height: 960, MinWidth: 360, MinHeight: 560, BackgroundColour: options.NewRGB(23, 30, 41), AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, SingleInstanceLock: &options.SingleInstanceLock{UniqueId: lockID, OnSecondInstanceLaunch: func(_ options.SecondInstanceData) { app.focus() }}, Windows: &windows.Options{WebviewUserDataPath: filepath.Join(dataDir, "webview"), Theme: windows.Dark}})
	if err != nil {
		logger.Error("desktop runtime failed")
		fatal("桌面窗口启动失败，请检查 WebView2 是否正确安装")
	}
}
