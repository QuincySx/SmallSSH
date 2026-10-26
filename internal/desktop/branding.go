package desktop

import (
	"github.com/egoist/mygo/ui"
	workbench "sshclient"
)

const applicationName = "SmallSSH"

// Decode once, rather than loading an image on each approval frame.
var appIcon, _ = ui.DecodeBitmap(workbench.AppIconPNG)
