//go:build promo

// TestPromo draws the screenshots in docs and the frames of the demo
// video from the real panel and settings, without a screen:
//
//	PROMO_FRAMES=/tmp/frames go test -tags promo -run TestPromo .
//
// It writes frames.txt, a list for ffmpeg's concat demuxer, next to the
// frames.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"clip-ferry/locales"
)

const sceneW, sceneH = 960, 540

func render(view func(*ui.Context), w, h int, dark bool) *image.RGBA {
	t := ui.NewTester(view, w, h)
	t.SetScale(2)
	t.SetDark(dark)
	t.Frame()
	img := t.Image()
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
	return out
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.WriteFile(path, encode(img), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Mock screenshots, the images the demo copies.

func mockError(c *ui.Context) {
	c.Root().Background(ui.Hex("#f6f7f9"))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(44).Padding(0, 18).Gap(8).AlignItems(ui.Center).Background(ui.Hex("#e9ebef")).Children(func() {
			for _, col := range []string{"#ff5f57", "#febc2e", "#28c840"} {
				ui.Box(c).Size(12, 12).Radius(6).Background(ui.Hex(col))
			}
			ui.Box(c).Height(26).Grow(1).MarginStart(24).Radius(6).Background(ui.Hex("#ffffff"))
		})
		ui.Column(c).Padding(48, 64).Gap(18).Children(func() {
			ui.Text(c, "Unhandled Runtime Error").FontSize(30).FontWeight(700).TextColor(ui.Hex("#1f2328"))
			ui.Column(c).Padding(20, 24).Gap(10).Radius(10).Background(ui.Hex("#fdecec")).Border(1, ui.Hex("#f5b5b5")).Children(func() {
				ui.Text(c, "TypeError: Cannot read properties of undefined (reading 'map')").FontSize(20).FontWeight(600).TextColor(ui.Hex("#b42318"))
				for _, l := range []string{"at OrderList (src/orders/List.tsx:42:18)", "at renderWithHooks (react-dom.development.js:16305)", "at mountIndeterminateComponent (react-dom.development.js:20074)"} {
					ui.Text(c, l).FontSize(16).Font("Menlo").TextColor(ui.Hex("#7a2e2a"))
				}
			})
		})
	})
}

func mockChart(c *ui.Context) {
	c.Root().Background(ui.Hex("#15171c"))
	ui.Column(c).Fill().Padding(56, 64).Gap(28).Children(func() {
		ui.Text(c, "Weekly active users").FontSize(30).FontWeight(700).TextColor(ui.Hex("#f2f3f5"))
		ui.Row(c).Grow(1).Gap(22).AlignItems(ui.End).Children(func() {
			for i, h := range []float32{34, 48, 41, 63, 58, 77, 70, 88, 81, 96} {
				col := ui.Hex("#5b8cff")
				if i == 9 {
					col = ui.Hex("#7ee2b8")
				}
				ui.Box(c).Grow(1).HeightPercent(h).Radius(8).Background(col)
			}
		})
	})
}

func mockDesign(c *ui.Context) {
	c.Root().Background(ui.Hex("#ffffff"))
	ui.Column(c).Fill().Padding(80).Gap(26).Justify(ui.Center).LinearGradient(ui.LinearGradient{From: ui.Hex("#ffe7d6"), To: ui.Hex("#e3e8ff"), Angle: 120}).Children(func() {
		ui.Text(c, "Ship the idea,").FontSize(64).FontWeight(800).TextColor(ui.Hex("#1b1d29"))
		ui.Text(c, "not the setup.").FontSize(64).FontWeight(800).TextColor(ui.Hex("#5b5ff0"))
		ui.Text(c, "Hero section · v3 · mobile pending").FontSize(22).TextColor(ui.Hex("#555a6e"))
		ui.Text(c, "Get started").FontSize(22).FontWeight(600).TextColor(ui.Hex("#ffffff")).Padding(16, 30).Radius(12).Background(ui.Hex("#1b1d29")).AlignSelf(ui.Start)
	})
}

// demo is the app the scenes show, with three images in its history.
type demo struct {
	a     *app
	clock time.Time
}

func newDemo(t *testing.T) *demo {
	d := &demo{clock: time.Date(2026, 10, 27, 10, 24, 0, 0, time.Local)}
	a := newApp(t.TempDir(), []string{"macmini", "gpu-box"})
	a.now = func() time.Time { return d.clock }
	d.a = a
	return d
}

