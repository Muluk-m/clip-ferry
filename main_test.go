package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"clip-ferry/locales"
)

func pngData(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSSHHosts(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "work"), []byte("Host devbox\n  HostName 10.0.0.2\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "config"), []byte(strings.Join([]string{
		"Include work",
		"Host *",
		"  ServerAliveInterval 30",
		"Host mini mini2 # the macs",
		"Host=gpu",
		"Host *.internal !bad",
		"Host mini",
		"Host github.com github.com-work",
		"Host gh-alt",
		"  HostName github.com",
	}, "\n")), 0o600)
	got := sshHosts(filepath.Join(dir, "config"))
	want := []string{"devbox", "mini", "mini2", "gpu"}
	if !slices.Equal(got, want) {
		t.Errorf("hosts %q, want %q", got, want)
	}
	if hosts := sshHosts(filepath.Join(dir, "missing")); len(hosts) != 0 {
		t.Errorf("hosts of a missing config %q", hosts)
	}
}

func TestHistory(t *testing.T) {
	h := &history{dir: t.TempDir(), max: 2}
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local)
	red, blue, green := pngData(t, 600, 300, color.NRGBA{R: 255, A: 255}),
		pngData(t, 20, 10, color.NRGBA{B: 255, A: 255}),
		pngData(t, 30, 30, color.NRGBA{G: 255, A: 255})

	s, isNew, err := h.add(red, t0)
	if err != nil || !isNew || s.W != 600 || s.H != 300 {
		t.Fatalf("add: %+v new=%v err=%v", s, isNew, err)
	}
	thumb, err := os.ReadFile(s.Thumb)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := png.DecodeConfig(bytes.NewReader(thumb)); cfg.Width != thumbWidth || cfg.Height != 120 {
		t.Errorf("thumbnail %dx%d", cfg.Width, cfg.Height)
	}
	h.add(blue, t0.Add(time.Minute))
	// The same image again comes to the top without a copy.
	if _, isNew, _ := h.add(red, t0.Add(2*time.Minute)); isNew {
		t.Error("the same image counted as new")
	}
	shots, _ := h.list()
	if len(shots) != 2 || shots[0].ID != imageID(red) || shots[1].ID != imageID(blue) {
		t.Fatalf("shots %+v", shots)
	}
	// A third image drops the oldest, its thumbnail included.
	h.add(green, t0.Add(3*time.Minute))
	shots, _ = h.list()
	if len(shots) != 2 || shots[0].ID != imageID(green) || shots[1].ID != imageID(red) {
		t.Fatalf("shots after trim %+v", shots)
	}
	if _, err := os.Stat(filepath.Join(h.thumbsDir(), imageID(blue)+".png")); !os.IsNotExist(err) {
		t.Error("trimmed thumbnail left behind")
	}
	if err := h.clear(); err != nil {
		t.Fatal(err)
	}
	if shots, _ := h.list(); len(shots) != 0 {
		t.Errorf("shots after clear %+v", shots)
	}
}

type fakeClip struct {
	formats []transfer.Format
	image   []byte
	reads   int
}

func (f *fakeClip) Formats() []transfer.Format { return f.formats }
func (f *fakeClip) ReadImage() []byte          { f.reads++; return f.image }

func TestWatcher(t *testing.T) {
	img := pngData(t, 4, 4, color.Black)
	clip := &fakeClip{formats: []transfer.Format{transfer.PNG}, image: img}
	change := int64(1)
	var got [][]byte
	w := &watcher{clip: clip, changes: func() int64 { return change }, lastChange: -1,
		onImage: func(b []byte, _ int64) { got = append(got, b) }}

	w.poll()
	w.poll() // unchanged: the image is not read again
	if len(got) != 1 || clip.reads != 1 {
		t.Fatalf("images %d, reads %d", len(got), clip.reads)
	}
	change++
	clip.formats = []transfer.Format{transfer.Text}
	w.poll()
	change++
	clip.formats = []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG}
	w.poll()
	if len(got) != 1 || clip.reads != 1 {
		t.Errorf("text or files counted: images %d, reads %d", len(got), clip.reads)
	}
	// Without a change count, the image's ID tells a new one apart.
	clip.formats = []transfer.Format{transfer.PNG}
	w2 := &watcher{clip: clip, onImage: func(b []byte, _ int64) { got = append(got, b) }}
	w2.poll()
	w2.poll()
	if len(got) != 2 {
		t.Errorf("images %d without a change count", len(got))
	}
}

