// Package notification maintains the daemon's Phoenix Channel hint connection.
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultReconnectInitial  = 250 * time.Millisecond
	defaultReconnectMaximum  = 10 * time.Second
	defaultDialTimeout       = 10 * time.Second
	defaultWriteTimeout      = 10 * time.Second
	defaultHeartbeatTimeout  = 10 * time.Second
)

// Hint tells the daemon to fetch authoritative state from the control plane.
type Hint struct {
	Type      string
	RuntimeID string
	CommandID string
	Reason    string
}

// Client maintains one Phoenix Channel connection for one enrolled machine.
// It is intended to have one Run invocation at a time.
type Client struct {
	websocketURL string
	machineID    string
	machineToken string
	httpClient   *http.Client
	options      clientOptions
	nextRef      atomic.Uint64
}

type clientOptions struct {
	heartbeatInterval time.Duration
	reconnectInitial  time.Duration
	reconnectMaximum  time.Duration
	dialTimeout       time.Duration
	writeTimeout      time.Duration
	heartbeatTimeout  time.Duration
}

func defaultClientOptions() clientOptions {
	return clientOptions{
		heartbeatInterval: defaultHeartbeatInterval,
		reconnectInitial:  defaultReconnectInitial,
		reconnectMaximum:  defaultReconnectMaximum,
		dialTimeout:       defaultDialTimeout,
		writeTimeout:      defaultWriteTimeout,
		heartbeatTimeout:  defaultHeartbeatTimeout,
	}
}

// New creates a notification client. websocketPath is the server-provided
// Phoenix path, such as /socket/websocket?vsn=2.0.0.
func New(baseURL, websocketPath, machineID, machineToken string, httpClient *http.Client) (*Client, error) {
	return newClient(baseURL, websocketPath, machineID, machineToken, httpClient, defaultClientOptions())
}

func newClient(baseURL, websocketPath, machineID, machineToken string, httpClient *http.Client, options clientOptions) (*Client, error) {
	if strings.TrimSpace(machineID) == "" {
		return nil, errors.New("machine ID must not be empty")
	}
	if strings.TrimSpace(machineToken) == "" {
		return nil, errors.New("machine token must not be empty")
	}
	websocketURL, err := buildWebSocketURL(baseURL, websocketPath)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &Client{
		websocketURL: websocketURL,
		machineID:    machineID,
		machineToken: machineToken,
		httpClient:   httpClient,
		options:      options,
	}, nil
}

func (options clientOptions) validate() error {
	if options.heartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	if options.reconnectInitial <= 0 || options.reconnectMaximum < options.reconnectInitial {
		return errors.New("reconnect backoff must be positive and bounded")
	}
	if options.dialTimeout <= 0 || options.writeTimeout <= 0 || options.heartbeatTimeout <= 0 {
		return errors.New("connection timeouts must be positive")
	}
	return nil
}

func buildWebSocketURL(baseURL, websocketPath string) (string, error) {
	base, err := url.ParseRequestURI(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil ||
		(base.Scheme != "http" && base.Scheme != "https") || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || strings.Contains(baseURL, "#") {
		return "", errors.New("base URL must be an absolute http or https URL without credentials, query, or fragment")
	}
	path, err := url.ParseRequestURI(websocketPath)
	if err != nil || path.Scheme != "" || path.Host != "" || path.User != nil || path.ForceQuery || path.Fragment != "" || path.Path == "" || !strings.HasPrefix(path.Path, "/") {
		return "", errors.New("websocket path must be an absolute path without credentials or fragment")
	}
	query := path.Query()
	if len(query) != 1 || len(query["vsn"]) != 1 || query.Get("vsn") != "2.0.0" {
		return "", errors.New("websocket path must contain only vsn=2.0.0")
	}

	scheme := "ws"
	if base.Scheme == "https" {
		scheme = "wss"
	}
	return (&url.URL{
		Scheme:   scheme,
		Host:     base.Host,
		Path:     path.Path,
		RawPath:  path.RawPath,
		RawQuery: path.RawQuery,
	}).String(), nil
}

// Run connects, joins the machine topic, and emits notification hints until
// ctx is cancelled. Transient connection failures reconnect with bounded
// exponential backoff; the only terminal error is ctx.Err().
func (client *Client) Run(ctx context.Context, hints chan<- Hint) error {
	backoff := client.options.reconnectInitial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		dialContext, cancel := context.WithTimeout(ctx, client.options.dialTimeout)
		connection, _, err := websocket.Dial(dialContext, client.websocketURL, &websocket.DialOptions{
			HTTPClient: client.httpClient,
			HTTPHeader: http.Header{"X-Symmetry-Token": []string{client.machineToken}},
		})
		cancel()
		connected := false
		if err == nil {
			connected, err = client.runConnection(ctx, connection, hints)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			backoff = client.options.reconnectInitial
		}
		if !waitForBackoff(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff, client.options.reconnectMaximum)
	}
}

