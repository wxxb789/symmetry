package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// TestProductionDaemonCrashResponseLossAndReconnect is an opt-in witness for
// the production daemon binary. It deliberately loses one committed terminal
// transition response, keeps the retry request unresolved until the daemon is
// killed, and then replays the same journal after a durable-state restart.
//
// The witness is intentionally separate from the in-process E2E tests: those
// tests validate application behavior, while this test validates process and
// restart boundaries. It needs a live Control plane and fake-agent binary.
func TestProductionDaemonCrashResponseLossAndReconnect(t *testing.T) {
	if os.Getenv("SYMMETRY_E2E_DAEMON_CRASH_WITNESS") != "1" {
		t.Skip("set SYMMETRY_E2E_DAEMON_CRASH_WITNESS=1 to run the production daemon crash witness")
	}

	environment := loadEnvironment(t)
	upstream, err := url.Parse(environment.baseURL)
	if err != nil {
		t.Fatalf("parse Control URL: %v", err)
	}
	proxy := newProductionResponseLossProxy(t, upstream)
	proxied := environment
	proxied.baseURL = proxy.URL

	runRoot := t.TempDir()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, name := range []string{"first-daemon.stderr.log", "first-daemon.stdout.log", "second-daemon.stderr.log", "second-daemon.stdout.log"} {
			path := filepath.Join(runRoot, name)
			contents, readErr := os.ReadFile(path)
			if readErr == nil && len(contents) > 0 {
				t.Logf("%s:\n%s", name, contents)
			}
		}
	})
	daemonBinary := buildProductionDaemonWitness(t, runRoot)
	stateDir := filepath.Join(runRoot, "state")
	workspacePath := filepath.Join(runRoot, "workspace")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create durable state directory: %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatalf("create workspace directory: %v", err)
	}
	profile := unique("production-crash-profile")
	workspace := unique("production-crash-workspace")
	value := daemonConfig(proxied, "production-crash", stateDir, workspacePath, profile, workspace)
	configPath := filepath.Join(runRoot, "daemon.json")
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal daemon config: %v", err)
	}
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatalf("write daemon config: %v", err)
	}

	operator := newOperator(t, environment)
	first := startProductionDaemonWitness(t, daemonBinary, configPath, runRoot, environment.enrollmentToken, "slow", "first")
	firstAlive := true
	t.Cleanup(func() {
		if firstAlive {
			first.stop(t)
		}
	})

	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "production-crash", "slow")
	running := waitForTask(t, operator, task.TaskID, 30*time.Second, func(value protocol.Task) bool {
		return value.State == "running"
	})
	runKey := state.RunKey{RunID: taskRunID(t, running), Generation: taskGeneration(t, running)}

	cancelTask(t, operator, task.TaskID)
	proxy.waitForCommittedResponse(t, 30*time.Second)
	pending := waitForJournalOnDisk(t, stateDir, runKey, 10*time.Second, func(journal state.RunJournal) bool {
		return journal.LocalState == "terminal_pending" && journal.TerminalState == "cancelled" && len(journal.PendingTransitions) > 0
	})
	if pending.PendingTransitions[0].State != "cancelled" {
		t.Fatalf("response-loss journal transition = %#v, want cancelled", pending.PendingTransitions[0])
	}
	if pending.TerminalVerdict != "" {
		t.Fatalf("response-loss journal terminal verdict = %q, want unknown before replay", pending.TerminalVerdict)
	}

	// The proxy blocks any retry before this point. Killing the real daemon now
	// therefore leaves the committed Control transition as an unknown local
	// outcome, with its exact request retained in the journal.
	first.stop(t)
	firstAlive = false
	proxy.allowReplay()

	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "")
	second := startProductionDaemonWitness(t, daemonBinary, configPath, runRoot, "", "slow", "second")
	secondAlive := true
	t.Cleanup(func() {
		if secondAlive {
			second.stop(t)
		}
	})

	proxy.waitForSecondRegistration(t, 30*time.Second)
	proxy.waitForReplay(t, 45*time.Second)
	cancelled := waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool {
		return value.State == "cancelled" && taskRunID(t, value) == runKey.RunID && taskGeneration(t, value) == runKey.Generation
	})
	if taskGeneration(t, cancelled) != runKey.Generation {
		t.Fatalf("replayed cancellation generation = %d, want %d", taskGeneration(t, cancelled), runKey.Generation)
	}

	if got := proxy.registrationRequests.Load(); got < 2 {
		t.Fatalf("daemon registration requests = %d, want first registration plus restart re-registration", got)
	}
	if got := proxy.committedTransitionResponses.Load(); got != 1 {
		t.Fatalf("committed transition responses before replay = %d, want 1", got)
	}
	if got := proxy.forwardedReplayRequests.Load(); got != 1 {
		t.Fatalf("forwarded replay transition requests = %d, want exactly 1", got)
	}
	firstRequest, replayRequest, replayStatus, found := proxy.transitionWitness()
	if !found {
		t.Fatal("response-loss proxy did not retain the first terminal transition witness")
	}
	if firstRequest.State != "cancelled" {
		t.Fatalf("first dropped transition state = %q, want cancelled", firstRequest.State)
	}
	if firstRequest.TransitionID == "" || firstRequest.Fence == (protocol.Fence{}) || len(firstRequest.Payload) == 0 || len(firstRequest.Body) == 0 {
		t.Fatalf("first dropped transition witness is incomplete: %#v", firstRequest)
	}
	if replayStatus != http.StatusOK {
		t.Fatalf("replayed transition HTTP status = %d, want %d", replayStatus, http.StatusOK)
	}
	if !sameProductionTransitionRequest(firstRequest, replayRequest) {
		t.Fatalf("replayed transition request differs from first terminal request: first=%#v replay=%#v", firstRequest, replayRequest)
	}

	transitions := collectHistory(t, environment, task.TaskID, "transitions")
	terminalTransitions := filterHistory(transitions, func(entry map[string]json.RawMessage) bool {
		state := historyString(t, entry, "state")
		return state == "cancelled" || state == "completed" || state == "failed"
	})
	if len(terminalTransitions) != 1 {
		t.Fatalf("Control terminal transition history = %d entries, want one: %#v", len(terminalTransitions), terminalTransitions)
	}
	if got := historyString(t, terminalTransitions[0], "state"); got != "cancelled" {
		t.Fatalf("Control terminal transition state = %q, want cancelled", got)
	}

	second.stop(t)
	secondAlive = false
}

