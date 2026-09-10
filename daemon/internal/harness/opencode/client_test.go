package opencode

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientRequiresExplicitNumericLoopbackBasicAuth(t *testing.T) {
	for _, config := range []Config{
		{BaseURL: "http://localhost:4096", Username: "opencode", Password: "secret"},
		{BaseURL: "https://127.0.0.1:4096", Username: "opencode", Password: "secret"},
		{BaseURL: "http://127.0.0.1", Username: "opencode", Password: "secret"},
		{BaseURL: "http://127.0.0.1:0", Username: "opencode", Password: "secret"},
		{BaseURL: "http://127.0.0.1:70000", Username: "opencode", Password: "secret"},
		{BaseURL: "http://192.168.1.1:4096", Username: "opencode", Password: "secret"},
		{BaseURL: "http://127.0.0.1:4096/api", Username: "opencode", Password: "secret"},
		{BaseURL: "http://127.0.0.1:4096", Username: "", Password: "secret"},
		{BaseURL: "http://127.0.0.1:4096", Username: "opencode", Password: ""},
	} {
		if _, err := NewClient(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewClient(%+v) error = %v, want ErrInvalidConfig", config, err)
		}
	}
	client, err := NewClient(Config{BaseURL: "http://[::1]:4096", Username: "opencode", Password: "secret"})
	if err != nil || client == nil {
		t.Fatalf("NewClient IPv6 loopback = %v, %v", client, err)
	}
}

func TestClientHealthUsesExactEndpointAndBasicAuth(t *testing.T) {
	server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
		assertAuth(t, request)
		if request.Method != http.MethodGet || request.URL.Path != "/api/health" || request.Header.Get("Content-Type") != "" {
			t.Fatalf("request = %s %s content-type=%q", request.Method, request.URL.Path, request.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(writer, `{"healthy":true}`)
	})
	if err := newClient(t, server).Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestClientHealthRejectsUnhealthyRedirectAndOversizedResponse(t *testing.T) {
	t.Run("unhealthy", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, `{"healthy":false}`)
		})
		if err := newClient(t, server).Health(context.Background()); !errors.Is(err, ErrInvalidHealth) {
			t.Fatalf("Health() error = %v, want ErrInvalidHealth", err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "http://127.0.0.1:1", http.StatusFound)
		})
		if err := newClient(t, server).Health(context.Background()); !errors.Is(err, ErrUnexpectedStatus) {
			t.Fatalf("Health() error = %v, want ErrUnexpectedStatus", err)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, `{"healthy":true}`)
		})
		client := newClient(t, server)
		client.maxBodyBytes = 2
		if err := client.Health(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("Health() error = %v, want ErrResponseTooLarge", err)
		}
	})
}

func TestClientCreatesBoundSessionAndDoesNotRetryPOST(t *testing.T) {
	requests := 0
	server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
		requests++
		assertAuth(t, request)
		if request.Method != http.MethodPost || request.URL.Path != "/api/session" || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s content-type=%q", request.Method, request.URL.Path, request.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(writer, `{"data":{"id":"ses_native_1","projectID":"prj_1","location":{"directory":"Q:\\repo","workspaceID":"wrk_1"}}}`)
	})
	request := CreateSessionRequest{ID: "ses_native_1", Location: SessionLocation{Directory: `Q:\repo`, WorkspaceID: "wrk_1"}}
	info, err := newClient(t, server).CreateSession(context.Background(), request)
	if err != nil || info.ID != request.ID {
		t.Fatalf("CreateSession() = %+v, %v", info, err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one non-retryable POST", requests)
	}
}

func TestClientRejectsSessionIdentityAndPayloadViolations(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "wrong id", body: `{"data":{"id":"ses_other","projectID":"prj_1","location":{"directory":"Q:\\repo"}}}`, want: ErrIdentityMismatch},
		{name: "wrong directory", body: `{"data":{"id":"ses_native_1","projectID":"prj_1","location":{"directory":"Q:\\other"}}}`, want: ErrIdentityMismatch},
		{name: "missing project", body: `{"data":{"id":"ses_native_1","location":{"directory":"Q:\\repo"}}}`, want: ErrInvalidSession},
		{name: "duplicate data", body: `{"data":{},"data":{}}`, want: ErrInvalidSession},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newServer(t, func(writer http.ResponseWriter, request *http.Request) { _, _ = io.WriteString(writer, test.body) })
			_, err := newClient(t, server).CreateSession(context.Background(), CreateSessionRequest{ID: "ses_native_1", Location: SessionLocation{Directory: `Q:\repo`}})
			if !errors.Is(err, test.want) {
				t.Fatalf("CreateSession() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestClientPromptValidatesAdmissionOnly(t *testing.T) {
	server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
		assertAuth(t, request)
		if request.Method != http.MethodPost || request.URL.Path != "/api/session/ses_native_1/prompt" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != `{"id":"msg_native_1","prompt":{"text":"protocol probe"},"delivery":"steer","resume":false}` {
			t.Fatalf("prompt body = %q, %v", body, err)
		}
		_, _ = io.WriteString(writer, `{"data":{"admittedSeq":1,"id":"msg_native_1","sessionID":"ses_native_1","prompt":{"text":"protocol probe"},"delivery":"steer","timeCreated":1789052190936,"promotedSeq":2}}`)
	})
	admission, err := newClient(t, server).Prompt(context.Background(), "ses_native_1", PromptRequest{ID: "msg_native_1", Text: "protocol probe", Delivery: "steer"})
	if err != nil || admission.AdmittedSeq != 1 || admission.PromotedSeq == nil || *admission.PromotedSeq != 2 {
		t.Fatalf("Prompt() = %+v, %v", admission, err)
	}
}

