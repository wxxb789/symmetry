package pi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestProviderBridgeRequiresExactNativePeerIdentity(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("loopback TCP peer ownership is supported only on Linux and Windows")
	}
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "run-peer-identity",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, Result: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	endpoint := startTestProviderBridge(t, bridge)
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"peer-1","input":{"title":"same"}}`)

	unbound := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body))
	if unbound.StatusCode != http.StatusServiceUnavailable {
		defer unbound.Body.Close()
		t.Fatalf("unbound status = %d, want %d", unbound.StatusCode, http.StatusServiceUnavailable)
	}
	_ = unbound.Body.Close()

	if err := bridge.BindProcess(os.Getpid(), identity); err != nil {
		t.Fatalf("BindProcess() error = %v", err)
	}
	if err := bridge.BindProcess(os.Getpid(), identity); err != nil {
		t.Fatalf("BindProcess() exact replay error = %v", err)
	}
	verified := decodeProviderBridgePayload(t, doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body)))
	if verified.Outcome != ProviderBridgeOutcomeSucceeded {
		t.Fatalf("verified response = %#v", verified)
	}
	if err := bridge.BindProcess(os.Getpid(), identity+"-changed"); err == nil {
		t.Fatal("BindProcess() replaced the authorized process identity")
	}
}

func TestProviderBridgeRejectsConnectionOwnedByDifferentProcessIdentity(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("loopback TCP peer ownership is supported only on Linux and Windows")
	}
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "run-peer-rejected",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			t.Fatal("unauthorized peer reached provider executor")
			return ProviderBridgeResponse{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	endpoint := startTestProviderBridge(t, bridge)
	if err := bridge.BindProcess(os.Getpid(), identity+"-changed"); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"resource.sync","action_key":"peer-rejected","input":{}}`)
	response := doProviderBridgeRequest(t, newProviderBridgeRequest(t, endpoint, http.MethodPost, endpoint.URL, body))
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}
