// Package linuxreadback carries the bounded root readback channel used by the
// Linux acceptance tests to observe B without granting A any capability.
package linuxreadback

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ChannelEnv = "SYMMETRY_LINUX_OWNER_READBACK_CHANNEL"

	requestSuffix  = ".req.json"
	responseSuffix = ".resp.json"

	maxRequestBytes  = 64 << 10
	maxResponseBytes = 1 << 20
	maxEnvironBytes  = 256 << 10
	maxDescriptors   = 4096
	defaultTimeout   = 30 * time.Second
	pollInterval     = 10 * time.Millisecond
)

type RefusalStage string

const (
	RefusalRequest       RefusalStage = "request"
	RefusalTargetMissing RefusalStage = "target_missing"
	RefusalIdentity      RefusalStage = "identity"
	RefusalCredentials   RefusalStage = "credentials"
	RefusalLeaf          RefusalStage = "leaf"
	RefusalExecutable    RefusalStage = "executable"
	RefusalRead          RefusalStage = "read"
	RefusalLimit         RefusalStage = "limit"
)

var ErrRefused = errors.New("linux readback refused")

type CaptureError struct {
	Stage  RefusalStage
	Reason string
}

func (failure *CaptureError) Error() string {
	if failure == nil {
		return ErrRefused.Error()
	}
	return fmt.Sprintf("%v at stage %s: %s", ErrRefused, failure.Stage, failure.Reason)
}

func (failure *CaptureError) Is(target error) bool {
	return target == ErrRefused
}

func RefusalStageOf(err error) (RefusalStage, bool) {
	var failure *CaptureError
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.Stage, true
}

type Want struct {
	PID         int      `json:"pid"`
	StartTime   uint64   `json:"start_time"`
	BootID      string   `json:"boot_id"`
	Identity    string   `json:"identity,omitempty"`
	Leaf        string   `json:"leaf"`
	Executable  string   `json:"executable"`
	ExeDevice   uint64   `json:"exe_device"`
	ExeInode    uint64   `json:"exe_inode"`
	Environ     bool     `json:"environ"`
	Descriptors bool     `json:"descriptors"`
	Namespaces  []string `json:"namespaces,omitempty"`
}

type Descriptor struct {
	Number int    `json:"number"`
	Target string `json:"target"`
}

type Namespace struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type Capture struct {
	PID         int
	StartTime   uint64
	BootID      string
	Identity    string
	Executable  string
	ExeDevice   uint64
	ExeInode    uint64
	Environ     []byte
	Descriptors []Descriptor
	Namespaces  map[string]Namespace
}

type ServeOptions struct {
	Channel     string
	LeafRoot    string
	AUID        uint32
	AGID        uint32
	BUID        uint32
	BGID        uint32
	MaxRequests int
	ReadyFile   string
	StopFile    string
	Log         io.Writer
}

