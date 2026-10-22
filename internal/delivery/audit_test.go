package delivery_test

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather"
)

// Options without a backoff still wait between attempts: a zero Backoff
// must not turn retries into a storm.
func TestZeroBackoffStillWaits(t *testing.T) {
	c := clock.NewFake(t0)
	opt := options(c)
	opt.Backoff = delivery.Backoff{}
	f := &scripted{outcomes: []feather.Outcome{retryable(503)}}
	r := drive(t, c, func() core.Report {
		return delivery.Run(context.Background(), msg, []delivery.Target{{Name: "a", Feather: f}}, opt)
	})
	for _, a := range r.Results[0].History[:len(r.Results[0].History)-1] {
		if a.Wait <= 0 {
			t.Fatalf("attempt %d was followed by no wait", a.Number)
		}
	}
}

type panicSecret struct{ s string }

func (p panicSecret) Deliver(context.Context, core.Message) feather.Outcome { panic("bad " + p.s) }

// A panic message goes through the redactor too.
func TestPanicMessageRedacted(t *testing.T) {
	red := &credential.Redactor{}
	red.Add(credential.NewSecret("https://hooks.example/T9/B9/PANICSECRET"))
	opt := options(clock.NewFake(t0))
	opt.Redactor = red
	r := run(t, context.Background(), []delivery.Target{{Name: "a", Feather: panicSecret{"https://hooks.example/T9/B9/PANICSECRET"}}}, opt)
	if strings.Contains(r.Results[0].Error.Message, "PANICSECRET") {
		t.Fatalf("message = %s", r.Results[0].Error.Message)
	}
}
