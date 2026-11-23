package state

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestValidateJournalRequiresConsistentProcessTuple(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	base := testJournal("run-1", 1)
	cases := []struct {
		name   string
		mutate func(*RunJournal)
	}{
		{
			name: "zero PID with start time",
			mutate: func(journal *RunJournal) {
				journal.PID = 0
			},
		},
		{
			name: "zero PID with identity",
			mutate: func(journal *RunJournal) {
				journal.PID = 0
				journal.StartedAt = time.Time{}
				journal.ProcessIdentity = "identity"
			},
		},
		{
			name: "PID without start time",
			mutate: func(journal *RunJournal) {
				journal.StartedAt = time.Time{}
			},
		},
		{
			name: "PID without identity",
			mutate: func(journal *RunJournal) {
				journal.ProcessIdentity = ""
			},
		},
		{
			name: "whitespace identity",
			mutate: func(journal *RunJournal) {
				journal.ProcessIdentity = " "
			},
		},
		{
			name: "missing workspace binding key",
			mutate: func(journal *RunJournal) {
				journal.WorkspaceBindingKey = ""
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			journal := base
			test.mutate(&journal)
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("SaveJournal() succeeded")
			}
		})
	}
	if err := store.SaveJournal(base); err != nil {
		t.Fatalf("SaveJournal() rejected valid process tuple: %v", err)
	}
}

func TestJournalValidatorFirstErrorStringsArePinned(t *testing.T) {
	t.Parallel()

	t.Run("validateJournal", func(t *testing.T) {
		t.Parallel()
		launchToken := strings.Repeat("d", authority.TokenBytes*2)
		if err := pinnedJournalV2Authority(42, "windows:42:created-at").Validate(); err != nil {
			t.Fatalf("V2 authority fixture Validate() error = %v, want nil", err)
		}
		if err := pinnedJournalV2Handoff(42, "windows:42:created-at").Validate(); err != nil {
			t.Fatalf("V2 handoff fixture Validate() error = %v, want nil", err)
		}
		for _, test := range slices.Concat(
			pinnedJournalOwnershipCases(launchToken),
			pinnedJournalScanAndReleaseCases(launchToken),
			pinnedJournalRecordCases(),
		) {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				store := mustStore(t)
				key := pinnedJournalSeed(t, store, test.seed)
				_, err := store.mutateJournal(key, func(journal *RunJournal) error {
					test.mutate(journal)
					return nil
				})
				if test.want == "" {
					if err != nil {
						t.Fatalf("mutateJournal() error = %v, want nil", err)
					}
					return
				}
				if err == nil || err.Error() != test.want {
					t.Fatalf("mutateJournal() error = %v, want %q", err, test.want)
				}
			})
		}
	})

	t.Run("linuxCgroupReservation", func(t *testing.T) {
		t.Parallel()
		for name, valid := range map[string]LinuxCgroupReservation{
			"minimal":        pinnedJournalMinimalCgroupReservation(),
			"leaf bound":     pinnedJournalLeafBoundCgroupReservation(),
			"helper bound":   pinnedJournalHelperBoundCgroupReservation(),
			"launcher bound": pinnedJournalLauncherBoundCgroupReservation(),
			"gate bound":     pinnedJournalGateBoundCgroupReservation(pinnedJournalCgroupGate()),
			"aborted":        pinnedJournalAbortedCgroupReservation(),
			"pre-gate":       pinnedJournalPreGateCgroupReservation(),
		} {
			if err := valid.Validate(); err != nil {
				t.Fatalf("%s fixture Validate() error = %v, want nil", name, err)
			}
		}
		tests := []struct {
			name   string
			base   LinuxCgroupReservation
			mutate func(reservation *LinuxCgroupReservation)
			want   string
		}{
			{name: "base fields", want: "Linux cgroup reservation is invalid"},
			{
				name: "leaf identity", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.LeafIdentity = &authority.LinuxCgroupIdentity{}
				},
				want: "Linux cgroup reservation leaf identity is invalid",
			},
			{
				name: "helper identity", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					helper := pinnedJournalCgroupHelper()
					reservation.Helper = &helper
				},
				want: "Linux cgroup helper identity is invalid",
			},
			{
				name: "helper aliases anchor", base: pinnedJournalLeafBoundCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					helper := reservation.Anchor
					reservation.Helper = &helper
				},
				want: "Linux cgroup helper aliases the anchor process",
			},
			{
				name: "launcher identity", base: pinnedJournalLeafBoundCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					launcher := pinnedJournalCgroupLauncher()
					reservation.Launcher = &launcher
				},
				want: "Linux cgroup launcher identity is invalid",
			},
			{
				name: "launcher grant without launcher", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.LauncherGrantPossible = true
				},
				want: "Linux cgroup launcher grant has no bound launcher",
			},
			{
				name: "launcher aliases anchor", base: pinnedJournalHelperBoundCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					launcher := reservation.Anchor
					reservation.Launcher = &launcher
				},
				want: "Linux cgroup launcher aliases another producer",
			},
			{
				name: "launcher exit proof", base: pinnedJournalLauncherBoundCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.LauncherExit = &authority.LinuxCgroupExitProof{}
				},
				want: "Linux cgroup launcher exit proof is invalid",
			},
			{
				name: "gate identity", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.Gate = &authority.LinuxCgroupGateIdentity{}
				},
				want: "Linux cgroup gate identity is invalid",
			},
			{
				name: "gate aliases anchor", base: pinnedJournalGateBoundCgroupReservation(pinnedJournalCgroupAnchor()),
				want: "Linux cgroup gate aliases another producer",
			},
			{
				name: "helper exit proof", base: pinnedJournalHelperBoundCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.HelperExit = &authority.LinuxCgroupExitProof{}
				},
				want: "Linux cgroup helper exit proof is invalid",
			},
			{
				name: "mirror aliases anchor", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					mirror := reservation.Anchor
					reservation.Mirror = &mirror
				},
				want: "Linux cgroup mirror aliases another producer",
			},
			{
				name: "abort proof", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.AbortProof = &authority.LinuxCgroupReservationAbortProof{}
				},
				want: "Linux cgroup reservation abort proof is invalid",
			},
			{
				name: "pre-gate stop receipt", base: pinnedJournalMinimalCgroupReservation(),
				mutate: func(reservation *LinuxCgroupReservation) {
					reservation.PreGateStopReceipt = &authority.LinuxCgroupPreGateStopReceipt{}
				},
				want: "Linux pre-gate cgroup stop receipt is invalid",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				value := test.base
				if test.mutate != nil {
					test.mutate(&value)
				}
				got := value.Validate()
				if got == nil || got.Error() != test.want {
					t.Fatalf("Validate() error = %v, want %q", got, test.want)
				}
			})
		}
	})
}

