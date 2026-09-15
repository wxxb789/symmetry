package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	piProviderActionPath = "/api/v1/provider-actions"

	piProviderActionFailureControlCanceled  = "control_action_canceled"
	piProviderActionFailureControlResponse  = "control_response_invalid"
	piProviderActionFailureControlUnknown   = "control_action_unknown"
	piProviderActionFailureControlServer    = "control_server_unknown"
	piProviderActionFailureControlTransport = "control_transport_unknown"
	piProviderActionFailureControlPanic     = "control_action_panic"
	piProviderActionFailureControlResult    = "control_result_unsafe"
	piProviderActionFailureControlInvalid   = "control_action_invalid"
	piProviderActionMaxResultBytes          = 64 << 10
)

var (
	errPiProviderActionAccessInvalid      = errors.New("provider access is invalid")
	errPiProviderActionControlUnavailable = errors.New("provider action control client is unavailable")
)

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
