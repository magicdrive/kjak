package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestFakeAdvanceFiresInOrder(t *testing.T) {
	c := NewFake(t0)
	got := make(chan int, 3)
	c.AfterFunc(3*time.Second, func() { got <- 3 })
	c.AfterFunc(time.Second, func() { got <- 1 })
	stop := c.AfterFunc(2*time.Second, func() { got <- 2 })
	if !stop() {
		t.Fatal("stop of a pending timer")
	}
	c.Advance(time.Second)
	if v := <-got; v != 1 {
		t.Fatalf("fired %d", v)
	}
	if d, ok := c.NextIn(); !ok || d != 2*time.Second {
		t.Fatalf("next in %s", d)
	}
	c.Advance(5 * time.Second)
	if v := <-got; v != 3 {
		t.Fatalf("fired %d", v)
	}
	if c.Pending() != 0 || !c.Now().Equal(t0.Add(6*time.Second)) {
		t.Fatal("state after advance")
	}
}

func TestSleepAndDeadline(t *testing.T) {
	c := NewFake(t0)
	cause := errors.New("deadline")
	ctx, cancel := WithDeadline(context.Background(), c, 10*time.Second, cause)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Sleep(ctx, c, time.Minute) }()
	wait, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if !c.BlockUntil(wait, 2) {
		t.Fatal("no timers")
	}
	c.Advance(10 * time.Second)
	if err := <-done; !errors.Is(err, cause) {
		t.Fatalf("sleep = %v", err)
	}
	if !errors.Is(context.Cause(ctx), cause) {
		t.Fatal("cause")
	}
	if err := Sleep(context.Background(), c, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRealClock(t *testing.T) {
	var c Clock = Real{}
	if c.Now().IsZero() {
		t.Fatal("zero time")
	}
	fired := make(chan struct{})
	c.AfterFunc(0, func() { close(fired) })
	<-fired
}
