//go:build linux && amd64

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/app"
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxV2CleanupWitnessRootEnvironment   = "SYMMETRY_PRODUCTION_LINUX_V2_SYNTHETIC_GOAL_CLEANUP_ROOT"
	linuxV2CleanupWitnessOutputEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR"
	linuxV2CleanupWitnessRoot              = "/opt/symmetry-owner-test/synthetic-goal-cleanup"
	linuxV2CleanupWitnessStateDir          = "/var/lib/symmetry-v2"
	linuxV2CleanupWitnessEvidenceFile      = "TestProductionLinuxV2CloneCleanupAuthenticatedScratch.json"
	linuxV2CleanupWitnessEvidenceLabel     = "INSTALLED SYNTHETIC-GOAL CLEANUP: REAL V2 PHYSICAL STOP + OPAQUE LEAF RELEASE + STORE/AUTHENTICATED ANCHOR/ROOT; TEST-LOCAL ARTIFACT DELIVERY ONLY"
)

type linuxV2CleanupWitnessDocument struct {
	Version       int    `json:"version"`
	Test          string `json:"test"`
	Status        string `json:"status"`
	EvidenceLabel string `json:"evidence_label"`
	Scope         struct {
		Admission                string `json:"admission"`
		Driver                   string `json:"driver"`
		ArtifactDelivery         string `json:"artifact_delivery"`
		AppEndToEnd              bool   `json:"app_end_to_end"`
		ControlIssuedWorkItem    bool   `json:"control_issued_work_item"`
		ControlAcceptedCandidate bool   `json:"control_accepted_candidate"`
		ProviderActionAttempted  bool   `json:"provider_action_attempted"`
		NativeCapabilityPromoted bool   `json:"native_capability_promoted"`
		TerminalVerdictLocalOnly bool   `json:"terminal_verdict_local_only"`
	} `json:"scope"`
	Fixture struct {
		CaseRoot              string `json:"case_root"`
		StateDir              string `json:"state_dir"`
		Source                string `json:"source"`
		SeedRoot              string `json:"seed_root"`
		CloneRoot             string `json:"clone_root"`
		BindingKey            string `json:"binding_key"`
		RuntimeKind           string `json:"runtime_kind"`
		RecoveryEndpointBytes int    `json:"recovery_endpoint_bytes"`
	} `json:"fixture"`
	Run struct {
		RunID               string `json:"run_id"`
		Generation          int64  `json:"generation"`
		ResourceID          string `json:"resource_id"`
		BaseCommit          string `json:"base_commit"`
		CandidateCommit     string `json:"candidate_commit"`
		CandidateTreeDigest string `json:"candidate_tree_digest"`
	} `json:"run"`
	PhysicalStop struct {
		ReceiptVersion      int    `json:"receipt_version"`
		ReceiptStatus       string `json:"receipt_status"`
		ProofKind           string `json:"proof_kind"`
		CgroupKillAck       bool   `json:"cgroup_kill_acknowledged"`
		CgroupPopulatedZero bool   `json:"cgroup_populated_zero"`
		NoMigrationObserved bool   `json:"no_migration_observed"`
		ProducerFenced      bool   `json:"producer_fenced"`
		DescendantsReaped   bool   `json:"descendants_reaped"`
	} `json:"physical_stop"`
	OpaqueRelease struct {
		IssuedByPlatform bool   `json:"issued_by_platform"`
		LeafDisposition  string `json:"leaf_disposition"`
		StoreClearCAS    bool   `json:"store_clear_cas"`
		ReleaseReadback  bool   `json:"release_readback"`
	} `json:"opaque_release"`
	Promotion struct {
		IntentCASReadback  bool   `json:"intent_cas_readback"`
		ReceiptCASReadback bool   `json:"receipt_cas_readback"`
		Ref                string `json:"ref"`
		RefRetained        bool   `json:"ref_retained"`
	} `json:"promotion"`
	BeforeBegin struct {
		StaleFenceDenied bool   `json:"stale_fence_denied"`
		AnchorDenied     bool   `json:"anchor_denied"`
		TargetDevice     uint64 `json:"target_device"`
		TargetInode      uint64 `json:"target_inode"`
		TargetUnchanged  bool   `json:"target_unchanged"`
		SiblingInode     uint64 `json:"sibling_inode"`
		SiblingUnchanged bool   `json:"sibling_unchanged"`
	} `json:"before_begin"`
	Begin struct {
		DurableReadback bool `json:"durable_readback"`
		ExactIntent     bool `json:"exact_intent"`
		Pending         bool `json:"pending"`
	} `json:"begin"`
	RootRemoval struct {
		ProofValid          bool   `json:"proof_valid"`
		OriginalTargetInode bool   `json:"original_target_inode"`
		TargetNlinkZero     bool   `json:"target_nlink_zero"`
		PathAbsent          bool   `json:"path_absent"`
		RootWorkerUID       uint32 `json:"root_worker_uid"`
		RootWorkerWaited    bool   `json:"root_worker_waited"`
		RootWorkerReaped    bool   `json:"root_worker_reaped"`
		SiblingIntact       bool   `json:"sibling_intact"`
	} `json:"root_removal"`
	OtherRunCheckout struct {
		RunID                      string `json:"run_id"`
		Target                     string `json:"target"`
		Device                     uint64 `json:"device"`
		Inode                      uint64 `json:"inode"`
		GitMetadataDirectoryBOwned bool   `json:"git_metadata_directory_b_owned"`
		IntactBeforeBegin          bool   `json:"intact_before_begin"`
		IntactAfterRootRemoval     bool   `json:"intact_after_root_removal"`
		IntactAfterACleanup        bool   `json:"intact_after_a_cleanup"`
	} `json:"other_run_checkout"`
	FailureGates struct {
		StaleTargetCASDenied                    bool `json:"stale_target_cas_denied"`
		UnpersistedRootProofCannotCleanup       bool `json:"unpersisted_root_proof_cannot_cleanup"`
		UnpersistedRootProofKeptPending         bool `json:"unpersisted_root_proof_kept_pending"`
		JournalRetirementBeforeCompletionDenied bool `json:"journal_retirement_before_completion_denied"`
	} `json:"failure_gates"`
	TargetRemovalCAS  bool `json:"target_removal_cas"`
	AWorkspaceCleanup struct {
		ProofValid          bool `json:"proof_valid"`
		SeedAbsent          bool `json:"seed_absent"`
		BaseRefAbsent       bool `json:"base_ref_absent"`
		ScratchAbsent       bool `json:"scratch_absent"`
		PromotionRefPresent bool `json:"promotion_ref_present"`
	} `json:"a_workspace_cleanup"`
	JournalRetired bool `json:"journal_retired"`
}

type linuxV2CleanupWitnessObservation struct {
	mu                 sync.Mutex
	workerCandidate    string
	stopReceipt        *authority.StopReceipt
	releaseProof       *authority.SupervisorHandoffReleaseProof
	promotionIntent    *authority.LinuxClonePromotionIntent
	promotionReceipt   *authority.LinuxClonePromotionReceipt
	opaqueReleaseSeen  bool
	storeClearReadback bool
}

