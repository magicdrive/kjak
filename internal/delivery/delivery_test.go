package delivery_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

var msg = core.Message{Body: "hello", Level: core.LevelInfo}

// scripted returns its outcomes in order (the last one repeats) and counts
// its attempts.
type scripted struct {
	outcomes []feather.Outcome
	calls    atomic.Int32
}

func (s *scripted) Deliver(ctx context.Context, _ core.Message) feather.Outcome {
	n := int(s.calls.Add(1))
	if n > len(s.outcomes) {
		n = len(s.outcomes)
	}
	return s.outcomes[n-1]
}

func ok() feather.Outcome { return feather.Confirmed(200) }

func retryable(status int) feather.Outcome {
	return feather.Rejected(core.ErrCodeServer, status, true, fmt.Errorf("HTTP %d", status))
}

func options(c clock.Clock) delivery.Options {
	return delivery.Options{
		Timeout: 10 * time.Second, MaxRetries: 2, Concurrency: 5, Clock: c,
		Backoff: delivery.Backoff{Base: 100 * time.Millisecond, Max: time.Second, Rand: func() float64 { return 0.5 }},
	}
}

// run runs the engine on a clock nobody advances: a regression that makes
// it wait (a retry that should not happen) fails the test instead of hanging
// it. Real time is used only to detect that.
func run(t *testing.T, ctx context.Context, targets []delivery.Target, opt delivery.Options) core.Report {
	t.Helper()
	done := make(chan core.Report, 1)
	go func() { done <- delivery.Run(ctx, msg, targets, opt) }()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("the engine is stuck: it waits for a clock that does not move")
		return core.Report{}
	}
}

// drive runs the engine on a fake clock and advances the clock to each
// pending wait (the deadline timer is always one pending timer).
func drive(t *testing.T, c *clock.Fake, run func() core.Report) core.Report {
	t.Helper()
	done := make(chan core.Report, 1)
	go func() { done <- run() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		select {
		case r := <-done:
			return r
		default:
		}
		waiting := make(chan bool, 1)
		go func() { waiting <- c.BlockUntil(ctx, 2) }()
		select {
		case r := <-done:
			cancel()
			return r
		case ok := <-waiting:
			if !ok {
				t.Fatal("the engine neither finished nor waited")
			}
			if d, has := c.NextIn(); has {
				c.Advance(d)
			}
		}
	}
}

func TestCritical_RetryLimit(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{retryable(503)}}
	targets := []delivery.Target{{Name: "a", Type: "slack", Feather: f}}
	r := drive(t, c, func() core.Report { return delivery.Run(context.Background(), msg, targets, options(c)) })

	if got := f.calls.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + max_retries 2)", got)
	}
	res := r.Results[0]
	if res.Status != core.StatusFailed || res.Attempts != 3 || len(res.History) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if res.Acceptance != core.AcceptanceRejected {
		t.Fatalf("acceptance = %s", res.Acceptance)
	}
	// waits: 50ms+25ms (half of 100ms + 0.5 of the other half), then 150ms
	if w := res.History[0].Wait; w != 75*time.Millisecond {
		t.Fatalf("first wait = %s", w)
	}
	if w := res.History[1].Wait; w != 150*time.Millisecond {
		t.Fatalf("second wait = %s", w)
	}
	if res.History[2].Wait != 0 {
		t.Fatal("no wait follows the last attempt")
	}
	if r.Outcome != core.OutcomeFailure {
		t.Fatalf("outcome = %s", r.Outcome)
	}
}

func TestCritical_RetryLimitZero(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{retryable(503)}}
	opt := options(c)
	opt.MaxRetries = 0
	r := drive(t, c, func() core.Report {
		return delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: f}}, opt)
	})
	if f.calls.Load() != 1 || r.Results[0].Attempts != 1 {
		t.Fatalf("attempts = %d", f.calls.Load())
	}
}

func TestCritical_RetrySucceeds(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{retryable(500), ok()}}
	r := drive(t, c, func() core.Report {
		return delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: f}}, options(c))
	})
	res := r.Results[0]
	if res.Status != core.StatusDelivered || res.Attempts != 2 || res.Error != nil {
		t.Fatalf("result = %+v", res)
	}
	if res.History[0].Acceptance != core.AcceptanceRejected || res.History[1].Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("history = %+v", res.History)
	}
}

