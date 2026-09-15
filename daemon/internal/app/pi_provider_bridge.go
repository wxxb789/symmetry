package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const piProviderBridgeCloseTimeout = 5 * time.Second

const (
	piProviderActionPath = "/api/v1/provider-actions"

	piProviderActionFailureControlCanceled  = "control_action_canceled"
	piProviderActionFailureControlResponse  = "control_response_invalid"
	piProviderActionFailureControlInFlight  = "control_action_in_flight"
	piProviderActionFailureControlUnknown   = "control_action_unknown"
	piProviderActionFailureControlServer    = "control_server_unknown"
	piProviderActionFailureControlTransport = "control_transport_unknown"
	piProviderActionFailureControlPanic     = "control_action_panic"
	piProviderActionFailureControlResult    = "control_result_unsafe"
	piProviderActionFailureControlInvalid   = "control_action_invalid"
	piProviderActionFailureLocalConflict    = "local_idempotency_conflict"
	piProviderActionFailureLocalPersistence = "local_persistence_unknown"
	piProviderActionMaxResultBytes          = 64 << 10
)

// newDurablePiProviderBridgeExecutor surrounds the Control mapper with the
// run-journal commit/ack boundary required before a native bridge can use it.
// Provider input is represented only by a SHA-256 digest in local state.
func newDurablePiProviderBridgeExecutor(controlAPI ControlAPI, store *state.Store, key state.RunKey, access protocol.ProviderAccess) (pi.ExecuteProviderAction, error) {
	if store == nil || strings.TrimSpace(key.RunID) == "" || key.Generation <= 0 {
		return nil, errPiProviderActionStoreUnavailable
	}
	mapped, err := newPiProviderBridgeExecutor(controlAPI, access)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, actionID string, request pi.ProviderBridgeRequest) (pi.ProviderBridgeResponse, error) {
		intent, err := piProviderActionIntent(actionID, request)
		if err != nil {
			return piProviderActionUnknown(piProviderActionFailureControlInvalid), nil
		}
		journal, dispatch, err := store.PrepareProviderAction(key, intent)
		if err != nil {
			if errors.Is(err, state.ErrProviderActionConflict) {
				return pi.ProviderBridgeResponse{Outcome: pi.ProviderBridgeOutcomeFailed, FailureCode: piProviderActionFailureLocalConflict}, nil
			}
			_, _ = store.RecoverProviderAction(key, intent.ActionID, intent.RequestDigest, piProviderActionFailureLocalPersistence)
			return piProviderActionUnknown(piProviderActionFailureLocalPersistence), nil
		}
		if !dispatch {
			stored, ok := findPiProviderActionIntent(journal, intent.ActionID)
			if !ok || stored.Outcome == "" {
				_, _ = store.RecoverProviderAction(key, intent.ActionID, intent.RequestDigest, piProviderActionFailureLocalPersistence)
				return piProviderActionUnknown(piProviderActionFailureLocalPersistence), nil
			}
			return piProviderActionResponseFromIntent(stored), nil
		}

		response, err := mapped(ctx, actionID, request)
		if err != nil {
			response = piProviderActionUnknown(piProviderActionFailureControlUnknown)
		}
		completed := intent
		completed.Outcome = string(response.Outcome)
		completed.Result = append(json.RawMessage(nil), response.Result...)
		completed.FailureCode = response.FailureCode
		if _, persistErr := store.CompleteProviderAction(key, completed); persistErr != nil {
			_, _ = store.RecoverProviderAction(key, intent.ActionID, intent.RequestDigest, piProviderActionFailureLocalPersistence)
			return piProviderActionUnknown(piProviderActionFailureLocalPersistence), nil
		}
		return response, nil
	}, nil
}

func piProviderActionIntent(actionID string, request pi.ProviderBridgeRequest) (state.ProviderActionIntent, error) {
	encoded, err := json.Marshal(struct {
		ResourceID string          `json:"resource_id"`
		Operation  string          `json:"operation"`
		ActionKey  string          `json:"action_key"`
		Input      json.RawMessage `json:"input"`
	}{
		ResourceID: request.ResourceID,
		Operation:  request.Operation,
		ActionKey:  request.ActionKey,
		Input:      request.Input,
	})
	if err != nil {
		return state.ProviderActionIntent{}, err
	}
	canonical, err := protocol.CanonicalizeJSON(encoded)
	if err != nil {
		return state.ProviderActionIntent{}, err
	}
	digest := sha256.Sum256(canonical)
	return state.ProviderActionIntent{
		ActionID:      actionID,
		ActionKey:     request.ActionKey,
		RequestDigest: fmt.Sprintf("%x", digest),
		ResourceID:    request.ResourceID,
		Operation:     request.Operation,
	}, nil
}

func findPiProviderActionIntent(journal state.RunJournal, actionID string) (state.ProviderActionIntent, bool) {
	for _, intent := range journal.ProviderActionIntents {
		if intent.ActionID == actionID {
			return intent, true
		}
	}
	return state.ProviderActionIntent{}, false
}

