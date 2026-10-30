package osnotify

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/core"
)

// TestNativeBackend posts one real notification with the backend of this OS
// (KJAK_OS_NOTIFY_NATIVE=1; CI sets it once per OS). It checks that the OS
// program runs the script: the OS accepts the notification, or says why it
// cannot (no GUI session, notifications off). A failure of the script itself
// (a syntax or a reference error) fails the test. It cannot tell whether
// anyone saw the notification.
func TestNativeBackend(t *testing.T) {
	if os.Getenv("KJAK_OS_NOTIFY_NATIVE") != "1" {
		t.Skip("set KJAK_OS_NOTIFY_NATIVE=1 to post one notification with this OS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	n := Compose(core.Message{Title: "kjak test", Body: "A test of kjak's os feather (日本語 ✅ \"quotes\" $(x) `y`)", Level: core.LevelSuccess})
	err := New(runtime.GOOS, SystemEnv()).Notify(ctx, n)
	t.Logf("%s: %v", runtime.GOOS, err)
	switch {
	case err == nil:
		t.Log("accepted by the OS (not seen by anyone here)")
	case errors.Is(err, ErrUnsupported):
		t.Fatalf("no backend for %s", runtime.GOOS)
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrDisabled):
		for _, bug := range []string{"ParserError", "ParseException", "SyntaxError", "ReferenceError", "TypeError", "CommandNotFound", "is not recognized", "Unable to find type"} {
			if strings.Contains(err.Error(), bug) {
				t.Fatalf("the script failed: %v", err)
			}
		}
		t.Logf("not shown here, for a reason the OS gave: %v", err)
	default:
		t.Fatal(err)
	}
}