func (client *Client) runConnection(ctx context.Context, connection *websocket.Conn, hints chan<- Hint) (connected bool, err error) {
	connectionContext, cancel := context.WithCancel(ctx)
	frames := make(chan []byte, 1)
	readErrors := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			messageType, value, readErr := connection.Read(connectionContext)
			if readErr != nil {
				select {
				case readErrors <- readErr:
				case <-connectionContext.Done():
				}
				return
			}
			if messageType != websocket.MessageText {
				continue
			}
			select {
			case frames <- value:
			case <-connectionContext.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		connection.CloseNow()
		<-readerDone
	}()

	topic := "daemon:" + client.machineID
	joinRef := client.reference()
	if err := writeFrameWithTimeout(ctx, connection, client.options.writeTimeout, phoenixFrame{JoinRef: joinRef, Ref: joinRef, Topic: topic, Event: "phx_join", Payload: json.RawMessage(`{}`)}); err != nil {
		return false, err
	}

	heartbeats := time.NewTicker(client.options.heartbeatInterval)
	defer heartbeats.Stop()
	heartbeatReply := time.NewTimer(time.Hour)
	heartbeatReply.Stop()
	defer heartbeatReply.Stop()
	var heartbeatReplyC <-chan time.Time
	var heartbeatRef string
	for {
		select {
		case <-ctx.Done():
			return connected, ctx.Err()
		case readErr := <-readErrors:
			return connected, readErr
		case value := <-frames:
			frame, decodeErr := decodeFrame(value)
			if decodeErr != nil {
				continue
			}
			switch frame.Event {
			case "phx_reply":
				if !connected {
					if frame.JoinRef != joinRef || frame.Topic != topic || frame.Ref != joinRef {
						continue
					}
					if !replySucceeded(frame.Payload) {
						return false, errors.New("Phoenix channel join was rejected")
					}
					connected = true
					sendHint(hints, Hint{Type: "connected"})
					continue
				}
				if frame.JoinRef == "" && frame.Topic == "phoenix" && frame.Ref == heartbeatRef {
					if !replySucceeded(frame.Payload) {
						return connected, errors.New("Phoenix heartbeat was rejected")
					}
					heartbeatRef = ""
					heartbeatReply.Stop()
					heartbeatReplyC = nil
				}
			case "phx_error", "phx_close":
				if frame.JoinRef == joinRef && frame.Topic == topic {
					return connected, fmt.Errorf("Phoenix channel %s", frame.Event)
				}
			case "work_available", "command_available", "reconcile_required":
				if !connected || frame.Topic != topic {
					continue
				}
				hint, hintErr := hintFromFrame(frame)
				if hintErr != nil {
					continue
				}
				sendHint(hints, hint)
			}
		case <-heartbeats.C:
			if !connected || heartbeatRef != "" {
				continue
			}
			heartbeatRef = client.reference()
			if writeErr := writeFrameWithTimeout(ctx, connection, client.options.writeTimeout, phoenixFrame{Ref: heartbeatRef, Topic: "phoenix", Event: "heartbeat", Payload: json.RawMessage(`{}`)}); writeErr != nil {
				return connected, writeErr
			}
			heartbeatReply.Reset(client.options.heartbeatTimeout)
			heartbeatReplyC = heartbeatReply.C
		case <-heartbeatReplyC:
			return connected, errors.New("Phoenix heartbeat reply timed out")
		}
	}
}

func (client *Client) reference() string {
	return fmt.Sprintf("%d", client.nextRef.Add(1))
}

func waitForBackoff(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}

type phoenixFrame struct {
	JoinRef string
	Ref     string
	Topic   string
	Event   string
	Payload json.RawMessage
}

func writeFrame(ctx context.Context, connection *websocket.Conn, frame phoenixFrame) error {
	joinRef := any(frame.JoinRef)
	if frame.JoinRef == "" {
		joinRef = nil
	}
	value, err := json.Marshal([]any{joinRef, frame.Ref, frame.Topic, frame.Event, frame.Payload})
	if err != nil {
		return fmt.Errorf("encode Phoenix frame: %w", err)
	}
	if err := connection.Write(ctx, websocket.MessageText, value); err != nil {
		return fmt.Errorf("write Phoenix frame: %w", err)
	}
	return nil
}

func writeFrameWithTimeout(ctx context.Context, connection *websocket.Conn, timeout time.Duration, frame phoenixFrame) error {
	writeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return writeFrame(writeContext, connection, frame)
}

func decodeFrame(value []byte) (phoenixFrame, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(value, &values); err != nil || len(values) != 5 {
		return phoenixFrame{}, errors.New("invalid Phoenix frame")
	}
	var frame phoenixFrame
	if err := json.Unmarshal(values[0], &frame.JoinRef); err != nil {
		return phoenixFrame{}, errors.New("invalid Phoenix frame join reference")
	}
	if err := json.Unmarshal(values[1], &frame.Ref); err != nil {
		return phoenixFrame{}, errors.New("invalid Phoenix frame reference")
	}
	if err := json.Unmarshal(values[2], &frame.Topic); err != nil || frame.Topic == "" {
		return phoenixFrame{}, errors.New("invalid Phoenix frame topic")
	}
	if err := json.Unmarshal(values[3], &frame.Event); err != nil || frame.Event == "" {
		return phoenixFrame{}, errors.New("invalid Phoenix frame event")
	}
	if !json.Valid(values[4]) {
		return phoenixFrame{}, errors.New("invalid Phoenix frame payload")
	}
	frame.Payload = values[4]
	return frame, nil
}

func replySucceeded(payload json.RawMessage) bool {
	var reply struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(payload, &reply) == nil && reply.Status == "ok"
}

func hintFromFrame(frame phoenixFrame) (Hint, error) {
	var payload struct {
		RuntimeID string `json:"runtime_id"`
		CommandID string `json:"command_id"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		return Hint{}, err
	}
	return Hint{
		Type:      frame.Event,
		RuntimeID: payload.RuntimeID,
		CommandID: payload.CommandID,
		Reason:    payload.Reason,
	}, nil
}

func sendHint(hints chan<- Hint, hint Hint) {
	select {
	case hints <- hint:
	default:
	}
}
