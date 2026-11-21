//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	linuxSupervisorProductionWitnessEnv          = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxSupervisorHelloSessionInjectionEnv      = "SYMMETRY_LINUX_SUPERVISOR_TEST_INJECT_WRONG_HELLO_SESSION"
	linuxSupervisorHelloSessionFiredEnv          = "SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_FIRED_PATH"
	linuxSupervisorHelloSessionReleaseEnv        = "SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_RELEASE_PATH"
	linuxSupervisorOwnerLossProofReachedEnv      = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_REACHED_FILE"
	linuxSupervisorOwnerLossProofReleaseEnv      = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_RELEASE_FILE"
	linuxSupervisorOwnerLossProofResultEnv       = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_PROOF_RESULT_FILE"
	linuxSupervisorOwnerRecoveryAuthenticatedEnv = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_RECOVERY_AUTHENTICATED_FILE"
	linuxSupervisorLeaseArmedBarrierReachedEnv   = "SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE"
	linuxSupervisorDropResponseOnceEnv           = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_ONCE"
	linuxSupervisorDropResponseMarkerEnv         = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_FIRED_PATH"
	linuxSupervisorReleaseBarrierReachedEnv      = "SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_REACHED_FILE"
	linuxSupervisorReleaseBarrierReleaseEnv      = "SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_RELEASE_FILE"
	linuxSupervisorScanFailureModeEnv            = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE"
	linuxSupervisorScanFailureTriggerEnv         = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH"
	linuxSupervisorScanFailureFiredEnv           = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH"
	linuxSupervisorScanPendingReleaseEnv         = "SYMMETRY_LINUX_SUPERVISOR_SCAN_PENDING_RELEASE_PATH"
	linuxSupervisorScanSealOwnerEnv              = "SYMMETRY_LINUX_SUPERVISOR_SCAN_SEAL_OWNER"
)

var errLinuxSupervisorWitnessMarkerParentUnsafe = errors.New("Linux supervisor witness marker parent is not a safe witness directory")

// linuxSupervisorWitnessConfig is re-parsed at every hook entry, never cached:
// the app witness toggles the gate in-process, so a snapshot would go stale.
type linuxSupervisorWitnessConfig struct {
	productionWitness string
	armed             bool

	scanFailureMode        string
	scanFailureTriggerPath string
	scanFailureFiredPath   string
	scanPendingReleasePath string
	scanSealOwner          string

	helloSessionInjection   string
	helloSessionFiredPath   string
	helloSessionReleasePath string

	ownerLossProofReachedPath string
	ownerLossProofReleasePath string
	ownerLossProofResultPath  string
	ownerLossProofReachedSet  bool
	ownerLossProofReleaseSet  bool
	ownerLossProofResultSet   bool

	ownerRecoveryAuthenticatedPath string
	ownerRecoveryAuthenticatedSet  bool

	leaseArmedBarrierReachedPath string

	dropResponseOnce      string
	dropResponseFiredPath string

	releaseBarrierReachedPath string
	releaseBarrierReleasePath string
	releaseBarrierReachedSet  bool
	releaseBarrierReleaseSet  bool
}

func linuxSupervisorWitnessConfigFromEnv() linuxSupervisorWitnessConfig {
	config := linuxSupervisorWitnessConfig{productionWitness: os.Getenv(linuxSupervisorProductionWitnessEnv)}
	config.ownerRecoveryAuthenticatedPath, config.ownerRecoveryAuthenticatedSet = os.LookupEnv(linuxSupervisorOwnerRecoveryAuthenticatedEnv)
	config.armed = strings.TrimSpace(config.productionWitness) == "1"
	if !config.armed {
		// The presence flag survives the empty config so the owner-recovery
		// reader keeps its fail-closed "configured without the gate" error.
		config.ownerRecoveryAuthenticatedPath = ""
		return config
	}
	config.scanFailureMode = os.Getenv(linuxSupervisorScanFailureModeEnv)
	config.scanFailureTriggerPath = os.Getenv(linuxSupervisorScanFailureTriggerEnv)
	config.scanFailureFiredPath = os.Getenv(linuxSupervisorScanFailureFiredEnv)
	config.scanPendingReleasePath = os.Getenv(linuxSupervisorScanPendingReleaseEnv)
	config.scanSealOwner = os.Getenv(linuxSupervisorScanSealOwnerEnv)
	config.helloSessionInjection = os.Getenv(linuxSupervisorHelloSessionInjectionEnv)
	config.helloSessionFiredPath = os.Getenv(linuxSupervisorHelloSessionFiredEnv)
	config.helloSessionReleasePath = os.Getenv(linuxSupervisorHelloSessionReleaseEnv)
	config.ownerLossProofReachedPath, config.ownerLossProofReachedSet = os.LookupEnv(linuxSupervisorOwnerLossProofReachedEnv)
	config.ownerLossProofReleasePath, config.ownerLossProofReleaseSet = os.LookupEnv(linuxSupervisorOwnerLossProofReleaseEnv)
	config.ownerLossProofResultPath, config.ownerLossProofResultSet = os.LookupEnv(linuxSupervisorOwnerLossProofResultEnv)
	config.leaseArmedBarrierReachedPath = os.Getenv(linuxSupervisorLeaseArmedBarrierReachedEnv)
	config.dropResponseOnce = os.Getenv(linuxSupervisorDropResponseOnceEnv)
	config.dropResponseFiredPath = os.Getenv(linuxSupervisorDropResponseMarkerEnv)
	config.releaseBarrierReachedPath, config.releaseBarrierReachedSet = os.LookupEnv(linuxSupervisorReleaseBarrierReachedEnv)
	config.releaseBarrierReleasePath, config.releaseBarrierReleaseSet = os.LookupEnv(linuxSupervisorReleaseBarrierReleaseEnv)
	return config
}

func validateLinuxSupervisorWitnessMarkerParent(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("%w: inspect %q: %v", errLinuxSupervisorWitnessMarkerParentUnsafe, parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %q is not a real directory", errLinuxSupervisorWitnessMarkerParentUnsafe, parent)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: %q has no ownership metadata", errLinuxSupervisorWitnessMarkerParentUnsafe, parent)
	}
	if stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%w: %q is owned by uid %d, not the writing uid %d", errLinuxSupervisorWitnessMarkerParentUnsafe, parent, stat.Uid, os.Getuid())
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: %q is group- or other-writable", errLinuxSupervisorWitnessMarkerParentUnsafe, parent)
	}
	return nil
}

func writeLinuxSupervisorScanWitnessMarker(path, contents string) error {
	if err := validateLinuxSupervisorWitnessMarkerParent(path); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o600)
}
