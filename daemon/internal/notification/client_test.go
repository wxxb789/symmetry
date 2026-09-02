package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRunJoinsAuthenticatesSendsHeartbeatAndDeliversHints(t *testing.T) {
	wrongReplySent := make(chan struct{})
	allowCorrectReply := make(chan struct{})
	heartbeatReceived := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, request *http.Request) {
		if got := request.Header.Get("X-Symmetry-Token"); got != "machine-token" {
			t.Errorf("X-Symmetry-Token = %q, want machine token", got)
		}
		if request.URL.RequestURI() != "/socket/websocket?vsn=2.0.0" {
			t.Errorf("request URI = %q", request.URL.RequestURI())
		}
		if request.URL.Query().Get("token") != "" {
			t.Error("machine token leaked into the WebSocket URL")
		}

		join := readChannelFrame(t, ctx, conn)
		if join.JoinRef == "" || join.JoinRef != join.Ref || join.Topic != "daemon:machine-1" || join.Event != "phx_join" || !isEmptyObject(join.Payload) {
			t.Errorf("unexpected join frame: %#v", join)
		}
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: "wrong-ref", Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		close(wrongReplySent)
		select {
		case <-allowCorrectReply:
		case <-ctx.Done():
			return
		}
		writeText(t, ctx, conn, "this is not JSON")
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})

		heartbeat := readChannelFrame(t, ctx, conn)
		if heartbeat.JoinRef != "" || heartbeat.Topic != "phoenix" || heartbeat.Event != "heartbeat" || !isEmptyObject(heartbeat.Payload) {
			t.Errorf("unexpected heartbeat frame: %#v", heartbeat)
		}
		writeChannelFrame(t, ctx, conn, channelFrame{Ref: heartbeat.Ref, Topic: "phoenix", Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		close(heartbeatReceived)

		writeChannelFrame(t, ctx, conn, channelFrame{Topic: join.Topic, Event: "work_available", Payload: json.RawMessage(`{"runtime_id":"runtime-1"}`)})
		writeChannelFrame(t, ctx, conn, channelFrame{Topic: join.Topic, Event: "command_available", Payload: json.RawMessage(`{"runtime_id":"runtime-2","command_id":"command-1"}`)})
		writeChannelFrame(t, ctx, conn, channelFrame{Topic: join.Topic, Event: "reconcile_required", Payload: json.RawMessage(`{"reason":"server_restart"}`)})
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Millisecond, reconnectInitial: 5 * time.Millisecond, reconnectMaximum: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hints := make(chan Hint, 4)
	done := runClient(client, ctx, hints)

	<-wrongReplySent
	select {
	case hint := <-hints:
		t.Fatalf("received hint before correlated join reply: %#v", hint)
	case <-time.After(20 * time.Millisecond):
	}
	close(allowCorrectReply)

	want := []Hint{
		{Type: "connected"},
		{Type: "work_available", RuntimeID: "runtime-1"},
		{Type: "command_available", RuntimeID: "runtime-2", CommandID: "command-1"},
		{Type: "reconcile_required", Reason: "server_restart"},
	}
	for _, expected := range want {
		select {
		case got := <-hints:
			if got != expected {
				t.Errorf("hint = %#v, want %#v", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for hint %#v", expected)
		}
	}
	select {
	case <-heartbeatReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for heartbeat")
	}

	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunReconnectsAfterPhoenixErrorWithNewJoinReference(t *testing.T) {
	var (
		mu       sync.Mutex
		joinRefs []string
	)
	connectedTwice := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		join := readChannelFrame(t, ctx, conn)
		mu.Lock()
		joinRefs = append(joinRefs, join.JoinRef)
		attempt := len(joinRefs)
		mu.Unlock()
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		if attempt == 1 {
			writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Topic: join.Topic, Event: "phx_error", Payload: json.RawMessage(`{}`)})
			return
		}
		close(connectedTwice)
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: 5 * time.Millisecond, reconnectMaximum: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runClient(client, ctx, make(chan Hint, 4))

	select {
	case <-connectedTwice:
	case <-time.After(time.Second):
		t.Fatal("client did not reconnect after phx_error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(joinRefs) != 2 || joinRefs[0] == "" || joinRefs[0] == joinRefs[1] {
		t.Errorf("join refs = %#v, want two distinct non-empty values", joinRefs)
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunStopsPromptlyWhenContextIsCancelled(t *testing.T) {
	joined := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		join := readChannelFrame(t, ctx, conn)
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		close(joined)
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: time.Second, reconnectMaximum: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := runClient(client, ctx, make(chan Hint, 1))
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("client did not join")
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunReconnectBackoffDoesNotBusySpin(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: 20 * time.Millisecond, reconnectMaximum: 40 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Millisecond)
	defer cancel()
	err := client.Run(ctx, make(chan Hint))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run error = %v, want context.DeadlineExceeded", err)
	}
	if got := attempts.Load(); got < 3 || got > 7 {
		t.Errorf("dial attempts = %d, want bounded retries without busy spin", got)
	}
}

func TestRunDropsWakeHintsWhenReceiverIsUnavailable(t *testing.T) {
	sent := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		join := readChannelFrame(t, ctx, conn)
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		for range 20 {
			writeChannelFrame(t, ctx, conn, channelFrame{Topic: join.Topic, Event: "work_available", Payload: json.RawMessage(`{"runtime_id":"runtime-1"}`)})
		}
		close(sent)
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: time.Second, reconnectMaximum: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := runClient(client, ctx, make(chan Hint))
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("client blocked while sending a wake hint")
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunReconnectsWhenHeartbeatReplyIsMissing(t *testing.T) {
	var attempts atomic.Int32
	reconnected := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		join := readChannelFrame(t, ctx, conn)
		attempt := attempts.Add(1)
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		if attempt == 1 {
			_ = readChannelFrame(t, ctx, conn)
			<-ctx.Done()
			return
		}
		close(reconnected)
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Millisecond, heartbeatTimeout: 20 * time.Millisecond, reconnectInitial: 5 * time.Millisecond, reconnectMaximum: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := runClient(client, ctx, make(chan Hint, 4))
	select {
	case <-reconnected:
	case <-time.After(time.Second):
		t.Fatal("client did not reconnect after a missing heartbeat reply")
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunIgnoresStaleChannelCloseFrames(t *testing.T) {
	var attempts atomic.Int32
	staleFramesSent := make(chan struct{})
	releaseCurrentClose := make(chan struct{})
	reconnected := make(chan struct{})
	server := newChannelServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		join := readChannelFrame(t, ctx, conn)
		attempt := attempts.Add(1)
		writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Ref: join.Ref, Topic: join.Topic, Event: "phx_reply", Payload: json.RawMessage(`{"status":"ok"}`)})
		if attempt == 1 {
			writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: "stale", Topic: join.Topic, Event: "phx_error", Payload: json.RawMessage(`{}`)})
			writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: "stale", Topic: join.Topic, Event: "phx_close", Payload: json.RawMessage(`{}`)})
			close(staleFramesSent)
			select {
			case <-releaseCurrentClose:
			case <-ctx.Done():
				return
			}
			writeChannelFrame(t, ctx, conn, channelFrame{JoinRef: join.JoinRef, Topic: join.Topic, Event: "phx_close", Payload: json.RawMessage(`{}`)})
			return
		}
		close(reconnected)
		<-ctx.Done()
	})
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: 5 * time.Millisecond, reconnectMaximum: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := runClient(client, ctx, make(chan Hint, 4))
	<-staleFramesSent
	time.Sleep(20 * time.Millisecond)
	if got := attempts.Load(); got != 1 {
		t.Fatalf("connections after stale frames = %d, want 1", got)
	}
	close(releaseCurrentClose)
	select {
	case <-reconnected:
	case <-time.After(time.Second):
		t.Fatal("client did not reconnect after the current phx_close")
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunDialUsesDeadline(t *testing.T) {
	dialStarted := make(chan struct{})
	dialCancelled := make(chan struct{})
	var startedOnce, cancelledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		startedOnce.Do(func() { close(dialStarted) })
		<-request.Context().Done()
		cancelledOnce.Do(func() { close(dialCancelled) })
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, testOptions{heartbeatInterval: time.Hour, reconnectInitial: time.Second, reconnectMaximum: time.Second, dialTimeout: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := runClient(client, ctx, make(chan Hint))
	select {
	case <-dialStarted:
	case <-time.After(time.Second):
		t.Fatal("client did not start dialing")
	}
	select {
	case <-dialCancelled:
	case <-time.After(time.Second):
		t.Fatal("WebSocket dial did not observe its deadline")
	}
	cancel()
	if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestNewRejectsUnsafeInputs(t *testing.T) {
	for _, test := range []struct {
		name          string
		baseURL       string
		websocketPath string
	}{
		{name: "missing machine ID", baseURL: "https://control.example", websocketPath: "/socket/websocket"},
		{name: "absolute websocket URL", baseURL: "https://control.example", websocketPath: "wss://other.example/socket"},
		{name: "token in websocket path", baseURL: "https://control.example", websocketPath: "/socket/websocket?token=machine-token"},
		{name: "unexpected websocket query", baseURL: "https://control.example", websocketPath: "/socket/websocket?vsn=2.0.0&debug=true"},
		{name: "unsupported Phoenix version", baseURL: "https://control.example", websocketPath: "/socket/websocket?vsn=1.0.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			machineID := "machine-1"
			if test.name == "missing machine ID" {
				machineID = ""
			}
			if _, err := New(test.baseURL, test.websocketPath, machineID, "machine-token", nil); err == nil {
				t.Fatal("New succeeded with unsafe input")
			}
		})
	}
}

type testOptions struct {
	heartbeatInterval time.Duration
	reconnectInitial  time.Duration
	reconnectMaximum  time.Duration
	dialTimeout       time.Duration
	writeTimeout      time.Duration
	heartbeatTimeout  time.Duration
}

func newTestClient(t *testing.T, baseURL string, options testOptions) *Client {
	t.Helper()
	if options.dialTimeout == 0 {
		options.dialTimeout = time.Second
	}
	if options.writeTimeout == 0 {
		options.writeTimeout = time.Second
	}
	if options.heartbeatTimeout == 0 {
		options.heartbeatTimeout = time.Second
	}
	client, err := newClient(baseURL, "/socket/websocket?vsn=2.0.0", "machine-1", "machine-token", http.DefaultClient, clientOptions{
		heartbeatInterval: options.heartbeatInterval,
		reconnectInitial:  options.reconnectInitial,
		reconnectMaximum:  options.reconnectMaximum,
		dialTimeout:       options.dialTimeout,
		writeTimeout:      options.writeTimeout,
		heartbeatTimeout:  options.heartbeatTimeout,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func newChannelServer(t *testing.T, handler func(context.Context, *websocket.Conn, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept WebSocket: %v", err)
			return
		}
		defer conn.CloseNow()
		handler(request.Context(), conn, request)
	}))
}

func runClient(client *Client, ctx context.Context, hints chan<- Hint) <-chan error {
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, hints) }()
	return done
}

func waitForRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
		return nil
	}
}

type channelFrame struct {
	JoinRef string
	Ref     string
	Topic   string
	Event   string
	Payload json.RawMessage
}

func readChannelFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) channelFrame {
	t.Helper()
	messageType, value, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read WebSocket message: %v", err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("message type = %v, want text", messageType)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(value, &values); err != nil || len(values) != 5 {
		t.Fatalf("decode Phoenix frame %q: %v", value, err)
	}
	frame := channelFrame{Payload: values[4]}
	json.Unmarshal(values[0], &frame.JoinRef)
	json.Unmarshal(values[1], &frame.Ref)
	json.Unmarshal(values[2], &frame.Topic)
	json.Unmarshal(values[3], &frame.Event)
	return frame
}

func writeChannelFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, frame channelFrame) {
	t.Helper()
	joinRef := any(frame.JoinRef)
	if frame.JoinRef == "" {
		joinRef = nil
	}
	value, err := json.Marshal([]any{joinRef, frame.Ref, frame.Topic, frame.Event, frame.Payload})
	if err != nil {
		t.Fatalf("encode Phoenix frame: %v", err)
	}
	writeText(t, ctx, conn, string(value))
}

func writeText(t *testing.T, ctx context.Context, conn *websocket.Conn, value string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, []byte(value)); err != nil {
		t.Fatalf("write WebSocket message: %v", err)
	}
}

func isEmptyObject(value json.RawMessage) bool {
	var object map[string]any
	return json.Unmarshal(value, &object) == nil && len(object) == 0
}