func (d *demo) add(view func(*ui.Context), w, h int, ago time.Duration) {
	now := d.clock
	d.clock = now.Add(-ago)
	d.a.addImage(encode(render(view, w/2, h/2, false)))
	d.clock = now
}

func (d *demo) panel(dark bool) *image.RGBA {
	return render(d.a.view, panelWidth, panelHeight, dark)
}

func (d *demo) settings(dark bool) *image.RGBA {
	return render(d.a.settingsView, 480, 620, dark)
}

// Scenes, drawn at 960×540 DIPs, 1920×1080 pixels.

var trayIcon = ui.MustParseSVG(must(os.ReadFile("resources/tray.svg")))
var logo = ui.MustParseSVG(must(os.ReadFile("resources/logo.svg")))

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type scene struct {
	step       string
	zh, en     string
	panel      *image.RGBA // under the tray icon
	settings   *image.RGBA // in a window
	terminal   []string    // lines; the last one is typed into the prompt box
	reply      []string
	trayActive bool
}

const trayX = 760 // the center of the tray icon

func desktop(c *ui.Context, active bool, body func()) {
	c.Root().Background(ui.Hex("#d9e0f0"))
	ui.Box(c).Fill().LinearGradient(ui.LinearGradient{From: ui.Hex("#eef1fa"), To: ui.Hex("#c6d1ec"), Angle: 150}).Children(func() {
		ui.Row(c).Absolute().Top(0).Left(0).Width(sceneW).Height(24).Padding(0, 14).Gap(18).AlignItems(ui.Center).
			Background(ui.RGBA(255, 255, 255, 0.55)).Children(func() {
			ui.Text(c, "Terminal").FontSize(12).FontWeight(700)
			for _, m := range []string{"Shell", "Edit", "View", "Window"} {
				ui.Text(c, m).FontSize(12)
			}
		})
		bg := ui.Transparent
		if active {
			bg = ui.RGBA(0, 0, 0, 0.12)
		}
		ui.Box(c).Absolute().Top(2).Left(trayX-14).Size(28, 20).Radius(5).Background(bg).Center().Children(func() {
			ui.Icon(c, trayIcon).FontSize(15).TextColor(ui.Hex("#1d1d1f"))
		})
		ui.Text(c, "Tue Oct 27  10:24").FontSize(12).Absolute().Top(5).Left(sceneW - 120)
		body()
	})
}

func caption(c *ui.Context, s scene, left float32) {
	ui.Column(c).Absolute().Left(left).Top(170).Width(480).Gap(12).Children(func() {
		ui.Text(c, s.step).FontSize(13).FontWeight(700).TextColor(ui.Hex("#4a6fe3")).FontFeatures("tnum")
		ui.Text(c, s.zh).FontSize(32).FontWeight(700).TextColor(ui.Hex("#15171c")).LineHeight(1.25)
		ui.Text(c, s.en).FontSize(17).TextColor(ui.Hex("#4b5163")).LineHeight(1.4)
	})
}

func (s scene) view(c *ui.Context) {
	desktop(c, s.trayActive, func() {
		switch {
		case s.panel != nil:
			caption(c, s, 64)
			px := float32(trayX - panelWidth/2)
			// The popover's arrow, in the panel's own color.
			arrow := s.panel.RGBAAt(panelWidth, 16)
			ui.Box(c).Absolute().Top(28).Left(trayX-8).Size(16, 16).Rotate(45).Radius(3).
				Background(ui.RGB(arrow.R, arrow.G, arrow.B))
			ui.Image(c, ui.NewBitmap(s.panel)).Absolute().Top(32).Left(px).Size(panelWidth, panelHeight).
				Radius(12).Shadow(0, 18, 48, 0, ui.RGBA(20, 30, 60, 0.28))
		case s.settings != nil:
			caption(c, s, 64)
			const k = 0.74
			ui.Column(c).Absolute().Top(40).Left(560).Width(480*k).Height(648*k).Radius(10).Clip().
				Background(ui.Hex("#ffffff")).Shadow(0, 18, 48, 0, ui.RGBA(20, 30, 60, 0.28)).Children(func() {
				ui.Row(c).Height(28 * k).Padding(0, 10).Gap(6).AlignItems(ui.Center).Background(ui.Hex("#ececee")).Children(func() {
					for _, col := range []string{"#ff5f57", "#febc2e", "#28c840"} {
						ui.Box(c).Size(9, 9).Radius(4.5).Background(ui.Hex(col))
					}
					ui.Text(c, locales.SettingsTitle()).FontSize(10).FontWeight(600).TextColor(ui.Hex("#55575e")).Grow(1).TextAlign(ui.Center).MarginEnd(40)
				})
				ui.Image(c, ui.NewBitmap(s.settings)).Size(480*k, 620*k)
			})
		case s.terminal != nil:
			caption(c, s, 64)
			terminal(c, s)
		default:
			titleCard(c, s)
		}
	})
}

