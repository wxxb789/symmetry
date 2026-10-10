//go:build linux

package opencode

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	openCodePeerServerEnv  = "SYMMETRY_OPENCODE_V2_PEER_SERVER"
	linuxV2PeerHostGateEnv = "SYMMETRY_LINUX_V2_ADAPTER_PEER_HOST_GATE"
)

func requireLinuxV2PeerHostGate(t *testing.T) {
	t.Helper()
	if os.Getenv(linuxV2PeerHostGateEnv) != "1" {
		t.Skip("requires the explicitly qualified Linux v2 peer inspection host gate")
	}
}

type openCodePeerOutput struct {
	mu           sync.Mutex
	content      strings.Builder
	ready        chan int
	readyAt      sync.Once
	denied       chan struct{}
	deniedAt     sync.Once
	accepted     chan struct{}
	acceptedAt   sync.Once
	requestCount int
}

func (output *openCodePeerOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	_, _ = output.content.Write(data)
	text := output.content.String()
	requestCount := strings.Count(text, "REQUEST")
	output.requestCount = requestCount
	output.mu.Unlock()
	if index := strings.Index(text, "READY "); index >= 0 {
		line := strings.Fields(text[index:])
		if len(line) >= 2 {
			if port, err := strconv.Atoi(line[1]); err == nil {
				output.readyAt.Do(func() { output.ready <- port })
			}
		}
	}
	if strings.Contains(text, "DUMPABLE 0") {
		output.deniedAt.Do(func() { close(output.denied) })
	}
	if strings.Contains(text, "ACCEPT") {
		output.acceptedAt.Do(func() { close(output.accepted) })
	}
	return len(data), nil
}

func (output *openCodePeerOutput) requestTotal() int {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.requestCount
}

type openCodePeerObservedListener struct {
	net.Listener
}

func (listener openCodePeerObservedListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err == nil {
		_, _ = fmt.Fprintln(os.Stdout, "ACCEPT")
	}
	return connection, err
}

