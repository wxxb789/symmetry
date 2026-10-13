//go:build linux && amd64

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

type linuxV2OwnerResources struct {
	mu             sync.Mutex
	config         config.Config
	owner          linuxgate.OwnerConfig
	anchor         *platform.LinuxOwnerAnchorClient
	parent         *platform.LinuxCgroupV2Parent
	anchorIdentity platform.LinuxOwnerAnchorIdentity
	closed         bool
}

type linuxCloneJournalCallbacks struct {
	Reserve          func(authority.LinuxCloneIntent) error
	BindSeed         func(authority.LinuxCloneIntent, authority.LinuxCloneSeedReceipt) error
	CheckoutReady    func(authority.LinuxCloneIntent, authority.LinuxCloneCheckoutReceipt) error
	ReservePromotion func(authority.LinuxCloneIntent, authority.LinuxClonePromotionIntent) error
	RecordPromotion  func(authority.LinuxCloneIntent, authority.LinuxClonePromotionIntent, authority.LinuxClonePromotionReceipt) error
}

func init() {
	defaultLinuxV2NativeOwnerFactory = newLinuxV2OwnerResources
}

func newLinuxV2OwnerResources(value config.Config) (linuxV2NativeOwner, error) {
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return nil, fmt.Errorf("read trusted root-installed Linux A/B owner configuration: %w", err)
	}
	if err := verifyLinuxOwnerIdentity(owner, uint32(os.Getuid()), uint32(os.Geteuid()), uint32(os.Getgid()), uint32(os.Getegid())); err != nil {
		return nil, err
	}
	// The provider extension share's B primary GID is installed authority, not
	// a JSON or ambient-environment input. Store it in this pinned local config
	// before any Pi preflight or bridge materialization can observe the value.
	value.PiProviderExtensionGroupID = owner.BGID
	parent, err := platform.OpenLinuxCgroupV2Parent(value.LinuxOwnerCgroupMount, value.LinuxOwnerDelegatedPath)
	if err != nil {
		return nil, fmt.Errorf("open configured delegated cgroup v2 parent: %w", err)
	}
	anchor, err := platform.NewLinuxOwnerAnchorClient(value.LinuxOwnerSocket)
	if err != nil {
		_ = parent.Close()
		return nil, fmt.Errorf("construct authenticated Linux owner anchor client: %w", err)
	}
	if parent.OwnerUID() != owner.AUID {
		_ = parent.Close()
		return nil, errors.New("configured delegated cgroup parent is not owned by trusted A")
	}
	return &linuxV2OwnerResources{config: value, owner: owner, anchor: anchor, parent: parent}, nil
}

