//go:build linux

package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

const gateProductionDomain = "symmetry-linux-gate-production-v1\x00"

// SignGateNeverProducedAuthenticator creates the v1 root-launcher MAC using
// the existing lowercase-hex, 32-byte per-run authority secret.
func SignGateNeverProducedAuthenticator(secretHex, launchToken string, launcher authority.LinuxCgroupProcessIdentity) (string, error) {
	secretKey, err := decodeCanonicalHex(secretHex, authority.SecretBytes)
	if err != nil {
		return "", fmt.Errorf("decode per-run launcher secret: %w", err)
	}
	defer clear(secretKey)
	mac, err := SignGateNeverProducedAuthenticatorKey(secretKey, launchToken, launcher)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(mac[:]), nil
}

// SignGateNeverProducedAuthenticatorKey is used by the transient root core
// with the raw 32-byte secret received on its authenticated control FD.
func SignGateNeverProducedAuthenticatorKey(secretKey []byte, launchToken string, launcher authority.LinuxCgroupProcessIdentity) ([sha256.Size]byte, error) {
	var signature [sha256.Size]byte
	if len(secretKey) != authority.SecretBytes || !canonicalLowerHex(launchToken, authority.TokenBytes) || launcher.Validate() != nil {
		return signature, errors.New("gate-never-created MAC inputs are invalid")
	}
	payload := make([]byte, 0, len(gateProductionDomain)+4+len(launchToken)+4+4+len(launcher.Identity)+4+len(authority.LinuxCgroupGateOutcomeNeverCreated))
	payload = append(payload, gateProductionDomain...)
	payload = appendLengthPrefixed(payload, []byte(launchToken))
	var pid [4]byte
	binary.LittleEndian.PutUint32(pid[:], uint32(launcher.PID))
	payload = append(payload, pid[:]...)
	payload = appendLengthPrefixed(payload, []byte(launcher.Identity))
	payload = appendLengthPrefixed(payload, []byte(authority.LinuxCgroupGateOutcomeNeverCreated))
	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write(payload)
	copy(signature[:], mac.Sum(nil))
	clear(payload)
	return signature, nil
}

// VerifyGateNeverProducedAuthenticator is shared by all Linux architectures
// so the platform's linux-tagged release path cannot drift from the root
// launcher's canonical encoding.
func VerifyGateNeverProducedAuthenticator(secretHex string, result authority.LinuxCgroupGateProductionResult) error {
	if result.ValidateFor(result.LaunchToken, result.Launcher) != nil {
		return errors.New("gate-never-created result shape is invalid")
	}
	want, err := SignGateNeverProducedAuthenticator(secretHex, result.LaunchToken, result.Launcher)
	if err != nil {
		return err
	}
	got, err := decodeCanonicalHex(result.Authenticator, sha256.Size)
	if err != nil {
		return fmt.Errorf("decode gate-never-created authenticator: %w", err)
	}
	wantBytes, _ := hex.DecodeString(want)
	if !hmac.Equal(got, wantBytes) {
		return errors.New("gate-never-created authenticator does not match per-run secret")
	}
	return nil
}

func appendLengthPrefixed(destination, value []byte) []byte {
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
	destination = append(destination, length[:]...)
	return append(destination, value...)
}

func decodeCanonicalHex(value string, expectedBytes int) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != expectedBytes || hex.EncodeToString(decoded) != value {
		return nil, errors.New("value is not canonical lowercase hexadecimal of the expected length")
	}
	return decoded, nil
}

func canonicalLowerHex(value string, expectedBytes int) bool {
	decoded, err := decodeCanonicalHex(value, expectedBytes)
	clear(decoded)
	return err == nil
}
