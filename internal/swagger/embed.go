package swagger

import "embed"

//go:embed *
// Static is an embedded file server containing static HTTP assets.
var Static embed.FS