func (owner *linuxV2OwnerResources) Preflight(ctx context.Context, value config.Config, kind harness.Kind, profile config.AgentProfile, providerAccess bool) ([]string, error) {
	if owner == nil || ctx == nil {
		return nil, errors.New("Linux v2 native owner preflight is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// This is the mandatory native-admission gate, not a same-UID or static
	// cgroup capability probe. It must remain closed until the owner-approved
	// B-owned standalone clone trust boundary and the Linux/WSL2 namespace plus
	// pidfd_getfd peer capability are both verified by the per-run preflight.
	if err := VerifyLinuxOwnerInstallation(value); err != nil {
		return nil, err
	}
	currentOwner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil || currentOwner != owner.owner {
		return nil, fmt.Errorf("trusted Linux A/B owner configuration changed after pin creation: %w", appErrorOr(err, "fixed UID/GID identity differs"))
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.parent == nil || owner.anchor == nil {
		return nil, platform.ErrLinuxCgroupV2Unresolved
	}
	if value.StateDir != owner.config.StateDir || value.LinuxOwnerCgroupMount != owner.config.LinuxOwnerCgroupMount ||
		value.LinuxOwnerDelegatedPath != owner.config.LinuxOwnerDelegatedPath || value.LinuxOwnerSocket != owner.config.LinuxOwnerSocket {
		return nil, errors.New("Linux v2 owner runtime configuration changed after its parent pin was opened")
	}
	if kind == harness.KindPi {
		if err := validatePiProviderExtensionGroupID(value, owner.owner.BGID); err != nil {
			return nil, errors.New("Pi provider extension group differs from the trusted installed B primary GID")
		}
	}
	configuredKind, err := configuredHarnessKind(value.Runtime.HarnessKind)
	if err != nil || configuredKind != kind {
		return nil, errors.New("Linux v2 owner preflight harness differs from configured runtime")
	}
	identity, err := owner.anchor.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover authenticated Linux owner anchor: %w", err)
	}
	if identity.OwnerUID != owner.owner.AUID || !identity.Parent.Equal(owner.parent.Identity()) || identity.ManagerPath == "" {
		return nil, errors.New("authenticated Linux owner anchor differs from the pinned delegated parent or trusted A identity")
	}
	if owner.anchorIdentity.Process.PID != 0 && (!owner.anchorIdentity.Process.Equal(identity.Process) ||
		!owner.anchorIdentity.Parent.Equal(identity.Parent) || owner.anchorIdentity.ManagerPath != identity.ManagerPath) {
		return nil, errors.New("Linux owner anchor identity changed after preflight")
	}
	owner.anchorIdentity = identity
	home, account, err := linuxV2FixedNativeHome(owner.owner)
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(home, ".cache")
	if err := linuxV2ValidateOwnedDirectory(cacheDir, owner.owner, false); err != nil {
		return nil, fmt.Errorf("validate fixed-B XDG cache directory: %w", err)
	}
	if err := linuxV2ValidateAmbientCredentialAllowlist(profile.EnvAllowlist); err != nil {
		return nil, err
	}
	environment := []string{
		"HOME=" + home,
		"USER=" + account.Username,
		"LOGNAME=" + account.Username,
		"TMPDIR=" + cacheDir,
		"TEMP=" + cacheDir,
		"TMP=" + cacheDir,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + cacheDir,
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
	}
	if kind == harness.KindPi {
		if value.PiProviderExtensionGroupID != owner.owner.BGID {
			return nil, errors.New("Pi provider extension group does not match the trusted fixed-B primary group")
		}
		agentDir := filepath.Join(home, ".pi", "agent")
		sessionDir := filepath.Join(agentDir, "sessions")
		if err := linuxV2ValidateOwnedDirectory(agentDir, owner.owner, false); err != nil {
			return nil, fmt.Errorf("validate fixed-B Pi agent directory: %w", err)
		}
		if err := linuxV2ValidateOwnedDirectory(sessionDir, owner.owner, true); err != nil {
			return nil, fmt.Errorf("validate fixed-B Pi session directory: %w", err)
		}
		if err := linuxV2ValidatePiSessionArguments(profile.Args, sessionDir); err != nil {
			return nil, err
		}
		if providerAccess && value.PiProviderExtensionDir == "" {
			return nil, errors.New("Linux Pi provider access requires its trusted read-only extension share")
		}
		environment = append(environment, "PI_CODING_AGENT_DIR="+agentDir)
	}
	return environment, nil
}

func (owner *linuxV2OwnerResources) trustedPiProviderExtensionGroupID() uint32 {
	if owner == nil {
		return 0
	}
	return owner.owner.BGID
}

func (owner *linuxV2OwnerResources) BuildInvocation(ctx context.Context, key state.RunKey, fence protocol.Fence, invocation execution.Invocation, callbacks execution.LinuxV2JournalCallbacks) (execution.Invocation, error) {
	if owner == nil || ctx == nil || ctx.Err() != nil || !ownerIdentityInV2Invocation(owner, key, fence, invocation) {
		return execution.Invocation{}, errors.New("Linux v2 native invocation does not match the prepared owner and lease fence")
	}
	if invocation.InitialLeaseDeadlineAt.IsZero() || invocation.InitialLeaseSequence != 1 || !filepath.IsAbs(invocation.Dir) {
		return execution.Invocation{}, errors.New("Linux v2 native invocation lacks the claim-derived initial lease or exact workspace")
	}
	if err := VerifyLinuxOwnerInstallation(owner.config); err != nil {
		return execution.Invocation{}, fmt.Errorf("revalidate Linux owner before native invocation: %w", err)
	}
	currentOwner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil || currentOwner != owner.owner {
		return execution.Invocation{}, fmt.Errorf("trusted Linux A/B owner configuration changed before native invocation: %w", appErrorOr(err, "fixed UID/GID identity differs"))
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.anchorIdentity.Process.PID == 0 || owner.parent == nil || owner.anchor == nil {
		return execution.Invocation{}, errors.New("Linux v2 native owner has no successful host preflight")
	}
	anchorIdentity, err := owner.anchor.Discover(ctx)
	if err != nil {
		return execution.Invocation{}, fmt.Errorf("revalidate Linux owner anchor before native invocation: %w", err)
	}
	if !anchorIdentity.Process.Equal(owner.anchorIdentity.Process) || !anchorIdentity.Parent.Equal(owner.parent.Identity()) || anchorIdentity.OwnerUID != owner.owner.AUID {
		return execution.Invocation{}, errors.New("Linux owner anchor or delegated parent changed before native invocation")
	}
	recoveryEndpoint, err := linuxV2RecoveryEndpoint(owner.config.StateDir, key)
	if err != nil {
		return execution.Invocation{}, err
	}
	invocation.LinuxV2Required = true
	invocation.LinuxV2 = &execution.LinuxV2Invocation{
		RunKey: key, Fence: fence, Parent: owner.parent, Anchor: owner.anchor,
		RecoveryEndpoint: recoveryEndpoint, Callbacks: callbacks,
	}
	return invocation, nil
}

func ownerIdentityInV2Invocation(owner *linuxV2OwnerResources, key state.RunKey, fence protocol.Fence, invocation execution.Invocation) bool {
	return key.RunID != "" && key.Generation > 0 && fence.Generation == key.Generation &&
		fence.RuntimeID != "" && fence.RuntimeEpoch > 0 && fence.ClaimID != "" && fence.LeaseToken != "" &&
		owner != nil && owner.config.Runtime.HarnessKind != "" && invocation.LinuxV2Required
}

func (owner *linuxV2OwnerResources) Close() error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed {
		return nil
	}
	owner.closed = true
	if owner.parent == nil {
		return nil
	}
	err := owner.parent.Close()
	owner.parent = nil
	return err
}

func linuxV2FixedNativeHome(owner linuxgate.OwnerConfig) (string, *user.User, error) {
	account, err := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
	if err != nil {
		return "", nil, fmt.Errorf("look up trusted fixed-B account: %w", err)
	}
	if account.Username == "" || account.HomeDir == "" || !filepath.IsAbs(account.HomeDir) || filepath.Clean(account.HomeDir) != account.HomeDir || account.HomeDir == "/" {
		return "", nil, errors.New("fixed-B account has no canonical provisioned HOME")
	}
	home, err := lstatCanonicalLinuxPath(account.HomeDir)
	if err != nil {
		return "", nil, fmt.Errorf("inspect fixed-B HOME: %w", err)
	}
	identity, err := linuxFileIdentity(home)
	if err != nil || !home.IsDir() || identity.uid != owner.BUID || identity.gid != owner.BGID {
		return "", nil, errors.New("fixed-B HOME ownership differs from trusted root-installed UID/GID")
	}
	b := linuxACLSubject{uid: owner.BUID, gid: owner.BGID}
	a := linuxACLSubject{uid: owner.AUID, gid: owner.AGID}
	if err := verifyLinuxPathTraversal(account.HomeDir, b); err != nil {
		return "", nil, fmt.Errorf("fixed B cannot traverse HOME: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(account.HomeDir, b, 7, 0); err != nil {
		return "", nil, fmt.Errorf("fixed B cannot update its HOME: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(account.HomeDir, a, 1, 6); err != nil {
		return "", nil, fmt.Errorf("A cannot stat fixed-B native files without reading or modifying HOME: %w", err)
	}
	return account.HomeDir, account, nil
}

func linuxV2ValidateOwnedDirectory(path string, owner linuxgate.OwnerConfig, statAccessibleByA bool) error {
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != owner.BUID || identity.gid != owner.BGID {
		return errors.New("directory must be B-owned on the configured primary group")
	}
	b := linuxACLSubject{uid: owner.BUID, gid: owner.BGID}
	if err := verifyEffectiveLinuxAccess(path, b, 7, 0); err != nil {
		return fmt.Errorf("fixed B cannot read, search, and update directory: %w", err)
	}
	if statAccessibleByA {
		a := linuxACLSubject{uid: owner.AUID, gid: owner.AGID}
		if err := verifyLinuxPathTraversal(path, a); err != nil {
			return fmt.Errorf("A cannot traverse the fixed-B directory path: %w", err)
		}
		if err := verifyEffectiveLinuxAccess(path, a, 1, 6); err != nil {
			return fmt.Errorf("A cannot stat fixed-B session files without listing or changing them: %w", err)
		}
	}
	return nil
}

func linuxV2ValidatePiSessionArguments(args []string, sessionDir string) error {
	seen := false
	for index := 0; index < len(args); index++ {
		value := ""
		switch {
		case args[index] == "--session-dir":
			if index+1 >= len(args) {
				return errors.New("Pi --session-dir requires a fixed-B session path")
			}
			index++
			value = args[index]
		case strings.HasPrefix(args[index], "--session-dir="):
			value = strings.TrimPrefix(args[index], "--session-dir=")
		default:
			continue
		}
		if seen || value != sessionDir {
			return errors.New("Pi session directory must be the provisioned fixed-B session path")
		}
		seen = true
	}
	return nil
}

func linuxV2ValidateAmbientCredentialAllowlist(values []string) error {
	for _, value := range values {
		name := strings.ToUpper(value)
		if strings.Contains(name, "API_KEY") || strings.Contains(name, "ACCESS_KEY") || strings.HasSuffix(name, "_KEY") ||
			strings.HasSuffix(name, "_TOKEN") || strings.HasSuffix(name, "_SECRET") ||
			strings.Contains(name, "PASSWORD") || strings.Contains(name, "CREDENTIAL") || strings.Contains(name, "PROXY") ||
			strings.HasPrefix(name, "GIT_CONFIG") || name == "GIT_ASKPASS" || name == "GIT_SSH_COMMAND" ||
			name == "SSH_AUTH_SOCK" || name == "SSH_AGENT_PID" ||
			strings.HasPrefix(name, "XDG_") && !linuxV2FixedBEnvironmentKey(name) {
			return fmt.Errorf("Linux native B cannot inherit daemon environment credential or Git override %q", value)
		}
	}
	return nil
}

func linuxV2FixedBEnvironmentKey(name string) bool {
	switch name {
	case "HOME", "USER", "LOGNAME", "TMPDIR", "TEMP", "TMP", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "PI_CODING_AGENT_DIR":
		return true
	default:
		return false
	}
}

// linuxV2JournalCallbacks binds every execution-side mutation to one existing
// RunJournal and the claim fence that App accepted. The helper and anchor only
// return observations; all durable authority transitions stay in Store.
func (daemon *daemon) linuxV2JournalCallbacks(key state.RunKey, fence protocol.Fence) execution.LinuxV2JournalCallbacks {
	store := daemon.store
	mutate := func(stage string, write func() (state.RunJournal, error), matches func(state.RunJournal) bool) error {
		return daemon.persistLinuxV2Mutation(store, key, fence, stage, write, matches)
	}
	return execution.LinuxV2JournalCallbacks{
		CheckNativeAdmission: func() error {
			return daemon.checkLinuxV2NativeAdmission(context.Background(), key, fence)
		},
		Reserve: func(value state.LinuxCgroupReservation) error {
			value = value.Clone()
			return mutate("reserve cgroup leaf", func() (state.RunJournal, error) {
				var updated state.RunJournal
				var writeErr error
				admissionErr := daemon.withLinuxV2NativeAdmission(context.Background(), key, func(liveOwners map[state.RunKey]protocol.Fence) error {
					updated, writeErr = store.ReserveLinuxCgroup(key, fence, value, liveOwners)
					return writeErr
				})
				if admissionErr != nil {
					return state.RunJournal{}, admissionErr
				}
				return updated, nil
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(value)
			})
		},
		MarkCreateStarted: func(expected state.LinuxCgroupReservation) error {
			expected = expected.Clone()
			after := expected.Clone()
			after.CreateStarted = true
			return mutate("mark cgroup creation started", func() (state.RunJournal, error) {
				var updated state.RunJournal
				var writeErr error
				admissionErr := daemon.withLinuxV2NativeAdmission(context.Background(), key, func(liveOwners map[state.RunKey]protocol.Fence) error {
					updated, writeErr = store.MarkLinuxCgroupCreateStarted(key, fence, expected, liveOwners)
					return writeErr
				})
				if admissionErr != nil {
					return state.RunJournal{}, admissionErr
				}
				return updated, nil
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		BindLeaf: func(expected state.LinuxCgroupReservation, leaf authority.LinuxCgroupIdentity) error {
			expected = expected.Clone()
			leafCopy := leaf
			after := expected.Clone()
			after.LeafIdentity = &leafCopy
			return mutate("bind pinned cgroup leaf", func() (state.RunJournal, error) {
				return store.BindLinuxCgroupLeaf(key, expected, leaf)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		BindHelper: func(expected state.LinuxCgroupReservation, helper authority.LinuxCgroupProcessIdentity) error {
			expected = expected.Clone()
			helperCopy := helper
			after := expected.Clone()
			after.Helper = &helperCopy
			return mutate("bind owner helper", func() (state.RunJournal, error) {
				return store.BindLinuxCgroupHelper(key, expected, helper)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		BindLauncher: func(expected state.LinuxCgroupReservation, launcher authority.LinuxCgroupProcessIdentity) error {
			expected = expected.Clone()
			launcherCopy := launcher
			after := expected.Clone()
			after.Launcher = &launcherCopy
			return mutate("bind root launcher", func() (state.RunJournal, error) {
				return store.BindLinuxCgroupLauncher(key, expected, launcher)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		ArmLauncherGrant: func(expected state.LinuxCgroupReservation) error {
			expected = expected.Clone()
			after := expected.Clone()
			after.LauncherGrantPossible = true
			return mutate("arm launcher grant", func() (state.RunJournal, error) {
				var updated state.RunJournal
				var writeErr error
				admissionErr := daemon.withLinuxV2NativeAdmission(context.Background(), key, func(liveOwners map[state.RunKey]protocol.Fence) error {
					updated, writeErr = store.ArmLinuxCgroupLauncherGrant(key, fence, expected, liveOwners)
					return writeErr
				})
				if admissionErr != nil {
					return state.RunJournal{}, admissionErr
				}
				return updated, nil
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		RecordLauncherExit: func(expected state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) error {
			expected = expected.Clone()
			proofCopy := proof
			after := expected.Clone()
			after.LauncherExit = &proofCopy
			return mutate("record root launcher wait", func() (state.RunJournal, error) {
				return store.RecordLinuxCgroupLauncherExit(key, expected, proof)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		RecordHelperExit: func(expected state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) error {
			expected = expected.Clone()
			if expected.Helper == nil || expected.HelperExit != nil || !proof.ValidFor(*expected.Helper, expected.Anchor) {
				return errors.New("helper exit proof does not match the exact reserved helper and anchor")
			}
			proofCopy := proof
			after := expected.Clone()
			after.HelperExit = &proofCopy
			return mutate("record exact helper wait/reap", func() (state.RunJournal, error) {
				return store.RecordLinuxCgroupHelperExit(key, expected, proof)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		BindGate: func(expected state.LinuxCgroupReservation, gate authority.LinuxCgroupGateIdentity) error {
			expected = expected.Clone()
			gateCopy := gate
			after := expected.Clone()
			after.Gate = &gateCopy
			return mutate("bind trusted A gate", func() (state.RunJournal, error) {
				return store.BindLinuxCgroupGate(key, expected, gate)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		PrepareHandoff: func(handoff authority.SupervisorHandoff) error {
			handoff = handoff.Clone()
			return mutate("prepare v2 handoff", func() (state.RunJournal, error) {
				return store.PrepareSupervisorHandoff(key, handoff)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Equal(handoff)
			})
		},
		BindHandoffV2: func(expected authority.SupervisorHandoff, helper, mirror authority.LinuxCgroupProcessIdentity) error {
			expected = expected.Clone()
			helperCopy, mirrorCopy := helper, mirror
			after := expected.Clone()
			after.SupervisorPID = helper.PID
			after.SupervisorIdentity = helper.Identity
			after.Mirror = &mirrorCopy
			return mutate("bind v2 handoff owners", func() (state.RunJournal, error) {
				return store.BindSupervisorHandoffV2(key, expected, helperCopy, mirrorCopy)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Equal(after) &&
					journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Helper != nil &&
					journal.ContainmentCgroup.Helper.Equal(helperCopy) && journal.ContainmentCgroup.Mirror != nil &&
					journal.ContainmentCgroup.Mirror.Equal(mirrorCopy)
			})
		},
		CommitHandoff: func(handoff authority.SupervisorHandoff, startedAt time.Time) error {
			handoff = handoff.Clone()
			value, valueErr := handoff.ToSupervisor()
			if valueErr != nil {
				return fmt.Errorf("convert v2 handoff for commit: %w", valueErr)
			}
			value.StopReceipt = nil
			return mutate("commit v2 handoff", func() (state.RunJournal, error) {
				return store.CommitSupervisorHandoff(key, handoff, startedAt)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentHandoff == nil && journal.ContainmentAuthority != nil &&
					journal.ContainmentAuthority.Equal(value) && journal.PID == value.TargetPID &&
					journal.ProcessIdentity == value.TargetIdentity && journal.StartedAt.Equal(startedAt) &&
					journal.ContainmentCgroup != nil && linuxV2ReservationMatchesHandoff(*journal.ContainmentCgroup, handoff)
			})
		},
		RecordStopReceipt: func(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
			handoff = handoff.Clone()
			receipt = cloneLinuxV2StopReceipt(receipt)
			return mutate("record v2 physical stop receipt", func() (state.RunJournal, error) {
				return store.RecordSupervisorHandoffStopReceipt(key, handoff, receipt)
			}, func(journal state.RunJournal) bool {
				return linuxV2JournalHasStopReceipt(journal, handoff, receipt)
			})
		},
		RecordPreGateStopReceipt: func(expected state.LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) error {
			expected = expected.Clone()
			receipt = receipt.Clone()
			after := expected.Clone()
			receiptCopy := receipt.Clone()
			after.PreGateStopReceipt = &receiptCopy
			return mutate("record authenticated pre-gate stop receipt", func() (state.RunJournal, error) {
				return store.RecordLinuxCgroupPreGateStopReceipt(key, expected, receipt)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		AbortReservation: func(expected state.LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) error {
			expected = expected.Clone()
			proof = proof.Clone()
			if expected.Validate() != nil || expected.Gate != nil || expected.LauncherGrantPossible || expected.PreGateStopReceipt != nil ||
				expected.AbortProof != nil || !proof.ValidFor(expected.LaunchToken, expected.Parent, expected.LeafName,
				expected.CreateStarted, expected.OwnerUID, expected.Anchor, *expected.Mirror,
				expected.Helper, expected.Launcher) {
				return errors.New("reservation abort requires the exact pre-effect producer-fencing proof")
			}
			after := expected.Clone()
			proofCopy := proof.Clone()
			after.AbortProof = &proofCopy
			return mutate("abort fenced no-target reservation", func() (state.RunJournal, error) {
				return store.AbortLinuxCgroupReservation(key, expected, proof)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Equal(after)
			})
		},
		ClearHandoffV2: func(handoff authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) error {
			handoff = handoff.Clone()
			proof, issued := release.Snapshot()
			if !issued {
				return errors.New("v2 handoff clear requires an opaque platform release")
			}
			return mutate("clear released v2 handoff", func() (state.RunJournal, error) {
				return store.ClearSupervisorHandoffV2(key, handoff, release)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentHandoff == nil && journal.ContainmentAuthority == nil &&
					journal.ContainmentCgroup == nil && !journal.HasProcessDetails() &&
					journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(proof)
			})
		},
		ClearProcessDetailsV2: func(pid int, identity string, release platform.LinuxCgroupV2Release) error {
			proof, issued := release.Snapshot()
			if !issued || proof.TargetPID != pid || proof.TargetIdentity != identity {
				return errors.New("v2 process clear requires the matching opaque platform release")
			}
			return mutate("clear receipt-released v2 process", func() (state.RunJournal, error) {
				return store.ClearProcessDetailsV2(key, pid, identity, release)
			}, func(journal state.RunJournal) bool {
				return !journal.HasProcessDetails() && journal.PID == 0 && journal.ProcessIdentity == "" &&
					journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil &&
					journal.ContainmentHandoff == nil && journal.ContainmentCgroup == nil &&
					journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(proof)
			})
		},
		ClearReservationV2: func(expected state.LinuxCgroupReservation, release platform.LinuxCgroupV2Release) error {
			expected = expected.Clone()
			proof, issued := release.SnapshotPreGate()
			if !issued {
				return errors.New("pre-gate reservation clear requires an opaque platform release")
			}
			return mutate("clear receipt-released pre-gate reservation", func() (state.RunJournal, error) {
				return store.ClearLinuxCgroupReservationV2(key, expected, release)
			}, func(journal state.RunJournal) bool {
				return journal.ContainmentCgroup == nil && journal.ContainmentAuthority == nil &&
					journal.ContainmentHandoff == nil && !journal.HasProcessDetails() &&
					journal.ContainmentCgroupPreGateReleaseProof != nil &&
					journal.ContainmentCgroupPreGateReleaseProof.Equal(proof)
			})
		},
	}
}

func (daemon *daemon) linuxCloneJournalCallbacks(key state.RunKey, fence protocol.Fence) linuxCloneJournalCallbacks {
	store := daemon.store
	mutate := func(stage string, write func() (state.RunJournal, error), matches func(state.RunJournal) bool) error {
		return daemon.persistLinuxV2Mutation(store, key, fence, stage, write, matches)
	}
	return linuxCloneJournalCallbacks{
		Reserve: func(intent authority.LinuxCloneIntent) error {
			intent = intent.Clone()
			return mutate("reserve standalone clone intent", func() (state.RunJournal, error) {
				var updated state.RunJournal
				var writeErr error
				admissionErr := daemon.withLinuxV2NativeAdmission(context.Background(), key, func(liveOwners map[state.RunKey]protocol.Fence) error {
					updated, writeErr = store.ReserveLinuxCloneIntent(key, fence, intent, liveOwners)
					return writeErr
				})
				if admissionErr != nil {
					return state.RunJournal{}, admissionErr
				}
				return updated, nil
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.WorkspacePath == intent.Target && journal.WorkspaceBindingKey == intent.BindingKey &&
					journal.WorkspaceRecoveryRequired
			})
		},
		BindSeed: func(intent authority.LinuxCloneIntent, receipt authority.LinuxCloneSeedReceipt) error {
			intent, receipt = intent.Clone(), receipt.Clone()
			return mutate("bind immutable A-owned clone seed", func() (state.RunJournal, error) {
				return store.BindLinuxCloneSeed(key, fence, intent, receipt)
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneSeedReceipt.Equal(receipt) &&
					journal.WorkspacePath == intent.Target && journal.WorkspaceBindingKey == intent.BindingKey
			})
		},
		CheckoutReady: func(intent authority.LinuxCloneIntent, receipt authority.LinuxCloneCheckoutReceipt) error {
			intent, receipt = intent.Clone(), receipt.Clone()
			return mutate("record B-owned clone checkout receipt", func() (state.RunJournal, error) {
				return store.RecordLinuxCloneCheckoutReady(key, fence, intent, receipt)
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneCheckoutReceipt != nil &&
					journal.LinuxCloneCheckoutReceipt.Equal(receipt) &&
					journal.WorkspacePath == intent.Target && journal.WorkspaceBindingKey == intent.BindingKey
			})
		},
		ReservePromotion: func(intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent) error {
			intent, candidate = intent.Clone(), candidate.Clone()
			if candidate.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) {
				return errors.New("Linux clone promotion candidate differs from its exact reserved resource or object format")
			}
			return mutate("reserve exact Linux clone promotion intent", func() (state.RunJournal, error) {
				return store.ReserveLinuxClonePromotion(key, fence, intent, candidate)
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneCheckoutReceipt != nil &&
					journal.LinuxClonePromotionIntent != nil && journal.LinuxClonePromotionIntent.Equal(candidate) &&
					(journal.LinuxClonePromotionReceipt == nil ||
						linuxClonePromotionReceiptMatchesIntent(key, candidate, *journal.LinuxClonePromotionReceipt)) &&
					journal.WorkspacePath == intent.Target &&
					journal.WorkspaceBindingKey == intent.BindingKey && linuxClonePromotionStopProven(journal)
			})
		},
		RecordPromotion: func(intent authority.LinuxCloneIntent, candidate authority.LinuxClonePromotionIntent, receipt authority.LinuxClonePromotionReceipt) error {
			intent, candidate, receipt = intent.Clone(), candidate.Clone(), receipt.Clone()
			return mutate("record A-validated clone promotion", func() (state.RunJournal, error) {
				return store.RecordLinuxClonePromotion(key, fence, intent, candidate, receipt)
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneCheckoutReceipt != nil &&
					journal.LinuxClonePromotionIntent != nil && journal.LinuxClonePromotionIntent.Equal(candidate) &&
					journal.LinuxClonePromotionReceipt != nil && journal.LinuxClonePromotionReceipt.Equal(receipt) &&
					journal.WorkspacePath == intent.Target && journal.WorkspaceBindingKey == intent.BindingKey &&
					linuxClonePromotionStopProven(journal)
			})
		},
	}
}

func (daemon *daemon) prepareLinuxV2CloneRun(
	ctx context.Context,
	key state.RunKey,
	fence protocol.Fence,
	admission protocol.Admission,
) (*linuxCloneRun, string, error) {
	if ctx == nil || key.RunID == "" || key.Generation <= 0 || fence.Generation != key.Generation {
		return nil, "", errors.New("Linux clone preparation lacks the exact RunKey, claim fence, or context")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if admission.SessionMode == protocol.SessionModeResume {
		return nil, "", errors.New("Linux clone preparation cannot reuse a prior RunKey target without a separately fenced reuse grant")
	}
	cloneWorkspace, ok := daemon.workspace.(linuxV2CloneWorkspace)
	if !ok {
		return nil, "", errors.New("workspace service does not implement the B-owned git_clone_v2 contract")
	}
	runRef := workspace.RunRef{RunID: key.RunID, Generation: key.Generation}
	intent, err := cloneWorkspace.PlanLinuxCloneIntent(daemon.config.Runtime.Workspace, runRef, admission.Subject)
	if err != nil {
		return nil, "", fmt.Errorf("plan exact Linux clone intent: %w", err)
	}
	if err := intent.Validate(); err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation ||
		intent.BindingKey != daemon.config.Runtime.Workspace || intent.ResourceID != admission.Subject.ResourceID ||
		intent.BaseCommit != admission.Subject.Commit || intent.BaseTreeDigest != admission.Subject.TreeDigest {
		return nil, "", fmt.Errorf("planned Linux clone intent differs from the exact RunKey, binding, or admitted Subject: %w", appErrorOr(err, "identity differs"))
	}
	unlockCloneOperation := daemon.lockLinuxCloneOperation(key)
	cloneOperationLocked := true
	releaseCloneOperation := func() {
		if cloneOperationLocked {
			cloneOperationLocked = false
			unlockCloneOperation()
		}
	}
	defer releaseCloneOperation()
	abortAfterPreparationFailure := func(cause error) error {
		if errors.Is(cause, workspace.ErrLinuxCloneProcessUnresolved) {
			daemon.rememberWorkspaceRetention(key)
			journal, loadErr := daemon.store.LoadJournal(key)
			if loadErr != nil {
				retainErr := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "retain unresolved Linux clone Git process", func() (state.RunJournal, error) {
					return daemon.store.RetainWorkspace(key)
				}, func(current state.RunJournal) bool {
					return current.LinuxCloneIntent != nil && current.LinuxCloneIntent.Equal(intent) &&
						current.LinuxClonePromotionReceipt == nil && current.RetainWorkspace
				})
				releaseCloneOperation()
				return errors.Join(cause, fmt.Errorf("read unresolved Linux clone RunJournal: %w", loadErr), retainErr)
			}
			if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) {
				releaseCloneOperation()
				return cause
			}
			var seed *authority.LinuxCloneSeedReceipt
			if journal.LinuxCloneSeedReceipt != nil {
				copy := journal.LinuxCloneSeedReceipt.Clone()
				seed = &copy
			}
			retainErr := daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
				fmt.Errorf("A-owned Git process-group stop remains unresolved; no clone fence or cleanup was attempted: %w", cause))
			releaseCloneOperation()
			return retainErr
		}
		journal, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			releaseCloneOperation()
			return errors.Join(cause, fmt.Errorf("read Linux clone reservation after preparation failure: %w", loadErr))
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) {
			releaseCloneOperation()
			return cause
		}
		abortContext, cancel := daemon.cleanupContext(context.WithoutCancel(ctx))
		abortErr := daemon.abortLinuxV2CloneBeforeCheckoutLocked(abortContext, key, fence, intent)
		cancel()
		releaseCloneOperation()
		if abortErr != nil {
			return errors.Join(cause, fmt.Errorf("abort exact pre-checkout Linux clone after preparation failure: %w", abortErr))
		}
		return cause
	}
	journalCallbacks := daemon.linuxCloneJournalCallbacks(key, fence)
	if err := journalCallbacks.Reserve(intent); err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("durably reserve exact Linux clone intent before Git effects: %w", err))
	}
	seed, err := cloneWorkspace.PrepareLinuxCloneSeed(ctx, intent)
	if err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("prepare A-owned Linux clone seed after durable reservation: %w", err))
	}
	if err := seed.Validate(); err != nil || seed.BaseCommit != intent.BaseCommit {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("Linux clone seed receipt differs from its reserved base: %w", appErrorOr(err, "base commit differs")))
	}
	if err := journalCallbacks.BindSeed(intent, seed); err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("durably bind exact A-owned Linux clone seed: %w", err))
	}
	grant := authority.LinuxCloneGrant{Intent: intent.Clone(), Seed: seed.Clone()}
	if err := grant.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate durable Linux clone grant: %w", err)
	}
	if _, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, nil); err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("read back exact reserved Linux clone grant: %w", err))
	}
	prepared, err := cloneWorkspace.PrepareLinuxClone(ctx, grant)
	if err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("prepare A-only Linux clone workspace metadata: %w", err))
	}
	if prepared.Path != intent.Target || prepared.BindingKey != intent.BindingKey || prepared.Run != runRef {
		return nil, "", abortAfterPreparationFailure(errors.New("prepared Linux clone workspace differs from the exact reserved B target"))
	}
	fingerprint, err := workspaceFingerprint(ctx, prepared)
	if err != nil {
		return nil, "", abortAfterPreparationFailure(fmt.Errorf("fingerprint A-prepared Linux clone grant: %w", err))
	}
	return &linuxCloneRun{workspace: cloneWorkspace, grant: grant, prepared: prepared}, fingerprint, nil
}

func (daemon *daemon) installLinuxCloneRun(key state.RunKey, cloneRun *linuxCloneRun) error {
	if cloneRun == nil || cloneRun.workspace == nil {
		return errors.New("Linux clone run is incomplete")
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil {
		// Focused unit callers may not install a runningRun. Production
		// assignments do; the callbacks still capture this exact run value.
		return nil
	}
	if active.linuxClone != nil && active.linuxClone != cloneRun {
		return errors.New("RunKey already has a different Linux clone owner")
	}
	active.linuxClone = cloneRun
	return nil
}

func (daemon *daemon) lockLinuxCloneOperation(key state.RunKey) func() {
	if daemon == nil {
		return func() {}
	}
	for {
		daemon.mu.Lock()
		active := daemon.running[key]
		daemon.mu.Unlock()
		if active == nil {
			return func() {}
		}
		active.linuxCloneOperationMu.Lock()
		daemon.mu.Lock()
		stillOwned := daemon.running[key] == active
		daemon.mu.Unlock()
		if stillOwned {
			return active.linuxCloneOperationMu.Unlock
		}
		active.linuxCloneOperationMu.Unlock()
	}
}

func (daemon *daemon) installLinuxCloneRunnerCallbacks(
	key state.RunKey,
	fence protocol.Fence,
	admission protocol.Admission,
	cloneRun *linuxCloneRun,
	callbacks *execution.LinuxV2JournalCallbacks,
) error {
	if callbacks == nil || cloneRun == nil || cloneRun.workspace == nil || cloneRun.grant.Validate() != nil ||
		cloneRun.grant.Intent.RunID != key.RunID || cloneRun.grant.Intent.Generation != key.Generation ||
		cloneRun.grant.Intent.ResourceID != admission.Subject.ResourceID ||
		cloneRun.grant.Intent.BaseCommit != admission.Subject.Commit || cloneRun.grant.Intent.BaseTreeDigest != admission.Subject.TreeDigest ||
		admission.SessionMode == protocol.SessionModeResume {
		return errors.New("Linux clone Runner callbacks do not match the exact non-resume RunKey and admitted Subject")
	}
	if callbacks.RecordLinuxCloneCheckoutReady != nil || callbacks.LinuxCloneProposedCommit != nil ||
		callbacks.RetainUnpromotedLinuxClone != nil || callbacks.PromoteLinuxClone != nil ||
		callbacks.RecordLinuxClonePromotion != nil || callbacks.FinalizeNoCheckoutLinuxCloneAfterRetirement != nil {
		return errors.New("Linux clone callbacks must be supplied only by the App-owned exact journal boundary")
	}
	journalCallbacks := daemon.linuxCloneJournalCallbacks(key, fence)
	grant := cloneRun.grant.Clone()
	callbacks.RecordLinuxCloneCheckoutReady = func(receipt authority.LinuxCloneCheckoutReceipt) error {
		if err := cloneRun.workspace.ValidateLinuxCloneCheckout(grant, receipt); err != nil {
			return fmt.Errorf("validate exact B-owned Linux clone checkout before journaling READY: %w", err)
		}
		if err := journalCallbacks.CheckoutReady(grant.Intent, receipt); err != nil {
			return err
		}
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, &receipt)
		if err != nil {
			return fmt.Errorf("verify durable B-owned Linux clone checkout receipt: %w", err)
		}
		if journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(receipt) {
			return errors.New("Linux clone READY receipt readback differs from the exact gate observation")
		}
		cloneRun.mu.Lock()
		defer cloneRun.mu.Unlock()
		if cloneRun.checkout != nil && !cloneRun.checkout.Equal(receipt) {
			return errors.New("Linux clone checkout callback attempted to replace the durable receipt")
		}
		copy := receipt.Clone()
		cloneRun.checkout = &copy
		return nil
	}
	callbacks.LinuxCloneProposedCommit = func(ctx context.Context, outcome execution.LinuxCloneRunOutcome) (string, bool, error) {
		if ctx == nil {
			return "", false, errors.New("Linux clone proposed-commit context is nil")
		}
		cloneRun.mu.Lock()
		cloneRun.promotionEligible = false
		cloneRun.mu.Unlock()
		if !outcome.PromotionEligible() || ctx.Err() != nil {
			return "", false, nil
		}
		cloneRun.mu.Lock()
		var taskResult *protocol.TaskResult
		var checkout *authority.LinuxCloneCheckoutReceipt
		if cloneRun.taskResult != nil {
			copy := *cloneRun.taskResult
			taskResult = &copy
		}
		if cloneRun.checkout != nil {
			copy := cloneRun.checkout.Clone()
			checkout = &copy
		}
		cloneRun.mu.Unlock()
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, checkout)
		if err != nil {
			return "", false, fmt.Errorf("read exact persisted Linux clone grant and checkout: %w", err)
		}
		if journal.LinuxClonePromotionReceipt != nil {
			return "", false, errors.New("Linux clone already has an A-promotion receipt before candidate selection")
		}
		if taskResult == nil {
			return "", false, nil
		}
		if err := taskResult.Validate(); err != nil {
			return "", false, fmt.Errorf("validate persisted Linux clone TaskResult: %w", err)
		}
		if err := protocol.ValidateTaskResultForAdmission(admission, *taskResult); err != nil {
			return "", false, fmt.Errorf("validate persisted Linux clone TaskResult for admission: %w", err)
		}
		if taskResult.Kind != protocol.TaskResultCandidateCompletion {
			if taskResult.Subject != admission.Subject {
				return "", false, errors.New("non-candidate Linux clone TaskResult differs from the admitted Subject")
			}
			if journal.LinuxClonePromotionIntent != nil {
				return "", false, errors.New("Linux clone has a durable promotion intent for a different TaskResult")
			}
			return "", false, nil
		}
		if taskResult.Subject.ResourceID != grant.Intent.ResourceID {
			return "", false, errors.New("Linux clone candidate ResourceID differs from the reserved source")
		}
		if intent := journal.LinuxClonePromotionIntent; intent != nil &&
			(intent.ResourceID != taskResult.Subject.ResourceID || intent.Commit != taskResult.Subject.Commit || intent.TreeDigest != taskResult.Subject.TreeDigest) {
			return "", false, errors.New("Linux clone candidate differs from its immutable promotion intent")
		}
		if checkout == nil {
			return "", false, nil
		}
		cloneRun.mu.Lock()
		cloneRun.promotionEligible = true
		cloneRun.mu.Unlock()
		return taskResult.Subject.Commit, true, nil
	}
	callbacks.RetainUnpromotedLinuxClone = func(ctx context.Context) error {
		if ctx == nil {
			return errors.New("Linux clone retention context is nil")
		}
		cloneRun.mu.Lock()
		cloneRun.promotionEligible = false
		var cachedCheckout *authority.LinuxCloneCheckoutReceipt
		if cloneRun.checkout != nil {
			copy := cloneRun.checkout.Clone()
			cachedCheckout = &copy
		}
		cloneRun.mu.Unlock()
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, nil)
		if err != nil {
			return fmt.Errorf("verify exact Linux clone before retaining unpromoted target: %w", err)
		}
		if journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone retention cannot replace a durable promotion receipt")
		}
		if journal.LinuxClonePromotionIntent != nil {
			return errors.New("Linux clone with a durable promotion intent cannot be retained as unpromoted")
		}
		var checkout *authority.LinuxCloneCheckoutReceipt
		if journal.LinuxCloneCheckoutReceipt != nil {
			copy := journal.LinuxCloneCheckoutReceipt.Clone()
			checkout = &copy
		}
		if cachedCheckout != nil && (checkout == nil || !cachedCheckout.Equal(*checkout)) {
			return errors.New("Linux clone retention checkout differs from the Store-bound receipt")
		}
		daemon.rememberWorkspaceRetention(key)
		return daemon.persistLinuxV2Mutation(daemon.store, key, fence, "retain unpromoted Linux clone", func() (state.RunJournal, error) {
			return daemon.store.RetainWorkspace(key)
		}, func(journal state.RunJournal) bool {
			checkoutMatches := journal.LinuxCloneCheckoutReceipt == nil && checkout == nil ||
				journal.LinuxCloneCheckoutReceipt != nil && checkout != nil && journal.LinuxCloneCheckoutReceipt.Equal(*checkout)
			return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(grant.Intent) &&
				journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneSeedReceipt.Equal(grant.Seed) &&
				checkoutMatches && journal.LinuxClonePromotionIntent == nil &&
				journal.LinuxClonePromotionReceipt == nil && journal.RetainWorkspace
		})
	}
	callbacks.PromoteLinuxClone = func(
		ctx context.Context,
		callbackGrant authority.LinuxCloneGrant,
		checkout authority.LinuxCloneCheckoutReceipt,
		commit string,
		stopReceipt authority.StopReceipt,
		release platform.LinuxCgroupV2Release,
	) (authority.LinuxClonePromotionReceipt, error) {
		if ctx == nil {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion context is nil")
		}
		unlockCloneOperation := daemon.lockLinuxCloneOperation(key)
		defer unlockCloneOperation()
		if err := ctx.Err(); err != nil {
			return authority.LinuxClonePromotionReceipt{}, err
		}
		if !callbackGrant.Equal(grant) {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion callback changed its exact grant")
		}
		cloneRun.mu.Lock()
		var taskResult *protocol.TaskResult
		var durableCheckout *authority.LinuxCloneCheckoutReceipt
		promotionEligible := cloneRun.promotionEligible
		if cloneRun.taskResult != nil {
			copy := *cloneRun.taskResult
			taskResult = &copy
		}
		if cloneRun.checkout != nil {
			copy := cloneRun.checkout.Clone()
			durableCheckout = &copy
		}
		cloneRun.mu.Unlock()
		if !promotionEligible || taskResult == nil || taskResult.Kind != protocol.TaskResultCandidateCompletion || durableCheckout == nil ||
			!durableCheckout.Equal(checkout) || taskResult.Subject.Commit != commit || taskResult.Subject.ResourceID != grant.Intent.ResourceID {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion differs from the exact persisted candidate and checkout")
		}
		promotionIntent := authority.LinuxClonePromotionIntent{
			Version: authority.LinuxCloneVersion, ResourceID: taskResult.Subject.ResourceID,
			Commit: taskResult.Subject.Commit, TreeDigest: taskResult.Subject.TreeDigest,
		}
		if err := promotionIntent.Validate(); err != nil || promotionIntent.ResourceID != grant.Intent.ResourceID ||
			len(promotionIntent.Commit) != len(grant.Intent.BaseCommit) {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("Linux clone promotion candidate is invalid for its exact resource and object format: %w", appErrorOr(err, "candidate identity differs"))
		}
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, &checkout)
		if err != nil {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("read exact Linux clone state before promotion: %w", err)
		}
		if !linuxCloneDurableStopReady(journal) || journal.ContainmentAuthority == nil ||
			journal.ContainmentAuthority.StopReceipt == nil || !journal.ContainmentAuthority.StopReceipt.Equal(stopReceipt) ||
			!stopReceipt.ValidFor(*journal.ContainmentAuthority) || journal.ContainmentCgroup == nil ||
			journal.ContainmentCgroupReleaseProof != nil {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion lacks the exact durable v2 stop receipt")
		}
		releaseProof, issued := release.Snapshot()
		if !issued || releaseProof.Validate() != nil || !releaseProof.ValidForSupervisor(*journal.ContainmentAuthority) ||
			releaseProof.StopReceipt == nil || !releaseProof.StopReceipt.Equal(stopReceipt) {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion lacks the matching opaque physical-removal release")
		}
		if err := journalCallbacks.ReservePromotion(grant.Intent, promotionIntent); err != nil {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("durably reserve exact Linux clone promotion intent before A Git effects: %w", err)
		}
		journal, err = daemon.loadLinuxCloneRunJournal(key, fence, grant, &checkout)
		if err != nil || journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(promotionIntent) ||
			!linuxClonePromotionStopProven(journal) {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("read back exact Linux clone promotion intent before A Git effects: %w", appErrorOr(err, "intent or physical-stop proof is missing"))
		}
		var promotion authority.LinuxClonePromotionReceipt
		err = daemon.withLinuxV2EffectReadyTarget(ctx, key, fence, func() error {
			var promoteErr error
			promotion, promoteErr = cloneRun.workspace.PromoteLinuxClone(ctx, grant, checkout.Clone(), promotionIntent, release)
			return promoteErr
		})
		if err != nil {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("promote validated stopped Linux clone candidate: %w", err)
		}
		expectedRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
		if err != nil || promotion.Validate() != nil || promotion.Ref != expectedRef || promotion.Commit != promotionIntent.Commit ||
			promotion.TreeDigest != promotionIntent.TreeDigest {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("A-validated Linux clone promotion receipt differs from the proposed candidate: %w", appErrorOr(err, "receipt identity differs"))
		}
		cloneRun.mu.Lock()
		defer cloneRun.mu.Unlock()
		if cloneRun.promotionIntent != nil && !cloneRun.promotionIntent.Equal(promotionIntent) {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion callback attempted to replace its immutable intent")
		}
		if cloneRun.promotion != nil && !cloneRun.promotion.Equal(promotion) {
			return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion callback attempted to replace its exact receipt")
		}
		intentCopy := promotionIntent.Clone()
		copy := promotion.Clone()
		cloneRun.promotionIntent = &intentCopy
		cloneRun.promotion = &copy
		return promotion, nil
	}
	callbacks.RecordLinuxClonePromotion = func(receipt authority.LinuxClonePromotionReceipt) error {
		unlockCloneOperation := daemon.lockLinuxCloneOperation(key)
		defer unlockCloneOperation()
		cloneRun.mu.Lock()
		var expectedIntent *authority.LinuxClonePromotionIntent
		if cloneRun.promotionIntent != nil {
			copy := cloneRun.promotionIntent.Clone()
			expectedIntent = &copy
		}
		var expected *authority.LinuxClonePromotionReceipt
		if cloneRun.promotion != nil {
			copy := cloneRun.promotion.Clone()
			expected = &copy
		}
		var checkout *authority.LinuxCloneCheckoutReceipt
		if cloneRun.checkout != nil {
			copy := cloneRun.checkout.Clone()
			checkout = &copy
		}
		cloneRun.mu.Unlock()
		if expectedIntent == nil || expected == nil || checkout == nil || !expected.Equal(receipt) ||
			receipt.Commit != expectedIntent.Commit || receipt.TreeDigest != expectedIntent.TreeDigest {
			return errors.New("Linux clone promotion receipt was not produced by its exact durable stopped-target intent")
		}
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, checkout)
		if err != nil {
			return fmt.Errorf("read exact Linux clone journal before promotion receipt: %w", err)
		}
		if journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(*expectedIntent) ||
			!linuxClonePromotionStopProven(journal) {
			return errors.New("Linux clone promotion receipt lacks its exact durable intent or physical-stop proof")
		}
		if err := journalCallbacks.RecordPromotion(grant.Intent, *expectedIntent, receipt); err != nil {
			return err
		}
		journal, err = daemon.loadLinuxCloneRunJournal(key, fence, grant, checkout)
		if err != nil || journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(*expectedIntent) ||
			journal.LinuxClonePromotionReceipt == nil || !journal.LinuxClonePromotionReceipt.Equal(receipt) {
			return errors.Join(errors.New("durable Linux clone promotion receipt readback differs"), err)
		}
		return nil
	}
	callbacks.FinalizeNoCheckoutLinuxCloneAfterRetirement = func(
		ctx context.Context,
		intent authority.LinuxCloneIntent,
		seed *authority.LinuxCloneSeedReceipt,
		preGate *authority.LinuxCgroupPreGateReleaseProof,
		cgroupAbort *authority.LinuxCgroupReservationAbortProof,
	) error {
		if ctx == nil {
			return errors.New("Linux clone post-retirement finalization context is nil")
		}
		if !intent.Equal(grant.Intent) || seed == nil || !seed.Equal(grant.Seed) ||
			(preGate != nil && cgroupAbort != nil) {
			return errors.New("Linux clone post-retirement callback differs from its exact reserved grant or proof variant")
		}
		return daemon.abortLinuxV2CloneAfterRetirement(ctx, key, fence, grant.Intent, seed, preGate, cgroupAbort)
	}
	return nil
}

func (daemon *daemon) loadLinuxCloneRunJournal(
	key state.RunKey,
	fence protocol.Fence,
	grant authority.LinuxCloneGrant,
	checkout *authority.LinuxCloneCheckoutReceipt,
) (state.RunJournal, error) {
	if daemon == nil || daemon.store == nil || grant.Validate() != nil {
		return state.RunJournal{}, errors.New("Linux clone Store readback is incomplete")
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return state.RunJournal{}, err
	}
	if !linuxV2JournalMatchesFence(journal, key, fence) || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(grant.Intent) || journal.LinuxCloneSeedReceipt == nil ||
		!journal.LinuxCloneSeedReceipt.Equal(grant.Seed) || journal.WorkspacePath != grant.Intent.Target ||
		journal.WorkspaceBindingKey != grant.Intent.BindingKey {
		return state.RunJournal{}, errors.New("Linux clone RunJournal differs from the exact RunKey, fence, grant, or target")
	}
	if checkout != nil && (journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(*checkout)) {
		return state.RunJournal{}, errors.New("Linux clone RunJournal lacks the exact B-owned checkout receipt")
	}
	return journal, nil
}

// reconcileLinuxClonePromotion finishes only a promotion whose exact candidate
// was durably reserved before the A-owned ref effect. An existing release
// tombstone authorizes read-only A-ref reconciliation after restart; creating a
// missing ref still passes the current exact target lease/lifecycle gate and
// never reconstructs a platform capability.
func (daemon *daemon) reconcileLinuxClonePromotion(
	ctx context.Context,
	expected state.RunJournal,
	release platform.LinuxCgroupV2Release,
) error {
	if ctx == nil || daemon == nil || daemon.store == nil {
		return errors.New("Linux clone promotion reconciliation is incomplete")
	}
	unlockCloneOperation := daemon.lockLinuxCloneOperation(expected.Key())
	defer unlockCloneOperation()
	if expected.LinuxClonePromotionIntent == nil {
		if expected.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone promotion receipt has no durable promotion intent")
		}
		return nil
	}
	key := expected.Key()
	fence := linuxV2FenceFromJournal(expected)
	if !linuxV2JournalMatchesFence(expected, key, fence) || expected.LinuxCloneIntent == nil ||
		expected.LinuxCloneSeedReceipt == nil || expected.LinuxCloneCheckoutReceipt == nil {
		return errors.New("Linux clone promotion recovery lacks the exact RunKey, fence, grant, or checkout")
	}
	intent := expected.LinuxCloneIntent.Clone()
	candidate := expected.LinuxClonePromotionIntent.Clone()
	if candidate.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) {
		return errors.New("Linux clone promotion recovery intent differs from the exact clone base")
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("reload Linux clone promotion RunJournal: %w", err)
	}
	if !linuxV2JournalMatchesFence(journal, key, fence) || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(intent) || journal.LinuxClonePromotionIntent == nil ||
		!journal.LinuxClonePromotionIntent.Equal(candidate) || journal.LinuxCloneSeedReceipt == nil ||
		!journal.LinuxCloneSeedReceipt.Equal(*expected.LinuxCloneSeedReceipt) || journal.LinuxCloneCheckoutReceipt == nil ||
		!journal.LinuxCloneCheckoutReceipt.Equal(*expected.LinuxCloneCheckoutReceipt) {
		return errors.New("Linux clone promotion journal changed after its recovery snapshot")
	}
	if journal.LinuxClonePromotionReceipt != nil {
		if !linuxClonePromotionReceiptMatchesIntent(key, candidate, *journal.LinuxClonePromotionReceipt) {
			return errors.New("durable Linux clone promotion receipt differs from its immutable intent")
		}
		return nil
	}
	if !linuxClonePromotionStopProven(journal) || !linuxClonePromotionReleaseProven(journal, release) {
		return errors.New("Linux clone promotion recovery lacks its exact physical-stop and release evidence")
	}
	cloneWorkspace, ok := daemon.workspace.(linuxV2CloneWorkspace)
	if !ok {
		return errors.New("workspace service does not implement Linux clone promotion reconciliation")
	}
	grant := authority.LinuxCloneGrant{
		Intent: intent.Clone(), Seed: journal.LinuxCloneSeedReceipt.Clone(),
	}
	checkout := journal.LinuxCloneCheckoutReceipt.Clone()
	if err := grant.Validate(); err != nil {
		return fmt.Errorf("validate Linux clone promotion recovery grant: %w", err)
	}
	receipt, readErr := cloneWorkspace.ReadExistingLinuxClonePromotion(ctx, intent, candidate, journal, release)
	if errors.Is(readErr, workspace.ErrLinuxClonePromotionRefMissing) {
		_, issued := release.Snapshot()
		if !issued {
			return errors.New("Linux clone promotion ref is absent after restart and no opaque release remains for a new effect")
		}
		if err := cloneWorkspace.ValidateLinuxCloneCheckout(grant, checkout); err != nil {
			return fmt.Errorf("validate exact B checkout before promotion recovery effect: %w", err)
		}
		readErr = daemon.withLinuxV2EffectReadyTarget(ctx, key, fence, func() error {
			var promoteErr error
			receipt, promoteErr = cloneWorkspace.PromoteLinuxClone(ctx, grant, checkout, candidate, release)
			return promoteErr
		})
		if readErr != nil {
			return fmt.Errorf("create missing A-owned Linux clone promotion ref: %w", readErr)
		}
	}
	if readErr != nil {
		return fmt.Errorf("read back or complete exact A-owned Linux clone promotion: %w", readErr)
	}
	if !linuxClonePromotionReceiptMatchesIntent(key, candidate, receipt) {
		return errors.New("A-owned Linux clone promotion readback differs from the exact RunKey intent")
	}
	callbacks := daemon.linuxCloneJournalCallbacks(key, fence)
	if err := callbacks.RecordPromotion(intent, candidate, receipt); err != nil {
		return fmt.Errorf("record reconciled Linux clone promotion receipt: %w", err)
	}
	readback, err := daemon.store.LoadJournal(key)
	if err != nil || !linuxV2JournalMatchesFence(readback, key, fence) || readback.LinuxClonePromotionIntent == nil ||
		!readback.LinuxClonePromotionIntent.Equal(candidate) || readback.LinuxClonePromotionReceipt == nil ||
		!readback.LinuxClonePromotionReceipt.Equal(receipt) {
		return errors.Join(errors.New("reconciled Linux clone promotion receipt did not read back exactly"), err)
	}
	return nil
}

func linuxClonePromotionReleaseProven(journal state.RunJournal, release platform.LinuxCgroupV2Release) bool {
	proof, issued := release.Snapshot()
	if issued {
		if proof.Validate() != nil {
			return false
		}
		if owner := journal.ContainmentAuthority; owner != nil {
			return owner.StopReceipt != nil && proof.ValidForSupervisor(*owner) &&
				proof.StopReceipt != nil && proof.StopReceipt.Equal(*owner.StopReceipt) &&
				(journal.ContainmentCgroupReleaseProof == nil || journal.ContainmentCgroupReleaseProof.Equal(proof))
		}
		if handoff := journal.ContainmentHandoff; handoff != nil {
			return handoff.StopReceipt != nil && proof.ValidFor(*handoff) &&
				proof.StopReceipt != nil && proof.StopReceipt.Equal(*handoff.StopReceipt) &&
				(journal.ContainmentCgroupReleaseProof == nil || journal.ContainmentCgroupReleaseProof.Equal(proof))
		}
		return journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(proof)
	}
	return journal.ContainmentCgroupReleaseProof != nil &&
		journal.ContainmentCgroupReleaseProof.Validate() == nil &&
		journal.ContainmentCgroupReleaseProof.Version == authority.SupervisorHandoffVersionV2 &&
		journal.ContainmentCgroupReleaseProof.StopReceipt != nil
}

func linuxClonePromotionReceiptMatchesIntent(
	key state.RunKey,
	candidate authority.LinuxClonePromotionIntent,
	receipt authority.LinuxClonePromotionReceipt,
) bool {
	expectedRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	return err == nil && candidate.Validate() == nil && receipt.Validate() == nil &&
		receipt.Version == candidate.Version && receipt.Commit == candidate.Commit &&
		receipt.TreeDigest == candidate.TreeDigest && receipt.Ref == expectedRef
}

func (daemon *daemon) abortLinuxV2CloneBeforeCheckout(
	ctx context.Context,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
) error {
	unlockCloneOperation := daemon.lockLinuxCloneOperation(key)
	defer unlockCloneOperation()
	return daemon.abortLinuxV2CloneBeforeCheckoutLocked(ctx, key, fence, intent)
}

func (daemon *daemon) abortLinuxV2CloneBeforeCheckoutLocked(
	ctx context.Context,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read Linux clone before no-checkout cleanup: %w", err)
	}
	seed, preGate, cgroupAbort, _, err := linuxCloneAbortEvidence(journal, key, fence, intent)
	if err != nil {
		return err
	}
	return daemon.abortLinuxV2CloneAfterRetirementLocked(ctx, key, fence, intent, seed, preGate, cgroupAbort)
}

func (daemon *daemon) abortLinuxV2CloneAfterRetirement(
	ctx context.Context,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
	expectedSeed *authority.LinuxCloneSeedReceipt,
	expectedPreGate *authority.LinuxCgroupPreGateReleaseProof,
	expectedCgroupAbort *authority.LinuxCgroupReservationAbortProof,
) error {
	unlockCloneOperation := daemon.lockLinuxCloneOperation(key)
	defer unlockCloneOperation()
	return daemon.abortLinuxV2CloneAfterRetirementLocked(ctx, key, fence, intent, expectedSeed, expectedPreGate, expectedCgroupAbort)
}

func (daemon *daemon) abortLinuxV2CloneAfterRetirementLocked(
	ctx context.Context,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
	expectedSeed *authority.LinuxCloneSeedReceipt,
	expectedPreGate *authority.LinuxCgroupPreGateReleaseProof,
	expectedCgroupAbort *authority.LinuxCgroupReservationAbortProof,
) error {
	if ctx == nil {
		return errors.New("Linux clone no-checkout cleanup context is nil")
	}
	cleanupContext, cancel := daemon.cleanupContext(context.WithoutCancel(ctx))
	defer cancel()
	if intent.Validate() != nil || intent.RunID != key.RunID || intent.Generation != key.Generation {
		return errors.New("Linux clone no-checkout cleanup intent differs from its RunKey")
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read Linux clone abort journal: %w", err)
	}
	seed, preGate, cgroupAbort, normalRelease, err := linuxCloneAbortEvidence(journal, key, fence, intent)
	if err != nil {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, expectedSeed, err)
	}
	if !sameOptionalLinuxCloneSeed(seed, expectedSeed) ||
		!sameOptionalPreGateRelease(preGate, expectedPreGate) ||
		!sameOptionalCgroupAbort(cgroupAbort, expectedCgroupAbort) {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, expectedSeed,
			errors.New("Linux clone abort callback proof differs from exact Store readback"))
	}
	if journal.LinuxCloneAbortProof != nil {
		proof := journal.LinuxCloneAbortProof.Clone()
		if !linuxCloneAbortProofMatchesStore(proof, intent, seed, preGate, cgroupAbort, normalRelease) {
			return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
				errors.New("persisted Linux clone abort tombstone differs from its exact Store evidence"))
		}
		return daemon.completeLinuxCloneAbort(key, fence, intent, proof)
	}

	if err := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "fence Linux clone before checkout", func() (state.RunJournal, error) {
		return daemon.store.FenceLinuxCloneBeforeCheckout(key, fence, intent)
	}, func(current state.RunJournal) bool {
		return linuxCloneAbortFenceMatches(current, key, fence, intent, seed, preGate, cgroupAbort, normalRelease) &&
			current.LinuxCloneAbortPending && current.LinuxCloneAbortProof == nil
	}); err != nil {
		latest, loadErr := daemon.store.LoadJournal(key)
		if loadErr == nil && latest.LinuxCloneAbortProof != nil {
			proof := latest.LinuxCloneAbortProof.Clone()
			if linuxCloneAbortProofMatchesStore(proof, intent, seed, preGate, cgroupAbort, normalRelease) {
				return daemon.completeLinuxCloneAbort(key, fence, intent, proof)
			}
		}
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			errors.Join(fmt.Errorf("fence Linux clone producer before no-target cleanup: %w", err), loadErr))
	}
	journal, err = daemon.store.LoadJournal(key)
	if err != nil {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			fmt.Errorf("read back fenced Linux clone before workspace cleanup: %w", err))
	}
	seed, preGate, cgroupAbort, currentNormalRelease, err := linuxCloneAbortEvidence(journal, key, fence, intent)
	if err != nil || !sameOptionalLinuxCloneSeed(seed, expectedSeed) ||
		!sameOptionalPreGateRelease(preGate, expectedPreGate) ||
		!sameOptionalCgroupAbort(cgroupAbort, expectedCgroupAbort) ||
		!sameOptionalNormalRelease(currentNormalRelease, normalRelease) ||
		!linuxCloneAbortFenceMatches(journal, key, fence, intent, seed, preGate, cgroupAbort, normalRelease) ||
		!journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			errors.Join(errors.New("Linux clone no-producer fence did not read back exactly"), err))
	}

	cloneWorkspace, ok := daemon.workspace.(linuxV2CloneWorkspace)
	if !ok {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			errors.New("workspace service does not implement Linux clone no-checkout cleanup"))
	}
	retained, retainErr := daemon.linuxCloneAbortHasRetentionOwner(journal)
	if retainErr != nil || retained {
		cause := errors.New("Linux clone no-checkout cleanup has a retained GoalSession or other workspace owner")
		if retainErr != nil {
			cause = errors.Join(cause, retainErr)
		}
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed, cause)
	}
	proof, err := cloneWorkspace.AbortLinuxCloneBeforeCheckout(cleanupContext, intent, seed, preGate, cgroupAbort, normalRelease)
	if err != nil {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			fmt.Errorf("clean exact A-owned clone seed/ref and prove B target absent: %w", err))
	}
	if !linuxCloneAbortProofMatchesStore(proof, intent, seed, preGate, cgroupAbort, normalRelease) {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, seed,
			errors.New("Workspace Linux clone abort proof differs from exact fenced Store evidence"))
	}
	return daemon.completeLinuxCloneAbort(key, fence, intent, proof)
}

