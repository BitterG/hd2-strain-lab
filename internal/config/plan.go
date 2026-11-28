package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// PlanetTarget represents a single planet's custom variant & campaign override configuration.
// Schema is compatible with CPV v2 target plans.
type PlanetTarget struct {
	Planet                int    `json:"planet"`
	Enabled               bool   `json:"enabled"`
	Effects               []int  `json:"effects"`
	DisabledNativeEffects []int  `json:"disabled_native_effects,omitempty"`
	Owner                 int    `json:"owner"`             // 0=Keep original, 1=Humans, 2=Terminids, 3=Automatons, 4=Illuminate
	Campaign          bool   `json:"campaign"`          // Enable/create campaign
	ForceOpen         bool   `json:"force_open"`        // Force open even if planet health is 0
	CampaignType      int    `json:"campaign_type"`     // 0=Liberation, 3=City/Special, 4=Defense
	CampaignRace      int    `json:"campaign_race"`     // 0=Auto infer, 2=Terminids, 3=Automatons, 4=Illuminate
	CampaignCount     int    `json:"campaign_count"`    // Default 20
	OpenRegions       bool   `json:"open_regions"`      // Open existing planet regions
	InitializeRegions bool   `json:"initialize_regions"`
	DefenseSource     int    `json:"defense_source"` // Enemy source planet ID (-1 if unset)
	DefenseLevel      int    `json:"defense_level"`  // 1..1000, default 20 (each level = 50,000 HP)
	DefenseHours      int    `json:"defense_hours"`  // 1..168, default 48
	DefenseCount      int    `json:"defense_count"`  // 1..1000, default 38
	Note              string `json:"note,omitempty"`
}

// DefaultTarget creates a PlanetTarget with sensible defaults for a planet.
func DefaultTarget(planetID int) PlanetTarget {
	return PlanetTarget{
		Planet:        planetID,
		Enabled:       true,
		Effects:       []int{},
		Owner:         0,
		Campaign:      false,
		ForceOpen:     false,
		CampaignType:  0,
		CampaignRace:  0,
		CampaignCount: 20,
		DefenseSource: -1,
		DefenseLevel:  20,
		DefenseHours:  48,
		DefenseCount:  38,
	}
}

// Clone returns a deep copy of PlanetTarget so callers cannot alias slices.
func (t PlanetTarget) Clone() PlanetTarget {
	cp := t
	cp.Effects = slices.Clone(t.Effects)
	cp.DisabledNativeEffects = slices.Clone(t.DisabledNativeEffects)
	return cp
}

// Plan is the top-level dispatch plan sent to the proxy.
type Plan struct {
	Schema                int            `json:"schema"`
	Targets               []PlanetTarget `json:"targets"`
	DisabledGlobalEffects []int          `json:"disabled_global_effects,omitempty"`
	EffectValueOverrides  map[int]int    `json:"effect_value_overrides,omitempty"`
}

// Clone returns a deep copy of Plan so UI and proxy never share mutable slices or maps.
func (p Plan) Clone() Plan {
	cp := Plan{
		Schema:                p.Schema,
		DisabledGlobalEffects: slices.Clone(p.DisabledGlobalEffects),
	}
	if p.Targets != nil {
		cp.Targets = make([]PlanetTarget, len(p.Targets))
		for i, t := range p.Targets {
			cp.Targets[i] = t.Clone()
		}
	}
	if len(p.EffectValueOverrides) > 0 {
		cp.EffectValueOverrides = make(map[int]int, len(p.EffectValueOverrides))
		for k, v := range p.EffectValueOverrides {
			cp.EffectValueOverrides[k] = v
		}
	}
	return cp
}

const (
	DefaultLeftSidebarWidth  = 312.0
	DefaultRightSidebarWidth = 348.0
	MinSidebarWidth          = 220.0
	MaxSidebarWidth          = 560.0
)

// Settings stores non-sensitive local application preferences.
type Settings struct {
	UseUpstreamProxy  bool              `json:"use_upstream_proxy"`
	UpstreamProxyURL  string            `json:"upstream_proxy_url"` // e.g. http://127.0.0.1:7890
	BindPort          int               `json:"bind_port"`          // 0 = ephemeral loopback port
	AllowAllEffects   bool              `json:"allow_all_effects"`  // Allow dispatching catalog effects even before live /GalacticWarEffects is seen (useful for offline/experimental)
	FontScale         float64           `json:"font_scale,omitempty"`
	LeftSidebarWidth  float64           `json:"left_sidebar_width,omitempty"`
	RightSidebarWidth float64           `json:"right_sidebar_width,omitempty"`
	PanelSides        map[string]string `json:"panel_sides,omitempty"`
}