func piProviderActionResponseFromIntent(intent state.ProviderActionIntent) pi.ProviderBridgeResponse {
	switch intent.Outcome {
	case state.ProviderActionOutcomeSucceeded:
		return pi.ProviderBridgeResponse{Outcome: pi.ProviderBridgeOutcomeSucceeded, Result: append(json.RawMessage(nil), intent.Result...)}
	case state.ProviderActionOutcomeFailed:
		return pi.ProviderBridgeResponse{Outcome: pi.ProviderBridgeOutcomeFailed, FailureCode: intent.FailureCode}
	case state.ProviderActionOutcomeUnknown:
		return pi.ProviderBridgeResponse{Outcome: pi.ProviderBridgeOutcomeUnknown, Result: append(json.RawMessage(nil), intent.Result...), FailureCode: intent.FailureCode}
	default:
		return piProviderActionUnknown(piProviderActionFailureLocalPersistence)
	}
}

var (
	errPiProviderActionAccessInvalid      = errors.New("provider access is invalid")
	errPiProviderActionControlUnavailable = errors.New("provider action control client is unavailable")
	errPiProviderActionStoreUnavailable   = errors.New("provider action state store is unavailable")
)

func (daemon *daemon) preparePiProviderBridge(ctx context.Context, key state.RunKey, kind harness.Kind, access *protocol.ProviderAccess) (*harness.ProviderBridgeLaunch, error) {
	if access == nil {
		return nil, nil
	}
	if daemon == nil || daemon.store == nil || kind != harness.KindPi {
		return nil, errors.New("pi provider bridge prerequisites are unavailable")
	}
	if ctx == nil {
		return nil, errors.New("pi provider bridge context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := daemon.store.RecoverProviderActions(key, "daemon_restart_unknown"); err != nil {
		return nil, fmt.Errorf("recover pi provider actions: %w", err)
	}
	executor, err := newDurablePiProviderBridgeExecutor(daemon.control, daemon.store, key, *access)
	if err != nil {
		return nil, fmt.Errorf("create pi provider bridge executor: %w", err)
	}
	extensionRoot, err := filepath.Abs(filepath.Join(daemon.config.StateDir, "generated", "pi", pi.TestedVersion))
	if err != nil {
		return nil, errors.New("resolve pi provider bridge extension directory")
	}
	extension, err := pi.MaterializeProviderBridgeExtension(extensionRoot, access.Grants)
	if err != nil {
		return nil, fmt.Errorf("materialize pi provider bridge extension: %w", err)
	}
	bridge, err := pi.NewProviderBridge(pi.ProviderBridgeOptions{
		RunID:               key.RunID,
		Grants:              access.Grants,
		Execute:             executor,
		RequirePeerIdentity: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create pi provider bridge: %w", err)
	}
	endpoint, err := bridge.Start(ctx)
	if err != nil {
		closeContext, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), piProviderBridgeCloseTimeout)
		closeErr := bridge.Close(closeContext)
		closeCancel()
		return nil, errors.Join(fmt.Errorf("start pi provider bridge: %w", err), closeErr)
	}
	return &harness.ProviderBridgeLaunch{
		URL:             endpoint.URL,
		Nonce:           endpoint.Nonce,
		ExtensionPath:   extension.Path,
		ExtensionSHA256: extension.SHA256,
		Lifecycle:       bridge,
	}, nil
}

// providerActionControlAPI is intentionally optional. Existing ControlAPI
// implementations do not need to grow a provider-action method before the
// capability is wired into a native adapter.
type providerActionControlAPI interface {
	ExecuteProviderAction(
		context.Context,
		protocol.ProviderAccess,
		string,
		string,
		string,
		json.RawMessage,
	) (control.ProviderActionResponse, error)
}

// newPiProviderBridgeExecutor binds one claim-scoped provider capability to a
// local Pi bridge callback. The access value is copied at construction and on
// every call so neither the caller nor the optional Control implementation can
// mutate the capability used by a later action.
func newPiProviderBridgeExecutor(controlAPI ControlAPI, access protocol.ProviderAccess) (pi.ExecuteProviderAction, error) {
	if err := validatePiProviderActionAccess(access); err != nil {
		return nil, errPiProviderActionAccessInvalid
	}

	optional, ok := controlAPI.(providerActionControlAPI)
	if !ok || isNilProviderActionControl(optional) {
		return nil, errPiProviderActionControlUnavailable
	}

	immutableAccess := clonePiProviderActionAccess(access)
	return func(
		ctx context.Context,
		actionID string,
		request pi.ProviderBridgeRequest,
	) (result pi.ProviderBridgeResponse, err error) {
		result = piProviderActionUnknown(piProviderActionFailureControlUnknown)
		defer func() {
			if recover() != nil {
				result = piProviderActionUnknown(piProviderActionFailureControlPanic)
				err = nil
			}
		}()

		if ctx == nil {
			return piProviderActionUnknown(piProviderActionFailureControlUnknown), nil
		}
		if ctx.Err() != nil {
			return piProviderActionUnknown(piProviderActionFailureControlCanceled), nil
		}

		response, err := optional.ExecuteProviderAction(
			ctx,
			clonePiProviderActionAccess(immutableAccess),
			actionID,
			request.ResourceID,
			request.Operation,
			append(json.RawMessage(nil), request.Input...),
		)
		if ctx.Err() != nil {
			return piProviderActionUnknown(piProviderActionFailureControlCanceled), nil
		}
		if err != nil {
			return mapPiProviderActionError(err), nil
		}
		return mapPiProviderActionResponse(response, immutableAccess.Token), nil
	}, nil
}