type pinnedJournalMutationCase struct {
	name   string
	seed   string
	mutate func(journal *RunJournal)
	want   string
}

func pinnedJournalOwnershipCases(launchToken string) []pinnedJournalMutationCase {
	return []pinnedJournalMutationCase{
		{
			name:   "valid claim intent control",
			seed:   pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {},
		},
		{
			name:   "valid stopped grant control",
			seed:   pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {},
		},
		{
			name:   "valid running grant control",
			seed:   pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {},
		},
		{
			name: "runtime key is blank",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.RuntimeKey = ""
			},
			want: invalidJournal,
		},
		{
			name: "zero pid with process details",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ProcessIdentity = "windows:1:created-at"
			},
			want: invalidProcessDetails,
		},
		{
			name: "process pid without details",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.PID = 42
			},
			want: invalidProcessDetails,
		},
		{
			name: "containment uncertainty without evidence",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ContainmentUnproven = true
			},
			want: "run journal containment uncertainty is invalid",
		},
		{
			name: "containment authority",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = &authority.Supervisor{}
			},
			want: "run journal containment authority is invalid",
		},
		{
			name: "containment authority target pid mismatch",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = testSupervisorAuthority(43, "windows:42:created-at")
			},
			want: "run journal containment authority is invalid",
		},
		{
			name: "containment authority target identity mismatch",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = testSupervisorAuthority(42, "windows:43:created-at")
			},
			want: "run journal containment authority is invalid",
		},
		{
			name: "v2 containment authority",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = pinnedJournalV2Authority(42, "windows:42:created-at")
			},
			want: "run journal v2 containment authority is invalid",
		},
		{
			name: "v1 containment authority with v2 reservation",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = testSupervisorAuthority(42, "windows:42:created-at")
				journal.ContainmentCgroup = &LinuxCgroupReservation{}
			},
			want: "v1 containment authority cannot use a v2 cgroup reservation",
		},
		{
			name: "containment handoff",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentHandoff = &authority.SupervisorHandoff{}
			},
			want: "run journal containment handoff is invalid",
		},
		{
			name: "containment handoff with process details",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
				journal.ContainmentHandoff = &handoff
				journal.PID = 42
				journal.ProcessIdentity = "windows:42:created-at"
				journal.StartedAt = time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC)
			},
			want: "run journal containment handoff is invalid",
		},
		{
			name: "v2 containment handoff",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentHandoff = pinnedJournalV2Handoff(42, "windows:42:created-at")
			},
			want: "run journal v2 containment handoff is invalid",
		},
		{
			name: "v1 containment handoff with v2 reservation",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
				journal.ContainmentHandoff = &handoff
				journal.ContainmentCgroup = &LinuxCgroupReservation{}
			},
			want: "v1 containment handoff cannot use a v2 cgroup reservation",
		},
		{
			name: "invalid v2 cgroup reservation",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentCgroup = &LinuxCgroupReservation{}
			},
			want: "run journal v2 cgroup reservation is invalid",
		},
		{
			name: "v2 cgroup reservation without claim grant",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalMinimalCgroupReservation()
				journal.ContainmentCgroup = &cgroup
			},
			want: "run journal v2 cgroup reservation is invalid",
		},
		{
			name: "aborted cgroup reservation with active owner",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalAbortedCgroupReservation()
				journal.ContainmentCgroup = &cgroup
			},
			want: "run journal aborted cgroup reservation has active owner state",
		},
		{
			name: "pre-gate cgroup receipt with active owner",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalPreGateCgroupReservation()
				journal.ContainmentCgroup = &cgroup
			},
			want: "run journal pre-gate cgroup receipt has active owner state",
		},
		{
			name: "uncommitted cgroup reservation with process authority",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalMinimalCgroupReservation()
				journal.ContainmentCgroup = &cgroup
			},
			want: "uncommitted v2 cgroup reservation has process authority",
		},
		{
			name: "cgroup reservation with containment scans",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalMinimalCgroupReservation()
				journal.ContainmentCgroup = &cgroup
				journal.ContainmentScans = pinnedJournalScans(launchToken)
			},
			want: "run journal v2 cgroup reservation is invalid",
		},
		{
			name: "aborted cgroup reservation with containment uncertainty",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalAbortedCgroupReservation()
				journal.ContainmentCgroup = &cgroup
				journal.ContainmentUnproven = true
			},
			want: "run journal aborted cgroup reservation has active owner state",
		},
		{
			name: "pre-gate cgroup receipt with containment uncertainty",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				cgroup := pinnedJournalPreGateCgroupReservation()
				journal.ContainmentCgroup = &cgroup
				journal.ContainmentUnproven = true
			},
			want: "run journal pre-gate cgroup receipt has active owner state",
		},
	}
}

