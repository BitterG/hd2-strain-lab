package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

// Plan is the top-level dispatch plan sent to the proxy.
type Plan struct {
	Schema  int            `json:"schema"`
	Targets []PlanetTarget `json:"targets"`
}

// Settings stores non-sensitive local application preferences.
type Settings struct {
	UseUpstreamProxy bool    `json:"use_upstream_proxy"`
	UpstreamProxyURL string  `json:"upstream_proxy_url"` // e.g. http://127.0.0.1:7890
	BindPort         int     `json:"bind_port"`          // 0 = ephemeral loopback port
	AllowAllEffects  bool    `json:"allow_all_effects"`  // Allow dispatching catalog effects even before live /GalacticWarEffects is seen (useful for offline/experimental)
	FontScale        float64 `json:"font_scale,omitempty"`
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

func DefaultSettings() Settings {
	return Settings{
		UseUpstreamProxy: false,
		UpstreamProxyURL: "",
		BindPort:         0,
		AllowAllEffects:  false,
		FontScale:        1.0,
	}
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
		t.Campaign = t.Campaign || t.ForceOpen
		if t.InitializeRegions && (!t.Campaign || (t.CampaignRace != 2 && t.CampaignRace != 3 && t.CampaignRace != 4)) {
			return nil, fmt.Errorf("星球 #%d 初始化分区需要开启战役并明确选择敌人阵营", t.Planet)
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

func SavePlan(profileDir string, p Plan) error {
	if p.Schema == 0 {
		p.Schema = 2
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(profileDir, "war-targets.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	return s, nil
}

func SaveSettings(profileDir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(profileDir, "settings.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
