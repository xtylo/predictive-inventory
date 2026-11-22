// Package worker computes forecasts in the background.
//
// HTTP handlers enqueue SKUs and return immediately; a fixed pool of
// goroutines takes them from a bounded queue and calls the Predictor.
// The bound is the back-pressure: when predictions are slower than
// requests, Enqueue fails fast instead of piling up unbounded work.
package worker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/xtylo/predictive-inventory/internal/cache"
	"github.com/xtylo/predictive-inventory/internal/predictor"
)

var (
	// ErrQueueFull means the queue is at capacity; the caller should retry later.
	ErrQueueFull = errors.New("prediction queue is full")
	// ErrClosed means the pool is shutting down and accepts no new work.
	ErrClosed = errors.New("prediction queue is closed")
)

// ForecastWriter stores computed forecasts (implemented by *cache.Cache).
type ForecastWriter interface {
	SetForecast(ctx context.Context, f cache.Forecast) error
}

// Locker is a distributed lock (implemented by *cache.Cache).
type Locker interface {
	TryLock(ctx context.Context, name string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, name string) error
}

// Config tunes the pool.
type Config struct {
	// Workers is the number of goroutines calling the Predictor in parallel.
	Workers int
	// QueueSize bounds how many SKUs can wait. Beyond it, Enqueue fails.
	QueueSize int
	// DedupWindow is the minimum time between two predictions of one SKU.
	DedupWindow time.Duration
	// JobTimeout bounds a single prediction.
	JobTimeout time.Duration
	// OnForecast, if set, runs after each forecast is stored. Its error is
	// logged but does not undo the forecast. A function field keeps the
	// worker unaware of what happens next (reorders, notifications...).
	OnForecast func(ctx context.Context, f cache.Forecast) error
}

// Pool is a fixed set of workers reading from a bounded queue.
type Pool struct {
	cfg       Config
	predictor predictor.Predictor
	forecasts ForecastWriter
	locker    Locker

	// A buffered channel is the queue: sends succeed while there is room.
	queue chan string

	// mu guards closed. Sending on a closed channel panics, so Enqueue and
	// Shutdown must never interleave. RWMutex lets many Enqueue calls hold
	// the read lock at once while Shutdown takes the exclusive write lock.
	mu     sync.RWMutex
	closed bool

	// ctx is cancelled to abort in-flight work when shutdown runs out of time.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New builds a pool. Call Start to launch the workers.
func New(cfg Config, p predictor.Predictor, forecasts ForecastWriter, locker Locker) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		cfg:       cfg,
		predictor: p,
		forecasts: forecasts,
		locker:    locker,
		queue:     make(chan string, cfg.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start launches the worker goroutines.
func (p *Pool) Start() {
	for range p.cfg.Workers {
		p.wg.Go(p.run)
	}
}

// Enqueue schedules a forecast for sku without blocking.
func (p *Pool) Enqueue(sku string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrClosed
	}

	// A select with a default case never blocks: if the channel has no
	// room, default runs immediately. That is the whole back-pressure trick.
	select {
	case p.queue <- sku:
		return nil
	default:
		return ErrQueueFull
	}
}

// Shutdown stops accepting work and waits for the queue to drain.
// If ctx expires first, in-flight predictions are cancelled and the
// remaining queue is dropped; Shutdown then returns ctx's error.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		// Closing the channel makes the workers' range loops end once the
		// buffered items are consumed.
		close(p.queue)
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		return nil
	case <-ctx.Done():
		p.cancel()
		<-done
		return ctx.Err()
	}
}

// run is the body of one worker goroutine.
func (p *Pool) run() {
	// range over a channel receives until the channel is closed and empty.
	for sku := range p.queue {
		if p.ctx.Err() != nil {
			continue // shutdown deadline passed: drop the remaining work
		}
		p.process(sku)
	}
}

// process computes and stores the forecast of one SKU.
func (p *Pool) process(sku string) {
	ctx, cancel := context.WithTimeout(p.ctx, p.cfg.JobTimeout)
	defer cancel()

	lockName := "forecast:" + sku

	// The lock is kept for the whole DedupWindow, not released after the
	// prediction: it means "this SKU was forecast recently", so a burst of
	// sales triggers one model call instead of one per sale.
	acquired, err := p.locker.TryLock(ctx, lockName, p.cfg.DedupWindow)
	if err != nil {
		log.Printf("worker: lock %s: %v", sku, err)
		return
	}
	if !acquired {
		log.Printf("worker: %s forecast recently, skipped", sku)
		return
	}

	forecast, err := p.predictor.Predict(ctx, sku)
	if err != nil {
		log.Printf("worker: predict %s: %v", sku, err)
		p.release(lockName)
		return
	}

	stored := cache.Forecast{
		SKU:         sku,
		Demand:      forecast.Demand,
		HorizonDays: forecast.HorizonDays,
		GeneratedAt: time.Now().UTC(),
		Rationale:   forecast.Rationale,
	}
	if err := p.forecasts.SetForecast(ctx, stored); err != nil {
		log.Printf("worker: store forecast %s: %v", sku, err)
		p.release(lockName)
		return
	}

	log.Printf("worker: %s forecast updated, demand %d", sku, forecast.Demand)

	if p.cfg.OnForecast != nil {
		if err := p.cfg.OnForecast(ctx, stored); err != nil {
			log.Printf("worker: after forecast %s: %v", sku, err)
		}
	}
}

// release frees the lock after a failure, so the next trigger can retry
// instead of waiting for the whole DedupWindow.
func (p *Pool) release(lockName string) {
	// A fresh context: the job's context may already be cancelled, and the
	// lock should be released anyway.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := p.locker.Unlock(ctx, lockName); err != nil {
		log.Printf("worker: %v", err)
	}
}
