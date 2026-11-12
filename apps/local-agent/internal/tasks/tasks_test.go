package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"robot/apps/local-agent/internal/business"
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

func TestTaskNoteAndShippingAddress(t *testing.T) {
	root := t.TempDir()
	writeCatalog(t, root, fixture())
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err = service.SaveNote(task.ID, "  特殊说明\n需要相邻座位  "); err != nil {
		t.Fatal(err)
	}
	if err = service.SaveNote(task.ID, strings.Repeat("字", 501)); err == nil {
		t.Fatal("accepted oversized note")
	}
	address := &ShippingAddress{Form: AddressForm{Province: "四川省", City: "成都市", District: "测试区", Address: "测试街道 1 号", Name: "测试收件人", Phone: "13800000000", Region: []string{"错误区域"}}}
	if err = service.SaveAddress(task.ID, address); err != nil {
		t.Fatal(err)
	}
	// Caller-owned region data and snapshot edits cannot mutate persisted configuration.
	address.Form.Name = "external edit"
	got := service.Snapshot().Tasks[0]
	if got.ShippingAddress.Form.Name != "测试收件人" || strings.Join(got.ShippingAddress.Form.Region, "/") != "四川省/成都市/测试区" {
		t.Fatal("address normalization or ownership failed")
	}
	got.ShippingAddress.Form.Region[0] = "changed"
	invalid := *got.ShippingAddress
	invalid.Form.Phone = "invalid"
	if service.SaveAddress(task.ID, &invalid) == nil {
		t.Fatal("accepted invalid phone")
	}
	service, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	got = service.Snapshot().Tasks[0]
	if got.Note != "特殊说明\n需要相邻座位" || got.ShippingAddress.Form.Region[0] != "四川省" {
		t.Fatal("note/address not persisted")
	}
	for _, entry := range got.Logs {
		if strings.Contains(entry.Text, "测试收件人") || strings.Contains(entry.Text, "13800000000") || strings.Contains(entry.Text, "测试街道") || strings.Contains(entry.Text, "特殊说明") {
			t.Fatal("sensitive details copied to log")
		}
	}
	count := len(got.Logs)
	if err = service.SaveAddress(task.ID, got.ShippingAddress); err != nil {
		t.Fatal(err)
	}
	if len(service.Snapshot().Tasks[0].Logs) != count {
		t.Fatal("unchanged address emitted duplicate log")
	}
	if err = service.SaveAddress(task.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err = service.SaveNote(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	service, err = New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	got = service.Snapshot().Tasks[0]
	if got.ShippingAddress != nil || got.Note != "" {
		t.Fatal("clear did not persist")
	}
	db.Close()
	if service.SaveAddress(task.ID, address) == nil {
		t.Fatal("expected persistence failure")
	}
	if service.Snapshot().Tasks[0].ShippingAddress != nil {
		t.Fatal("failed commit changed address")
	}
}

func TestBusinessPersistenceAndPrivacy(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	b := BusinessData{DeviceID: "17944492587160", DeviceType: "ios", Account: "wechat:account", People: []business.Person{{ID: "1", Name: "测试", Card: "123***456", Phone: "138****0000", FullCard: "sensitive-card", FullPhone: "sensitive-phone", Signature: "private-signature"}}, Usable: true, Status: "ready"}
	if err = s.SaveBusiness(task.ID, b); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Tasks[0].PeopleSelected) != 0 {
		t.Fatal("selected first fetch automatically")
	}
	if err = s.SavePeopleSelection(task.ID, []string{"1"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s.Snapshot())
	for _, secret := range []string{"sensitive-card", "sensitive-phone", "private-signature", "17944492587160", "wechat:account"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("front end leaked %s", secret)
		}
	}
	restarted, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := restarted.Business(task.ID)
	if err != nil || saved.DeviceID != b.DeviceID || saved.People[0].FullCard != "sensitive-card" || len(saved.Selected) != 1 {
		t.Fatalf("lost encrypted cache: %+v %v", saved, err)
	}
	if err = restarted.DisableBusiness(); err != nil {
		t.Fatal(err)
	}
	if err = restarted.SavePeopleSelection(task.ID, []string{"1"}); err == nil {
		t.Fatal("allowed blocked cached people")
	}
	if err = restarted.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Business(task.ID); err == nil {
		t.Fatal("deleted task retained device")
	}
}

func TestDeviceIDLifecycle(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, _ := New(root, db)
	task, _ := s.Create()
	first, err := s.EnsureBusinessDeviceID(task.ID, "windows")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.EnsureBusinessDeviceID(task.ID, "windows")
	if again != first {
		t.Fatal("regenerated same device")
	}
	b, _ := s.Business(task.ID)
	b.Account = "wechat:new-account"
	if err = s.SaveBusiness(task.ID, b); err != nil {
		t.Fatal(err)
	}
	restarted, _ := New(root, db)
	again, _ = restarted.EnsureBusinessDeviceID(task.ID, "windows")
	if again != first {
		t.Fatal("restart/rebind changed device ID")
	}
	changed, err := restarted.EnsureBusinessDeviceID(task.ID, "android")
	if err != nil || changed == first {
		t.Fatal("device switch failed")
	}
	if err = restarted.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.EnsureBusinessDeviceID(task.ID, "android"); err == nil {
		t.Fatal("retained deleted task device")
	}
}