// TestHelperProcess stands in for ssh in the tests below.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("CLIP_SHARE_HELPER") == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if os.Getenv("CLIP_SHARE_HELPER") == "fail" {
		fmt.Fprintln(os.Stderr, "ssh: connect to host gpu port 22: Connection refused")
		os.Exit(255)
	}
	data, _ := io.ReadAll(os.Stdin)
	cmd := args[len(args)-1]
	name := cmd[strings.Index(cmd, "cat > ")+len("cat > "):]
	name = name[:strings.Index(name, ";")]
	fmt.Printf("Welcome to the box\n/home/me/.cache/ferry/%s\n", name)
	fmt.Fprintf(os.Stderr, "%s %d", strings.Join(args[:len(args)-1], " "), len(data))
	os.Exit(0)
}

func fakeSSH(mode string, calls *[]string) sender {
	return sender{run: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "CLIP_SHARE_HELPER="+mode)
		return cmd
	}}
}

func TestSend(t *testing.T) {
	h := &history{dir: t.TempDir(), max: 10}
	s, _, _ := h.add(pngData(t, 8, 8, color.White), time.Now())
	var calls []string
	opts := defaultSettings().sendOptions()
	var progress []int64
	path, err := fakeSSH("ok", &calls).send(context.Background(), "gpu", s, opts, func(done, total int64) { progress = append(progress, done) })
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/me/.cache/ferry/" + s.ID + "-1568.png"; path != want {
		t.Errorf("path %q, want %q", path, want)
	}
	if len(progress) == 0 || progress[len(progress)-1] == 0 {
		t.Errorf("progress %v", progress)
	}
	call := calls[0]
	for _, part := range []string{"/usr/bin/ssh ", "BatchMode=yes", "ControlMaster=auto", " gpu ", "sh -c ", "-mtime +7"} {
		if !strings.Contains(call, part) {
			t.Errorf("ssh call %q lacks %q", call, part)
		}
	}
	_, err = fakeSSH("fail", &calls).send(context.Background(), "gpu", s, opts, nil)
	if err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("error %v", err)
	}
	if _, err := fakeSSH("ok", &calls).send(context.Background(), "", s, opts, nil); err == nil {
		t.Error("sent without a host")
	}
}

// The remote script, run by a real sh in a home of its own.
func TestRemoteCommand(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "it's here", "old.png")
	os.MkdirAll(filepath.Dir(old), 0o700)
	os.WriteFile(old, nil, 0o600)
	stale := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(old, stale, stale)
	for _, dir := range []string{"~/it's here", filepath.Join(home, "it's here")} {
		cmd := exec.Command("/bin/sh", "-c", remoteCommand(sendOptions{dir: dir, keepDays: 7}, "abc-1568.jpg"))
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		cmd.Stdin = strings.NewReader("data")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", dir, err, out)
		}
		want := filepath.Join(home, "it's here", "abc-1568.jpg")
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s: printed %q, want %q", dir, got, want)
		}
		if data, _ := os.ReadFile(want); string(data) != "data" {
			t.Errorf("%s: wrote %q", dir, data)
		}
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an image past keepDays stayed")
	}
}

func noisyPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.IntN(256))
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	path := filepath.Join(t.TempDir(), "noisy.png")
	os.WriteFile(path, b.Bytes(), 0o600)
	return path
}

