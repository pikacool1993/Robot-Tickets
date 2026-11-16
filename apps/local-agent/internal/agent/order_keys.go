package agent

import (
	"context"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/userkey"
	"time"
)

func (s *Service) stopOrderKeysLocked(b *businessRun) {
	if b == nil {
		return
	}
	b.keyEpoch++
	if b.keyCancel != nil {
		b.keyCancel()
		b.keyCancel = nil
	}
}

func (s *Service) adoptOrderSessionLocked(id int64, b *businessRun, session business.Session) {
	if b.session.AuthCode != session.AuthCode || b.keys == nil {
		if s.orderKeys == nil {
			s.orderKeys = map[string]*userkey.Cache{}
		}
		account := b.binding.Protocol + "\x00" + b.binding.Account.Ref
		if s.orderKeys[account] == nil {
			s.orderKeys[account] = &userkey.Cache{}
		}
		s.stopOrderKeysLocked(b)
		b.keys = s.orderKeys[account]
		b.keys.Clear()
	}
	b.session = session
	s.startOrderKeysLocked(id, b)
}

// Prefetch after business login, persist an encrypted snapshot, and refresh at expiry.
func (s *Service) startOrderKeysLocked(id int64, b *businessRun) {
	if b.keyCancel != nil || b.keys == nil || b.session.Token == "" {
		return
	}
	ctx, cancel := context.WithCancel(b.ctx)
	b.keyCancel = cancel
	b.keyEpoch++
	epoch := b.keyEpoch
	cache := b.keys
	manager := s.protocols
	s.businessWG.Add(1)
	go func() {
		defer s.businessWG.Done()
		failed := false
		for ctx.Err() == nil {
			s.mu.Lock()
			offlineSince := b.offlineSince
			refreshReady := b.refreshReady
			s.mu.Unlock()
			if !offlineSince.IsZero() && !refreshReady {
				remaining := 18*time.Hour - time.Since(offlineSince)
				if remaining <= 0 || !waitInventory(ctx, remaining) {
					return
				}
				continue
			}
			key, err := cache.Get(ctx, func(fetchCtx context.Context) (userkey.Material, error) {
				callCtx, done := context.WithTimeout(fetchCtx, 15*time.Second)
				defer done()
				return manager.UserEncryptionKey(s.taskNetworkContext(callCtx, id), id, b.binding, business.AppID)
			})
			s.mu.Lock()
			current := ctx.Err() == nil && b.keyEpoch == epoch && s.currentBusinessLocked(id, b)
			if current {
				if err != nil && !failed {
					_ = s.tasks.AppendLog(id, "warning", "订单加密参数暂不可用，获取结束后间隔1秒重试")
				} else if err == nil {
					_ = s.tasks.AppendLog(id, "info", "订单加密参数已就绪")
					s.persistBusinessSessionLocked(id, b)
					s.completeMaintenanceLocked(id, b)
				}
			}
			s.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failed = true
				if !waitInventory(ctx, time.Second) {
					return
				}
				continue
			}
			failed = false
			if !waitInventory(ctx, key.Remaining(time.Now())) {
				return
			}
		}
	}()
}

// One worker owns a staged plan until a response or cancellation completes it.
func (s *Service) waitOrderParametersLocked(id int64, r *inventoryRun) {
	b := r.business
	if b.keys == nil {
		r.message = "等待订单加密参数"
		return
	}
	r.message = "等待订单加密参数；购票方案已保留"
	cache := b.keys
	manager := s.protocols
	s.inventory.wg.Add(1)
	go func() {
		defer s.inventory.wg.Done()
		ctx, cancel := context.WithCancel(r.ctx)
		defer cancel()
		stop := context.AfterFunc(b.ctx, cancel)
		defer stop()
		for ctx.Err() == nil {
			s.mu.Lock()
			offline := !b.offlineSince.IsZero() && time.Since(b.offlineSince) < 18*time.Hour
			s.mu.Unlock()
			var key userkey.Material
			var err error
			if offline {
				var ok bool
				key, ok = cache.Peek()
				if !ok {
					err = context.DeadlineExceeded
				}
			} else {
				key, err = cache.Get(ctx, func(fetchCtx context.Context) (userkey.Material, error) {
					c, done := context.WithTimeout(fetchCtx, 15*time.Second)
					defer done()
					return manager.UserEncryptionKey(s.taskNetworkContext(c, id), id, b.binding, business.AppID)
				})
			}
			if err != nil {
				if !waitInventory(ctx, time.Second) {
					return
				}
				continue
			}
			if !key.Valid(time.Now()) && !(offline && key.Present()) {
				continue
			}
			if s.submitInventoryOrder(ctx, id, r, key) {
				return
			}
		}
	}()
}