func (daemon *daemon) completeLinuxCloneAbort(
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
	proof authority.LinuxCloneAbortProof,
) error {
	if err := proof.Validate(); err != nil || !proof.Intent.Equal(intent) {
		return errors.New("Linux clone abort tombstone is invalid for the exact intent")
	}
	if err := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "complete fenced Linux clone abort", func() (state.RunJournal, error) {
		return daemon.store.CompleteLinuxCloneAbort(key, fence, intent, proof)
	}, func(journal state.RunJournal) bool {
		return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
			journal.LinuxCloneAbortProof != nil && journal.LinuxCloneAbortProof.Equal(proof) &&
			sameOptionalLinuxCloneSeed(journal.LinuxCloneSeedReceipt, proof.SeedReceipt) &&
			!journal.LinuxCloneAbortPending && journal.WorkspacePath == "" && !journal.WorkspaceRecoveryRequired
	}); err != nil {
		latest, loadErr := daemon.store.LoadJournal(key)
		if loadErr == nil && latest.LinuxCloneAbortProof != nil && latest.LinuxCloneAbortProof.Equal(proof) &&
			!latest.LinuxCloneAbortPending {
			if retryErr := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "replay completed Linux clone abort", func() (state.RunJournal, error) {
				return daemon.store.CompleteLinuxCloneAbort(key, fence, intent, proof)
			}, func(journal state.RunJournal) bool {
				return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
					journal.LinuxCloneAbortProof != nil && journal.LinuxCloneAbortProof.Equal(proof) &&
					!journal.LinuxCloneAbortPending && journal.WorkspacePath == "" && !journal.WorkspaceRecoveryRequired
			}); retryErr == nil {
				return nil
			} else {
				err = errors.Join(err, retryErr)
			}
		}
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, proof.SeedReceipt,
			errors.Join(fmt.Errorf("persist exact Linux clone abort tombstone: %w", err), loadErr))
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || journal.LinuxCloneAbortProof == nil || !journal.LinuxCloneAbortProof.Equal(proof) ||
		journal.LinuxCloneAbortPending || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired {
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, proof.SeedReceipt,
			errors.Join(errors.New("completed Linux clone abort did not read back exactly"), err))
	}
	retained, retainErr := daemon.linuxCloneAbortHasRetentionOwner(journal)
	if retainErr != nil || retained {
		cause := errors.New("completed Linux clone abort still has a retained GoalSession or other workspace owner")
		if retainErr != nil {
			cause = errors.Join(cause, retainErr)
		}
		return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, proof.SeedReceipt, cause)
	}
	if journal.RetainWorkspace {
		if err := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "clear retention after exact no-target abort", func() (state.RunJournal, error) {
			return daemon.store.ClearLinuxCloneNoTargetRetention(key, fence, proof)
		}, func(current state.RunJournal) bool {
			return linuxV2JournalMatchesFence(current, key, fence) && current.LinuxCloneIntent != nil &&
				current.LinuxCloneIntent.Equal(intent) && current.LinuxCloneAbortProof != nil &&
				current.LinuxCloneAbortProof.Equal(proof) && !current.LinuxCloneAbortPending && !current.RetainWorkspace &&
				current.WorkspacePath == "" && !current.WorkspaceRecoveryRequired
		}); err != nil {
			return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, proof.SeedReceipt,
				fmt.Errorf("clear retention after exact no-target abort: %w", err))
		}
		journal, err = daemon.store.LoadJournal(key)
		if err != nil || journal.LinuxCloneAbortProof == nil || !journal.LinuxCloneAbortProof.Equal(proof) ||
			journal.RetainWorkspace || journal.LinuxCloneAbortPending || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired {
			return daemon.retainLinuxCloneAbortUncertainty(key, fence, intent, proof.SeedReceipt,
				errors.Join(errors.New("no-target retention clear did not read back exactly"), err))
		}
	}
	daemon.forgetWorkspaceRetention(key)
	return nil
}

