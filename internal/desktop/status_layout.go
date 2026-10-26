package desktop

import "sshclient/internal/desktop/components"

// A collection fills its available width before adding columns. Rich cards
// need more room than capacity or core meters; their internal layout uses the
// resulting cell width rather than the enclosing window width.
var (
	overviewCardLayout   = components.ResourceLayout{MinimumWidth: 180, MaximumColumns: 4}
	filesystemCardLayout = components.ResourceLayout{MinimumWidth: 360, MaximumColumns: 3}
	containerCardLayout  = components.ResourceLayout{MinimumWidth: 400, MaximumColumns: 2}
	networkCardLayout    = components.ResourceLayout{MinimumWidth: 420, MaximumColumns: 2}
	systemCardLayout     = components.ResourceLayout{MinimumWidth: 360, MaximumColumns: 2}
	containerInfoLayout  = components.ResourceLayout{MinimumWidth: 360, MaximumColumns: 2}
	blockCardLayout      = components.ResourceLayout{MinimumWidth: 360, MaximumColumns: 2}
	coreMeterLayout      = components.ResourceLayout{MinimumWidth: 160, MaximumColumns: 4}
)

// Reserve the native overlay scrollbar's full hit area, including its inset.
const statusScrollGutter float32 = 14