func TestCritical_PermanentRejectionNotRetried(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{feather.Rejected(core.ErrCodeRejected, 404, false, errors.New("HTTP 404"))}}
	r := run(t, context.Background(), []delivery.Target{{Name: "a", Feather: f}}, options(c))
	if f.calls.Load() != 1 || r.Results[0].Error.Code != core.ErrCodeRejected {
		t.Fatalf("calls = %d, result = %+v", f.calls.Load(), r.Results[0])
	}
}

func TestCritical_AmbiguousAcceptanceNotRetried(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{feather.Unknown(core.ErrCodeAmbiguous, errors.New("connection reset after the request was sent"))}}
	r := run(t, context.Background(), []delivery.Target{{Name: "a", Feather: f}}, options(c))
	res := r.Results[0]
	if f.calls.Load() != 1 {
		t.Fatalf("an unknown acceptance was retried (%d attempts)", f.calls.Load())
	}
	if res.Status != core.StatusFailed || res.Acceptance != core.AcceptanceUnknown || res.Error.Code != core.ErrCodeAmbiguous {
		t.Fatalf("result = %+v", res)
	}
	if r.Summary.Unknown != 1 || r.Summary.Failed != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
}

// An Outcome that claims to be retryable with an unknown acceptance is not
// retried either: the engine does not trust the flag alone.
func TestCritical_AmbiguousAcceptanceRetryableFlagIgnored(t *testing.T) {
	c := clock.NewFake(t0)
	o := feather.Unknown(core.ErrCodeAmbiguous, errors.New("lost"))
	o.Retryable = true
	f := &scripted{outcomes: []feather.Outcome{o}}
	run(t, context.Background(), []delivery.Target{{Name: "a", Feather: f}}, options(c))
	if f.calls.Load() != 1 {
		t.Fatalf("attempts = %d", f.calls.Load())
	}
}

func TestCritical_RetryAfterHonored(t *testing.T) {
	c := clock.NewFake(t0)
	limited := feather.Rejected(core.ErrCodeRateLimited, 429, true, errors.New("HTTP 429"))
	limited.RetryAfter = 3 * time.Second
	f := &scripted{outcomes: []feather.Outcome{limited, ok()}}
	r := drive(t, c, func() core.Report {
		return delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: f}}, options(c))
	})
	res := r.Results[0]
	if res.Status != core.StatusDelivered || res.Attempts != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res.History[0].Wait != 3*time.Second || res.History[0].RetryAfter != 3*time.Second {
		t.Fatalf("wait = %s, want the Retry-After of 3s", res.History[0].Wait)
	}
	if got := res.History[1].StartedAt.Sub(res.History[0].StartedAt); got != 3*time.Second {
		t.Fatalf("second attempt started %s after the first", got)
	}
}

func TestCritical_RetryAfterBeyondDeadline(t *testing.T) {
	c := clock.NewFake(t0)
	limited := feather.Rejected(core.ErrCodeRateLimited, 429, true, errors.New("HTTP 429"))
	limited.RetryAfter = 30 * time.Second // more than the 10s deadline
	f := &scripted{outcomes: []feather.Outcome{limited, ok()}}
	r := run(t, context.Background(), []delivery.Target{{Name: "a", Feather: f}}, options(c))
	res := r.Results[0]
	if f.calls.Load() != 1 || res.Status != core.StatusFailed || res.Error.Code != core.ErrCodeRateLimited {
		t.Fatalf("calls = %d, result = %+v", f.calls.Load(), res)
	}
	if c.Now() != t0 {
		t.Fatal("the engine waited for a retry that cannot fit the deadline")
	}
}

// blocking blocks each attempt until its context is done, then reports what
// a real adapter reports: a request that never fully left.
type blocking struct {
	entered chan struct{}
	calls   atomic.Int32
}

func (b *blocking) Deliver(ctx context.Context, _ core.Message) feather.Outcome {
	b.calls.Add(1)
	if b.entered != nil {
		b.entered <- struct{}{}
	}
	<-ctx.Done()
	return feather.Rejected(feather.ContextCode(ctx), 0, false, feather.ContextErr(ctx))
}