func TestProductionResponseLossProxyOnlyDropsCancelledTerminalTransition(t *testing.T) {
	var (
		upstreamMu    sync.Mutex
		upstreamState []string
		upstreamBody  [][]byte
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		var value struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &value); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		upstreamMu.Lock()
		upstreamState = append(upstreamState, value.State)
		upstreamBody = append(upstreamBody, append([]byte(nil), body...))
		upstreamMu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse test upstream URL: %v", err)
	}
	proxy := newProductionResponseLossProxy(t, upstreamURL)
	client := &http.Client{Timeout: 2 * time.Second}
	fence := protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 2, Generation: 3, ClaimID: "claim-1", LeaseToken: "lease-1"}
	runningBody, err := json.Marshal(struct {
		protocol.Fence
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}{Fence: fence, State: "running", Payload: json.RawMessage(`{"step":"start"}`)})
	if err != nil {
		t.Fatalf("marshal running transition: %v", err)
	}
	if response := doProductionTransitionRequest(t, client, proxy.URL, "running-1", runningBody); response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		t.Fatalf("running transition HTTP status = %d, want %d", response.StatusCode, http.StatusOK)
	} else {
		_ = response.Body.Close()
	}
	if _, _, _, found := proxy.transitionWitness(); found {
		t.Fatal("running transition was treated as the response-loss witness")
	}

	cancelledBody, err := json.Marshal(struct {
		protocol.Fence
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}{Fence: fence, State: "cancelled", Payload: json.RawMessage(`{"reason":"operator"}`)})
	if err != nil {
		t.Fatalf("marshal cancelled transition: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		request, requestErr := http.NewRequest(http.MethodPut, proxy.URL+"/api/v1/runs/run-1/transitions/cancelled-1", bytes.NewReader(cancelledBody))
		if requestErr != nil {
			firstDone <- requestErr
			return
		}
		response, requestErr := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		firstDone <- requestErr
	}()
	proxy.waitForCommittedResponse(t, 2*time.Second)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("response-loss request did not finish after the proxy closed its connection")
	}
	proxy.allowReplay()
	response := doProductionTransitionRequest(t, client, proxy.URL, "cancelled-1", cancelledBody)
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		t.Fatalf("replayed cancelled transition HTTP status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	_ = response.Body.Close()

	firstRequest, replayRequest, replayStatus, found := proxy.transitionWitness()
	if !found || firstRequest.State != "cancelled" || replayStatus != http.StatusOK || !sameProductionTransitionRequest(firstRequest, replayRequest) {
		t.Fatalf("cancelled transition witness/replay mismatch: first=%#v replay=%#v status=%d found=%t", firstRequest, replayRequest, replayStatus, found)
	}
	upstreamMu.Lock()
	states := append([]string(nil), upstreamState...)
	bodies := append([][]byte(nil), upstreamBody...)
	upstreamMu.Unlock()
	if len(states) != 3 || states[0] != "running" || states[1] != "cancelled" || states[2] != "cancelled" {
		t.Fatalf("upstream transition states = %#v, want running then cancelled replay", states)
	}
	if len(bodies) != 3 || !bytes.Equal(bodies[1], bodies[2]) {
		t.Fatalf("cancelled replay body differs from first body: bodies=%#v", bodies)
	}
}