func TestPrepareUpload(t *testing.T) {
	path := noisyPNG(t, 3000, 2000)
	orig, _ := os.ReadFile(path)
	up, err := prepareUpload(path, claudeMaxEdge)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(up.data))
	if err != nil || cfg.Width != 1568 || cfg.Height != 1045 {
		t.Fatalf("scaled to %dx%d (%v)", cfg.Width, cfg.Height, err)
	}
	if up.ext != ".jpg" || len(up.data) >= len(orig)/4 {
		t.Errorf("photo-like image sent as %s, %d of %d bytes", up.ext, len(up.data), len(orig))
	}
	if up, _ := prepareUpload(path, 0); up.ext != ".png" || !bytes.Equal(up.data, orig) {
		t.Error("the original was not sent as it is")
	}
	// Transparency stays PNG.
	img := image.NewNRGBA(image.Rect(0, 0, 2000, 100))
	var b bytes.Buffer
	png.Encode(&b, img)
	clear := filepath.Join(t.TempDir(), "clear.png")
	os.WriteFile(clear, b.Bytes(), 0o600)
	if up, _ := prepareUpload(clear, claudeMaxEdge); up.ext != ".png" {
		t.Errorf("transparent image sent as %s", up.ext)
	}
}

func TestChinese(t *testing.T) {
	zh := locales.In("zh-Hans")
	if got := zh.StatusCantReach("mini"); got != "无法连接 mini" {
		t.Errorf("zh %q", got)
	}
	if got := locales.In("en").KeepDays(1); got != "1 day" {
		t.Errorf("en %q", got)
	}
}

func TestSendFailure(t *testing.T) {
	for _, c := range []struct{ err, want string }{
		{"ssh: connect to host mini port 22: Operation timed out", "Can't reach mini"},
		{"ssh: Could not resolve hostname mini: nodename nor servname provided", "Can't reach mini"},
		{"me@mini: Permission denied (publickey).", "mini refused the SSH key"},
		{"sh: 1: cannot create x.png: Read-only file system", "Send failed: sh: 1: cannot create x.png: Read-only file system"},
	} {
		if got := sendFailure("mini", errors.New(c.err)); got != c.want {
			t.Errorf("sendFailure(%q) = %q, want %q", c.err, got, c.want)
		}
	}
}

// run applies updates from workers until done says the work has finished.
func run(updates chan func(), done func() bool) {
	for !done() {
		(<-updates)()
	}
}

