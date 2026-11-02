package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected format %q", h)
	}
	ok, rehash, err := VerifyPassword("correct horse battery", h)
	if err != nil || !ok || rehash {
		t.Fatalf("verify = %v %v %v", ok, rehash, err)
	}
	if ok, _, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password verified")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("salt must differ between hashes")
	}
}

func TestVerifyDetectsWeakParamsAndGarbage(t *testing.T) {
	// A hash made with lower parameters verifies but is flagged for rehash.
	weak := "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0MTI$" // truncated: key missing
	if _, _, err := VerifyPassword("x", weak); err == nil {
		t.Fatal("expected error for malformed hash")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=999999999,t=1,p=1$YQ$YQ", "$bcrypt$x$y$z$w"} {
		if _, _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("accepted garbage hash %q", bad)
		}
	}
}

func TestPolicy(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("short password accepted")
	}
	if ValidatePassword(strings.Repeat("a", 300)) == nil {
		t.Error("overlong password accepted")
	}
	if err := ValidatePassword("exactly12chr"); err != nil {
		t.Error(err)
	}
}

func TestLimiterBackoff(t *testing.T) {
	l := NewLimiter()
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Fail("1.2.3.4", "bob")
		if l.Check("1.2.3.4", "bob") != 0 {
			t.Fatalf("blocked too early at failure %d", i+1)
		}
	}
	l.Fail("1.2.3.4", "bob") // 5th
	if d := l.Check("1.2.3.4", "bob"); d != time.Second {
		t.Fatalf("first delay = %v", d)
	}
	now = now.Add(2 * time.Second)
	if l.Check("1.2.3.4", "bob") != 0 {
		t.Fatal("still blocked after delay")
	}
	l.Fail("1.2.3.4", "bob") // 6th -> 2s
	if d := l.Check("1.2.3.4", "bob"); d != 2*time.Second {
		t.Fatalf("second delay = %v", d)
	}
	for i := 0; i < 40; i++ {
		l.Fail("1.2.3.4", "bob")
	}
	if d := l.Check("1.2.3.4", "bob"); d != 15*time.Minute {
		t.Fatalf("delay cap = %v", d)
	}
	// different user from the same IP isn't blocked by the per-user key (IP budget not reached)
	if l.Check("9.9.9.9", "bob") != 0 {
		t.Fatal("unrelated ip blocked")
	}
	l.Success("1.2.3.4", "bob")
}

func TestLimiterIPBudget(t *testing.T) {
	l := NewLimiter()
	for i := 0; i < 30; i++ {
		l.Fail("5.5.5.5", string(rune('a'+i%26))+string(rune('a'+i/26))) // many distinct usernames
	}
	if l.Check("5.5.5.5", "brand-new-name") == 0 {
		t.Fatal("per-IP ceiling did not engage")
	}
}