func TestClientPromptRejectsAdmissionMismatchAndContextCancellation(t *testing.T) {
	t.Run("mismatch", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, `{"data":{"admittedSeq":1,"id":"msg_other","sessionID":"ses_native_1","prompt":{"text":"protocol probe"},"delivery":"steer","timeCreated":1}}`)
		})
		_, err := newClient(t, server).Prompt(context.Background(), "ses_native_1", PromptRequest{ID: "msg_native_1", Text: "protocol probe", Delivery: "steer"})
		if !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("Prompt() error = %v, want ErrIdentityMismatch", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
		context, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := newClient(t, server).Prompt(context, "ses_native_1", PromptRequest{ID: "msg_native_1", Text: "protocol probe", Delivery: "steer"})
		if !errors.Is(err, context.Err()) {
			t.Fatalf("Prompt() error = %v, want context cancelled", err)
		}
	})
}

func TestClientInterruptRequiresExactNoContent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			assertAuth(t, request)
			if request.Method != http.MethodPost || request.URL.Path != "/api/session/ses_native_1/interrupt" || request.ContentLength != 0 {
				t.Fatalf("request = %s %s length=%d", request.Method, request.URL.Path, request.ContentLength)
			}
			writer.WriteHeader(http.StatusNoContent)
		})
		if err := newClient(t, server).Interrupt(context.Background(), "ses_native_1"); err != nil {
			t.Fatalf("Interrupt() error = %v", err)
		}
	})
	t.Run("body", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
			_, _ = io.WriteString(writer, "unexpected")
		})
		if err := newClient(t, server).Interrupt(context.Background(), "ses_native_1"); err != nil {
			// net/http correctly strips a 204 body, so an exact status is the observable contract.
			t.Fatalf("Interrupt() error = %v", err)
		}
	})
}

func TestClientOpensAuthenticatedSSEWithExclusiveCursor(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			assertAuth(t, request)
			if request.Method != http.MethodGet || request.URL.Path != "/api/event" || request.URL.RawQuery != "" || request.Header.Get("Accept") != "text/event-stream" {
				t.Fatalf("request = %s %s?%s accept=%q", request.Method, request.URL.Path, request.URL.RawQuery, request.Header.Get("Accept"))
			}
			writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			_, _ = io.WriteString(writer, "data: {\"id\":\"evt_connected_1\",\"type\":\"server.connected\",\"data\":{}}\n\n")
		})
		body, err := newClient(t, server).OpenGlobalEvents(context.Background())
		if err != nil {
			t.Fatalf("OpenGlobalEvents() error = %v", err)
		}
		defer body.Close()
		decoded, err := io.ReadAll(body)
		if err != nil || !strings.Contains(string(decoded), "server.connected") {
			t.Fatalf("stream = %q, %v", decoded, err)
		}
	})
	t.Run("session", func(t *testing.T) {
		server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
			assertAuth(t, request)
			if request.URL.Path != "/api/session/ses_native_1/event" || request.URL.Query().Get("after") != "9" || request.URL.Query().Encode() != "after=9" {
				t.Fatalf("request URL = %s", request.URL)
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(writer, ": heartbeat\n\n")
		})
		body, err := newClient(t, server).OpenSessionEvents(context.Background(), "ses_native_1", 9)
		if err != nil {
			t.Fatalf("OpenSessionEvents() error = %v", err)
		}
		if err := body.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
}

func TestClientRejectsUnexpectedSSEResponse(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		contentType string
		want        error
	}{
		{name: "status", status: http.StatusUnauthorized, contentType: "application/json", want: ErrUnexpectedStatus},
		{name: "content type", status: http.StatusOK, contentType: "application/json", want: ErrMalformedResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				writer.WriteHeader(test.status)
			})
			body, err := newClient(t, server).OpenGlobalEvents(context.Background())
			if body != nil || !errors.Is(err, test.want) {
				t.Fatalf("OpenGlobalEvents() = %v, %v; want nil, %v", body, err, test.want)
			}
		})
	}
}

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(handler))
}

func newClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	if parsed.Hostname() == "127.0.0.1" {
		// httptest currently uses loopback IPv4, but preserve strict construction.
	} else if parsed.Hostname() == "::1" {
	} else {
		t.Fatalf("httptest host = %q, want numeric loopback", parsed.Hostname())
	}
	client, err := NewClient(Config{BaseURL: server.URL, Username: "opencode", Password: "secret", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func assertAuth(t *testing.T, request *http.Request) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:secret"))
	if got := request.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestParseLoopbackURLPreservesNoQueryOrCredentials(t *testing.T) {
	for _, input := range []string{
		"http://opencode:secret@127.0.0.1:4096",
		"http://127.0.0.1:4096?redirect=1",
		"http://127.0.0.1:4096#fragment",
	} {
		if _, err := parseLoopbackURL(input); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("parseLoopbackURL(%q) error = %v", input, err)
		}
	}
}

func TestClientRejectsNonSuccessAPIStatus(t *testing.T) {
	server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"_tag":"UnauthorizedError"}`)
	})
	if err := newClient(t, server).Health(context.Background()); !errors.Is(err, ErrUnexpectedStatus) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Health() error = %v, want 401 unexpected status", err)
	}
}
