package taskcsv

import (
	"encoding/csv"
	"strings"
	"testing"

	"robot/apps/local-agent/internal/tasks"
)

func catalog() tasks.Catalog {
	return tasks.Catalog{Tiers: []tasks.Tier{
		{Tier: "B", Price: 180, Areas: []tasks.Area{{Code: "102", Level: "首层看台", IsSaleable: true}, {Code: "103", Level: "首层看台", IsSaleable: false}}},
		{Tier: "VIP", Price: 1288, Areas: []tasks.Area{{Code: "vip1", Level: "首层看台", IsSaleable: true}}},
		{Tier: "E", Price: 100, Areas: []tasks.Area{{Code: "504", Level: "顶层看台", IsSaleable: true}}},
	}}
}
func document(t *testing.T, overrides ...map[string]string) *Document {
	t.Helper()
	var output strings.Builder
	writer := csv.NewWriter(&output)
	header := append(append([]string{}, fields...), "客户备注", "状态", "实收费用")
	if err := writer.Write(header); err != nil {
		t.Fatal(err)
	}
	for _, override := range overrides {
		values := map[string]string{"观演人": "张三 李四", "购票张数": "2", "座位要求类型": "固定区域", "指定区域": "102 首层看台 ￥180", "允许跨区凑单": "0", "按价格放宽区域": "1", "开票后自动扩区秒数": "30", "客户备注": "原文,含逗号\n以及\"引号\"", "状态": "已成功出票"}
		for k, v := range override {
			values[k] = v
		}
		row := make([]string, len(header))
		for i, key := range header {
			row[i] = values[key]
		}
		if err := writer.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	doc, err := Parse(append([]byte{0xef, 0xbb, 0xbf}, []byte(output.String())...))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func TestSeatModesAndDirectoryAuthority(t *testing.T) {
	cases := []struct {
		name        string
		values      map[string]string
		seats, mode string
		warn        bool
	}{
		{"fixed", map[string]string{"指定区域": "102 首层看台 ￥999 未开售区域, VIP1 ￥1288"}, "102 vip1", "area", true},
		{"tier", map[string]string{"座位要求类型": "指定票类", "指定区域": "", "指定票类": "B类 ￥180, VIP ￥1288"}, "B VIP", "tier", false},
		{"levels", map[string]string{"座位要求类型": "按层选择", "指定区域": "", "按层选择": "首层看台, 顶层看台"}, "102 504 vip1", "area", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			doc := document(t, test.values)
			row := doc.Preview(tasks.View{Catalog: catalog()})[0]
			if row.Status != "ready" || row.Seats != test.seats || row.Config.Mode != test.mode || (row.Warning != "") != test.warn {
				t.Fatalf("wrong result %+v", row)
			}
			if row.Config.Note != "" || row.Config.ImportCount != 2 || row.Config.RelaxAfterSec != 30 {
				t.Fatal("wrong task fields")
			}
		})
	}
}
func TestValidationAndDuplicateRows(t *testing.T) {
	invalid := []map[string]string{
		{"观演人": "张三", "购票张数": "2"}, {"购票张数": "3"}, {"按价格放宽区域": "是"},
		{"开票后自动扩区秒数": ""}, {"开票后自动扩区秒数": "86401"}, {"开票后自动扩区秒数": "0.5"},
		{"指定区域": "103"}, {"指定区域": "999"}, {"任务ID": "7"},
	}
	for _, values := range invalid {
		row := document(t, values).Preview(tasks.View{Catalog: catalog()})[0]
		if row.Status != "error" {
			t.Fatalf("accepted invalid %v: %+v", values, row)
		}
	}
	doc := document(t, map[string]string{}, map[string]string{"观演人": "李四 张三"})
	rows := doc.Preview(tasks.View{Catalog: catalog()})
	if rows[0].Status != "ready" || rows[1].Status != "duplicate" {
		t.Fatal("same-file duplicate not detected")
	}
	task := rows[0].Config
	task.ID = 7
	rows = doc.Preview(tasks.View{Catalog: catalog(), Tasks: []tasks.Task{task}})
	if rows[0].Status != "duplicate" {
		t.Fatal("existing task not detected")
	}
	linked := document(t, map[string]string{"任务ID": "7", "观演人": "", "指定区域": ""}).Preview(tasks.View{Catalog: catalog(), Tasks: []tasks.Task{task}})[0]
	if linked.Status != "linked" {
		t.Fatal("linked task was not skipped")
	}
}
func TestRoundTripPreservesOriginalColumnsAndOnlyBackfillsID(t *testing.T) {
	doc := document(t, map[string]string{})
	before := append([]string{}, doc.Records[0]...)
	doc.Records[0][doc.IDColumn] = "42"
	raw, err := doc.Export()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range result.Records[0] {
		if i == doc.IDColumn {
			if value != "42" {
				t.Fatal("ID missing")
			}
		} else if value != before[i] {
			t.Fatalf("column %d changed", i)
		}
	}
	if len(result.Header) != len(doc.Header) {
		t.Fatal("lost columns")
	}
}
func TestDuplicateIgnoresInactiveRelaxationDelay(t *testing.T) {
	doc := document(t, map[string]string{"按价格放宽区域": "0"})
	task := doc.Preview(tasks.View{Catalog: catalog()})[0].Config
	task.ID = 7
	task.RelaxAfterSec = 120
	rows := doc.Preview(tasks.View{Catalog: catalog(), Tasks: []tasks.Task{task}})
	if rows[0].Status != "duplicate" {
		t.Fatal("inactive relaxation delay hid duplicate")
	}
}
func TestQualifiedTaskIDsAreScopedToClient(t *testing.T) {
	for _, test := range []struct {
		id, status string
	}{
		{"代理-01-7", "linked"},
		{"7", "linked"}, // Previously exported numeric IDs remain readable.
		{"其他-7", "error"},
		{"代理-01-8", "error"},
		{"代理-01-0", "error"},
		{"代理-01-", "error"},
		{"-7", "error"},
	} {
		t.Run(test.id, func(t *testing.T) {
			doc := document(t, map[string]string{"任务ID": test.id})
			doc.ClientNumber = "代理-01"
			rows := doc.Preview(tasks.View{Tasks: []tasks.Task{{ID: 7}}})
			if rows[0].Status != test.status {
				t.Fatalf("unexpected result %+v", rows[0])
			}
		})
	}
}
func TestMalformedAndMissingHeaders(t *testing.T) {
	for _, raw := range []string{"观演人,观演人\n张三,李四", "观演人\n张三", strings.Join(fields, ",") + "\n\"unterminated"} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatal("malformed CSV accepted")
		}
	}
}
