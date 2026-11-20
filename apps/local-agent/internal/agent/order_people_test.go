package agent

import (
	"context"
	"encoding/json"
	"testing"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/orders"
)

func TestOrderPeopleSnapshotRejectsChangedSession(t *testing.T) {
	for _, field := range []string{"code", "token", "uid"} {
		t.Run(field, func(t *testing.T) {
			s, runs, key := orderFixture(t, 1)
			r := runs[0]
			sent := false
			s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
				sent = true
				return business.OrderResult{}, nil
			})
			switch field {
			case "code":
				r.business.session.AuthCode = "new-code"
			case "token":
				r.business.session.Token = "new-token"
			case "uid":
				r.business.session.UserID = "999"
			}
			s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
			if sent || r.status != "order_review" || r.orderInFlight {
				t.Fatal("stale spectator/session combination allowed submission")
			}
		})
	}
}

func TestRefreshedOrderPeopleAndSessionTravelTogether(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	old := r.orderPeople
	r.business.session = business.Session{Token: "fresh-token", UserID: "42", AuthCode: "fresh-code"}
	r.people[0].Name = "fresh-viewer"
	r.people[0].Signature = "fresh-signature"
	r.people[0].FullPhone = "13812340000"
	if err := r.prepareOrderPeople(); err != nil {
		t.Fatal(err)
	}
	if old.people.People()[0].Signature == "fresh-signature" || old.session.Token == "fresh-token" {
		t.Fatal("refresh mutated old snapshot")
	}
	s.orderClient = orderSenderFunc(func(_ context.Context, _ business.Config, session business.Session, _ string, body any) (business.OrderResult, error) {
		var request orders.Request
		raw, err := body.(business.OrderJSONBody).OrderJSON()
		if err != nil || json.Unmarshal(raw, &request) != nil {
			t.Fatal("invalid encoded order", err)
		}
		if session.Token != "fresh-token" || session.AuthCode != "fresh-code" || request.Users[0].Signature != "fresh-signature" {
			t.Fatal("request mixed refresh versions")
		}
		// An in-flight request must retain its original credentials and people.
		r.business.session.Token = "later-token"
		r.people[0].FullPhone = "later-phone"
		return business.OrderResult{Code: 1, OrderID: "12345"}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	if r.order == nil || r.order.Token != "fresh-token" || r.order.Viewers[0].Phone != "13812340000" {
		t.Fatal("result did not retain submitted snapshot")
	}
}
