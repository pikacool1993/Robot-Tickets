package inventory

import (
	"fmt"
	"os"
	"robot/apps/local-agent/internal/tasks"
	"testing"
	"time"
)

func fixture() (tasks.Catalog, tasks.Task) {
	c := tasks.Catalog{Tiers: []tasks.Tier{{Tier: "A", Areas: []tasks.Area{{Code: "109", IsSaleable: true}, {Code: "110", IsSaleable: true}, {Code: "111", IsSaleable: true}}}, {Tier: "B", Areas: []tasks.Area{{Code: "101", IsSaleable: true}, {Code: "102", IsSaleable: true}}}}}
	return c, tasks.Task{Mode: "tier", Selected: []string{"A", "B"}}
}
func region(name string, count int64) Region {
	return Region{Group: 1, ID: 1, Name: name, Price: "100.00", Count: count}
}
func zero(int64) int64 { return 0 }
func TestPlanPriorityAndCompleteAllocation(t *testing.T) {
	c, task := fixture()
	rs := []Region{region("109", 3), region("110", 2), region("111", 1), region("101", 3), region("102", 2)}
	if _, ok := Match(rs, c, task, 4, false, zero); ok {
		t.Fatal("cross disabled allowed split")
	}
	p, ok := Match(rs, c, task, 4, true, zero)
	if !ok || len(p.Parts) != 2 || p.Parts[0].Region.Name != "109" || p.Parts[0].Quantity != 3 || p.Parts[1].Quantity != 1 {
		t.Fatalf("bad greedy allocation: %+v", p)
	}
	for _, a := range p.Parts {
		if a.Region.Tier != "A" {
			t.Fatal("split across tiers despite same-tier solution")
		}
	}
	p, ok = Match(rs, c, task, 2, true, zero)
	if !ok || len(p.Parts) != 1 || p.Parts[0].Quantity != 2 {
		t.Fatal("single-region priority lost")
	}
	p, ok = Match([]Region{region("109", 1), region("101", 1)}, c, task, 2, true, zero)
	if !ok || len(p.Parts) != 2 {
		t.Fatal("cross-tier solution not selected")
	}
	if _, ok = Match(rs, c, task, 12, true, zero); ok {
		t.Fatal("partial plan allowed")
	}
	task.Selected = []string{"A"}
	if _, ok = Match([]Region{region("101", 100)}, c, task, 1, true, zero); ok {
		t.Fatal("used unselected tier")
	}
	task.Mode = "area"
	task.Selected = []string{"110"}
	p, ok = Match(rs, c, task, 2, false, zero)
	if !ok || p.Parts[0].Region.Name != "110" {
		t.Fatal("area filtering failed")
	}
}
func TestWeightedTierAndRegionBoundaries(t *testing.T) {
	c, task := fixture()
	rs := []Region{region("109", 3), region("110", 2), region("111", 1), region("101", 3), region("102", 2)}
	for x := int64(0); x < 11; x++ {
		first := true
		p, ok := Match(rs, c, task, 4, true, func(n int64) int64 {
			if first {
				first = false
				if n != 11 {
					t.Fatalf("tier weight %d", n)
				}
				return x
			}
			return 0
		})
		want := "A"
		if x >= 6 {
			want = "B"
		}
		if !ok || p.Parts[0].Region.Tier != want {
			t.Fatalf("weighted tier x=%d: %+v", x, p)
		}
	}
	counts := map[string]int{}
	for x := int64(0); x < 6; x++ {
		p, _ := Match([]Region{region("109", 3), region("110", 2), region("111", 1)}, c, task, 1, false, func(int64) int64 { return x })
		counts[p.Parts[0].Region.Name]++
	}
	if counts["109"] != 3 || counts["110"] != 2 || counts["111"] != 1 {
		t.Fatalf("weights: %v", counts)
	}
}
func TestTopFiveLimit(t *testing.T) {
	c := tasks.Catalog{}
	task := tasks.Task{Mode: "tier", Selected: []string{"A"}}
	tier := tasks.Tier{Tier: "A"}
	var rs []Region
	for i := 0; i < 6; i++ {
		name := fmt.Sprint(i)
		tier.Areas = append(tier.Areas, tasks.Area{Code: name, IsSaleable: true})
		rs = append(rs, region(name, 1))
	}
	c.Tiers = []tasks.Tier{tier}
	if _, ok := Match(rs, c, task, 6, true, zero); ok {
		t.Fatal("silently expanded pool beyond five")
	}
	for i := 0; i < 100; i++ {
		p, ok := Match(rs, c, task, 1, false, nil)
		if !ok || p.Parts[0].Region.Name == "5" {
			t.Fatal("outside top five")
		}
	}
}
func TestParseRealDefaultAndInvalidCounts(t *testing.T) {
	raw, err := os.ReadFile("../../config/bill-regions.json")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := Parse(raw)
	if err != nil || len(rs) != 56 {
		t.Fatalf("fixture: %d %v", len(rs), err)
	}
	if _, err = Parse([]byte(`{"code":1,"data":[{"region":1,"list":[{"id":1,"name":"109","price":"220","usable_count":-1}]}]}`)); err == nil {
		t.Fatal("negative accepted")
	}
	if _, err = Parse([]byte(`{"code":1}`)); err == nil {
		t.Fatal("missing data accepted as empty stock")
	}
	if _, err = Parse([]byte(`{"code":1,"data":[]}`)); err != nil {
		t.Fatal("valid empty list rejected")
	}
}
func TestScheduleWindowBoundaries(t *testing.T) {
	sale := time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)
	s := Defaults()
	for _, offset := range []time.Duration{-time.Hour, 0, 500 * time.Millisecond, 2 * time.Second, 5 * time.Second, 7499 * time.Millisecond, 7500 * time.Millisecond, 8 * time.Second} {
		now := sale.Add(offset)
		q, target, early := Schedule(now, sale, s)
		if q.Before(sale.Add(time.Second)) || q.Before(now) {
			t.Fatal("query before eligibility")
		}
		if offset >= 7500*time.Millisecond {
			if early || !target.IsZero() {
				t.Fatal("late start used defaults")
			}
			continue
		}
		lower := sale.Add(4500 * time.Millisecond)
		if now.After(lower) {
			lower = now
		}
		if !early || target.Before(lower) || target.After(sale.Add(7500*time.Millisecond)) {
			t.Fatalf("target outside remainder: %s", target)
		}
	}
	s.OrderMinMS = 5000
	s.OrderMaxMS = 5000
	_, target, early := Schedule(sale, sale, s)
	if !early || !target.Equal(sale.Add(5*time.Second)) {
		t.Fatal("fixed window failed")
	}
	s.QueryMinMS = 0
	if s.Validate() == nil {
		t.Fatal("zero query interval accepted")
	}
}
