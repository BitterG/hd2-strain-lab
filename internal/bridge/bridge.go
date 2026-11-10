package bridge

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"hd2-strain-lab/internal/config"
)

const stillActive = 259

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// SteamLaunchOption returns the exact launch option string to paste into Steam.
func SteamLaunchOption(exePath string) string {
	return fmt.Sprintf(`"%s" --steam-bridge %%command%%`, exePath)
}

// RunSteamBridge implements `--steam-bridge <game_exe> [args...]`.
// When the proxy is active and reachable, it injects HTTP_PROXY/HTTPS_PROXY/NO_PROXY
// into the launched game process; otherwise it launches the game unmodified.
func RunSteamBridge(args []string) int {
	if len(args) < 1 {
		return 2
	}
	profileDir, err := config.ProfileDir()
	proxyActive := false
	proxyPort := 0
	if err == nil {
		if port, pid, ok := config.ReadSteamProxyINI(profileDir); ok && processAlive(pid) {
			conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close()
				proxyActive = true
				proxyPort = port
			}
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if proxyActive {
		proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
		prevNoProxy := os.Getenv("NO_PROXY")
		bypassParts := []string{}
		if prevNoProxy != "" {
			bypassParts = append(bypassParts, prevNoProxy)
		}
		bypassParts = append(bypassParts,
			"localhost", "127.0.0.1", "::1",
			"playfabapi.com", ".playfabapi.com",
			"playfab.com", ".playfab.com",
			"easyanticheat.net", ".easyanticheat.net",
			"easyanticheat.com", ".easyanticheat.com",
			"epicgames.com", ".epicgames.com",
			"epicgames.dev", ".epicgames.dev",
		)
		bypass := strings.Join(bypassParts, ",")

		env := os.Environ()
		filtered := make([]string, 0, len(env)+6)
		for _, kv := range env {
			k, _, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(k) {
			case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY":
				continue
			default:
				filtered = append(filtered, kv)
			}
		}
		filtered = append(filtered,
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
			"http_proxy="+proxyURL,
			"https_proxy="+proxyURL,
			"NO_PROXY="+bypass,
			"no_proxy="+bypass,
		)
		cmd.Env = filtered
	}

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
