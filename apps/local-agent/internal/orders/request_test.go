package orders

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/userkey"
	"strings"
	"testing"
	"time"
)

func orderFixture() (inventory.Plan, []business.Person, []string) {
	return inventory.Plan{MatchID: "72", Parts: []inventory.Allocation{
			{Region: inventory.Region{Group: 1, Estate: 2, Name: "102", Price: "180.00", Count: 1}, Quantity: 1},
			{Region: inventory.Region{Group: 6, Estate: 7, Name: "507", Price: "100.00", Count: 5}, Quantity: 1},
		}}, []business.Person{
			{ID: "11", UID: "99", Name: "测试甲", Card: "masked-a", Timestamp: 100, Signature: "test-a"},
			{ID: "12", UID: "99", Name: "测试乙", Card: "masked-b", Timestamp: 101, Signature: "test-b"},
		}, []string{"12", "11"}
}

func TestCompleteCrossRegionOrderEncryptionAndLegacyExpireTime(t *testing.T) {
	plan, people, selected := orderFixture()
	body, err := FromPlan(plan, people, selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(body.Regions) != 2 || len(body.Users) != 2 || body.Users[0].ID != "12" || body.Users[0].UID != "99" {
		t.Fatal("lost cross-region allocation or viewer identity")
	}
	now := time.Now()
	key, err := userkey.Parse([]byte(`{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":50}`), now)
	if err != nil {
		t.Fatal(err)
	}
	session := business.Session{Token: "test-token", UserID: "99", AuthCode: "test-code"}
	// Submission wall time may differ from the local monotonic key clock.
	submit := time.Unix(1772777181, 250000000)
	req, path, err := Build(body, key, session, submit)
	if err != nil {
		t.Fatal(err)
	}
	if req.ExpireTime != 4600250 || req.Version != 54 {
		t.Fatalf("wrong legacy expiry: %d", req.ExpireTime)
	}
	u, _ := url.Parse(path)
	wantSign := md5.Sum([]byte("Y6xkC7b5" + "1772777181" + "test-code" + "99"))
	if u.Query().Get("s_time") != "1772777181" || u.Query().Get("s_sign") != hex.EncodeToString(wantSign[:]) || u.Query().Get("lid2") != "99" {
		t.Fatal("wrong account signature or timestamp")
	}
	encrypted, _ := hex.DecodeString(req.EncryptedData)
	block, _ := aes.NewCipher([]byte("0123456789abcdef"))
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, []byte(key.IV)).CryptBlocks(plain, encrypted)
	pad := int(plain[len(plain)-1])
	for _, v := range plain[len(plain)-pad:] {
		if int(v) != pad {
			t.Fatal("bad PKCS7 padding")
		}
	}
	plain = plain[:len(plain)-pad]
	expected, _ := json.Marshal(body)
	if string(plain) != string(expected) {
		t.Fatal("encrypted and visible order bodies differ")
	}
	wire, _ := json.Marshal(req)
	if !strings.Contains(string(wire), `"uid":99`) || strings.Contains(string(wire), `"uid":"99"`) {
		t.Fatal("viewer IDs not numeric")
	}
	if _, _, err := Build(body, key, business.Session{Token: "test", UserID: "99"}, submit); err == nil {
		t.Fatal("missing signing code accepted")
	}
	expired, _ := userkey.Parse([]byte(`{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":1}`), now.Add(-time.Second))
	if _, _, err := Build(body, expired, session, submit); err == nil {
		t.Fatal("expired material accepted")
	}
}

func TestRejectPartialAndMismatchedViewerData(t *testing.T) {
	plan, people, selected := orderFixture()
	if _, err := FromPlan(plan, people, selected[:1]); err == nil {
		t.Fatal("partial order accepted")
	}
	if _, err := FromPlan(plan, people, []string{"11", "11"}); err == nil {
		t.Fatal("duplicate viewer accepted")
	}
	people[0].UID = ""
	if _, err := FromPlan(plan, people, selected); err == nil {
		t.Fatal("missing uid guessed from id")
	}
}
