package tasks

import (
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/store"
	"testing"
)

func TestImportedTaskAndMatchingPersistAcrossRestart(t *testing.T) {
	root := t.TempDir()
	writeCatalog(t, root, fixture())
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	config := Task{Mode: "area", Selected: []string{"109"}, ImportNames: []string{"张三", "李四"}, ImportCount: 2}
	ids, err := service.ImportConfigured([]Task{config}, service.ImportFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	b := BusinessData{Account: "one", Usable: true, People: []business.Person{{ID: "1", Name: "张三"}, {ID: "2", Name: "李四"}}}
	if err := service.SaveBusiness(ids[0], b); err != nil {
		t.Fatal(err)
	}
	if !service.Snapshot().Tasks[0].PeopleComplete {
		t.Fatal("unique matches not selected")
	}
	if err := service.SavePeopleSelection(ids[0], []string{"1"}); err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().Tasks[0].PeopleComplete {
		t.Fatal("partial manual selection enabled purchase")
	}
	next, err := New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task := next.Snapshot().Tasks[0]
	if task.ImportCount != 2 || len(task.ImportNames) != 2 || task.PeopleComplete {
		t.Fatal("import requirements lost on restart")
	}
	config.ImportNames[0] = "modified"
	if task.ImportNames[0] != "张三" {
		t.Fatal("caller mutated task")
	}
	if _, err := service.ImportConfigured([]Task{config}, "stale"); err == nil {
		t.Fatal("stale catalog accepted")
	}
}

func TestImportedPeopleAllRequiredAndManualConfirmation(t *testing.T) {
	task := Task{ImportNames: []string{"张三", "李四"}, ImportCount: 2}
	b := BusinessData{Usable: true, People: []business.Person{{ID: "1", Name: "张三"}}}
	matchImportedPeople(task, &b)
	if importSelectionComplete(task, b) || len(b.Selected) != 1 {
		t.Fatal("partial matching enabled purchase")
	}
	if importedIssue(task, b) != "观演人待确认：李四未找到" {
		t.Fatal(importedIssue(task, b))
	}
	b.People = append(b.People, business.Person{ID: "2", Name: "李四"}, business.Person{ID: "3", Name: "李四"})
	matchImportedPeople(task, &b)
	if importSelectionComplete(task, b) {
		t.Fatal("ambiguous name auto selected")
	}
	b.Selected = []string{"1", "3"}
	b.ImportConfirmed = true
	if !importSelectionComplete(task, b) {
		t.Fatal("manual selection rejected")
	}
	matchImportedPeople(task, &b)
	if b.Selected[1] != "3" {
		t.Fatal("refresh replaced manual selection")
	}
	b.People[2].Disabled = true
	matchImportedPeople(task, &b)
	if !importSelectionComplete(task, b) || b.Selected[1] != "2" {
		t.Fatal("refresh did not match remaining unique name")
	}
	b.People = b.People[:1]
	matchImportedPeople(task, &b)
	if importSelectionComplete(task, b) {
		t.Fatal("removed viewer did not block")
	}
}
func TestSameNameBookingsRequireManualDistinctPeople(t *testing.T) {
	task := Task{ImportNames: []string{"张三", "张三"}, ImportCount: 2}
	b := BusinessData{Usable: true, People: []business.Person{{ID: "1", Name: "张三"}, {ID: "2", Name: "张三"}}}
	matchImportedPeople(task, &b)
	if importSelectionComplete(task, b) {
		t.Fatal("same names auto matched")
	}
	b.Selected = []string{"1", "2"}
	b.ImportConfirmed = true
	if !importSelectionComplete(task, b) {
		t.Fatal("cannot confirm same-name pair")
	}
	b.Selected = []string{"1", "1"}
	if importSelectionComplete(task, b) {
		t.Fatal("same person selected twice")
	}
}
