//go:build linux && amd64

package platform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	daemonprotocol "github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const linuxOwnerAnchorTestRole = "SYMMETRY_LINUX_OWNER_ANCHOR_TEST_ROLE"

type linuxOwnerAnchorSubprocessEvent struct {
	Stage                   string                               `json:"stage"`
	Anchor                  authority.LinuxCgroupProcessIdentity `json:"anchor,omitempty"`
	Helper                  authority.LinuxCgroupProcessIdentity `json:"helper,omitempty"`
	Process                 authority.LinuxCgroupProcessIdentity `json:"process,omitempty"`
	ForeignProcess          authority.LinuxCgroupProcessIdentity `json:"foreign_process,omitempty"`
	ParentPID               int                                  `json:"parent_pid,omitempty"`
	HelperExit              *authority.LinuxCgroupExitProof      `json:"helper_exit,omitempty"`
	ProcessExit             *authority.LinuxCgroupExitProof      `json:"process_exit,omitempty"`
	PhysicalOnlyWaited      bool                                 `json:"physical_only_waited,omitempty"`
	NoMigrationProven       bool                                 `json:"no_migration_proven,omitempty"`
	RunReapCount            int                                  `json:"run_reap_count,omitempty"`
	StopEvidenceCreated     bool                                 `json:"stop_evidence_created,omitempty"`
	ForeignProcessStillLive bool                                 `json:"foreign_process_still_live,omitempty"`
	Error                   string                               `json:"error,omitempty"`
}

// This test entry is also used as an isolated helper, target and daemon
// process. The environment role is set only by the focused subprocess tests.
func TestLinuxOwnerAnchorProcessHelper(t *testing.T) {
	role := os.Getenv(linuxOwnerAnchorTestRole)
	if role == "" {
		return
	}
	var err error
	switch role {
	case "anchor":
		err = runLinuxOwnerAnchorTestProcess()
	case "physical-anchor":
		err = runLinuxOwnerAnchorPhysicalOnlyTestProcess()
	case "helper":
		err = runLinuxOwnerAnchorTestHelper()
	case "target":
		select {}
	case "daemon":
		err = runLinuxOwnerAnchorTestDaemon()
	default:
		err = fmt.Errorf("unknown owner-anchor test process role %q", role)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func TestLinuxOwnerAnchorPIDFDReadyTracksExactChildExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	process := startLinuxOwnerAnchorTestProcess(t, ctx, "target", false)
	identity := authority.LinuxCgroupProcessIdentity{
		PID: process.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, process.command.Process.Pid),
	}
	pidfd := openLinuxOwnerAnchorTestPIDFD(t, identity)
	pidfdFile := os.NewFile(uintptr(pidfd), "linux-owner-anchor-helper-death-test-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(pidfd)
		t.Fatal("wrap exact helper pidfd")
	}
	defer pidfdFile.Close()
	if ready, err := linuxOwnerAnchorPIDFDReady(pidfdFile); err != nil || ready {
		t.Fatalf("live helper pidfd readiness = %t, %v; want not ready", ready, err)
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("kill exact helper by pidfd: %v", err)
	}
	if err := process.command.Wait(); err == nil {
		t.Fatal("killed helper subprocess unexpectedly exited successfully")
	}
	process.waited = true
	if ready, err := linuxOwnerAnchorPIDFDReady(pidfdFile); err != nil || !ready {
		t.Fatalf("exited helper pidfd readiness = %t, %v; want ready", ready, err)
	}
}

func TestLinuxOwnerHelperV2CloneCheckoutBindsExactRunGrant(t *testing.T) {
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: "run-1", Generation: 3, BindingKey: "primary",
		ResourceID: "00000000-0000-4000-8000-000000000001", BaseCommit: strings.Repeat("a", 40),
		BaseTreeDigest: "sha256:" + strings.Repeat("b", 64), Nonce: strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/srv/symmetry/seeds/run-1.bundle",
		Target:   "/srv/symmetry/work/binding-primary/run-run-1/generation-3",
	}
	clone := authority.LinuxCloneGrant{
		Intent: intent,
		Seed: authority.LinuxCloneSeedReceipt{
			Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
			Digest: "sha256:" + strings.Repeat("d", 64), Size: 128, Device: 7, Inode: 11,
		},
	}
	if err := clone.Validate(); err != nil {
		t.Fatalf("test clone grant is invalid: %v", err)
	}
	grant := LinuxOwnerAnchorGrant{RunKey: LinuxOwnerAnchorRunKey{RunID: intent.RunID, Generation: intent.Generation}}
	if !linuxOwnerHelperV2CloneGrantMatchesRun(&clone, grant, intent.Target) {
		t.Fatal("exact clone grant was not bound to its RunKey and B target")
	}
	receipt := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
		SeedDigest: clone.Seed.Digest, BaseCommit: intent.BaseCommit, TargetDevice: 9, TargetInode: 13,
	}
	if !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, clone) {
		t.Fatal("exact gate checkout receipt did not match the durable clone grant")
	}
	mutations := []func(*authority.LinuxCloneCheckoutReceipt){
		func(value *authority.LinuxCloneCheckoutReceipt) { value.RunID = "run-2" },
		func(value *authority.LinuxCloneCheckoutReceipt) { value.Generation++ },
		func(value *authority.LinuxCloneCheckoutReceipt) { value.BindingKey = "secondary" },
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.ResourceID = "00000000-0000-4000-8000-000000000002"
		},
		func(value *authority.LinuxCloneCheckoutReceipt) { value.Target = "/srv/symmetry/work/other" },
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.Nonce = strings.Repeat("e", authority.LinuxCloneNonceBytes*2)
		},
		func(value *authority.LinuxCloneCheckoutReceipt) {
			value.SeedDigest = "sha256:" + strings.Repeat("f", 64)
		},
		func(value *authority.LinuxCloneCheckoutReceipt) { value.BaseCommit = strings.Repeat("9", 40) },
	}
	for index, mutate := range mutations {
		changed := receipt
		mutate(&changed)
		if err := changed.Validate(); err != nil {
			t.Fatalf("mutated receipt %d is structurally invalid: %v", index, err)
		}
		if linuxOwnerHelperV2CheckoutReceiptMatches(changed, clone) {
			t.Fatalf("checkout receipt mutation %d matched a different clone grant", index)
		}
	}
	if linuxOwnerHelperV2CloneGrantMatchesRun(&clone, grant, "/srv/symmetry/work/other") {
		t.Fatal("clone grant was accepted for a different B working directory")
	}
}

func TestLinuxOwnerHelperFinalizationIsSingleFlightAndExactReplay(t *testing.T) {
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 7, Device: 8, Inode: 9,
		Path: "/sys/fs/cgroup/user.slice/symmetry/manager",
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 100, Identity: "anchor:100:created"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 101, Identity: "mirror:101:created"}
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Parent: parent, LeafName: strings.Repeat("b", 32), CreateStarted: false,
		LeafDisposition: authority.LinuxCgroupAbortLeafNeverCreated, OwnerUID: 1000,
		Anchor: anchor, Mirror: mirror, Witness: anchor, CreatorFenced: true, NoProducerCanAct: true,
		ParentUnchanged: true, LeafAbsent: true, LeafNeverAppeared: false,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("test abort proof is invalid: %v", err)
	}
	request := linuxOwnerHelperV2Request{Operation: "release-run", AbortProof: &proof}
	helper := &linuxOwnerHelperV2{}
	start := make(chan struct{})
	type result struct {
		record  *linuxOwnerHelperV2Finalization
		owner   bool
		already bool
		err     error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer workers.Done()
			<-start
			record, owner, already, err := helper.beginFinalization(request)
			results <- result{record: record, owner: owner, already: already, err: err}
		}()
	}
	close(start)
	workers.Wait()
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.record == nil || first.record != second.record ||
		first.owner == second.owner || first.already || second.already {
		t.Fatalf("concurrent finalizer claims = %#v and %#v; want one owner and one waiter on the same exact record", first, second)
	}
	helper.finishFinalization(first.record, nil, false)
	if err := waitLinuxOwnerHelperV2Finalization(first.record); err != nil {
		t.Fatalf("wait for serialized finalizer = %v", err)
	}
	replay, owner, already, err := helper.beginFinalization(request)
	if err != nil || owner || !already || replay != first.record {
		t.Fatalf("exact finalizer replay = (%p, owner=%t, already=%t, err=%v); want same already-finalized record", replay, owner, already, err)
	}
	changed := proof.Clone()
	changed.LaunchToken = strings.Repeat("c", authority.TokenBytes*2)
	conflicting := request
	conflicting.AbortProof = &changed
	if _, _, _, err := helper.beginFinalization(conflicting); err == nil {
		t.Fatal("finalizer accepted a different durable abort proof after exact retirement")
	}
}

func TestLinuxOwnerAnchorSubreaperAdoptsAndReapsAfterHelperDeath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	anchor := startLinuxOwnerAnchorTestProcess(t, ctx, "anchor", true)
	ready := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if ready.Stage != "ready" || ready.Anchor.PID != anchor.command.Process.Pid || ready.Helper.Validate() != nil || ready.Process.Validate() != nil {
		t.Fatalf("owner-anchor ready event = %#v", ready)
	}
	anchor.targetID = ready.Process
	anchor.targetFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Process)
	anchor.helperFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Helper)

	// The test daemon is a separate process with no parent/control relationship
	// to the anchor. Its death must not be mistaken for anchor or unit stop proof.
	daemon := startLinuxOwnerAnchorTestProcess(t, ctx, "daemon", true)
	daemonReady := readLinuxOwnerAnchorTestEvent(t, daemon.output)
	if daemonReady.Stage != "daemon-ready" {
		t.Fatalf("daemon process ready event = %#v", daemonReady)
	}
	daemonFD := openLinuxOwnerAnchorTestPIDFD(t, daemonReady.Anchor)
	defer unix.Close(daemonFD)
	if err := unix.PidfdSendSignal(daemonFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("kill exact daemon subprocess via pidfd: %v", err)
	}
	if err := daemon.command.Wait(); err == nil {
		t.Fatal("killed daemon subprocess unexpectedly exited successfully")
	}
	daemon.waited = true
	if got, err := ProcessIdentity(ready.Anchor.PID); err != nil || got != ready.Anchor.Identity {
		t.Fatalf("owner anchor identity after unrelated daemon death = %q, %v; want %q", got, err, ready.Anchor.Identity)
	}
	if err := unix.PidfdSendSignal(anchor.anchorFD, 0, nil, 0); err != nil {
		t.Fatalf("owner anchor did not survive unrelated daemon process death: %v", err)
	}

	if _, err := io.WriteString(anchor.input, "kill-helper\n"); err != nil {
		t.Fatalf("request helper loss witness: %v", err)
	}
	adopted := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if adopted.Stage != "adopted" || adopted.ParentPID != ready.Anchor.PID || adopted.HelperExit == nil ||
		!adopted.HelperExit.ValidFor(ready.Helper, ready.Anchor) {
		t.Fatalf("subreaper adoption event = %#v", adopted)
	}
	if got, err := ProcessIdentity(ready.Process.PID); err != nil || got != ready.Process.Identity {
		t.Fatalf("adopted descendant identity = %q, %v; want %q", got, err, ready.Process.Identity)
	}

	if _, err := io.WriteString(anchor.input, "exit-target\n"); err != nil {
		t.Fatalf("request adopted descendant exit: %v", err)
	}
	reaped := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if reaped.Stage != "reaped" || reaped.HelperExit == nil || reaped.ProcessExit == nil ||
		!reaped.HelperExit.ValidFor(ready.Helper, ready.Anchor) ||
		!reaped.ProcessExit.ValidForOwnedDescendant(ready.Process, ready.Helper, ready.Anchor, reaped.HelperExit) {
		t.Fatalf("adopted descendant wait/reap event = %#v", reaped)
	}
	if err := anchor.command.Wait(); err != nil {
		t.Fatalf("anchor subprocess exited after exact reaping: %v", err)
	}
	anchor.waited = true
}

