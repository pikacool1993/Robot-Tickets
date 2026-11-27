package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	Level string `json:"level"`
	At    string `json:"at"`
	Text  string `json:"text"`
}
type AddressForm struct {
	Province string   `json:"province"`
	City     string   `json:"city"`
	District string   `json:"district"`
	Address  string   `json:"address"`
	Name     string   `json:"name"`
	Phone    string   `json:"phone"`
	Region   []string `json:"region"`
}
type ShippingAddress struct {
	Form AddressForm `json:"form"`
}
type Task struct {
	ImportNames     []string         `json:"importNames,omitempty"`
	ImportCount     int              `json:"importCount,omitempty"`
	ImportIssue     string           `json:"importIssue,omitempty"`
	AllowCross      bool             `json:"allowCross"`
	RelaxSeats      bool             `json:"relaxSeats,omitempty"`
	RelaxAfterSec   int64            `json:"relaxAfterSec,omitempty"`
	BusinessStatus  string           `json:"businessStatus"`
	BusinessMessage string           `json:"businessMessage"`
	People          []PersonView     `json:"people"`
	PeopleSelected  []string         `json:"peopleSelected"`
	SpectatorNames  []string         `json:"spectatorNames"`
	PeopleUsable    bool             `json:"peopleUsable"`
	PeopleComplete  bool             `json:"peopleComplete"`
	Note            string           `json:"note"`
	ShippingAddress *ShippingAddress `json:"shippingAddress,omitempty"`

	WechatNickname string   `json:"wechatNickname,omitempty"`
	ProtocolAvatar string   `json:"protocolAvatar,omitempty"`
	ProtocolStatus string   `json:"protocolStatus,omitempty"`
	AuthProtocol   string   `json:"authProtocol,omitempty"`
	Paused         bool     `json:"paused,omitempty"`
	ID             int64    `json:"id"`
	Mode           string   `json:"mode"`
	Selected       []string `json:"selected"`
	SeatValid      bool     `json:"seatValid"`
	Fingerprint    string   `json:"fingerprint"`
	Logs           []Log    `json:"logs"`
	Latest         *Log     `json:"latest"`
}
type document struct {
	Business    map[int64]BusinessData `json:"business,omitempty"`
	NextID      int64                  `json:"nextID"`
	Fingerprint string                 `json:"fingerprint"`
	Tasks       []Task                 `json:"tasks"`
}
type View struct {
	Catalog     Catalog `json:"catalog"`
	ConfigError string  `json:"configError"`
	Tasks       []Task  `json:"tasks"`
}
type Service struct {
	mu          sync.Mutex
	db          *store.Store
	doc         document
	catalog     Catalog
	configError string
	fingerprint string
}

