// Package orders prepares complete order requests for a single submission.
package orders

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/userkey"
	"strconv"
	"strings"
	"time"
)

type Region struct {
	Group    int    `json:"region"`
	Estate   int    `json:"estate"`
	Quantity int    `json:"num"`
	Name     string `json:"name"`
	Price    string `json:"price"`
	Count    int64  `json:"usable_count"`
}
type Person struct {
	ID        json.Number `json:"id"`
	UID       json.Number `json:"uid"`
	Name      string      `json:"realname"`
	Card      string      `json:"real_card_id"`
	Phone     string      `json:"phone"`
	Self      bool        `json:"is_self"`
	FullCard  string      `json:"real_card_id2"`
	FullPhone string      `json:"phone2"`
	Timestamp int64       `json:"timestamp"`
	Signature string      `json:"signature"`
	Disabled  bool        `json:"disabled"`
	Disabled2 bool        `json:"disabled2"`
	ShowText  string      `json:"showText"`
}
type Body struct {
	Agree   bool     `json:"agree"`
	MatchID string   `json:"id"`
	Regions []Region `json:"regions"`
	Users   []Person `json:"users"`
}
type Request struct {
	EncryptedData string `json:"encryptedData"`
	Version       int    `json:"version"`
	ExpireTime    int64  `json:"expireTime"`
	Body
}

// EncodedRequest owns read-only JSON bytes for one submission.
type EncodedRequest struct{ wire []byte }

func (r EncodedRequest) OrderJSON() ([]byte, error) {
	if len(r.wire) == 0 {
		return nil, errors.New("订单请求未构建")
	}
	return r.wire, nil
}

func positiveID(s string) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n > 0 && !strings.HasPrefix(s, "0")
}

func FromPlan(plan inventory.Plan, people []business.Person, selected []string) (Body, error) {
	if !positiveID(plan.MatchID) || len(selected) == 0 || len(plan.Parts) == 0 {
		return Body{}, errors.New("订单比赛、人数或票区方案无效")
	}
	prepared, err := PreparePeople(people, selected)
	if err != nil {
		return Body{}, err
	}
	return prepared.FromPlan(plan)
}

// PreparedPeople owns validated, ordered spectator data for one run/session.
// Refreshes build a new snapshot instead of mutating this one.
type PreparedPeople struct {
	users []Person
}

func PreparePeople(people []business.Person, selected []string) (*PreparedPeople, error) {
	if len(selected) == 0 {
		return nil, errors.New("订单观演人不能为空")
	}
	prepared := &PreparedPeople{users: make([]Person, 0, len(selected))}
	byID := map[string]business.Person{}
	for _, p := range people {
		byID[p.ID] = p
	}
	seen := map[string]bool{}
	for _, id := range selected {
		p, ok := byID[id]
		if !ok || !p.Available() || seen[id] || !positiveID(p.ID) || !positiveID(p.UID) || p.Timestamp <= 0 || p.Signature == "" {
			return nil, errors.New("观演人资料不完整，请停止后刷新观演人")
		}
		seen[id] = true
		prepared.users = append(prepared.users, Person{ID: json.Number(p.ID), UID: json.Number(p.UID), Name: p.Name, Card: p.Card, Phone: p.Phone, Self: p.Self, FullCard: p.FullCard, FullPhone: p.FullPhone, Timestamp: p.Timestamp, Signature: p.Signature, ShowText: p.Name + " " + p.Card})
	}
	return prepared, nil
}

// People returns a detached copy so callers cannot modify the prepared snapshot.
func (p *PreparedPeople) People() []Person { return append([]Person(nil), p.users...) }

func (p *PreparedPeople) FromPlan(plan inventory.Plan) (Body, error) {
	if p == nil || len(p.users) == 0 || !positiveID(plan.MatchID) || len(plan.Parts) == 0 {
		return Body{}, errors.New("订单比赛、人数或票区方案无效")
	}
	b := Body{Agree: true, MatchID: plan.MatchID, Users: p.People()}
	sum := 0
	seen := map[string]bool{}
	for _, part := range plan.Parts {
		r := part.Region
		key := fmt.Sprintf("%d/%d", r.Group, r.Estate)
		price, e := strconv.ParseFloat(r.Price, 64)
		if part.Quantity <= 0 || part.Quantity > len(b.Users)-sum || r.Group <= 0 || r.Estate <= 0 || r.Name == "" || e != nil || price < 0 || price > 1000000 || !(price >= 0) || r.Count < int64(part.Quantity) || seen[key] {
			return Body{}, errors.New("订单票区分配无效或人数不完整")
		}
		seen[key] = true
		sum += part.Quantity
		b.Regions = append(b.Regions, Region{r.Group, r.Estate, part.Quantity, r.Name, r.Price, r.Count})
	}
	if sum != len(b.Users) {
		return Body{}, errors.New("订单必须满足全部观演人人数")
	}
	return b, nil
}

