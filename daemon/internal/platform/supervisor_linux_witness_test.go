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

	// An ambient export of this name would keep the presence bit.
	unsetWitnessEnvForTest(t, linuxSupervisorOwnerRecoveryAuthenticatedEnv)

	config := linuxSupervisorWitnessConfigFromEnv()
	if config.scanFailureMode != linuxSupervisorScanFailureChildren ||
		config.scanFailureTriggerPath != triggerPath || config.scanFailureFiredPath != firedPath {
		t.Fatalf("armed witness config = %+v, want the installed gate and scan-failure markers", config)
	}
	injection, enabled := linuxSupervisorScanFailureInjectionFromEnv()
	if !enabled || injection.triggerPath != triggerPath || injection.firedPath != firedPath {
		t.Fatalf("armed scan-failure injection = (%+v, %v), want the configured markers", injection, enabled)
	}

	if err := os.Unsetenv(linuxSupervisorProductionWitnessEnv); err != nil {
		t.Fatalf("unset witness gate: %v", err)
	}
	unarmed := linuxSupervisorWitnessConfigFromEnv()
	if unarmed != (linuxSupervisorWitnessConfig{}) {
		t.Fatalf("witness config after the gate was cleared = %+v, want an empty unarmed config", unarmed)
	}
	if injection, enabled := linuxSupervisorScanFailureInjectionFromEnv(); enabled {
		t.Fatalf("scan-failure injection after the gate was cleared = %+v, want none", injection)
	}

	if err := os.Setenv(linuxSupervisorProductionWitnessEnv, "1"); err != nil {
		t.Fatalf("re-arm witness gate: %v", err)
	}
	rearmed := linuxSupervisorWitnessConfigFromEnv()
	if rearmed.scanFailureMode != linuxSupervisorScanFailureChildren ||
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
	markers := []struct{ env, path string }{
		{linuxSupervisorScanFailureFiredEnv, marker("scan.fired")},
		{linuxSupervisorScanPendingReleaseEnv, marker("scan.release")},
		{linuxSupervisorHelloSessionFiredEnv, marker("hello.json")},
		{linuxSupervisorHelloSessionReleaseEnv, marker("hello.release")},
		{linuxSupervisorOwnerLossProofReachedEnv, marker("owner-loss.reached")},
		{linuxSupervisorOwnerLossProofReleaseEnv, marker("owner-loss.release")},
		{linuxSupervisorOwnerLossProofResultEnv, marker("owner-loss.result")},
		{linuxSupervisorOwnerRecoveryAuthenticatedEnv, marker("owner-recovery.marker")},
		{linuxSupervisorLeaseArmedBarrierReachedEnv, marker("lease-armed.reached")},
		{linuxSupervisorDropResponseMarkerEnv, marker("response-drop.marker")},
		{linuxSupervisorReleaseBarrierReachedEnv, marker("release-loss.reached")},
		{linuxSupervisorReleaseBarrierReleaseEnv, marker("release-loss.release")},
	}
	for _, entry := range markers {
		t.Setenv(entry.env, entry.path)
	}
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanSealOwnerEnv, "both")
	t.Setenv(linuxSupervisorHelloSessionInjectionEnv, "1")
	t.Setenv(linuxSupervisorDropResponseOnceEnv, linuxSupervisorOpRelease)
	ambientGate, ambientGateSet := os.LookupEnv(linuxSupervisorProductionWitnessEnv)
	t.Cleanup(func() {
		if ambientGateSet {
			_ = os.Setenv(linuxSupervisorProductionWitnessEnv, ambientGate)
		} else {
			_ = os.Unsetenv(linuxSupervisorProductionWitnessEnv)
		}
	})
	if err := os.Unsetenv(linuxSupervisorProductionWitnessEnv); err != nil {
		t.Fatalf("unset witness gate: %v", err)
	}
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatalf("write scan trigger: %v", err)
	}

	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness != (linuxSupervisorWitnessConfig{ownerRecoveryAuthenticatedSet: true}) {
		t.Fatalf("gate-unset witness config = %+v, want the unarmed config with only the owner-recovery presence flag", witness)
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
	markerValue, enabled, err := linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv()
	if enabled || markerValue.path != "" || err == nil {
		t.Fatalf("gate-unset owner-recovery marker = (%+v, %v, %v), want the fail-closed refusal without a marker", markerValue, enabled, err)
	}
	if err.Error() != "Linux supervisor authenticated owner-recovery marker requires the production witness flag" {
		t.Fatalf("gate-unset owner-recovery refusal = %q, want the base gate refusal", err)
	}
	dropHarness, releaseHarness := witnessHookHarness(t), witnessHookHarness(t)
	releaseHarness.responseDropTriggered = true
	releaseHarness.responseTransportLost = true
	if dropHarness.dropResponseTransportOnce(linuxSupervisorOpRelease) {
		t.Fatal("gate-unset drop-response hook fired")
	}
	if err := releaseHarness.waitForLinuxSupervisorReleaseResponseLossBarrier(); err != nil {
		t.Fatalf("gate-unset release response-loss barrier = %v, want nil", err)
	}

	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("read witness test identity: %v", err)
	}
	calls := 0
	group := countingProcessGroup(&calls)
	restore := installLinuxSupervisorScanInjection(group, os.Getpid(), identity, false, authority.SupervisorScanOwnerHelper, false)
	t.Cleanup(restore)
	children, err := group.monitorReadChildren(os.Getpid())
	if err != nil || len(children) != 1 || calls != 1 {
		t.Fatalf("gate-unset monitor reader = (%v, %v), calls=%d, want the real reader: an installed injection fires on this first read", children, err, calls)
	}
	if _, err := os.Lstat(triggerPath); err != nil {
		t.Fatalf("gate-unset scan trigger = %v, want the untouched trigger", err)
	}
	for _, entry := range markers {
		if _, err := os.Lstat(entry.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gate-unset hook produced witness path %q: %v", entry.path, err)
		}
	}

	if err := os.Setenv(linuxSupervisorProductionWitnessEnv, " 1 "); err != nil {
		t.Fatalf("set whitespace-padded witness gate: %v", err)
	}
	t.Setenv(linuxSupervisorScanFailureModeEnv, "bogus")
	unsetWitnessEnvForTest(t, linuxSupervisorOwnerLossProofResultEnv)
	unsetWitnessEnvForTest(t, linuxSupervisorReleaseBarrierReleaseEnv)
	if err := validateLinuxSupervisorScanFailureEnv(); err != nil {
		t.Fatalf("whitespace-gate scan-failure validation = %v, want nil", err)
	}
	if injection, enabled, err := linuxSupervisorHelloSessionInjectionFromEnv(); enabled || err != nil {
		t.Fatalf("whitespace-gate hello-session injection = (%+v, %v, %v), want none", injection, enabled, err)
	}
	if barrier, enabled, err := linuxSupervisorOwnerLossProofBarrierFromEnv(); enabled || err != nil {
		t.Fatalf("whitespace-gate owner-loss proof barrier = (%+v, %v, %v), want none", barrier, enabled, err)
	}
	if err := validateLinuxSupervisorOwnerLossProofBarrierEnv(); err != nil {
		t.Fatalf("whitespace-gate owner-loss proof barrier validation = %v, want nil", err)
	}
	markerValue, enabled, err = linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv()
	if enabled || markerValue.path != "" || err == nil {
		t.Fatalf("whitespace-gate owner-recovery marker = (%+v, %v, %v), want the fail-closed refusal without a marker", markerValue, enabled, err)
	}
	if err.Error() != "Linux supervisor authenticated owner-recovery marker requires the production witness flag" {
		t.Fatalf("whitespace-gate owner-recovery refusal = %q, want the base gate refusal", err)
	}
	if dropHarness.dropResponseTransportOnce(linuxSupervisorOpRelease) {
		t.Fatal("whitespace-gate drop-response hook fired")
	}
	if err := releaseHarness.waitForLinuxSupervisorReleaseResponseLossBarrier(); err != nil {
		t.Fatalf("whitespace-gate release response-loss barrier = %v, want nil", err)
	}
}

