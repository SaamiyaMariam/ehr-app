package auth

import (
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	for i := 0; i < maxLoginFailures-1; i++ {
		l.fail("a@example.test", now)
		if l.blocked("a@example.test", now) {
			t.Fatalf("blocked after only %d failures", i+1)
		}
	}

	l.fail("a@example.test", now)
	if !l.blocked("a@example.test", now.Add(time.Minute)) {
		t.Fatal("not blocked after the maximum number of failures")
	}

	// Another account is unaffected.
	if l.blocked("b@example.test", now) {
		t.Fatal("a different email must not be blocked")
	}

	// The lockout ends when the window passes.
	if l.blocked("a@example.test", now.Add(loginWindow+time.Second)) {
		t.Fatal("still blocked after the window")
	}

	// A successful sign-in clears the counter.
	for i := 0; i < maxLoginFailures; i++ {
		l.fail("c@example.test", now)
	}
	l.reset("c@example.test")
	if l.blocked("c@example.test", now) {
		t.Fatal("reset must clear the failures")
	}
}

func TestLoginLimiterStaysBounded(t *testing.T) {
	l := newLoginLimiter()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 10001; i++ {
		l.fail(string(rune('a'+i%26))+time.Duration(i).String(), old)
	}

	// A later failure purges expired entries.
	l.fail("fresh@example.test", old.Add(2*loginWindow))

	if len(l.failures) > 5 {
		t.Fatalf("expired entries were not purged: %d left", len(l.failures))
	}
}

func TestLoginLimiterNeverExceedsItsCapWithFreshEntries(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 3*maxTrackedLogins; i++ {
		l.fail(limiterKey(time.Duration(i).String()+"@example.test"), now)
	}

	if len(l.failures) > maxTrackedLogins {
		t.Fatalf("%d entries tracked; the cap is %d", len(l.failures), maxTrackedLogins)
	}
}

func TestLimiterKeyIsFixedSizeWhateverTheInput(t *testing.T) {
	short, long := limiterKey("a@example.test"), limiterKey(string(make([]byte, 1<<20)))

	if len(short) != len(long) || short == long {
		t.Fatalf("keys: %d vs %d bytes", len(short), len(long))
	}
}
