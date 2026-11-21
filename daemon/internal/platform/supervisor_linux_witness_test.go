//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestLinuxSupervisorWitnessConfigIsReadPerUse(t *testing.T) {
	directory := t.TempDir()
	triggerPath := filepath.Join(directory, "trigger")
	firedPath := filepath.Join(directory, "fired")
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)

	armed := linuxSupervisorWitnessConfigFromEnv()
	if !armed.armed || armed.scanFailureMode != linuxSupervisorScanFailureChildren ||
		armed.scanFailureTriggerPath != triggerPath || armed.scanFailureFiredPath != firedPath {
		t.Fatalf("armed witness config = %+v, want the installed gate and scan-failure markers", armed)
	}
	injection, enabled := linuxSupervisorScanFailureInjectionFromEnv()
	if !enabled || injection.triggerPath != triggerPath || injection.firedPath != firedPath {
		t.Fatalf("armed scan-failure injection = (%+v, %v), want the configured markers", injection, enabled)
	}

	if err := os.Unsetenv(linuxSupervisorProductionWitnessEnv); err != nil {
		t.Fatalf("unset witness gate: %v", err)
	}
	unarmed := linuxSupervisorWitnessConfigFromEnv()
	if unarmed.armed || unarmed.productionWitness != "" || unarmed.scanFailureMode != "" ||
		unarmed.scanFailureTriggerPath != "" || unarmed.scanFailureFiredPath != "" ||
		unarmed.dropResponseOnce != "" || unarmed.ownerLossProofReachedPath != "" {
		t.Fatalf("witness config after the gate was cleared = %+v, want an empty unarmed config", unarmed)
	}
	if injection, enabled := linuxSupervisorScanFailureInjectionFromEnv(); enabled {
		t.Fatalf("scan-failure injection after the gate was cleared = %+v, want none", injection)
	}

	if err := os.Setenv(linuxSupervisorProductionWitnessEnv, "1"); err != nil {
		t.Fatalf("re-arm witness gate: %v", err)
	}
	rearmed := linuxSupervisorWitnessConfigFromEnv()
	if !rearmed.armed || rearmed.scanFailureMode != linuxSupervisorScanFailureChildren ||
		rearmed.scanFailureFiredPath != firedPath {
		t.Fatalf("witness config after the gate was re-armed = %+v, want the installed markers", rearmed)
	}
	if _, enabled := linuxSupervisorScanFailureInjectionFromEnv(); !enabled {
		t.Fatal("scan-failure injection after the gate was re-armed = none, want the configured injection")
	}
}

