package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"
)

type HostLimiter struct {
	global     chan struct{}
	perHost    int
	minimumGap time.Duration
	mu         sync.Mutex
	hosts      map[string]*hostLimit
}

type hostLimit struct {
	semaphore chan struct{}
	mu        sync.Mutex
	capacity  float64
	tokens    float64
	updatedAt time.Time
}

func NewHostLimiter(global int, perHost int, minimumGap time.Duration) (*HostLimiter, error) {
	if global < 1 || perHost < 1 || perHost > global {
		return nil, errors.New("fetch concurrency must satisfy 1 <= per-host <= global")
	}
	if minimumGap < 0 {
		return nil, errors.New("minimum host request gap cannot be negative")
	}
	return &HostLimiter{
		global:     make(chan struct{}, global),
		perHost:    perHost,
		minimumGap: minimumGap,
		hosts:      make(map[string]*hostLimit),
	}, nil
}

func (limiter *HostLimiter) Acquire(ctx context.Context, host string) (func(), error) {
	hostLimiter := limiter.forHost(host)
	if err := hostLimiter.wait(ctx, limiter.minimumGap); err != nil {
		return nil, err
	}
	select {
	case limiter.global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case hostLimiter.semaphore <- struct{}{}:
		return func() {
			<-hostLimiter.semaphore
			<-limiter.global
		}, nil
	case <-ctx.Done():
		<-limiter.global
		return nil, ctx.Err()
	}
}

func (limiter *HostLimiter) forHost(host string) *hostLimit {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	current := limiter.hosts[host]
	if current == nil {
		current = &hostLimit{
			semaphore: make(chan struct{}, limiter.perHost),
			capacity:  float64(limiter.perHost),
			tokens:    float64(limiter.perHost),
			updatedAt: time.Now(),
		}
		limiter.hosts[host] = current
	}
	return current
}

func (limit *hostLimit) wait(ctx context.Context, minimumGap time.Duration) error {
	if minimumGap == 0 {
		return nil
	}
	for {
		limit.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(limit.updatedAt)
		if elapsed > 0 {
			limit.tokens += float64(elapsed) / float64(minimumGap)
			if limit.tokens > limit.capacity {
				limit.tokens = limit.capacity
			}
			limit.updatedAt = now
		}
		if limit.tokens >= 1 {
			limit.tokens--
			limit.mu.Unlock()
			return nil
		}
		delay := time.Duration((1 - limit.tokens) * float64(minimumGap))
		limit.mu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		}
	}
}

type unlimitedLimiter struct{}

func (unlimitedLimiter) Acquire(context.Context, string) (func(), error) {
	return func() {}, nil
}