func TestCritical_DeadlineEndsAttempt(t *testing.T) {
	c := clock.NewFake(t0)
	b := &blocking{entered: make(chan struct{}, 1)}
	done := make(chan core.Report, 1)
	go func() {
		done <- delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: b}}, options(c))
	}()
	<-b.entered
	c.Advance(10 * time.Second)
	r := <-done
	res := r.Results[0]
	if res.Status != core.StatusFailed || res.Error.Code != core.ErrCodeDeadline {
		t.Fatalf("result = %+v", res)
	}
	if r.Canceled {
		t.Fatal("a deadline is not a cancellation")
	}
}

// The deadline covers the waits between attempts: a retry whose backoff
// still fits is made, the next is not, and nothing runs past the deadline.
func TestCritical_DeadlineCoversRetriesAndBackoff(t *testing.T) {
	c := clock.NewFake(t0)
	opt := options(c)
	opt.Timeout = time.Second
	opt.MaxRetries = 10
	opt.Backoff = delivery.Backoff{Base: 400 * time.Millisecond, Max: 400 * time.Millisecond, Rand: func() float64 { return 1 }}
	f := &scripted{outcomes: []feather.Outcome{retryable(503)}}
	r := drive(t, c, func() core.Report {
		return delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: f}}, opt)
	})
	res := r.Results[0]
	// attempts at 0, 400ms, 800ms; the wait after the third (400ms) does not
	// fit the remaining 200ms
	if f.calls.Load() != 3 || res.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", f.calls.Load())
	}
	if elapsed := c.Now().Sub(t0); elapsed > time.Second {
		t.Fatalf("the run ended %s after its start, past the 1s deadline", elapsed)
	}
	if res.Status != core.StatusFailed {
		t.Fatalf("status = %s", res.Status)
	}
}

// Targets queued behind a busy pool when the deadline passes fail without
// an attempt.
func TestCritical_DeadlineBeforeFirstAttempt(t *testing.T) {
	c := clock.NewFake(t0)
	opt := options(c)
	opt.Concurrency = 1
	b := &blocking{entered: make(chan struct{}, 1)}
	never := &scripted{outcomes: []feather.Outcome{ok()}}
	done := make(chan core.Report, 1)
	go func() {
		done <- delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: b}, {Name: "b", Feather: never}}, opt)
	}()
	<-b.entered
	c.Advance(10 * time.Second)
	r := <-done
	if never.calls.Load() != 0 {
		t.Fatal("a target was attempted after the deadline")
	}
	res := r.Results[1]
	if res.Status != core.StatusFailed || res.Attempts != 0 || res.Acceptance != core.AcceptanceNone || res.Error == nil || res.Error.Code != core.ErrCodeDeadline {
		t.Fatalf("result = %+v", res)
	}
}

func TestCritical_Cancellation(t *testing.T) {
	c := clock.NewFake(t0)
	opt := options(c)
	opt.Concurrency = 1
	b := &blocking{entered: make(chan struct{}, 1)}
	never := &scripted{outcomes: []feather.Outcome{ok()}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan core.Report, 1)
	go func() {
		done <- delivery.Run(ctx, msg, []delivery.Target{{Name: "a", Feather: b}, {Name: "b", Feather: never}}, opt)
	}()
	<-b.entered
	cancel()
	r := <-done
	if !r.Canceled || r.Outcome != core.OutcomeCanceled {
		t.Fatalf("report = %+v", r)
	}
	for _, res := range r.Results {
		if res.Error == nil || res.Error.Code != core.ErrCodeCanceled {
			t.Fatalf("results = %+v", r.Results)
		}
	}
	if never.calls.Load() != 0 {
		t.Fatal("a target was attempted after the cancellation")
	}
}

// A cancellation during the wait for a retry ends the wait at once.
func TestCritical_CancellationDuringBackoff(t *testing.T) {
	c := clock.NewFake(t0)
	f := &scripted{outcomes: []feather.Outcome{retryable(503)}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan core.Report, 1)
	go func() {
		done <- delivery.Run(ctx, msg, []delivery.Target{{Name: "a", Feather: f}}, options(c))
	}()
	wait, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	if !c.BlockUntil(wait, 2) { // the deadline and the backoff
		t.Fatal("no backoff wait")
	}
	cancel()
	r := <-done
	if f.calls.Load() != 1 {
		t.Fatalf("attempts = %d", f.calls.Load())
	}
	res := r.Results[0]
	if res.Error.Code != core.ErrCodeCanceled || res.Acceptance != core.AcceptanceRejected {
		t.Fatalf("result = %+v", res)
	}
}

