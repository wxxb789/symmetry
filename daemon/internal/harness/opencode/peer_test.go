//go:build linux || windows

package opencode

import (
	"context"
	"io"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestLegacyWindowsPeerVerifierAllowsOwnedHTTPConnection(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux exact peer ownership is exercised through the launch-retained v2 owner test")
	}
	server := newServer(t, func(writer http.ResponseWriter, request *http.Request) {
		assertAuth(t, request)
		_, _ = io.WriteString(writer, `{"healthy":true}`)
	})
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []*Adapter{NewAdapter(), NewAdapterWithRunner("opencode", nil)} {
		client, err := NewClient(Config{
			BaseURL: server.URL, Username: "opencode", Password: "secret",
			VerifyConnection: adapter.newPeerVerifier(os.Getpid(), identity, nil),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer client.httpClient.CloseIdleConnections()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Health(ctx); err != nil {
			t.Fatalf("production peer verifier rejected its owned HTTP connection: %v", err)
		}
	}
}

func TestLegacyWindowsPeerVerifierRejectsWrongIdentityBeforeWriting(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux exact peer rejection is exercised through launch-retained v2 owner tests")
	}
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []*Adapter{NewAdapter(), NewAdapterWithRunner("opencode", nil)} {
		assertRejectedConnectionHasNoBytes(t, adapter.newPeerVerifier(os.Getpid(), identity+"-stale", nil), nil)
	}
}
