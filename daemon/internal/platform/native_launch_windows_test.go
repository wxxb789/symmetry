//go:build windows

package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const nativeLaunchHelperEnvironment = "SYMMETRY_NATIVE_LAUNCH_HELPER"

type nativeJobBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func TestPrepareNativeLaunchUsesExplicitCommandLineAndCwd(t *testing.T) {
	application, commandLine, cwd, environment, err := prepareNativeLaunch(NativeLaunchSpec{
		Application: `C:\Program Files\native.exe`,
		CommandLine: `native.exe "quoted value"`,
		Cwd:         `C:\work`,
		Env:         []string{"z=last", "A=first"},
	})
	if err != nil {
		t.Fatalf("prepareNativeLaunch() error = %v", err)
	}
	if got := windows.UTF16PtrToString(application); got != `C:\Program Files\native.exe` {
		t.Fatalf("application = %q", got)
	}
	if got := windows.UTF16PtrToString(commandLine); got != `native.exe "quoted value"` {
		t.Fatalf("command line = %q", got)
	}
	if got := windows.UTF16PtrToString(cwd); got != `C:\work` {
		t.Fatalf("cwd = %q", got)
	}
	if environment == nil {
		t.Fatal("environment block pointer is nil for explicit environment")
	}
	values, err := nativeEnvironmentBlockValues([]string{"z=last", "A=first"})
	if err != nil {
		t.Fatalf("nativeEnvironmentBlockValues() error = %v", err)
	}
	if got := readNativeEnvironmentValues(values); strings.Join(got, ",") != "A=first,z=last" {
		t.Fatalf("environment block = %#v", got)
	}
}

func TestPrepareNativeLaunchUsesArgvAndInheritsNilEnvironment(t *testing.T) {
	application, commandLine, cwd, environment, err := prepareNativeLaunch(NativeLaunchSpec{
		Argv: []string{`C:\native.exe`, `a b`, `quote"value`},
	})
	if err != nil {
		t.Fatalf("prepareNativeLaunch() error = %v", err)
	}
	if application != nil || cwd != nil || environment != nil {
		t.Fatalf("prepared pointers = application:%v cwd:%v env:%v, want nil", application, cwd, environment)
	}
	if got, want := windows.UTF16PtrToString(commandLine), windows.ComposeCommandLine([]string{`C:\native.exe`, `a b`, `quote"value`}); got != want {
		t.Fatalf("command line = %q, want %q", got, want)
	}
}

