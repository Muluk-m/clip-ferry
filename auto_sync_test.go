package main

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/transfer"
	"time"
)

func drainSends(t *testing.T, a *app, updates chan func()) {
	t.Helper()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for len(a.sending) > 0 {
		select {
		case fn := <-updates:
			fn()
		case <-timeout.C:
			t.Fatal("send did not complete")
		}
	}
}

func TestAutoSync(t *testing.T) {
	for _, scenario := range []string{"off", "on", "changed clipboard", "changed while saving", "changed host", "newer image", "disabled during send", "failure", "no host", "duplicate in flight"} {
		t.Run(scenario, func(t *testing.T) {
			a := newApp(t.TempDir(), []string{"mini"})
			a.opts.AutoSync = scenario != "off"
			updates := make(chan func(), 32)
			a.update = func(fn func()) { updates <- fn }
			var calls, copied []string
			mode := "ok"
			if scenario == "failure" {
				mode = "fail"
			}
			a.sender = fakeSSH(mode, &calls)
			a.copy = func(path string) { copied = append(copied, path) }
			closed := 0
			a.close = func() { closed++ }
			revision := int64(1)
			a.clipboardChanges = func() int64 { return revision }
			if scenario == "no host" {
				a.host = ""
			}
			a.addImage(pngData(t, 8, 8, color.White))
			if scenario == "changed while saving" {
				revision++
			}
			(<-updates)()
			id := a.items[0].ID
			switch scenario {
			case "changed clipboard":
				revision++
			case "changed host":
				a.host = "gpu"
			case "newer image":
				a.items = append([]item{{shot: shot{ID: "newer"}}}, a.items...)
			case "disabled during send":
				a.opts.AutoSync = false
			case "duplicate in flight":
				a.sendImage(a.items[0], true, revision)
			}
			drainSends(t, a, updates)
			wantCalls := 1
			if scenario == "off" || scenario == "no host" {
				wantCalls = 0
			}
			if len(calls) != wantCalls {
				t.Fatalf("SSH calls = %d, want %d", len(calls), wantCalls)
			}
			wantCopy := scenario == "on" || scenario == "duplicate in flight"
			if (len(copied) == 1) != wantCopy {
				t.Errorf("copied %v", copied)
			}
			if closed != 0 {
				t.Error("background send closed the panel")
			}
			if scenario == "failure" || scenario == "no host" {
				if !a.failed || a.sent[sentKey("mini", id)] != "" {
					t.Errorf("failed=%v, sent=%v", a.failed, a.sent)
				}
			} else if wantCalls == 1 && a.sent[sentKey("mini", id)] == "" {
				t.Error("successful upload not recorded")
			}
		})
	}
}

func TestAutoSyncSettings(t *testing.T) {
	dir := t.TempDir()
	if loadSettings(dir).AutoSync {
		t.Fatal("auto sync should default to off")
	}
	s := defaultSettings()
	s.AutoSync = true
	if err := saveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	if !newApp(dir, nil).opts.AutoSync {
		t.Fatal("auto sync not restored after relaunch")
	}
	s.AutoSync = false
	if err := saveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	if loadSettings(dir).AutoSync {
		t.Fatal("auto sync could not be disabled")
	}
}

func TestAutoSyncRepeatedClipboardImage(t *testing.T) {
	for _, withChanges := range []bool{false, true} {
		t.Run(map[bool]string{false: "polling", true: "change count"}[withChanges], func(t *testing.T) {
			a := newApp(t.TempDir(), []string{"mini"})
			a.opts.AutoSync = true
			updates := make(chan func(), 32)
			a.update = func(fn func()) { updates <- fn }
			var calls []string
			a.sender = fakeSSH("ok", &calls)
			clip := &fakeClip{formats: []transfer.Format{transfer.PNG}, image: pngData(t, 8, 8, color.White)}
			revision := int64(1)
			w := &watcher{clip: clip, onImage: a.addImage}
			if withChanges {
				w.changes = func() int64 { return revision }
			}
			for i := 0; i < 2; i++ {
				w.poll()
				(<-updates)()
				drainSends(t, a, updates)
				w.poll()
				if len(updates) != 0 {
					t.Fatal("unchanged clipboard emitted another image")
				}
				revision++
				clip.formats = []transfer.Format{transfer.Text}
				w.poll()
				revision++
				clip.formats = []transfer.Format{transfer.PNG}
			}
			if len(calls) != 2 {
				t.Fatalf("sent %d times, want 2", len(calls))
			}
			if len(a.items) != 1 {
				t.Fatal("duplicate image added to history")
			}
		})
	}
}
