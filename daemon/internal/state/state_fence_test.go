//go:build linux

package state

import (
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestLinuxCloneCleanupBeginRejectsStaleFence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	base := newPinnedLinuxCloneCleanupReady(t, "00000000-0000-4000-8000-000000000101", "pinned-clone-cleanup-completed")
	staleFence := base.fence
	staleFence.LeaseToken += "-stale"
	if _, err := base.store.BeginLinuxCloneCleanup(base.key, staleFence); err == nil ||
		err.Error() != "Linux clone cleanup RunKey fence is stale" {
		t.Fatalf("BeginLinuxCloneCleanup() stale fence error = %v", err)
	}
}

func TestLinuxCloneCleanupCompleteReplayRejectsStaleFence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	base := newPinnedLinuxCloneCleanupReady(t, "00000000-0000-4000-8000-000000000102", "pinned-clone-cleanup-completed")
	store := base.store
	ready := loadStateJournal(t, store, base.key)
	cleanupIntent, err := LinuxCloneCleanupIntentForJournal(ready)
	if err != nil {
		t.Fatalf("cleanup intent fixture: %v", err)
	}
	if _, err := store.BeginLinuxCloneCleanup(base.key, base.fence); err != nil {
		t.Fatal(err)
	}
	rootDeleter := authority.LinuxCgroupProcessIdentity{PID: 901, Identity: "root-clone-cleaner:901:start"}
	witness := authority.LinuxCgroupProcessIdentity{
		PID: cleanupIntent.NormalReleaseProof.AnchorPID, Identity: cleanupIntent.NormalReleaseProof.AnchorIdentity,
	}
	targetProof := authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent,
		RootDevice: 9, RootInode: 11,
		TargetDevice: cleanupIntent.CheckoutReceipt.TargetDevice, TargetInode: cleanupIntent.CheckoutReceipt.TargetInode,
		Disposition: authority.LinuxCloneTargetDispositionRemoved, PathRewalkVerified: true,
		BOwnedMountConfined: true, TargetNlinkZero: true,
		RootDeleterUID: 0, RootDeleter: rootDeleter,
		RootDeleterExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: rootDeleter, Witness: witness, Waited: true, Reaped: true,
		},
	}
	if err := targetProof.Validate(); err != nil {
		t.Fatalf("target-removal proof fixture: %v", err)
	}
	if _, err := store.RecordLinuxCloneTargetRemoval(base.key, base.fence, cleanupIntent, targetProof); err != nil {
		t.Fatal(err)
	}
	proof := authority.LinuxCloneCleanupProof{
		Version: authority.LinuxCloneVersion, Intent: cleanupIntent, TargetRemovalProof: targetProof,
		SeedAbsent: true, BaseRefAbsent: true, ScratchAbsent: true, PromotionRefPresent: true,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("cleanup proof fixture: %v", err)
	}
	if _, err := store.CompleteLinuxCloneCleanup(base.key, base.fence, cleanupIntent, proof); err != nil {
		t.Fatal(err)
	}
	staleFence := base.fence
	staleFence.LeaseToken += "-stale"
	if _, err := store.CompleteLinuxCloneCleanup(base.key, staleFence, cleanupIntent, proof); err == nil ||
		err.Error() != "Linux clone cleanup completion fence is stale" {
		t.Fatalf("CompleteLinuxCloneCleanup() stale fence error = %v", err)
	}
}

func TestLinuxCloneReuseAbortRejectsStaleFence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	base := newPinnedLinuxCloneReuse(t, "00000000-0000-4000-8000-000000000103")
	grant := *loadStateJournal(t, base.store, base.key).LinuxCloneReuseGrant
	staleFence := base.fence
	staleFence.LeaseToken += "-stale"
	if _, err := base.store.AbortLinuxCloneReuseBeforeNativeStart(base.key, staleFence, grant); err == nil ||
		err.Error() != "Linux clone reuse abort has no exact consuming grant" {
		t.Fatalf("AbortLinuxCloneReuseBeforeNativeStart() stale fence error = %v", err)
	}
}

func TestLinuxCloneReuseBindRejectsStaleFence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	base := newPinnedLinuxCloneReuse(t, "00000000-0000-4000-8000-000000000105")
	grant := *loadStateJournal(t, base.store, base.key).LinuxCloneReuseGrant
	if _, err := base.store.BindLinuxCloneReuse(base.key, base.fence, grant, nil); err != nil {
		t.Fatalf("BindLinuxCloneReuse() exact-fence replay error = %v", err)
	}
	staleFence := base.fence
	staleFence.LeaseToken += "-stale"
	if _, err := base.store.BindLinuxCloneReuse(base.key, staleFence, grant, nil); err == nil ||
		err.Error() != "Linux clone reuse target lease or lifecycle is not effect-ready" {
		t.Fatalf("BindLinuxCloneReuse() stale fence error = %v", err)
	}
}

func TestLinuxCgroupLauncherGrantRejectsStaleFence(t *testing.T) {
	t.Parallel()
	requireLinuxCloneAMD64(t)
	store := mustStore(t)
	key, fence, _, reservation := setupLinuxV2AdmissionStage(t, store, "00000000-0000-4000-8000-000000000104", "launcher-grant")
	staleFence := fence
	staleFence.LeaseToken += "-stale"
	if _, err := store.ArmLinuxCgroupLauncherGrant(key, staleFence, reservation, nil); err == nil ||
		err.Error() != "Linux cgroup launcher grant RunKey/fence is stale" {
		t.Fatalf("ArmLinuxCgroupLauncherGrant() stale fence error = %v", err)
	}
}
