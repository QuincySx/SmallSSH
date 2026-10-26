package desktop

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

func BenchmarkFileTreeFrame(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := &app{}
			a.ensurePanes()
			p := a.primaryFilePane
			p.endpoint = "benchmark"
			state := p.browser()
			state.dir = "/"
			state.seq = 1
			entries := make([]sshconn.File, count)
			for i := range entries {
				entries[i] = sshconn.File{Name: fmt.Sprintf("directory-%06d", i), Directory: true}
			}
			a.applyDirectory(state, false, "/", entries, 1, true)
			tt := ui.NewTester(func(c *ui.Context) { a.directoryTree(c, p, state) }, 240, 600)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tt.Frame()
			}
		})
	}
}