// This unprivileged subprocess fixture exercises the Anchor's physical-only
// adoption/wait boundary; it does not substitute for installed-gate or
// delegated-cgroup host validation.
func TestLinuxOwnerAnchorPhysicalOnlyChildReapsWithoutRunReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	anchor := startLinuxOwnerAnchorTestProcess(t, ctx, "physical-anchor", true)
	ready := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if ready.Stage != "ready" || ready.Anchor.PID != anchor.command.Process.Pid ||
		ready.Helper.Validate() != nil || ready.Process.Validate() != nil || ready.ForeignProcess.Validate() != nil {
		t.Fatalf("pre-RegisterGate physical containment ready event = %#v", ready)
	}
	anchor.targetID = ready.Process
	anchor.targetFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Process)
	anchor.helperFD = openLinuxOwnerAnchorTestPIDFD(t, ready.Helper)
	anchor.foreignFD = openLinuxOwnerAnchorTestPIDFD(t, ready.ForeignProcess)

	if _, err := io.WriteString(anchor.input, "kill-helper-physical\n"); err != nil {
		t.Fatalf("request helper loss after unregistered gate birth: %v", err)
	}
	adopted := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if adopted.Stage != "adopted" || adopted.ParentPID != ready.Anchor.PID || adopted.HelperExit == nil ||
		!adopted.HelperExit.ValidFor(ready.Helper, ready.Anchor) {
		t.Fatalf("unregistered gate adoption after helper loss = %#v", adopted)
	}
	if _, err := io.WriteString(anchor.input, "contain-unregistered-gate\n"); err != nil {
		t.Fatalf("request exact physical-only gate wait/reap: %v", err)
	}
	contained := readLinuxOwnerAnchorTestEvent(t, anchor.output)
	if contained.Stage != "physical-only-reaped" || !contained.PhysicalOnlyWaited ||
		contained.NoMigrationProven || contained.RunReapCount != 0 || contained.StopEvidenceCreated ||
		!contained.ForeignProcessStillLive || contained.ProcessExit != nil ||
		contained.HelperExit == nil || !contained.HelperExit.ValidFor(ready.Helper, ready.Anchor) {
		t.Fatalf("physical-only pre-RegisterGate result fabricated RunKey evidence or affected sibling: %#v", contained)
	}
	if identity, err := ProcessIdentity(ready.Process.PID); err == nil && identity == ready.Process.Identity {
		t.Fatalf("exact physical-only child %d remained live after Anchor wait/reap", ready.Process.PID)
	}
	if identity, err := ProcessIdentity(ready.ForeignProcess.PID); err != nil || identity != ready.ForeignProcess.Identity {
		t.Fatalf("unrelated foreign-run process changed after physical-only containment: identity=%q err=%v", identity, err)
	}
	foreignProbeFD, err := unix.FcntlInt(uintptr(anchor.foreignFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate foreign-process pidfd for readiness probe: %v", err)
	}
	foreignProbe := os.NewFile(uintptr(foreignProbeFD), "test-foreign-process-pidfd")
	if foreignProbe == nil {
		_ = unix.Close(foreignProbeFD)
		t.Fatal("wrap duplicated foreign-process pidfd")
	}
	defer foreignProbe.Close()
	if ready, err := linuxOwnerAnchorPIDFDReady(foreignProbe); err != nil || ready {
		t.Fatalf("physical-only wait changed unrelated foreign process pidfd: ready=%t err=%v", ready, err)
	}
	if _, err := io.WriteString(anchor.input, "finish-physical-test\n"); err != nil {
		t.Fatalf("release physical-only test anchor cleanup barrier: %v", err)
	}
	if err := anchor.command.Wait(); err != nil {
		t.Fatalf("physical-only anchor subprocess exited after exact target reap: %v", err)
	}
	anchor.waited = true
}

func TestLinuxOwnerAnchorWaitRejectsECHILDAndStalePIDIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	helper := startLinuxOwnerAnchorTestProcess(t, ctx, "helper", true)
	ready := readLinuxOwnerAnchorTestEvent(t, helper.output)
	if ready.Stage != "child-ready" || ready.Process.Validate() != nil {
		t.Fatalf("helper child event = %#v", ready)
	}
	helper.targetID = ready.Process
	targetFD := openLinuxOwnerAnchorTestPIDFD(t, ready.Process)
	targetFile := os.NewFile(uintptr(targetFD), "linux-owner-anchor-echild-target")
	if targetFile == nil {
		t.Fatal("wrap exact target pidfd")
	}
	helper.targetFile = targetFile

	stale := ready.Process
	stale.Identity += ":reused"
	if err := linuxOwnerAnchorWaitPIDFD(ctx, targetFile, stale); err == nil {
		t.Fatal("wait accepted a stale creation identity for a live numeric PID")
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, targetFile, ready.Process); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("wait on a non-child pidfd = %v, want ECHILD and no proof", err)
	}
	if _, err := io.WriteString(helper.input, "exit-target\n"); err != nil {
		t.Fatalf("tell helper to stop and reap its child: %v", err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatalf("helper exited after reaping its child: %v", err)
	}
	helper.waited = true
}

func TestLinuxOwnerAnchorPeerCredentialsAndManagerIdentityAreExact(t *testing.T) {
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if uid == 0 || gid == 0 || uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		t.Skip("exact unprivileged A UID/GID checks require an unprivileged test process")
	}
	processIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owner := protocol.OwnerConfig{AUID: uid, AGID: gid, BUID: uid + 1, BGID: gid + 1}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid(), cgroupPath); err != nil {
		t.Fatalf("verify current exact owner process: %v", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid()+1, cgroupPath); err == nil {
		t.Fatal("owner process verification accepted the wrong parent PID")
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), processIdentity, uid, gid, os.Getppid(), cgroupPath+"/other"); err == nil {
		t.Fatal("owner process verification accepted a different manager cgroup")
	}

	endpoint := filepath.Join(t.TempDir(), "owner.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	expected := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: processIdentity}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, expected); err != nil {
		t.Fatalf("authenticate real SO_PEERCRED peer: %v", err)
	}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 1, Identity: processIdentity}); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different peer PID")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, owner, authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: processIdentity + ":reused"}); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different process creation identity")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, protocol.OwnerConfig{AUID: uid + 1, AGID: gid, BUID: uid, BGID: gid + 1}, expected); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different A UID")
	}
	if err := verifyLinuxOwnerAnchorPeer(server, protocol.OwnerConfig{AUID: uid, AGID: gid + 1, BUID: uid + 1, BGID: gid}, expected); err == nil {
		t.Fatal("SO_PEERCRED verification accepted a different A GID")
	}

	if linuxOwnerCgroupPathsOverlap("/system.slice/symmetry-daemon.service", "/system.slice/symmetry-owner.service/manager") {
		t.Fatal("sibling systemd service cgroups were classified as one ownership subtree")
	}
	if !linuxOwnerCgroupPathsOverlap("/system.slice/symmetry-owner.service", "/system.slice/symmetry-owner.service/manager") {
		t.Fatal("daemon peer in the owner unit ancestor was not rejected as a shared survival boundary")
	}
}

func TestLinuxOwnerAnchorRunChildClassificationBindsRunIdentity(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := testLinuxOwnerAnchorGrant(authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 10, Identity: "linux:v2:classification-helper"}
	process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 11, Identity: "linux:v2:classification-child"}
	classification := linuxOwnerAnchorRunChildClassification{
		Version: linuxOwnerAnchorRunChildClassificationVersion,
		RunKey:  grant.RunKey, OriginRunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Helper: helper, Process: process, Role: "descendant",
		Origin: linuxOwnerAnchorRunChildOriginGateUserNSCurrent,
	}
	if err := classification.ValidateFor(grant, helper, process); err != nil {
		t.Fatalf("validate exact namespace-derived RunKey child classification: %v", err)
	}
	mutations := []func(*linuxOwnerAnchorRunChildClassification){
		func(value *linuxOwnerAnchorRunChildClassification) { value.Version++ },
		func(value *linuxOwnerAnchorRunChildClassification) { value.RunKey.RunID = "other-run" },
		func(value *linuxOwnerAnchorRunChildClassification) { value.RunKey.Generation++ },
		func(value *linuxOwnerAnchorRunChildClassification) { value.Fence.ClaimID = "other-claim" },
		func(value *linuxOwnerAnchorRunChildClassification) {
			value.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
		},
		func(value *linuxOwnerAnchorRunChildClassification) { value.Helper.PID++ },
		func(value *linuxOwnerAnchorRunChildClassification) { value.Process.Identity += ":reused" },
		func(value *linuxOwnerAnchorRunChildClassification) { value.Role = "native" },
		func(value *linuxOwnerAnchorRunChildClassification) {
			value.Origin = linuxOwnerAnchorRunChildOriginUnknown
		},
	}
	for index, mutate := range mutations {
		changed := classification
		mutate(&changed)
		if err := changed.ValidateFor(grant, helper, process); err == nil {
			t.Fatalf("classification mutation %d retained namespace-derived run authority", index)
		}
	}
	registered := classification
	registered.Origin = linuxOwnerAnchorRunChildOriginRegisteredCurrent
	registered.Role = "launcher"
	if err := registered.ValidateFor(grant, helper, process); err != nil {
		t.Fatalf("validate exact registered-process classification: %v", err)
	}
	physicalOnly := classification
	physicalOnly.OriginRunKey = LinuxOwnerAnchorRunKey{}
	physicalOnly.Origin = linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly
	physicalOnly.Role = "pre_release_gate"
	if err := physicalOnly.ValidateFor(grant, helper, process); err != nil {
		t.Fatalf("validate Anchor-only pre-release physical containment classification: %v", err)
	}
	if physicalOnly.currentRunMember() {
		t.Fatal("physical-only pre-release gate classification became run-bound receipt evidence")
	}
	physicalForeign := physicalOnly
	physicalForeign.Origin = linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign
	if err := physicalForeign.ValidateFor(grant, helper, process); err != nil || physicalForeign.currentRunMember() {
		t.Fatalf("physical-only foreign gate classification became current RunKey evidence: class=%+v err=%v", physicalForeign, err)
	}
}

