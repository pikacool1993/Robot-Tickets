// Package inventory parses stock snapshots and selects complete purchase plans.
package inventory

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"robot/apps/local-agent/internal/tasks"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	QueryMinMS int64 `json:"queryMinMS"`
	QueryMaxMS int64 `json:"queryMaxMS"`
	OrderMinMS int64 `json:"orderMinMS"`
	OrderMaxMS int64 `json:"orderMaxMS"`
}

func Defaults() Settings { return Settings{300, 800, 4500, 7500} }
func (s Settings) Validate() error {
	if s.QueryMinMS <= 0 || s.QueryMaxMS < s.QueryMinMS || s.OrderMinMS < 0 || s.OrderMaxMS < s.OrderMinMS || s.QueryMaxMS > 86400000 || s.OrderMaxMS > 86400000 {
		return errors.New("时间区间无效：查询间隔须大于0、上限不小于下限，且均不超过24小时")
	}
	return nil
}
func Random(n int64) int64 {
	if n <= 1 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic("secure random unavailable")
	}
	return v.Int64()
}
func Between(min, max int64) int64 { return min + Random(max-min+1) }

type Region struct {
	Group  int    `json:"group"`
	ID     int    `json:"id"`
	Estate int    `json:"estate"`
	Name   string `json:"name"`
	Price  string `json:"price"`
	Count  int64  `json:"count"`
	Tier   string `json:"tier"`
}

func (r Region) Key() string {
	return fmt.Sprintf("%d/%d/%d/%s", r.Group, r.ID, r.Estate, strings.ToLower(r.Name))
}

type Snapshot struct {
	Code         int    `json:"code"`
	ButtonStatus int    `json:"btn_status"`
	ButtonText   string `json:"btn_text"`
	MaxNum       int    `json:"max_num"`
	TypeCode     int    `json:"type_code"`
	Data         []struct {
		Region int `json:"region"`
		List   []struct {
			Count  int64  `json:"usable_count"`
			Price  string `json:"price"`
			Estate int    `json:"estate"`
			Name   string `json:"name"`
			ID     int    `json:"id"`
		} `json:"list"`
	} `json:"data"`
}
type Allocation struct {
	Region   Region `json:"region"`
	Quantity int    `json:"quantity"`
}
type Plan struct {
	Parts      []Allocation `json:"parts"`
	Source     string       `json:"source"`
	ObservedAt time.Time    `json:"observedAt"`
	MatchID    string       `json:"matchID"`
}

func Parse(raw []byte) ([]Region, error) {
	var s Snapshot
	if len(raw) > 2<<20 || json.Unmarshal(raw, &s) != nil || (s.Code != 1 && s.Code != 200) || s.Data == nil {
		return nil, errors.New("票区响应格式或业务状态无效")
	}
	regions := []Region{}
	seen := map[string]bool{}
	for _, g := range s.Data {
		for _, a := range g.List {
			r := Region{Group: g.Region, ID: a.ID, Estate: a.Estate, Name: strings.TrimSpace(a.Name), Price: a.Price, Count: a.Count}
			price, err := strconv.ParseFloat(r.Price, 64)
			if r.Name == "" || r.ID <= 0 || r.Group <= 0 || r.Count < 0 || r.Count > 1000000000 || err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price > 1000000 || seen[r.Key()] {
				return nil, errors.New("票区字段无效或重复")
			}
			seen[r.Key()] = true
			regions = append(regions, r)
		}
	}
	return regions, nil
}
func Candidates(regions []Region, c tasks.Catalog, t tasks.Task) []Region {
	allowed := map[string]string{}
	selected := map[string]bool{}
	for _, v := range t.Selected {
		selected[strings.ToLower(v)] = true
	}
	for _, tier := range c.Tiers {
		for _, a := range tier.Areas {
			if a.IsSaleable && ((t.Mode == "tier" && selected[strings.ToLower(tier.Tier)]) || (t.Mode == "area" && selected[strings.ToLower(a.Code)])) {
				allowed[strings.ToLower(a.Code)] = tier.Tier
			}
		}
	}
	out := []Region{}
	for _, r := range regions {
		if tier, ok := allowed[strings.ToLower(r.Name)]; ok && r.Count > 0 {
			r.Tier = tier
			out = append(out, r)
		}
	}
	return out
}
func top(regions []Region) []Region {
	out := append([]Region{}, regions...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key() < out[j].Key()
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
func total(rs []Region) int64 {
	var n int64
	for _, r := range rs {
		n += r.Count
	}
	return n
}
func pick(rs []Region, random func(int64) int64) int {
	n := random(total(rs))
	for i, r := range rs {
		if n < r.Count {
			return i
		}
		n -= r.Count
	}
	return len(rs) - 1
}
func allocate(pool []Region, n int, random func(int64) int64) []Allocation {
	pool = append([]Region{}, pool...)
	out := []Allocation{}
	for n > 0 {
		i := pick(pool, random)
		r := pool[i]
		q := n
		if int64(q) > r.Count {
			q = int(r.Count)
		}
		out = append(out, Allocation{r, q})
		n -= q
		pool = append(pool[:i], pool[i+1:]...)
	}
	return out
}

// Match never combines separate snapshots. random is injected for deterministic tests.
func Match(rs []Region, c tasks.Catalog, t tasks.Task, n int, cross bool, random func(int64) int64) (Plan, bool) {
	if n <= 0 {
		return Plan{}, false
	}
	if random == nil {
		random = Random
	}
	rs = Candidates(rs, c, t)
	singles := []Region{}
	for _, r := range rs {
		if r.Count >= int64(n) {
			singles = append(singles, r)
		}
	}
	if len(singles) > 0 {
		pool := top(singles)
		return Plan{Parts: []Allocation{{pool[pick(pool, random)], n}}}, true
	}
	if !cross {
		return Plan{}, false
	}
	grouped := map[string][]Region{}
	for _, r := range rs {
		grouped[r.Tier] = append(grouped[r.Tier], r)
	}
	names := []string{}
	var weight int64
	for name, group := range grouped {
		pool := top(group)
		grouped[name] = pool
		if total(pool) >= int64(n) {
			names = append(names, name)
			weight += total(pool)
		}
	}
	sort.Strings(names)
	if weight > 0 {
		x := random(weight)
		for _, name := range names {
			pool := grouped[name]
			if x < total(pool) {
				return Plan{Parts: allocate(pool, n, random)}, true
			}
			x -= total(pool)
		}
	}
	pool := top(rs)
	if total(pool) < int64(n) {
		return Plan{}, false
	}
	return Plan{Parts: allocate(pool, n, random)}, true
}

// Schedule computes wall-clock targets once; callers translate them into monotonic deadlines.
func Schedule(now, sale time.Time, s Settings) (query, target time.Time, early bool) {
	query = sale.Add(time.Second)
	if now.After(query) {
		query = now
	}
	end := sale.Add(time.Duration(s.OrderMaxMS) * time.Millisecond)
	if !now.Before(end) {
		return query, time.Time{}, false
	}
	start := sale.Add(time.Duration(s.OrderMinMS) * time.Millisecond)
	if now.After(start) {
		start = now
	}
	target = start.Add(time.Duration(Between(0, end.Sub(start).Milliseconds())) * time.Millisecond)
	return query, target, true
}