// Service serializes task mutations, including background protocol event logs.
// The agent layer enforces the session for user operations.
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
		for i := range s.doc.Tasks {
			t := &s.doc.Tasks[i]
			if len(t.Logs) == 0 && t.Latest != nil {
				t.Logs = []Log{*t.Latest}
			}
			if len(t.Logs) > 500 {
				t.Logs = append([]Log{}, t.Logs[len(t.Logs)-500:]...)
			}
			if len(t.Logs) > 0 {
				latest := t.Logs[len(t.Logs)-1]
				t.Latest = &latest
			}
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
				addLog(&next.Tasks[i], "warning", "座位目录已变化，请重新配置座位")
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
	next.Business = cloneBusiness(s.doc.Business)
	next.Tasks = append([]Task{}, s.doc.Tasks...)
	for i := range next.Tasks {
		if a := next.Tasks[i].ShippingAddress; a != nil {
			copy := *a
			copy.Form.Region = append([]string{}, a.Form.Region...)
			next.Tasks[i].ShippingAddress = &copy
		}
		if l := next.Tasks[i].Latest; l != nil {
			copy := *l
			next.Tasks[i].Latest = &copy
		}
		next.Tasks[i].Logs = append([]Log{}, next.Tasks[i].Logs...)
		next.Tasks[i].Selected = append([]string{}, next.Tasks[i].Selected...)
		next.Tasks[i].ImportNames = append([]string{}, next.Tasks[i].ImportNames...)
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := s.clone()
	tasks := copy.Tasks
	for i := range tasks {
		fillBusinessView(&tasks[i], copy.Business[tasks[i].ID])
	}
	if tasks == nil {
		tasks = []Task{}
	}
	return View{Catalog: s.catalog, ConfigError: s.configError, Tasks: tasks}
}
func (s *Service) Create() (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	addLog(&t, "success", "任务已创建")
	next := s.clone()
	next.NextID++
	next.Tasks = append(next.Tasks, t)
	if err := s.commit(next); err != nil {
		return Task{}, err
	}
	return t, nil
}
func (s *Service) ResetIDs() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	next.Tasks = []Task{}
	next.Business = nil
	next.NextID = 1
	return s.commit(next)
}
func (s *Service) SaveSeats(id int64, mode string, selected []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.savePurchaseLocked(id, mode, selected, nil)
}
func (s *Service) SavePurchaseConfiguration(id int64, mode string, seats, people []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(people) == 0 {
		return errors.New("请选择至少一位观演人")
	}
	return s.savePurchaseLocked(id, mode, seats, people)
}
func (s *Service) savePurchaseLocked(id int64, mode string, selected, people []string) error {
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
	if people != nil {
		b := next.Business[id]
		if !b.Usable {
			return errors.New("业务账号尚未就绪，请先刷新")
		}
		allowed, seen := map[string]bool{}, map[string]bool{}
		for _, p := range b.People {
			allowed[p.ID] = p.Available()
		}
		for _, p := range people {
			if !allowed[p] || seen[p] {
				return errors.New("观演人已不存在或不可用，请重新选择")
			}
			seen[p] = true
		}
		b.Selected = append([]string{}, people...)
		for _, t := range next.Tasks {
			if t.ID == id {
				b.ImportConfirmed = t.ImportCount > 0 && len(people) == t.ImportCount
			}
		}
		next.Business[id] = b
	}
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			t := &next.Tasks[i]
			t.Mode = mode
			t.Selected = unique
			t.SeatValid = len(unique) > 0
			t.Fingerprint = s.fingerprint
			label := "指定票类"
			if mode == "area" {
				label = "固定区域"
			}
			selection := strings.Join(unique, "、")
			if selection == "" {
				selection = "未配置"
			}
			addLog(t, "success", "已保存座位要求："+label+" · "+selection)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
func (s *Service) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i, t := range next.Tasks {
		if t.ID == id {
			next.Tasks = append(next.Tasks[:i], next.Tasks[i+1:]...)
			delete(next.Business, id)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}

func addLog(t *Task, level, text string) {
	addLogAt(t, level, text, time.Now())
}

func addLogAt(t *Task, level, text string, at time.Time) {
	entry := Log{At: at.Format(time.RFC3339Nano), Level: level, Text: text}
	t.Logs = append(t.Logs, entry)
	// Deferred execution logs can arrive after synchronous UI/maintenance logs.
	// Preserve event order, retention, and the actual latest event.
	for i := len(t.Logs) - 1; i > 0; i-- {
		prev, err := time.Parse(time.RFC3339Nano, t.Logs[i-1].At)
		if err != nil || !prev.After(at) {
			break
		}
		t.Logs[i], t.Logs[i-1] = t.Logs[i-1], t.Logs[i]
	}
	if len(t.Logs) > 500 {
		t.Logs = append([]Log{}, t.Logs[len(t.Logs)-500:]...)
	}
	latest := t.Logs[len(t.Logs)-1]
	t.Latest = &latest
}
func (s *Service) AppendLog(id int64, level, text string) error {
	return s.AppendLogAt(id, level, text, time.Now())
}

// AppendLogAt persists a deferred event using its occurrence time.
func (s *Service) AppendLogAt(id int64, level, text string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			addLogAt(&next.Tasks[i], level, text, at)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}

// Notes and addresses are committed together with their non-sensitive operation log.
func (s *Service) SaveNote(id int64, note string) error {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		return errors.New("任务备注不能超过 500 字")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			if next.Tasks[i].Note == note {
				return nil
			}
			next.Tasks[i].Note = note
			addLog(&next.Tasks[i], "success", "任务备注已更新")
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
func (s *Service) SaveAddress(id int64, address *ShippingAddress) error {
	if address != nil {
		copy := *address
		f := &copy.Form
		f.Province = strings.TrimSpace(f.Province)
		f.City = strings.TrimSpace(f.City)
		f.District = strings.TrimSpace(f.District)
		f.Address = strings.TrimSpace(f.Address)
		f.Name = strings.TrimSpace(f.Name)
		f.Phone = strings.TrimSpace(f.Phone)
		for _, v := range []string{f.Province, f.City, f.District, f.Name} {
			if v == "" || len([]rune(v)) > 50 {
				return errors.New("请填写完整的省、市、区县及收货人，单项不超过 50 字")
			}
		}
		if f.Address == "" || len([]rune(f.Address)) > 200 {
			return errors.New("请填写详细地址，最多 200 字")
		}
		if len(f.Phone) != 11 || f.Phone[0] != '1' {
			return errors.New("请填写有效的 11 位手机号")
		}
		for _, c := range f.Phone {
			if c < '0' || c > '9' {
				return errors.New("手机号只能包含数字")
			}
		}
		f.Region = []string{f.Province, f.City, f.District}
		address = &copy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			before, _ := json.Marshal(next.Tasks[i].ShippingAddress)
			after, _ := json.Marshal(address)
			if bytes.Equal(before, after) {
				return nil
			}
			next.Tasks[i].ShippingAddress = address
			text := "收货地址已更新"
			if address == nil {
				text = "收货地址已清除"
			}
			addLog(&next.Tasks[i], "success", text)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}

func (s *Service) SaveCross(id int64, value bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			next.Tasks[i].AllowCross = value
			addLog(&next.Tasks[i], "info", "已更新跨区域凑单设置")
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}

func (s *Service) SaveSeatRelaxation(id int64, enabled bool, afterSec int64) error {
	if afterSec < 0 || afterSec > 86400 {
		return errors.New("触发时间须在开票后 0 至 24 小时之间")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			t := &next.Tasks[i]
			t.RelaxSeats, t.RelaxAfterSec = enabled, afterSec
			label := "已关闭库存不足时放宽区域"
			if enabled {
				label = fmt.Sprintf("已设置开票后 %d 秒放宽座位区域", afterSec)
			}
			addLog(t, "info", label)
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