func doProductionTransitionRequest(t *testing.T, client *http.Client, baseURL, transitionID string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, baseURL+"/api/v1/runs/run-1/transitions/"+transitionID, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create transition request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send transition request: %v", err)
	}
	return response
}

type productionDaemonWitnessProcess struct {
	command *exec.Cmd
	stdout  *os.File
	stderr  *os.File
	waited  bool
}

type productionTransitionRequest struct {
	Path         string
	TransitionID string
	Fence        protocol.Fence
	State        string
	Payload      json.RawMessage
	Body         []byte
}

func (request productionTransitionRequest) clone() productionTransitionRequest {
	request.Payload = append(json.RawMessage(nil), request.Payload...)
	request.Body = append([]byte(nil), request.Body...)
	return request
}

func sameProductionTransitionRequest(left, right productionTransitionRequest) bool {
	return left.Path == right.Path &&
		left.TransitionID == right.TransitionID &&
		left.Fence == right.Fence &&
		left.State == right.State &&
		bytes.Equal(left.Payload, right.Payload) &&
		bytes.Equal(left.Body, right.Body)
}

func startProductionDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, agentMode, label string) *productionDaemonWitnessProcess {
	t.Helper()
	stdoutPath := filepath.Join(workDir, label+"-daemon.stdout.log")
	stderrPath := filepath.Join(workDir, label+"-daemon.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open daemon stdout log: %v", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open daemon stderr log: %v", err)
	}
	command := exec.Command(binary, "-config", configPath)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("configure hidden daemon process: %v", err)
	}
	command.Env = witnessEnvironment(enrollmentToken, agentMode)
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start production daemon: %v", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
}

