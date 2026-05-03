package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPLimiter_AllowUnderLimit(t *testing.T) {
	// 60 req/min, burst 5 — first 5 calls should pass immediately.
	rl := newIPLimiter(60, 5, 15*time.Minute)
	for i := 0; i < 5; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("call %d should be allowed (under burst)", i+1)
		}
	}
}

func TestIPLimiter_BlocksWhenExceeded(t *testing.T) {
	// 60 req/min, burst 1 — second call immediately must be blocked.
	rl := newIPLimiter(60, 1, 15*time.Minute)
	if !rl.allow("10.0.0.1") {
		t.Fatal("first call should pass")
	}
	if rl.allow("10.0.0.1") {
		t.Fatal("second call should be blocked after burst=1 is exhausted")
	}
}

func TestClientIP_SingleXFF(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	if ip := clientIP(r); ip != "203.0.113.5" {
		t.Errorf("expected 203.0.113.5, got %s", ip)
	}
}

func TestClientIP_CommaSeparatedXFF(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1, 172.16.0.1")
	if ip := clientIP(r); ip != "203.0.113.5" {
		t.Errorf("expected first hop 203.0.113.5, got %s", ip)
	}
}

func TestClientIP_FallbackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.42:54321"
	if ip := clientIP(r); ip != "192.168.1.42" {
		t.Errorf("expected 192.168.1.42, got %s", ip)
	}
}

func TestRateLimiterMiddleware_Returns429WhenOverQuota(t *testing.T) {
	rl := newIPLimiter(60, 1, 15*time.Minute)
	// Consume the single burst token.
	rl.allow("5.5.5.5")

	handler := rl.middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.5.5.5:1234"
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimiterMiddleware_CallsNextWhenUnderQuota(t *testing.T) {
	rl := newIPLimiter(60, 5, 15*time.Minute)

	called := false
	handler := rl.middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "6.6.6.6:1234"
	w := httptest.NewRecorder()
	handler(w, r)

	if !called {
		t.Error("expected next handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
