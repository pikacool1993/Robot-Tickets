package feishu

import "testing"

func TestWebhookValidation(t *testing.T) {
	good := "https://open.feishu.cn/open-apis/bot/v2/hook/test-hook-1234"
	for _, raw := range []string{"", good} {
		if err := ValidateWebhook(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"http://open.feishu.cn/open-apis/bot/v2/hook/test-hook-1234", "https://attacker.test/open-apis/bot/v2/hook/test-hook-1234", good + "?secret=x", good + "#x", "https://user@open.feishu.cn/open-apis/bot/v2/hook/test-hook-1234", good + "/other", "https://open.feishu.cn:443/open-apis/bot/v2/hook/test-hook-1234"} {
		if ValidateWebhook(raw) == nil {
			t.Fatalf("accepted invalid hook %q", raw)
		}
	}
	c := Default()
	if !c.Enabled || !c.AuthExpired || !c.OrderCreated || !c.OrderBlocked {
		t.Fatal("defaults must be enabled")
	}
	if c.Allows("test") {
		t.Fatal("unconfigured enabled")
	}
	c.Webhook = good
	if !c.Allows("test") || c.Allows("unknown") {
		t.Fatal("wrong kind gate")
	}
}