func (process *productionDaemonWitnessProcess) stop(t *testing.T) {
	t.Helper()
	if process == nil || process.waited || process.command == nil {
		return
	}
	if process.command.Process != nil {
		if err := process.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("kill production daemon: %v", err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- process.command.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("production daemon did not exit after hard crash")
	}
	process.waited = true
	if err := process.stdout.Close(); err != nil {
		t.Fatalf("close daemon stdout log: %v", err)
	}
	if err := process.stderr.Close(); err != nil {
		t.Fatalf("close daemon stderr log: %v", err)
	}
}

func witnessEnvironment(enrollmentToken, agentMode string) []string {
	environment := append([]string(nil), os.Environ()...)
	set := func(key, value string) {
		prefix := key + "="
		for index, entry := range environment {
			if strings.HasPrefix(entry, prefix) {
				environment[index] = prefix + value
				return
			}
		}
		environment = append(environment, prefix+value)
	}
	set("SYMMETRY_FAKE_AGENT_MODE", agentMode)
	if enrollmentToken == "" {
		filtered := environment[:0]
		for _, entry := range environment {
			if !strings.HasPrefix(entry, "SYMMETRY_ENROLLMENT_TOKEN=") {
				filtered = append(filtered, entry)
			}
		}
		environment = filtered
	} else {
		set("SYMMETRY_ENROLLMENT_TOKEN", enrollmentToken)
	}
	return environment
}

func buildProductionDaemonWitness(t *testing.T, runRoot string) string {
	t.Helper()
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate production daemon witness source")
	}
	daemonRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), ".."))
	binaryName := "symmetry-daemon"
	if filepath.Separator == '\\' {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(runRoot, binaryName)
	command := exec.Command("go", "build", "-o", binaryPath, "./cmd/symmetry-daemon")
	command.Dir = daemonRoot
	command.Stdout = io.Discard
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("configure hidden daemon build: %v", err)
	}
	if err := command.Run(); err != nil {
		t.Fatalf("build production daemon: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return binaryPath
}

type productionResponseLossProxy struct {
	*httptest.Server
	reverseProxy *httputil.ReverseProxy
	client       *http.Client

	dropped       chan struct{}
	releaseReplay chan struct{}
	secondReady   chan struct{}
	replayReady   chan struct{}
	dropOnce      sync.Once
	releaseOnce   sync.Once
	secondOnce    sync.Once
	replayOnce    sync.Once
	witnessMu     sync.Mutex
	firstRequest  *productionTransitionRequest
	replayRequest *productionTransitionRequest
	replayStatus  int

	registrationRequests         atomic.Int64
	committedTransitionResponses atomic.Int64
	forwardedReplayRequests      atomic.Int64
}

func newProductionResponseLossProxy(t *testing.T, upstream *url.URL) *productionResponseLossProxy {
	t.Helper()
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, err error) {
		http.Error(response, "proxy upstream error: "+err.Error(), http.StatusBadGateway)
	}
	witness := &productionResponseLossProxy{
		reverseProxy:  proxy,
		client:        &http.Client{Timeout: 30 * time.Second},
		dropped:       make(chan struct{}),
		releaseReplay: make(chan struct{}),
		secondReady:   make(chan struct{}),
		replayReady:   make(chan struct{}),
	}
	witness.Server = httptest.NewServer(http.HandlerFunc(witness.serveHTTP))
	t.Cleanup(func() {
		witness.allowReplay()
		witness.Close()
	})
	return witness
}

func (proxy *productionResponseLossProxy) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if isProductionRegistrationRequest(request) {
		if proxy.registrationRequests.Add(1) >= 2 {
			proxy.secondOnce.Do(func() { close(proxy.secondReady) })
		}
	}
	if !isProductionTransitionRequest(request) {
		proxy.reverseProxy.ServeHTTP(response, request)
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	transition, err := parseProductionTransitionRequest(request, body)
	if err != nil {
		http.Error(response, "parse transition body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isProductionTerminalTransitionRequest(request, transition) {
		proxy.reverseProxy.ServeHTTP(response, request)
		return
	}
	first := false
	proxy.dropOnce.Do(func() { first = true })
	if first {
		status, forwardErr := proxy.forwardAndDiscard(request, body)
		if forwardErr != nil {
			http.Error(response, "forward committed transition: "+forwardErr.Error(), http.StatusBadGateway)
			return
		}
		if status != http.StatusOK {
			http.Error(response, fmt.Sprintf("unexpected transition status %d", status), http.StatusBadGateway)
			return
		}
		proxy.witnessMu.Lock()
		firstRequest := transition.clone()
		proxy.firstRequest = &firstRequest
		proxy.witnessMu.Unlock()
		proxy.committedTransitionResponses.Add(1)
		close(proxy.dropped)
		closeHijackedConnection(response)
		return
	}
	proxy.witnessMu.Lock()
	firstRequest := proxy.firstRequest
	var expected productionTransitionRequest
	if firstRequest != nil {
		expected = firstRequest.clone()
	}
	proxy.witnessMu.Unlock()
	if firstRequest == nil || !sameProductionTransitionRequest(expected, transition) {
		http.Error(response, "replayed transition does not match first terminal request", http.StatusBadGateway)
		return
	}

	select {
	case <-proxy.releaseReplay:
		proxy.forwardedReplayRequests.Add(1)
		request.Body = io.NopCloser(bytes.NewReader(body))
		recorded := &productionResponseRecorder{ResponseWriter: response}
		proxy.reverseProxy.ServeHTTP(recorded, request)
		proxy.witnessMu.Lock()
		replayRequest := transition.clone()
		proxy.replayRequest = &replayRequest
		proxy.replayStatus = recorded.statusCode()
		proxy.witnessMu.Unlock()
		proxy.replayOnce.Do(func() { close(proxy.replayReady) })
	case <-request.Context().Done():
		return
	}
}

func (proxy *productionResponseLossProxy) forwardAndDiscard(request *http.Request, body []byte) (int, error) {
	forwarded := request.Clone(request.Context())
	forwarded.Body = io.NopCloser(bytes.NewReader(body))
	forwarded.RequestURI = ""
	proxy.reverseProxy.Director(forwarded)
	upstreamResponse, err := proxy.client.Do(forwarded)
	if err != nil {
		return 0, err
	}
	status := upstreamResponse.StatusCode
	_, copyErr := io.Copy(io.Discard, upstreamResponse.Body)
	closeErr := upstreamResponse.Body.Close()
	return status, errors.Join(copyErr, closeErr)
}

func (proxy *productionResponseLossProxy) waitForCommittedResponse(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-proxy.dropped:
	case <-time.After(timeout):
		t.Fatalf("Control did not commit and lose a terminal transition response within %s", timeout)
	}
}