func terminal(c *ui.Context, s scene) {
	mono := func(text string, col ui.Color) ui.Element {
		return ui.Text(c, text).Font("Menlo").FontSize(12.5).TextColor(col).LineHeight(1.5)
	}
	ui.Column(c).Absolute().Top(64).Left(470).Width(456).Height(400).Radius(10).Clip().
		Background(ui.Hex("#1c1e23")).Shadow(0, 18, 48, 0, ui.RGBA(20, 30, 60, 0.32)).Children(func() {
		ui.Row(c).Height(26).Padding(0, 10).Gap(6).AlignItems(ui.Center).Background(ui.Hex("#2a2d33")).Children(func() {
			for _, col := range []string{"#ff5f57", "#febc2e", "#28c840"} {
				ui.Box(c).Size(10, 10).Radius(5).Background(ui.Hex(col))
			}
			ui.Text(c, "me@macmini: ~/app — ssh").FontSize(11).TextColor(ui.Hex("#9aa0aa")).Grow(1).TextAlign(ui.Center).MarginEnd(40)
		})
		ui.Column(c).Padding(14, 16).Gap(10).Children(func() {
			for _, l := range s.terminal[:len(s.terminal)-1] {
				mono(l, ui.Hex("#c9ced6"))
			}
			ui.Column(c).Padding(8, 10).Radius(6).Border(1, ui.Hex("#5a5f69")).Children(func() {
				ui.Text(c, "> "+s.terminal[len(s.terminal)-1]+"▍").Font("Menlo").FontSize(12.5).TextColor(ui.Hex("#eef0f3")).LineHeight(1.5)
			})
			for i, l := range s.reply {
				col := ui.Hex("#c9ced6")
				if i == 0 {
					col = ui.Hex("#7ee2b8")
				}
				mono(l, col)
			}
		})
	})
}

func titleCard(c *ui.Context, s scene) {
	ui.Column(c).Fill().Center().Gap(14).Children(func() {
		ui.Image(c, logo).Size(140, 140)
		ui.Text(c, s.zh).FontSize(44).FontWeight(800).TextColor(ui.Hex("#15171c")).MarginY(3)
		ui.Text(c, s.en).FontSize(19).TextColor(ui.Hex("#4b5163")).TextAlign(ui.Center).LineHeight(1.5)
		if s.step != "" {
			ui.Text(c, s.step).FontSize(14).Font("Menlo").TextColor(ui.Hex("#4a6fe3")).MarginY(5)
		}
	})
}

// frames collects the video: stills held for a while, with crossfades.
type frames struct {
	t    *testing.T
	dir  string
	n    int
	list bytes.Buffer
	last *image.RGBA
}

func (f *frames) put(img *image.RGBA, d time.Duration) {
	path := filepath.Join(f.dir, fmt.Sprintf("%04d.png", f.n))
	f.n++
	writePNG(f.t, path, img)
	fmt.Fprintf(&f.list, "file '%s'\nduration %.3f\n", path, d.Seconds())
}

// hold shows a scene for d, fading in from the last one when fade.
func (f *frames) hold(s scene, d time.Duration, fade bool) {
	img := render(s.view, sceneW, sceneH, false)
	if fade && f.last != nil {
		const steps = 9
		for i := 1; i < steps; i++ {
			f.put(blend(f.last, img, float64(i)/steps), time.Second/30)
		}
	}
	f.put(img, d)
	f.last = img
}

func blend(a, b *image.RGBA, k float64) *image.RGBA {
	out := image.NewRGBA(a.Bounds())
	for i := range out.Pix {
		out.Pix[i] = uint8(float64(a.Pix[i])*(1-k) + float64(b.Pix[i])*k + 0.5)
	}
	return out
}