func pinnedJournalScanAndReleaseCases(launchToken string) []pinnedJournalMutationCase {
	return []pinnedJournalMutationCase{
		{
			name: "containment scan state",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ContainmentScans = &ContainmentScanJournal{Version: 0}
			},
			want: "run journal containment scan state is invalid",
		},
		{
			name: "containment scan version",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = pinnedJournalV1Authority(42, "windows:42:created-at", authority.OwnerKindLinuxHelper, "linux-helper:pinned", launchToken)
				journal.ContainmentScans = pinnedJournalScans(launchToken)
				journal.ContainmentScans.Version = 0
			},
			want: "run journal containment scan state is invalid",
		},
		{
			name: "containment scan handoff",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
				journal.ContainmentHandoff = &handoff
				journal.ContainmentScans = pinnedJournalScans(launchToken)
			},
			want: "run journal containment scan handoff is invalid",
		},
		{
			name: "containment scan authority",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = pinnedJournalV1Authority(42, "windows:42:created-at", "", "", "")
				journal.ContainmentScans = pinnedJournalScans(launchToken)
			},
			want: "run journal containment scan authority is invalid",
		},
		{
			name: "containment scan state without Linux owner",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ContainmentScans = pinnedJournalScans(launchToken)
			},
			want: "run journal containment scan state has no Linux owner",
		},
		{
			name: "containment scan failure is not sticky",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = pinnedJournalV1Authority(42, "windows:42:created-at", authority.OwnerKindLinuxHelper, "linux-helper:pinned", launchToken)
				journal.ContainmentScans = pinnedJournalScans(launchToken)
				journal.ContainmentScans.Helper = ContainmentScanProgress{CompletedSequence: 1, UnprovenSequence: 2, FailureKind: "helper_scan_failed"}
			},
			want: "run journal containment scan failure is not sticky",
		},
		{
			name: "valid Linux helper scan control",
			seed: pinnedJournalSeedRunningGrant,
			mutate: func(journal *RunJournal) {
				journal.ContainmentAuthority = pinnedJournalV1Authority(42, "windows:42:created-at", authority.OwnerKindLinuxHelper, "linux-helper:pinned", launchToken)
				journal.ContainmentScans = pinnedJournalScans(launchToken)
			},
		},
		{
			name: "v2 cgroup release proof",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ContainmentCgroupReleaseProof = &authority.SupervisorHandoffReleaseProof{}
			},
			want: "run journal v2 cgroup release proof is invalid",
		},
		{
			name: "v1 cgroup release proof",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				legacy := testStateSupervisorHandoffFixture(42, "windows:42:created-at").ReleaseProof()
				journal.ContainmentCgroupReleaseProof = &legacy
			},
			want: "run journal v2 cgroup release proof is invalid",
		},
		{
			name: "pre-gate cgroup release proof",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.ContainmentCgroupPreGateReleaseProof = &authority.LinuxCgroupPreGateReleaseProof{}
			},
			want: "run journal pre-gate cgroup release proof is invalid",
		},
	}
}

