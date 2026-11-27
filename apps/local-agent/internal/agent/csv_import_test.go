package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"robot/apps/local-agent/internal/taskcsv"
	"robot/apps/local-agent/internal/tasks"
)

func csvFixture(t *testing.T) *Service {
	t.Helper()
	s, _, _ := orderFixture(t, 1)
	s.state.ClientNumber = "代理-01"
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := tasks.Catalog{Stadium: "测试", Tiers: []tasks.Tier{{Tier: "B", Price: 180, Areas: []tasks.Area{{Code: "102", Level: "首层看台", IsSaleable: true}}}}}
	raw, _ := json.Marshal(catalog)
	if err := os.WriteFile(filepath.Join(root, "config", "seat-catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	s.tasks, err = tasks.New(root, s.store)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const importCSV = "任务ID,观演人,购票张数,座位要求类型,指定区域,指定票类,按层选择,允许跨区凑单,按价格放宽区域,开票后自动扩区秒数,客户备注\n,张三 李四,2,固定区域,102 首层看台 ￥180,,,1,1,30,仅统计\n,李四 张三,2,固定区域,102,,,1,1,30,第二次\n,王五,2,固定区域,102,,,0,0,,错误行\n"

func TestCSVPartialImportReceiptAndRepeatProtection(t *testing.T) {
	s := csvFixture(t)
	before := len(s.tasks.Snapshot().Tasks)
	preview, err := s.PreviewTaskCSV([]byte(importCSV))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rows[0].Status != "ready" || preview.Rows[1].Status != "duplicate" || preview.Rows[2].Status != "error" {
		t.Fatalf("wrong preview %+v", preview)
	}
	if _, err := s.CommitTaskCSV([]int{1}, nil); err == nil {
		t.Fatal("unconfirmed duplicate imported")
	}
	if _, err := s.CommitTaskCSV([]int{0, 2}, nil); err == nil {
		t.Fatal("invalid row committed")
	}
	if len(s.tasks.Snapshot().Tasks) != before {
		t.Fatal("failed batch partially committed")
	}
	result, err := s.CommitTaskCSV([]int{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Rows[0].Status != "linked" {
		t.Fatal("missing receipt")
	}
	if _, err := s.CommitTaskCSV([]int{0}, nil); err == nil {
		t.Fatal("same preview replay created another task")
	}
	view := s.tasks.Snapshot()
	task := view.Tasks[len(view.Tasks)-1]
	if task.ImportCount != 2 || !task.AllowCross || !task.RelaxSeats || task.RelaxAfterSec != 30 || task.Mode != "area" || task.Note != "" || task.PeopleUsable || task.PeopleComplete {
		t.Fatalf("bad imported task %+v", task)
	}
	raw, err := s.TaskCSVForExport()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "仅统计") || !strings.Contains(string(raw), "错误行") {
		t.Fatal("lost original customer values")
	}
	receipt, err := taskcsv.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Records[0][receipt.IDColumn] != fmt.Sprintf("代理-01-%d", result.Rows[0].TaskID) {
		t.Fatal("receipt is missing client number or has wrong task ID")
	}
	next, err := s.PreviewTaskCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if next.Rows[0].Status != "linked" || next.Rows[1].Status != "duplicate" {
		t.Fatal("receipt not reusable")
	}
	s.state.ClientNumber = "另一客户端"
	foreign, err := s.PreviewTaskCSV(raw)
	if err != nil || foreign.Rows[0].Status != "error" {
		t.Fatal("task from another client was linked", err)
	}
	s.state.ClientNumber = "代理-01"
	if _, err := s.PreviewTaskCSV(raw); err != nil {
		t.Fatal(err)
	}
	result, err = s.CommitTaskCSV([]int{1}, []int{1})
	if err != nil || result.Created != 1 {
		t.Fatal("confirmed duplicate not created", err)
	}
}
func TestCSVPreviewRechecksExistingTasksBeforeCreation(t *testing.T) {
	s := csvFixture(t)
	_, err := s.PreviewTaskCSV([]byte(importCSV))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTaskCSV([]int{0}, nil); err != nil {
		t.Fatal(err)
	}
	exported, err := s.TaskCSVForExport()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewTaskCSV(exported); err != nil {
		t.Fatal(err)
	}
	// An existing task can be deleted after preview. Its row then becomes an
	// error instead of silently creating a replacement with a new ID.
	preview, _ := s.GetTaskCSVPreview()
	if err = s.tasks.Delete(preview.Rows[0].TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTaskCSV([]int{0}, nil); err == nil {
		t.Fatal("missing linked ID created a replacement")
	}
}