// TestProductionLinuxV2CloneCleanupAuthenticatedScratch is a test-scoped direct
// Runner/Store/Workspace harness over the installed A/B helper/anchor. It exercises
// a real B checkout, a Runner-derived StopReceipt from helper/anchor stop
// evidence, opaque leaf release, local candidate CAS, authenticated root
// cleanup, and A-side retirement. It is not an App end-to-end positive.
// Its synthetic admission and local artifact/terminal CAS are not a Control
// WorkItem, Goal acceptance, provider action, or native capability promotion.
func TestProductionLinuxV2CloneCleanupAuthenticatedScratch(t *testing.T) {
	caseRoot := strings.TrimSpace(os.Getenv(linuxV2CleanupWitnessRootEnvironment))
	if caseRoot == "" {
		t.Skip("set " + linuxV2CleanupWitnessRootEnvironment + " to run the installed synthetic-goal cleanup witness")
	}
	if caseRoot != linuxV2CleanupWitnessRoot || !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot {
		t.Fatalf("%s must equal the dedicated canonical fixture root %q", linuxV2CleanupWitnessRootEnvironment, linuxV2CleanupWitnessRoot)
	}
	resolvedRoot, err := filepath.EvalSymlinks(caseRoot)
	if err != nil || resolvedRoot != caseRoot {
		t.Fatalf("synthetic cleanup fixture root is missing or traverses a symlink: %v", err)
	}
	outputDir := strings.TrimSpace(os.Getenv(linuxV2CleanupWitnessOutputEnvironment))
	if outputDir == "" || !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) != outputDir {
		t.Fatalf("%s must name an existing canonical output directory when the witness is enabled", linuxV2CleanupWitnessOutputEnvironment)
	}
	resolvedOutputDir, err := filepath.EvalSymlinks(outputDir)
	if err != nil || resolvedOutputDir != outputDir {
		t.Fatalf("redacted witness output directory is missing or traverses a symlink: %v", err)
	}
	outputInfo, err := os.Lstat(outputDir)
	if err != nil || !outputInfo.IsDir() || outputInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("redacted witness output path is not a real directory: %v", err)
	}

	configPath := filepath.Join(caseRoot, "config.json")
	value, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load root-controlled synthetic cleanup config: %v", err)
	}
	if value.StateDir != linuxV2CleanupWitnessStateDir {
		t.Fatalf("installed cleanup StateDir differs from the fixed OwnerConfig path %q", linuxV2CleanupWitnessStateDir)
	}
	bindingKey := value.Runtime.Workspace
	binding, ok := value.Workspaces[bindingKey]
	if !ok || binding.Policy != config.WorkspacePolicyGitCloneV2 ||
		filepath.Clean(binding.Repository) != filepath.Join(caseRoot, "source") ||
		filepath.Clean(binding.SeedRoot) != filepath.Join(caseRoot, "seeds") ||
		filepath.Clean(binding.Root) != filepath.Join(caseRoot, "clones") {
		t.Fatal("synthetic cleanup config does not bind the dedicated A source, A seed root, and B clone root")
	}
	if value.Runtime.RepositoryResourceID == "" || bindingKey == "" {
		t.Fatal("synthetic cleanup config lacks its exact repository resource or workspace binding")
	}
	if err := app.VerifyLinuxOwnerInstallation(value); err != nil {
		t.Fatalf("installed owner/anchor/StateDir/workspace preflight failed: %v", err)
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatalf("read installed A/B owner config: %v", err)
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID || owner.AUID == 0 {
		t.Fatal("cleanup witness must run as the exact installed unprivileged A identity")
	}
	bAccount, err := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
	if err != nil || bAccount.HomeDir == "" || !filepath.IsAbs(bAccount.HomeDir) {
		t.Fatalf("lookup fixed B home for the controlled native process: %v", err)
	}
	aAccount, err := user.LookupId(strconv.FormatUint(uint64(owner.AUID), 10))
	if err != nil || aAccount.HomeDir == "" || !filepath.IsAbs(aAccount.HomeDir) {
		t.Fatalf("lookup fixed A home for isolated Git subprocesses: %v", err)
	}
	if err := platform.EnsureLinuxChildSubreaper(); err != nil {
		t.Fatalf("enable bounded cleanup witness child reaping: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	parent, err := platform.OpenLinuxCgroupV2Parent(value.LinuxOwnerCgroupMount, value.LinuxOwnerDelegatedPath)
	if err != nil {
		t.Fatalf("open installed delegated cgroup-v2 parent: %v", err)
	}
	parentOpen := true
	t.Cleanup(func() {
		if parentOpen {
			if closeErr := parent.Close(); closeErr != nil {
				t.Errorf("close installed delegated cgroup-v2 parent: %v", closeErr)
			}
		}
	})
	if parent.OwnerUID() != owner.AUID {
		t.Fatal("delegated cgroup-v2 parent is not owned by installed A")
	}
	anchor, err := platform.NewLinuxOwnerAnchorClient(value.LinuxOwnerSocket)
	if err != nil {
		t.Fatalf("construct authenticated installed owner-anchor client: %v", err)
	}
	anchorIdentity, err := anchor.Discover(ctx)
	if err != nil || anchorIdentity.OwnerUID != owner.AUID || !anchorIdentity.Parent.Equal(parent.Identity()) ||
		anchorIdentity.ManagerPath != parent.Identity().Path {
		t.Fatalf("authenticated owner anchor differs from the pinned A delegated parent: %v", err)
	}

	gitPath, err := exec.LookPath("git")
	if err != nil || !filepath.IsAbs(gitPath) {
		t.Fatalf("find an absolute installed Git executable for the controlled candidate: %v", err)
	}
	gitEnv := linuxV2CleanupGitEnvironment(aAccount.HomeDir)
	baseCommit, err := linuxV2CleanupRunGit(ctx, gitPath, binding.Repository, gitEnv, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || !linuxV2CleanupValidGitObjectID(baseCommit) {
		t.Fatalf("read exact A source base commit: %v", err)
	}
	sourceManager := workspace.New(map[string]config.Workspace{
		"synthetic-source": {Policy: config.WorkspacePolicyGitWorktree, Repository: binding.Repository},
	})
	baseSubject, err := sourceManager.MaterializeSubject(ctx, "synthetic-source", value.Runtime.RepositoryResourceID, baseCommit)
	if err != nil || baseSubject.Validate() != nil {
		t.Fatalf("derive exact A source base Subject: %v", err)
	}
	runID, err := linuxV2CleanupUUID()
	if err != nil {
		t.Fatalf("create isolated synthetic RunID: %v", err)
	}
	suffix := strings.ReplaceAll(runID, "-", "")[:12]
	candidateFile := "synthetic-cleanup-" + suffix + ".txt"
	candidateContent := "test-only synthetic local candidate artifact for " + runID
	siblingName := "cleanup-sibling-" + suffix
	siblingContent := "sibling retained by authenticated target-only cleanup " + runID
	otherRunID, err := linuxV2CleanupUUID()
	if err != nil || otherRunID == runID {
		t.Fatalf("create a distinct sibling RunKey: %v", err)
	}
	otherRunContent := "independent B-owned checkout retained by exact-target cleanup " + otherRunID
	candidateSubject, err := linuxV2CleanupPrepareCandidateSubject(
		t, ctx, gitPath, gitEnv, binding.Repository, value.Runtime.RepositoryResourceID,
		baseCommit, candidateFile, candidateContent,
	)
	if err != nil || candidateSubject.Validate() != nil || candidateSubject.ResourceID != baseSubject.ResourceID {
		t.Fatalf("prepare independent expected candidate Subject: %v", err)
	}

	key := state.RunKey{RunID: runID, Generation: 1}
	claimID, err := linuxV2CleanupUUID()
	if err != nil {
		t.Fatalf("create synthetic claim ID: %v", err)
	}
	workItemID, err := linuxV2CleanupUUID()
	if err != nil {
		t.Fatalf("create test-only work item identity: %v", err)
	}
	admission, err := linuxV2CleanupSyntheticAdmission(baseSubject, workItemID)
	if err != nil {
		t.Fatalf("construct test-only synthetic implement admission: %v", err)
	}
	admissionInput, err := json.Marshal(admission)
	if err != nil {
		t.Fatalf("encode test-only synthetic implement admission: %v", err)
	}
	parsedAdmission, present, err := protocol.ParseAdmissionInput(admissionInput)
	if err != nil || !present || parsedAdmission.Purpose != protocol.AdmissionPurposeImplement || parsedAdmission.Subject != baseSubject {
		t.Fatalf("validate test-only synthetic implement admission before State claim CAS: %v", err)
	}
	work := protocol.Work{
		Goal:         "TEST ONLY: create one local candidate commit for installed Linux v2 cleanup evidence",
		AgentProfile: value.Runtime.AgentProfile, Workspace: bindingKey, Input: admissionInput,
	}
	runtimeID := "synthetic-cleanup-" + suffix
	leaseToken, err := linuxV2CleanupToken()
	if err != nil {
		t.Fatalf("create local-only State fence token: %v", err)
	}
	leaseExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	store, err := state.New(value.StateDir)
	if err != nil {
		t.Fatalf("open fixed production OwnerConfig StateDir after daemon.service stop: %v", err)
	}
	storeOpen := true
	t.Cleanup(func() {
		if storeOpen {
			if closeErr := store.Close(); closeErr != nil {
				t.Errorf("close synthetic cleanup Store: %v", closeErr)
			}
		}
	})
	journals, err := store.ListJournals()
	if err != nil || len(journals) != 0 {
		t.Fatalf("dedicated installed StateDir must start with no journals: count=%d err=%v", len(journals), err)
	}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: value.Runtime.RuntimeKey, RuntimeID: runtimeID, RuntimeEpoch: 1,
		ClaimID: claimID, LocalState: "claiming", Work: work, WorkspaceBindingKey: bindingKey,
	}); err != nil {
		t.Fatalf("persist test-only synthetic implement admission before clone reservation: %v", err)
	}
	claim := protocol.ClaimResponse{
		RunID: key.RunID, TaskID: workItemID, Generation: key.Generation,
		ClaimID: claimID, LeaseToken: leaseToken, LeaseExpiresAt: leaseExpiresAt,
		LeaseRemainingMS: int64((10 * time.Minute) / time.Millisecond), Work: work,
	}
	if _, err := store.SaveClaimGrant(key, claim); err != nil {
		t.Fatalf("persist local-only synthetic claim grant before clone reservation: %v", err)
	}
	fence := protocol.Fence{
		RuntimeID: runtimeID, RuntimeEpoch: 1, Generation: key.Generation,
		ClaimID: claimID, LeaseToken: leaseToken,
	}
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatalf("move local-only synthetic run to running before clone reservation: %v", err)
	}

	cloneWorkspace := workspace.New(value.Workspaces)
	intent, err := cloneWorkspace.PlanLinuxCloneIntent(bindingKey, workspace.RunRef{RunID: key.RunID, Generation: key.Generation}, baseSubject)
	if err != nil || intent.Validate() != nil || intent.RunID != key.RunID || intent.Generation != key.Generation {
		t.Fatalf("plan exact isolated Linux clone intent: %v", err)
	}
	otherIntent, err := cloneWorkspace.PlanLinuxCloneIntent(bindingKey, workspace.RunRef{RunID: otherRunID, Generation: key.Generation}, baseSubject)
	if err != nil || otherIntent.Validate() != nil || otherIntent.Target == intent.Target || otherIntent.RunID != otherRunID {
		t.Fatalf("plan distinct RunKey-shaped sibling checkout path: %v", err)
	}
	if _, err := store.ReserveLinuxCloneIntent(key, fence, intent, nil); err != nil {
		t.Fatalf("durably reserve synthetic RunKey clone before A Git/seed effects: %v", err)
	}
	seed, err := cloneWorkspace.PrepareLinuxCloneSeed(ctx, intent)
	if err != nil || seed.Validate() != nil || seed.BaseCommit != baseCommit {
		t.Fatalf("prepare exact A-owned immutable seed: %v", err)
	}
	if _, err := store.BindLinuxCloneSeed(key, fence, intent, seed); err != nil {
		t.Fatalf("bind exact A seed receipt into the same RunJournal: %v", err)
	}
	grant := authority.LinuxCloneGrant{Intent: intent.Clone(), Seed: seed.Clone()}
	if err := grant.Validate(); err != nil {
		t.Fatalf("validate exact RunKey/fence clone grant: %v", err)
	}
	prepared, err := cloneWorkspace.PrepareLinuxClone(ctx, grant)
	if err != nil || prepared.Path != intent.Target || prepared.Run != (workspace.RunRef{RunID: key.RunID, Generation: key.Generation}) {
		t.Fatalf("prepare A-only metadata for exact B checkout target: %v", err)
	}

	observation := &linuxV2CleanupWitnessObservation{}
	callbacks := linuxV2CleanupJournalCallbacks(store, key, fence, cloneWorkspace, grant, candidateSubject, observation)
	recoveryEndpoint, err := linuxV2CleanupRecoveryEndpoint(value.StateDir, key)
	if err != nil {
		t.Fatalf("derive bounded per-RunKey recovery endpoint: %v", err)
	}
	recoveryDigest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", key.RunID, key.Generation)))
	expectedRecoveryEndpoint := filepath.Join(value.StateDir, "owner-"+hex.EncodeToString(recoveryDigest[:])+".sock")
	if len(recoveryEndpoint) >= 100 || recoveryEndpoint != expectedRecoveryEndpoint {
		t.Fatal("test-scoped Runner recovery endpoint differs from App's full-SHA-256 derivation or exceeds its 100-byte bound")
	}
	childEnvironment := linuxV2CleanupChildEnvironment(
		bAccount, baseCommit, candidateSubject.Commit, candidateFile, candidateContent,
		filepath.Join(filepath.Dir(intent.Target), siblingName), siblingContent,
		otherIntent.Target, otherRunContent,
	)
	command := `set -eu
umask 022
mkdir "$SYMMETRY_CLEANUP_SIBLING_PATH"
printf '%s\n' "$SYMMETRY_CLEANUP_SIBLING_CONTENT" > "$SYMMETRY_CLEANUP_SIBLING_PATH/sentinel.txt"
printf '%s\n' "$SYMMETRY_CLEANUP_CANDIDATE_CONTENT" > "$SYMMETRY_CLEANUP_CANDIDATE_FILE"
git add -- "$SYMMETRY_CLEANUP_CANDIDATE_FILE"
tree=$(git write-tree)
commit=$(printf '%s\n' 'test-only synthetic cleanup candidate' | git -c commit.gpgsign=false commit-tree "$tree" -p "$SYMMETRY_CLEANUP_BASE_COMMIT")
git update-ref --no-deref HEAD "$commit"
test "$commit" = "$SYMMETRY_CLEANUP_CANDIDATE_COMMIT"
git update-ref refs/heads/synthetic-cleanup-candidate "$commit"
mkdir -p "$(dirname "$SYMMETRY_CLEANUP_OTHER_RUN_TARGET")"
git clone --quiet --no-hardlinks --branch synthetic-cleanup-candidate "$PWD" "$SYMMETRY_CLEANUP_OTHER_RUN_TARGET"
printf '%s\n' "$SYMMETRY_CLEANUP_OTHER_RUN_CONTENT" > "$SYMMETRY_CLEANUP_OTHER_RUN_TARGET/sentinel.txt"
/usr/bin/sleep 300 >/dev/null 2>&1 &
sleep_pid=$!
kill -0 "$sleep_pid"
printf 'CANDIDATE:%s\n' "$commit"
printf 'READY\n'
IFS= read -r response
[ "$response" = finish ]
exit 0
`
	candidateReady := make(chan string, 1)
	var outputMu sync.Mutex
	var outputRemainder []byte
	sink := execution.SinkFunc(func(_ context.Context, event execution.Event) error {
		if event.Stream != execution.Stdout {
			return nil
		}
		outputMu.Lock()
		defer outputMu.Unlock()
		outputRemainder = append(outputRemainder, event.Data...)
		for {
			newline := bytes.IndexByte(outputRemainder, '\n')
			if newline < 0 {
				return nil
			}
			line := strings.TrimSpace(string(outputRemainder[:newline]))
			outputRemainder = outputRemainder[newline+1:]
			if strings.HasPrefix(line, "CANDIDATE:") {
				commit := strings.TrimSpace(strings.TrimPrefix(line, "CANDIDATE:"))
				observation.mu.Lock()
				if observation.workerCandidate == "" {
					observation.workerCandidate = commit
				}
				observation.mu.Unlock()
				select {
				case candidateReady <- commit:
				default:
				}
			}
		}
	})
	process, startErr := execution.NewRunner().Start(ctx, execution.Invocation{
		Program: "/bin/sh", Args: []string{"-c", command, "linux-v2-cleanup-worker"},
		Dir: prepared.Path, Env: childEnvironment,
		InitialLeaseDeadlineAt: leaseExpiresAt, InitialLeaseSequence: 1,
		LinuxV2Required: true,
		LinuxV2: &execution.LinuxV2Invocation{
			RunKey: key, Fence: fence, Parent: parent, Anchor: anchor,
			RecoveryEndpoint: recoveryEndpoint, CloneGrant: &grant, Callbacks: callbacks,
		},
	}, sink)
	processSettled := false
	if process != nil {
		t.Cleanup(func() {
			if processSettled {
				return
			}
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer stopCancel()
			if terminateErr := process.Terminate(stopCtx, time.Second); terminateErr != nil {
				t.Errorf("terminate unfinished controlled B process during test cleanup: %v", terminateErr)
			}
			select {
			case <-process.ResultDone():
				_ = process.Wait()
			case <-stopCtx.Done():
				t.Errorf("controlled B process did not settle during bounded test cleanup: %v", stopCtx.Err())
			}
		})
	}
	if startErr != nil {
		t.Fatalf("start controlled B Git worker through the real Linux v2 launcher/anchor/helper: %v", startErr)
	}
	if process == nil {
		t.Fatal("Linux v2 Runner returned no owner for the controlled B Git worker")
	}
	var workerCommit string
	select {
	case workerCommit = <-candidateReady:
	case <-ctx.Done():
		t.Fatalf("controlled B Git worker did not publish its candidate before the bound: %v", ctx.Err())
	case <-time.After(2 * time.Minute):
		t.Fatal("controlled B Git worker did not publish its candidate within two minutes")
	}
	if workerCommit != candidateSubject.Commit {
		t.Fatalf("controlled B Git candidate differs from the independently derived test candidate: got=%q", workerCommit)
	}
	peer, err := process.DuplicateLinuxV2Peer()
	if err != nil {
		t.Fatalf("pin exact live B worker through the Runner's pidfd peer: %v", err)
	}
	peerOpen := true
	t.Cleanup(func() {
		if peerOpen {
			if closeErr := peer.Close(); closeErr != nil {
				t.Errorf("close exact B worker pidfd peer: %v", closeErr)
			}
		}
	})
	if peer.PID != process.PID || peer.Identity != process.Identity || peer.PIDFD == nil ||
		peer.UserNamespace.Device == 0 || peer.UserNamespace.Inode == 0 {
		t.Fatal("live controlled B process lacks its exact pidfd and verified namespace identity")
	}
	if err := linuxV2CleanupRequireLivePIDFD(int(peer.PIDFD.Fd())); err != nil {
		t.Fatalf("controlled B worker was not live at its exact checkout boundary: %v", err)
	}
	journalAtCandidate, err := store.LoadJournal(key)
	if err != nil || journalAtCandidate.Fence() != fence || journalAtCandidate.LinuxCloneIntent == nil ||
		!journalAtCandidate.LinuxCloneIntent.Equal(intent) || journalAtCandidate.LinuxCloneCheckoutReceipt == nil ||
		journalAtCandidate.ContainmentAuthority == nil || journalAtCandidate.ContainmentAuthority.StopReceipt != nil ||
		journalAtCandidate.ContainmentCgroupReleaseProof != nil || !journalAtCandidate.HasProcessDetails() {
		t.Fatalf("live B checkout lacks its exact pre-stop State boundary: %v", err)
	}
	checkout := journalAtCandidate.LinuxCloneCheckoutReceipt.Clone()
	rootFD, targetFD, targetRel, err := linuxV2CleanupOpenClonePins(binding.Root, intent.Target)
	if err != nil {
		t.Fatalf("pin exact B clone root and original checkout O_PATH inode: %v", err)
	}
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	var rootInfo, originalTargetInfo unix.Stat_t
	if err := unix.Fstat(rootFD, &rootInfo); err != nil {
		t.Fatalf("stat pinned B clone root: %v", err)
	}
	if err := unix.Fstat(targetFD, &originalTargetInfo); err != nil {
		t.Fatalf("stat original pinned B checkout: %v", err)
	}
	if originalTargetInfo.Mode&unix.S_IFMT != unix.S_IFDIR || originalTargetInfo.Nlink == 0 ||
		uint64(originalTargetInfo.Dev) != checkout.TargetDevice || uint64(originalTargetInfo.Ino) != checkout.TargetInode {
		t.Fatal("pinned B checkout is not the original inode in its exact Store CheckoutReady receipt")
	}
	rootFlags, err := unix.FcntlInt(uintptr(rootFD), unix.F_GETFL, 0)
	if err != nil || rootFlags&unix.O_PATH == 0 {
		t.Fatal("B clone root request descriptor is not an O_PATH pin")
	}
	targetFlags, err := unix.FcntlInt(uintptr(targetFD), unix.F_GETFL, 0)
	if err != nil || targetFlags&unix.O_PATH == 0 {
		t.Fatal("B checkout request descriptor is not an O_PATH pin")
	}
	siblingPath := filepath.Join(filepath.Dir(intent.Target), siblingName)
	siblingRel, err := filepath.Rel(binding.Root, siblingPath)
	if err != nil || !linuxV2CleanupSafeRelativePath(siblingRel) {
		t.Fatalf("derive safe exact sibling path: %v", err)
	}
	siblingFD, err := linuxV2CleanupOpenAt(rootFD, siblingRel, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin B-owned sibling directory created by the controlled worker: %v", err)
	}
	defer unix.Close(siblingFD)
	sentinelFD, err := linuxV2CleanupOpenAt(rootFD, filepath.Join(siblingRel, "sentinel.txt"), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin controlled worker's sibling sentinel: %v", err)
	}
	defer unix.Close(sentinelFD)
	var originalSiblingInfo unix.Stat_t
	if err := unix.Fstat(siblingFD, &originalSiblingInfo); err != nil || originalSiblingInfo.Mode&unix.S_IFMT != unix.S_IFDIR {
		t.Fatalf("stat exact B sibling directory: %v", err)
	}
	var originalSiblingSentinelInfo unix.Stat_t
	if err := unix.Fstat(sentinelFD, &originalSiblingSentinelInfo); err != nil ||
		originalSiblingSentinelInfo.Mode&unix.S_IFMT != unix.S_IFREG || originalSiblingSentinelInfo.Nlink == 0 {
		t.Fatalf("stat exact B sibling sentinel inode: %v", err)
	}
	gotSiblingContent, err := linuxV2CleanupReadFD(sentinelFD)
	if err != nil || gotSiblingContent != siblingContent+"\n" {
		t.Fatalf("read exact B sibling sentinel: %v", err)
	}
	expectedOtherRunTarget := filepath.Join(binding.Root, "binding-"+bindingKey, "run-"+otherRunID, "generation-1")
	if otherIntent.Target != expectedOtherRunTarget || otherIntent.RunID != otherRunID || otherIntent.Generation != key.Generation {
		t.Fatal("second B checkout is not a distinct canonical RunKey-shaped target under the same clone root")
	}
	otherRunRel, err := filepath.Rel(binding.Root, otherIntent.Target)
	if err != nil || !linuxV2CleanupSafeRelativePath(otherRunRel) {
		t.Fatalf("derive safe second RunKey checkout path: %v", err)
	}
	otherRunFD, err := linuxV2CleanupOpenAt(rootFD, otherRunRel, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin second B-owned RunKey-shaped checkout: %v", err)
	}
	defer unix.Close(otherRunFD)
	otherRunSentinelFD, err := linuxV2CleanupOpenAt(rootFD, filepath.Join(otherRunRel, "sentinel.txt"), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin second RunKey checkout sentinel: %v", err)
	}
	defer unix.Close(otherRunSentinelFD)
	var otherRunSentinelInfo unix.Stat_t
	if err := unix.Fstat(otherRunSentinelFD, &otherRunSentinelInfo); err != nil ||
		otherRunSentinelInfo.Mode&unix.S_IFMT != unix.S_IFREG || otherRunSentinelInfo.Nlink == 0 {
		t.Fatalf("stat second RunKey checkout sentinel inode: %v", err)
	}
	var otherRunInfo unix.Stat_t
	if err := unix.Fstat(otherRunFD, &otherRunInfo); err != nil || otherRunInfo.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint32(otherRunInfo.Uid) != owner.BUID || otherRunInfo.Dev != originalTargetInfo.Dev ||
		otherRunInfo.Ino == originalTargetInfo.Ino || otherRunInfo.Nlink == 0 {
		t.Fatalf("second RunKey checkout is not a B-owned directory: %v", err)
	}
	otherRunSentinel, err := linuxV2CleanupReadFD(otherRunSentinelFD)
	if err != nil || otherRunSentinel != otherRunContent+"\n" {
		t.Fatalf("read second RunKey checkout sentinel: %v", err)
	}
	otherGitRel := filepath.Join(otherRunRel, ".git")
	otherGitFD, err := linuxV2CleanupOpenAt(rootFD, otherGitRel, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin independent RunKey checkout Git metadata directory: %v", err)
	}
	defer unix.Close(otherGitFD)
	var otherGitInfo unix.Stat_t
	if err := unix.Fstat(otherGitFD, &otherGitInfo); err != nil || otherGitInfo.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint32(otherGitInfo.Uid) != owner.BUID {
		t.Fatalf("independent checkout Git metadata is not B-owned: %v", err)
	}
	candidateFD, err := linuxV2CleanupOpenAt(targetFD, candidateFile, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatalf("pin candidate artifact file produced by the B worker: %v", err)
	}
	candidateBytes, readErr := linuxV2CleanupReadFD(candidateFD)
	_ = unix.Close(candidateFD)
	if readErr != nil || candidateBytes != candidateContent+"\n" {
		t.Fatalf("read exact test-local candidate artifact from B checkout: %v", readErr)
	}

	if err := process.WriteInput([]byte("finish\n")); err != nil {
		t.Fatalf("release controlled B Git worker for successful physical-stop finalization: %v", err)
	}
	select {
	case <-process.ResultDone():
	case <-ctx.Done():
		t.Fatalf("real helper/anchor stop and A promotion did not settle before the bound: %v", ctx.Err())
	case <-time.After(2 * time.Minute):
		t.Fatal("real helper/anchor stop and A promotion did not settle within two minutes")
	}
	processResult := process.Wait()
	processSettled = true
	if !processResult.Success() {
		t.Fatalf("controlled B candidate run did not complete with real stop and promotion evidence: exit=%d terminated=%t wait=%v sink=%v output=%v containment=%v", processResult.ExitCode, processResult.Terminated, processResult.WaitError, processResult.SinkError, processResult.OutputError, processResult.ContainmentError)
	}
	if err := linuxV2CleanupRequireExitedPIDFD(int(peer.PIDFD.Fd())); err != nil {
		t.Fatalf("exact B gate pidfd did not report exit after the worker completion: %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.Key() != key || journal.Fence() != fence || journal.LinuxCloneIntent == nil ||
		!journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneSeedReceipt == nil ||
		!journal.LinuxCloneSeedReceipt.Equal(seed) || journal.LinuxCloneCheckoutReceipt == nil ||
		!journal.LinuxCloneCheckoutReceipt.Equal(checkout) || journal.LinuxClonePromotionIntent == nil ||
		journal.LinuxClonePromotionIntent.Commit != candidateSubject.Commit ||
		journal.LinuxClonePromotionIntent.TreeDigest != candidateSubject.TreeDigest || journal.LinuxClonePromotionReceipt == nil ||
		journal.ContainmentCgroupReleaseProof == nil || journal.ContainmentCgroupReleaseProof.Validate() != nil ||
		journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.HasProcessDetails() || journal.RetainWorkspace {
		t.Fatalf("final RunJournal lacks exact promotion/release readback or still owns a process: %v", err)
	}
	observation.mu.Lock()
	stopReceipt := observation.stopReceipt
	releaseProofObserved := observation.releaseProof
	promotionIntentObserved := observation.promotionIntent
	promotionReceiptObserved := observation.promotionReceipt
	opaqueReleaseSeen := observation.opaqueReleaseSeen
	storeClearReadback := observation.storeClearReadback
	workerCandidateObserved := observation.workerCandidate
	observation.mu.Unlock()
	if stopReceipt == nil || stopReceipt.Version != authority.SupervisorVersionV2 || stopReceipt.Status != "stopped" ||
		stopReceipt.LinuxProof == nil ||
		!stopReceipt.LinuxProof.CgroupKillAcknowledged || !stopReceipt.LinuxProof.CgroupPopulatedZero ||
		!stopReceipt.LinuxProof.NoMigrationObserved || !stopReceipt.LinuxProof.ProducerFenced || !stopReceipt.LinuxProof.DescendantsReaped ||
		journal.ContainmentCgroupReleaseProof.StopReceipt == nil || !journal.ContainmentCgroupReleaseProof.StopReceipt.Equal(*stopReceipt) {
		t.Fatal("State did not retain the exact real v2 helper/anchor physical StopReceipt")
	}
	if !opaqueReleaseSeen || !storeClearReadback || releaseProofObserved == nil ||
		!releaseProofObserved.Equal(*journal.ContainmentCgroupReleaseProof) ||
		releaseProofObserved.LeafDisposition != authority.LinuxCgroupLeafRemoved {
		t.Fatal("the exact opaque platform leaf-removal release was not used for the Store clear/readback")
	}
	if workerCandidateObserved != candidateSubject.Commit || promotionIntentObserved == nil ||
		promotionIntentObserved.Commit != candidateSubject.Commit || promotionIntentObserved.TreeDigest != candidateSubject.TreeDigest ||
		promotionReceiptObserved == nil || !promotionReceiptObserved.Equal(*journal.LinuxClonePromotionReceipt) {
		t.Fatal("A promotion did not consume the exact candidate observed from the controlled B Git worker")
	}
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil || journal.LinuxClonePromotionReceipt.Ref != promotionRef {
		t.Fatalf("derive exact RunKey promotion ref: %v", err)
	}

	evidenceID, err := linuxV2CleanupUUID()
	if err != nil {
		t.Fatalf("create test-local artifact evidence ID: %v", err)
	}
	evidence, err := linuxV2CleanupArtifactEvidence(key.RunID, evidenceID, candidateSubject, candidateFile, []byte(candidateBytes))
	if err != nil {
		t.Fatalf("construct test-marked candidate artifact evidence: %v", err)
	}
	queued, err := store.QueueGoalEvidence(key, evidence)
	if err != nil || len(queued.PendingGoalDeliveries) != 1 {
		t.Fatalf("queue test-local candidate artifact CAS: pending=%d err=%v", len(queued.PendingGoalDeliveries), err)
	}
	delivery := queued.PendingGoalDeliveries[0]
	if delivery.Kind != state.GoalDeliveryEvidence || delivery.DeliveryID != evidence.EvidenceKey || delivery.Evidence == nil ||
		delivery.Evidence.Subject != candidateSubject {
		t.Fatal("queued local artifact delivery differs from the exact promoted candidate Subject")
	}
	delivered, err := store.MarkGoalDeliveryDelivered(key, delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest)
	if err != nil || len(delivered.PendingGoalDeliveries) != 0 || len(delivered.DeliveredGoalDeliveries) != 1 {
		t.Fatalf("persist test-local artifact delivery CAS/readback: err=%v", err)
	}
	terminalAt := time.Now().UTC()
	transition := protocol.StateTransitionRequest{
		TransitionID: "synthetic-cleanup-completed-" + suffix,
		State:        "completed", Payload: json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}
	if _, err := store.QueueTerminalTransitionAt(key, transition, terminalAt); err != nil {
		t.Fatalf("queue test-local candidate-completion terminal CAS: %v", err)
	}
	if _, err := store.MarkTransitionsDelivered(key, []string{transition.TransitionID}); err != nil {
		t.Fatalf("persist test-local terminal-transition delivery CAS: %v", err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, terminalAt.Add(time.Second)); err != nil {
		t.Fatalf("resolve local-only terminal into cleanup eligibility without Control acceptance: %v", err)
	}
	ready, err := store.LoadJournal(key)
	if err != nil || !ready.LinuxCloneCleanupBeginEligible(key, fence) || ready.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion ||
		ready.TerminalVerdict != state.TerminalVerdictOwnershipLost || !ready.GoalDeliveryEnabled || ready.RetainWorkspace {
		t.Fatalf("test-local delivered artifact/terminal did not satisfy the exact cleanup gate: %v", err)
	}
	cleanupIntent, err := state.LinuxCloneCleanupIntentForJournal(ready)
	if err != nil || cleanupIntent.Validate() != nil || !cleanupIntent.Intent.Equal(intent) ||
		!cleanupIntent.CheckoutReceipt.Equal(checkout) || !cleanupIntent.PromotionReceipt.Equal(*journal.LinuxClonePromotionReceipt) {
		t.Fatalf("derive exact State-bound cleanup intent before Begin: %v", err)
	}
	if cleanupIntent.RootPath != binding.Root || cleanupIntent.Intent.Target != intent.Target {
		t.Fatal("derived cleanup intent differs from the exact installed B clone root/target")
	}

	document := linuxV2CleanupWitnessDocument{
		Version: 1, Test: "TestProductionLinuxV2CloneCleanupAuthenticatedScratch",
		Status: "passed", EvidenceLabel: linuxV2CleanupWitnessEvidenceLabel,
	}
	document.Scope.Admission = "test-only synthetic implement admission persisted before clone reservation"
	document.Scope.Driver = "test-scoped direct execution.Runner/State Store/Workspace harness using installed Linux v2 helper and authenticated anchor; not App end-to-end"
	document.Scope.ArtifactDelivery = "test-local Store QueueGoalEvidence/MarkGoalDeliveryDelivered CAS only"
	document.Scope.AppEndToEnd = false
	document.Scope.ControlIssuedWorkItem = false
	document.Scope.ControlAcceptedCandidate = false
	document.Scope.ProviderActionAttempted = false
	document.Scope.NativeCapabilityPromoted = false
	document.Scope.TerminalVerdictLocalOnly = true
	document.Fixture.CaseRoot = caseRoot
	document.Fixture.StateDir = value.StateDir
	document.Fixture.Source = binding.Repository
	document.Fixture.SeedRoot = binding.SeedRoot
	document.Fixture.CloneRoot = binding.Root
	document.Fixture.BindingKey = bindingKey
	document.Fixture.RuntimeKind = value.Runtime.HarnessKind
	document.Fixture.RecoveryEndpointBytes = len(recoveryEndpoint)
	document.OtherRunCheckout.RunID = otherRunID
	document.OtherRunCheckout.Target = otherIntent.Target
	document.OtherRunCheckout.Device = uint64(otherRunInfo.Dev)
	document.OtherRunCheckout.Inode = otherRunInfo.Ino
	document.Run.RunID = key.RunID
	document.Run.Generation = key.Generation
	document.Run.ResourceID = candidateSubject.ResourceID
	document.Run.BaseCommit = baseCommit
	document.Run.CandidateCommit = candidateSubject.Commit
	document.Run.CandidateTreeDigest = candidateSubject.TreeDigest
	document.PhysicalStop.ReceiptVersion = stopReceipt.Version
	document.PhysicalStop.ReceiptStatus = stopReceipt.Status
	document.PhysicalStop.ProofKind = stopReceipt.LinuxProof.Kind
	document.PhysicalStop.CgroupKillAck = stopReceipt.LinuxProof.CgroupKillAcknowledged
	document.PhysicalStop.CgroupPopulatedZero = stopReceipt.LinuxProof.CgroupPopulatedZero
	document.PhysicalStop.NoMigrationObserved = stopReceipt.LinuxProof.NoMigrationObserved
	document.PhysicalStop.ProducerFenced = stopReceipt.LinuxProof.ProducerFenced
	document.PhysicalStop.DescendantsReaped = stopReceipt.LinuxProof.DescendantsReaped
	document.OpaqueRelease.IssuedByPlatform = opaqueReleaseSeen
	document.OpaqueRelease.LeafDisposition = releaseProofObserved.LeafDisposition
	document.OpaqueRelease.StoreClearCAS = true
	document.OpaqueRelease.ReleaseReadback = storeClearReadback
	document.Promotion.IntentCASReadback = promotionIntentObserved != nil
	document.Promotion.ReceiptCASReadback = promotionReceiptObserved != nil
	document.Promotion.Ref = promotionRef

	staleFence := fence
	staleFence.LeaseToken = "stale-cleanup-fence"
	if !t.Run("stale-fence-cannot-begin", func(t *testing.T) {
		if _, err := store.BeginLinuxCloneCleanup(key, staleFence); err == nil {
			t.Fatal("stale cleanup fence unexpectedly persisted Begin")
		}
		current, err := store.LoadJournal(key)
		if err != nil || current.LinuxCloneCleanupPending || current.LinuxCloneCleanupIntent != nil {
			t.Fatalf("stale fence changed the pre-Begin journal: %v", err)
		}
	}) {
		return
	}
	if !t.Run("anchor-denies-before-durable-begin", func(t *testing.T) {
		if err := grant.Validate(); err != nil {
			t.Fatalf("pre-Begin request clone grant is not structurally valid: %v", err)
		}
		if err := cleanupIntent.Validate(); err != nil {
			t.Fatalf("pre-Begin request cleanup intent is not structurally valid: %v", err)
		}
		if proof, proofErr := anchor.RemoveBCloneAfterReceipt(ctx, rootFD, targetFD, grant, cleanupIntent, nil); proofErr == nil {
			t.Fatalf("Anchor accepted a structurally valid cleanup request before durable Begin: disposition=%q", proof.Disposition)
		}
		var afterTarget unix.Stat_t
		if err := unix.Fstat(targetFD, &afterTarget); err != nil || afterTarget.Dev != originalTargetInfo.Dev ||
			afterTarget.Ino != originalTargetInfo.Ino || afterTarget.Nlink != originalTargetInfo.Nlink || afterTarget.Nlink == 0 {
			t.Fatalf("pre-Begin Anchor denial changed the original pinned checkout inode: %v", err)
		}
		if err := linuxV2CleanupVerifyNamedTarget(rootFD, targetRel, originalTargetInfo); err != nil {
			t.Fatalf("pre-Begin denial changed the named checkout target: %v", err)
		}
		if err := linuxV2CleanupVerifySibling(rootFD, siblingRel, siblingFD, sentinelFD, originalSiblingInfo, originalSiblingSentinelInfo, siblingContent+"\n"); err != nil {
			t.Fatalf("pre-Begin denial changed the B sibling: %v", err)
		}
		if err := linuxV2CleanupVerifySibling(rootFD, otherRunRel, otherRunFD, otherRunSentinelFD, otherRunInfo, otherRunSentinelInfo, otherRunContent+"\n"); err != nil {
			t.Fatalf("pre-Begin denial changed the independent B RunKey checkout: %v", err)
		}
		if err := linuxV2CleanupVerifyDirectory(rootFD, otherGitRel, otherGitFD, otherGitInfo, owner.BUID); err != nil {
			t.Fatalf("pre-Begin denial changed the independent checkout's B-owned Git metadata: %v", err)
		}
		document.BeforeBegin.AnchorDenied = true
		document.BeforeBegin.TargetDevice = uint64(originalTargetInfo.Dev)
		document.BeforeBegin.TargetInode = originalTargetInfo.Ino
		document.BeforeBegin.TargetUnchanged = true
		document.BeforeBegin.SiblingInode = originalSiblingInfo.Ino
		document.BeforeBegin.SiblingUnchanged = true
		document.OtherRunCheckout.IntactBeforeBegin = true
	}) {
		return
	}
	document.BeforeBegin.StaleFenceDenied = true

	begun, err := store.BeginLinuxCloneCleanup(key, fence)
	if err != nil || !begun.LinuxCloneCleanupPending || begun.LinuxCloneCleanupIntent == nil ||
		!begun.LinuxCloneCleanupIntent.Equal(cleanupIntent) {
		t.Fatalf("persist exact normal Linux clone cleanup Begin CAS: %v", err)
	}
	beginReadback, err := store.LoadJournal(key)
	if err != nil || !beginReadback.LinuxCloneCleanupPending || beginReadback.LinuxCloneCleanupIntent == nil ||
		!beginReadback.LinuxCloneCleanupIntent.Equal(cleanupIntent) || beginReadback.LinuxCloneTargetRemovalProof != nil {
		t.Fatalf("read exact durable pending Begin before root effect: %v", err)
	}
	document.Begin.DurableReadback = err == nil
	document.Begin.ExactIntent = err == nil && beginReadback.LinuxCloneCleanupIntent != nil && beginReadback.LinuxCloneCleanupIntent.Equal(cleanupIntent)
	document.Begin.Pending = err == nil && beginReadback.LinuxCloneCleanupPending

	rootProof, err := anchor.RemoveBCloneAfterReceipt(ctx, rootFD, targetFD, grant, cleanupIntent, nil)
	if err != nil || rootProof.Validate() != nil || !rootProof.Intent.Equal(cleanupIntent) ||
		rootProof.Disposition != authority.LinuxCloneTargetDispositionRemoved {
		t.Fatalf("run exact root cleanup through the authenticated installed Anchor: %v", err)
	}
	var removedTarget unix.Stat_t
	if err := unix.Fstat(targetFD, &removedTarget); err != nil || removedTarget.Dev != originalTargetInfo.Dev ||
		removedTarget.Ino != originalTargetInfo.Ino || removedTarget.Nlink != 0 {
		t.Fatalf("root proof does not bind the original O_PATH checkout inode at nlink=0: %v", err)
	}
	if rootProof.TargetDevice != checkout.TargetDevice || rootProof.TargetInode != checkout.TargetInode ||
		rootProof.RootDevice != uint64(rootInfo.Dev) || rootProof.RootInode != rootInfo.Ino ||
		rootProof.RootDeleterUID != 0 || !rootProof.RootDeleterExit.Waited || !rootProof.RootDeleterExit.Reaped ||
		!rootProof.RootDeleterExit.Process.Equal(rootProof.RootDeleter) {
		t.Fatal("authenticated root proof differs from the exact checkout/root inode or lacks the reaped root worker")
	}
	if err := linuxV2CleanupRequireTargetAbsent(rootFD, targetRel); err != nil {
		t.Fatalf("verify B target path absent after exact root effect: %v", err)
	}
	if err := linuxV2CleanupVerifySibling(rootFD, siblingRel, siblingFD, sentinelFD, originalSiblingInfo, originalSiblingSentinelInfo, siblingContent+"\n"); err != nil {
		t.Fatalf("root target removal changed the adjacent B sibling: %v", err)
	}
	if err := linuxV2CleanupVerifySibling(rootFD, otherRunRel, otherRunFD, otherRunSentinelFD, otherRunInfo, otherRunSentinelInfo, otherRunContent+"\n"); err != nil {
		t.Fatalf("root target removal changed the independent B RunKey checkout: %v", err)
	}
	if err := linuxV2CleanupVerifyDirectory(rootFD, otherGitRel, otherGitFD, otherGitInfo, owner.BUID); err != nil {
		t.Fatalf("root target removal changed the independent checkout's B-owned Git metadata: %v", err)
	}
	document.OtherRunCheckout.IntactAfterRootRemoval = true
	pendingAfterRoot, err := store.LoadJournal(key)
	if err != nil || !pendingAfterRoot.LinuxCloneCleanupPending || pendingAfterRoot.LinuxCloneTargetRemovalProof != nil ||
		pendingAfterRoot.LinuxCloneCleanupProof != nil {
		t.Fatalf("root effect without a durable target proof changed cleanup state: %v", err)
	}
	document.RootRemoval.ProofValid = rootProof.Validate() == nil
	document.RootRemoval.OriginalTargetInode = rootProof.TargetDevice == checkout.TargetDevice && rootProof.TargetInode == checkout.TargetInode
	document.RootRemoval.TargetNlinkZero = removedTarget.Nlink == 0 && rootProof.TargetNlinkZero
	document.RootRemoval.PathAbsent = true
	document.RootRemoval.RootWorkerUID = rootProof.RootDeleterUID
	document.RootRemoval.RootWorkerWaited = rootProof.RootDeleterExit.Waited
	document.RootRemoval.RootWorkerReaped = rootProof.RootDeleterExit.Reaped
	document.RootRemoval.SiblingIntact = true

	if !t.Run("unpersisted-root-proof-cannot-advance-cleanup", func(t *testing.T) {
		if _, err := cloneWorkspace.CleanupLinuxClone(ctx, grant, checkout, pendingAfterRoot); err == nil {
			t.Fatal("A Workspace accepted root removal without the exact Store-persisted target-removal proof")
		}
		called := false
		_, err := cloneWorkspace.RemoveLinuxCloneTarget(ctx, grant, checkout, pendingAfterRoot,
			func(context.Context, int, int, authority.LinuxCloneGrant, authority.LinuxCloneCleanupIntent, *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
				called = true
				return authority.LinuxCloneTargetRemovalProof{}, errors.New("unexpected second root effect")
			})
		if err == nil || called {
			t.Fatal("absent target without the exact Store proof was treated as removed or retried through a root worker")
		}
		current, err := store.LoadJournal(key)
		if err != nil || !current.LinuxCloneCleanupPending || current.LinuxCloneTargetRemovalProof != nil ||
			current.LinuxCloneCleanupProof != nil {
			t.Fatalf("unpersisted root proof advanced or retired the pending journal: %v", err)
		}
		if _, err := os.Lstat(intent.SeedPath); err != nil {
			t.Fatalf("A seed was retired before target-removal proof persistence: %v", err)
		}
		document.FailureGates.UnpersistedRootProofCannotCleanup = true
		document.FailureGates.UnpersistedRootProofKeptPending = true
	}) {
		return
	}
	if !t.Run("stale-fence-cannot-record-target-removal", func(t *testing.T) {
		if _, err := store.RecordLinuxCloneTargetRemoval(key, staleFence, cleanupIntent, rootProof); err == nil {
			t.Fatal("stale cleanup fence unexpectedly persisted a root target-removal proof")
		}
		current, err := store.LoadJournal(key)
		if err != nil || !current.LinuxCloneCleanupPending || current.LinuxCloneTargetRemovalProof != nil {
			t.Fatalf("stale target-removal CAS changed pending State: %v", err)
		}
		document.FailureGates.StaleTargetCASDenied = true
	}) {
		return
	}
	recorded, err := store.RecordLinuxCloneTargetRemoval(key, fence, cleanupIntent, rootProof)
	if err != nil || !recorded.LinuxCloneCleanupPending || recorded.LinuxCloneTargetRemovalProof == nil ||
		!recorded.LinuxCloneTargetRemovalProof.Equal(rootProof) {
		t.Fatalf("persist exact authenticated root target-removal proof CAS: %v", err)
	}
	document.TargetRemovalCAS = true

	if !t.Run("journal-cannot-retire-before-a-cleanup-proof", func(t *testing.T) {
		if err := store.DeleteJournal(key); err == nil {
			t.Fatal("Linux clone journal retired before its exact final A cleanup proof")
		}
		current, err := store.LoadJournal(key)
		if err != nil || !current.LinuxCloneCleanupPending || current.LinuxCloneCleanupProof != nil ||
			current.LinuxCloneTargetRemovalProof == nil || !current.LinuxCloneTargetRemovalProof.Equal(rootProof) {
			t.Fatalf("premature journal deletion changed the exact pending proof: %v", err)
		}
		document.FailureGates.JournalRetirementBeforeCompletionDenied = true
	}) {
		return
	}
	pendingWithRootProof, err := store.LoadJournal(key)
	if err != nil || pendingWithRootProof.LinuxCloneTargetRemovalProof == nil ||
		!pendingWithRootProof.LinuxCloneTargetRemovalProof.Equal(rootProof) {
		t.Fatalf("read exact persisted root proof before A cleanup: %v", err)
	}
	cleanupProof, err := cloneWorkspace.CleanupLinuxClone(ctx, grant, checkout, pendingWithRootProof)
	if err != nil || cleanupProof.Validate() != nil || !cleanupProof.Intent.Equal(cleanupIntent) ||
		!cleanupProof.TargetRemovalProof.Equal(rootProof) || !cleanupProof.SeedAbsent || !cleanupProof.BaseRefAbsent ||
		!cleanupProof.ScratchAbsent || !cleanupProof.PromotionRefPresent {
		t.Fatalf("retire exact A seed/base/scratch after root proof while preserving promotion ref: %v", err)
	}
	completed, err := store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, cleanupProof)
	if err != nil || completed.LinuxCloneCleanupPending || completed.LinuxCloneCleanupIntent == nil ||
		!completed.LinuxCloneCleanupIntent.Equal(cleanupIntent) || completed.LinuxCloneCleanupProof == nil ||
		!completed.LinuxCloneCleanupProof.Equal(cleanupProof) || completed.WorkspacePath != "" ||
		completed.WorkspaceRecoveryRequired || !completed.LinuxCloneCleanupEligible(key, fence) {
		t.Fatalf("persist final exact State cleanup completion proof: %v", err)
	}
	finalReadback, err := store.LoadJournal(key)
	if err != nil || finalReadback.LinuxCloneCleanupProof == nil || !finalReadback.LinuxCloneCleanupProof.Equal(cleanupProof) ||
		!finalReadback.LinuxCloneCleanupEligible(key, fence) {
		t.Fatalf("read back completed cleanup proof before journal retirement: %v", err)
	}
	promotedRefValue, err := linuxV2CleanupRunGit(ctx, gitPath, binding.Repository, gitEnv, "rev-parse", "--verify", promotionRef)
	if err != nil || promotedRefValue != candidateSubject.Commit {
		t.Fatalf("A promotion ref did not survive normal cleanup with its exact candidate: %v", err)
	}
	if _, err := os.Lstat(intent.SeedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact A seed still exists after the final cleanup proof: %v", err)
	}
	if err := linuxV2CleanupRequireTargetAbsent(rootFD, targetRel); err != nil {
		t.Fatalf("B target reappeared before final journal retirement: %v", err)
	}
	if err := linuxV2CleanupVerifySibling(rootFD, siblingRel, siblingFD, sentinelFD, originalSiblingInfo, originalSiblingSentinelInfo, siblingContent+"\n"); err != nil {
		t.Fatalf("A cleanup changed the B sibling outside the exact target: %v", err)
	}
	if err := linuxV2CleanupVerifySibling(rootFD, otherRunRel, otherRunFD, otherRunSentinelFD, otherRunInfo, otherRunSentinelInfo, otherRunContent+"\n"); err != nil {
		t.Fatalf("A cleanup changed the independent B RunKey checkout: %v", err)
	}
	if err := linuxV2CleanupVerifyDirectory(rootFD, otherGitRel, otherGitFD, otherGitInfo, owner.BUID); err != nil {
		t.Fatalf("A cleanup changed the independent checkout's B-owned Git metadata: %v", err)
	}
	document.OtherRunCheckout.IntactAfterACleanup = true
	document.OtherRunCheckout.GitMetadataDirectoryBOwned = uint32(otherGitInfo.Uid) == owner.BUID
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("retire exact RunJournal after target and A-side proof readbacks: %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("completed exact cleanup journal remained after retirement: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close fixed StateDir after cleanup journal retirement: %v", err)
	}
	storeOpen = false
	if err := parent.Close(); err != nil {
		t.Fatalf("close delegated cgroup-v2 parent after all real helper operations: %v", err)
	}
	parentOpen = false
	if err := peer.Close(); err != nil {
		t.Fatalf("close exact B worker pidfd after its physical stop: %v", err)
	}
	peerOpen = false

	document.Promotion.RefRetained = promotedRefValue == candidateSubject.Commit
	document.AWorkspaceCleanup.ProofValid = cleanupProof.Validate() == nil
	document.AWorkspaceCleanup.SeedAbsent = cleanupProof.SeedAbsent
	document.AWorkspaceCleanup.BaseRefAbsent = cleanupProof.BaseRefAbsent
	document.AWorkspaceCleanup.ScratchAbsent = cleanupProof.ScratchAbsent
	document.AWorkspaceCleanup.PromotionRefPresent = cleanupProof.PromotionRefPresent && document.Promotion.RefRetained
	document.JournalRetired = true
	if err := linuxV2CleanupWriteWitness(outputDir, document); err != nil {
		t.Fatalf("write only the redacted installed cleanup witness JSON: %v", err)
	}
	t.Log(linuxV2CleanupWitnessEvidenceLabel)
	t.Logf("test-scoped direct Runner synthetic RunKey %s completed actual helper stop, opaque release, local Store CAS, authenticated Anchor removal, and A cleanup; not App end-to-end, no Control work item or acceptance was used", key.RunID)
}

func linuxV2CleanupJournalCallbacks(
	store *state.Store,
	key state.RunKey,
	fence protocol.Fence,
	cloneWorkspace *workspace.Manager,
	grant authority.LinuxCloneGrant,
	candidate protocol.Subject,
	observation *linuxV2CleanupWitnessObservation,
) execution.LinuxV2JournalCallbacks {
	mutate := func(write func() (state.RunJournal, error)) (state.RunJournal, error) {
		before, err := store.LoadJournal(key)
		if err != nil || before.Key() != key || before.Fence() != fence {
			return state.RunJournal{}, errors.Join(errors.New("Linux v2 callback started with a stale RunKey/fence"), err)
		}
		updated, err := write()
		if err != nil {
			return state.RunJournal{}, err
		}
		if updated.Key() != key || updated.Fence() != fence {
			return state.RunJournal{}, errors.New("Linux v2 callback mutation changed the exact RunKey/fence")
		}
		readback, err := store.LoadJournal(key)
		if err != nil || readback.Key() != key || readback.Fence() != fence {
			return state.RunJournal{}, errors.Join(errors.New("Linux v2 callback mutation lost its exact RunKey/fence readback"), err)
		}
		return readback, nil
	}
	return execution.LinuxV2JournalCallbacks{
		Reserve: func(reservation state.LinuxCgroupReservation) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.ReserveLinuxCgroup(key, fence, reservation, nil) })
			return err
		},
		MarkCreateStarted: func(reservation state.LinuxCgroupReservation) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil)
			})
			return err
		},
		BindLeaf: func(reservation state.LinuxCgroupReservation, identity authority.LinuxCgroupIdentity) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.BindLinuxCgroupLeaf(key, reservation, identity) })
			return err
		},
		BindHelper: func(reservation state.LinuxCgroupReservation, helper authority.LinuxCgroupProcessIdentity) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.BindLinuxCgroupHelper(key, reservation, helper) })
			return err
		},
		BindLauncher: func(reservation state.LinuxCgroupReservation, launcher authority.LinuxCgroupProcessIdentity) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.BindLinuxCgroupLauncher(key, reservation, launcher) })
			return err
		},
		ArmLauncherGrant: func(reservation state.LinuxCgroupReservation) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.ArmLinuxCgroupLauncherGrant(key, fence, reservation, nil)
			})
			return err
		},
		RecordLauncherExit: func(reservation state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.RecordLinuxCgroupLauncherExit(key, reservation, proof) })
			return err
		},
		RecordHelperExit: func(reservation state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.RecordLinuxCgroupHelperExit(key, reservation, proof) })
			return err
		},
		BindGate: func(reservation state.LinuxCgroupReservation, gate authority.LinuxCgroupGateIdentity) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.BindLinuxCgroupGate(key, reservation, gate) })
			return err
		},
		PrepareHandoff: func(handoff authority.SupervisorHandoff) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.PrepareSupervisorHandoff(key, handoff) })
			return err
		},
		BindHandoffV2: func(handoff authority.SupervisorHandoff, helper, mirror authority.LinuxCgroupProcessIdentity) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.BindSupervisorHandoffV2(key, handoff, helper, mirror) })
			return err
		},
		CommitHandoff: func(handoff authority.SupervisorHandoff, startedAt time.Time) error {
			_, err := mutate(func() (state.RunJournal, error) { return store.CommitSupervisorHandoff(key, handoff, startedAt) })
			return err
		},
		RecordStopReceipt: func(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
			if !receipt.ValidForHandoff(handoff) || receipt.LinuxProof == nil {
				return errors.New("real helper StopLeaf receipt is invalid for the exact v2 handoff")
			}
			journal, err := mutate(func() (state.RunJournal, error) {
				return store.RecordSupervisorHandoffStopReceipt(key, handoff, receipt)
			})
			if err != nil || journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil ||
				!journal.ContainmentAuthority.StopReceipt.Equal(receipt) || journal.ContainmentCgroupReleaseProof != nil {
				return errors.Join(errors.New("exact real StopReceipt did not read back before release"), err)
			}
			observation.mu.Lock()
			receiptCopy := receipt
			observation.stopReceipt = &receiptCopy
			observation.mu.Unlock()
			return nil
		},
		RecordPreGateStopReceipt: func(reservation state.LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.RecordLinuxCgroupPreGateStopReceipt(key, reservation, receipt)
			})
			return err
		},
		AbortReservation: func(reservation state.LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.AbortLinuxCgroupReservation(key, reservation, proof)
			})
			return err
		},
		ClearHandoffV2: func(handoff authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.ClearSupervisorHandoffV2(key, handoff, release)
			})
			return err
		},
		ClearProcessDetailsV2: func(pid int, identity string, release platform.LinuxCgroupV2Release) error {
			releaseProof, issued := release.Snapshot()
			if !issued || releaseProof.Validate() != nil || releaseProof.StopReceipt == nil ||
				releaseProof.TargetPID != pid || releaseProof.TargetIdentity != identity ||
				releaseProof.LeafDisposition != authority.LinuxCgroupLeafRemoved {
				return errors.New("Store clear requires the exact opaque normal Linux v2 leaf-removal release")
			}
			journal, err := store.LoadJournal(key)
			if err != nil || journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil ||
				!journal.ContainmentAuthority.StopReceipt.Equal(*releaseProof.StopReceipt) || journal.ContainmentCgroupReleaseProof != nil {
				return errors.Join(errors.New("opaque release does not match the State-recorded real StopReceipt"), err)
			}
			readback, err := mutate(func() (state.RunJournal, error) {
				return store.ClearProcessDetailsV2(key, pid, identity, release)
			})
			if err != nil || readback.HasProcessDetails() || readback.ContainmentAuthority != nil ||
				readback.ContainmentHandoff != nil || readback.ContainmentCgroup != nil ||
				readback.ContainmentCgroupReleaseProof == nil || !readback.ContainmentCgroupReleaseProof.Equal(releaseProof) {
				return errors.Join(errors.New("opaque release State clear did not read back exactly"), err)
			}
			observation.mu.Lock()
			proofCopy := releaseProof.Clone()
			observation.releaseProof = &proofCopy
			observation.opaqueReleaseSeen = true
			observation.storeClearReadback = true
			observation.mu.Unlock()
			return nil
		},
		ClearReservationV2: func(reservation state.LinuxCgroupReservation, release platform.LinuxCgroupV2Release) error {
			_, err := mutate(func() (state.RunJournal, error) {
				return store.ClearLinuxCgroupReservationV2(key, reservation, release)
			})
			return err
		},
		RecordLinuxCloneCheckoutReady: func(checkout authority.LinuxCloneCheckoutReceipt) error {
			if err := cloneWorkspace.ValidateLinuxCloneCheckout(grant, checkout); err != nil {
				return err
			}
			journal, err := mutate(func() (state.RunJournal, error) {
				return store.RecordLinuxCloneCheckoutReady(key, fence, grant.Intent, checkout)
			})
			if err != nil || journal.Fence() != fence || journal.LinuxCloneCheckoutReceipt == nil ||
				!journal.LinuxCloneCheckoutReceipt.Equal(checkout) {
				return errors.Join(errors.New("real B checkout receipt did not read back under the exact RunKey/fence"), err)
			}
			return nil
		},
		LinuxCloneProposedCommit: func(ctx context.Context, outcome execution.LinuxCloneRunOutcome) (string, bool, error) {
			if ctx == nil {
				return "", false, errors.New("synthetic cleanup candidate context is nil")
			}
			if err := ctx.Err(); err != nil {
				return "", false, err
			}
			if !outcome.PromotionEligible() {
				return "", false, nil
			}
			observation.mu.Lock()
			workerCommit := observation.workerCandidate
			observation.mu.Unlock()
			if workerCommit == "" || workerCommit != candidate.Commit {
				return "", false, errors.New("controlled B worker did not emit the exact independently derived candidate commit")
			}
			promotionIntent := authority.LinuxClonePromotionIntent{
				Version: authority.LinuxCloneVersion, ResourceID: candidate.ResourceID,
				Commit: candidate.Commit, TreeDigest: candidate.TreeDigest,
			}
			if err := promotionIntent.Validate(); err != nil {
				return "", false, err
			}
			if _, err := store.ReserveLinuxClonePromotion(key, fence, grant.Intent, promotionIntent); err != nil {
				return "", false, err
			}
			journal, err := store.LoadJournal(key)
			if err != nil || journal.LinuxClonePromotionIntent == nil ||
				!journal.LinuxClonePromotionIntent.Equal(promotionIntent) || journal.ContainmentAuthority == nil ||
				journal.ContainmentAuthority.StopReceipt == nil || journal.ContainmentCgroupReleaseProof != nil {
				return "", false, errors.Join(errors.New("promotion intent CAS lacks exact pre-clear StopReceipt readback"), err)
			}
			observation.mu.Lock()
			intentCopy := promotionIntent.Clone()
			observation.promotionIntent = &intentCopy
			observation.mu.Unlock()
			return workerCommit, true, nil
		},
		RetainUnpromotedLinuxClone: func(ctx context.Context) error {
			if ctx == nil {
				return errors.New("unpromoted clone retention context is nil")
			}
			journal, err := store.LoadJournal(key)
			if err != nil || journal.Fence() != fence || journal.LinuxCloneIntent == nil ||
				!journal.LinuxCloneIntent.Equal(grant.Intent) || journal.LinuxClonePromotionIntent != nil ||
				journal.LinuxClonePromotionReceipt != nil {
				return errors.Join(errors.New("unpromoted clone retention differs from the exact test RunKey/fence"), err)
			}
			_, err = mutate(func() (state.RunJournal, error) { return store.RetainWorkspace(key) })
			return err
		},
		PromoteLinuxClone: func(
			ctx context.Context,
			callbackGrant authority.LinuxCloneGrant,
			checkout authority.LinuxCloneCheckoutReceipt,
			commit string,
			stopReceipt authority.StopReceipt,
			release platform.LinuxCgroupV2Release,
		) (authority.LinuxClonePromotionReceipt, error) {
			if ctx == nil || ctx.Err() != nil || !grant.Intent.Equal(callbackGrant.Intent) || !grant.Seed.Equal(callbackGrant.Seed) ||
				commit != candidate.Commit {
				return authority.LinuxClonePromotionReceipt{}, errors.New("A promotion callback differs from the exact synthetic clone/candidate grant")
			}
			promotionIntent := authority.LinuxClonePromotionIntent{
				Version: authority.LinuxCloneVersion, ResourceID: candidate.ResourceID,
				Commit: candidate.Commit, TreeDigest: candidate.TreeDigest,
			}
			journal, err := store.LoadJournal(key)
			if err != nil || journal.Fence() != fence || journal.LinuxCloneCheckoutReceipt == nil ||
				!journal.LinuxCloneCheckoutReceipt.Equal(checkout) || journal.LinuxClonePromotionIntent == nil ||
				!journal.LinuxClonePromotionIntent.Equal(promotionIntent) || journal.ContainmentAuthority == nil ||
				journal.ContainmentAuthority.StopReceipt == nil || !journal.ContainmentAuthority.StopReceipt.Equal(stopReceipt) ||
				journal.ContainmentCgroupReleaseProof != nil {
				return authority.LinuxClonePromotionReceipt{}, errors.Join(errors.New("A promotion lacks the exact durable checkout, candidate, and physical StopReceipt"), err)
			}
			releaseProof, issued := release.Snapshot()
			if !issued || releaseProof.Validate() != nil || !releaseProof.ValidForSupervisor(*journal.ContainmentAuthority) ||
				releaseProof.StopReceipt == nil || !releaseProof.StopReceipt.Equal(stopReceipt) ||
				releaseProof.LeafDisposition != authority.LinuxCgroupLeafRemoved {
				return authority.LinuxClonePromotionReceipt{}, errors.New("A promotion lacks the matching opaque physical leaf-removal release")
			}
			promotion, err := cloneWorkspace.PromoteLinuxClone(ctx, grant, checkout, promotionIntent, release)
			if err != nil {
				return authority.LinuxClonePromotionReceipt{}, err
			}
			if promotion.Validate() != nil || promotion.Commit != candidate.Commit || promotion.TreeDigest != candidate.TreeDigest {
				return authority.LinuxClonePromotionReceipt{}, errors.New("Workspace promotion receipt differs from the independently derived B candidate")
			}
			observation.mu.Lock()
			intentCopy := promotionIntent.Clone()
			receiptCopy := promotion.Clone()
			observation.promotionIntent = &intentCopy
			observation.promotionReceipt = &receiptCopy
			observation.mu.Unlock()
			return promotion, nil
		},
		RecordLinuxClonePromotion: func(receipt authority.LinuxClonePromotionReceipt) error {
			promotionIntent := authority.LinuxClonePromotionIntent{
				Version: authority.LinuxCloneVersion, ResourceID: candidate.ResourceID,
				Commit: candidate.Commit, TreeDigest: candidate.TreeDigest,
			}
			if receipt.Validate() != nil || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest {
				return errors.New("promotion receipt differs from the exact candidate CAS")
			}
			if _, err := store.RecordLinuxClonePromotion(key, fence, grant.Intent, promotionIntent, receipt); err != nil {
				return err
			}
			journal, err := store.LoadJournal(key)
			if err != nil || journal.LinuxClonePromotionIntent == nil ||
				!journal.LinuxClonePromotionIntent.Equal(promotionIntent) || journal.LinuxClonePromotionReceipt == nil ||
				!journal.LinuxClonePromotionReceipt.Equal(receipt) {
				return errors.Join(errors.New("A promotion receipt CAS did not read back exactly"), err)
			}
			return nil
		},
		FinalizeNoCheckoutLinuxCloneAfterRetirement: func(
			ctx context.Context,
			abortIntent authority.LinuxCloneIntent,
			seed *authority.LinuxCloneSeedReceipt,
			preGate *authority.LinuxCgroupPreGateReleaseProof,
			abort *authority.LinuxCgroupReservationAbortProof,
		) error {
			if ctx == nil || !abortIntent.Equal(grant.Intent) || seed == nil || !seed.Equal(grant.Seed) ||
				(preGate != nil && abort != nil) {
				return errors.New("no-checkout finalization differs from the exact synthetic clone grant")
			}
			if _, err := store.FenceLinuxCloneBeforeCheckout(key, fence, abortIntent); err != nil {
				return err
			}
			proof, err := cloneWorkspace.AbortLinuxCloneBeforeCheckout(ctx, abortIntent, seed, preGate, abort, nil)
			if err != nil {
				return err
			}
			_, err = store.CompleteLinuxCloneAbort(key, fence, abortIntent, proof)
			return err
		},
	}
}