func TestLinuxOwnerAnchorPreReleaseGateClassifierRejectsPostRegisterOrReleaseState(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchorIdentity := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchorIdentity, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 60, Identity: "linux:v2:pre-release-classifier-helper"}
	helperExit := &authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchorIdentity, Waited: true, Reaped: true,
	}
	gate := authority.LinuxCgroupGateIdentity{Process: authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 61, Identity: "linux:v2:pre-release-classifier-gate"}}
	stopFence := &LinuxOwnerAnchorProducerFence{}
	var namespaceFD, imageFD *os.File
	for _, openFile := range []**os.File{&namespaceFD, &imageFD} {
		file, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		*openFile = file
		defer file.Close()
	}
	for _, test := range []struct {
		name string
		edit func(*linuxOwnerAnchorRun)
	}{
		{name: "helper death absent", edit: func(run *linuxOwnerAnchorRun) { run.helperExit = nil }},
		{name: "gate registered", edit: func(run *linuxOwnerAnchorRun) { run.gateIdentity = &gate }},
		{name: "gate ready authenticated", edit: func(run *linuxOwnerAnchorRun) { run.gateReadyAuthenticated = true }},
		{name: "release authorized", edit: func(run *linuxOwnerAnchorRun) { run.gateReleaseAuthorized = true }},
		{name: "user namespace pinned", edit: func(run *linuxOwnerAnchorRun) { run.gateUserNamespaceFD = namespaceFD }},
		{name: "gate image pinned", edit: func(run *linuxOwnerAnchorRun) { run.gateImageFD = imageFD }},
		{name: "stop requested", edit: func(run *linuxOwnerAnchorRun) { run.stopRequested = true }},
		{name: "leaf stop attempted", edit: func(run *linuxOwnerAnchorRun) { run.stopLeafAttempted = true }},
		{name: "producer fence installed", edit: func(run *linuxOwnerAnchorRun) { run.stopFence = stopFence }},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := &linuxOwnerAnchorRun{grant: grant, helper: helper, helperExit: cloneLinuxOwnerAnchorExitProof(helperExit)}
			test.edit(run)
			classification, eligible, err := (&linuxOwnerAnchor{identity: anchorIdentity}).classifyUnregisteredPreReleaseGate(
				context.Background(), run, authority.LinuxCgroupProcessIdentity{}, -1, "",
			)
			if err != nil || eligible || classification != (linuxOwnerAnchorRunChildClassification{}) {
				t.Fatalf("post-registration/release physical-only classification = (%+v, %t, %v), want no classification", classification, eligible, err)
			}
		})
	}
}

func TestLinuxOwnerAnchorNamespaceMembershipDistinguishesForeignMigration(t *testing.T) {
	current := LinuxOwnerAnchorRunKey{RunID: "target-run", Generation: 1}
	foreign := LinuxOwnerAnchorRunKey{RunID: "foreign-run", Generation: 2}
	leaf := "/sys/fs/cgroup/user.slice/symmetry/manager/run-target"
	cases := []struct {
		name       string
		originRun  LinuxOwnerAnchorRunKey
		path       string
		wantOrigin linuxOwnerAnchorRunChildOrigin
	}{
		{name: "own leaf member", originRun: current, path: leaf, wantOrigin: linuxOwnerAnchorRunChildOriginGateUserNSCurrent},
		{name: "own nested cgroup member", originRun: current, path: leaf + "/nested", wantOrigin: linuxOwnerAnchorRunChildOriginGateUserNSCurrent},
		{name: "foreign migrated into target leaf", originRun: foreign, path: leaf, wantOrigin: linuxOwnerAnchorRunChildOriginOtherUserNSForeign},
		{name: "own process escaped leaf", originRun: current, path: "/sys/fs/cgroup/user.slice/symmetry/manager/other", wantOrigin: linuxOwnerAnchorRunChildOriginCurrentRunEscaped},
		{name: "unknown process", path: leaf, wantOrigin: linuxOwnerAnchorRunChildOriginUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxOwnerAnchorClassifyNamespaceMembership(current, test.originRun, test.path, leaf); got != test.wantOrigin {
				t.Fatalf("namespace membership origin = %q, want %q", got, test.wantOrigin)
			}
		})
	}
}

const linuxOwnerAnchorNamespaceTestMode = "SYMMETRY_LINUX_OWNER_ANCHOR_NAMESPACE_TEST_MODE"

// TestLinuxOwnerAnchorNamespaceTestProcess is an exec.Cmd child that creates
// a nested user namespace and keeps it alive until its parent's pipe closes.
func TestLinuxOwnerAnchorNamespaceTestProcess(t *testing.T) {
	switch os.Getenv(linuxOwnerAnchorNamespaceTestMode) {
	case "":
		return
	case "nested-parent":
		nested := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorNamespaceTestProcess$")
		nested.Env = append(os.Environ(), linuxOwnerAnchorNamespaceTestMode+"=block")
		nested.SysProcAttr = linuxOwnerAnchorTestUserNamespaceSysProcAttr(os.Getuid(), os.Getgid())
		nestedInput, err := nested.StdinPipe()
		if err != nil {
			fmt.Fprintf(os.Stdout, "error: nested user namespace stdin: %v\n", err)
			return
		}
		nested.Stdout = io.Discard
		nested.Stderr = io.Discard
		if err := nested.Start(); err != nil {
			_ = nestedInput.Close()
			fmt.Fprintf(os.Stdout, "error: nested user namespace unavailable: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stdout, "%d\n", nested.Process.Pid)
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = nestedInput.Close()
		_ = nested.Wait()
	case "block":
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		t.Fatalf("unknown namespace subprocess mode %q", os.Getenv(linuxOwnerAnchorNamespaceTestMode))
	}
}

func TestLinuxOwnerAnchorUserNamespaceAncestryFindsNestedGateAndRejectsSibling(t *testing.T) {
	startGate := func(mode string, uid, gid int) (*exec.Cmd, io.WriteCloser, *bufio.Reader, *os.File, LinuxUserNamespaceIdentity) {
		command := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorNamespaceTestProcess$")
		command.Env = append(os.Environ(), linuxOwnerAnchorNamespaceTestMode+"="+mode)
		command.SysProcAttr = linuxOwnerAnchorTestUserNamespaceSysProcAttr(uid, gid)
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatalf("create namespace test process input: %v", err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatalf("create namespace test process output: %v", err)
		}
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			_ = input.Close()
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOSYS) {
				t.Skipf("kernel disallows unprivileged user namespaces: %v", err)
			}
			t.Fatalf("start user namespace test process: %v", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, command.Process.Pid)}
		pidfd := openLinuxOwnerAnchorTestPIDFD(t, process)
		namespaceFD, namespace, err := openLinuxOwnerAnchorUserNamespaceFD(process, pidfd)
		if err != nil {
			_ = input.Close()
			_ = unix.Close(pidfd)
			_ = command.Wait()
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOSYS) {
				t.Skipf("kernel/procfs does not expose user namespace FDs: %v", err)
			}
			t.Fatalf("pin exact user namespace process: %v", err)
		}
		t.Cleanup(func() {
			_ = input.Close()
			_ = command.Wait()
			_ = namespaceFD.Close()
			_ = unix.Close(pidfd)
		})
		return command, input, bufio.NewReader(output), namespaceFD, namespace
	}

	_, gateInput, gateOutput, gateNamespaceFD, gateNamespace := startGate("nested-parent", os.Getuid(), os.Getgid())
	line, err := gateOutput.ReadString('\n')
	if err != nil {
		t.Fatalf("read nested namespace child identity: %v", err)
	}
	if strings.HasPrefix(line, "error:") {
		_ = gateInput.Close()
		t.Skipf("kernel disallows nested unprivileged user namespaces: %s", strings.TrimSpace(line))
	}
	var nestedPID int
	if _, err := fmt.Sscan(strings.TrimSpace(line), &nestedPID); err != nil || nestedPID <= 1 {
		t.Fatalf("nested namespace child PID %q is invalid: %v", strings.TrimSpace(line), err)
	}
	nestedProcess := authority.LinuxCgroupProcessIdentity{PID: nestedPID, Identity: mustLinuxOwnerAnchorTestIdentity(t, nestedPID)}
	nestedPIDFD := openLinuxOwnerAnchorTestPIDFD(t, nestedProcess)
	defer unix.Close(nestedPIDFD)
	nestedNamespaceFD, nestedNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(nestedProcess, nestedPIDFD)
	if err != nil {
		t.Fatalf("pin nested process user namespace: %v", err)
	}
	defer nestedNamespaceFD.Close()
	if nestedNamespace == gateNamespace {
		t.Fatal("nested process unexpectedly remained in the gate's exact user namespace")
	}

	_, _, _, foreignNamespaceFD, foreignNamespace := startGate("block", os.Getuid(), os.Getgid())
	if foreignNamespace == gateNamespace || foreignNamespace == nestedNamespace {
		t.Fatal("sibling gate process reused a pinned user namespace")
	}
	pins := []linuxOwnerAnchorNamespacePin{
		{runKey: LinuxOwnerAnchorRunKey{RunID: "namespace-run", Generation: 1}, namespace: gateNamespace, fd: gateNamespaceFD},
		{runKey: LinuxOwnerAnchorRunKey{RunID: "foreign-run", Generation: 2}, namespace: foreignNamespace, fd: foreignNamespaceFD},
	}
	anchorNamespaceFD, err := os.Open("/proc/self/ns/user")
	if err != nil {
		t.Fatal(err)
	}
	defer anchorNamespaceFD.Close()
	ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	matched, found, err := linuxOwnerAnchorUserNamespaceRun(ctx, nestedProcess, nestedPIDFD, pins, anchorNamespaceFD)
	if err != nil || !found || matched != pins[0].runKey {
		t.Fatalf("nested B namespace ancestry = (%+v, found=%t, err=%v), want exact gate RunKey", matched, found, err)
	}
	matched, found, err = linuxOwnerAnchorUserNamespaceRun(ctx, nestedProcess, nestedPIDFD, pins[1:], anchorNamespaceFD)
	if err != nil || found {
		t.Fatalf("nested namespace matched sibling gate = (%+v, found=%t, err=%v), want no match", matched, found, err)
	}
}

func TestLinuxOwnerAnchorUserNamespaceAncestryFailsClosedOnInaccessibleNamespaceFD(t *testing.T) {
	process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: mustLinuxOwnerAnchorTestIdentity(t, os.Getpid())}
	pidfd := openLinuxOwnerAnchorTestPIDFD(t, process)
	defer unix.Close(pidfd)
	invalid, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer invalid.Close()
	pins := []linuxOwnerAnchorNamespacePin{{
		runKey:    LinuxOwnerAnchorRunKey{RunID: "inaccessible-namespace", Generation: 1},
		namespace: LinuxUserNamespaceIdentity{Device: 1, Inode: 2}, fd: invalid,
	}}
	if _, found, err := linuxOwnerAnchorUserNamespaceRun(context.Background(), process, pidfd, pins, invalid); err == nil || found {
		t.Fatalf("inaccessible namespace FD classification = found:%t err:%v; want fail-closed error", found, err)
	}
}

func linuxOwnerAnchorTestUserNamespaceSysProcAttr(uid, gid int) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: uid, Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: gid, Size: 1}},
		GidMappingsEnableSetgroups: false,
	}
}