func (f *frames) done() {
	// The concat demuxer holds the last file only when it is listed twice.
	fmt.Fprintf(&f.list, "file '%s'\n", filepath.Join(f.dir, fmt.Sprintf("%04d.png", f.n-1)))
	if err := os.WriteFile(filepath.Join(f.dir, "frames.txt"), f.list.Bytes(), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func TestPromo(t *testing.T) {
	lang := "en"
	i18n.SetLocaleFunc(func() string { return lang })
	defer i18n.SetLocaleFunc(nil)
	os.MkdirAll("docs", 0o755)

	// Screenshots for the READMEs, in either language.
	for _, l := range []string{"en", "zh-Hans"} {
		lang = l
		d := newDemo(t)
		d.add(mockDesign, 1280, 800, 26*time.Minute)
		d.add(mockChart, 1600, 1000, 4*time.Minute)
		d.add(mockError, 1440, 900, 0)
		id := d.a.items[1].ID
		d.a.sent[sentKey("macmini", id)] = "/home/me/.cache/ferry/" + id + ".png"
		suffix := map[string]string{"en": "en", "zh-Hans": "zh"}[l]
		writePNG(t, "docs/panel-"+suffix+".png", d.panel(false))
		writePNG(t, "docs/panel-dark-"+suffix+".png", d.panel(true))
		d.a.newHost = "me@10.0.0.8"
		d.a.addHost()
		d.a.chooseHost("macmini")
		writePNG(t, "docs/settings-"+suffix+".png", d.settings(false))
	}

	dir := os.Getenv("PROMO_FRAMES")
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0o755)
	lang = "zh-Hans"
	f := &frames{t: t, dir: dir}
	d := newDemo(t)
	a := d.a

	f.hold(scene{zh: "Ferry 摆渡", en: "把本机截图摆渡给远端的 Claude Code\nFerry screenshots to Claude Code on a remote machine"}, 2600*time.Millisecond, false)

	d.add(mockDesign, 1280, 800, 26*time.Minute)
	d.add(mockChart, 1600, 1000, 4*time.Minute)
	intro := scene{step: "01", zh: "远端的 Claude 看不到\n你本机的截图", en: "Over SSH, Claude Code can't see\nthe screenshot on your Mac."}
	intro.panel, intro.trayActive = d.panel(false), true
	f.hold(intro, 2600*time.Millisecond, true)

	d.add(mockError, 1440, 900, 0)
	cp := scene{step: "02", zh: "截图或复制图片\n自动收进菜单栏", en: "Capture or copy an image —\nit lands in the menu bar."}
	cp.panel, cp.trayActive = d.panel(false), true
	f.hold(cp, 2600*time.Millisecond, true)

	it := a.items[0]
	up, err := prepareUpload(it.Path, claudeMaxEdge)
	if err != nil {
		t.Fatal(err)
	}
	path := "/home/me/.cache/ferry/" + it.ID + "-1568" + up.ext
	send := scene{step: "03", zh: "按 ↵ 经 SSH 发送\n只传这一张", en: "Press ↵ to send just this one\nover SSH. No LAN, no sync."}
	send.trayActive = true
	for i, p := range []float64{0, 0.18, 0.42, 0.66, 0.85, 1} {
		a.sending[it.ID] = p
		a.setStatus(locales.StatusSending(int(p*100)), false)
		send.panel = d.panel(false)
		f.hold(send, 130*time.Millisecond, i == 0)
	}
	delete(a.sending, it.ID)
	a.sent[sentKey(a.host, it.ID)] = path
	a.setStatus(locales.StatusCopied(), false)
	send.panel = d.panel(false)
	f.hold(send, 2200*time.Millisecond, false)

	paste := scene{step: "04", zh: "⌘V 粘贴远端路径\nClaude 直接读图", en: "Paste the remote path with ⌘V.\nClaude reads the image."}
	paste.terminal = []string{"me@macmini ~/app $ claude", "这个报错怎么修？"}
	f.hold(paste, 1300*time.Millisecond, true)
	paste.terminal[1] = "这个报错怎么修？ " + path
	f.hold(paste, 1500*time.Millisecond, false)
	paste.reply = []string{"⏺ Read(" + filepath.Base(path) + ")", "  orders 还没加载就调用了 .map，", "  在 List.tsx:42 加个空数组默认值即可。"}
	f.hold(paste, 2600*time.Millisecond, false)

	a.newHost = "me@10.0.0.8"
	a.addHost()
	a.chooseHost("macmini")
	set := scene{step: "05", zh: "读取 ~/.ssh/config\n也能手动添加主机", en: "Hosts come from ~/.ssh/config,\nor add your own."}
	set.settings = d.settings(false)
	f.hold(set, 2800*time.Millisecond, true)

	f.hold(scene{zh: "Ferry", en: "免费开源 · macOS 菜单栏应用\nFree and open source · for macOS", step: "github.com/Muluk-m/clip-ferry"}, 3000*time.Millisecond, true)
	f.done()
}