func linuxV2CleanupSyntheticAdmission(subject protocol.Subject, workItemID string) (protocol.Admission, error) {
	admissionID, err := linuxV2CleanupUUID()
	if err != nil {
		return protocol.Admission{}, err
	}
	goalID, err := linuxV2CleanupUUID()
	if err != nil {
		return protocol.Admission{}, err
	}
	snapshotID, err := linuxV2CleanupUUID()
	if err != nil {
		return protocol.Admission{}, err
	}
	contextHash := sha256.Sum256([]byte("test-only installed synthetic-goal cleanup admission"))
	workItemCopy := workItemID
	admission := protocol.Admission{
		SchemaVersion: protocol.AdmissionSchemaVersion,
		AdmissionID:   admissionID, GoalID: goalID, GoalRevision: 1,
		WorkItemID: &workItemCopy, Purpose: protocol.AdmissionPurposeImplement,
		ContextSnapshotID: snapshotID, ContextHash: "sha256:" + hex.EncodeToString(contextHash[:]),
		ModelProfile: "synthetic-cleanup-test", SessionMode: protocol.SessionModeFresh,
		Subject: subject,
		Limits: protocol.AdmissionLimits{
			MaxTurns: 1, DeadlineAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
		},
	}
	if err := admission.Validate(); err != nil {
		return protocol.Admission{}, err
	}
	return admission, nil
}

