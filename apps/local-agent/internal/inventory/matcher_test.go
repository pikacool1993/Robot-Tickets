package inventory

import (
	"fmt"
	"reflect"
	"testing"

	"robot/apps/local-agent/internal/tasks"
)

func TestMatcherSnapshotAndFreshStock(t *testing.T) {
	c := tasks.Catalog{Tiers: []tasks.Tier{{Tier: "A", Areas: []tasks.Area{{Code: "East", IsSaleable: true}, {Code: "Closed", IsSaleable: false}}}}}
	task := tasks.Task{Mode: "area", Selected: []string{"EAST", "closed"}}
	m := NewMatcher(c, task)
	rs := []Region{region("east", 2), region("Closed", 9), region("West", 9)}
	want := rs[0]
	want.Tier = "A"
	if got := m.Candidates(rs); !reflect.DeepEqual(got, []Region{want}) {
		t.Fatalf("case/saleability filtering: %+v", got)
	}
	// Compilation owns its lookup table, independent of later configuration edits.
	task.Selected[0] = "West"
	c.Tiers[0].Areas[0].Code = "West"
	if got := m.Candidates(rs); !reflect.DeepEqual(got, []Region{want}) {
		t.Fatalf("compiled configuration changed: %+v", got)
	}
	if _, ok := m.Match(rs, 2, false, zero); !ok {
		t.Fatal("initial stock not matched")
	}
	rs[0].Count = 0
	if _, ok := m.Match(rs, 1, false, zero); ok {
		t.Fatal("reused stale stock quantities")
	}
	if rs[0].Tier != "" {
		t.Fatal("matcher mutated shared response")
	}
	if _, ok := NewMatcher(c, task).Match(rs, 1, false, zero); !ok {
		t.Fatal("new run did not consume updated configuration")
	}
}

func TestPriceMatcherUsesSelectedRangeInclusively(t *testing.T) {
	c := tasks.Catalog{Tiers: []tasks.Tier{
		{Tier: "A", Price: 100, Areas: []tasks.Area{{Code: "selected-a", IsSaleable: true}, {Code: "other-a", IsSaleable: true}}},
		{Tier: "B", Price: 200, Areas: []tasks.Area{{Code: "selected-b", IsSaleable: true}}},
		{Tier: "C", Price: 250, Areas: []tasks.Area{{Code: "too-high", IsSaleable: true}}},
	}}
	task := tasks.Task{Mode: "area", Selected: []string{"selected-a", "selected-b"}}
	rs := []Region{region("other-a", 1), region("selected-b", 1), region("too-high", 1)}
	got := NewPriceMatcher(c, task).Candidates(rs)
	if len(got) != 2 || got[0].Name != "other-a" || got[1].Name != "selected-b" {
		t.Fatalf("unexpected price-range candidates: %+v", got)
	}
}

func TestCompiledMatchPreservesSelection(t *testing.T) {
	c, task := fixture()
	rs := []Region{region("109", 3), region("110", 2), region("111", 1), region("101", 3), region("102", 2)}
	for _, mode := range []string{"tier", "area"} {
		task.Mode = mode
		if mode == "area" {
			task.Selected = []string{"109", "110", "101"}
		}
		m := NewMatcher(c, task)
		for n := 1; n <= 13; n++ {
			for _, cross := range []bool{false, true} {
				for seed := int64(0); seed < 20; seed++ {
					var bounds [2][]int64
					random := func(i int) func(int64) int64 {
						return func(bound int64) int64 {
							bounds[i] = append(bounds[i], bound)
							return seed % bound
						}
					}
					want, wok := Match(rs, c, task, n, cross, random(0))
					got, gok := m.Match(rs, n, cross, random(1))
					if wok != gok || !reflect.DeepEqual(want, got) || !reflect.DeepEqual(bounds[0], bounds[1]) {
						t.Fatalf("mode=%s n=%d cross=%v seed=%d: got %+v want %+v", mode, n, cross, seed, got, want)
					}
				}
			}
		}
	}
}

func BenchmarkMatcherReuse(b *testing.B) {
	c, task := fixture()
	var rs []Region
	for i := 0; i < 56; i++ {
		name := fmt.Sprintf("area-%d", i)
		c.Tiers[0].Areas = append(c.Tiers[0].Areas, tasks.Area{Code: name, IsSaleable: true})
		rs = append(rs, region(name, int64(i%5)))
	}
	m := NewMatcher(c, task)
	b.Run("prepare-each-response", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Match(rs, c, task, 4, true, zero)
		}
	})
	b.Run("compiled-once", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.Match(rs, 4, true, zero)
		}
	})
}
