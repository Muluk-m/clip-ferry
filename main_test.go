package main

import (
	"bytes"
	"context"
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

	"clip-share/locales"
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
		onImage: func(b []byte) { got = append(got, b) }}

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
	w2 := &watcher{clip: clip, onImage: func(b []byte) { got = append(got, b) }}
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
	fmt.Printf("Welcome to the box\n/home/me/.cache/clip-share/%s\n", name)
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
	if want := "/home/me/.cache/clip-share/" + s.ID + "-1568.png"; path != want {
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
	if got := zh.ImageCount(3); got != "3 张图片，仅保存在本机" {
		t.Errorf("zh %q", got)
	}
	if got := locales.In("en").ImageCount(1); got != "1 image, kept on this Mac only" {
		t.Errorf("en %q", got)
	}
}

// run applies updates from workers until done says the work has finished.
func run(updates chan func(), done func() bool) {
	for !done() {
		(<-updates)()
	}
}

// The view runs without a window in tests, which click as a user would.
func TestView(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir, []string{"mini", "gpu"})
	updates := make(chan func(), 4)
	a.update = func(fn func()) { updates <- fn }
	var copied []string
	a.copy = func(s string) { copied = append(copied, s) }
	var calls []string
	a.sender = fakeSSH("ok", &calls)
	tt := ui.NewTester(a.view, 380, 520)
	a.now = tt.Now
	if !tt.HasText("No images yet") || a.host != "mini" {
		t.Fatalf("empty view: host %q, texts %q", a.host, tt.Texts())
	}

	a.addImage(pngData(t, 1920, 1080, color.White))
	(<-updates)()
	tt.Frame()
	id := a.items[0].ID
	if !tt.HasText("1920 × 1080") || !tt.HasText("Just now") {
		t.Fatalf("texts %q after an image", tt.Texts())
	}
	if err := tt.Click("Send image " + id); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("0%") {
		t.Errorf("texts %q while sending", tt.Texts())
	}
	run(updates, func() bool { _, busy := a.sending[id]; return !busy })
	tt.Frame()
	up, _ := prepareUpload(a.items[0].Path, claudeMaxEdge)
	want := "/home/me/.cache/clip-share/" + id + "-1568" + up.ext
	if !slices.Equal(copied, []string{want}) {
		t.Errorf("copied %q, want %q", copied, want)
	}
	if !tt.HasText("Remote path copied: "+want) || !tt.HasText("1920 × 1080 · Sent") {
		t.Errorf("texts %q after sending", tt.Texts())
	}
	if !strings.Contains(calls[0], " mini ") {
		t.Errorf("sent with %q", calls[0])
	}

	var login []bool
	a.login = func(on bool) error { login = append(login, on); return nil }
	if err := tt.Click("Open at login"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.atLogin || !slices.Equal(login, []bool{true}) {
		t.Errorf("open at login %v, calls %v", a.atLogin, login)
	}

	tt.Advance(5 * time.Minute)
	if !tt.HasText("5 min ago") {
		t.Errorf("texts %q five minutes on", tt.Texts())
	}
	if err := tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("No images yet") || !tt.HasText("History cleared") {
		t.Errorf("texts %q after clearing", tt.Texts())
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
		{3 * time.Hour, "Today 12:00 PM"},
		{30 * time.Hour, "Oct 26 9:00 AM"},
	} {
		if got := ago(now.Add(-c.d), now); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