// The view runs without a window in tests, which click and type as a
// user would.
func TestView(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir, []string{"mini", "gpu"})
	updates := make(chan func(), 4)
	a.update = func(fn func()) { updates <- fn }
	var copied []string
	a.copy = func(s string) { copied = append(copied, s) }
	closed := 0
	a.close = func() { closed++ }
	var calls []string
	a.sender = fakeSSH("ok", &calls)
	tt := ui.NewTester(a.view, panelWidth, panelHeight)
	a.now = tt.Now
	if !tt.HasText("Copy or capture an image") || !tt.HasText("mini") {
		t.Fatalf("empty view: texts %q", tt.Texts())
	}

	a.addImage(pngData(t, 1920, 1080, color.White), 0)
	(<-updates)()
	tt.Frame()
	id := a.items[0].ID
	if !tt.HasText("1920 × 1080") || !tt.HasText("Just now") {
		t.Fatalf("texts %q after an image", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter) // sends the newest, selected
	if !tt.HasText("Sending 0%") {
		t.Errorf("texts %q while sending", tt.Texts())
	}
	run(updates, func() bool { _, busy := a.sending[id]; return !busy })
	tt.Frame()
	up, _ := prepareUpload(a.items[0].Path, claudeMaxEdge)
	want := "/home/me/.cache/ferry/" + id + "-1568" + up.ext
	if !slices.Equal(copied, []string{want}) {
		t.Errorf("copied %q, want %q", copied, want)
	}
	if !tt.HasText("Path copied") || !tt.HasText("Sent") || closed != 1 {
		t.Errorf("texts %q after sending, closed %d times", tt.Texts(), closed)
	}
	if !strings.Contains(calls[0], " mini ") {
		t.Errorf("sent with %q", calls[0])
	}

	// A new image goes on top, selected; 2 sends the one below it.
	a.addImage(pngData(t, 800, 600, color.Black), 0)
	(<-updates)()
	tt.Frame()
	if a.sel != 0 || len(a.items) != 2 || a.items[1].ID != id {
		t.Fatalf("selection %d of %d after a second image", a.sel, len(a.items))
	}
	tt.Key(0, ui.Key2)
	run(updates, func() bool { _, busy := a.sending[id]; return !busy })
	if len(calls) != 2 || a.sel != 1 {
		t.Errorf("%d sends, selection %d after pressing 2", len(calls), a.sel)
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyBackspace) // deletes the newest
	tt.Frame()
	if len(a.items) != 1 || a.items[0].ID != id {
		t.Errorf("items %v after deleting", a.items)
	}

	tt.Advance(5 * time.Minute)
	if !tt.HasText("5 min ago") {
		t.Errorf("texts %q five minutes on", tt.Texts())
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Clear History"); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if !tt.HasText("Copy or capture an image") || !tt.HasText("Cleared") {
		t.Errorf("texts %q after clearing", tt.Texts())
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir, []string{"mini"})
	tt := ui.NewTester(a.settingsView, 460, 430)
	for _, s := range []string{"Remote folder", "7 days", "1568 px", "⌃⌥V", "System"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	// A shortcut another app holds keeps the one there was.
	a.rebind = func(_, k shortcut) error {
		if k.accel == "Ctrl+Shift+V" {
			return errors.New("taken")
		}
		return nil
	}
	a.chooseShortcut(shortcuts[1])
	if a.opts.Shortcut != "Ctrl+Alt+V" || a.status != "⌃⇧V is in use by another app" {
		t.Errorf("shortcut %q, status %q", a.opts.Shortcut, a.status)
	}
	a.chooseShortcut(shortcuts[2])
	if got := loadSettings(dir); got.Shortcut != "Alt+Shift+V" || got.Host != "mini" {
		t.Errorf("saved %+v", got)
	}
}

func TestHostIsRemembered(t *testing.T) {
	dir := t.TempDir()
	saveSettings(dir, settings{Host: "gpu"})
	if a := newApp(dir, []string{"mini", "gpu"}); a.host != "gpu" {
		t.Errorf("host %q", a.host)
	}
	// A host gone from the ssh config falls back to the first one.
	if a := newApp(dir, []string{"mini"}); a.host != "mini" {
		t.Errorf("host %q", a.host)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "Just now"},
		{12 * time.Minute, "12 min ago"},
		{3 * time.Hour, "12:00 PM"},
		{30 * time.Hour, "Oct 26 9:00 AM"},
	} {
		if got := ago(now.Add(-c.d), now); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseHost(t *testing.T) {
	for s, ok := range map[string]bool{
		"me@box":                 true,
		"box":                    true,
		" me@10.0.0.2 ":          true,
		"ssh://me@box:2222":      true,
		"ssh://box":              true,
		"":                       false,
		"-oProxyCommand=evil":    false,
		"me@-oProxyCommand=evil": false,
		"ssh://-oX@box":          false,
		"ssh://me@box:99999":     false,
		"ssh://me@box/path":      false,
		"me@box; rm -rf /":       false,
		"@box":                   false,
		"me@":                    false,
	} {
		if _, got := parseHost(s); got != ok {
			t.Errorf("parseHost(%q) = %v, want %v", s, got, ok)
		}
	}
}

func TestAddHost(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir, []string{"mini"})
	a.newHost = "me@10.0.0.2"
	a.addHost()
	if a.host != "me@10.0.0.2" || a.newHost != "" || a.hostErr != "" {
		t.Fatalf("host %q, field %q, error %q", a.host, a.newHost, a.hostErr)
	}
	a.newHost = "mini"
	if a.addHost(); a.hostErr != "mini is already in the list" {
		t.Errorf("error %q for a host of the config", a.hostErr)
	}
	if got := newApp(dir, []string{"mini"}); got.host != "me@10.0.0.2" || !slices.Equal(got.allHosts(), []string{"mini", "me@10.0.0.2"}) {
		t.Errorf("after relaunch: host %q of %q", got.host, got.allHosts())
	}

	tt := ui.NewTester(a.view, panelWidth, panelHeight)
	if err := tt.Click("Host"); err != nil {
		t.Fatal(err)
	}
	want := []string{"From ~/.ssh/config", "mini", "-", "Added", "me@10.0.0.2", "-", "Add Host…"}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Errorf("host menu %q, want %q", got, want)
	}

	a.removeHost("me@10.0.0.2")
	if a.host != "mini" || len(a.opts.Hosts) != 0 {
		t.Errorf("after removing: host %q, added %q", a.host, a.opts.Hosts)
	}
}