// This checks wire-shape binding only; the exit proofs here are synthetic and
// are not treated as process-stop evidence. The subprocess witness above
// exercises actual waitid/reap, while provisioned-host work must verify leaf
// membership before the anchor creates run-bound evidence.
func TestLinuxOwnerAnchorReapEvidenceRejectsRunAndRoleMismatches(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := testLinuxOwnerAnchorGrant(authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	changedGrant := grant
	changedGrant.Secret = strings.Repeat("f", authority.SecretBytes*2)
	if grant.Equal(changedGrant) {
		t.Fatal("grant equality ignored a changed per-run secret")
	}
	for _, mutate := range []struct {
		name string
		edit func(*LinuxOwnerAnchorGrant)
	}{
		{"uncommitted leaf", func(value *LinuxOwnerAnchorGrant) { value.CreateStarted = false }},
		{"missing secret", func(value *LinuxOwnerAnchorGrant) { value.Secret = "" }},
		{"root owner UID", func(value *LinuxOwnerAnchorGrant) { value.OwnerUID = 0 }},
		{"non-root launcher UID", func(value *LinuxOwnerAnchorGrant) { value.LauncherUID = value.OwnerUID }},
		{"fence generation", func(value *LinuxOwnerAnchorGrant) { value.Fence.Generation++ }},
		{"parent mismatch", func(value *LinuxOwnerAnchorGrant) { value.Parent.Inode++ }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := grant
			mutate.edit(&changed)
			if err := changed.Validate(); err == nil {
				t.Fatal("invalid durable owner grant was accepted")
			}
		})
	}
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationStart,
		RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken, Anchor: grant.Anchor,
		Parent: grant.Parent, OwnerUID: grant.OwnerUID, ManagerPath: grant.Parent.Path,
	}
	if !response.matches(grant, linuxOwnerAnchorOperationStart) {
		t.Fatal("exact grant response did not match its request")
	}
	response.RunKey.Generation++
	if response.matches(grant, linuxOwnerAnchorOperationStart) {
		t.Fatal("response from a different generation matched the durable grant")
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 10, Identity: "linux:v2:helper-create-identity"}
	process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 11, Identity: "linux:v2:gate-create-identity"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: grant.Anchor, Waited: true, Reaped: true,
	}
	processExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: process, Witness: grant.Anchor,
		Waited: true, Reaped: true, HelperExit: &helperExit,
	}
	evidence, err := makeLinuxOwnerAnchorReapEvidence(grant, helper, process, "gate", helperExit, processExit)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.ValidateFor(grant, helper, process, "gate"); err != nil {
		t.Fatalf("validate exact run-bound gate wait evidence: %v", err)
	}
	for _, mutate := range []struct {
		name string
		edit func(*LinuxOwnerAnchorReapEvidence)
	}{
		{"run key", func(value *LinuxOwnerAnchorReapEvidence) { value.RunKey.RunID += "-other" }},
		{"generation", func(value *LinuxOwnerAnchorReapEvidence) { value.RunKey.Generation++ }},
		{"fence", func(value *LinuxOwnerAnchorReapEvidence) { value.Fence.LeaseToken += "-other" }},
		{"launch token", func(value *LinuxOwnerAnchorReapEvidence) {
			value.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
		}},
		{"leaf", func(value *LinuxOwnerAnchorReapEvidence) { value.Leaf.LeafInode++ }},
		{"helper", func(value *LinuxOwnerAnchorReapEvidence) { value.Helper.Identity += "-reused" }},
		{"process", func(value *LinuxOwnerAnchorReapEvidence) { value.Process.Identity += "-reused" }},
		{"role", func(value *LinuxOwnerAnchorReapEvidence) { value.ProcessRole = "descendant" }},
		{"helper ECHILD-shaped proof", func(value *LinuxOwnerAnchorReapEvidence) { value.HelperExit.Reaped = false }},
		{"child ECHILD-shaped proof", func(value *LinuxOwnerAnchorReapEvidence) { value.ProcessExit.Waited = false }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := evidence
			mutate.edit(&changed)
			if err := changed.ValidateFor(grant, helper, process, "gate"); err == nil {
				t.Fatal("mismatched run-bound evidence was accepted")
			}
		})
	}
}

func TestLinuxOwnerAnchorArgumentsRequireExplicitOperatorPaths(t *testing.T) {
	valid, err := parseLinuxOwnerAnchorArgs([]string{
		"-cgroup-mount", "/sys/fs/cgroup",
		"-delegated-path", "system.slice/symmetry-owner.service/manager",
		"-socket", "/run/symmetry-owner/owner.sock",
	})
	if err != nil {
		t.Fatalf("parse explicit owner-anchor operator flags: %v", err)
	}
	if valid.cgroupMount != "/sys/fs/cgroup" || valid.delegatedPath != "system.slice/symmetry-owner.service/manager" ||
		valid.socketPath != "/run/symmetry-owner/owner.sock" {
		t.Fatalf("parsed operator flags = %#v", valid)
	}
	for _, args := range [][]string{
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "../escape", "-socket", "/run/symmetry-owner/owner.sock"},
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "system.slice/owner/manager", "-socket", "relative.sock"},
		{"-cgroup-mount", "/sys/fs/cgroup", "-delegated-path", "system.slice/owner/manager", "-socket", "/run/symmetry-owner/owner.sock", "unexpected"},
	} {
		if _, err := parseLinuxOwnerAnchorArgs(args); err == nil {
			t.Fatalf("accepted unsafe or extra operator arguments: %v", args)
		}
	}
}

func TestLinuxOwnerAnchorWitnessSelectorUsesStrictPinnedShape(t *testing.T) {
	valid := []byte(`{"version":1,"run_id":"run-1","generation":4,"helper_action":"crash_after_release_before_register","hold_for_release_file":true}`)
	witness, err := parseLinuxOwnerAnchorWitnessConfig(valid)
	if err != nil || witness == nil || witness.RunID != "run-1" || witness.Generation != 4 ||
		witness.HelperAction != "crash_after_release_before_register" || !witness.HoldForReleaseFile {
		t.Fatalf("parse exact release-crash selector = %#v, %v", witness, err)
	}
	invalid := [][]byte{
		[]byte(`{"version":1,"run_id":"run-1","generation":4,"drop_response":"stop","extra":true}`),
		[]byte(`{"version":1,"run_id":"run-1","generation":4,"drop_response":"stop"} {}`),
		[]byte(`{"version":1,"run_id":"run-1","generation":4,"drop_response":"unknown"}`),
		[]byte(`{"version":1,"run_id":"run-1","generation":0,"drop_response":"stop"}`),
		[]byte(`{"version":1,"run_id":"run-1","generation":4,"drop_response":"stop","helper_action":"crash_after_release_before_register"}`),
		[]byte(`{"version":1,"run_id":"run-1","generation":4,"helper_action":"crash_after_release_before_register"}`),
	}
	for _, contents := range invalid {
		if witness, err := parseLinuxOwnerAnchorWitnessConfig(contents); err == nil || witness != nil {
			t.Fatalf("accepted invalid root witness selector %s: %#v, %v", contents, witness, err)
		}
	}
}

func TestLinuxOwnerAnchorNativeTransitionRequiresRegisteredReleaseAuthorization(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: anchor, HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	run := &linuxOwnerAnchorRun{
		grant: grant, gateIdentity: &gate, gateUserNamespace: LinuxUserNamespaceIdentity{Device: 1, Inode: 2},
		children: make(map[int]linuxOwnerAnchorTrackedProcess),
	}
	if err := run.verifyAuthorizedGateExec(anchor, false); err == nil {
		t.Fatal("same-PID native transition succeeded without the helper's one-use release authorization")
	}
}

func TestLinuxOwnerAnchorCannotAuthorizeReleaseBeforeRegisterGate(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 50, Identity: "linux:v2:unregistered-helper"}
	gate := authority.LinuxCgroupGateIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 51, Identity: "linux:v2:unregistered-gate"},
		HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	run := &linuxOwnerAnchorRun{grant: grant, helper: helper}
	if err := run.authorizeGateRelease(gate, LinuxUserNamespaceIdentity{Device: 1, Inode: 2}); err == nil {
		t.Fatal("Anchor authorized private RELEASE without its authenticated RegisterGate state")
	}
	if run.gateReleaseAuthorized {
		t.Fatal("failed pre-RegisterGate authorization changed the one-use RELEASE state")
	}
}

func TestLinuxOwnerAnchorStartTombstoneIsOneUseAtForkBoundary(t *testing.T) {
	key := LinuxOwnerAnchorRunKey{RunID: "owner-anchor-run-test", Generation: 1}
	anchor := &linuxOwnerAnchor{runs: make(map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun)}
	if _, exists := anchor.runs[key]; exists {
		t.Fatal("pre-fork setup created a RunKey tombstone")
	}
	if err := anchor.recordHelperForkMayStart(key); err != nil {
		t.Fatalf("record exact run immediately before cmd.Start: %v", err)
	}
	if run, exists := anchor.runs[key]; !exists || run != nil {
		t.Fatalf("fork-boundary RunKey tombstone = (%#v,%t), want present nil entry", run, exists)
	}
	if err := anchor.recordHelperForkMayStart(key); !errors.Is(err, ErrLinuxOwnerAnchorHelperAlreadyStarted) {
		t.Fatalf("replayed fork-boundary tombstone error = %v, want already-started rejection", err)
	}
}

type denyingLinuxOwnerAnchorCleanupVerifier struct {
	calls int
	err   error
}

func (verifier *denyingLinuxOwnerAnchorCleanupVerifier) Verify(
	string,
	int64,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCleanupIntent,
	*authority.LinuxCloneTargetRemovalProof,
	bool,
) error {
	verifier.calls++
	return verifier.err
}

func (*denyingLinuxOwnerAnchorCleanupVerifier) Close() error { return nil }

func TestLinuxOwnerAnchorStateReadbackDenialPreventsRootCleanup(t *testing.T) {
	missingBegin := errors.New("durable RunJournal has no matching BeginCleanup")
	verifier := &denyingLinuxOwnerAnchorCleanupVerifier{err: missingBegin}
	rootWorkerCalled := false
	anchor := &linuxOwnerAnchor{
		cleanupVerifier: verifier,
		cloneCleanupWorker: func(
			context.Context,
			int,
			int,
			authority.LinuxCloneGrant,
			authority.LinuxCloneCleanupIntent,
			*authority.LinuxCloneTargetRemovalProof,
		) (authority.LinuxCloneTargetRemovalProof, error) {
			rootWorkerCalled = true
			return authority.LinuxCloneTargetRemovalProof{}, nil
		},
	}
	cleanup := authority.LinuxCloneCleanupIntent{
		Intent:      authority.LinuxCloneIntent{RunID: "unpersisted-cleanup", Generation: 1},
		FenceDigest: "sha256:" + strings.Repeat("a", 64),
	}
	grant := authority.LinuxCloneGrant{}
	for _, targetPresent := range []bool{true, false} {
		var prior *authority.LinuxCloneTargetRemovalProof
		targetFD := 11
		if !targetPresent {
			prior = &authority.LinuxCloneTargetRemovalProof{}
			targetFD = -1
		}
		request := linuxOwnerAnchorRequest{
			Operation:   linuxOwnerAnchorOperationCloneCleanup,
			RunKey:      LinuxOwnerAnchorRunKey{RunID: cleanup.Intent.RunID, Generation: cleanup.Intent.Generation},
			FenceDigest: cleanup.FenceDigest, CloneGrant: &grant, Cleanup: &cleanup,
			PriorRemovalProof: prior, TargetFDPresent: targetPresent,
		}
		_, err := anchor.runCloneCleanupAfterStateReadback(context.Background(), request, 10, targetFD, grant, cleanup, prior)
		if !errors.Is(err, errLinuxOwnerAnchorCleanupAuthority) {
			t.Fatalf("unpersisted cleanup result for target_present=%t = %v", targetPresent, err)
		}
	}
	if verifier.calls != 2 || rootWorkerCalled {
		t.Fatalf("unpersisted cleanup verifier calls = %d, root worker called = %t", verifier.calls, rootWorkerCalled)
	}
}

