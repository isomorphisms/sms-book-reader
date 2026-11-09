package main

import (
	"strings"
	"testing"
)

func TestIndependentBookmarksAndUndo(t *testing.T) {
	l, err := loadLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	send := func(user, cmd string) string {
		t.Helper()
		out, err := l.jog(user, cmd)
		if err != nil {
			t.Fatalf("%s %s: %v", user, cmd, err)
		}
		return out
	}
	send("ada", "select inferno")
	first := send("ada", "next")
	if !strings.Contains(first, "dark wood") || !strings.Contains(first, "\n[Mock verse:") {
		t.Fatalf("verse break lost: %q", first)
	}
	send("ada", "continue")
	send("ben", "select inferno")
	other := send("ben", "n")
	if other != first {
		t.Fatal("readers did not begin independently")
	}
	if again := send("ada", "again"); strings.Contains(again, "dark wood") {
		t.Fatal("ada position overwritten")
	}
	back := send("ada", "back")
	if !strings.Contains(back, "dark wood") {
		t.Fatal("back did not return to first passage")
	}
	if got := send("ada", "undo"); strings.Contains(got, "dark wood") {
		t.Fatal("undo failed")
	}
	if got := send("ada", "restart"); !strings.Contains(got, "dark wood") {
		t.Fatal("restart failed")
	}
	send("ada", "select nostromo")
	send("ada", "continue")
	send("ada", "select inferno")
	if got := send("ada", "again"); !strings.Contains(got, "dark wood") {
		t.Fatal("selection lost book bookmark")
	}
	// A second instance can read the persisted filesystem data.
	l2, err := loadLibrary(l.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := l2.jog("ada", "again"); err != nil || !strings.Contains(got, "dark wood") {
		t.Fatalf("persistence: %q %v", got, err)
	}
}
func TestHalfBackAndSeed(t *testing.T) {
	l, err := loadLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = l.seed(); err != nil {
		t.Fatal(err)
	}
	r, err := l.read("ben")
	if err != nil || r.Bookmarks["nostromo"].Index != 1 {
		t.Fatalf("seed: %+v %v", r, err)
	}
	l.jog("ada", "select nostromo")
	l.jog("ada", "continue")
	l.jog("ada", "continue")
	response, err := l.jog("ada", "half back")
	if err != nil || !strings.Contains(response, "chapter-1-p1") || strings.Contains(response, "the harbor") {
		t.Fatalf("half back: %q %v", response, err)
	}
	r, err = l.read("ada")
	if err != nil || r.Bookmarks["nostromo"].Index != 0 || r.Bookmarks["nostromo"].Offset == 0 {
		t.Fatalf("half back position: %+v %v", r, err)
	}
}
func TestNoUnknownCommandsOrBadReader(t *testing.T) {
	l, _ := loadLibrary(t.TempDir())
	if _, err := l.jog("../bad", "books"); err == nil {
		t.Fatal("unsafe reader ID accepted")
	}
	if _, err := l.jog("ada", "continue"); err == nil {
		t.Fatal("missing selection accepted")
	}
	if _, err := l.jog("ada", "select 9999"); err == nil {
		t.Fatal("unknown book accepted")
	}
}