// gate records how many attempts run at the same time.
type gate struct {
	active, max atomic.Int32
	entered     chan struct{}
	release     chan struct{}
}

func (g *gate) Deliver(ctx context.Context, _ core.Message) feather.Outcome {
	n := g.active.Add(1)
	for {
		m := g.max.Load()
		if n <= m || g.max.CompareAndSwap(m, n) {
			break
		}
	}
	g.entered <- struct{}{}
	<-g.release
	g.active.Add(-1)
	return feather.Confirmed(200)
}

func TestCritical_WorkerPoolConcurrencyLimit(t *testing.T) {
	for _, tc := range []struct{ targets, limit int }{{20, 3}, {5, 5}, {3, 8}, {7, 1}} {
		t.Run(fmt.Sprintf("%d_targets_limit_%d", tc.targets, tc.limit), func(t *testing.T) {
			c := clock.NewFake(t0)
			g := &gate{entered: make(chan struct{}, tc.targets), release: make(chan struct{})}
			targets := make([]delivery.Target, tc.targets)
			for i := range targets {
				targets[i] = delivery.Target{Name: fmt.Sprint(i), Feather: g}
			}
			opt := options(c)
			opt.Concurrency = tc.limit
			done := make(chan core.Report, 1)
			go func() { done <- delivery.Run(context.Background(), msg, targets, opt) }()

			full := min(tc.limit, tc.targets)
			for i := 0; i < full; i++ {
				<-g.entered // the pool fills up to its limit
			}
			if got := g.active.Load(); got != int32(full) {
				t.Fatalf("active = %d, want %d", got, full)
			}
			// Every attempt holds its slot until released: a pool without a
			// limit starts the next target at once. Wait for that a moment
			// (real time only bounds the detection; a correct pool always
			// passes).
			if tc.targets > full {
				select {
				case <-g.entered:
					t.Fatalf("a target started while %d attempts held the %d slots", full, tc.limit)
				case <-time.After(200 * time.Millisecond):
				}
			}
			// one release lets exactly one more in
			for i := 0; i < tc.targets; i++ {
				g.release <- struct{}{}
				if i+full < tc.targets {
					<-g.entered
				}
				if got := g.active.Load(); got > int32(tc.limit) {
					t.Fatalf("active = %d, over the limit %d", got, tc.limit)
				}
			}
			r := <-done
			if got := g.max.Load(); got != int32(full) {
				t.Fatalf("max concurrent attempts = %d, want %d", got, full)
			}
			if r.Summary.Delivered != tc.targets {
				t.Fatalf("delivered = %d", r.Summary.Delivered)
			}
		})
	}
}

// ordered completes its attempts in the order the test releases them.
type ordered struct {
	name    string
	started chan string
	release chan struct{}
	outcome feather.Outcome
}

func (o *ordered) Deliver(ctx context.Context, _ core.Message) feather.Outcome {
	o.started <- o.name
	<-o.release
	return o.outcome
}