func TestLinuxOwnerAnchorLegacyOwnerConfigSkipsCleanupVerifier(t *testing.T) {
	if _, err := openLinuxOwnerAnchorCleanupVerifierForConfig(protocol.OwnerConfig{}, nil); err == nil {
		t.Fatal("nil cleanup-verifier factory accepted for legacy owner config")
	}
	opened := 0
	factory := func(stateDir string) (LinuxOwnerAnchorCleanupVerifier, error) {
		opened++
		return &denyingLinuxOwnerAnchorCleanupVerifier{}, nil
	}
	verifier, err := openLinuxOwnerAnchorCleanupVerifierForConfig(protocol.OwnerConfig{}, factory)
	if err != nil || verifier != nil || opened != 0 {
		t.Fatalf("legacy v1 verifier setup = (%T,%v), open count %d; want nil/no open", verifier, err, opened)
	}
	verifier, err = openLinuxOwnerAnchorCleanupVerifierForConfig(protocol.OwnerConfig{StateDir: "/var/lib/symmetry/state"}, factory)
	if err != nil || verifier == nil || opened != 1 {
		t.Fatalf("v2 verifier setup = (%T,%v), open count %d; want pinned verifier", verifier, err, opened)
	}
	if err := verifier.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openLinuxOwnerAnchorCleanupVerifierForConfig(protocol.OwnerConfig{StateDir: "/var/lib/../state"}, factory); err == nil || opened != 1 {
		t.Fatalf("noncanonical installed StateDir invoked verifier factory: open count %d", opened)
	}
	if _, err := openLinuxOwnerAnchorCleanupVerifierForConfig(protocol.OwnerConfig{StateDir: "/var/lib/symmetry/state"}, func(string) (LinuxOwnerAnchorCleanupVerifier, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("v2 StateDir verifier factory returned nil without an error")
	}
}

func TestRunLinuxOwnerAnchorRequiresCleanupVerifierFactory(t *testing.T) {
	if err := RunLinuxOwnerAnchor(nil, nil); err == nil || !strings.Contains(err.Error(), "State cleanup verifier factory") {
		t.Fatalf("Linux owner anchor entrypoint error = %v; want mandatory verifier-factory rejection", err)
	}
}

func TestLinuxOwnerAnchorFailedPrepareFenceCarriesOnlyExactUntrustedChild(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 40, Identity: "linux:v2:helper-failed-prepare-test"}
	launcher := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 41, Identity: "linux:v2:launcher-failed-prepare-test"}
	child := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 42, Identity: "linux:v2:untrusted-gate-child-test"}
	launcherExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	fence := LinuxOwnerAnchorProducerFence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Helper: helper, StopLatched: true, LeaseStopped: true,
		PrepareAttempted: true, PrepareQuiesced: true, ReleaseQuiesced: true, LeafMembershipVerified: true,
		Launcher: &launcher, LauncherExit: &launcherExit, UntrustedProcess: &child,
	}
	if err := fence.ValidateFor(grant, helper); err != nil {
		t.Fatalf("validate exact failed-Prepare containment fence: %v", err)
	}
	mutated := fence.Clone()
	mutated.ReleaseAttempted = true
	if err := mutated.ValidateFor(grant, helper); err == nil {
		t.Fatal("failed-Prepare untrusted child fence authorized RELEASE")
	}
	mutated = fence.Clone()
	mutated.GateOutcome = &authority.LinuxCgroupGateProductionResult{Version: authority.LinuxCgroupIdentityVersion}
	if err := mutated.ValidateFor(grant, helper); err == nil {
		t.Fatal("failed-Prepare untrusted child fence also claimed no gate was produced")
	}
}

func TestLinuxOwnerAnchorKillWriteRetriesOnlyAfterDefiniteNoWrite(t *testing.T) {
	for _, test := range []struct {
		name       string
		writeError error
		wantAck    bool
		allowRetry bool
	}{
		{name: "acknowledged", wantAck: true},
		{name: "unknown outcome", writeError: errors.New("write result unknown")},
		{name: "known no-write", writeError: errors.Join(errLinuxCgroupKillWriteNotAttempted, errors.New("control unavailable")), allowRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := &linuxOwnerAnchorRun{}
			calls := 0
			write := func(context.Context) error {
				calls++
				return test.writeError
			}
			firstErr := run.writeCgroupKillOnce(context.Background(), write)
			if test.writeError == nil && firstErr != nil || test.writeError != nil && firstErr == nil {
				t.Fatalf("first anchor kill result = %v, write error = %v", firstErr, test.writeError)
			}
			if run.leafKillAcknowledged != test.wantAck && !test.allowRetry {
				t.Fatalf("positive cgroup.kill ACK = %t, want %t", run.leafKillAcknowledged, test.wantAck)
			}
			if test.allowRetry {
				if run.leafKillCallStarted || run.leafKillAttempted {
					t.Fatal("definite pre-write error retained an attempted-write latch")
				}
				run.stopLeafAttempted = true
				fence := &LinuxOwnerAnchorProducerFence{
					Version: linuxOwnerAnchorStopEvidenceVersion,
					RunKey:  LinuxOwnerAnchorRunKey{RunID: "known-no-write", Generation: 1},
				}
				run.stopFence = fence
				if cached, err := run.cachedStopLeaf(fence); err != nil || cached != nil {
					t.Fatalf("cache after definite pre-write error = (%#v,%v), want safe retry", cached, err)
				}
				changedFence := fence.Clone()
				changedFence.StopLatched = true
				if cached, err := run.cachedStopLeaf(&changedFence); err == nil || cached != nil {
					t.Fatalf("cache accepted a changed producer fence after definite no-write: (%#v,%v)", cached, err)
				}
				retryErr := run.writeCgroupKillOnce(context.Background(), func(context.Context) error {
					calls++
					return nil
				})
				if retryErr != nil || calls != 2 || !run.leafKillAttempted || !run.leafKillAcknowledged {
					t.Fatalf("first actual write after definite no-write = err:%v calls:%d attempted:%t ack:%t", retryErr, calls, run.leafKillAttempted, run.leafKillAcknowledged)
				}
				return
			}
			secondErr := run.writeCgroupKillOnce(context.Background(), write)
			if calls != 1 {
				t.Fatalf("anchor called the writer %d times, want exactly one attempted write", calls)
			}
			if test.wantAck && secondErr != nil {
				t.Fatalf("replay after positive ACK did not reuse cached result: %v", secondErr)
			}
			if !test.wantAck && secondErr == nil {
				t.Fatal("replay after an unknown/no-write result fabricated positive evidence")
			}
			if !run.leafKillCallStarted {
				t.Fatal("one-use cgroup.kill call boundary was not retained")
			}
		})
	}
}

func TestLinuxOwnerAnchorUnresolvedRunDoesNotAbortSiblingSupervision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	healthyProcess := startLinuxOwnerAnchorTestProcess(t, ctx, "target", false)
	defer healthyProcess.stop(t)
	helper := authority.LinuxCgroupProcessIdentity{
		PID: healthyProcess.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, healthyProcess.command.Process.Pid),
	}
	helperPIDFD := openLinuxOwnerAnchorTestPIDFD(t, helper)
	helperFile := os.NewFile(uintptr(helperPIDFD), "sibling-run-helper-pidfd")
	if helperFile == nil {
		_ = unix.Close(helperPIDFD)
		t.Fatal("wrap sibling helper pidfd")
	}
	defer helperFile.Close()
	foreignChild := errors.New("foreign namespace process remains in this RunKey leaf")
	unresolved := &linuxOwnerAnchorRun{
		helperDeathContainmentAttempted: true,
		supervisionTerminal:             true,
		supervisionError:                foreignChild,
	}
	sibling := &linuxOwnerAnchorRun{helper: helper, helperPIDFD: helperFile}
	anchor := &linuxOwnerAnchor{runs: map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun{
		{RunID: "unresolved-run", Generation: 1}: unresolved,
		{RunID: "healthy-run", Generation: 1}:    sibling,
	}}
	if err := anchor.superviseExitedHelpers(); err != nil {
		t.Fatalf("one RunKey's unresolved containment aborted the owner service loop: %v", err)
	}
	if !unresolved.supervisionTerminal || !errors.Is(unresolved.supervisionError, foreignChild) || unresolved.leafKillAttempted {
		t.Fatalf("foreign RunKey was not retained unresolved without a kill: %#v", unresolved)
	}
	if sibling.helperDeathContainmentComplete || sibling.helperDeathContainmentAttempted || sibling.leafKillAttempted || sibling.stopRequested {
		t.Fatalf("unrelated sibling RunKey was changed by the foreign-child refusal: %#v", sibling)
	}
	if err := unix.PidfdSendSignal(int(helperFile.Fd()), 0, nil, 0); err != nil {
		t.Fatalf("healthy sibling helper was signalled by the unresolved RunKey: %v", err)
	}
}

func TestLinuxOwnerAnchorHelperDeathReusesCachedStopEvidence(t *testing.T) {
	fence := &LinuxOwnerAnchorProducerFence{RunKey: LinuxOwnerAnchorRunKey{RunID: "run-1", Generation: 1}}
	cached := &LinuxOwnerAnchorStopEvidence{RunKey: fence.RunKey, KillAttempted: true, KillAcknowledged: true}
	run := &linuxOwnerAnchorRun{
		stopLeafAttempted: true, stopFence: fence, stopEvidence: cached,
		leafKillAttempted: true, leafKillCallStarted: true, leafKillAcknowledged: true,
	}
	writes := 0
	if err := run.writeCgroupKillOnce(context.Background(), func(context.Context) error {
		writes++
		return nil
	}); err != nil || writes != 0 {
		t.Fatalf("helper-death takeover repeated the acknowledged cgroup.kill: writes=%d err=%v", writes, err)
	}
	for _, replayFence := range []*LinuxOwnerAnchorProducerFence{fence, nil} {
		replay, err := run.cachedStopLeaf(replayFence)
		if err != nil || replay == nil || !replay.Equal(*cached) {
			t.Fatalf("exact StopLeaf/takeover replay = %#v, %v; want cached proof", replay, err)
		}
	}
	conflictingFence := *fence
	conflictingFence.ReleaseAttempted = true
	if replay, err := run.cachedStopLeaf(&conflictingFence); err == nil || replay != nil {
		t.Fatalf("StopLeaf replay with a changed producer fence = %#v, %v; want conflict", replay, err)
	}
	unknown := &linuxOwnerAnchorRun{stopLeafAttempted: true, leafKillAttempted: true}
	if replay, err := unknown.cachedStopLeaf(nil); err == nil || replay != nil {
		t.Fatalf("unknown prior kill result replay = %#v, %v; want no evidence and no retry", replay, err)
	}
}