func pinnedJournalRecordCases() []pinnedJournalMutationCase {
	return []pinnedJournalMutationCase{
		{
			name: "lease token without expiry",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.LeaseExpiresAt = time.Time{}
			},
			want: invalidJournal,
		},
		{
			name: "terminal state without pending intent",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.LocalState = "terminal_pending"
			},
			want: invalidJournal,
		},
		{
			name: "claim-grant-free dropped output",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.DroppedOutputChunks = 1
			},
			want: invalidJournal,
		},
		{
			name: "invalid pending event",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.PendingEvents = []protocol.RunEvent{{}}
			},
			want: invalidJournal,
		},
		{
			name: "pending event without identity",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.PendingEvents = []protocol.RunEvent{pinnedJournalEvent("", 1)}
			},
			want: invalidJournal,
		},
		{
			name: "out-of-order pending events",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.PendingEvents = []protocol.RunEvent{
					pinnedJournalEvent("event-1", 1),
					pinnedJournalEvent("event-2", 1),
				}
			},
			want: invalidJournal,
		},
		{
			name: "pending transition with foreign fence",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.PendingTransitions = []protocol.StateTransitionRequest{{
					TransitionID: "pending-1", State: "running", Payload: json.RawMessage(`{}`),
				}}
			},
			want: invalidJournal,
		},
		{
			name: "invalid command acknowledgement",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{{CommandID: "command-1"}}
			},
			want: invalidJournal,
		},
		{
			name: "duplicate command acknowledgement CommandID",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				first := protocol.CommandAcknowledgement{
					Fence: journal.Fence(), RunID: journal.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1",
				}
				second := first
				second.AckID = "ack-2"
				journal.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{first, second}
			},
			want: invalidJournal,
		},
		{
			name: "native usage observation",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.NativeUsageObservation = &NativeUsageObservation{}
			},
			want: "native usage observation is invalid",
		},
		{
			name: "native usage terminal recovery without required flag",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.NativeUsageTerminalRecovery = &NativeUsageTerminalRecovery{State: "completed", Payload: json.RawMessage(`{}`)}
			},
			want: invalidJournal,
		},
		{
			name: "input command intent",
			seed: pinnedJournalSeedClaimIntent,
			mutate: func(journal *RunJournal) {
				journal.InputCommandIntent = &InputCommandIntent{}
			},
			want: "run journal input command intent is invalid",
		},
		{
			name: "resolved input command without acknowledgement delivery",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.InputCommandIntent = &InputCommandIntent{
					CommandID: "command-1", PayloadDigest: strings.Repeat("a", sha256.Size*2),
					RunningTransitionID: "running-1", AckID: "ack-1", Outcome: "applied",
				}
			},
			want: "input command acknowledgement delivery is invalid",
		},
		{
			name: "delivered input command acknowledgement still pending",
			seed: pinnedJournalSeedStoppedGrant,
			mutate: func(journal *RunJournal) {
				journal.InputCommandIntent = &InputCommandIntent{
					CommandID: "command-1", PayloadDigest: strings.Repeat("a", sha256.Size*2),
					RunningTransitionID: "running-1", AckID: "ack-1", Outcome: "applied", AcknowledgementDelivered: true,
				}
				journal.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{{
					Fence: journal.Fence(), RunID: journal.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1",
				}}
			},
			want: "input command acknowledgement delivery is invalid",
		},
	}
}

