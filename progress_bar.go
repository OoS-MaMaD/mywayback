package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Simple progress bar inspired by elulcao/progress-bar but implemented locally
// to avoid external dependencies. It prints to stderr and keeps the last line
// as the progress bar.

type PBar struct {
	Total      int
	Width      int
	DoneStr    string
	OngoingStr string
	mu         sync.Mutex
}

func NewPBar(total int) *PBar {
	p := &PBar{
		Total:      total,
		Width:      40,
		DoneStr:    "#",
		OngoingStr: ".",
	}
	return p
}

// Render updates the progress bar to the given current value.
func (p *PBar) Render(curr int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Total <= 0 {
		fmt.Fprintf(os.Stderr, "\rProgress: %d/%d", curr, p.Total)
		return
	}
	if curr > p.Total {
		curr = p.Total
	}
	done := int(float64(curr) * float64(p.Width) / float64(p.Total))
	if done > p.Width {
		done = p.Width
	}
	bar := strings.Repeat(p.DoneStr, done) + strings.Repeat(p.OngoingStr, p.Width-done)
	fmt.Fprintf(os.Stderr, "\r[%s] %d/%d (%.1f%%)", bar, curr, p.Total, float64(curr)/float64(p.Total)*100)
}

// ClearLine erases the current progress line so other output can be printed.
func (p *PBar) ClearLine() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// ANSI escape to clear line
	fmt.Fprint(os.Stderr, "\r\033[K")
}

// Finish moves to the next line (call when done)
func (p *PBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
}