// Build uses one submission timestamp and one material snapshot. Call again immediately before any future send.
func Build(body Body, key userkey.Material, session business.Session, submit time.Time) (Request, string, error) {
	return BuildWithExpiredKey(body, key, session, submit, false)
}

func BuildWithExpiredKey(body Body, key userkey.Material, session business.Session, submit time.Time, allowExpiredKey bool) (Request, string, error) {
	return build(body, key, session, submit, allowExpiredKey, nil)
}

// BuildEncodedWithExpiredKey reuses the plaintext encoding in the wire body.
func BuildEncodedWithExpiredKey(body Body, key userkey.Material, session business.Session, submit time.Time, allowExpiredKey bool) (EncodedRequest, string, error) {
	var wire []byte
	_, path, err := build(body, key, session, submit, allowExpiredKey, &wire)
	return EncodedRequest{wire: wire}, path, err
}

func build(body Body, key userkey.Material, session business.Session, submit time.Time, allowExpiredKey bool, wire *[]byte) (Request, string, error) {
	if !key.Valid(time.Now()) && !(allowExpiredKey && key.Present()) {
		return Request{}, "", errors.New("订单加密参数已过期")
	}
	if session.Token == "" || !positiveID(session.UserID) || session.AuthCode == "" {
		return Request{}, "", errors.New("订单业务会话不完整，请重新登录业务")
	}
	if !body.Agree || !positiveID(body.MatchID) || len(body.Users) == 0 || len(body.Regions) == 0 {
		return Request{}, "", errors.New("订单数据无效")
	}
	total := 0
	for _, r := range body.Regions {
		if r.Quantity <= 0 || r.Quantity > len(body.Users)-total {
			return Request{}, "", errors.New("订单人数无效")
		}
		total += r.Quantity
	}
	if total != len(body.Users) {
		return Request{}, "", errors.New("订单人数不完整")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Request{}, "", errors.New("订单数据无法序列化")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(key.Key)
	if err != nil {
		return Request{}, "", errors.New("订单加密密钥无效")
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil || len([]byte(key.IV)) != aes.BlockSize {
		return Request{}, "", errors.New("订单加密参数无效")
	}
	padding := aes.BlockSize - len(raw)%aes.BlockSize
	encrypted := make([]byte, len(raw)+padding)
	copy(encrypted, raw)
	for i := len(raw); i < len(encrypted); i++ {
		encrypted[i] = byte(padding)
	}
	cipher.NewCBCEncrypter(block, []byte(key.IV)).CryptBlocks(encrypted, encrypted)
	ms := submit.UnixMilli()
	if ms < 0 {
		return Request{}, "", errors.New("订单提交时间无效")
	}
	seconds := ms / 1000
	signature := md5.Sum([]byte("Y6xkC7b5" + strconv.FormatInt(seconds, 10) + session.AuthCode + session.UserID))
	path := "/fc/wx_api/v1/MatchCon/createMatchOrder?lid2=" + url.QueryEscape(session.UserID) + "&s_time=" + strconv.FormatInt(seconds, 10) + "&s_sign=" + hex.EncodeToString(signature[:])
	if !key.Valid(time.Now()) && !(allowExpiredKey && key.Present()) {
		return Request{}, "", errors.New("订单加密参数已过期")
	}
	request := Request{hex.EncodeToString(encrypted), key.Version, (key.Created+3600)*1000 + ms%1000, body}
	if wire != nil {
		// Only lowercase cipher hex and integers are formatted here. User fields
		// retain encoding/json escaping, without a second traversal.
		out := make([]byte, 0, len(raw)+len(request.EncryptedData)+96)
		out = append(out, `{"encryptedData":"`...)
		out = append(out, request.EncryptedData...)
		out = append(out, `","version":`...)
		out = strconv.AppendInt(out, int64(request.Version), 10)
		out = append(out, `,"expireTime":`...)
		out = strconv.AppendInt(out, request.ExpireTime, 10)
		out = append(out, ","...)
		out = append(out, raw[1:]...)
		*wire = out
	}
	return request, path, nil
}
