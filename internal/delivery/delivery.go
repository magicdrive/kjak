// Package delivery is the delivery engine: best-effort delivery of one
// message to many feathers, each independent of the others, through a worker
// pool of bounded size, under one deadline that covers every attempt and
// every wait between attempts.
package delivery

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
)

// Target is one entry of a send, delivered to unless Skip is set.
type Target struct {
	Name    string
	Type    string
	Groups  []string
	Feather feather.Feather
	Skip    *core.SkipInfo
}

// Options of a run.
type Options struct {
	// Timeout is the deadline of the whole run: every attempt and every wait
	// of every target ends by then.
	Timeout     time.Duration
	MaxRetries  int // retries after the first attempt
	Concurrency int // targets delivered at the same time, at most
	Clock       clock.Clock
	Backoff     Backoff
	Redactor    *credential.Redactor
}

// Backoff is the wait before a retry: exponential from Base, capped at Max,
// with equal jitter (half fixed, half random).
type Backoff struct {
	Base time.Duration
	Max  time.Duration
	// Rand returns a number in [0, 1) (math/rand when nil).
	Rand func() float64
}

// DefaultBackoff waits 0.5s, 1s, 2s, ... up to 5s (each between half and all
// of it).
func DefaultBackoff() Backoff {
	return Backoff{Base: 500 * time.Millisecond, Max: 5 * time.Second}
}

// Delay is the wait before retry n (1: the first retry).
func (b Backoff) Delay(n int) time.Duration {
	d := b.Base
	for i := 1; i < n && d < b.Max; i++ {
		d *= 2
	}
	if d > b.Max {
		d = b.Max
	}
	r := b.Rand
	if r == nil {
		r = rand.Float64
	}
	half := d / 2
	return half + time.Duration(r()*float64(d-half))
}

// Run delivers msg to every target and reports one result per target, in
// the order of targets. A target's failure does not stop the others. A
// canceled ctx (SIGINT) stops every delivery as soon as possible; the report
// is then Canceled.
func Run(ctx context.Context, msg core.Message, targets []Target, opt Options) core.Report {
	c := opt.Clock
	if c == nil {
		c = clock.Real{}
	}
	if opt.Backoff.Base <= 0 || opt.Backoff.Max <= 0 {
		// never retry without waiting
		r := opt.Backoff.Rand
		opt.Backoff = DefaultBackoff()
		opt.Backoff.Rand = r
	}
	start := c.Now()
	deadline := start.Add(opt.Timeout)
	runCtx, cancel := clock.WithDeadline(ctx, c, opt.Timeout, feather.ErrDeadline)
	defer cancel()

	results := make([]core.Result, len(targets))
	var active []int
	for i, t := range targets {
		if t.Skip != nil {
			results[i] = core.Result{
				Feather: t.Name, Type: t.Type, Groups: t.Groups,
				Status: core.StatusSkipped, Acceptance: core.AcceptanceNone,
				History: []core.Attempt{}, Skipped: t.Skip,
			}
			continue
		}
		active = append(active, i)
	}

	workers := opt.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(active) {
		workers = len(active)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = deliver(runCtx, msg, targets[i], opt, c, deadline)
			}
		}()
	}
	for _, i := range active {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// canceled: interrupted before the last delivery ended (a later SIGINT,
	// while the report is written, interrupts nothing)
	return core.Aggregate(results, ctx.Err() != nil, c.Now().Sub(start))
}

// deliver makes the attempts of one target.
func deliver(ctx context.Context, msg core.Message, t Target, opt Options, c clock.Clock, deadline time.Time) core.Result {
	res := core.Result{Feather: t.Name, Type: t.Type, Groups: t.Groups, History: []core.Attempt{}}
	start := c.Now()
	defer func() { res.Duration = c.Now().Sub(start) }()

	errInfo := func(code core.ErrorCode, err error) *core.ErrorInfo {
		return &core.ErrorInfo{Code: code, Message: opt.Redactor.Redact(err.Error())}
	}

	for n := 1; ; n++ {
		if ctx.Err() != nil {
			res.Status = core.StatusFailed
			if n == 1 {
				res.Acceptance = core.AcceptanceNone
				res.Error = errInfo(feather.ContextCode(ctx), fmt.Errorf("%v before the first attempt", feather.ContextErr(ctx)))
			} else {
				last := res.History[len(res.History)-1]
				res.Acceptance = last.Acceptance
				res.Error = errInfo(feather.ContextCode(ctx), fmt.Errorf("%v while waiting to retry (last attempt: %s)", feather.ContextErr(ctx), last.Error.Message))
			}
			return res
		}

		at := c.Now()
		o := attempt(ctx, t.Feather, msg)
		a := core.Attempt{
			Number: n, StartedAt: at, Duration: c.Now().Sub(at),
			Acceptance: o.Acceptance, Retryable: o.Retryable,
			StatusCode: o.StatusCode, RetryAfter: o.RetryAfter,
		}
		if o.Acceptance != core.AcceptanceConfirmed {
			if o.Err == nil {
				o.Err = fmt.Errorf("not confirmed")
			}
			a.Error = errInfo(o.Code, o.Err)
		}
		res.History = append(res.History, a)
		res.Attempts = n
		res.Acceptance = o.Acceptance

		if o.Acceptance == core.AcceptanceConfirmed {
			res.Status, res.Error = core.StatusDelivered, nil
			return res
		}
		res.Status = core.StatusFailed
		res.Error = a.Error
		// unknown is never retried: the message may be there already
		if !o.Retryable || o.Acceptance != core.AcceptanceRejected {
			return res
		}
		if ctx.Err() != nil {
			res.Error = errInfo(feather.ContextCode(ctx), fmt.Errorf("%v (last attempt: %s)", feather.ContextErr(ctx), a.Error.Message))
			return res
		}
		if n > opt.MaxRetries {
			res.Error = errInfo(o.Code, fmt.Errorf("%s (gave up after %d attempts)", a.Error.Message, n))
			return res
		}
		wait := opt.Backoff.Delay(n)
		if o.RetryAfter > 0 {
			wait = o.RetryAfter
		}
		if remaining := deadline.Sub(c.Now()); wait >= remaining {
			res.Error = errInfo(o.Code, fmt.Errorf("%s (no retry: the wait of %s does not fit the remaining %s of the deadline)", a.Error.Message, wait, remaining.Round(time.Millisecond)))
			return res
		}
		res.History[len(res.History)-1].Wait = wait
		_ = clock.Sleep(ctx, c, wait) // a done ctx is handled at the top
	}
}

// attempt runs one Deliver, turning a panic of an adapter into an internal
// error of its own target.
func attempt(ctx context.Context, f feather.Feather, msg core.Message) (o feather.Outcome) {
	defer func() {
		if r := recover(); r != nil {
			// the acceptance of a crashed attempt is unknown
			o = feather.Unknown(core.ErrCodeInternal, fmt.Errorf("internal error in the feather: %v", r))
		}
	}()
	return f.Deliver(ctx, msg)
}