func TestLinuxSupervisorWitnessMarkerParentRefusal(t *testing.T) {
	owned := t.TempDir()
	ownedMarker := filepath.Join(owned, "marker")
	if err := writeLinuxSupervisorAbsentWitnessMarker(ownedMarker, "owned\n"); err != nil {
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
	ownedEntries := witnessDirectoryEntries(t, owned)
	for name, write := range map[string]func() error{
		"publish behind a symlinked parent": func() error {
			return writeLinuxSupervisorAbsentWitnessMarker(symlinkedMarker, "refused\n")
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
	if got := witnessDirectoryEntries(t, owned); got != ownedEntries {
		t.Fatalf("symlinked-parent refusals left %d entries in the real parent, want %d (no temporary file)", got, ownedEntries)
	}
	if _, err := os.Lstat(filepath.Join(owned, "symlink-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlinked-parent witness marker = %v, want nothing written through the link", err)
	}

	for _, writable := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "group-writable", mode: 0o770},
		{name: "other-writable", mode: 0o707},
	} {
		shared := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(shared, writable.mode); err != nil {
			t.Fatalf("create %s witness parent: %v", writable.name, err)
		}
		if err := os.Chmod(shared, writable.mode); err != nil {
			t.Fatalf("set %s witness parent mode: %v", writable.name, err)
		}
		sharedMarker := filepath.Join(shared, "marker")
		sharedEntries := witnessDirectoryEntries(t, shared)
		if err := writeLinuxSupervisorAbsentWitnessMarker(sharedMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
			t.Fatalf("%s-parent witness marker (mode %#o) = %v, want the marker parent refusal", writable.name, writable.mode, err)
		}
		if got := witnessDirectoryEntries(t, shared); got != sharedEntries {
			t.Fatalf("%s-parent refusal left %d entries, want %d (no temporary file)", writable.name, got, sharedEntries)
		}
		if _, err := os.Lstat(sharedMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s-parent witness marker (mode %#o) = %v, want nothing written", writable.name, writable.mode, err)
		}
	}

	absentParentMarker := filepath.Join(t.TempDir(), "absent", "marker")
	if err := writeLinuxSupervisorAbsentWitnessMarker(absentParentMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
		t.Fatalf("absent-parent witness marker = %v, want the marker parent refusal", err)
	}

	if os.Getuid() != 0 {
		rootOwnedMarker := filepath.Join("/", fmt.Sprintf("symmetry-witness-refusal-%d.marker", os.Getpid()))
		if err := writeLinuxSupervisorAbsentWitnessMarker(rootOwnedMarker, "refused\n"); !errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe) {
			t.Fatalf("root-owned-parent witness marker = %v, want the marker parent refusal", err)
		}
		if _, err := os.Lstat(rootOwnedMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("root-owned-parent witness marker = %v, want nothing written", err)
		}
	}
}

func unsetWitnessEnvForTest(t *testing.T, name string) {
	t.Helper()
	previous, present := os.LookupEnv(name)
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
}

func witnessHookHarness(t *testing.T) *linuxSupervisor {
	t.Helper()
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create witness hook harness pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = responseWrite.Close()
		_ = responseRead.Close()
	})
	receipt := authority.StopReceipt{}
	return &linuxSupervisor{responseRead: responseRead, receipt: &receipt}
}

func witnessDirectoryEntries(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read witness directory %q: %v", directory, err)
	}
	return len(entries)
}

func countingProcessGroup(calls *int) *processGroup {
	return &processGroup{
		monitorReadStat: func(int) (linuxProcessStat, error) { return linuxProcessStat{}, nil },
		monitorReadChildren: func(int) ([]int, error) {
			*calls++
			return []int{7}, nil
		},
	}
}