const (
	pinnedJournalSeedClaimIntent  = "claim intent"
	pinnedJournalSeedStoppedGrant = "stopped grant"
	pinnedJournalSeedRunningGrant = "running grant"

	invalidJournal        = "run journal is invalid"
	invalidProcessDetails = "run journal process details are invalid"
)

func pinnedJournalSeed(t *testing.T, store *Store, seed string) RunKey {
	t.Helper()
	switch seed {
	case pinnedJournalSeedClaimIntent:
		journal, err := store.SaveClaimIntent(ClaimIntent{
			Key: RunKey{RunID: "pinned-run", Generation: 1}, RuntimeKey: "local", RuntimeID: "runtime-1",
			RuntimeEpoch: 3, ClaimID: "claim-1", Work: protocol.Work{Input: json.RawMessage(`{}`)},
			WorkspaceBindingKey: "binding-1",
		})
		if err != nil {
			t.Fatalf("SaveClaimIntent() error = %v", err)
		}
		return journal.Key()
	case pinnedJournalSeedStoppedGrant:
		journal := stoppedTestJournal("pinned-run", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal(stopped) error = %v", err)
		}
		return journal.Key()
	case pinnedJournalSeedRunningGrant:
		journal := testJournal("pinned-run", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal(running) error = %v", err)
		}
		return journal.Key()
	default:
		t.Fatalf("unknown journal seed %q", seed)
		return RunKey{}
	}
}

func pinnedJournalEvent(eventID string, sequence int64) protocol.RunEvent {
	return protocol.RunEvent{
		EventID: eventID, Sequence: sequence, Kind: "output",
		OccurredAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC), Payload: json.RawMessage(`{}`),
	}
}

func pinnedJournalCgroupParent() authority.LinuxCgroupParentIdentity {
	return authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
		Path: "/system.slice/symmetry.slice/pinned",
	}
}

func pinnedJournalCgroupLeaf() authority.LinuxCgroupIdentity {
	return authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: pinnedJournalCgroupParent(),
		LeafName: strings.Repeat("e", authority.TokenBytes*2), LeafDevice: 168, LeafInode: 210,
	}
}

func pinnedJournalCgroupAnchor() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
}

func pinnedJournalCgroupHelper() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
}

func pinnedJournalCgroupLauncher() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
}

func pinnedJournalCgroupMirror() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "mirror:50:start"}
}

func pinnedJournalCgroupGate() authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"}
}

func pinnedJournalMinimalCgroupReservation() LinuxCgroupReservation {
	mirror := pinnedJournalCgroupMirror()
	return LinuxCgroupReservation{
		Version:          LinuxCgroupReservationVersion,
		LaunchToken:      strings.Repeat("a", authority.TokenBytes*2),
		Secret:           strings.Repeat("b", authority.SecretBytes*2),
		PipeToken:        strings.Repeat("c", authority.TokenBytes*2),
		JobID:            strings.Repeat("d", authority.TokenBytes*2),
		OwnerUID:         1000,
		RecoveryEndpoint: "/run/symmetry/pinned-recovery.sock",
		Parent:           pinnedJournalCgroupParent(),
		LeafName:         strings.Repeat("e", authority.TokenBytes*2),
		Anchor:           pinnedJournalCgroupAnchor(),
		Mirror:           &mirror,
	}
}

func pinnedJournalLeafBoundCgroupReservation() LinuxCgroupReservation {
	reservation := pinnedJournalMinimalCgroupReservation()
	reservation.CreateStarted = true
	leaf := pinnedJournalCgroupLeaf()
	reservation.LeafIdentity = &leaf
	return reservation
}

func pinnedJournalHelperBoundCgroupReservation() LinuxCgroupReservation {
	reservation := pinnedJournalLeafBoundCgroupReservation()
	helper := pinnedJournalCgroupHelper()
	reservation.Helper = &helper
	return reservation
}