// The results follow the order of the targets whatever the order in which
// the deliveries finish, skipped targets included.
func TestCritical_ResultOrdering(t *testing.T) {
	c := clock.NewFake(t0)
	started := make(chan string, 8)
	names := []string{"a", "b", "c", "d"}
	fs := map[string]*ordered{}
	var targets []delivery.Target
	for i, n := range names {
		o := &ordered{name: n, started: started, release: make(chan struct{}), outcome: ok()}
		if i%2 == 1 {
			o.outcome = feather.Rejected(core.ErrCodeRejected, 400, false, errors.New("HTTP 400"))
		}
		fs[n] = o
		targets = append(targets, delivery.Target{Name: n, Type: "slack", Feather: o})
		if i == 1 {
			targets = append(targets, delivery.Target{Name: "skipped", Type: "discord", Skip: &core.SkipInfo{Reason: "disabled", Group: "g"}})
		}
	}
	done := make(chan core.Report, 1)
	go func() { done <- delivery.Run(context.Background(), msg, targets, options(c)) }()
	for range names {
		<-started
	}
	// finish in reverse order
	for i := len(names) - 1; i >= 0; i-- {
		fs[names[i]].release <- struct{}{}
	}
	r := <-done
	want := []string{"a", "b", "skipped", "c", "d"}
	for i, res := range r.Results {
		if res.Feather != want[i] {
			t.Fatalf("result %d = %s, want %s (order %v)", i, res.Feather, want[i], want)
		}
	}
	if r.Results[2].Status != core.StatusSkipped || r.Results[2].Skipped.Group != "g" {
		t.Fatalf("skipped = %+v", r.Results[2])
	}
	if r.Results[1].Status != core.StatusFailed || r.Results[0].Status != core.StatusDelivered {
		t.Fatalf("results = %+v", r.Results)
	}
}

// panicking is an adapter with a bug.
type panicking struct{}

func (panicking) Deliver(context.Context, core.Message) feather.Outcome { panic("boom") }

// One target's failure, even a crash of its adapter, does not stop the
// others.
func TestCritical_FailureIsolation(t *testing.T) {
	c := clock.NewFake(t0)
	good := &scripted{outcomes: []feather.Outcome{ok()}}
	bad := &scripted{outcomes: []feather.Outcome{feather.Rejected(core.ErrCodeRejected, 403, false, errors.New("HTTP 403"))}}
	targets := []delivery.Target{{Name: "bad", Feather: bad}, {Name: "crash", Feather: panicking{}}, {Name: "good", Feather: good}}
	r := run(t, context.Background(), targets, options(c))
	if r.Results[2].Status != core.StatusDelivered {
		t.Fatalf("good = %+v", r.Results[2])
	}
	if r.Results[1].Status != core.StatusFailed || r.Results[1].Acceptance != core.AcceptanceUnknown {
		t.Fatalf("crash = %+v", r.Results[1])
	}
	if r.Outcome != core.OutcomePartial || r.Summary.Delivered != 1 || r.Summary.Failed != 2 {
		t.Fatalf("report = %+v", r)
	}
}

// leaky puts a secret into its error, as a careless adapter might.
type leaky struct{ secret string }

func (l leaky) Deliver(context.Context, core.Message) feather.Outcome {
	return feather.Rejected(core.ErrCodeNetwork, 0, false, fmt.Errorf("Post %q: connection refused", l.secret))
}

func TestErrorsAreRedacted(t *testing.T) {
	c := clock.NewFake(t0)
	secret := "https://hooks.slack.com/services/T000/B000/XXXXSECRET"
	red := &credential.Redactor{}
	red.Add(credential.NewSecret(secret))
	opt := options(c)
	opt.Redactor = red
	r := run(t, context.Background(), []delivery.Target{{Name: "a", Feather: leaky{secret}}}, opt)
	for _, m := range []string{r.Results[0].Error.Message, r.Results[0].History[0].Error.Message} {
		if containsAny(m, secret, "XXXXSECRET", "T000/B000") {
			t.Fatalf("secret in %q", m)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func TestBackoffDelay(t *testing.T) {
	b := delivery.Backoff{Base: 100 * time.Millisecond, Max: time.Second, Rand: func() float64 { return 0 }}
	for n, want := range map[int]time.Duration{1: 50 * time.Millisecond, 2: 100 * time.Millisecond, 3: 200 * time.Millisecond, 5: 500 * time.Millisecond, 9: 500 * time.Millisecond} {
		if got := b.Delay(n); got != want {
			t.Errorf("Delay(%d) = %s, want %s", n, got, want)
		}
	}
	b.Rand = func() float64 { return 0.999999 }
	if got := b.Delay(10); got > time.Second || got < 999*time.Millisecond {
		t.Errorf("Delay(10) = %s", got)
	}
}

func TestEmptyRun(t *testing.T) {
	r := run(t, context.Background(), nil, options(clock.NewFake(t0)))
	if r.Outcome != core.OutcomeSuccess || len(r.Results) != 0 {
		t.Fatalf("report = %+v", r)
	}
}
