package execution

import (
	"errors"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// RecorderCapabilities reports which durable writes one Recorder performs. A
// false field reproduces the legacy nil callback for that write, so a caller
// sees the branch, order and arguments the callback it replaces produced.
type RecorderCapabilities struct {
	PrepareSupervisorHandoff           bool
	BindSupervisorHandoff              bool
	CommitSupervisorHandoff            bool
	RecordSupervisorHandoffStopReceipt bool
	ClearSupervisorHandoff             bool
	PersistProcess                     bool
	PersistProcessWithAuthority        bool
	PersistProcessAuthority            bool
	PersistContainmentStopReceipt      bool
	PersistContainmentUnproven         bool
}

// SupervisorHandoffRequested is the legacy durable-handoff gate: any of the five
// handoff writes is present.
func (capabilities RecorderCapabilities) SupervisorHandoffRequested() bool {
	return capabilities.PrepareSupervisorHandoff ||
		capabilities.BindSupervisorHandoff ||
		capabilities.CommitSupervisorHandoff ||
		capabilities.RecordSupervisorHandoffStopReceipt ||
		capabilities.ClearSupervisorHandoff
}

// SupervisorHandoffComplete reports a handoff that can be prepared, bound and
// committed before a suspended target resumes or a lease arms.
func (capabilities RecorderCapabilities) SupervisorHandoffComplete() bool {
	return capabilities.PrepareSupervisorHandoff &&
		capabilities.BindSupervisorHandoff &&
		capabilities.CommitSupervisorHandoff
}

// Recorder is the one run-scoped owner of the durable writes a launch performs.
// App binds the RunKey and fence when it builds a recorder, so each method is
// exactly the mutation the callback it replaces performed.
type Recorder interface {
	Capabilities() RecorderCapabilities
	// PrepareSupervisorHandoff durably records the exact launch binding before a
	// containment helper can outlive the daemon.
	PrepareSupervisorHandoff(authority.SupervisorHandoff) error
	// BindSupervisorHandoff records the helper PID and creation identity after
	// the inherited pre-auth channel has identified the helper.
	BindSupervisorHandoff(authority.SupervisorHandoff, int, string) error
	// CommitSupervisorHandoff promotes a fully bound handoff into the normal
	// process marker and containment authority before resume or lease arm.
	CommitSupervisorHandoff(authority.SupervisorHandoff, time.Time) error
	// RecordSupervisorHandoffStopReceipt persists a stop proof for a pending
	// handoff before its helper is released.
	RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff, authority.StopReceipt) error
	// ClearSupervisorHandoff removes a pending handoff only after release proof.
	ClearSupervisorHandoff(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
	// PersistProcess runs immediately after the OS process identity is captured,
	// before output readers or the Process value are exposed.
	PersistProcess(pid int, identity string) error
	// PersistProcessWithAuthority atomically persists the process marker and its
	// independently releasable containment authority. A containment owner that
	// supports it takes this write instead of the two legacy ones, so recovery
	// cannot observe a marker without the authority required to release it.
	PersistProcessWithAuthority(pid int, identity string, value *authority.Supervisor) error
	// PersistProcessAuthority runs immediately after PersistProcess for a
	// supervisor-backed containment owner.
	PersistProcessAuthority(pid int, identity string, value *authority.Supervisor) error
	// PersistContainmentStopReceipt runs after the containment boundary proves an
	// empty tree and before an independent supervisor is released. A failure
	// retains that helper authority for retry or restart recovery.
	PersistContainmentStopReceipt(pid int, identity string, receipt authority.StopReceipt) error
	// PersistContainmentUnproven retains the exact process marker when local
	// containment cleanup cannot prove the complete owned boundary stopped.
	PersistContainmentUnproven(pid int, identity string) error
}

var errRecorderWriteUnavailable = errors.New("process recorder write is unavailable")

// RecorderFuncs is the functional form of one run's durable writes. An absent
// function is an absent capability, exactly like the legacy nil callback.
type RecorderFuncs struct {
	PrepareSupervisorHandoff           func(authority.SupervisorHandoff) error
	BindSupervisorHandoff              func(authority.SupervisorHandoff, int, string) error
	CommitSupervisorHandoff            func(authority.SupervisorHandoff, time.Time) error
	RecordSupervisorHandoffStopReceipt func(authority.SupervisorHandoff, authority.StopReceipt) error
	ClearSupervisorHandoff             func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
	PersistProcess                     func(pid int, identity string) error
	PersistProcessWithAuthority        func(pid int, identity string, value *authority.Supervisor) error
	PersistProcessAuthority            func(pid int, identity string, value *authority.Supervisor) error
	PersistContainmentStopReceipt      func(pid int, identity string, receipt authority.StopReceipt) error
	PersistContainmentUnproven         func(pid int, identity string) error
}

func (writes RecorderFuncs) Capabilities() RecorderCapabilities {
	return RecorderCapabilities{
		PrepareSupervisorHandoff:           writes.PrepareSupervisorHandoff != nil,
		BindSupervisorHandoff:              writes.BindSupervisorHandoff != nil,
		CommitSupervisorHandoff:            writes.CommitSupervisorHandoff != nil,
		RecordSupervisorHandoffStopReceipt: writes.RecordSupervisorHandoffStopReceipt != nil,
		ClearSupervisorHandoff:             writes.ClearSupervisorHandoff != nil,
		PersistProcess:                     writes.PersistProcess != nil,
		PersistProcessWithAuthority:        writes.PersistProcessWithAuthority != nil,
		PersistProcessAuthority:            writes.PersistProcessAuthority != nil,
		PersistContainmentStopReceipt:      writes.PersistContainmentStopReceipt != nil,
		PersistContainmentUnproven:         writes.PersistContainmentUnproven != nil,
	}
}

// NewRecorder adapts write functions into the port. A write with no function
// fails closed instead of panicking a daemon.
func NewRecorder(writes RecorderFuncs) Recorder {
	return recorderFuncs{writes}
}

type recorderFuncs struct {
	RecorderFuncs
}

func (recorder recorderFuncs) PrepareSupervisorHandoff(handoff authority.SupervisorHandoff) error {
	if recorder.RecorderFuncs.PrepareSupervisorHandoff == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PrepareSupervisorHandoff(handoff)
}

func (recorder recorderFuncs) BindSupervisorHandoff(handoff authority.SupervisorHandoff, pid int, identity string) error {
	if recorder.RecorderFuncs.BindSupervisorHandoff == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.BindSupervisorHandoff(handoff, pid, identity)
}

func (recorder recorderFuncs) CommitSupervisorHandoff(handoff authority.SupervisorHandoff, startedAt time.Time) error {
	if recorder.RecorderFuncs.CommitSupervisorHandoff == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.CommitSupervisorHandoff(handoff, startedAt)
}

func (recorder recorderFuncs) RecordSupervisorHandoffStopReceipt(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
	if recorder.RecorderFuncs.RecordSupervisorHandoffStopReceipt == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.RecordSupervisorHandoffStopReceipt(handoff, receipt)
}

func (recorder recorderFuncs) ClearSupervisorHandoff(handoff authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) error {
	if recorder.RecorderFuncs.ClearSupervisorHandoff == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.ClearSupervisorHandoff(handoff, proof)
}

func (recorder recorderFuncs) PersistProcess(pid int, identity string) error {
	if recorder.RecorderFuncs.PersistProcess == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PersistProcess(pid, identity)
}

func (recorder recorderFuncs) PersistProcessWithAuthority(pid int, identity string, value *authority.Supervisor) error {
	if recorder.RecorderFuncs.PersistProcessWithAuthority == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PersistProcessWithAuthority(pid, identity, value)
}

func (recorder recorderFuncs) PersistProcessAuthority(pid int, identity string, value *authority.Supervisor) error {
	if recorder.RecorderFuncs.PersistProcessAuthority == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PersistProcessAuthority(pid, identity, value)
}

func (recorder recorderFuncs) PersistContainmentStopReceipt(pid int, identity string, receipt authority.StopReceipt) error {
	if recorder.RecorderFuncs.PersistContainmentStopReceipt == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PersistContainmentStopReceipt(pid, identity, receipt)
}

func (recorder recorderFuncs) PersistContainmentUnproven(pid int, identity string) error {
	if recorder.RecorderFuncs.PersistContainmentUnproven == nil {
		return errRecorderWriteUnavailable
	}
	return recorder.RecorderFuncs.PersistContainmentUnproven(pid, identity)
}

// invocationWrites returns the durable writes one launch performs: the
// invocation's recorder when it has one, otherwise the frozen OpenCode-facing
// members, which are all a rebuilt OpenCode invocation can carry.
func invocationWrites(invocation Invocation) RecorderFuncs {
	if invocation.Recorder != nil {
		return recorderWrites(invocation.Recorder)
	}
	return RecorderFuncs{
		PrepareSupervisorHandoff:           invocation.PrepareSupervisorHandoff,
		BindSupervisorHandoff:              invocation.BindSupervisorHandoff,
		CommitSupervisorHandoff:            invocation.CommitSupervisorHandoff,
		RecordSupervisorHandoffStopReceipt: invocation.RecordSupervisorHandoffStopReceipt,
		ClearSupervisorHandoff:             invocation.ClearSupervisorHandoff,
		PersistProcess:                     invocation.PersistProcess,
		PersistProcessWithAuthority:        invocation.PersistProcessWithAuthority,
		PersistProcessAuthority:            invocation.PersistProcessAuthority,
		PersistContainmentStopReceipt:      invocation.PersistContainmentStopReceipt,
		PersistContainmentUnproven:         invocation.PersistContainmentUnproven,
	}
}

// recorderWrites reads a recorder's write functions. The shared functional
// representation is read field-wise so the runner's deferred-write plumbing
// reuses the caller's functions instead of allocating a method value per write.
func recorderWrites(recorder Recorder) RecorderFuncs {
	if writes, ok := recorder.(recorderFuncs); ok {
		return writes.RecorderFuncs
	}
	capabilities := recorder.Capabilities()
	writes := RecorderFuncs{}
	if capabilities.PrepareSupervisorHandoff {
		writes.PrepareSupervisorHandoff = recorder.PrepareSupervisorHandoff
	}
	if capabilities.BindSupervisorHandoff {
		writes.BindSupervisorHandoff = recorder.BindSupervisorHandoff
	}
	if capabilities.CommitSupervisorHandoff {
		writes.CommitSupervisorHandoff = recorder.CommitSupervisorHandoff
	}
	if capabilities.RecordSupervisorHandoffStopReceipt {
		writes.RecordSupervisorHandoffStopReceipt = recorder.RecordSupervisorHandoffStopReceipt
	}
	if capabilities.ClearSupervisorHandoff {
		writes.ClearSupervisorHandoff = recorder.ClearSupervisorHandoff
	}
	if capabilities.PersistProcess {
		writes.PersistProcess = recorder.PersistProcess
	}
	if capabilities.PersistProcessWithAuthority {
		writes.PersistProcessWithAuthority = recorder.PersistProcessWithAuthority
	}
	if capabilities.PersistProcessAuthority {
		writes.PersistProcessAuthority = recorder.PersistProcessAuthority
	}
	if capabilities.PersistContainmentStopReceipt {
		writes.PersistContainmentStopReceipt = recorder.PersistContainmentStopReceipt
	}
	if capabilities.PersistContainmentUnproven {
		writes.PersistContainmentUnproven = recorder.PersistContainmentUnproven
	}
	return writes
}

// MirrorRecorderMembers sets the frozen OpenCode-facing members to the
// recorder's writes, so a false capability stays nil like the legacy absent
// callback. daemon/internal/harness/opencode rebuilds an invocation from these
// members; W6-04 deletes them (docs/TODO.md).
func (invocation *Invocation) MirrorRecorderMembers() {
	if invocation.Recorder == nil {
		return
	}
	writes := recorderWrites(invocation.Recorder)
	invocation.PrepareSupervisorHandoff = writes.PrepareSupervisorHandoff
	invocation.BindSupervisorHandoff = writes.BindSupervisorHandoff
	invocation.CommitSupervisorHandoff = writes.CommitSupervisorHandoff
	invocation.RecordSupervisorHandoffStopReceipt = writes.RecordSupervisorHandoffStopReceipt
	invocation.ClearSupervisorHandoff = writes.ClearSupervisorHandoff
	invocation.PersistProcess = writes.PersistProcess
	invocation.PersistProcessWithAuthority = writes.PersistProcessWithAuthority
	invocation.PersistProcessAuthority = writes.PersistProcessAuthority
	invocation.PersistContainmentStopReceipt = writes.PersistContainmentStopReceipt
	invocation.PersistContainmentUnproven = writes.PersistContainmentUnproven
}
