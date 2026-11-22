package platform

import "errors"

// ErrLinuxSupervisorResponseLost identifies a committed Linux supervisor
// request whose response transport disappeared before the client decoded it.
// Recovery must replay the exact durable authority instead of converting this
// transport outcome into descendant-scan uncertainty.
var ErrLinuxSupervisorResponseLost = errors.New("Linux supervisor response was lost after request commit")