func (daemon *daemon) linuxCloneAbortHasRetentionOwner(journal state.RunJournal) (bool, error) {
	if daemon == nil || daemon.store == nil || journal.LinuxCloneIntent == nil ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven ||
		!journal.LinuxCloneAbortPending && journal.LinuxCloneAbortProof == nil {
		return false, errors.New("Linux clone retention check lacks the exact stopped no-checkout journal")
	}
	if journal.HasPendingGoalDeliveries() {
		return true, nil
	}
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return false, fmt.Errorf("list Goal sessions before Linux clone no-target cleanup: %w", err)
	}
	key := journal.Key()
	for _, session := range sessions {
		owner := session.WorkspaceOwnerRunKey
		if owner.RunID == "" || owner.Generation <= 0 {
			if session.WorkspacePath == "" || session.WorkspacePath == journal.LinuxCloneIntent.Target {
				return true, nil
			}
			owner = state.WorkspaceOwnerRunKey{RunID: session.RunID, Generation: session.Generation}
		}
		if owner.RunID != key.RunID || owner.Generation != key.Generation {
			if session.WorkspacePath != "" && session.WorkspacePath == journal.LinuxCloneIntent.Target {
				return true, nil
			}
			continue
		}
		if session.SessionState != state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateClosed ||
			session.NeedsReconciliation() {
			return true, nil
		}
		currentKey := state.RunKey{RunID: session.RunID, Generation: session.Generation}
		current := journal
		if currentKey != key {
			current, err = daemon.store.LoadJournal(currentKey)
			if err != nil {
				return false, fmt.Errorf("load current retained GoalSession RunJournal: %w", err)
			}
		}
		if current.HasPendingGoalDeliveries() || currentKey != key &&
			(current.RetainWorkspace || current.WorkspaceRecoveryRequired) {
			return true, nil
		}
	}
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return false, fmt.Errorf("list RunJournals before Linux clone no-target cleanup: %w", err)
	}
	intent := journal.LinuxCloneIntent
	for _, other := range journals {
		if other.Key() == key {
			continue
		}
		if other.WorkspacePath != "" && other.WorkspacePath == intent.Target {
			return true, nil
		}
		if other.LinuxCloneIntent != nil &&
			(other.LinuxCloneIntent.Target == intent.Target || other.LinuxCloneIntent.SeedPath == intent.SeedPath) {
			if other.LinuxCloneAbortProof != nil && !other.LinuxCloneAbortPending && !other.RetainWorkspace &&
				other.WorkspacePath == "" && !other.WorkspaceRecoveryRequired && linuxCloneAbortProofMatchesJournal(other) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func linuxCloneAbortEvidence(
	journal state.RunJournal,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
) (*authority.LinuxCloneSeedReceipt, *authority.LinuxCgroupPreGateReleaseProof,
	*authority.LinuxCgroupReservationAbortProof, *authority.SupervisorHandoffReleaseProof, error) {
	if !linuxV2JournalMatchesFence(journal, key, fence) || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil || journal.HasProcessDetails() ||
		journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven {
		return nil, nil, nil, nil, errors.New("Linux clone abort is not fenced to an exact no-checkout stopped RunJournal")
	}
	var seed *authority.LinuxCloneSeedReceipt
	if journal.LinuxCloneSeedReceipt != nil {
		copy := journal.LinuxCloneSeedReceipt.Clone()
		seed = &copy
	}
	var normalRelease *authority.SupervisorHandoffReleaseProof
	if journal.ContainmentCgroupReleaseProof != nil {
		proof := journal.ContainmentCgroupReleaseProof.Clone()
		if proof.Validate() != nil || proof.Version != authority.SupervisorHandoffVersionV2 ||
			proof.StopReceipt == nil || proof.CgroupIdentity == nil {
			return nil, nil, nil, nil, errors.New("Linux clone normal release proof is invalid for no-checkout cleanup")
		}
		normalRelease = &proof
	}
	var preGate *authority.LinuxCgroupPreGateReleaseProof
	if journal.ContainmentCgroupPreGateReleaseProof != nil {
		copy := journal.ContainmentCgroupPreGateReleaseProof.Clone()
		preGate = &copy
	}
	var cgroupAbort *authority.LinuxCgroupReservationAbortProof
	if journal.ContainmentCgroup != nil {
		if journal.ContainmentCgroup.AbortProof == nil {
			return nil, nil, nil, nil, errors.New("Linux clone abort has an unresolved cgroup reservation")
		}
		copy := journal.ContainmentCgroup.AbortProof.Clone()
		cgroupAbort = &copy
	}
	if normalRelease != nil && (preGate != nil || cgroupAbort != nil || journal.ContainmentCgroup != nil) ||
		preGate != nil && (cgroupAbort != nil || journal.ContainmentCgroup != nil) ||
		preGate == nil && cgroupAbort != nil && journal.ContainmentCgroup == nil ||
		preGate == nil && cgroupAbort == nil && normalRelease == nil &&
			(journal.ContainmentCgroup != nil || journal.ContainmentCgroupPreGateReleaseProof != nil || journal.ContainmentCgroupReleaseProof != nil) ||
		normalRelease == nil && journal.ContainmentCgroupReleaseProof != nil ||
		normalRelease != nil && journal.ContainmentCgroupReleaseProof == nil {
		return nil, nil, nil, nil, errors.New("Linux clone abort proof variant does not match the exact cgroup history")
	}
	return seed, preGate, cgroupAbort, normalRelease, nil
}

func linuxCloneAbortFenceMatches(
	journal state.RunJournal,
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
	seed *authority.LinuxCloneSeedReceipt,
	preGate *authority.LinuxCgroupPreGateReleaseProof,
	cgroupAbort *authority.LinuxCgroupReservationAbortProof,
	normalRelease *authority.SupervisorHandoffReleaseProof,
) bool {
	if !linuxV2JournalMatchesFence(journal, key, fence) || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		!sameOptionalLinuxCloneSeed(journal.LinuxCloneSeedReceipt, seed) ||
		!sameOptionalPreGateRelease(journal.ContainmentCgroupPreGateReleaseProof, preGate) ||
		!sameOptionalNormalRelease(journal.ContainmentCgroupReleaseProof, normalRelease) || journal.HasProcessDetails() ||
		journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven ||
		journal.WorkspacePath != intent.Target || journal.WorkspaceBindingKey != intent.BindingKey ||
		!journal.WorkspaceRecoveryRequired {
		return false
	}
	if cgroupAbort == nil {
		return journal.ContainmentCgroup == nil && (preGate == nil || journal.ContainmentCgroupPreGateReleaseProof != nil) &&
			(normalRelease == nil || journal.ContainmentCgroupReleaseProof != nil)
	}
	return normalRelease == nil && journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof != nil &&
		journal.ContainmentCgroup.AbortProof.Equal(*cgroupAbort) && preGate == nil
}

func linuxCloneAbortProofMatchesStore(
	proof authority.LinuxCloneAbortProof,
	intent authority.LinuxCloneIntent,
	seed *authority.LinuxCloneSeedReceipt,
	preGate *authority.LinuxCgroupPreGateReleaseProof,
	cgroupAbort *authority.LinuxCgroupReservationAbortProof,
	normalRelease *authority.SupervisorHandoffReleaseProof,
) bool {
	return proof.Validate() == nil && proof.Intent.Equal(intent) &&
		sameOptionalLinuxCloneSeed(proof.SeedReceipt, seed) &&
		sameOptionalPreGateRelease(proof.PreGateReleaseProof, preGate) &&
		sameOptionalCgroupAbort(proof.CgroupAbortProof, cgroupAbort) &&
		sameOptionalNormalRelease(proof.NormalReleaseProof, normalRelease)
}

func sameOptionalNormalRelease(left, right *authority.SupervisorHandoffReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func (daemon *daemon) retainLinuxCloneAbortUncertainty(
	key state.RunKey,
	fence protocol.Fence,
	intent authority.LinuxCloneIntent,
	seed *authority.LinuxCloneSeedReceipt,
	cause error,
) error {
	daemon.rememberWorkspaceRetention(key)
	retainErr := daemon.persistLinuxV2Mutation(daemon.store, key, fence, "retain uncertain Linux clone abort", func() (state.RunJournal, error) {
		return daemon.store.RetainWorkspace(key)
	}, func(journal state.RunJournal) bool {
		return journal.LinuxCloneIntent != nil && journal.LinuxCloneIntent.Equal(intent) &&
			journal.LinuxClonePromotionIntent == nil && journal.LinuxClonePromotionReceipt == nil && journal.RetainWorkspace &&
			(seed == nil && journal.LinuxCloneSeedReceipt == nil || seed != nil && journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneSeedReceipt.Equal(*seed))
	})
	return errors.Join(cause, retainErr)
}

func sameOptionalLinuxCloneSeed(left, right *authority.LinuxCloneSeedReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalPreGateRelease(left, right *authority.LinuxCgroupPreGateReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalCgroupAbort(left, right *authority.LinuxCgroupReservationAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func linuxCloneDurableStopReady(journal state.RunJournal) bool {
	owner := journal.ContainmentAuthority
	return journal.ContainmentHandoff == nil && journal.ContainmentCgroup != nil &&
		journal.ContainmentCgroupReleaseProof == nil && owner != nil && owner.Version == authority.SupervisorVersionV2 &&
		owner.StopReceipt != nil && owner.StopReceipt.ValidFor(*owner)
}

func linuxClonePromotionStopProven(journal state.RunJournal) bool {
	if linuxCloneDurableStopReady(journal) {
		return true
	}
	proof := journal.ContainmentCgroupReleaseProof
	return proof != nil && proof.Version == authority.SupervisorHandoffVersionV2 && proof.Validate() == nil &&
		proof.StopReceipt != nil
}

func linuxV2ReservationMatchesHandoff(reservation state.LinuxCgroupReservation, handoff authority.SupervisorHandoff) bool {
	if reservation.Version != state.LinuxCgroupReservationVersion || handoff.Version != authority.SupervisorHandoffVersionV2 ||
		reservation.LaunchToken != handoff.LaunchToken || reservation.Secret != handoff.Secret ||
		reservation.PipeToken != handoff.PipeToken || reservation.JobID != handoff.JobID ||
		reservation.RecoveryEndpoint != handoff.OwnerContext || !reservation.CreateStarted || !reservation.LauncherGrantPossible ||
		reservation.OwnerUID != handoff.OwnerUID || reservation.LauncherUID != handoff.LauncherUID ||
		reservation.LeafIdentity == nil || handoff.CgroupIdentity == nil || !reservation.LeafIdentity.Equal(*handoff.CgroupIdentity) ||
		reservation.Helper == nil || reservation.Helper.PID != handoff.SupervisorPID ||
		reservation.Helper.Identity != handoff.SupervisorIdentity || reservation.Launcher == nil ||
		reservation.Launcher.PID != handoff.LauncherPID || reservation.Launcher.Identity != handoff.LauncherIdentity ||
		reservation.LauncherExit == nil || handoff.LauncherExit == nil || !reservation.LauncherExit.Equal(*handoff.LauncherExit) ||
		reservation.Gate == nil || reservation.Gate.Process.PID != handoff.TargetPID ||
		reservation.Gate.Process.Identity != handoff.TargetIdentity || reservation.Gate.BirthKind != handoff.TargetBirthKind ||
		reservation.Mirror == nil || handoff.Mirror == nil || !reservation.Mirror.Equal(*handoff.Mirror) ||
		reservation.Anchor.PID != handoff.AnchorPID || reservation.Anchor.Identity != handoff.AnchorIdentity {
		return false
	}
	return true
}

func linuxV2JournalHasStopReceipt(journal state.RunJournal, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) bool {
	baseHandoff := handoff.Clone()
	baseHandoff.StopReceipt = nil
	if journal.ContainmentCgroup == nil || !linuxV2ReservationMatchesHandoff(*journal.ContainmentCgroup, baseHandoff) {
		return false
	}
	if journal.ContainmentHandoff != nil {
		current := journal.ContainmentHandoff.Clone()
		if current.StopReceipt == nil || !current.StopReceipt.Equal(receipt) {
			return false
		}
		current.StopReceipt = nil
		return current.Equal(baseHandoff)
	}
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil ||
		!journal.ContainmentAuthority.StopReceipt.Equal(receipt) {
		return false
	}
	expected, err := baseHandoff.ToSupervisor()
	if err != nil {
		return false
	}
	current := journal.ContainmentAuthority.Clone()
	current.StopReceipt = nil
	expected.StopReceipt = nil
	return current.Equal(expected) && journal.PID == expected.TargetPID && journal.ProcessIdentity == expected.TargetIdentity
}

func (daemon *daemon) persistLinuxV2Mutation(
	store *state.Store,
	key state.RunKey,
	fence protocol.Fence,
	stage string,
	write func() (state.RunJournal, error),
	matches func(state.RunJournal) bool,
) error {
	daemon.storeLifetimeMu.RLock()
	defer daemon.storeLifetimeMu.RUnlock()
	if daemon.storeClosing || store == nil || store != daemon.store {
		return errDaemonStoreClosing
	}
	if journal, err := store.LoadJournal(key); err != nil {
		return fmt.Errorf("verify v2 %s subject before mutation: %w", stage, err)
	} else if !linuxV2JournalMatchesFence(journal, key, fence) {
		return fmt.Errorf("v2 %s subject no longer matches the exact RunKey and claim fence", stage)
	}
	journal, err := write()
	if err == nil {
		if !linuxV2JournalMatchesFence(journal, key, fence) || !matches(journal) {
			return fmt.Errorf("v2 %s mutation returned a mismatched journal subject", stage)
		}
		return nil
	}
	firstErr := err

	// The first error may follow rename but precede the directory-sync ACK.
	// Re-submit the exact same Store CAS to obtain a new persistence barrier;
	// readback alone never upgrades an unknown write to durable authority.
	replayed, replayErr := write()
	if replayErr != nil {
		return errors.Join(firstErr, fmt.Errorf("replay exact v2 %s mutation: %w", stage, replayErr))
	}
	if !linuxV2JournalMatchesFence(replayed, key, fence) || !matches(replayed) {
		return errors.Join(firstErr, fmt.Errorf("replayed v2 %s mutation returned a mismatched journal subject", stage))
	}
	readback, readErr := store.LoadJournal(key)
	if readErr != nil {
		return errors.Join(firstErr, fmt.Errorf("read back replayed v2 %s mutation: %w", stage, readErr))
	}
	if !linuxV2JournalMatchesFence(readback, key, fence) || !matches(readback) {
		return errors.Join(firstErr, fmt.Errorf("replayed v2 %s mutation readback differs from the exact subject", stage))
	}
	return nil
}

func cloneLinuxV2StopReceipt(receipt authority.StopReceipt) authority.StopReceipt {
	owner := authority.Supervisor{StopReceipt: &receipt}.Clone()
	return *owner.StopReceipt
}

func linuxV2JournalMatchesFence(journal state.RunJournal, key state.RunKey, fence protocol.Fence) bool {
	return journal.RunID == key.RunID && journal.Generation == key.Generation &&
		fence.Generation == key.Generation && journal.RuntimeID == fence.RuntimeID &&
		journal.ClaimedRuntimeEpoch == fence.RuntimeEpoch && journal.ClaimID == fence.ClaimID &&
		journal.LeaseToken == fence.LeaseToken
}
