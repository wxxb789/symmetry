package platform

import (
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// ContainmentAuthorityProvider is an optional process-containment seam. The
// runner uses it to persist a typed recovery binding after the plain process
// marker has been committed; the base Containment interface remains unchanged
// for legacy and non-Windows implementations.
type ContainmentAuthorityProvider interface {
	ContainmentAuthority() *authority.Supervisor
}

// ContainmentAuthorityCapability distinguishes a real supervisor-backed owner
// from the test-only/noop containment lease. A nil authority from a capable
// owner is a launch failure; an incapable legacy owner remains backwards
// compatible and has no recoverable Windows authority.
type ContainmentAuthorityCapability interface {
	ContainmentAuthorityProvider
	ContainmentAuthorityAvailable() bool
}

// ContainmentStopReceiptProvider exposes the exact positive stop witness after
// Close has stopped the owned process tree. The caller must persist this value
// before asking the provider to release any independent helper authority.
type ContainmentStopReceiptProvider interface {
	ContainmentStopReceipt() (authority.StopReceipt, bool)
}

// ContainmentStopReceiptReleaser releases an independent containment helper
// only after the matching stop receipt has been durably persisted. Releasing
// without a receipt is a contract violation and must fail closed.
type ContainmentStopReceiptReleaser interface {
	ReleaseContainment() error
}

// ContainmentStopReceiptAborter is used only when durable supervisor
// authority was never committed. It proves stop and releases the local helper
// during pre-authority startup cleanup; once authority is durable callers must
// use ContainmentStopReceiptReleaser after journal persistence instead.
type ContainmentStopReceiptAborter interface {
	AbortContainment() error
}

// ContainmentLeaseRenewer is an optional local watchdog capability. The
// deadline is relative to the helper's receipt of the request and must be
// implemented with a monotonic timer; sequence orders renewals for one helper
// authority and is not a durable run-event sequence.
type ContainmentLeaseRenewer interface {
	RenewLease(deadline time.Duration, sequence uint64) error
	LeaseRenewalAvailable() bool
}
