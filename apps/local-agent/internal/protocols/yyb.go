package protocols

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type yybAdapter struct{ *adapter }

var _ Protocol = (*yybAdapter)(nil)

func (a *yybAdapter) Begin(ctx context.Context) (Scan, error) {
	var d struct {
		ID    string `json:"session_id"`
		Image string `json:"image_base64"`
	}
	err := a.call(ctx, "POST", "/qr?as_base64=true", struct{}{}, &d)
	return Scan{ID: d.ID, Image: d.Image, Status: "pending"}, err
}

func (a *yybAdapter) Poll(ctx context.Context, id string) (Scan, error) {
	s := Scan{ID: id, Status: "pending"}
	var d struct{ Status string }
	err := a.call(ctx, "GET", "/qr/"+url.PathEscape(id)+"/poll", nil, &d)
	if err != nil {
		var ce *callError
		if errors.As(err, &ce) && ce.status == 404 {
			s.Status = "expired"
			return s, nil
		}
		return s, err
	}
	s.Status = d.Status
	if s.Status == "unknown" {
		s.Status = "failed"
	}
	if d.Status == "authorized" || d.Status == "confirmed" {
		var acc struct {
			ID               int64
			OpenID, Nickname string
		}
		err = a.call(ctx, "POST", "/qr/"+url.PathEscape(id)+"/confirm", struct{}{}, &acc)
		if err != nil {
			return s, err
		}
		s.Account = &Account{Ref: strconv.FormatInt(acc.ID, 10), Nickname: acc.Nickname}
		s.Status = "confirmed"
	}
	return s, nil
}

func (a *yybAdapter) Maintain(ctx context.Context, acc Account) (string, error) {
	var d struct{ Status string }
	err := a.call(ctx, "POST", "/accounts/refresh", map[string]string{"ref": acc.Ref}, &d)
	var ce *callError
	if errors.As(err, &ce) && ce.status == 404 {
		return "expired", nil
	}
	if err != nil {
		return "unknown", err
	}
	if d.Status == "alive" {
		return "alive", nil
	}
	if d.Status == "expired" {
		return "expired", nil
	}
	return "unknown", nil
}

func (a *yybAdapter) Logout(ctx context.Context, acc Account) error {
	err := a.call(ctx, "DELETE", "/accounts?ref="+url.QueryEscape(acc.Ref), nil, nil)
	var ce *callError
	if errors.As(err, &ce) && ce.status == 404 {
		return nil
	}
	return err
}
