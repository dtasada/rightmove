package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// ProgressBar renders a simple single-line terminal progress bar.
type ProgressBar struct {
	totalCount   uint32
	currentCount uint32
	printMutex   sync.Mutex
}

func NewProgressBar(total uint32) *ProgressBar {
	return &ProgressBar{totalCount: total}
}

func (p *ProgressBar) Increment() {
	if p == nil {
		return
	}
	atomic.AddUint32(&p.currentCount, 1)
	p.Draw()
}

func (p *ProgressBar) Draw() {
	if p == nil {
		return
	}
	p.printMutex.Lock()
	defer p.printMutex.Unlock()

	total := atomic.LoadUint32(&p.totalCount)
	current := atomic.LoadUint32(&p.currentCount)
	if total == 0 {
		return
	}
	if current > total {
		current = total
	}
	width := 40
	filled := int(float64(current) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}
	percent := int(float64(current) / float64(total) * 100.0)
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	fmt.Printf("\r[%s] %d/%d (%d%%)", bar, current, total, percent)
	if current >= total {
		fmt.Println()
	}
}
