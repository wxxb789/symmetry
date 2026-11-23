//go:build linux

package execution

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite(t *testing.T) {
	var calls []string
	writes := RecorderFuncs{
		PrepareSupervisorHandoff:         func(authority.SupervisorHandoff) error { calls = append(calls, "prepare"); return nil },
		BindSupervisorHandoff:            func(authority.SupervisorHandoff, int, string) error { calls = append(calls, "bind"); return nil },
		RecordSupervisorRecoveryScanSeal: func(authority.Supervisor, uint64) error { calls = append(calls, "recovery-seal"); return nil },
	}
	spec := linuxSupervisorStartSpec(exec.Command("/bin/true"), Invocation{Program: "native"}, writes)
	if spec.Command == nil {
		t.Fatal("start spec dropped the target command")
	}
	if spec.OwnerContext != "linux-helper:native" {
		t.Fatalf("start spec owner context = %q, want the invocation-derived context", spec.OwnerContext)
	}
	if spec.Prepare == nil || spec.Bind == nil || spec.RecordRecoveryScanSeal == nil {
		t.Fatal("start spec dropped a recorder write")
	}
	if err := spec.Prepare(authority.SupervisorHandoff{}); err != nil {
		t.Fatalf("spec prepare error = %v", err)
	}
	if err := spec.Bind(authority.SupervisorHandoff{}, 7, "created:7"); err != nil {
		t.Fatalf("spec bind error = %v", err)
	}
	if err := spec.RecordRecoveryScanSeal(authority.Supervisor{}, 2); err != nil {
		t.Fatalf("spec recovery seal error = %v", err)
	}
	if !slices.Equal(calls, []string{"prepare", "bind", "recovery-seal"}) {
		t.Fatalf("start spec reached %#v, want the same recorder writes", calls)
	}
	empty := linuxSupervisorStartSpec(exec.Command("/bin/true"), Invocation{}, invocationWrites(Invocation{}))
	if empty.Prepare != nil || empty.Bind != nil || empty.RecordRecoveryScanSeal != nil {
		t.Fatal("a recorder-less invocation produced launch writes")
	}
}