// This child is a real OS TCP peer for adapter/verifier tests. Its test-owned
// pidfd is not an App-owned Linux v2 capability and does not establish
// production owner acceptance.
func TestOpenCodeKernelPeerServerProcess(t *testing.T) {
	if os.Getenv(openCodePeerServerEnv) != "1" {
		return
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for OpenCode peer test: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprintln(os.Stdout, "REQUEST")
		username, password, ok := request.BasicAuth()
		if !ok || username != "opencode" || password != "secret" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"healthy":true}`)
	})}
	go func() { _ = server.Serve(openCodePeerObservedListener{Listener: listener}) }()
	_, _ = fmt.Fprintf(os.Stdout, "READY %d\n", listener.Addr().(*net.TCPAddr).Port)
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = server.Close()
			return
		}
		switch strings.TrimSpace(line) {
		case "dumpable=0":
			if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
				t.Fatalf("disable target dumpability: %v", err)
			}
			_, _ = fmt.Fprintln(os.Stdout, "DUMPABLE 0")
		case "stop":
			_ = server.Close()
			return
		}
	}
}

type openCodeKernelPeerProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	peer    *execution.LinuxV2Peer
	wait    sync.Once
}

func startOpenCodeKernelPeerServer(t *testing.T) (*openCodeKernelPeerProcess, *execution.LinuxV2Peer, *openCodePeerOutput, string) {
	t.Helper()
	output := &openCodePeerOutput{
		ready: make(chan int, 1), denied: make(chan struct{}),
		accepted: make(chan struct{}),
	}
	command := exec.Command(os.Args[0], "-test.run=^TestOpenCodeKernelPeerServerProcess$")
	environment := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, openCodePeerServerEnv+"=") {
			environment = append(environment, item)
		}
	}
	environment = append(environment, openCodePeerServerEnv+"=1")
	command.Env = environment
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open OpenCode peer test stdin: %v", err)
	}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		_ = input.Close()
		t.Fatalf("start OpenCode kernel peer test process: %v", err)
	}
	process := &openCodeKernelPeerProcess{command: command, input: input}
	t.Cleanup(process.close)
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		process.close()
		t.Fatalf("open real OpenCode peer pidfd: %v", err)
	}
	peer := captureOpenCodeKernelPeer(t, command.Process.Pid, pidfd)
	process.peer = peer
	var port int
	select {
	case port = <-output.ready:
	case <-time.After(5 * time.Second):
		_ = peer.Close()
		process.close()
		t.Fatal("OpenCode v2 peer test process did not become ready")
	}
	return process, peer, output, fmt.Sprintf("http://127.0.0.1:%d", port)
}

func captureOpenCodeKernelPeer(t *testing.T, pid, pidfd int) *execution.LinuxV2Peer {
	t.Helper()
	pidfdFile := os.NewFile(uintptr(pidfd), "opencode-kernel-peer-pidfd")
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		_ = pidfdFile.Close()
		t.Fatalf("capture real peer process identity: %v", err)
	}
	namespace, err := platform.CaptureLinuxUserNamespaceIdentity(pid, identity, pidfdFile)
	if err != nil {
		_ = pidfdFile.Close()
		t.Fatalf("capture real peer user namespace: %v", err)
	}
	return &execution.LinuxV2Peer{PID: pid, Identity: identity, UserNamespace: namespace, PIDFD: pidfdFile}
}

func duplicateOpenCodeKernelPeer(t *testing.T, peer *execution.LinuxV2Peer) *execution.LinuxV2Peer {
	t.Helper()
	fd, err := unix.FcntlInt(peer.PIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate test-owned peer pidfd: %v", err)
	}
	clone := *peer
	clone.PIDFD = os.NewFile(uintptr(fd), "opencode-kernel-peer-clone")
	return &clone
}

func requireOpenCodeKernelPeerBaseline(t *testing.T, peer *execution.LinuxV2Peer, output *openCodePeerOutput, endpoint string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp4", strings.TrimPrefix(endpoint, "http://"), 3*time.Second)
	if err != nil {
		t.Fatalf("dial test-owned OpenCode peer baseline: %v", err)
	}
	defer connection.Close()
	select {
	case <-output.accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("OpenCode peer did not accept baseline connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := platform.VerifyLoopbackTCPPeerV2(ctx, connection, peer.PID, peer.Identity, peer.PIDFD, peer.UserNamespace); err != nil {
		t.Fatalf("valid test-owned OpenCode peer baseline failed: %v", err)
	}
	if output.requestTotal() != 0 {
		t.Fatal("peer baseline unexpectedly sent HTTP request bytes")
	}
}

func makeOpenCodeKernelPeerNonDumpable(t *testing.T, process *openCodeKernelPeerProcess, output *openCodePeerOutput) {
	t.Helper()
	if _, err := io.WriteString(process.input, "dumpable=0\n"); err != nil {
		t.Fatalf("request test peer dumpability denial: %v", err)
	}
	select {
	case <-output.denied:
	case <-time.After(5 * time.Second):
		t.Fatal("OpenCode peer did not acknowledge PR_SET_DUMPABLE=0")
	}
}

func (process *openCodeKernelPeerProcess) close() {
	if process == nil {
		return
	}
	process.terminate()
	if process.peer != nil {
		_ = process.peer.Close()
	}
}

func (process *openCodeKernelPeerProcess) terminate() {
	if process == nil {
		return
	}
	process.wait.Do(func() {
		_ = process.command.Process.Kill()
		_ = process.input.Close()
		_ = process.command.Wait()
	})
}

func TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest(t *testing.T) {
	process, peer, output, endpoint := startOpenCodeKernelPeerServer(t)
	_, wrongProcessPeer, _, _ := startOpenCodeKernelPeerServer(t)
	if wrongProcessPeer.PID <= 0 || wrongProcessPeer.PID == peer.PID || wrongProcessPeer.Identity == peer.Identity {
		t.Fatal("wrong-pidfd case did not capture a distinct live process identity")
	}
	wrongPIDFD := *peer
	wrongPIDFD.PIDFD = wrongProcessPeer.PIDFD
	closedFD, err := unix.FcntlInt(peer.PIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate peer pidfd for closed-binding case: %v", err)
	}
	closedPeer := *peer
	closedPeer.PIDFD = os.NewFile(uintptr(closedFD), "closed-opencode-peer-pidfd")
	if err := closedPeer.PIDFD.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := closedPeer.PIDFD.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed peer pidfd stat error = %v, want os.ErrClosed", err)
	}
	t.Cleanup(func() { _ = closedPeer.Close() })
	cases := []struct {
		name                   string
		peer                   *execution.LinuxV2Peer
		wantPIDFDMismatchError bool
	}{
		{name: "mismatched retained pidfd", peer: &wrongPIDFD, wantPIDFDMismatchError: true},
		{name: "closed retained pidfd", peer: &closedPeer},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(Config{
				BaseURL: endpoint, Username: "opencode", Password: "secret",
				VerifyConnection: func(ctx context.Context, conn net.Conn) error {
					return platform.VerifyLoopbackTCPPeerV2(ctx, conn, test.peer.PID, test.peer.Identity, test.peer.PIDFD, test.peer.UserNamespace)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = client.Health(ctx)
			if !errors.Is(err, ErrPeerOwnership) {
				t.Fatalf("Health() error = %v, want peer ownership rejection", err)
			}
			if test.wantPIDFDMismatchError && !errors.Is(err, platform.ErrLinuxPeerPIDFDMismatch) {
				t.Fatalf("Health() error = %v, want typed retained PIDFD/process mismatch", err)
			}
			client.httpClient.CloseIdleConnections()
		})
	}
	process.close()
	if output.requestTotal() != 0 {
		t.Fatal("OpenCode received HTTP bytes for an invalid Linux v2 peer binding")
	}
	_ = closedPeer.Close()
}

func TestOpenCodeQualifiedStartNamespacePeerDenials(t *testing.T) {
	requireLinuxV2PeerHostGate(t)
	process, peer, output, endpoint := startOpenCodeKernelPeerServer(t)
	requireOpenCodeKernelPeerBaseline(t, peer, output, endpoint)
	wrongIdentity := *peer
	identityParts := strings.Split(wrongIdentity.Identity, ":")
	if len(identityParts) != 6 {
		t.Fatalf("captured process identity has unexpected shape: %q", wrongIdentity.Identity)
	}
	startTime, err := strconv.ParseUint(identityParts[len(identityParts)-1], 10, 64)
	if err != nil {
		t.Fatalf("parse captured process start identity: %v", err)
	}
	identityParts[len(identityParts)-1] = strconv.FormatUint(startTime+1, 10)
	wrongIdentity.Identity = strings.Join(identityParts, ":")
	wrongNamespace := *peer
	wrongNamespace.UserNamespace.Inode++
	for _, test := range []struct {
		name string
		peer *execution.LinuxV2Peer
	}{
		{name: "changed start identity", peer: &wrongIdentity},
		{name: "wrong user namespace", peer: &wrongNamespace},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(Config{
				BaseURL: endpoint, Username: "opencode", Password: "secret",
				VerifyConnection: func(ctx context.Context, conn net.Conn) error {
					return platform.VerifyLoopbackTCPPeerV2(ctx, conn, test.peer.PID, test.peer.Identity, test.peer.PIDFD, test.peer.UserNamespace)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := client.Health(ctx); !errors.Is(err, ErrPeerOwnership) {
				t.Fatalf("Health() error = %v, want peer ownership rejection", err)
			}
			client.httpClient.CloseIdleConnections()
		})
	}
	process.close()
	if output.requestTotal() != 0 {
		t.Fatal("OpenCode received HTTP bytes for a mismatched start identity or namespace")
	}
}

// The valid baseline is captured before the test tells the peer to disable
// dumpability; any later EACCES/EPERM stage must fail before HTTP is sent.
func TestOpenCodeQualifiedNonDumpablePeerDeniedBeforeHTTP(t *testing.T) {
	requireLinuxV2PeerHostGate(t)
	process, peer, output, endpoint := startOpenCodeKernelPeerServer(t)
	requireOpenCodeKernelPeerBaseline(t, peer, output, endpoint)
	makeOpenCodeKernelPeerNonDumpable(t, process, output)
	peerClone := duplicateOpenCodeKernelPeer(t, peer)
	t.Cleanup(func() { _ = peerClone.Close() })
	owner := &linuxV2PeerOwner{peer: peerClone}
	client, err := NewClient(Config{
		BaseURL: endpoint, Username: "opencode", Password: "secret",
		VerifyConnection: ownedPeerVerifier(peerClone.PID, peerClone.Identity, owner),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = client.Health(ctx)
	client.httpClient.CloseIdleConnections()
	permissionDenied := errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)
	if !errors.Is(err, ErrPeerOwnership) || !permissionDenied {
		t.Fatalf("Health() error = %v, want fail-closed peer ownership with kernel permission denial", err)
	}
	ownedPIDFD, sourcePIDFD := peerClone.PIDFD, peer.PIDFD
	if closeErr := owner.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if peerClone.PIDFD != nil {
		t.Fatal("v2 verifier error cleanup did not release its owned pidfd clone")
	}
	if _, err := ownedPIDFD.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("verifier-owned pidfd stat error = %v, want closed descriptor", err)
	}
	if peer.PIDFD == nil || sourcePIDFD == nil {
		t.Fatal("OpenCode peer owner released the source owner's retained pidfd")
	}
	if _, err := sourcePIDFD.Stat(); err != nil {
		t.Fatalf("source peer pidfd was closed with verifier clone: %v", err)
	}
	process.close()
	if output.requestTotal() != 0 {
		t.Fatal("OpenCode received HTTP bytes after Linux v2 peer permission denial")
	}
}

func TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- struct{}{}
		_, _ = fmt.Fprint(writer, `{"healthy":true}`)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	process := newFakeProcess()
	start := func(ctx context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
		readiness := []byte("opencode server listening on http://127.0.0.1:" + port + "\n")
		if err := sink.Handle(ctx, execution.Event{Stream: execution.Stdout, Sequence: 1, Data: readiness}); err != nil {
			return process, err
		}
		return process, nil
	}
	adapter := newAdapter("opencode-test", nil, start, newAPI, func() (string, string, error) {
		return "opencode", "secret", nil
	}, ownedPeerVerifier)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = session.(harness.StagedSession).Open(context.Background())
	if !errors.Is(err, errors.ErrUnsupported) || !errors.Is(err, ErrPeerOwnership) {
		t.Fatalf("Open() error = %v, want unsupported v2 peer ownership", err)
	}
	select {
	case <-requests:
		t.Fatal("OpenCode health request bytes reached the server without a v2 process owner")
	default:
	}
}

func TestOpenCodeV2VerifierRejectsReleasedPeer(t *testing.T) {
	process, peer, _, endpoint := startOpenCodeKernelPeerServer(t)
	connection, err := net.DialTimeout("tcp4", strings.TrimPrefix(endpoint, "http://"), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	process.terminate()
	if err := unix.PidfdSendSignal(int(peer.PIDFD.Fd()), 0, nil, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("retained peer pidfd still identifies a live process: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = platform.VerifyLoopbackTCPPeerV2(ctx, connection, peer.PID, peer.Identity, peer.PIDFD, peer.UserNamespace)
	if err == nil {
		t.Fatal("VerifyLoopbackTCPPeerV2() accepted a released peer process")
	}
}

type blockingSyscallConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (connection *blockingSyscallConn) SyscallConn() (syscall.RawConn, error) {
	connection.once.Do(func() { close(connection.started) })
	<-connection.release
	return connection.Conn.(syscall.Conn).SyscallConn()
}

func TestOpenCodePeerCloseWaitsForActiveVerification(t *testing.T) {
	process, peer, _, endpoint := startOpenCodeKernelPeerServer(t)
	connection, err := net.DialTimeout("tcp4", strings.TrimPrefix(endpoint, "http://"), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	peerClone := duplicateOpenCodeKernelPeer(t, peer)
	t.Cleanup(func() { _ = peerClone.Close() })
	owner := &linuxV2PeerOwner{peer: peerClone}
	blocked := &blockingSyscallConn{Conn: connection, started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseVerification := func() { releaseOnce.Do(func() { close(blocked.release) }) }
	verifyDone := make(chan error, 1)
	verifyFinished := make(chan struct{})
	go func() {
		defer close(verifyFinished)
		verifyDone <- owner.verify(context.Background(), blocked)
	}()
	t.Cleanup(func() {
		releaseVerification()
		<-verifyFinished
	})
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("peer verification did not reach the protected connection inspection")
	}
	if owner.mu.TryLock() {
		owner.mu.Unlock()
		t.Fatal("active verification does not hold the peer owner's read lock")
	}
	if !owner.mu.TryRLock() {
		t.Fatal("peer owner has a writer before Close starts")
	}
	owner.mu.RUnlock()
	closeDone := make(chan error, 1)
	closeFinished := make(chan struct{})
	ownedPIDFD, sourcePIDFD := peerClone.PIDFD, peer.PIDFD
	go func() {
		defer close(closeFinished)
		closeDone <- owner.Close()
	}()
	t.Cleanup(func() {
		releaseVerification()
		<-closeFinished
	})
	deadline := time.Now().Add(5 * time.Second)
	for owner.mu.TryRLock() {
		owner.mu.RUnlock()
		select {
		case err := <-closeDone:
			t.Fatalf("peer Close() completed while verification still borrowed its pidfd: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not queue for the peer owner's write lock")
		}
		runtime.Gosched()
	}
	select {
	case err := <-closeDone:
		t.Fatalf("peer Close() completed while verification still borrowed its pidfd: %v", err)
	default:
	}
	if _, err := ownedPIDFD.Stat(); err != nil {
		t.Fatalf("peer Close released the pidfd before verification completed: %v", err)
	}
	releaseVerification()
	<-verifyDone // Host policy may deny inspection; lock release must not depend on success.
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if peerClone.PIDFD != nil {
		t.Fatal("peer Close did not release its retained pidfd after verification")
	}
	if _, err := ownedPIDFD.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("peer Close pidfd stat error = %v, want closed descriptor", err)
	}
	if peer.PIDFD == nil || sourcePIDFD == nil {
		t.Fatal("peer Close released the source owner's separate retained pidfd")
	}
	if _, err := sourcePIDFD.Stat(); err != nil {
		t.Fatalf("source peer pidfd was closed with owner clone: %v", err)
	}
	process.close()
}