func linuxV2CleanupArtifactEvidence(runID, evidenceID string, subject protocol.Subject, path string, content []byte) (protocol.Evidence, error) {
	subjectHash, err := subject.Hash()
	if err != nil {
		return protocol.Evidence{}, err
	}
	contentHash := sha256.Sum256(content)
	contentDigest := "sha256:" + hex.EncodeToString(contentHash[:])
	resourceID, commit := subject.ResourceID, subject.Commit
	profile, predicateID := "synthetic-local", "synthetic-cleanup-artifact"
	subjectCopy := subject
	evidence := protocol.Evidence{
		SchemaVersion: protocol.EvidenceSchemaVersion,
		EvidenceID:    evidenceID, RunID: runID, EvidenceKey: "synthetic-cleanup-artifact",
		Kind: protocol.EvidenceArtifact, Subject: subject, SubjectHash: subjectHash,
		SourceRef: protocol.SourceRef{
			Kind: protocol.EvidenceArtifact, Ref: "test-local-synthetic-artifact",
			SubjectHash: &subjectHash, ResourceID: &resourceID, Commit: &commit, Path: &path,
		},
		SourceRevision: "synthetic-local-test", ValidatorProfile: &profile,
		Verdict: protocol.EvidencePassed,
		Payload: protocol.Receipt{
			PredicateID: &predicateID, Subject: &subjectCopy, SubjectHash: &subjectHash,
			ResourceID: &resourceID, Commit: &commit, Path: &path, ContentDigest: &contentDigest,
		},
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := evidence.Validate(); err != nil {
		return protocol.Evidence{}, err
	}
	return evidence, nil
}

func linuxV2CleanupPrepareCandidateSubject(
	t *testing.T,
	ctx context.Context,
	gitPath string,
	gitEnv []string,
	sourceRepository, resourceID, baseCommit, candidateFile, candidateContent string,
) (protocol.Subject, error) {
	t.Helper()
	listed, err := linuxV2CleanupRunGit(ctx, gitPath, sourceRepository, gitEnv, "ls-tree", baseCommit, "--", candidateFile)
	if err != nil {
		return protocol.Subject{}, err
	}
	if listed != "" {
		return protocol.Subject{}, errors.New("unique candidate path already exists in the admitted base tree")
	}
	scratchParent := t.TempDir()
	scratchRepository := filepath.Join(scratchParent, "candidate-source")
	if _, err := linuxV2CleanupRunGit(ctx, gitPath, scratchParent, gitEnv, "clone", "--quiet", "--no-hardlinks", "--no-checkout", sourceRepository, scratchRepository); err != nil {
		return protocol.Subject{}, err
	}
	if _, err := linuxV2CleanupRunGit(ctx, gitPath, scratchRepository, gitEnv, "checkout", "--quiet", "--detach", baseCommit); err != nil {
		return protocol.Subject{}, err
	}
	candidatePath := filepath.Join(scratchRepository, candidateFile)
	if err := os.WriteFile(candidatePath, []byte(candidateContent+"\n"), 0o644); err != nil {
		return protocol.Subject{}, err
	}
	if err := os.Chmod(candidatePath, 0o644); err != nil {
		return protocol.Subject{}, err
	}
	if _, err := linuxV2CleanupRunGit(ctx, gitPath, scratchRepository, gitEnv, "add", "--", candidateFile); err != nil {
		return protocol.Subject{}, err
	}
	tree, err := linuxV2CleanupRunGit(ctx, gitPath, scratchRepository, gitEnv, "write-tree")
	if err != nil {
		return protocol.Subject{}, err
	}
	commit, err := linuxV2CleanupRunGitWithEnv(ctx, gitPath, scratchRepository, gitEnv, linuxV2CleanupCommitEnvironment(),
		"-c", "commit.gpgsign=false", "commit-tree", tree, "-p", baseCommit, "-m", "test-only synthetic cleanup candidate")
	if err != nil {
		return protocol.Subject{}, err
	}
	if !linuxV2CleanupValidGitObjectID(commit) {
		return protocol.Subject{}, errors.New("independent scratch Git candidate has an invalid object ID")
	}
	if _, err := linuxV2CleanupRunGit(ctx, gitPath, scratchRepository, gitEnv, "update-ref", "--no-deref", "HEAD", commit); err != nil {
		return protocol.Subject{}, err
	}
	scratchWorkspace := workspace.New(map[string]config.Workspace{
		"synthetic-candidate": {Policy: config.WorkspacePolicyGitWorktree, Repository: scratchRepository},
	})
	subject, err := scratchWorkspace.MaterializeSubject(ctx, "synthetic-candidate", resourceID, commit)
	if err != nil {
		return protocol.Subject{}, err
	}
	return subject, nil
}

func linuxV2CleanupCommitEnvironment() []string {
	return []string{
		"GIT_AUTHOR_NAME=Symmetry Synthetic Cleanup Test",
		"GIT_AUTHOR_EMAIL=synthetic-cleanup@example.invalid",
		"GIT_AUTHOR_DATE=946684800 +0000",
		"GIT_COMMITTER_NAME=Symmetry Synthetic Cleanup Test",
		"GIT_COMMITTER_EMAIL=synthetic-cleanup@example.invalid",
		"GIT_COMMITTER_DATE=946684800 +0000",
	}
}

func linuxV2CleanupChildEnvironment(
	bAccount *user.User,
	baseCommit, candidateCommit, candidateFile, candidateContent, siblingPath, siblingContent, otherRunTarget, otherRunContent string,
) []string {
	home := filepath.Clean(bAccount.HomeDir)
	cache := filepath.Join(home, ".cache")
	return []string{
		"HOME=" + home,
		"USER=" + bAccount.Username,
		"LOGNAME=" + bAccount.Username,
		"TMPDIR=" + cache,
		"TEMP=" + cache,
		"TMP=" + cache,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + cache,
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"PATH=/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_AUTHOR_NAME=Symmetry Synthetic Cleanup Test",
		"GIT_AUTHOR_EMAIL=synthetic-cleanup@example.invalid",
		"GIT_AUTHOR_DATE=946684800 +0000",
		"GIT_COMMITTER_NAME=Symmetry Synthetic Cleanup Test",
		"GIT_COMMITTER_EMAIL=synthetic-cleanup@example.invalid",
		"GIT_COMMITTER_DATE=946684800 +0000",
		"SYMMETRY_CLEANUP_BASE_COMMIT=" + baseCommit,
		"SYMMETRY_CLEANUP_CANDIDATE_COMMIT=" + candidateCommit,
		"SYMMETRY_CLEANUP_CANDIDATE_FILE=" + candidateFile,
		"SYMMETRY_CLEANUP_CANDIDATE_CONTENT=" + candidateContent,
		"SYMMETRY_CLEANUP_SIBLING_PATH=" + siblingPath,
		"SYMMETRY_CLEANUP_SIBLING_CONTENT=" + siblingContent,
		"SYMMETRY_CLEANUP_OTHER_RUN_TARGET=" + otherRunTarget,
		"SYMMETRY_CLEANUP_OTHER_RUN_CONTENT=" + otherRunContent,
	}
}

func linuxV2CleanupGitEnvironment(home string) []string {
	return []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + filepath.Clean(home),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_REPLACE_OBJECTS=1",
		"LC_ALL=C",
	}
}