type responseBody struct {
	Version     int                  `json:"version"`
	Nonce       string               `json:"nonce"`
	Stage       RefusalStage         `json:"stage,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	PID         int                  `json:"pid,omitempty"`
	StartTime   uint64               `json:"start_time,omitempty"`
	BootID      string               `json:"boot_id,omitempty"`
	Identity    string               `json:"identity,omitempty"`
	Executable  string               `json:"executable,omitempty"`
	ExeDevice   uint64               `json:"exe_device,omitempty"`
	ExeInode    uint64               `json:"exe_inode,omitempty"`
	Environ     []byte               `json:"environ"`
	Descriptors []Descriptor         `json:"descriptors,omitempty"`
	Namespaces  map[string]Namespace `json:"namespaces,omitempty"`
}

// ChannelFromEnv returns the configured channel, or a typed error naming the
// missing environment variable so a fixture-less run fails instead of skipping.
func ChannelFromEnv() (string, error) {
	channel := os.Getenv(ChannelEnv)
	if channel == "" {
		return "", fmt.Errorf("%s is unset: the root readback reader must be started by the acceptance fixture", ChannelEnv)
	}
	if err := validateChannelPath(channel); err != nil {
		return "", err
	}
	return channel, nil
}

func validateChannelPath(channel string) error {
	if !filepath.IsAbs(channel) || filepath.Clean(channel) != channel || channel == "/" {
		return errors.New("readback channel must be a normalized absolute non-root path")
	}
	info, err := os.Stat(channel)
	if err != nil {
		return fmt.Errorf("stat readback channel: %w", err)
	}
	if !info.IsDir() {
		return errors.New("readback channel is not a directory")
	}
	return nil
}

func requestName(nonce string) string  { return nonce + requestSuffix }
func responseName(nonce string) string { return nonce + responseSuffix }

func validNonce(nonce string) bool {
	if len(nonce) != 32 || strings.Trim(nonce, "0123456789abcdef") != "" {
		return false
	}
	return true
}

// Read writes one request and returns the reader's capture or its typed
// refusal. A reader that never answers is an error, never a skipped assertion.
func Read(channel string, want Want, timeout time.Duration) (Capture, error) {
	if err := validateChannelPath(channel); err != nil {
		return Capture{}, err
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	nonce, err := newNonce()
	if err != nil {
		return Capture{}, err
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		return Capture{}, fmt.Errorf("encode readback request: %w", err)
	}
	if len(encoded) == 0 || len(encoded) > maxRequestBytes {
		return Capture{}, fmt.Errorf("readback request size %d is outside its bound", len(encoded))
	}
	tempPath := filepath.Join(channel, requestName(nonce)+".tmp")
	finalPath := filepath.Join(channel, requestName(nonce))
	temp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Capture{}, fmt.Errorf("create readback request: %w", err)
	}
	writeErr := func() error {
		if _, err := temp.Write(encoded); err != nil {
			return err
		}
		return temp.Close()
	}()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return Capture{}, fmt.Errorf("write readback request: %w", writeErr)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return Capture{}, fmt.Errorf("publish readback request: %w", err)
	}
	deadline := time.Now().Add(timeout)
	responsePath := filepath.Join(channel, responseName(nonce))
	for {
		body, err := readResponse(responsePath)
		if err == nil {
			_ = os.Remove(responsePath)
			return decodeCapture(nonce, want, body)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Capture{}, err
		}
		if !time.Now().Before(deadline) {
			return Capture{}, fmt.Errorf("root readback reader did not answer %s within %s", finalPath, timeout)
		}
		time.Sleep(pollInterval)
	}
}

func newNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate readback nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func readResponse(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxResponseBytes {
		return nil, errors.New("readback response exceeds its bound")
	}
	return contents, nil
}

func decodeCapture(nonce string, want Want, body []byte) (Capture, error) {
	var response responseBody
	if err := json.Unmarshal(body, &response); err != nil {
		return Capture{}, fmt.Errorf("decode readback response: %w", err)
	}
	if response.Version != 1 || response.Nonce != nonce {
		return Capture{}, errors.New("readback response does not bind this request's nonce and version")
	}
	if response.Stage != "" {
		return Capture{}, &CaptureError{Stage: response.Stage, Reason: response.Reason}
	}
	if response.PID != want.PID || response.StartTime != want.StartTime {
		return Capture{}, errors.New("readback capture does not bind the requested PID and start time")
	}
	capture := Capture{
		PID: response.PID, StartTime: response.StartTime, BootID: response.BootID,
		Identity: response.Identity, Executable: response.Executable,
		ExeDevice: response.ExeDevice, ExeInode: response.ExeInode,
		Environ: response.Environ, Descriptors: response.Descriptors, Namespaces: response.Namespaces,
	}
	if want.Environ && capture.Environ == nil {
		capture.Environ = []byte{}
	}
	return capture, nil
}

// CheckEnvironment compares the captured bytes with the exact expected vector,
// including the trailing NUL, exactly as the raw /proc/<pid>/environ read did.
func CheckEnvironment(capture Capture, expected []string) error {
	wanted := []byte(strings.Join(expected, "\x00"))
	if len(expected) != 0 {
		wanted = append(wanted, 0)
	}
	if !bytes.Equal(capture.Environ, wanted) {
		return fmt.Errorf("process environment differs from the exact expected vector: bytes=%d want=%d", len(capture.Environ), len(wanted))
	}
	return nil
}
