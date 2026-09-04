package output

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Spinner provides a simple animated spinner for progress indication.
type Spinner struct {
	colors  *ColorHelper
	message string
	stopCh  chan struct{}
	stopWg  sync.WaitGroup
	running bool
	frames  []string
	index   int
	mu      sync.Mutex
}

// NewSpinner creates a new Spinner with the given colors and message.
func NewSpinner(colors *ColorHelper, message string) *Spinner {
	return &Spinner{
		colors:  colors,
		message: message,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stopCh:  make(chan struct{}),
	}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.stopWg.Add(1)
	go func() {
		defer s.stopWg.Done()
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.mu.Lock()
				if !s.running {
					s.mu.Unlock()
					return
				}
				frame := s.frames[s.index%len(s.frames)]
				msg := s.message
				if s.colors != nil && s.colors.Enabled() {
					frame = s.colors.Blue(frame)
				}
				fmt.Printf("\r%s %s", frame, msg)
				s.index++
				s.mu.Unlock()
			}
		}
	}()
}

// Stop ends the spinner animation.
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	s.stopWg.Wait()
	fmt.Printf("\r\033[K") // Clear line
}

// ProgressBar provides a simple text-based progress bar.
type ProgressBar struct {
	colors  *ColorHelper
	total   int64
	current int64
	mu      sync.Mutex
	label   string
}

// NewProgressBar creates a new ProgressBar.
func NewProgressBar(colors *ColorHelper, label string, total int) *ProgressBar {
	return &ProgressBar{
		colors:  colors,
		total:   int64(total),
		label:   label,
	}
}

// SetTotal sets the total number of items.
func (p *ProgressBar) SetTotal(total int) {
	p.mu.Lock()
	p.total = int64(total)
	p.mu.Unlock()
}

// Advance increments the current count by one.
func (p *ProgressBar) Advance() {
	p.mu.Lock()
	p.current++
	p.mu.Unlock()
	p.render()
}

// Set sets the current count to a specific value.
func (p *ProgressBar) Set(current int) {
	p.mu.Lock()
	p.current = int64(current)
	p.mu.Unlock()
	p.render()
}

func (p *ProgressBar) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	current := p.current
	total := p.total
	if total == 0 {
		return
	}

	percent := float64(current) / float64(total)
	barWidth := 40
	filled := int(float64(barWidth) * percent)

	// Use ASCII for portability
	bar := make([]rune, barWidth+2)
	bar[0] = '['
	for i := 1; i <= filled; i++ {
		bar[i] = '='
	}
	for i := filled + 1; i < barWidth+1; i++ {
		bar[i] = '-'
	}
	bar[barWidth+1] = ']'

	fmt.Printf("\r%s %s %3.0f%%", p.label, string(bar), percent*100)
}

// Done clears the progress bar.
func (p *ProgressBar) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r\033[K") // Clear line
}

// AtomicCounter is a simple thread-safe counter.
type AtomicCounter struct {
	count int64
}

// NewAtomicCounter creates a new AtomicCounter starting at 0.
func NewAtomicCounter() *AtomicCounter {
	return &AtomicCounter{}
}

// Increment atomically increments the counter and returns the new value.
func (c *AtomicCounter) Increment() int64 {
	return atomic.AddInt64(&c.count, 1)
}

// Value returns the current counter value.
func (c *AtomicCounter) Value() int64 {
	return atomic.LoadInt64(&c.count)
}

// Reset atomically resets the counter to 0.
func (c *AtomicCounter) Reset() {
	atomic.StoreInt64(&c.count, 0)
}
