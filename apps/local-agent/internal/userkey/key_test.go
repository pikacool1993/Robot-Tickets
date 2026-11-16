package userkey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const sample = `{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":50}`

func TestRemainingSecondsAndInvalidParameters(t *testing.T) {
	now := time.Now()
	k, err := Parse([]byte(sample), now)
	if err != nil || k.Remaining(now) != 50*time.Second || k.Remaining(now.Add(20*time.Second)) != 30*time.Second || k.Valid(now.Add(50*time.Second)) {
		t.Fatalf("remaining-seconds rule: %v", err)
	}
	// A different server create_time must not change the local lifetime.
	other, err := Parse([]byte(strings.Replace(sample, `"create_time":1000`, `"create_time":900000`, 1)), now)
	if err != nil || other.Remaining(now) != k.Remaining(now) {
		t.Fatal("create_time affected local lifetime")
	}
	for _, bad := range []string{strings.Replace(sample, `"expire_in":50`, `"expire_in":0`, 1), strings.Replace(sample, `"expire_in":50`, `"expire_in":-1`, 1), strings.Replace(sample, "0123456789abcdef", "short", 1), strings.Replace(sample, "MDEyMzQ1Njc4OWFiY2RlZg==", "invalid", 1), `{}`} {
		if _, err := Parse([]byte(bad), now); err == nil {
			t.Fatal("accepted invalid parameters")
		}
	}
	raw, _ := json.Marshal(k)
	if string(raw) != "{}" {
		t.Fatal("material leaked through JSON")
	}
}

func TestCacheMergesConcurrentFetchAndRejectsLateResult(t *testing.T) {
	var c Cache
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	fetch := func(ctx context.Context) (Material, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return Parse([]byte(sample), time.Now())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(t.Context(), fetch); err != nil {
				t.Error(err)
			}
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent fetches not merged")
	}
	c.Clear()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Get(ctx, fetch); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled get succeeded")
	}
	entered, release = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(t.Context(), func(context.Context) (Material, error) {
			close(entered)
			<-release
			return Parse([]byte(sample), time.Now())
		})
		done <- err
	}()
	<-entered
	c.Clear()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late response crossed cache invalidation")
	}
}

func TestCacheFailureCannotRetryBeforeOneSecond(t *testing.T) {
	var c Cache
	first := time.Time{}
	calls := 0
	fetch := func(context.Context) (Material, error) {
		calls++
		if calls == 1 {
			first = time.Now()
			return Material{}, errors.New("failed")
		}
		if time.Since(first) < time.Second {
			t.Error("retried too early")
		}
		return Parse([]byte(sample), time.Now())
	}
	if _, err := c.Get(t.Context(), fetch); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := c.Get(t.Context(), fetch); err != nil || calls != 2 {
		t.Fatal("retry did not recover")
	}
}
