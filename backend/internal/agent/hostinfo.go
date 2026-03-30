package agent

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

var hostEnvOnce sync.Once
var hostEnvLine string

// cachedHostEnvironment returns a one-line description of the OS family, version detail when available, and CPU arch.
func cachedHostEnvironment() string {
	hostEnvOnce.Do(func() {
		hostEnvLine = buildHostEnvironmentLine()
	})
	return hostEnvLine
}

func buildHostEnvironmentLine() string {
	osName := goosDisplayName(runtime.GOOS)
	arch := runtime.GOARCH
	detail := strings.TrimSpace(osVersionDetail())
	var b strings.Builder
	b.WriteString(osName)
	if detail != "" {
		b.WriteString(" ")
		b.WriteString(detail)
	}
	b.WriteString(", ")
	b.WriteString(arch)
	return b.String()
}

func goosDisplayName(goos string) string {
	switch goos {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "freebsd":
		return "FreeBSD"
	default:
		if goos == "" {
			return "unknown OS"
		}
		return strings.ToUpper(goos[:1]) + goos[1:]
	}
}

func osVersionDetail() string {
	switch runtime.GOOS {
	case "linux":
		return strings.TrimSpace(linuxPrettyName())
	case "darwin":
		return strings.TrimSpace(darwinProductVersion())
	case "windows":
		return strings.TrimSpace(windowsVerOutput())
	default:
		return ""
	}
}

func linuxPrettyName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil || len(b) == 0 {
		return ""
	}
	var name, version string
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name = unquoteOSReleaseValue(strings.TrimPrefix(line, "PRETTY_NAME="))
			break
		}
	}
	if name == "" {
		s = bufio.NewScanner(bytes.NewReader(b))
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if strings.HasPrefix(line, "NAME=") {
				name = unquoteOSReleaseValue(strings.TrimPrefix(line, "NAME="))
			}
			if strings.HasPrefix(line, "VERSION_ID=") {
				version = unquoteOSReleaseValue(strings.TrimPrefix(line, "VERSION_ID="))
			}
		}
		if name != "" && version != "" {
			return name + " " + version
		}
	}
	return name
}

func unquoteOSReleaseValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strings.Trim(v, `"`)
	}
	return v
}

func darwinProductVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func windowsVerOutput() string {
	out, err := exec.Command("cmd", "/c", "ver").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
