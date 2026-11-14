package omp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"golang.org/x/sys/windows"
)

func TestRetainedResumeAcceptsShortPathIdentity(t *testing.T) {
	for _, returnedCanonical := range []bool{false, true} {
		name := "returned-short"
		if returnedCanonical {
			name = "returned-canonical"
		}
		t.Run(name, func(t *testing.T) {
			resume := &harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "native-session"}
			session, process := testSession(t, nil, nil, resume)
			canonical, err := filepath.EvalSymlinks(resume.NativeSessionFilename)
			if err != nil {
				t.Fatal(err)
			}
			longPath, err := windows.UTF16PtrFromString(filepath.Dir(canonical))
			if err != nil {
				t.Fatal(err)
			}
			buffer := make([]uint16, 32768)
			n, err := windows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
			if err != nil {
				t.Skipf("8.3 short path unavailable: %v", err)
			}
			if n == 0 || n >= uint32(len(buffer)) {
				t.Fatalf("GetShortPathName returned invalid length %d", n)
			}
			short := filepath.Join(windows.UTF16ToString(buffer[:n]), filepath.Base(canonical))
			if short == canonical {
				t.Skip("filesystem did not provide an 8.3 alias")
			}
			resolved, err := filepath.EvalSymlinks(short)
			if err != nil || resolved != canonical {
				t.Fatalf("short path %q resolved to %q, %v; want %q", short, resolved, err, canonical)
			}
			t.Logf("retained short path %q; canonical path %q", short, canonical)
			resume.NativeSessionFilename = short
			process.filename = short
			if returnedCanonical {
				process.filename = canonical
			}
			ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
			defer cancel()
			handle, err := session.Open(ctx)
			if err != nil || handle.ID != resume.NativeSessionID || handle.Filename != canonical {
				t.Fatalf("Open=%+v, %v; want the same canonical retained file", handle, err)
			}
			select {
			case <-process.promptSeen:
				t.Fatal("idle retained resume submitted an unadmitted prompt")
			default:
			}
		})
	}
}