// NormalizeFontScale clamps a UI font scale factor to [0.80, 1.50], defaulting <= 0 to 1.0.
func NormalizeFontScale(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 1.0
	}
	if v < 0.80 {
		v = 0.80
	}
	if v > 1.50 {
		v = 1.50
	}
	return math.Round(v*100) / 100
}

// NormalizeSidebarWidth clamps a sidebar width to [MinSidebarWidth, MaxSidebarWidth], falling back if invalid or <= 0.
func NormalizeSidebarWidth(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return fallback
	}
	if v < MinSidebarWidth {
		v = MinSidebarWidth
	}
	if v > MaxSidebarWidth {
		v = MaxSidebarWidth
	}
	return math.Round(v)
}

// DefaultPanelSides returns the default left/right docking side for each panel.
func DefaultPanelSides() map[string]string {
	return map[string]string{
		"planet_nav":    "left",
		"effect_picker": "left",
		"console":       "right",
	}
}

// NormalizePanelSides fills defaults and retains only valid ("left"/"right") panel docking assignments.
func NormalizePanelSides(in map[string]string) map[string]string {
	out := DefaultPanelSides()
	for k, v := range in {
		if _, known := out[k]; !known {
			continue
		}
		if v == "left" || v == "right" {
			out[k] = v
		}
	}
	return out
}

func DefaultSettings() Settings {
	return Settings{
		UseUpstreamProxy:  false,
		UpstreamProxyURL:  "",
		BindPort:          0,
		AllowAllEffects:   false,
		FontScale:         1.0,
		LeftSidebarWidth:  DefaultLeftSidebarWidth,
		RightSidebarWidth: DefaultRightSidebarWidth,
		PanelSides:        DefaultPanelSides(),
	}
}

// EffectiveUpstreamProxyURL returns the configured upstream proxy URL when UseUpstreamProxy is true,
// falling back to the Windows system HTTP proxy if UpstreamProxyURL is empty.
func (s Settings) EffectiveUpstreamProxyURL() string {
	if !s.UseUpstreamProxy {
		return ""
	}
	u := strings.TrimSpace(s.UpstreamProxyURL)
	if u != "" {
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		return u
	}
	return DetectSystemHTTPProxy()
}

// DetectSystemHTTPProxy reads the current user's Windows Internet Settings proxy if enabled.
func DetectSystemHTTPProxy() string {
	cmd := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	text := string(out)
	enabled := false
	var rawServer string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ProxyEnable") && strings.HasSuffix(line, "0x1") {
			enabled = true
		} else if strings.HasPrefix(line, "ProxyServer") {
			if idx := strings.Index(line, "REG_SZ"); idx >= 0 {
				rawServer = strings.TrimSpace(line[idx+len("REG_SZ"):])
			}
		}
	}
	if !enabled || rawServer == "" {
		return ""
	}
	if strings.Contains(rawServer, "=") {
		picked := ""
		for _, part := range strings.Split(rawServer, ";") {
			part = strings.TrimSpace(part)
			if v, ok := strings.CutPrefix(part, "http="); ok {
				picked = strings.TrimSpace(v)
				break
			}
		}
		rawServer = picked
	}
	if rawServer == "" {
		return ""
	}
	if !strings.Contains(rawServer, "://") {
		rawServer = "http://" + rawServer
	}
	return rawServer
}

