package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clip-share/locales"
)

// sshHosts returns the concrete Host aliases of an ssh config, following
// Include, in the order they appear. Patterns such as * are skipped: they
// configure hosts rather than name one.
func sshHosts(configPath string) []string {
	seen := map[string]bool{}
	var hosts []string
	var block []string // the hosts the current Host line names
	var read func(path string, depth int)
	read = func(path string, depth int) {
		if depth > 8 {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(strings.ReplaceAll(sc.Text(), "=", " "))
			if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "host":
				block = nil
				for _, h := range fields[1:] {
					if strings.HasPrefix(h, "#") {
						break
					}
					if strings.ContainsAny(h, "*?!") || seen[h] || isGitForge(h) {
						continue
					}
					seen[h] = true
					hosts = append(hosts, h)
					block = append(block, h)
				}
			case "hostname":
				if isGitForge(fields[1]) {
					hosts = slices.DeleteFunc(hosts, func(h string) bool { return slices.Contains(block, h) })
				}
			case "include":
				for _, pattern := range fields[1:] {
					pattern = expandHome(pattern)
					if !filepath.IsAbs(pattern) {
						pattern = filepath.Join(filepath.Dir(configPath), pattern)
					}
					matches, _ := filepath.Glob(pattern)
					for _, m := range matches {
						read(m, depth+1)
					}
				}
			}
		}
	}
	read(configPath, 0)
	return hosts
}

// isGitForge tells the aliases for pushing to a forge, which take no
// files, from machines.
func isGitForge(host string) bool {
	for _, forge := range []string{"github.com", "gitlab.com", "bitbucket.org"} {
		if host == forge || strings.HasPrefix(host, forge+"-") || strings.HasSuffix(host, "."+forge) {
			return true
		}
	}
	return false
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// sender copies one image to a host over ssh and returns its absolute path
// there. run is exec.CommandContext's shape, replaced in tests.
type sender struct {
	run func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// sendOptions are the settings a send follows.
type sendOptions struct {
	dir      string // on the remote, ~/ for its home
	keepDays int    // older images there are removed; 0 keeps them
	maxEdge  int    // 0 sends the original
}

// sshArgs keeps a master connection for half an hour, so sends after the
// first one skip the handshake. The socket lives in /tmp because macOS
// limits socket paths to 104 bytes, which the per-user temp dir nearly
// fills.
func sshArgs(host string, extra ...string) []string {
	return append([]string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=/tmp/clip-share-%C",
		"-o", "ControlPersist=30m",
		"-o", "ServerAliveInterval=30",
		host,
	}, extra...)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// remoteDirExpr is dir as a shell word, with ~/ expanded by the shell.
func remoteDirExpr(dir string) string {
	if dir == "~" {
		return `"$HOME"`
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return `"$HOME"/` + shellQuote(rest)
	}
	return shellQuote(dir)
}

// remoteCommand writes stdin to the file and prints its absolute path. It
// runs under sh, whatever the login shell (fish, nu) would make of it.
func remoteCommand(o sendOptions, name string) string {
	d := remoteDirExpr(o.dir)
	script := fmt.Sprintf(`set -e; mkdir -p %[1]s; cd %[1]s; cat > %[2]s; `, d, name)
	if o.keepDays > 0 {
		script += fmt.Sprintf(`find . -maxdepth 1 -type f \( -name '*.png' -o -name '*.jpg' \) -mtime +%d -exec rm -f {} + 2>/dev/null || true; `, o.keepDays)
	}
	script += fmt.Sprintf(`printf '%%s/%%s\n' "$(pwd)" %s`, name)
	return "sh -c " + shellQuote(script)
}

// warm opens the master connection ahead of a send, so picking an image
// covers the handshake. It returns at once when one is open.
func (s sender) warm(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if s.run(ctx, "/usr/bin/ssh", sshArgs(host, "-O", "check")...).Run() == nil {
		return nil
	}
	var stderr bytes.Buffer
	cmd := s.run(ctx, "/usr/bin/ssh", sshArgs(host, "true")...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return sshError(err, &stderr)
	}
	return nil
}

func sshError(err error, stderr *bytes.Buffer) error {
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = err.Error()
	}
	return errors.New(lastLine(msg))
}

// progressReader reports how much of the upload ssh has taken.
type progressReader struct {
	r      *bytes.Reader
	total  int64
	report func(done, total int64)
	last   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if now := time.Now(); p.report != nil && (now.Sub(p.last) > 100*time.Millisecond || p.r.Len() == 0) {
		p.last = now
		p.report(p.total-int64(p.r.Len()), p.total)
	}
	return n, err
}

func (s sender) send(ctx context.Context, host string, sh shot, o sendOptions, progress func(done, total int64)) (string, error) {
	if host == "" {
		return "", errors.New(locales.ErrNoHost())
	}
	up, err := prepareUpload(sh.Path, o.maxEdge)
	if err != nil {
		return "", err
	}
	name := sh.ID + up.ext
	if o.maxEdge > 0 {
		name = fmt.Sprintf("%s-%d%s", sh.ID, o.maxEdge, up.ext)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// A GUI app's PATH lacks /usr/local and Homebrew: use the system ssh,
	// which reads the same config and agent as the terminal.
	cmd := s.run(ctx, "/usr/bin/ssh", sshArgs(host, remoteCommand(o, name))...)
	cmd.Stdin = &progressReader{r: bytes.NewReader(up.data), total: int64(len(up.data)), report: progress}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", sshError(err, &stderr)
	}
	path := lastLine(strings.TrimSpace(stdout.String()))
	if !strings.HasSuffix(path, "/"+name) {
		return "", errors.New(locales.ErrBadPath(path))
	}
	return path, nil
}

// lastLine drops banners and MOTDs that login shells print before the
// output that matters.
func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}
