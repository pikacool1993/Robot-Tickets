package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"robot/apps/local-agent/internal/store"
)

type Area struct {
	Code        string `json:"code"`
	Level       string `json:"level"`
	SpecialArea string `json:"specialArea"`
	IsSaleable  bool   `json:"isSaleable"`
	Restriction string `json:"restriction"`
}
type Tier struct {
	Tier  string  `json:"tier"`
	Price float64 `json:"price"`
	Areas []Area  `json:"areas"`
}
type Catalog struct {
	Stadium string `json:"stadium"`
	Tiers   []Tier `json:"tiers"`
}
type Log struct {
	At   string `json:"at"`
	Text string `json:"text"`
}
type Task struct {
	ID          int64    `json:"id"`
	Mode        string   `json:"mode"`
	Selected    []string `json:"selected"`
	SeatValid   bool     `json:"seatValid"`
	Fingerprint string   `json:"fingerprint"`
	Latest      *Log     `json:"latest"`
}
type document struct {
	NextID      int64  `json:"nextID"`
	Fingerprint string `json:"fingerprint"`
	Tasks       []Task `json:"tasks"`
}
type View struct {
	Catalog     Catalog `json:"catalog"`
	ConfigError string  `json:"configError"`
	Tasks       []Task  `json:"tasks"`
}
type Service struct {
	db          *store.Store
	doc         document
	catalog     Catalog
	configError string
	fingerprint string
}

// Caller serializes access and enforces the agent session for every operation.
func New(root string, db *store.Store) (*Service, error) {
	s := &Service{db: db, doc: document{NextID: 1, Tasks: []Task{}}, catalog: Catalog{Tiers: []Tier{}}}
	data, err := db.Get("tasks.v1")
	if err != nil {
		return nil, errors.New("无法读取本地任务数据")
	}
	if len(data) > 0 {
		if json.Unmarshal(data, &s.doc) != nil || s.doc.NextID < 1 {
			return nil, errors.New("本地任务数据无效")
		}
		seen := map[int64]bool{}
		for _, t := range s.doc.Tasks {
			if t.ID < 1 || t.ID >= s.doc.NextID || seen[t.ID] {
				return nil, errors.New("本地任务编号数据无效")
			}
			seen[t.ID] = true
		}
	}
	file, err := os.Open(filepath.Join(root, "config", "seat-catalog.json"))
	if err != nil {
		s.configError = "座位目录无法读取，请检查 config/seat-catalog.json 后重启客户端"
		return s, nil
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		s.configError = "座位目录过大或无法读取，请修正后重启客户端"
		return s, nil
	}
	c, fp, err := parseCatalog(data)
	if err != nil {
		s.configError = "座位目录格式或内容无效，请修正后重启客户端"
		return s, nil
	}
	s.catalog = c
	s.fingerprint = fp
	if s.doc.Fingerprint != fp {
		next := s.clone()
		if next.Fingerprint != "" {
			for i := range next.Tasks {
				next.Tasks[i].SeatValid = false
				next.Tasks[i].Selected = []string{}
			}
		}
		next.Fingerprint = fp
		if err = s.commit(next); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func parseCatalog(data []byte) (Catalog, string, error) {
	var c Catalog
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, "", err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, "", errors.New("trailing data")
	}
	valid := func(s string) bool { return s != "" && s == strings.TrimSpace(s) && len(s) <= 256 }
	if !valid(c.Stadium) || c.Tiers == nil {
		return c, "", errors.New("invalid catalog")
	}
	names := map[string]bool{}
	codes := map[string]bool{}
	for _, t := range c.Tiers {
		if !valid(t.Tier) || names[t.Tier] || math.IsNaN(t.Price) || math.IsInf(t.Price, 0) || t.Price < 0 || t.Price > 1e6 || math.Abs(t.Price*100-math.Round(t.Price*100)) > 1e-6 || t.Areas == nil {
			return c, "", errors.New("invalid tier")
		}
		names[t.Tier] = true
		for _, a := range t.Areas {
			key := strings.ToLower(a.Code)
			if !valid(a.Code) || codes[key] || (a.Level != "首层看台" && a.Level != "顶层看台") || len(a.SpecialArea) > 1024 || len(a.Restriction) > 1024 {
				return c, "", errors.New("invalid area")
			}
			codes[key] = true
		}
	}
	canonical := Catalog{Stadium: c.Stadium, Tiers: append([]Tier{}, c.Tiers...)}
	for i := range canonical.Tiers {
		canonical.Tiers[i].Areas = append([]Area{}, canonical.Tiers[i].Areas...)
		sort.Slice(canonical.Tiers[i].Areas, func(a, b int) bool { return canonical.Tiers[i].Areas[a].Code < canonical.Tiers[i].Areas[b].Code })
	}
	sort.Slice(canonical.Tiers, func(i, j int) bool { return canonical.Tiers[i].Tier < canonical.Tiers[j].Tier })
	b, _ := json.Marshal(canonical)
	h := sha256.Sum256(b)
	return c, hex.EncodeToString(h[:]), nil
}
func (s *Service) clone() document {
	next := s.doc
	next.Tasks = append([]Task{}, s.doc.Tasks...)
	return next
}
func (s *Service) commit(next document) error {
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = s.db.Put("tasks.v1", b); err != nil {
		return errors.New("任务保存失败，请检查软件目录权限与磁盘空间")
	}
	s.doc = next
	return nil
}
func (s *Service) Snapshot() View {
	tasks := s.clone().Tasks
	if tasks == nil {
		tasks = []Task{}
	}
	return View{Catalog: s.catalog, ConfigError: s.configError, Tasks: tasks}
}
func (s *Service) Create() (Task, error) {
	if s.doc.NextID >= 9007199254740991 {
		return Task{}, errors.New("任务编号已达到上限")
	}
	t := Task{ID: s.doc.NextID, Mode: "tier", Selected: []string{}, Fingerprint: s.fingerprint}
	if s.configError == "" {
		for _, name := range []string{"A", "B", "C", "D", "E"} {
			for _, tier := range s.catalog.Tiers {
				if tier.Tier == name {
					t.Selected = append(t.Selected, name)
				}
			}
		}
		t.SeatValid = len(t.Selected) > 0
	}
	next := s.clone()
	next.NextID++
	next.Tasks = append(next.Tasks, t)
	if err := s.commit(next); err != nil {
		return Task{}, err
	}
	return t, nil
}
func (s *Service) SaveSeats(id int64, mode string, selected []string) error {
	if s.configError != "" {
		return errors.New(s.configError)
	}
	if mode != "tier" && mode != "area" {
		return errors.New("无效的座位模式")
	}
	allowed := map[string]bool{}
	for _, tier := range s.catalog.Tiers {
		if mode == "tier" {
			allowed[tier.Tier] = true
		} else {
			for _, a := range tier.Areas {
				if a.IsSaleable {
					allowed[a.Code] = true
				}
			}
		}
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, v := range selected {
		if !allowed[v] {
			return errors.New("座位选项不在当前可选目录中")
		}
		if !seen[v] {
			unique = append(unique, v)
			seen[v] = true
		}
	}
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			t := &next.Tasks[i]
			t.Mode = mode
			t.Selected = unique
			t.SeatValid = len(unique) > 0
			t.Fingerprint = s.fingerprint
			t.Latest = &Log{At: time.Now().Format(time.RFC3339), Text: "已保存座位要求"}
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
func (s *Service) Delete(id int64) error {
	next := s.clone()
	for i, t := range next.Tasks {
		if t.ID == id {
			next.Tasks = append(next.Tasks[:i], next.Tasks[i+1:]...)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
