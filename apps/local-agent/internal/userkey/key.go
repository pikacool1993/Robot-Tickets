// Package userkey holds ephemeral, account-scoped order encryption material.
package userkey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type Material struct {
	Key      string `json:"-"`
	IV       string `json:"-"`
	Version  int    `json:"-"`
	Created  int64  `json:"-"`
	deadline time.Time
}
type Snapshot struct {
	Key               string `json:"key"`
	IV                string `json:"iv"`
	Version           int    `json:"version"`
	Created           int64  `json:"created"`
	DeadlineUnixMilli int64  `json:"deadlineUnixMilli"`
}

func Parse(raw []byte, received time.Time) (Material, error) {
	var v struct {
		Key       string `json:"encrypt_key"`
		IV        string `json:"iv"`
		Version   int    `json:"version"`
		Created   int64  `json:"create_time"`
		Remaining int64  `json:"expire_in"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return Material{}, errors.New("用户加密参数格式无效")
	}
	key, err := base64.StdEncoding.DecodeString(v.Key)
	if err != nil || (len(key) != 16 && len(key) != 24 && len(key) != 32) || len([]byte(v.IV)) != 16 || v.Version <= 0 || v.Created <= 0 || v.Created > (1<<63-1)/1000-3601 || v.Remaining <= 0 || v.Remaining > int64((1<<63-1)/time.Second) {
		return Material{}, errors.New("用户加密参数字段无效或已过期")
	}
	return Material{v.Key, v.IV, v.Version, v.Created, received.Add(time.Duration(v.Remaining) * time.Second)}, nil
}
func (m Material) Remaining(now time.Time) time.Duration { return m.deadline.Sub(now) }
func (m Material) Valid(now time.Time) bool              { return m.Key != "" && m.Remaining(now) > 0 }
func (m Material) Present() bool                         { return m.Key != "" && m.IV != "" && m.Version > 0 && m.Created > 0 }
func (m Material) Snapshot() Snapshot {
	return Snapshot{m.Key, m.IV, m.Version, m.Created, m.deadline.UnixMilli()}
}
func Restore(v Snapshot) Material {
	return Material{Key: v.Key, IV: v.IV, Version: v.Version, Created: v.Created, deadline: time.UnixMilli(v.DeadlineUnixMilli)}
}

// Cache merges concurrent fetches. Clear invalidates even a response already in flight.
// It has no persistence or automatic timer; the owning task controls cancellation/retry.
type Cache struct {
	mu         sync.Mutex
	value      Material
	flight     chan struct{}
	generation uint64
	retryAfter time.Time
}

func (c *Cache) Clear() { c.mu.Lock(); defer c.mu.Unlock(); c.generation++; c.value = Material{} }
func (c *Cache) Peek() (Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value, c.value.Present()
}
func (c *Cache) Set(value Material) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.value = value
}
func (c *Cache) Get(ctx context.Context, fetch func(context.Context) (Material, error)) (Material, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Material{}, err
		}
		c.mu.Lock()
		if c.value.Valid(time.Now()) {
			v := c.value
			c.mu.Unlock()
			return v, nil
		}
		if done := c.flight; done != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return Material{}, ctx.Err()
			case <-done:
				continue
			}
		}
		if delay := time.Until(c.retryAfter); delay > 0 {
			c.mu.Unlock()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Material{}, ctx.Err()
			case <-timer.C:
				continue
			}
		}
		done := make(chan struct{})
		c.flight = done
		generation := c.generation
		c.mu.Unlock()
		v, err := fetch(ctx)
		c.mu.Lock()
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if c.generation != generation {
			err = errors.New("用户加密参数所属会话已变化")
		} else if err == nil && !v.Valid(time.Now()) {
			err = errors.New("用户加密参数已过期")
		}
		if err == nil {
			c.value = v
		} else {
			c.retryAfter = time.Now().Add(time.Second)
		}
		c.flight = nil
		close(done)
		c.mu.Unlock()
		if err != nil {
			return Material{}, err
		}
		return v, nil
	}
}
