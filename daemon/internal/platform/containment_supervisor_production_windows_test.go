//go:build windows

package platform

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse
// exercises the non-test daemon binary and its hidden supervisor dispatch. It
// is opt-in because it builds a production binary and owns a real Windows Job.
func TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse(t *testing.T) {
	if os.Getenv("SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS") != "1" {
		t.Skip("set SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS=1 to run the production supervisor witness")
	}

	productionBinary := buildProductionDaemonBinary(t)
	previousExecutable := os.Args[0]
	os.Args[0] = productionBinary
	t.Cleanup(func() { os.Args[0] = previousExecutable })

	target := exec.Command("cmd", "/c", "ping", "-t", "127.0.0.1")
	if err := ConfigureHeadlessProcess(target); err != nil {
		t.Fatal(err)
	}
	if err := target.Start(); err != nil {
		t.Fatalf("start target: %v", err)
	}

	var containment Containment
	var targetIdentity string
	var supervisor *processContainmentSupervisorLease
	released := false
	t.Cleanup(func() {
		if !released && supervisor != nil && supervisor.waiter != nil {
			_ = supervisor.waiter.killAndWait(time.Now().Add(2 * time.Second))
		}
		if target.Process != nil {
			_ = target.Process.Kill()
		}
		_ = target.Wait()
		if containment != nil && !released {
			_ = containment.Close()
		}
	})

	var err error
	containment, targetIdentity, err = AttachProcess(target.Process)
	if err != nil {
		t.Fatalf("AttachProcess() error = %v", err)
	}
	job, ok := containment.(*jobContainment)
	if !ok || job.supervisor == nil {
		t.Fatalf("AttachProcess() containment = %#v, want supervisor-backed job", containment)
	}
	supervisor, ok = job.supervisor.(*processContainmentSupervisorLease)
	if !ok {
		t.Fatalf("job supervisor = %T, want processContainmentSupervisorLease", job.supervisor)
	}
	authority := job.ContainmentAuthority()
	if authority == nil {
		t.Fatal("production supervisor did not expose durable authority")
	}

	// Simulate daemon loss: close the inherited owner writer and the daemon's
	// Job handle. The production helper owns the duplicate Job handle and must
	// retain the stopped witness for a restarted daemon.
	supervisor.closeOwnerWriterLocked()
	job.mutex.Lock()
	daemonJob := job.handle
	job.handle = 0
	job.mutex.Unlock()
	if daemonJob != 0 {
		if err := closeJob(daemonJob); err != nil {
			t.Fatalf("close daemon Job handle: %v", err)
		}
	}

	previousRequest := requestContainmentSupervisor
	firstRecover := true
	requestContainmentSupervisor = func(endpoint containmentSupervisorEndpoint, operation string, deadline time.Time) (containmentSupervisorResponse, error) {
		response, err := requestContainmentSupervisorPipe(endpoint, operation, deadline)
		if operation == "recover" && firstRecover {
			firstRecover = false
			return containmentSupervisorResponse{}, errors.New("simulated lost recover response")
		}
		return response, err
	}
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })

	if _, err := RecoverPersistedContainmentWithAuthority(target.Process.Pid, targetIdentity, authority); !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("first recover error = %v, want lost-response recovery failure", err)
	}

	requestContainmentSupervisor = previousRequest
	receipt, err := RecoverPersistedContainmentWithAuthority(target.Process.Pid, targetIdentity, authority)
	if err != nil {
		t.Fatalf("replayed recover error = %v", err)
	}
	if !receipt.ValidFor(*authority) {
		t.Fatalf("replayed receipt = %#v, want authority-bound stop proof", receipt)
	}
	authority.StopReceipt = &receipt
	if err := ReleasePersistedContainmentWithAuthority(target.Process.Pid, targetIdentity, authority); err != nil {
		t.Fatalf("authenticated release error = %v", err)
	}
	released = true
	if err := target.Wait(); err != nil {
		// ping is expected to be terminated by the Job; any non-nil exit is
		// acceptable as long as the process has been reaped.
		_ = err
	}
}

func buildProductionDaemonBinary(t *testing.T) string {
	t.Helper()
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve production witness source path")
	}
	daemonRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", ".."))
	output := filepath.Join(t.TempDir(), "symmetry-daemon-production-witness.exe")
	command := exec.Command("go", "build", "-o", output, "./cmd/symmetry-daemon")
	command.Dir = daemonRoot
	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("configure production build: %v", err)
	}
	var outputBuffer bytes.Buffer
	command.Stdout = &outputBuffer
	command.Stderr = &outputBuffer
	if err := command.Run(); err != nil {
		t.Fatalf("build production daemon: %v\n%s", err, strings.TrimSpace(outputBuffer.String()))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("production daemon binary missing: %v", err)
	}
	return output
}
