package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"robot/apps/local-agent/internal/agent"
	"robot/apps/local-agent/internal/taskcsv"
)

func (a *App) OpenTaskCSV() (agent.CSVPreview, error) {
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "选择飞书导出的客户预定 CSV", Filters: []wruntime.FileFilter{{DisplayName: "CSV 文件", Pattern: "*.csv"}}})
	if err != nil {
		return agent.CSVPreview{}, errors.New("无法打开文件选择窗口")
	}
	if path == "" {
		return a.service.GetTaskCSVPreview()
	}
	file, err := os.Open(path)
	if err != nil {
		return agent.CSVPreview{}, errors.New("无法读取 CSV 文件")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, taskcsv.MaxBytes+1))
	if err != nil {
		return agent.CSVPreview{}, errors.New("CSV 文件读取失败")
	}
	return a.service.PreviewTaskCSV(raw)
}
func (a *App) CommitTaskCSV(indices, duplicates []int) (agent.CSVPreview, error) {
	return a.service.CommitTaskCSV(indices, duplicates)
}
func (a *App) GetTaskCSVPreview() (agent.CSVPreview, error) { return a.service.GetTaskCSVPreview() }
func (a *App) ExportTaskCSV() (bool, error) {
	data, err := a.service.TaskCSVForExport()
	if err != nil {
		return false, err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "保存已回填任务 ID 的 CSV", DefaultFilename: "客户预定_已回填.csv", Filters: []wruntime.FileFilter{{DisplayName: "CSV 文件", Pattern: "*.csv"}}})
	if err != nil {
		return false, errors.New("无法打开保存窗口")
	}
	if path == "" {
		return false, nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".csv") {
		path += ".csv"
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".task-csv-*")
	if err != nil {
		return false, errors.New("无法写入目标目录")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if writeErr != nil || closeErr != nil {
		return false, errors.New("回填 CSV 保存失败，请重试")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, errors.New("无法保存回填 CSV，请关闭占用文件的程序或选择其他文件名")
	}
	return true, nil
}
