package api

import (
	"strconv"
	"testing"
	"time"
)

// Login attempts are limited per client IP through allow(). Every key it ever
// saw used to stay in the map; once it is large, stale keys must be swept so
// a stream of distinct addresses cannot grow it without bound.
func TestLimiterForgetsStaleKeys(t *testing.T) {
	l := newLimiter(3, 10*time.Millisecond)
	for wave := range 3 {
		for i := range sweepAbove {
			l.allow(strconv.Itoa(wave) + "-" + strconv.Itoa(i))
		}
		time.Sleep(20 * time.Millisecond)
	}
	l.allow("fresh")
	if n := len(l.hits); n > sweepAbove+1 {
		t.Fatalf("%d keys kept; stale ones were never swept", n)
	}
}

func TestLimiterAllow(t *testing.T) {
	l := newLimiter(2, 50*time.Millisecond)
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Fatal("the third attempt inside the window should be refused")
	}
	if !l.allow("b") {
		t.Fatal("keys are independent")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.allow("a") {
		t.Fatal("allowed again once the window passed")
	}
}
