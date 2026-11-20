package orders

import (
	"fmt"
	"reflect"
	"testing"

	"robot/apps/local-agent/internal/business"
)

func TestPreparedPeopleIsolationAndValidation(t *testing.T) {
	plan, people, selected := orderFixture()
	want, err := FromPlan(plan, people, selected)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PreparePeople(people, selected)
	if err != nil {
		t.Fatal(err)
	}
	people[1].Signature = "changed"
	selected[0] = "missing"
	got, err := p.FromPlan(plan)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("prepared data changed with source", err)
	}
	got.Users[0].Signature = "caller mutation"
	again, err := p.FromPlan(plan)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal("request mutated prepared users", err)
	}
	plan.Parts[0].Quantity = 2
	if _, err := p.FromPlan(plan); err == nil {
		t.Fatal("cached users bypassed allocation validation")
	}
	for _, ids := range [][]string{nil, {"missing"}, {"11", "11"}} {
		if _, err := PreparePeople(people, ids); err == nil {
			t.Fatalf("invalid selection accepted: %v", ids)
		}
	}
	people[0].Disabled = true
	if _, err := PreparePeople(people, []string{"11"}); err == nil {
		t.Fatal("disabled refreshed viewer accepted")
	}
	people[0].Disabled = false
	people[0].UID = ""
	if _, err := PreparePeople(people, []string{"11"}); err == nil {
		t.Fatal("incomplete refreshed viewer accepted")
	}
}

func BenchmarkPreparedPeople(b *testing.B) {
	plan, _, _ := orderFixture()
	var people []business.Person
	for i := 1; i <= 100; i++ {
		people = append(people, business.Person{ID: fmt.Sprint(i), UID: "99", Timestamp: 100, Signature: "signature"})
	}
	selected := []string{"99", "100"}
	p, err := PreparePeople(people, selected)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("prepare-each-submit", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := FromPlan(plan, people, selected); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("prepared", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := p.FromPlan(plan); err != nil {
				b.Fatal(err)
			}
		}
	})
}