func linuxV2CleanupRunGit(ctx context.Context, gitPath, directory string, environment []string, args ...string) (string, error) {
	return linuxV2CleanupRunGitWithEnv(ctx, gitPath, directory, environment, nil, args...)
}

func linuxV2CleanupRunGitWithEnv(ctx context.Context, gitPath, directory string, environment, extraEnvironment []string, args ...string) (string, error) {
	if ctx == nil || gitPath == "" || !filepath.IsAbs(gitPath) || directory == "" || !filepath.IsAbs(directory) {
		return "", errors.New("isolated Git command inputs are incomplete")
	}
	commandArgs := make([]string, 0, len(args)+2)
	if directory != "" {
		commandArgs = append(commandArgs, "-C", directory)
	}
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, gitPath, commandArgs...)
	command.Env = append(append([]string(nil), environment...), extraEnvironment...)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return "", fmt.Errorf("configure headless isolated Git process: %w", err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run isolated Git %q: %w", args[0], err)
	}
	return strings.TrimSpace(string(output)), nil
}

func linuxV2CleanupValidGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded)*2 == len(value) && value == strings.ToLower(value)
}

func linuxV2CleanupRecoveryEndpoint(stateDir string, key state.RunKey) (string, error) {
	// Mirror the App's private linuxV2RecoveryEndpoint derivation for this direct
	// Runner test; the shortened installed StateDir keeps the full hash below App's limit.
	if stateDir == "" || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || key.RunID == "" || key.Generation <= 0 {
		return "", errors.New("recovery endpoint inputs are invalid")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", key.RunID, key.Generation)))
	path := filepath.Join(stateDir, "owner-"+hex.EncodeToString(digest[:])+".sock")
	if len(path) >= 100 {
		return "", errors.New("StateDir is too long for App's bounded Linux v2 recovery endpoint")
	}
	return path, nil
}

func linuxV2CleanupOpenClonePins(root, target string) (rootFD, targetFD int, relativeTarget string, err error) {
	rootFD = -1
	targetFD = -1
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return -1, -1, "", errors.New("clone root/target path is not canonical absolute")
	}
	rootFD, err = linuxV2CleanupOpenAbsoluteDirectory(root)
	if err != nil {
		return -1, -1, "", err
	}
	relativeTarget, err = filepath.Rel(root, target)
	if err != nil || !linuxV2CleanupSafeRelativePath(relativeTarget) {
		_ = unix.Close(rootFD)
		return -1, -1, "", errors.New("checkout target is not safely beneath its exact clone root")
	}
	targetFD, err = linuxV2CleanupOpenAt(rootFD, relativeTarget, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		_ = unix.Close(rootFD)
		return -1, -1, "", err
	}
	return rootFD, targetFD, relativeTarget, nil
}

