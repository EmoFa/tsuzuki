package procfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSweepStale(t *testing.T) {
	dir := t.TempDir()
	// A PID that has certainly exited: a finished child.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := strconv.Itoa(cmd.Process.Pid)
	mine := Name("tsuzuki-x-") + "abc"
	for _, name := range []string{mine, "tsuzuki-x-" + dead + "-abc", "tsuzuki-x-" + dead + "-sock", "tsuzuki-x-junk", "other-" + dead + "-abc"} {
		os.MkdirAll(filepath.Join(dir, name, "sub"), 0o700)
	}
	if !Alive(os.Getpid()) {
		t.Fatal("this process should be alive")
	}

	SweepStale(dir, "tsuzuki-x-")

	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := map[string]bool{mine: true, "tsuzuki-x-junk": true, "other-" + dead + "-abc": true}
	if len(left) != len(want) {
		t.Fatalf("left = %v", left)
	}
	for _, n := range left {
		if !want[n] {
			t.Errorf("unexpected %s left", n)
		}
	}
}
