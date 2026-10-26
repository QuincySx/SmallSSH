package dockerlog

import (
	"strings"
	"sync"
	"testing"
)

func TestLogBufferStripsSplitControlsAndBoundsLongOutput(t *testing.T) {
	b := &Buffer{}
	for _, chunk := range []string{"\x1b[3", "2mhello\x1b[0m ", "\x1b]52;c;private", "\x1b\\world\n", "中", "文\n"} {
		b.Write([]byte(chunk))
	}
	rows, _, _ := b.Snapshot()
	if strings.Join(rows, "\n") != "hello world\n中文" {
		t.Fatal(rows)
	}
	b.Write([]byte(strings.Repeat("x", 1<<20) + "\n" + strings.Repeat("line\n", 10000)))
	rows, _, trimmed := b.Snapshot()
	if !trimmed || len(rows) > 4001 || len(strings.Join(rows, "\n")) > 512<<10 {
		t.Fatal("log cache is unbounded", len(rows))
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				b.Write([]byte("row\n"))
				b.Snapshot()
			}
		})
	}
	wg.Wait()
}
