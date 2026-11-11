package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"robot/apps/local-agent/internal/store"
)

func fixture() Catalog {
	return Catalog{Stadium: "球场", Tiers: []Tier{{Tier: "A", Price: 220, Areas: []Area{{Code: "109", Level: "首层看台", IsSaleable: true}, {Code: "110", Level: "首层看台", IsSaleable: false}}}, {Tier: "E", Price: 100, Areas: []Area{{Code: "505", Level: "顶层看台", IsSaleable: true, SpecialArea: "协会"}}}}}
}
func writeCatalog(t *testing.T, root string, c Catalog) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(root, "config", "seat-catalog.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCanonicalFingerprint(t *testing.T) {
	c := fixture()
	a, _ := json.Marshal(c)
	_, fp, err := parseCatalog(a)
	if err != nil {
		t.Fatal(err)
	}
	c.Tiers[0], c.Tiers[1] = c.Tiers[1], c.Tiers[0]
	c.Tiers[1].Areas[0], c.Tiers[1].Areas[1] = c.Tiers[1].Areas[1], c.Tiers[1].Areas[0]
	b, _ := json.MarshalIndent(c, "", "  ")
	_, other, err := parseCatalog(b)
	if err != nil || other != fp {
		t.Fatal("order or whitespace changed fingerprint", err)
	}
	c.Tiers[0].Areas[0].SpecialArea = "新说明"
	b, _ = json.Marshal(c)
	_, changed, err := parseCatalog(b)
	if err != nil || changed == fp {
		t.Fatal("business edit did not change fingerprint", err)
	}
	var obj map[string]any
	if err = json.Unmarshal(a, &obj); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(obj)
	_, other, err = parseCatalog(b)
	if err != nil || other != fp {
		t.Fatal("object key order changed fingerprint", err)
	}
}
func TestTaskPersistenceAndCatalogChange(t *testing.T) {
	root := t.TempDir()
	c := fixture()
	writeCatalog(t, root, c)
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || !first.SeatValid || len(first.Selected) != 2 {
		t.Fatalf("wrong defaults: %+v", first)
	}
	if err = s.SaveSeats(first.ID, "area", []string{"110"}); err == nil {
		t.Fatal("accepted unavailable area")
	}
	if err = s.SaveSeats(first.ID, "area", []string{"109", "109"}); err != nil {
		t.Fatal(err)
	}
	second, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(second.ID); err != nil {
		t.Fatal(err)
	}
	s, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if !s.doc.Tasks[0].SeatValid || s.doc.Tasks[0].Latest == nil || len(s.doc.Tasks[0].Selected) != 1 {
		t.Fatal("restart lost selections or log")
	}
	third, err := s.Create()
	if err != nil || third.ID != 3 {
		t.Fatal("reused deleted number", err)
	}
	c.Tiers[1].Areas[0].SpecialArea = "新说明"
	writeCatalog(t, root, c)
	s, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range s.doc.Tasks {
		if task.SeatValid || len(task.Selected) > 0 {
			t.Fatal("catalog change retained seats")
		}
	}
	s, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if s.doc.Tasks[0].SeatValid {
		t.Fatal("restart restored invalidated seats")
	}
	if err = s.SaveSeats(first.ID, "tier", []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "config", "seat-catalog.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if s.configError == "" || !s.doc.Tasks[0].SeatValid {
		t.Fatal("bad catalog overwrote valid data")
	}
	if err = s.SaveSeats(first.ID, "tier", []string{"A"}); err == nil {
		t.Fatal("accepted unavailable catalog")
	}
}

func TestLogRetentionMigrationAndPersistence(t *testing.T) {
	root := t.TempDir()
	writeCatalog(t, root, fixture())
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := document{NextID: 2, Tasks: []Task{{ID: 1, Latest: &Log{At: "2026-11-11T01:00:00Z", Text: "旧日志"}}}}
	raw, _ := json.Marshal(legacy)
	if err = db.Put("tasks.v1", raw); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Tasks[0]; len(got.Logs) != 1 || got.Logs[0].Text != "旧日志" {
		t.Fatal("legacy log lost")
	}
	for i := 0; i < 505; i++ {
		if err = service.AppendLog(1, "info", fmt.Sprintf("事件 %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := service.Snapshot()
	got := snapshot.Tasks[0]
	if len(got.Logs) != 500 || got.Logs[0].Text != "事件 5" || got.Latest.Text != "事件 504" {
		t.Fatal("retention or latest incorrect")
	}
	snapshot.Tasks[0].Logs[0].Text = "external mutation"
	if service.Snapshot().Tasks[0].Logs[0].Text != "事件 5" {
		t.Fatal("snapshot aliases stored history")
	}
	service, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(service.Snapshot().Tasks[0].Logs) != 500 {
		t.Fatal("history not persisted")
	}
	if err = service.Delete(1); err != nil {
		t.Fatal(err)
	}
	service, err = New(root, db)
	if err != nil || len(service.Snapshot().Tasks) != 0 {
		t.Fatal("deleted history retained", err)
	}
}
func TestLogWriteFailureDoesNotChangeMemory(t *testing.T) {
	root := t.TempDir()
	writeCatalog(t, root, fixture())
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if service.AppendLog(task.ID, "error", "should not commit") == nil {
		t.Fatal("expected write failure")
	}
	if len(service.Snapshot().Tasks[0].Logs) != 1 {
		t.Fatal("failed commit modified memory")
	}
}
