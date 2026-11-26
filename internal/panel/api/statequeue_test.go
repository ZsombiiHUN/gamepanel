package api

import (
	"strconv"
	"testing"
	"time"
)

// A node's connection hands states over without ever waiting for the
// database: pushes never block, keep their order, and stop at the cap.
func TestStateQueue(t *testing.T) {
	q := stateQueue{wake: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		for i := range 1000 { // nobody is taking: must not block
			q.push([2]string{"s", strconv.Itoa(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("push blocked")
	}
	<-q.wake
	got := q.take()
	if len(got) != 1000 || got[0][1] != "0" || got[999][1] != "999" {
		t.Fatalf("got %d states, first %v last %v", len(got), got[0], got[len(got)-1])
	}
	if len(q.take()) != 0 {
		t.Fatal("take did not empty the queue")
	}
	for range maxQueuedStates {
		q.push([2]string{"s", "x"})
	}
	if q.push([2]string{"s", "over"}) {
		t.Fatal("the queue grew past its cap")
	}
}