func TestLinuxOwnerAnchorNormalStopRejectsForeignRunChildInLeaf(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchorProcess, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	otherGrant := grant
	otherGrant.RunKey.RunID = "other-owner-anchor-run"
	otherGrant.Leaf.LeafName = strings.Repeat("f", authority.TokenBytes*2)
	otherKey := otherGrant.RunKey
	foreignRunProcess := startLinuxOwnerAnchorTestProcess(t, ctx, "helper", false)
	foreignChild := readLinuxOwnerAnchorTestEvent(t, foreignRunProcess.output)
	if foreignChild.Stage != "child-ready" || foreignChild.Process.Validate() != nil {
		t.Fatalf("foreign RunKey child event = %#v", foreignChild)
	}
	otherProcess := foreignChild.Process
	foreignRunProcess.targetID = otherProcess
	foreignRunProcess.targetFile = os.NewFile(
		uintptr(openLinuxOwnerAnchorTestPIDFD(t, otherProcess)), "foreign-run-cleanup-child-pidfd",
	)
	if foreignRunProcess.targetFile == nil {
		t.Fatal("wrap foreign RunKey cleanup child pidfd")
	}
	foreignHelper := authority.LinuxCgroupProcessIdentity{
		PID:      foreignRunProcess.command.Process.Pid,
		Identity: mustLinuxOwnerAnchorTestIdentity(t, foreignRunProcess.command.Process.Pid),
	}
	foreignHelperPIDFD := os.NewFile(
		uintptr(openLinuxOwnerAnchorTestPIDFD(t, foreignHelper)), "foreign-run-helper-pidfd",
	)
	if foreignHelperPIDFD == nil {
		t.Fatal("wrap foreign RunKey helper pidfd")
	}
	t.Cleanup(func() { _ = foreignHelperPIDFD.Close() })
	foreignChildPIDFD := os.NewFile(
		uintptr(openLinuxOwnerAnchorTestPIDFD(t, otherProcess)), "foreign-run-child-pidfd",
	)
	if foreignChildPIDFD == nil {
		t.Fatal("wrap foreign RunKey child pidfd")
	}
	t.Cleanup(func() { _ = foreignChildPIDFD.Close() })
	foreignClassification := linuxOwnerAnchorRunChildClassification{
		Version: linuxOwnerAnchorRunChildClassificationVersion,
		RunKey:  otherGrant.RunKey, OriginRunKey: otherGrant.RunKey, Fence: otherGrant.Fence, LaunchToken: otherGrant.LaunchToken,
		Helper: foreignHelper, Process: otherProcess, Role: "descendant", Origin: linuxOwnerAnchorRunChildOriginRegisteredCurrent,
	}
	if err := foreignClassification.ValidateFor(otherGrant, foreignHelper, otherProcess); err != nil {
		t.Fatalf("foreign RunKey child classification fixture: %v", err)
	}
	otherRun := &linuxOwnerAnchorRun{
		grant: otherGrant, helper: foreignHelper, helperPIDFD: foreignHelperPIDFD,
		children: map[int]linuxOwnerAnchorTrackedProcess{otherProcess.PID: {
			identity: otherProcess, creator: foreignHelper, classification: foreignClassification,
			role: "descendant", pidfd: foreignChildPIDFD,
		}},
	}
	run := &linuxOwnerAnchorRun{
		grant: grant, helper: authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 20, Identity: "linux:v2:target-run-helper"},
	}
	anchor := &linuxOwnerAnchor{
		identity: anchorProcess, managerPath: managerPath,
		runs: map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun{grant.RunKey: run, otherKey: otherRun},
	}
	currentLeafPath := strings.TrimRight(grant.Parent.Path, "/") + "/" + grant.Leaf.LeafName
	foreignPath, err := readLinuxOwnerProcessCgroupPath(otherProcess.PID)
	if err != nil {
		t.Fatal(err)
	}
	if skip, err := anchor.verifyNormalStopChildMembership(ctx, run, otherProcess, foreignPath); err != nil || !skip {
		t.Fatalf("foreign RunKey child outside this leaf = (skip=%t, err=%v), want skip", skip, err)
	}
	if skip, err := anchor.verifyNormalStopChildMembership(ctx, run, otherProcess, currentLeafPath); err == nil || skip {
		t.Fatalf("foreign RunKey child inside this leaf = (skip=%t, err=%v), want fail before cgroup.kill", skip, err)
	}
	if skip, err := anchor.verifyNormalStopChildMembership(ctx, run, anchorProcess, currentLeafPath); err == nil || skip {
		t.Fatalf("unregistered target-leaf child = (skip=%t, err=%v), want fail before cgroup.kill", skip, err)
	}
}

func TestLinuxOwnerAnchorStopEvidenceRequiresPositivePhysicalObservations(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 10, Identity: "linux:v2:helper-stop-evidence-test"}
	launcher := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 11, Identity: "linux:v2:launcher-stop-evidence-test"}
	gateProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 12, Identity: "linux:v2:gate-stop-evidence-test"}
	launcherExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: gateProcess, HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	producerFence := LinuxOwnerAnchorProducerFence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Helper: helper, StopLatched: true, LeaseStopped: true,
		PrepareAttempted: true, PrepareQuiesced: true, ReleaseQuiesced: true, LeafMembershipVerified: true,
		Launcher: &launcher, LauncherExit: &launcherExit, Gate: &gate,
		UserNamespace: &LinuxUserNamespaceIdentity{Device: 7, Inode: 9},
	}
	evidence := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf,
		Anchor: anchor, Helper: helper, ProducerFence: &producerFence,
		Launcher: &launcher, Gate: &gate, UserNamespace: producerFence.UserNamespace,
		PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true,
	}
	if err := evidence.ValidateFor(grant, helper, anchor, nil, false); err != nil {
		t.Fatalf("validate exact live-helper anchor StopLeaf evidence: %v", err)
	}
	run := &linuxOwnerAnchorRun{
		grant: grant, helper: helper, stopFence: &producerFence,
		leafKillAttempted: true, leafKillAcknowledged: true, leafEmptyObserved: true,
	}
	generated := makeLinuxOwnerAnchorStopEvidence(run, anchor)
	if generated.NoMigrationObserved {
		t.Fatal("Anchor synthesized NoMigrationObserved from an empty migration flag without its positive premise")
	}
	if err := generated.ValidateFor(grant, helper, anchor, nil, false); err == nil {
		t.Fatal("Anchor stop evidence without positive no-migration premise passed validation")
	}
	run.noMigrationProven = true
	generated = makeLinuxOwnerAnchorStopEvidence(run, anchor)
	if err := generated.ValidateFor(grant, helper, anchor, nil, false); err != nil {
		t.Fatalf("Anchor stop evidence with the separately established no-migration premise: %v", err)
	}
	for _, mutate := range []struct {
		name string
		edit func(*LinuxOwnerAnchorStopEvidence)
	}{
		{"kill not attempted", func(value *LinuxOwnerAnchorStopEvidence) { value.KillAttempted = false }},
		{"kill ACK missing", func(value *LinuxOwnerAnchorStopEvidence) { value.KillAcknowledged = false }},
		{"recursive empty missing", func(value *LinuxOwnerAnchorStopEvidence) { value.CgroupPopulatedZero = false }},
		{"migration check missing", func(value *LinuxOwnerAnchorStopEvidence) { value.NoMigrationObserved = false }},
		{"producer fence missing", func(value *LinuxOwnerAnchorStopEvidence) { value.ProducerFenced = false }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			invalid := evidence.Clone()
			mutate.edit(&invalid)
			if err := invalid.ValidateFor(grant, helper, anchor, nil, false); err == nil {
				t.Fatal("anchor stop evidence accepted a missing physical observation")
			}
		})
	}
}

func TestLinuxOwnerAnchorStopEvidenceBindsRetainedGateNamespacePin(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	namespaceFD, err := os.Open("/proc/self/ns/user")
	if err != nil {
		t.Fatal(err)
	}
	defer namespaceFD.Close()
	namespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(namespaceFD)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 70, Identity: "linux:v2:namespace-pin-helper"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchor, Waited: true, Reaped: true,
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: anchor, HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	evidence := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		HelperExit: &helperExit, Gate: &gate, UserNamespace: &namespace,
		PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true,
	}
	if err := evidence.ValidateFor(grant, helper, anchor, nil, false, nil); err == nil {
		t.Fatal("Anchor validation accepted gate-bound stop evidence without its retained nsfs pin")
	}
	if err := evidence.ValidateFor(grant, helper, anchor, nil, false, namespaceFD); err != nil {
		t.Fatalf("Anchor stop evidence matching its retained gate nsfs pin was rejected: %v", err)
	}
	wrongNamespace := namespace
	wrongNamespace.Inode++
	evidence.UserNamespace = &wrongNamespace
	if err := evidence.ValidateFor(grant, helper, anchor, nil, false, namespaceFD); err == nil {
		t.Fatal("Anchor stop evidence accepted a gate user-namespace identity different from its retained nsfs pin")
	}
}

func TestLinuxOwnerAnchorPhysicalOnlyGateCannotCreateStopReceipt(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 40, Identity: "linux:v2:physical-only-helper"}
	gate := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 41, Identity: "linux:v2:physical-only-gate"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchor, Waited: true, Reaped: true,
	}
	run := &linuxOwnerAnchorRun{
		grant: grant, helper: helper, helperExit: &helperExit,
		physicalOnlyChildren: map[int]linuxOwnerAnchorPhysicalOnlyChild{gate.PID: {identity: gate}},
		noMigrationProven:    true, reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}
	anchorCore := &linuxOwnerAnchor{identity: anchor}
	if err := anchorCore.establishRunNoMigrationProof(context.Background(), run, nil); err != nil {
		t.Fatalf("physical-only classification should remain containable without migration authority: %v", err)
	}
	if run.noMigrationProven {
		t.Fatal("physical-only gate established NoMigrationObserved without a registered GateIdentity")
	}
	evidence := makeLinuxOwnerAnchorStopEvidence(run, anchor)
	if err := evidence.ValidateFor(grant, helper, anchor, nil, false); err == nil {
		t.Fatal("physical-only pre-RegisterGate gate produced normal or PreGate StopReceipt evidence")
	}
	if run.stopEvidence != nil || len(run.reaped) != 0 {
		t.Fatalf("physical-only gate entered durable evidence state: stop=%#v reaps=%#v", run.stopEvidence, run.reaped)
	}
}