func linuxV2CleanupOpenAbsoluteDirectory(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, errors.New("directory path is not canonical absolute")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(fd)
			return -1, errors.New("directory path contains an invalid component")
		}
		next, openErr := unix.Openat2(fd, component, &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
		})
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, openErr
		}
		fd = next
	}
	return fd, nil
}

func linuxV2CleanupOpenAt(directoryFD int, relative string, flags int) (int, error) {
	if directoryFD < 0 || !linuxV2CleanupSafeRelativePath(relative) {
		return -1, errors.New("pinned relative path is invalid")
	}
	return unix.Openat2(directoryFD, relative, &unix.OpenHow{
		Flags:   uint64(flags),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	})
}

func linuxV2CleanupSafeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func linuxV2CleanupRequireLivePIDFD(pidfd int) error {
	if pidfd < 0 {
		return errors.New("pidfd is invalid")
	}
	poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	ready, err := unix.Poll(poll, 0)
	if err != nil {
		return err
	}
	if ready != 0 && poll[0].Revents&unix.POLLIN != 0 {
		return errors.New("exact pidfd already reports process exit")
	}
	return nil
}

func linuxV2CleanupRequireExitedPIDFD(pidfd int) error {
	if pidfd < 0 {
		return errors.New("pidfd is invalid")
	}
	poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	ready, err := unix.Poll(poll, 0)
	if err != nil {
		return err
	}
	if ready == 0 || poll[0].Revents&unix.POLLIN == 0 {
		return errors.New("exact B worker pidfd did not report process exit")
	}
	return nil
}