func (proxy *productionResponseLossProxy) waitForSecondRegistration(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-proxy.secondReady:
	case <-time.After(timeout):
		t.Fatalf("Control proxy did not observe daemon re-registration within %s; requests=%d", timeout, proxy.registrationRequests.Load())
	}
}

func (proxy *productionResponseLossProxy) waitForReplay(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-proxy.replayReady:
	case <-time.After(timeout):
		t.Fatalf("Control proxy did not observe the exact transition replay within %s", timeout)
	}
}

func (proxy *productionResponseLossProxy) allowReplay() {
	proxy.releaseOnce.Do(func() { close(proxy.releaseReplay) })
}

func (proxy *productionResponseLossProxy) transitionWitness() (productionTransitionRequest, productionTransitionRequest, int, bool) {
	proxy.witnessMu.Lock()
	defer proxy.witnessMu.Unlock()
	if proxy.firstRequest == nil || proxy.replayRequest == nil {
		return productionTransitionRequest{}, productionTransitionRequest{}, proxy.replayStatus, false
	}
	return proxy.firstRequest.clone(), proxy.replayRequest.clone(), proxy.replayStatus, true
}

type productionResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *productionResponseRecorder) WriteHeader(status int) {
	if recorder.status != 0 {
		return
	}
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *productionResponseRecorder) Write(body []byte) (int, error) {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(body)
}

func (recorder *productionResponseRecorder) statusCode() int {
	if recorder.status == 0 {
		return http.StatusOK
	}
	return recorder.status
}

func closeHijackedConnection(response http.ResponseWriter) {
	hijacker, ok := response.(http.Hijacker)
	if !ok {
		return
	}
	connection, _, err := hijacker.Hijack()
	if err == nil {
		_ = connection.Close()
	}
}

func isProductionTransitionRequest(request *http.Request) bool {
	return request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/v1/runs/") && strings.Contains(request.URL.Path, "/transitions/")
}

func isProductionTerminalTransitionRequest(request *http.Request, transition productionTransitionRequest) bool {
	return isProductionTransitionRequest(request) && transition.State == "cancelled"
}

func parseProductionTransitionRequest(request *http.Request, body []byte) (productionTransitionRequest, error) {
	var value struct {
		protocol.Fence
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return productionTransitionRequest{}, err
	}
	transitionID := path.Base(request.URL.Path)
	if transitionID == "." || transitionID == "/" || transitionID == "" {
		return productionTransitionRequest{}, errors.New("transition path has no transition ID")
	}
	return productionTransitionRequest{
		Path:         request.URL.Path,
		TransitionID: transitionID,
		Fence:        value.Fence,
		State:        value.State,
		Payload:      append(json.RawMessage(nil), value.Payload...),
		Body:         append([]byte(nil), body...),
	}, nil
}

func isProductionRegistrationRequest(request *http.Request) bool {
	return request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/v1/machines/") && strings.Contains(request.URL.Path, "/sessions/")
}
