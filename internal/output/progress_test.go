package output

import (
	"testing"
)

func TestSpinner_New(t *testing.T) {
	c, _ := NewColorHelper("never")
	s := NewSpinner(c, "loading")
	if s == nil {
		t.Fatal("NewSpinner() returned nil")
	}
	if s.message != "loading" {
		t.Errorf("message = %q, want %q", s.message, "loading")
	}
	if len(s.frames) == 0 {
		t.Error("frames is empty")
	}
}

func TestSpinner_StartStop(t *testing.T) {
	c, _ := NewColorHelper("never")
	s := NewSpinner(c, "test")
	s.Start()
	s.Stop()
	// If we get here without panic, it's good
}

func TestProgressBar_SetTotal(t *testing.T) {
	c, _ := NewColorHelper("never")
	pb := NewProgressBar(c, "test", 10)
	pb.SetTotal(20)
	if pb.total != 20 {
		t.Errorf("total = %d, want 20", pb.total)
	}
}

func TestProgressBar_Advance(t *testing.T) {
	c, _ := NewColorHelper("never")
	pb := NewProgressBar(c, "test", 10)
	pb.Advance()
	if pb.current != 1 {
		t.Errorf("current = %d, want 1", pb.current)
	}
	pb.Advance()
	pb.Advance()
	if pb.current != 3 {
		t.Errorf("current = %d, want 3", pb.current)
	}
}

func TestAtomicCounter(t *testing.T) {
	c := NewAtomicCounter()

	if c.Value() != 0 {
		t.Errorf("initial value = %d, want 0", c.Value())
	}

	c.Increment()
	c.Increment()
	c.Increment()

	if c.Value() != 3 {
		t.Errorf("value = %d, want 3", c.Value())
	}

	c.Reset()
	if c.Value() != 0 {
		t.Errorf("after reset, value = %d, want 0", c.Value())
	}
}
