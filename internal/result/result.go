// Package result accumulates the summary table and failure count.
package result

import (
	"fmt"
	"sync"

	"mins/internal/logx"
)

// Row is one summary-table line.
type Row struct {
	Output string
	Mode   string
	Q      string
	VMAF   string
	Size   string
	Status string
}

// Table is a concurrency-safe, index-addressed summary table.
type Table struct {
	mu     sync.Mutex
	rows   map[int]Row
	next   int
	failed int
}

// New returns an empty table.
func New() *Table { return &Table{rows: map[int]Row{}} }

// Add appends a row at the next sequential index.
func (t *Table) Add(r Row) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows[t.next] = r
	t.next++
}

// Set writes the row for a specific input index.
func (t *Table) Set(idx int, r Row) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows[idx] = r
	if idx >= t.next {
		t.next = idx + 1
	}
}

// Fail increments the failure count.
func (t *Table) Fail() {
	t.mu.Lock()
	t.failed++
	t.mu.Unlock()
}

// Failed returns the failure count.
func (t *Table) Failed() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

// Next returns the next sequential index.
func (t *Table) Next() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.next
}

// Print writes the table to stdout and the run log.
func (t *Table) Print(log *logx.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Println()
	log.Append("---- summary ----")
	hdr := RowFormat("OUTPUT", "MODE", "Q", "VMAF", "SIZE", "STATUS")
	fmt.Println(hdr)
	log.Append(hdr)
	for i := 0; i < t.next; i++ {
		r, ok := t.rows[i]
		if !ok {
			continue
		}
		line := RowFormat(r.Output, r.Mode, r.Q, r.VMAF, r.Size, r.Status)
		fmt.Println(line)
		log.Append(line)
	}
	log.Append(fmt.Sprintf("==== run end: %d failure(s) ====", t.failed))
}

// RowFormat reproduces mins.sh's row_fmt widths.
func RowFormat(a, b, c, d, e, f string) string {
	return fmt.Sprintf("%-42s %-5s %-5s %-9s %-24s %s", a, b, c, d, e, f)
}