func pinnedJournalGateBoundCgroupReservation(process authority.LinuxCgroupProcessIdentity) LinuxCgroupReservation {
	reservation := pinnedJournalLauncherBoundCgroupReservation()
	reservation.Gate = &authority.LinuxCgroupGateIdentity{
		Process: process, HostUID: reservation.OwnerUID,
		ObservedAt: time.Date(2026, 9, 26, 6, 7, 8, 0, time.UTC),
		BirthKind:  authority.LinuxCgroupTargetBirthTrustedAGate,
		Cgroup:     *reservation.LeafIdentity, BornInLeaf: true, TrustedImage: true,
	}
	return reservation
}

func pinnedJournalPreGateCgroupReservation() LinuxCgroupReservation {
	reservation := pinnedJournalLauncherBoundCgroupReservation()
	reservation.PreGateStopReceipt = pinnedJournalPreGateStopReceipt(reservation)
	return reservation
}

func pinnedJournalV1Authority(targetPID int, targetIdentity, ownerKind, ownerContext, launchToken string) *authority.Supervisor {
	value := testSupervisorAuthority(targetPID, targetIdentity)
	value.LaunchToken = launchToken
	value.OwnerKind = ownerKind
	value.OwnerContext = ownerContext
	return value
}

func pinnedJournalV2Authority(targetPID int, targetIdentity string) *authority.Supervisor {
	leaf := pinnedJournalCgroupLeaf()
	launcher, helper := pinnedJournalCgroupLauncher(), pinnedJournalCgroupHelper()
	exit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper,
		Waited: true, Reaped: true,
	}
	mirror := pinnedJournalCgroupMirror()
	return &authority.Supervisor{
		Version:        authority.SupervisorVersionV2,
		Secret:         strings.Repeat("b", authority.SecretBytes*2),
		LaunchToken:    strings.Repeat("a", authority.TokenBytes*2),
		OwnerKind:      authority.OwnerKindLinuxHelper,
		OwnerContext:   "linux-helper:pinned|endpoint=/run/symmetry/pinned.sock",
		TargetPID:      targetPID,
		TargetIdentity: targetIdentity,
		OwnerUID:       1000,
		PipeToken:      strings.Repeat("c", authority.TokenBytes*2),
		JobID:          strings.Repeat("d", authority.TokenBytes*2),
		SupervisorPID:  helper.PID, SupervisorIdentity: helper.Identity,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  &leaf,
		AnchorPID:       pinnedJournalCgroupAnchor().PID, AnchorIdentity: pinnedJournalCgroupAnchor().Identity,
		LauncherPID: launcher.PID, LauncherIdentity: launcher.Identity,
		LauncherExit: &exit, Mirror: &mirror,
	}
}

func pinnedJournalV2Handoff(targetPID int, targetIdentity string) *authority.SupervisorHandoff {
	leaf := pinnedJournalCgroupLeaf()
	launcher := pinnedJournalCgroupLauncher()
	exit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher,
		Witness: authority.LinuxCgroupProcessIdentity{PID: 60, Identity: "witness:60:start"},
		Waited:  true, Reaped: true,
	}
	return &authority.SupervisorHandoff{
		Version:         authority.SupervisorHandoffVersionV2,
		Secret:          strings.Repeat("b", authority.SecretBytes*2),
		LaunchToken:     strings.Repeat("a", authority.TokenBytes*2),
		OwnerKind:       authority.OwnerKindLinuxHelper,
		OwnerContext:    "linux-helper:pinned|endpoint=/run/symmetry/pinned.sock",
		TargetPID:       targetPID,
		TargetIdentity:  targetIdentity,
		OwnerUID:        1000,
		PipeToken:       strings.Repeat("c", authority.TokenBytes*2),
		JobID:           strings.Repeat("d", authority.TokenBytes*2),
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  &leaf,
		AnchorPID:       pinnedJournalCgroupAnchor().PID, AnchorIdentity: pinnedJournalCgroupAnchor().Identity,
		LauncherPID: launcher.PID, LauncherIdentity: launcher.Identity,
		LauncherExit: &exit,
	}
}

func pinnedJournalScans(launchToken string) *ContainmentScanJournal {
	return &ContainmentScanJournal{
		Version:            1,
		LaunchToken:        launchToken,
		TargetPID:          42,
		TargetIdentity:     "windows:42:created-at",
		SupervisorPID:      99,
		SupervisorIdentity: "windows:99:created-at",
		MirrorIdentity:     strings.Repeat("f", authority.TokenBytes*2),
	}
}
