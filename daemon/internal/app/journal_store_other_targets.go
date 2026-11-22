//go:build !(linux && amd64)

package app

// journalStore on every other target is the cross-target seam: no target-only Store method is
// selected by the code that builds here.
type journalStore = journalStoreBase
