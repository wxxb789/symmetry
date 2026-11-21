package platform

import "errors"

// ErrLinuxSupervisorResponseLost identifies a committed Linux supervisor
// request whose response transport disappeared before the client decoded it.
// Recovery must replay the exact durable authority instead of converting this
// transport outcome into descendant-scan uncertainty.
//
// This value stays in an unconstrained file because execution/runner.go
// consumes it on every platform; the ErrLinuxOwner* mismatch sentinels live in
// containment_errors_linux.go next to their linux && amd64 producing sites.
var ErrLinuxSupervisorResponseLost = errors.New("Linux supervisor response was lost after request commit")