func TestLinuxSupervisorWitnessHooksAreInertWithoutTheGate(t *testing.T) {
	directory := t.TempDir()
	marker := func(name string) string { return filepath.Join(directory, name) }
	triggerPath := marker("scan.trigger")
	firedPath := marker("scan.fired")
	paths := []string{
		firedPath,
		marker("scan.release"),
		marker("hello.json"),
		marker("hello.release"),
		marker("owner-loss.reached"),
		marker("owner-loss.release"),
		marker("owner-loss.result"),
		marker("owner-recovery.marker"),
		marker("lease-armed.reached"),
		marker("response-drop.marker"),
		marker("release-loss.reached"),
		marker("release-loss.release"),
	}
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)
	t.Setenv(linuxSupervisorScanPendingReleaseEnv, marker("scan.release"))
	t.Setenv(linuxSupervisorScanSealOwnerEnv, "both")
	t.Setenv(linuxSupervisorHelloSessionInjectionEnv, "1")
	t.Setenv(linuxSupervisorHelloSessionFiredEnv, marker("hello.json"))
	t.Setenv(linuxSupervisorHelloSessionReleaseEnv, marker("hello.release"))
	t.Setenv(linuxSupervisorOwnerLossProofReachedEnv, marker("owner-loss.reached"))
	t.Setenv(linuxSupervisorOwnerLossProofReleaseEnv, marker("owner-loss.release"))
	t.Setenv(linuxSupervisorOwnerLossProofResultEnv, marker("owner-loss.result"))
	t.Setenv(linuxSupervisorOwnerRecoveryAuthenticatedEnv, marker("owner-recovery.marker"))
	t.Setenv(linuxSupervisorLeaseArmedBarrierReachedEnv, marker("lease-armed.reached"))
	t.Setenv(linuxSupervisorDropResponseOnceEnv, linuxSupervisorOpRelease)
	t.Setenv(linuxSupervisorDropResponseMarkerEnv, marker("response-drop.marker"))
	t.Setenv(linuxSupervisorReleaseBarrierReachedEnv, marker("release-loss.reached"))
	t.Setenv(linuxSupervisorReleaseBarrierReleaseEnv, marker("release-loss.release"))
	if err := os.Unsetenv(linuxSupervisorProductionWitnessEnv); err != nil {
		t.Fatalf("unset witness gate: %v", err)
	}
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatalf("write scan trigger: %v", err)
	}

	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.armed || witness.productionWitness != "" || witness.scanFailureMode != "" ||
		witness.helloSessionInjection != "" || witness.ownerLossProofReachedPath != "" ||
		witness.ownerLossProofReachedSet || witness.dropResponseOnce != "" ||
		witness.releaseBarrierReachedPath != "" || witness.releaseBarrierReachedSet ||
		witness.leaseArmedBarrierReachedPath != "" {
		t.Fatalf("gate-unset witness config = %+v, want an empty unarmed config", witness)
	}
	if injection, enabled := linuxSupervisorScanFailureInjectionFromEnv(); enabled {
		t.Fatalf("gate-unset scan-failure injection = %+v, want none", injection)
	}
	if err := validateLinuxSupervisorScanFailureEnv(); err != nil {
		t.Fatalf("gate-unset scan-failure validation = %v, want nil", err)
	}
	if injection, enabled, err := linuxSupervisorHelloSessionInjectionFromEnv(); enabled || err != nil {
		t.Fatalf("gate-unset hello-session injection = (%+v, %v, %v), want none", injection, enabled, err)
	}
	if barrier, enabled, err := linuxSupervisorOwnerLossProofBarrierFromEnv(); enabled || err != nil {
		t.Fatalf("gate-unset owner-loss proof barrier = (%+v, %v, %v), want none", barrier, enabled, err)
	}
	if err := validateLinuxSupervisorOwnerLossProofBarrierEnv(); err != nil {
		t.Fatalf("gate-unset owner-loss proof barrier validation = %v, want nil", err)
	}
	if markerValue, enabled, err := linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv(); enabled || markerValue.path != "" || err == nil {
		t.Fatalf("gate-unset owner-recovery marker = (%+v, %v, %v), want the fail-closed refusal without a marker", markerValue, enabled, err)
	}
	empty := &linuxSupervisor{}
	if empty.dropResponseTransportOnce(linuxSupervisorOpRelease) {
		t.Fatal("gate-unset drop-response hook fired")
	}
	if err := empty.waitForLinuxSupervisorReleaseResponseLossBarrier(); err != nil {
		t.Fatalf("gate-unset release response-loss barrier = %v, want nil", err)
	}

	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("read witness test identity: %v", err)
	}
	calls := 0
	group := &processGroup{
		monitorReadStat: func(int) (linuxProcessStat, error) { return linuxProcessStat{}, nil },
		monitorReadChildren: func(int) ([]int, error) {
			calls++
			return []int{7}, nil
		},
	}
	restore := installLinuxSupervisorScanInjection(group, os.Getpid(), identity, true, authority.SupervisorScanOwnerHelper, false)
	t.Cleanup(restore)
	children, err := group.monitorReadChildren(os.Getpid())
	if err != nil || len(children) != 1 || calls != 1 {
		t.Fatalf("gate-unset monitor reader = (%v, %v), calls=%d, want the real reader", children, err, calls)
	}
	if _, err := os.Lstat(triggerPath); err != nil {
		t.Fatalf("gate-unset scan trigger = %v, want the untouched trigger", err)
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gate-unset hook produced witness path %q: %v", path, err)
		}
	}
}

