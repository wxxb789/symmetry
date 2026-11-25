//go:build linux && amd64

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func linuxV2BoundWitnessOwner(pid int) *linuxV2OwnerWitnessProcess {
	return &linuxV2OwnerWitnessProcess{PID: pid, Identity: fmt.Sprintf("creation-identity-%d", pid)}
}

func linuxV2UnboundWitnessOwner() *linuxV2OwnerWitnessProcess {
	return &linuxV2OwnerWitnessProcess{}
}

func linuxV2WitnessOwners(anchor, helper, launcher, gate *linuxV2OwnerWitnessProcess) linuxV2OwnerWitnessOwners {
	return linuxV2OwnerWitnessOwners{Anchor: anchor, Helper: helper, Launcher: launcher, Gate: gate}
}

func linuxV2JournalOwner(pid int) authority.LinuxCgroupProcessIdentity {
	return authority.LinuxCgroupProcessIdentity{PID: pid, Identity: fmt.Sprintf("creation-identity-%d", pid)}
}

func linuxV2OwnerBindingJournal(anchor, helper, launcher, gate int) state.RunJournal {
	reservation := &state.LinuxCgroupReservation{Anchor: linuxV2JournalOwner(anchor)}
	if helper > 0 {
		owner := linuxV2JournalOwner(helper)
		reservation.Helper = &owner
	}
	if launcher > 0 {
		owner := linuxV2JournalOwner(launcher)
		reservation.Launcher = &owner
	}
	if gate > 0 {
		reservation.Gate = &authority.LinuxCgroupGateIdentity{Process: linuxV2JournalOwner(gate)}
	}
	return state.RunJournal{ContainmentCgroup: reservation}
}

func TestLinuxV2RequireSameOwnerIdentities(t *testing.T) {
	sharedIdentityA := &linuxV2OwnerWitnessProcess{PID: 4201, Identity: "creation-identity-shared"}
	sharedIdentityB := &linuxV2OwnerWitnessProcess{PID: 4202, Identity: "creation-identity-shared"}
	cases := []struct {
		name     string
		expected linuxV2OwnerWitnessOwners
		actual   linuxV2OwnerWitnessOwners
		journal  state.RunJournal
		wantErr  string
	}{
		{
			name:     "unbound placeholder pairs pass",
			expected: linuxV2WitnessOwners(linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner()),
			actual:   linuxV2WitnessOwners(linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner(), linuxV2UnboundWitnessOwner()),
			journal:  state.RunJournal{},
		},
		{
			name:     "pre-gate rehearsal passes with one unbound slot",
			expected: linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4202), linuxV2BoundWitnessOwner(4203), linuxV2UnboundWitnessOwner()),
			actual:   linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4202), linuxV2BoundWitnessOwner(4203), linuxV2UnboundWitnessOwner()),
			journal:  linuxV2OwnerBindingJournal(4201, 4202, 4203, 0),
		},
		{
			name:     "a journal-bound slot placeholder pair fails",
			expected: linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4202), linuxV2BoundWitnessOwner(4203), linuxV2UnboundWitnessOwner()),
			actual:   linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4202), linuxV2BoundWitnessOwner(4203), linuxV2UnboundWitnessOwner()),
			journal:  linuxV2OwnerBindingJournal(4201, 4202, 4203, 4204),
			wantErr:  "incomplete process identity",
		},
		{
			name:     "a bound marker owner replayed unbound fails",
			expected: linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), nil, nil, nil),
			actual:   linuxV2WitnessOwners(linuxV2UnboundWitnessOwner(), nil, nil, nil),
			journal:  linuxV2OwnerBindingJournal(4201, 0, 0, 0),
			wantErr:  "changed exact anchor PID/creation identity",
		},
		{
			name:     "an unbound marker owner replayed bound fails",
			expected: linuxV2WitnessOwners(linuxV2UnboundWitnessOwner(), nil, nil, nil),
			actual:   linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), nil, nil, nil),
			journal:  linuxV2OwnerBindingJournal(4201, 0, 0, 0),
			wantErr:  "changed exact anchor PID/creation identity",
		},
		{
			name:     "a bound marker owner replayed with another identity fails",
			expected: linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), nil, nil, nil),
			actual:   linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4202), nil, nil, nil),
			journal:  linuxV2OwnerBindingJournal(4201, 0, 0, 0),
			wantErr:  "changed exact anchor PID/creation identity",
		},
		{
			name:     "a duplicated replay PID fails",
			expected: linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4201), nil, nil),
			actual:   linuxV2WitnessOwners(linuxV2BoundWitnessOwner(4201), linuxV2BoundWitnessOwner(4201), nil, nil),
			journal:  linuxV2OwnerBindingJournal(4201, 4201, 0, 0),
			wantErr:  "duplicates a numeric PID",
		},
		{
			name:     "a duplicated replay identity fails",
			expected: linuxV2WitnessOwners(sharedIdentityA, sharedIdentityB, nil, nil),
			actual:   linuxV2WitnessOwners(sharedIdentityA, sharedIdentityB, nil, nil),
			journal:  linuxV2OwnerBindingJournal(4201, 4202, 0, 0),
			wantErr:  "duplicates a creation identity",
		},
		{
			name:     "an unbound replay slot without a marker slot fails",
			expected: linuxV2OwnerWitnessOwners{},
			actual:   linuxV2WitnessOwners(linuxV2UnboundWitnessOwner(), nil, nil, nil),
			journal:  state.RunJournal{},
			wantErr:  "incomplete process identity",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := linuxV2RequireSameOwnerIdentities(testCase.expected, testCase.actual, testCase.journal)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("owner identity comparison error = %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("owner identity comparison error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}