func TestPrepareNativeLaunchRejectsAmbiguousOrUnsafeInput(t *testing.T) {
	tests := []struct {
		name string
		spec NativeLaunchSpec
		want string
	}{
		{name: "missing command", want: "application or argv is required"},
		{name: "command line and argv", spec: NativeLaunchSpec{CommandLine: "native.exe", Argv: []string{"native.exe"}}, want: "mutually exclusive"},
		{name: "application nul", spec: NativeLaunchSpec{Application: "native\x00.exe"}, want: "application"},
		{name: "command line nul", spec: NativeLaunchSpec{CommandLine: "native\x00.exe"}, want: "command line"},
		{name: "cwd nul", spec: NativeLaunchSpec{Application: "native.exe", Cwd: "C:\x00work"}, want: "working directory"},
		{name: "environment nul", spec: NativeLaunchSpec{Application: "native.exe", Env: []string{"BAD=\x00VALUE"}}, want: "environment contains NUL"},
		{name: "environment empty entry", spec: NativeLaunchSpec{Application: "native.exe", Env: []string{"", "A=first"}}, want: "environment contains an empty entry"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, _, err := prepareNativeLaunch(test.spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("prepareNativeLaunch() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestNativeEnvironmentBlockUsesDoubleNulForEmptyExplicitEnvironment(t *testing.T) {
	block, err := nativeEnvironmentBlockValues([]string{})
	if err != nil {
		t.Fatalf("nativeEnvironmentBlockValues() error = %v", err)
	}
	if got := readNativeEnvironmentValues(block); len(got) != 0 {
		t.Fatalf("empty environment entries = %#v, want none", got)
	}
	if len(block) != 2 || block[0] != 0 || block[1] != 0 {
		t.Fatalf("empty environment terminator = %#v, want [0 0]", block)
	}
}

func TestNativeLaunchRejectsPartialStandardHandles(t *testing.T) {
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdin) error = %v", err)
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()

	_, _, err = prepareNativeStandardHandles(NativeLaunchSpec{Stdin: stdinRead})
	if err == nil || !strings.Contains(err.Error(), "all set or all nil") {
		t.Fatalf("prepareNativeStandardHandles() error = %v, want partial-handle rejection", err)
	}
}

func TestPrepareNativeStandardHandlesRestoresSharedHandleOnce(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer read.Close()
	defer write.Close()

	handle := windows.Handle(read.Fd())
	original, err := nativeHandleInformation(handle)
	if err != nil {
		t.Fatalf("nativeHandleInformation() before launch error = %v", err)
	}
	handles, restore, err := prepareNativeStandardHandles(NativeLaunchSpec{
		Stdin:  read,
		Stdout: read,
		Stderr: read,
	})
	if err != nil {
		t.Fatalf("prepareNativeStandardHandles() error = %v", err)
	}
	if len(handles) != 3 || handles[0] == handle || handles[1] != handles[0] || handles[2] != handles[0] {
		t.Fatalf("standard handles = %#v, want three references to one duplicated handle distinct from %#x", handles, handle)
	}
	restore()
	got, err := nativeHandleInformation(handle)
	if err != nil {
		t.Fatalf("nativeHandleInformation() after restore error = %v", err)
	}
	if got != original {
		t.Fatalf("shared handle flags = %#x, want original %#x", got, original)
	}
}

func TestLaunchSuspendedOwnsConfiguredJobAndHandles(t *testing.T) {
	process := launchNativeTestProcess(t, "exit", "0")
	t.Cleanup(func() { _ = process.Close() })

	if process.PID() == 0 || process.process == 0 || process.thread == 0 || process.job == 0 {
		t.Fatalf("owned state = pid:%d process:%d thread:%d job:%d", process.PID(), process.process, process.thread, process.job)
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var returned uint32
	if err := windows.QueryInformationJobObject(
		process.job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
		&returned,
	); err != nil {
		t.Fatalf("QueryInformationJobObject() error = %v", err)
	}
	if limits.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
		t.Fatalf("job limit flags = %#x, want KILL_ON_JOB_CLOSE", limits.BasicLimitInformation.LimitFlags)
	}
	var accounting nativeJobBasicAccountingInformation
	if err := windows.QueryInformationJobObject(
		process.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&accounting)),
		uint32(unsafe.Sizeof(accounting)),
		&returned,
	); err != nil {
		t.Fatalf("QueryInformationJobObject(accounting) error = %v", err)
	}
	if accounting.ActiveProcesses != 1 {
		t.Fatalf("active processes before Resume = %d, want 1", accounting.ActiveProcesses)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := process.WaitContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitContext() before Resume error = %v, want deadline", err)
	}
	if err := process.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := process.Resume(); !errors.Is(err, errNativeLaunchResumed) {
		t.Fatalf("repeated Resume() error = %v, want %v", err, errNativeLaunchResumed)
	}
	if code, err := process.Wait(); err != nil || code != 0 {
		t.Fatalf("Wait() = (%d, %v), want (0, nil)", code, err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if process.process != 0 || process.thread != 0 || process.job != 0 {
		t.Fatalf("closed handles = process:%d thread:%d job:%d, want zero", process.process, process.thread, process.job)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
}

func TestAttachSuspendedProcessTransfersJobOwnershipToContainment(t *testing.T) {
	process := launchNativeTestProcess(t, "block")
	containment, identity, err := AttachSuspendedProcess(process)
	if err != nil {
		_ = process.Close()
		t.Fatalf("AttachSuspendedProcess() error = %v", err)
	}
	if containment == nil || identity == "" {
		_ = process.Close()
		t.Fatalf("AttachSuspendedProcess() = (%#v, %q), want containment and identity", containment, identity)
	}
	job, ok := containment.(*jobContainment)
	if !ok {
		_ = process.Close()
		t.Fatalf("containment type = %T, want *jobContainment", containment)
	}
	process.mu.Lock()
	nativeJob := process.job
	process.mu.Unlock()
	if nativeJob != 0 || job.handle == 0 {
		_ = containment.Close()
		_ = process.Close()
		t.Fatalf("Job ownership = native:%d containment:%d, want native:0 and containment non-zero", nativeJob, job.handle)
	}

	if err := containment.Terminate(true); err != nil {
		t.Fatalf("containment.Terminate(true) error = %v", err)
	}
	if _, err := process.Wait(); err != nil {
		t.Fatalf("suspended process Wait() error = %v", err)
	}
	if err := containment.Close(); err != nil {
		t.Fatalf("containment.Close() error = %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("process.Close() after transferred Job error = %v", err)
	}
}

func TestSuspendedProcessTerminateAndClosePreventOrphans(t *testing.T) {
	process := launchNativeTestProcess(t, "block")
	if err := process.Terminate(37); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if code, err := process.Wait(); err != nil || code != 37 {
		t.Fatalf("Wait() after Terminate = (%d, %v), want (37, nil)", code, err)
	}
	if err := process.Terminate(38); err != nil {
		t.Fatalf("repeated Terminate() error = %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close() after Terminate error = %v", err)
	}

	process = launchNativeTestProcess(t, "block")
	if err := process.Close(); err != nil {
		t.Fatalf("Close() on suspended process error = %v", err)
	}
	if process.process != 0 || process.thread != 0 || process.job != 0 {
		t.Fatalf("closed suspended process handles = process:%d thread:%d job:%d, want zero", process.process, process.thread, process.job)
	}
}

func TestSuspendedProcessWaitAndCloseDoNotRaceHandleRelease(t *testing.T) {
	process := launchNativeTestProcess(t, "block")
	waitResult := make(chan struct {
		code uint32
		err  error
	}, 1)
	go func() {
		code, err := process.Wait()
		waitResult <- struct {
			code uint32
			err  error
		}{code: code, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		process.mu.Lock()
		waiters := process.waiters
		process.mu.Unlock()
		if waiters == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Wait() did not register as an active waiter")
		}
		runtime.Gosched()
	}

	if err := process.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case result := <-waitResult:
		if result.err != nil {
			t.Fatalf("concurrent Wait() error = %v", result.err)
		}
		if result.code != 1 {
			t.Fatalf("concurrent Wait() exit code = %d, want 1", result.code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Wait() did not finish after Close()")
	}
}

func TestLaunchSuspendedPassesExplicitEnvironmentAndCwd(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "native-launch-result.txt")
	process := launchNativeTestProcessWithSpec(t, NativeLaunchSpec{
		Application: os.Args[0],
		Argv: []string{
			os.Args[0],
			"-test.run=^TestNativeLaunchHelper$",
			"--",
			"inspect",
		},
		Env: []string{
			nativeLaunchHelperEnvironment + "=1",
			"SYMMETRY_NATIVE_LAUNCH_VALUE=explicit",
			"SYMMETRY_NATIVE_LAUNCH_OUTPUT=" + output,
		},
		Cwd:      directory,
		Headless: true,
	})
	if err := process.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if code, err := process.Wait(); err != nil || code != 0 {
		t.Fatalf("Wait() = (%d, %v), want (0, nil)", code, err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read helper output: %v", err)
	}
	parts := strings.SplitN(string(data), "\n", 2)
	if len(parts) != 2 || parts[0] != "explicit" || !strings.EqualFold(filepath.Clean(parts[1]), filepath.Clean(directory)) {
		t.Fatalf("helper output = %q, want explicit value and cwd %q", data, directory)
	}
}

func TestLaunchSuspendedPassesExplicitStandardHandles(t *testing.T) {
	const input = "native-stdio"

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdin) error = %v", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("os.Pipe(stdout) error = %v", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatalf("os.Pipe(stderr) error = %v", err)
	}
	defer stdinWrite.Close()
	defer stdoutRead.Close()
	defer stderrRead.Close()

	childHandles := map[string]*os.File{
		"stdin":  stdinRead,
		"stdout": stdoutWrite,
		"stderr": stderrWrite,
	}
	originalFlags := make(map[string]uint32, len(childHandles))
	for name, file := range childHandles {
		flags, err := nativeHandleInformation(windows.Handle(file.Fd()))
		if err != nil {
			t.Fatalf("GetHandleInformation(%s) before launch error = %v", name, err)
		}
		originalFlags[name] = flags
	}

	process := launchNativeTestProcessWithSpec(t, NativeLaunchSpec{
		Application: os.Args[0],
		Argv: []string{
			os.Args[0],
			"-test.run=^TestNativeLaunchHelper$",
			"--",
			"stdio",
		},
		Env:      append(os.Environ(), nativeLaunchHelperEnvironment+"=1"),
		Headless: true,
		Stdin:    stdinRead,
		Stdout:   stdoutWrite,
		Stderr:   stderrWrite,
	})
	defer process.Close()

	for name, file := range childHandles {
		flags, err := nativeHandleInformation(windows.Handle(file.Fd()))
		if err != nil {
			t.Fatalf("GetHandleInformation(%s) error = %v", name, err)
		}
		if flags != originalFlags[name] {
			t.Fatalf("%s parent handle flags = %#x, want original %#x", name, flags, originalFlags[name])
		}
	}
	stdinRead.Close()
	stdoutWrite.Close()
	stderrWrite.Close()

	if err := process.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if _, err := io.WriteString(stdinWrite, input); err != nil {
		t.Fatalf("write child stdin: %v", err)
	}
	if err := stdinWrite.Close(); err != nil {
		t.Fatalf("close child stdin: %v", err)
	}
	stdout, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatalf("read child stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatalf("read child stderr: %v", err)
	}
	if code, err := process.Wait(); err != nil || code != 0 {
		t.Fatalf("Wait() = (%d, %v), want (0, nil)", code, err)
	}
	if got, want := string(stdout), "stdout="+input; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := string(stderr), "stderr="+input; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func launchNativeTestProcess(t *testing.T, mode string, args ...string) *SuspendedProcess {
	t.Helper()
	return launchNativeTestProcessWithSpec(t, NativeLaunchSpec{
		Application: os.Args[0],
		Argv: append([]string{
			os.Args[0],
			"-test.run=^TestNativeLaunchHelper$",
			"--",
			mode,
		}, args...),
		Env:      append(os.Environ(), nativeLaunchHelperEnvironment+"=1"),
		Cwd:      os.TempDir(),
		Headless: true,
	})
}

func launchNativeTestProcessWithSpec(t *testing.T, spec NativeLaunchSpec) *SuspendedProcess {
	t.Helper()
	process, err := LaunchSuspended(spec)
	if err != nil {
		t.Fatalf("LaunchSuspended() error = %v", err)
	}
	return process
}

func readNativeEnvironmentValues(values []uint16) []string {
	if len(values) == 0 {
		return nil
	}
	entries := make([]string, 0)
	start := 0
	for index, value := range values {
		if value != 0 {
			continue
		}
		if index == start {
			return entries
		}
		entries = append(entries, windows.UTF16ToString(values[start:index]))
		start = index + 1
	}
	return entries
}

func TestNativeLaunchHelper(t *testing.T) {
	if os.Getenv(nativeLaunchHelperEnvironment) != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator == -1 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}

	switch os.Args[separator+1] {
	case "exit":
		code, err := strconv.Atoi(os.Args[separator+2])
		if err != nil {
			os.Exit(3)
		}
		os.Exit(code)
	case "block":
		select {}
	case "inspect":
		output := os.Getenv("SYMMETRY_NATIVE_LAUNCH_OUTPUT")
		value := os.Getenv("SYMMETRY_NATIVE_LAUNCH_VALUE")
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(4)
		}
		if err := os.WriteFile(output, []byte(value+"\n"+cwd), 0o600); err != nil {
			os.Exit(5)
		}
		os.Exit(0)
	case "stdio":
		input := make([]byte, len("native-stdio"))
		if _, err := io.ReadFull(os.Stdin, input); err != nil {
			os.Exit(7)
		}
		if _, err := io.WriteString(os.Stdout, "stdout="+string(input)); err != nil {
			os.Exit(8)
		}
		if _, err := io.WriteString(os.Stderr, "stderr="+string(input)); err != nil {
			os.Exit(9)
		}
		os.Exit(0)
	default:
		os.Exit(6)
	}
}
