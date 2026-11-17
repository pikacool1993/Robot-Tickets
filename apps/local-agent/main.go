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
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/netdebug"
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
		answer, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "退出 Robot", Message: "确定退出程序吗？客户端登录、扫码等待和账号维护将结束；已启动的微信或 YYB 协议服务继续在后台运行。", Buttons: []string{"Yes", "No"}, DefaultButton: "Yes", CancelButton: "No"})
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
	debugNetwork := os.Getenv("ROBOT_DEBUG") != "0"
	level := slog.LevelInfo
	if debugNetwork {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level}))
	netdebug.Configure(logger, debugNetwork)
	logger.Info("network response debug logging", "enabled", debugNetwork)
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
	err = wails.Run(&options.App{Title: "Robot · 本地客户端", Width: 1280, Height: 720, MinWidth: 1280, MinHeight: 720, BackgroundColour: options.NewRGB(23, 30, 41), AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, SingleInstanceLock: &options.SingleInstanceLock{UniqueId: lockID, OnSecondInstanceLaunch: func(_ options.SecondInstanceData) { app.focus() }}, Windows: &windows.Options{WebviewUserDataPath: filepath.Join(dataDir, "webview"), Theme: windows.Dark}})
	if err != nil {
		logger.Error("desktop runtime failed")
		fatal("桌面窗口启动失败，请检查 WebView2 是否正确安装")
	}
}

func (a *App) CopyTaskLogs(id int64) error {
	view, err := a.service.TaskView()
	if err != nil {
		return err
	}
	for _, task := range view.Tasks {
		if task.ID == id {
			text := fmt.Sprintf("任务 %d · 操作日志\r\n", id)
			labels := map[string]string{"debug": "调试", "info": "普通", "success": "成功", "warning": "提醒", "error": "失败", "": "普通"}
			for _, entry := range task.Logs {
				at := entry.At
				if t, e := time.Parse(time.RFC3339Nano, entry.At); e == nil {
					at = t.Local().Format("2006-01-02 15:04:05")
				}
				text += fmt.Sprintf("%s [%s] %s\r\n", at, labels[entry.Level], entry.Text)
			}
			return wruntime.ClipboardSetText(a.ctx, text)
		}
	}
	return fmt.Errorf("任务不存在")
}

func (a *App) SaveTaskNote(id int64, note string) error { return a.service.SaveTaskNote(id, note) }
func (a *App) SaveTaskAddress(id int64, address *tasks.ShippingAddress) error {
	return a.service.SaveTaskAddress(id, address)
}

func (a *App) RefreshTaskPeople(id int64, login bool) error {
	return a.service.RefreshTaskPeople(id, login)
}
func (a *App) SaveTaskPeople(id int64, ids []string) error { return a.service.SaveTaskPeople(id, ids) }
func (a *App) GetTaskPersonDetails(id int64, personID string) (tasks.PersonView, error) {
	return a.service.TaskPersonDetails(id, personID)
}

func (a *App) GetInventory() (agent.InventoryView, error)     { return a.service.InventoryView() }
func (a *App) SetSaleStopped(value bool) error                { return a.service.SetSaleStopped(value) }
func (a *App) GetOrderHistory() ([]agent.OrderDisplay, error) { return a.service.GetOrderHistory() }
func (a *App) RefreshOrderDetail(taskID int64, id string) error {
	return a.service.RefreshOrderDetail(taskID, id)
}
func (a *App) OrderAfterSale(taskID int64, id, action string) error {
	return a.service.OrderAfterSale(taskID, id, action)
}
func (a *App) DeleteOrderRecord(taskID int64, orderID string) error {
	return a.service.DeleteOrderRecord(taskID, orderID)
}
func (a *App) RecoverInventory(id int64, action string) error {
	return a.service.RecoverInventory(id, action)
}
func (a *App) PrepareRepurchase(id int64, viewerIDs, seats []string, mode string) error {
	return a.service.PrepareRepurchase(id, viewerIDs, seats, mode)
}
func (a *App) SaveInventorySettings(v inventory.Settings) error {
	return a.service.SaveInventorySettings(v)
}
func (a *App) SyncInventoryClock() error                { return a.service.SyncInventoryClock() }
func (a *App) StartInventory(id int64) error            { return a.service.StartInventory(id) }
func (a *App) StopInventory(id int64) error             { return a.service.StopInventory(id) }
func (a *App) SaveTaskCross(id int64, value bool) error { return a.service.SaveTaskCross(id, value) }
