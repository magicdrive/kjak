// Package clock is the time source of the delivery engine. Tests use Fake,
// which moves only when the test advances it: no test waits for real time.
package clock

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Clock tells the time and runs functions after a duration.
type Clock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine after d. stop cancels the call
	// and reports whether it did (false: f ran or runs already).
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// Sleep waits d on c, or until ctx is done (it then returns ctx's cause).
func Sleep(ctx context.Context, c Clock, d time.Duration) error {
	if d <= 0 {
		return context.Cause(ctx)
	}
	done := make(chan struct{})
	stop := c.AfterFunc(d, func() { close(done) })
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		stop()
		return context.Cause(ctx)
	}
}

// WithDeadline returns a context canceled with cause after d on c. It is the
// deadline of a delivery: on a Fake clock it passes when the test advances
// the clock past it.
func WithDeadline(parent context.Context, c Clock, d time.Duration, cause error) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := c.AfterFunc(d, func() { cancel(cause) })
	return ctx, func() {
		stop()
		cancel(context.Canceled)
	}
}

// Fake is a manually advanced clock.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	seq     int
	changed chan struct{} // closed and replaced whenever timers change
}

type fakeTimer struct {
	at  time.Time
	seq int
	f   func()
}

// NewFake returns a Fake clock at t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t, changed: make(chan struct{})}
}

func (c *Fake) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Fake) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	t := &fakeTimer{at: c.now.Add(d), seq: c.seq, f: f}
	if d <= 0 {
		go f()
		return func() bool { return false }
	}
	c.timers = append(c.timers, t)
	c.notifyLocked()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, x := range c.timers {
			if x == t {
				c.timers = append(c.timers[:i], c.timers[i+1:]...)
				c.notifyLocked()
				return true
			}
		}
		return false
	}
}

func (c *Fake) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// Advance moves the clock d forward and runs the timers that become due, in
// the order of their due time.
func (c *Fake) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	sort.SliceStable(c.timers, func(i, j int) bool {
		if c.timers[i].at.Equal(c.timers[j].at) {
			return c.timers[i].seq < c.timers[j].seq
		}
		return c.timers[i].at.Before(c.timers[j].at)
	})
	var due []*fakeTimer
	for len(c.timers) > 0 && !c.timers[0].at.After(c.now) {
		due = append(due, c.timers[0])
		c.timers = c.timers[1:]
	}
	if len(due) > 0 {
		c.notifyLocked()
	}
	c.mu.Unlock()
	for _, t := range due {
		go t.f()
	}
}

// Pending is the number of timers that are not due yet.
func (c *Fake) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// BlockUntil waits until at least n timers are pending, or ctx is done
// (it then returns false).
func (c *Fake) BlockUntil(ctx context.Context, n int) bool {
	for {
		c.mu.Lock()
		count, changed := len(c.timers), c.changed
		c.mu.Unlock()
		if count >= n {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

// NextIn is the duration until the earliest pending timer (false: none).
func (c *Fake) NextIn() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) == 0 {
		return 0, false
	}
	earliest := c.timers[0].at
	for _, t := range c.timers[1:] {
		if t.at.Before(earliest) {
			earliest = t.at
		}
	}
	return earliest.Sub(c.now), true
}
