package main

import "testing"

func TestSupportedOS_GUIOnlyMatrix(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want bool
	}{
		{"linux", true},
		{"windows", true},
		{"darwin", false},
		{"freebsd", false},
	} {
		if got := supportedOS(tc.goos); got != tc.want {
			t.Errorf("supportedOS(%q) = %v, want %v", tc.goos, got, tc.want)
		}
	}
}

func TestRun_DefaultAndGUIAliasLaunchDesktop(t *testing.T) {
	old := launchDesktop
	t.Cleanup(func() { launchDesktop = old })

	calls := 0
	launchDesktop = func() int {
		calls++
		return 23
	}
	if got := run(nil); got != 23 {
		t.Fatalf("default run returned %d, want desktop result", got)
	}
	if got := run([]string{"--gui"}); got != 23 {
		t.Fatalf("--gui run returned %d, want desktop result", got)
	}
	if calls != 2 {
		t.Fatalf("desktop launches = %d, want 2", calls)
	}
}
