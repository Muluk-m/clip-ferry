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
)

// remoteDir is where images land on the remote machine. Images older than
// remoteKeepDays are removed there on each send.
const (
	remoteDir      = "~/.cache/clip-share"
	remoteKeepDays = 7
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

// sshArgs keeps a master connection for a few minutes, so sends after the
// first one skip the handshake. The socket lives in /tmp because macOS
// limits socket paths to 104 bytes, which the per-user temp dir nearly
// fills.
func sshArgs(host, remoteCmd string) []string {
	return []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=/tmp/clip-share-%C",
		"-o", "ControlPersist=10m",
		host, remoteCmd,
	}
}

// remoteCommand writes stdin to the file and prints its absolute path.
// The name is a hex ID, so it needs no quoting.
func remoteCommand(id string) string {
	name := id + ".png"
	return fmt.Sprintf(
		`mkdir -p %[1]s && cat > %[1]s/%[2]s && find %[1]s -name '*.png' -mtime +%[3]d -delete; echo "$HOME/.cache/clip-share/%[2]s"`,
		remoteDir, name, remoteKeepDays)
}

func (s sender) send(ctx context.Context, host string, sh shot) (string, error) {
	if host == "" {
		return "", errors.New("未选择远端主机")
	}
	data, err := os.ReadFile(sh.Path)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// A GUI app's PATH lacks /usr/local and Homebrew: use the system ssh,
	// which reads the same config and agent as the terminal.
	cmd := s.run(ctx, "/usr/bin/ssh", sshArgs(host, remoteCommand(sh.ID))...)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(lastLine(msg))
	}
	path := lastLine(strings.TrimSpace(stdout.String()))
	if !strings.HasSuffix(path, sh.ID+".png") {
		return "", fmt.Errorf("远端返回了意外的路径：%q", path)
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