// ProfileDir returns the persistent profile directory (%APPDATA%\HD2-Strain-Lab or override),
// automatically migrating files from legacy %APPDATA%\HD2-MY-CPV on first launch.
func ProfileDir() (string, error) {
	for _, key := range []string{"HD2_STRAIN_LAB_PROFILE", "HD2_MY_CPV_PROFILE"} {
		if env := strings.TrimSpace(os.Getenv(key)); env != "" {
			if err := os.MkdirAll(env, 0o755); err != nil {
				return "", err
			}
			return env, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return "", err
		}
		base = filepath.Dir(exe)
	}
	dir := filepath.Join(base, "HD2-Strain-Lab")
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		legacyDir := filepath.Join(base, "HD2-MY-CPV")
		if entries, readErr := os.ReadDir(legacyDir); readErr == nil {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				if raw, rErr := os.ReadFile(filepath.Join(legacyDir, ent.Name())); rErr == nil {
					_ = os.WriteFile(filepath.Join(dir, ent.Name()), raw, 0o600)
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// NormalizeTargets validates and deduplicates enabled targets in a plan.
func NormalizeTargets(plan Plan) ([]PlanetTarget, error) {
	if plan.Schema != 0 && plan.Schema != 2 {
		return nil, fmt.Errorf("不支持的方案版本: %d", plan.Schema)
	}
	if len(plan.Targets) > 64 {
		return nil, errors.New("方案最多包含 64 颗星球")
	}
	seen := make(map[int]bool, len(plan.Targets))
	out := make([]PlanetTarget, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		if !t.Enabled {
			continue
		}
		if t.Planet < 0 || t.Planet > 10000 || seen[t.Planet] {
			return nil, fmt.Errorf("星球索引无效或重复: %d", t.Planet)
		}
		seen[t.Planet] = true
		if len(t.Effects) > 32 {
			return nil, fmt.Errorf("星球 #%d 效果过多 (最多 32 个)", t.Planet)
		}
		dedupEffects := make([]int, 0, len(t.Effects))
		seenEff := make(map[int]bool, len(t.Effects))
		for _, eid := range t.Effects {
			if eid <= 0 {
				return nil, fmt.Errorf("星球 #%d 包含无效效果 ID: %d", t.Planet, eid)
			}
			if !seenEff[eid] {
				seenEff[eid] = true
				dedupEffects = append(dedupEffects, eid)
			}
		}
		t.Effects = dedupEffects
		if len(t.DisabledNativeEffects) > 64 {
			return nil, fmt.Errorf("星球 #%d 取消的原版效果过多", t.Planet)
		}
		dedupDisabled := make([]int, 0, len(t.DisabledNativeEffects))
		seenDis := make(map[int]bool, len(t.DisabledNativeEffects))
		for _, eid := range t.DisabledNativeEffects {
			if eid <= 0 {
				return nil, fmt.Errorf("星球 #%d 包含无效取消效果 ID: %d", t.Planet, eid)
			}
			if !seenDis[eid] {
				seenDis[eid] = true
				dedupDisabled = append(dedupDisabled, eid)
			}
		}
		t.DisabledNativeEffects = dedupDisabled
		if t.Owner < 0 || t.Owner > 4 {
			return nil, fmt.Errorf("星球 #%d 阵营无效: %d", t.Planet, t.Owner)
		}
		if t.CampaignType < 0 || t.CampaignType > 4 {
			return nil, fmt.Errorf("星球 #%d 战役类型无效: %d", t.Planet, t.CampaignType)
		}
		if t.CampaignRace != 0 && t.CampaignRace != 1 && t.CampaignRace != 2 && t.CampaignRace != 3 && t.CampaignRace != 4 {
			return nil, fmt.Errorf("星球 #%d 作战敌人阵营无效: %d", t.Planet, t.CampaignRace)
		}
		t.Campaign = t.Campaign || t.ForceOpen || t.InitializeRegions
		if t.InitializeRegions && t.CampaignRace == 1 {
			return nil, fmt.Errorf("星球 #%d 初始化分区不能将作战敌人设为超级地球", t.Planet)
		}
		if t.CampaignCount == 0 {
			t.CampaignCount = 20
		}
		if t.CampaignCount < 1 || t.CampaignCount > 1000 {
			return nil, fmt.Errorf("星球 #%d 作战数量无效: %d", t.Planet, t.CampaignCount)
		}
		if t.DefenseLevel == 0 {
			t.DefenseLevel = 20
		}
		if t.DefenseHours == 0 {
			t.DefenseHours = 48
		}
		if t.DefenseCount == 0 {
			t.DefenseCount = 38
		}
		if t.DefenseSource < -1 || t.DefenseSource > 10000 ||
			t.DefenseLevel < 1 || t.DefenseLevel > 1000 ||
			t.DefenseHours < 1 || t.DefenseHours > 168 ||
			t.DefenseCount < 1 || t.DefenseCount > 1000 {
			return nil, fmt.Errorf("星球 #%d 防守参数无效", t.Planet)
		}
		if t.CampaignType == 4 {
			if !t.Campaign || t.Owner != 1 || (t.CampaignRace != 2 && t.CampaignRace != 3 && t.CampaignRace != 4) {
				return nil, fmt.Errorf("星球 #%d 防守战役需要开启战役、超级地球归属和明确敌人阵营", t.Planet)
			}
			if t.DefenseSource < 0 || t.DefenseSource == t.Planet {
				return nil, fmt.Errorf("星球 #%d 请选择另一颗敌方星球作为进攻来源", t.Planet)
			}
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b PlanetTarget) int {
		return a.Planet - b.Planet
	})
	return out, nil
}

// NormalizeDisabledGlobalEffects validates, deduplicates, and sorts globally disabled effect IDs.
func NormalizeDisabledGlobalEffects(plan Plan) ([]int, error) {
	if plan.Schema != 0 && plan.Schema != 2 {
		return nil, fmt.Errorf("不支持的方案版本: %d", plan.Schema)
	}
	if len(plan.DisabledGlobalEffects) > 64 {
		return nil, errors.New("全局取消的原版效果过多")
	}
	out := make([]int, 0, len(plan.DisabledGlobalEffects))
	seen := make(map[int]bool, len(plan.DisabledGlobalEffects))
	for _, eid := range plan.DisabledGlobalEffects {
		if eid <= 0 {
			return nil, fmt.Errorf("全局取消包含无效效果 ID: %d", eid)
		}
		if !seen[eid] {
			seen[eid] = true
			out = append(out, eid)
		}
	}
	slices.Sort(out)
	return out, nil
}

// NormalizeEffectValueOverrides validates and clones custom GalacticWarEffects value overrides.
func NormalizeEffectValueOverrides(plan Plan) (map[int]int, error) {
	if plan.Schema != 0 && plan.Schema != 2 {
		return nil, fmt.Errorf("不支持的方案版本: %d", plan.Schema)
	}
	if len(plan.EffectValueOverrides) > 64 {
		return nil, errors.New("自定义效果数值覆盖过多")
	}
	if len(plan.EffectValueOverrides) == 0 {
		return nil, nil
	}
	out := make(map[int]int, len(plan.EffectValueOverrides))
	for eid, val := range plan.EffectValueOverrides {
		if eid <= 0 {
			return nil, fmt.Errorf("自定义数值覆盖包含无效效果 ID: %d", eid)
		}
		if val < -10000 || val > 100000 {
			return nil, fmt.Errorf("效果 #%d 自定义数值超出允许范围: %d", eid, val)
		}
		out[eid] = val
	}
	return out, nil
}

// Digest returns a stable SHA-256 hex digest of the plan.
func (p Plan) Digest() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func LoadPlan(profileDir string) (Plan, error) {
	path := filepath.Join(profileDir, "war-targets.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Plan{Schema: 2, Targets: []PlanetTarget{}}, nil
		}
		return Plan{}, err
	}
	var p Plan
	// Also support raw array format from older war-targets.json
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "[") {
		var list []PlanetTarget
		if err := json.Unmarshal(b, &list); err != nil {
			return Plan{}, err
		}
		p = Plan{Schema: 2, Targets: list}
	} else {
		if err := json.Unmarshal(b, &p); err != nil {
			return Plan{}, err
		}
		if p.Schema == 0 {
			p.Schema = 2
		}
	}
	return p, nil
}

func writeAtomicFile(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func SavePlan(profileDir string, p Plan) error {
	if p.Schema == 0 {
		p.Schema = 2
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(filepath.Join(profileDir, "war-targets.json"), b)
}

func LoadSettings(profileDir string) (Settings, error) {
	path := filepath.Join(profileDir, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSettings(), nil
		}
		return Settings{}, err
	}
	s := DefaultSettings()
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings(), err
	}
	s.FontScale = NormalizeFontScale(s.FontScale)
	s.LeftSidebarWidth = NormalizeSidebarWidth(s.LeftSidebarWidth, DefaultLeftSidebarWidth)
	s.RightSidebarWidth = NormalizeSidebarWidth(s.RightSidebarWidth, DefaultRightSidebarWidth)
	s.PanelSides = NormalizePanelSides(s.PanelSides)
	return s, nil
}

func SaveSettings(profileDir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(filepath.Join(profileDir, "settings.json"), b)
}

// WriteSteamProxyINI writes the active proxy port and PID so --steam-bridge can attach.
func WriteSteamProxyINI(profileDir string, port int, pid int) error {
	content := fmt.Sprintf("port=%d\npid=%d\n", port, pid)
	path := filepath.Join(profileDir, "steam-proxy.ini")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ClearSteamProxyINI removes steam-proxy.ini when the proxy stops.
func ClearSteamProxyINI(profileDir string) {
	_ = os.Remove(filepath.Join(profileDir, "steam-proxy.ini"))
}

// ReadSteamProxyINI reads port and pid from steam-proxy.ini.
func ReadSteamProxyINI(profileDir string) (port int, pid int, ok bool) {
	b, err := os.ReadFile(filepath.Join(profileDir, "steam-proxy.ini"))
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if k, v, found := strings.Cut(line, "="); found {
			switch strings.TrimSpace(k) {
			case "port":
				port, _ = strconv.Atoi(strings.TrimSpace(v))
			case "pid":
				pid, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
	}
	return port, pid, port > 0 && port < 65536 && pid > 0
}