func TestLinuxOwnerAnchorRunStopEvidenceBindsCompleteReapSet(t *testing.T) {
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 20, Identity: "linux:v2:helper-run-stop-test"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchor, Waited: true, Reaped: true,
	}
	processes := []LinuxOwnerAnchorReapEvidence{}
	encoded, err := json.Marshal(processes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	evidence := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		HelperExit: &helperExit, PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true,
		DescendantsReaped: true, ReapCount: 0, ReapDigest: hex.EncodeToString(digest[:]),
	}
	if err := evidence.ValidateFor(grant, helper, anchor, processes, true); err != nil {
		t.Fatalf("validate exact helper-death run stop evidence: %v", err)
	}
	evidence.ReapDigest = strings.Repeat("0", sha256.Size*2)
	if err := evidence.ValidateFor(grant, helper, anchor, processes, true); err == nil {
		t.Fatal("anchor stop evidence accepted a digest unrelated to its complete reap set")
	}
}

func TestLinuxOwnerAnchorNativeReapRequiresCachedGateReleaseAuthorization(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	grant, err := testLinuxOwnerAnchorGrant(anchor, managerPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 30, Identity: "linux:v2:helper-native-reap-test"}
	gateProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 31, Identity: "linux:v2:gate-native-reap-test"}
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchor, Waited: true, Reaped: true,
	}
	processExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: gateProcess, Witness: anchor,
		Waited: true, Reaped: true, HelperExit: &helperExit,
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: gateProcess, HostUID: grant.OwnerUID, ObservedAt: time.Now().UTC(),
		BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: grant.Leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	reap := LinuxOwnerAnchorReapEvidence{
		Version: linuxOwnerAnchorProtocolVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf,
		Helper: helper, Process: gateProcess, ProcessRole: "native", HelperExit: helperExit, ProcessExit: processExit,
	}
	processes := []LinuxOwnerAnchorReapEvidence{reap}
	encoded, err := json.Marshal(processes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	evidence := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Anchor: anchor, Helper: helper,
		HelperExit: &helperExit, Gate: &gate, UserNamespace: &LinuxUserNamespaceIdentity{Device: 7, Inode: 9},
		PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		ReapCount: 1, ReapDigest: hex.EncodeToString(digest[:]),
	}
	if err := evidence.ValidateFor(grant, helper, anchor, processes, true); err == nil {
		t.Fatal("anchor native role reaping passed without its one-use gate-release authorization")
	}
	evidence.GateReleaseAuthorized = true
	if err := evidence.ValidateFor(grant, helper, anchor, processes, true); err != nil {
		t.Fatalf("anchor native role reaping with the exact cached release authorization: %v", err)
	}
}

func startLinuxOwnerAnchorTestProcess(t *testing.T, ctx context.Context, role string, withInput bool) *linuxOwnerAnchorTestProcess {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	command.Env = linuxOwnerAnchorTestEnvironment(role)
	command.Stderr = io.Discard
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process := &linuxOwnerAnchorTestProcess{command: command, output: bufio.NewReader(output), anchorFD: -1, helperFD: -1, targetFD: -1, foreignFD: -1}
	if withInput {
		process.input, err = command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process.stop(t)
	})
	if role == "anchor" || role == "physical-anchor" {
		process.anchorFD = openLinuxOwnerAnchorTestPIDFD(t, authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, command.Process.Pid)})
	}
	return process
}

type linuxOwnerAnchorTestProcess struct {
	command    *exec.Cmd
	input      io.WriteCloser
	output     *bufio.Reader
	anchorFD   int
	helperFD   int
	targetFD   int
	foreignFD  int
	targetFile *os.File
	targetID   authority.LinuxCgroupProcessIdentity
	waited     bool
}

func (process *linuxOwnerAnchorTestProcess) stop(t *testing.T) {
	t.Helper()
	if process.input != nil {
		_ = process.input.Close()
	}
	for _, fd := range []int{process.helperFD, process.targetFD, process.foreignFD, process.anchorFD} {
		if fd >= 0 {
			_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			_ = unix.Close(fd)
		}
	}
	if process.targetFile != nil {
		_ = unix.PidfdSendSignal(int(process.targetFile.Fd()), unix.SIGKILL, nil, 0)
		if process.targetID.Validate() == nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = linuxOwnerAnchorWaitPIDFD(cleanup, process.targetFile, process.targetID)
			cancel()
		}
		_ = process.targetFile.Close()
		process.targetFile = nil
	}
	if process.command != nil && process.command.Process != nil && !process.waited {
		_ = process.command.Process.Kill()
		_ = process.command.Wait()
		process.waited = true
	}
}

func readLinuxOwnerAnchorTestEvent(t *testing.T, reader *bufio.Reader) linuxOwnerAnchorSubprocessEvent {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read owner-anchor subprocess event: %v", err)
	}
	var event linuxOwnerAnchorSubprocessEvent
	if err := json.Unmarshal(line, &event); err != nil {
		t.Fatalf("decode owner-anchor subprocess event %q: %v", line, err)
	}
	if event.Error != "" {
		t.Fatalf("owner-anchor subprocess reported: %s", event.Error)
	}
	return event
}

func openLinuxOwnerAnchorTestPIDFD(t *testing.T, expected authority.LinuxCgroupProcessIdentity) int {
	t.Helper()
	if expected.Validate() != nil {
		t.Fatalf("test process identity is invalid: %#v", expected)
	}
	pidfd, err := unix.PidfdOpen(expected.PID, 0)
	if err != nil {
		t.Fatalf("open exact process pidfd for %d: %v", expected.PID, err)
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, expected); err != nil {
		_ = unix.Close(pidfd)
		t.Fatalf("verify exact process pidfd: %v", err)
	}
	return pidfd
}

func mustLinuxOwnerAnchorTestIdentity(t *testing.T, pid int) string {
	t.Helper()
	identity, err := ProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func linuxOwnerAnchorTestEnvironment(role string) []string {
	result := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, linuxOwnerAnchorTestRole+"=") {
			result = append(result, entry)
		}
	}
	return append(result, linuxOwnerAnchorTestRole+"="+role)
}

func runLinuxOwnerAnchorTestDaemon() error {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "daemon-ready", Anchor: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity},
	}); err != nil {
		return err
	}
	_, err = bufio.NewReader(os.Stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func runLinuxOwnerAnchorTestHelper() error {
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	command.Env = linuxOwnerAnchorTestEnvironment("target")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start test target: %w", err)
	}
	identity, err := ProcessIdentity(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return err
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return err
	}
	defer unix.Close(pidfd)
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "child-ready", Process: authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: identity},
	}); err != nil {
		_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
		_, _ = command.Process.Wait()
		return err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		select {}
	}
	if err != nil || strings.TrimSpace(line) != "exit-target" {
		return fmt.Errorf("unexpected helper test control %q: %w", line, errOr(err, errors.New("unknown command")))
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("stop test target via exact pidfd: %w", err)
	}
	if _, err := command.Process.Wait(); err != nil {
		return fmt.Errorf("wait/reap helper-owned target: %w", err)
	}
	return nil
}

func runLinuxOwnerAnchorTestProcess() error {
	return runLinuxOwnerAnchorTestProcessMode(false)
}

func runLinuxOwnerAnchorPhysicalOnlyTestProcess() error {
	return runLinuxOwnerAnchorTestProcessMode(true)
}

