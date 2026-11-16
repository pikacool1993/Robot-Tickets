package inventory

import (
	"encoding/json"
	"os"
	"robot/apps/local-agent/internal/tasks"
	"testing"
)

// The fixture contains only the historical response body, without request credentials.
func TestCapturedStockResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/captured-stock.json")
	if err != nil {
		t.Fatal(err)
	}
	regions, err := Parse(raw)
	if err != nil || len(regions) != 56 {
		t.Fatalf("captured response: regions=%d err=%v", len(regions), err)
	}
	counts, totals := map[int]int{}, map[int]int64{}
	for _, r := range regions {
		counts[r.Group]++
		totals[r.Group] += r.Count
	}
	for group, want := range map[int]struct {
		areas int
		stock int64
	}{1: {23, 7401}, 3: {2, 277}, 6: {31, 13324}} {
		if counts[group] != want.areas || totals[group] != want.stock {
			t.Fatalf("group %d: areas=%d stock=%d", group, counts[group], totals[group])
		}
	}
	first := regions[0]
	if first.Group != 1 || first.ID != 2 || first.Estate != 2 || first.Name != "102" || first.Price != "180.00" || first.Count != 452 {
		t.Fatalf("lost order fields: %+v", first)
	}

	// Replay the actual response against the shipped seat catalog and an area task.
	catalogRaw, err := os.ReadFile("../../config/seat-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog tasks.Catalog
	if err := json.Unmarshal(catalogRaw, &catalog); err != nil {
		t.Fatal(err)
	}
	task := tasks.Task{Mode: "area", Selected: []string{"102"}}
	plan, ok := Match(regions, catalog, task, 2, false, zero)
	if !ok || len(plan.Parts) != 1 || plan.Parts[0].Quantity != 2 || plan.Parts[0].Region.Key() != first.Key() {
		t.Fatalf("captured response did not produce complete plan: %+v", plan)
	}

	// Derived scenarios retain captured identifiers; these are not additional official samples.
	for i := range regions {
		regions[i].Count = 0
	}
	if _, ok := Match(regions, catalog, task, 2, true, zero); ok {
		t.Fatal("zero-stock replay matched")
	}
	regions[0].Count = 1
	if _, ok := Match(regions, catalog, task, 2, true, zero); ok {
		t.Fatal("insufficient-stock replay matched")
	}
}

func TestCapturedEmptySaleResponses(t *testing.T) {
	for _, name := range []string{"captured-sale-ended.json", "captured-sold-out.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			regions, err := Parse(raw)
			if err != nil || len(regions) != 0 {
				t.Fatalf("valid empty response: regions=%d err=%v", len(regions), err)
			}
			catalog, task := fixture()
			if _, ok := Match(regions, catalog, task, 2, true, zero); ok {
				t.Fatal("empty sale response produced a plan")
			}
		})
	}
}
