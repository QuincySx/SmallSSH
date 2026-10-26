package components

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/localize"
)

func tr(c *ui.Context, source string) string {
	return localize.Text(c.Locale(), source)
}