func linuxV2CleanupVerifyNamedTarget(rootFD int, relative string, expected unix.Stat_t) error {
	fd, err := linuxV2CleanupOpenAt(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var current unix.Stat_t
	if err := unix.Fstat(fd, &current); err != nil {
		return err
	}
	if current.Dev != expected.Dev || current.Ino != expected.Ino || current.Nlink != expected.Nlink {
		return errors.New("named B checkout differs from the original pinned inode")
	}
	return nil
}

func linuxV2CleanupRequireTargetAbsent(rootFD int, relative string) error {
	fd, err := linuxV2CleanupOpenAt(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err == nil {
		_ = unix.Close(fd)
		return errors.New("B checkout target reappeared after root removal")
	}
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	return fmt.Errorf("inspect B checkout absence: %w", err)
}

func linuxV2CleanupVerifySibling(
	rootFD int,
	relative string,
	directoryFD, sentinelFD int,
	expected, expectedSentinel unix.Stat_t,
	content string,
) error {
	var pinned unix.Stat_t
	if err := unix.Fstat(directoryFD, &pinned); err != nil {
		return err
	}
	if pinned.Dev != expected.Dev || pinned.Ino != expected.Ino || pinned.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("pinned B sibling directory changed")
	}
	namedFD, err := linuxV2CleanupOpenAt(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		return err
	}
	var named unix.Stat_t
	statErr := unix.Fstat(namedFD, &named)
	_ = unix.Close(namedFD)
	if statErr != nil || named.Dev != expected.Dev || named.Ino != expected.Ino {
		return errors.Join(errors.New("named B sibling no longer matches its original inode"), statErr)
	}
	var pinnedSentinel unix.Stat_t
	if err := unix.Fstat(sentinelFD, &pinnedSentinel); err != nil {
		return err
	}
	if pinnedSentinel.Dev != expectedSentinel.Dev || pinnedSentinel.Ino != expectedSentinel.Ino ||
		pinnedSentinel.Mode != expectedSentinel.Mode || pinnedSentinel.Nlink != expectedSentinel.Nlink ||
		pinnedSentinel.Mode&unix.S_IFMT != unix.S_IFREG || pinnedSentinel.Nlink == 0 {
		return errors.New("pinned B sibling sentinel differs from its original fstat identity")
	}
	namedSentinelFD, err := linuxV2CleanupOpenAt(rootFD, filepath.Join(relative, "sentinel.txt"),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK)
	if err != nil {
		return fmt.Errorf("reopen named B sibling sentinel without following links: %w", err)
	}
	defer unix.Close(namedSentinelFD)
	var namedSentinel unix.Stat_t
	if err := unix.Fstat(namedSentinelFD, &namedSentinel); err != nil {
		return err
	}
	if namedSentinel.Dev != expectedSentinel.Dev || namedSentinel.Ino != expectedSentinel.Ino ||
		namedSentinel.Mode != expectedSentinel.Mode || namedSentinel.Nlink != expectedSentinel.Nlink ||
		namedSentinel.Uid != expectedSentinel.Uid || namedSentinel.Mode&unix.S_IFMT != unix.S_IFREG || namedSentinel.Nlink == 0 {
		return errors.New("reopened named B sibling sentinel differs from its original pinned fstat")
	}
	pinnedContent, err := linuxV2CleanupReadFD(sentinelFD)
	if err != nil || pinnedContent != content {
		return errors.Join(errors.New("pinned B sibling sentinel content changed"), err)
	}
	namedContent, err := linuxV2CleanupReadFD(namedSentinelFD)
	if err != nil || namedContent != content || namedContent != pinnedContent ||
		namedSentinel.Dev != pinnedSentinel.Dev || namedSentinel.Ino != pinnedSentinel.Ino {
		return errors.Join(errors.New("named B sibling sentinel content changed"), err)
	}
	return nil
}

func linuxV2CleanupVerifyDirectory(rootFD int, relative string, directoryFD int, expected unix.Stat_t, ownerUID uint32) error {
	var pinned unix.Stat_t
	if err := unix.Fstat(directoryFD, &pinned); err != nil {
		return err
	}
	if pinned.Dev != expected.Dev || pinned.Ino != expected.Ino || pinned.Mode&unix.S_IFMT != unix.S_IFDIR || uint32(pinned.Uid) != ownerUID {
		return errors.New("pinned checkout metadata directory changed identity or owner")
	}
	namedFD, err := linuxV2CleanupOpenAt(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		return err
	}
	var named unix.Stat_t
	statErr := unix.Fstat(namedFD, &named)
	_ = unix.Close(namedFD)
	if statErr != nil || named.Dev != expected.Dev || named.Ino != expected.Ino || uint32(named.Uid) != ownerUID {
		return errors.Join(errors.New("named checkout metadata directory no longer matches its B-owned inode"), statErr)
	}
	return nil
}

func linuxV2CleanupReadFD(fd int) (string, error) {
	if fd < 0 {
		return "", errors.New("pinned file descriptor is invalid")
	}
	buffer := make([]byte, 4096)
	n, err := unix.Pread(fd, buffer, 0)
	if err != nil {
		return "", err
	}
	if n == len(buffer) {
		return "", errors.New("pinned test file exceeds the bounded read")
	}
	return string(buffer[:n]), nil
}

func linuxV2CleanupUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func linuxV2CleanupToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func linuxV2CleanupWriteWitness(outputDir string, document linuxV2CleanupWitnessDocument) error {
	if !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) != outputDir {
		return errors.New("witness output directory is not canonical absolute")
	}
	name := filepath.Join(outputDir, linuxV2CleanupWitnessEvidenceFile)
	if _, err := os.Lstat(name); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("redacted cleanup witness output already exists or cannot be inspected")
	}
	temporary, err := os.CreateTemp(outputDir, ".synthetic-cleanup-witness-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, temporaryName, unix.AT_FDCWD, name, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	directoryFD, err := unix.Open(outputDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	if err := unix.Fsync(directoryFD); err != nil {
		return err
	}
	return nil
}
