package harness

import (
	"slices"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

func TestPopulateOpencodeRecorderMembersReachesTheSameRecorderMethod(t *testing.T) {
	var calls []string
	writes := execution.RecorderFuncs{
		PrepareSupervisorHandoff: func(authority.SupervisorHandoff) error {
			calls = append(calls, "PrepareSupervisorHandoff")
			return nil
		},
		BindSupervisorHandoff: func(authority.SupervisorHandoff, int, string) error {
			calls = append(calls, "BindSupervisorHandoff")
			return nil
		},
		CommitSupervisorHandoff: func(authority.SupervisorHandoff, time.Time) error {
			calls = append(calls, "CommitSupervisorHandoff")
			return nil
		},
		RecordSupervisorHandoffStopReceipt: func(authority.SupervisorHandoff, authority.StopReceipt) error {
			calls = append(calls, "RecordSupervisorHandoffStopReceipt")
			return nil
		},
		ClearSupervisorHandoff: func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error {
			calls = append(calls, "ClearSupervisorHandoff")
			return nil
		},
		PersistProcess: func(int, string) error {
			calls = append(calls, "PersistProcess")
			return nil
		},
		PersistProcessWithAuthority: func(int, string, *authority.Supervisor) error {
			calls = append(calls, "PersistProcessWithAuthority")
			return nil
		},
		PersistProcessAuthority: func(int, string, *authority.Supervisor) error {
			calls = append(calls, "PersistProcessAuthority")
			return nil
		},
		PersistContainmentStopReceipt: func(int, string, authority.StopReceipt) error {
			calls = append(calls, "PersistContainmentStopReceipt")
			return nil
		},
		PersistContainmentUnproven: func(int, string) error {
			calls = append(calls, "PersistContainmentUnproven")
			return nil
		},
	}
	request := StartRequest{Invocation: execution.Invocation{Recorder: execution.NewRecorder(writes)}}
	request = PopulateOpencodeRecorderMembers(request)
	if request.Invocation.Recorder == nil {
		t.Fatal("populate dropped the recorder")
	}
	members := []struct {
		name string
		call func() error
	}{
		{"PrepareSupervisorHandoff", func() error {
			return request.Invocation.PrepareSupervisorHandoff(authority.SupervisorHandoff{})
		}},
		{"BindSupervisorHandoff", func() error {
			return request.Invocation.BindSupervisorHandoff(authority.SupervisorHandoff{}, 7, "bound:7")
		}},
		{"CommitSupervisorHandoff", func() error {
			return request.Invocation.CommitSupervisorHandoff(authority.SupervisorHandoff{}, time.Unix(1, 0))
		}},
		{"RecordSupervisorHandoffStopReceipt", func() error {
			return request.Invocation.RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff{}, authority.StopReceipt{})
		}},
		{"ClearSupervisorHandoff", func() error {
			return request.Invocation.ClearSupervisorHandoff(authority.SupervisorHandoff{}, authority.SupervisorHandoffReleaseProof{})
		}},
		{"PersistProcessWithAuthority", func() error {
			return request.Invocation.PersistProcessWithAuthority(7, "created:7", nil)
		}},
		{"PersistContainmentUnproven", func() error {
			return request.Invocation.PersistContainmentUnproven(7, "created:7")
		}},
		{"PersistProcess", func() error { return request.PersistProcess(7, "created:7") }},
		{"PersistProcessAuthority", func() error { return request.PersistProcessAuthority(7, "created:7", nil) }},
		{"PersistContainmentStopReceipt", func() error {
			return request.PersistContainmentStopReceipt(7, "created:7", authority.StopReceipt{})
		}},
	}
	for _, member := range members {
		calls = nil
		if err := member.call(); err != nil {
			t.Fatalf("frozen member %s error = %v", member.name, err)
		}
		if !slices.Equal(calls, []string{member.name}) {
			t.Fatalf("frozen member %s reached %#v, want the same recorder method", member.name, calls)
		}
	}
}

func TestPopulateOpencodeRecorderMembersWithoutARecorderChangesNothing(t *testing.T) {
	request := StartRequest{Invocation: execution.Invocation{Program: "configured-opencode"}}
	populated := PopulateOpencodeRecorderMembers(request)
	if populated.Invocation.Program != request.Invocation.Program || populated.Invocation.Recorder != nil ||
		populated.Invocation.PersistProcess != nil || populated.Invocation.PersistProcessAuthority != nil ||
		populated.Invocation.PersistContainmentStopReceipt != nil ||
		populated.PersistProcess != nil || populated.PersistProcessAuthority != nil || populated.PersistContainmentStopReceipt != nil {
		t.Fatalf("populate without a recorder changed the request: %+v", populated)
	}
}