func validatePiProviderActionAccess(access protocol.ProviderAccess) error {
	if access.Path != piProviderActionPath || strings.TrimSpace(access.Token) == "" || len(access.Grants) == 0 {
		return errPiProviderActionAccessInvalid
	}
	return nil
}

func clonePiProviderActionAccess(access protocol.ProviderAccess) protocol.ProviderAccess {
	grants := access.Grants
	access.Grants = make([]protocol.ProviderGrant, len(grants))
	for index, grant := range grants {
		access.Grants[index] = grant
		access.Grants[index].Operations = append([]string(nil), grant.Operations...)
	}
	return access
}

func isNilProviderActionControl(value providerActionControlAPI) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func mapPiProviderActionResponse(response control.ProviderActionResponse, token string) pi.ProviderBridgeResponse {
	safeResult, safe := cloneSafePiProviderActionResult(response.Result, token)
	switch response.Outcome {
	case control.ProviderActionSucceeded:
		if !safe {
			return piProviderActionUnknown(piProviderActionFailureControlResult)
		}
		return pi.ProviderBridgeResponse{
			Outcome: pi.ProviderBridgeOutcomeSucceeded,
			Result:  safeResult,
		}
	case control.ProviderActionUnknown:
		if !safe {
			return piProviderActionUnknown(piProviderActionFailureControlResult)
		}
		return pi.ProviderBridgeResponse{
			Outcome:     pi.ProviderBridgeOutcomeUnknown,
			Result:      safeResult,
			FailureCode: piProviderActionFailureControlUnknown,
		}
	default:
		return piProviderActionUnknown(piProviderActionFailureControlInvalid)
	}
}

func cloneSafePiProviderActionResult(result json.RawMessage, token string) (json.RawMessage, bool) {
	if len(result) == 0 || len(result) > piProviderActionMaxResultBytes || !json.Valid(result) {
		return nil, false
	}
	trimmed := bytes.TrimSpace(result)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return nil, false
	}
	if token != "" && (bytes.Contains(result, []byte(token)) || jsonResultContainsToken(trimmed, token)) {
		return nil, false
	}
	return append(json.RawMessage(nil), result...), true
}

func jsonResultContainsToken(result []byte, token string) bool {
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		return false
	}
	return jsonValueContainsToken(value, token)
}

func jsonValueContainsToken(value any, token string) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(value, token)
	case []any:
		for _, item := range value {
			if jsonValueContainsToken(item, token) {
				return true
			}
		}
	case map[string]any:
		for key, item := range value {
			if strings.Contains(key, token) || jsonValueContainsToken(item, token) {
				return true
			}
		}
	}
	return false
}

func mapPiProviderActionError(err error) pi.ProviderBridgeResponse {
	if err == nil {
		return piProviderActionUnknown(piProviderActionFailureControlUnknown)
	}

	var apiError *control.APIError
	if errors.As(err, &apiError) && apiError != nil {
		switch {
		case apiError.Code == control.StateConflict:
			return piProviderActionUnknown(piProviderActionFailureControlInFlight)
		case apiError.StatusCode >= http.StatusBadRequest && apiError.StatusCode < http.StatusInternalServerError:
			return pi.ProviderBridgeResponse{
				Outcome:     pi.ProviderBridgeOutcomeFailed,
				FailureCode: "control_rejected_" + strconv.Itoa(apiError.StatusCode),
			}
		case apiError.StatusCode >= http.StatusInternalServerError && apiError.StatusCode < 600:
			return piProviderActionUnknown(piProviderActionFailureControlServer)
		default:
			return piProviderActionUnknown(piProviderActionFailureControlUnknown)
		}
	}

	var responseError *control.ResponseError
	if errors.As(err, &responseError) && responseError != nil {
		return piProviderActionUnknown(piProviderActionFailureControlResponse)
	}
	return piProviderActionUnknown(piProviderActionFailureControlTransport)
}

func piProviderActionUnknown(failureCode string) pi.ProviderBridgeResponse {
	return pi.ProviderBridgeResponse{
		Outcome:     pi.ProviderBridgeOutcomeUnknown,
		FailureCode: failureCode,
	}
}
