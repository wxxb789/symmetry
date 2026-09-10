//go:build windows

package state

import "testing"

func TestWindowsGoalSessionPathsUseProtectedAccountOnlyDACL(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}

	for _, path := range []string{
		store.goalSessionsDir(),
		store.goalSessionPath(saved.Key()),
		store.goalSessionLineagePath(saved.LineageKey()),
	} {
		if err := verifyWindowsPrivatePath(path); err != nil {
			t.Fatalf("verifyWindowsPrivatePath(%q) error = %v", path, err)
		}
	}
}