func runLinuxOwnerAnchorTestProcessMode(physicalOnly bool) error {
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		return err
	}
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
	if err != nil {
		return err
	}
	var helperPIDFD = -1
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
	helper.Env = linuxOwnerAnchorTestEnvironment("helper")
	helper.Stderr = io.Discard
	helper.SysProcAttr = &syscall.SysProcAttr{PidFD: &helperPIDFD}
	helperOutput, err := helper.StdoutPipe()
	if err != nil {
		return err
	}
	if err := helper.Start(); err != nil {
		return fmt.Errorf("start test helper: %w", err)
	}
	helperIdentity, err := ProcessIdentity(helper.Process.Pid)
	if err != nil || helperPIDFD < 0 {
		if helperPIDFD >= 0 {
			_ = unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0)
			_ = unix.Close(helperPIDFD)
		} else {
			_ = helper.Process.Kill()
		}
		_, _ = helper.Process.Wait()
		return fmt.Errorf("capture test helper identity and pidfd: %w", errOr(err, errors.New("pidfd unavailable")))
	}
	helperFile := os.NewFile(uintptr(helperPIDFD), "linux-owner-anchor-test-helper-pidfd")
	if helperFile == nil {
		_ = unix.Close(helperPIDFD)
		return errors.New("wrap test helper pidfd")
	}
	var targetFile *os.File
	var targetIdentity authority.LinuxCgroupProcessIdentity
	var foreignCommand *exec.Cmd
	var foreignFile *os.File
	var foreignIdentity authority.LinuxCgroupProcessIdentity
	helperWaited := false
	targetWaited := false
	defer func() {
		if !helperWaited {
			_ = unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0)
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = linuxOwnerAnchorWaitPIDFD(cleanup, helperFile, authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity})
			cancel()
			_ = helper.Process.Release()
		}
		if targetFile != nil {
			if !targetWaited {
				_ = unix.PidfdSendSignal(int(targetFile.Fd()), unix.SIGKILL, nil, 0)
				if targetIdentity.Validate() == nil {
					cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = linuxOwnerAnchorWaitPIDFD(cleanup, targetFile, targetIdentity)
					cancel()
				}
			}
			_ = targetFile.Close()
		}
		if foreignFile != nil {
			_ = unix.PidfdSendSignal(int(foreignFile.Fd()), unix.SIGKILL, nil, 0)
			if foreignIdentity.Validate() == nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = linuxOwnerAnchorWaitPIDFD(cleanup, foreignFile, foreignIdentity)
				cancel()
			}
			_ = foreignCommand.Process.Release()
			_ = foreignFile.Close()
		}
		_ = helperFile.Close()
		_ = helperOutput.Close()
	}()
	helperEventLine, err := bufio.NewReader(helperOutput).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read test helper child identity: %w", err)
	}
	var helperEvent linuxOwnerAnchorSubprocessEvent
	if err := json.Unmarshal(helperEventLine, &helperEvent); err != nil || helperEvent.Stage != "child-ready" || helperEvent.Process.Validate() != nil {
		return fmt.Errorf("decode test helper child identity: %w", errOr(err, errors.New("invalid child event")))
	}
	targetIdentity = helperEvent.Process
	targetPIDFD, err := unix.PidfdOpen(targetIdentity.PID, 0)
	if err != nil {
		return fmt.Errorf("open target pidfd: %w", err)
	}
	targetFile = os.NewFile(uintptr(targetPIDFD), "linux-owner-anchor-test-target-pidfd")
	if targetFile == nil {
		_ = unix.Close(targetPIDFD)
		return errors.New("wrap target pidfd")
	}
	if physicalOnly {
		foreignCommand = exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorProcessHelper$")
		foreignCommand.Env = linuxOwnerAnchorTestEnvironment("target")
		foreignCommand.Stdout = io.Discard
		foreignCommand.Stderr = io.Discard
		foreignCommand.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := foreignCommand.Start(); err != nil {
			return fmt.Errorf("start unrelated foreign-run test process: %w", err)
		}
		identity, err := ProcessIdentity(foreignCommand.Process.Pid)
		if err != nil {
			_ = foreignCommand.Process.Kill()
			_, _ = foreignCommand.Process.Wait()
			return fmt.Errorf("capture foreign-run test process identity: %w", err)
		}
		foreignIdentity = authority.LinuxCgroupProcessIdentity{PID: foreignCommand.Process.Pid, Identity: identity}
		pidfd, err := unix.PidfdOpen(foreignIdentity.PID, 0)
		if err != nil {
			_ = foreignCommand.Process.Kill()
			_, _ = foreignCommand.Process.Wait()
			return fmt.Errorf("open foreign-run test process pidfd: %w", err)
		}
		foreignFile = os.NewFile(uintptr(pidfd), "linux-owner-anchor-test-foreign-pidfd")
		if foreignFile == nil {
			_ = unix.Close(pidfd)
			_ = foreignCommand.Process.Kill()
			_, _ = foreignCommand.Process.Wait()
			return errors.New("wrap foreign-run test process pidfd")
		}
	}
	ownerUID, ownerGID := uint32(os.Getuid()), uint32(os.Getgid())
	if err := verifyLinuxOwnerAProcess(helper.Process.Pid, helperIdentity, ownerUID, ownerGID, os.Getpid(), managerPath); err != nil && os.Geteuid() != 0 {
		return fmt.Errorf("verify actual helper peer credentials and parent: %w", err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "ready", Anchor: anchorProcess, Helper: authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity}, Process: targetIdentity,
		ForeignProcess: foreignIdentity,
	}); err != nil {
		return err
	}
	control := bufio.NewReader(os.Stdin)
	line, err := control.ReadString('\n')
	command := strings.TrimSpace(line)
	if err != nil || command != "kill-helper" && !(physicalOnly && command == "kill-helper-physical") {
		return fmt.Errorf("unexpected anchor test command %q: %w", line, errOr(err, errors.New("expected kill-helper")))
	}
	if err := unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0); err != nil {
		return fmt.Errorf("kill exact helper via pidfd: %w", err)
	}
	helperProcess := authority.LinuxCgroupProcessIdentity{PID: helper.Process.Pid, Identity: helperIdentity}
	if err := linuxOwnerAnchorWaitPIDFD(context.Background(), helperFile, helperProcess); err != nil {
		return fmt.Errorf("wait/reap exact helper: %w", err)
	}
	helperWaited = true
	if err := helper.Process.Release(); err != nil {
		return fmt.Errorf("release Go helper handle after pidfd wait: %w", err)
	}
	credentials, err := readLinuxOwnerProcessCredentials(targetIdentity.PID)
	if err != nil || credentials.Parent != os.Getpid() {
		return fmt.Errorf("verify subreaper adopted exact target: %w", errOr(err, errors.New("target parent is not the owner anchor")))
	}
	if identity, err := ProcessIdentity(targetIdentity.PID); err != nil || identity != targetIdentity.Identity {
		return fmt.Errorf("verify adopted target creation identity: %w", errOr(err, errors.New("identity differs")))
	}
	helperExit := authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: helperProcess, Witness: anchorProcess, Waited: true, Reaped: true}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "adopted", Anchor: anchorProcess, Helper: helperProcess, Process: targetIdentity,
		ParentPID: credentials.Parent, HelperExit: &helperExit,
	}); err != nil {
		return err
	}
	if physicalOnly {
		line, err = control.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "contain-unregistered-gate" {
			return fmt.Errorf("unexpected physical-only anchor command %q: %w", line, errOr(err, errors.New("expected contain-unregistered-gate")))
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(targetFile.Fd()), targetIdentity); err != nil {
			return fmt.Errorf("verify exact unregistered gate before test containment: %w", err)
		}
		if err := unix.PidfdSendSignal(int(targetFile.Fd()), unix.SIGKILL, nil, 0); err != nil {
			return fmt.Errorf("contain exact unregistered gate by pidfd in test fixture: %w", err)
		}
		waitContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = waitLinuxOwnerAnchorPIDFDReady(waitContext, targetFile)
		cancel()
		if err != nil {
			return fmt.Errorf("observe contained physical-only gate pidfd exit: %w", err)
		}
		grant, err := testLinuxOwnerAnchorGrant(anchorProcess, managerPath)
		if err != nil {
			return fmt.Errorf("construct non-durable physical-only test run grant: %w", err)
		}
		run := &linuxOwnerAnchorRun{
			grant: grant, helper: helperProcess, helperExit: cloneLinuxOwnerAnchorExitProof(&helperExit), leaf: &LinuxCgroupV2Leaf{},
			leafKillAttempted: true, leafKillAcknowledged: true, leafEmptyObserved: true,
			children: make(map[int]linuxOwnerAnchorTrackedProcess), physicalOnlyChildren: make(map[int]linuxOwnerAnchorPhysicalOnlyChild),
			reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
		}
		classification := (&linuxOwnerAnchor{}).makeLinuxOwnerAnchorChildClassification(
			run, targetIdentity, LinuxOwnerAnchorRunKey{}, "pre_release_gate", linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly,
		)
		if err := classification.ValidateFor(grant, helperProcess, targetIdentity); err != nil {
			return fmt.Errorf("validate physical-only test origin classification: %w", err)
		}
		copyFD, err := unix.FcntlInt(targetFile.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("retain physical-only test pidfd: %w", err)
		}
		physicalPIDFD := os.NewFile(uintptr(copyFD), "linux-owner-anchor-test-physical-only-pidfd")
		if physicalPIDFD == nil {
			_ = unix.Close(copyFD)
			return errors.New("wrap physical-only test pidfd")
		}
		// This unprivileged subprocess smoke has no delegated cgroup mount or
		// installed gate image. The production classifier pins those before it
		// creates this map entry; /dev/null is only a live descriptor marker for
		// this isolated wait/reap-boundary fixture.
		imageFD, err := os.Open(os.DevNull)
		if err != nil {
			_ = physicalPIDFD.Close()
			return fmt.Errorf("open physical-only test image marker: %w", err)
		}
		run.physicalOnlyChildren[targetIdentity.PID] = linuxOwnerAnchorPhysicalOnlyChild{
			identity: targetIdentity, classification: classification, pidfd: physicalPIDFD, imageFD: imageFD,
		}
		anchorCore := &linuxOwnerAnchor{identity: anchorProcess, runs: map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun{grant.RunKey: run}}
		waitContext, cancel = context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		err = anchorCore.reapPhysicalOnlyChild(waitContext, run, targetIdentity)
		cancel()
		if err != nil {
			_ = physicalPIDFD.Close()
			_ = imageFD.Close()
			return fmt.Errorf("wait/reap physical-only test gate without RunKey receipt: %w", err)
		}
		targetWaited = true
		foreignReady, err := linuxOwnerAnchorPIDFDReady(foreignFile)
		if err != nil || foreignReady {
			_ = physicalPIDFD.Close()
			_ = imageFD.Close()
			return fmt.Errorf("foreign-run test process changed during physical-only wait: %w", errOr(err, errors.New("foreign pidfd is ready")))
		}
		foreignActual, err := ProcessIdentity(foreignIdentity.PID)
		if err != nil || foreignActual != foreignIdentity.Identity {
			_ = physicalPIDFD.Close()
			_ = imageFD.Close()
			return fmt.Errorf("foreign-run test process identity changed during physical-only wait: %w", errOr(err, errors.New("creation identity differs")))
		}
		physicalChild := run.physicalOnlyChildren[targetIdentity.PID]
		if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
			Stage: "physical-only-reaped", Anchor: anchorProcess, Helper: helperProcess, Process: targetIdentity,
			ForeignProcess: foreignIdentity, ParentPID: credentials.Parent, HelperExit: &helperExit,
			PhysicalOnlyWaited: physicalChild.waited, NoMigrationProven: run.noMigrationProven,
			RunReapCount: len(run.reaped), StopEvidenceCreated: run.stopEvidence != nil,
			ForeignProcessStillLive: true,
		}); err != nil {
			_ = physicalPIDFD.Close()
			_ = imageFD.Close()
			return err
		}
		_ = physicalPIDFD.Close()
		_ = imageFD.Close()
		line, err = control.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "finish-physical-test" {
			return fmt.Errorf("unexpected physical-only anchor completion command %q: %w", line, errOr(err, errors.New("expected finish-physical-test")))
		}
		return nil
	}
	line, err = control.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "exit-target" {
		return fmt.Errorf("unexpected anchor test command %q: %w", line, errOr(err, errors.New("expected exit-target")))
	}
	if err := unix.PidfdSendSignal(int(targetFile.Fd()), unix.SIGTERM, nil, 0); err != nil {
		return fmt.Errorf("stop adopted target via exact pidfd: %w", err)
	}
	if err := linuxOwnerAnchorWaitPIDFD(context.Background(), targetFile, targetIdentity); err != nil {
		return fmt.Errorf("wait/reap exact adopted target: %w", err)
	}
	targetWaited = true
	processExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: targetIdentity, Witness: anchorProcess,
		Waited: true, Reaped: true, HelperExit: &helperExit,
	}
	if err := json.NewEncoder(os.Stdout).Encode(linuxOwnerAnchorSubprocessEvent{
		Stage: "reaped", Anchor: anchorProcess, Helper: helperProcess, Process: targetIdentity,
		HelperExit: &helperExit, ProcessExit: &processExit,
	}); err != nil {
		return err
	}
	return nil
}

func testLinuxOwnerAnchorGrant(anchor authority.LinuxCgroupProcessIdentity, managerPath string) (LinuxOwnerAnchorGrant, error) {
	bootID := readLinuxCgroupBootID()
	if bootID == "" || anchor.Validate() != nil || !canonicalCgroupHierarchyPath(managerPath) {
		return LinuxOwnerAnchorGrant{}, errors.New("owner-anchor test grant identity is unavailable")
	}
	parent := authority.LinuxCgroupParentIdentity{BootID: bootID, MountID: 1, Device: 1, Inode: 2, Path: managerPath}
	leaf := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent,
		LeafName: strings.Repeat("a", authority.TokenBytes*2), LeafDevice: 1, LeafInode: 3,
	}
	return LinuxOwnerAnchorGrant{
		ReservationVersion: linuxOwnerAnchorReservationVersion,
		RunKey:             LinuxOwnerAnchorRunKey{RunID: "owner-anchor-run-test", Generation: 1},
		Fence:              daemonprotocol.Fence{RuntimeID: "runtime-test", RuntimeEpoch: 1, Generation: 1, ClaimID: "claim-test", LeaseToken: "lease-test"},
		LaunchToken:        strings.Repeat("b", authority.TokenBytes*2),
		Secret:             strings.Repeat("c", authority.SecretBytes*2),
		PipeToken:          strings.Repeat("d", authority.TokenBytes*2),
		JobID:              strings.Repeat("e", authority.TokenBytes*2),
		OwnerUID:           1000, LauncherUID: 0, RecoveryEndpoint: "/run/symmetry-owner/run.sock", CreateStarted: true,
		Anchor: anchor, Parent: parent, Leaf: leaf,
	}, nil
}
