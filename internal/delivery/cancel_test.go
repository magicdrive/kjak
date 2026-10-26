package delivery_test

import (
	"context"
	"testing"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather"
)

// cancelingFeather is refused (retryable) by an attempt during which the
// send is canceled (a SIGINT while the request is out).
type cancelingFeather struct{ cancel context.CancelFunc }

func (f cancelingFeather) Deliver(context.Context, core.Message) feather.Outcome {
	f.cancel()
	return retryable(503)
}

// A send canceled during an attempt that may be retried ends with that
// attempt: no wait for a retry is started (none is recorded), the result
// is canceled and keeps the attempt's acceptance.
func TestCritical_CancellationDuringAttemptStartsNoWait(t *testing.T) {
	c := clock.NewFake(t0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := delivery.Run(ctx, msg, []delivery.Target{{Name: "a", Feather: cancelingFeather{cancel}}}, options(c))
	res := r.Results[0]
	if !r.Canceled || res.Attempts != 1 || len(res.History) != 1 {
		t.Fatalf("report = %+v", r)
	}
	if res.History[0].Wait != 0 {
		t.Fatalf("a wait of %s was started after the cancellation", res.History[0].Wait)
	}
	if res.Error == nil || res.Error.Code != core.ErrCodeCanceled || res.Acceptance != core.AcceptanceRejected || res.Status != core.StatusFailed {
		t.Fatalf("result = %+v", res)
	}
}