func TestLinuxSupervisorWitnessMarkerParentRefusal(t *testing.T) {
	owned := t.TempDir()
	ownedMarker := filepath.Join(owned, "marker")
	if err := writeLinuxSupervisorTestMarker(ownedMarker, "owned\n"); err != nil {
		t.Fatalf("owned-directory witness marker = %v, want published", err)
	}
	if contents, err := os.ReadFile(ownedMarker); err != nil || string(contents) != "owned\n" {
		t.Fatalf("owned-directory witness marker = (%q, %v), want the published contents", contents, err)
	}
	if err := writeLinuxSupervisorScanWitnessMarker(filepath.Join(owned, "scan.marker"), "fired\n"); err != nil {
		t.Fatalf("owned-directory scan witness marker = %v, want published", err)
	}
	if err := writeLinuxSupervisorOwnerRecoveryAuthenticatedMarker(linuxSupervisorOwnerRecoveryAuthenticatedMarker{path: filepath.Join(owned, "recovery.marker")}, linuxSupervisorOpStop, os.Getpid(), os.Getpid(), "other_error"); err != nil {
		t.Fatalf("owned-directory owner-recovery marker = %v, want published", err)
	}

	symlinkParent := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(owned, symlinkParent); err != nil {
		t.Fatalf("create symlinked witness parent: %v", err)
	}
	symlinkedMarker := filepath.Join(symlinkParent, "symlink-marker")
	for name, write := range map[string]func() error{
		"publish behind a symlinked parent": func() error {
			return writeLinuxSupervisorTestMarker(symlinkedMarker, "refused\n")
		},
		"scan witness behind a symlinked parent": func() error {
			return writeLinuxSupervisorScanWitnessMarker(symlinkedMarker, "refused\n")
		},
		"owner-recovery behind a symlinked parent": func() error {
			return writeLinuxSupervisorOwnerRecoveryAuthenticatedMarker(linuxSupervisorOwnerRecoveryAuthenticatedMarker{path: symlinkedMarker}, linuxSupervisorOpStop, os.Getpid(), os.Getpid(), "other_error")
		},
	} {
		if err := write(); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
			t.Fatalf("%s = %v, want the marker parent refusal", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(owned, "symlink-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlinked-parent witness marker = %v, want nothing written through the link", err)
	}

	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatalf("create group-writable witness parent: %v", err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatalf("widen group-writable witness parent: %v", err)
	}
	sharedMarker := filepath.Join(shared, "marker")
	if err := writeLinuxSupervisorTestMarker(sharedMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
		t.Fatalf("group- or other-writable-parent witness marker = %v, want the marker parent refusal", err)
	}
	if _, err := os.Lstat(sharedMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("group-writable-parent witness marker = %v, want nothing written", err)
	}

	absentParentMarker := filepath.Join(t.TempDir(), "absent", "marker")
	if err := writeLinuxSupervisorTestMarker(absentParentMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
		t.Fatalf("absent-parent witness marker = %v, want the marker parent refusal", err)
	}

	if os.Getuid() != 0 {
		rootOwnedMarker := filepath.Join("/", fmt.Sprintf("symmetry-witness-refusal-%d.marker", os.Getpid()))
		if err := writeLinuxSupervisorTestMarker(rootOwnedMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
			t.Fatalf("root-owned-parent witness marker = %v, want the marker parent refusal", err)
		}
		if _, err := os.Lstat(rootOwnedMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("root-owned-parent witness marker = %v, want nothing written", err)
		}
	}
}
