package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"hd2-mygo-cpv/assets"
)

const (
	FactionHumans     = 1
	FactionTerminids  = 2
	FactionAutomatons = 3
	FactionIlluminate = 4
)

func FactionNameZH(f int) string {
	switch f {
	case FactionHumans:
		return "超级地球"
	case FactionTerminids:
		return "终结族"
	case FactionAutomatons:
		return "机器人"
	case FactionIlluminate:
		return "光能者"
	default:
		return "未知"
	}
}

type Effect struct {
	ID          int      `json:"id"`
	EffectType  int      `json:"effectType"`
	Name        string   `json:"name"`
	EnglishName string   `json:"englishName"`
	Native      string   `json:"native"`
	GroupKey    string   `json:"groupKey"`
	GroupLabel  string   `json:"groupLabel"`
	Aliases     []string `json:"aliases"`
	RefNote     string   `json:"reference_note"`
	Experimental bool    `json:"experimental"`
}

type Planet struct {
	ID            int     `json:"id"`
	NameEN        string  `json:"en"`
	NameZH        string  `json:"zh"`
	SectorEN      string  `json:"sector_en"`
	SectorZH      string  `json:"sector_zh"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Owner         int     `json:"owner"`
	InitialOwner  int     `json:"initial_owner"`
	Health        float64 `json:"health"`
	MaxHealth     float64 `json:"max_health"`
	Players       int     `json:"players"`
	RegenPerSec   float64 `json:"regen_per_sec"`
	ActiveEffects []int   `json:"active_effects"`
	HasCampaign    bool    `json:"has_campaign"`
	CampaignType   int     `json:"campaign_type"`
	CampaignRace   int     `json:"campaign_race"`
	CampaignCount  int     `json:"campaign_count"`
	IsDefense      bool    `json:"is_defense"`
	EventHealth    float64 `json:"event_health"`
	EventMaxHealth float64 `json:"event_max_health"`
	Waypoints      []int   `json:"waypoints"`
	Hidden        bool    `json:"hidden"`
	InGloom       bool    `json:"in_gloom"`
	InTCS         bool    `json:"in_tcs"`
	Unlisted      bool    `json:"unlisted"`
}

func (p *Planet) DisplayName() string {
	if p.NameZH != "" {
		return p.NameZH
	}
	if p.NameEN != "" {
		return p.NameEN
	}
	return fmt.Sprintf("星球 #%d", p.ID)
}

func (p *Planet) LiberationPct() float64 {
	if p.IsDefense && p.EventMaxHealth > 0 {
		v := (1.0 - p.EventHealth/p.EventMaxHealth) * 100.0
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}
	if p.Owner == FactionHumans {
		return 100
	}
	if p.MaxHealth <= 0 {
		return 0
	}
	v := (1.0 - p.Health/p.MaxHealth) * 100.0
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

type Edge struct {
	From int
	To   int
}

type BattleRoute struct {
	Source   int
	Target   int
	Attacker int
}

type SectorInfo struct {
	Name        string
	PlanetCount int
	LibPct      float64
	EnemyRaces  []int
	Dominant    int // 1 if all Humans, else dominant enemy race
	HasEnemy    bool
	CentroidX   float64
	CentroidY   float64
	Rings       [][][2]float64 // Outer boundary rings in galaxy coordinates
}

type GalaxyData struct {
	WarID          int
	StatusTime     float64
	Planets        []*Planet
	PlanetsByID    map[int]*Planet
	Edges          []Edge
	BattleRoutes   []BattleRoute
	Sectors        []*SectorInfo
	SectorsByName  map[string]*SectorInfo
	Effects        []Effect
	EffectsByID    map[int]Effect
	GroupLabels    []string // Ordered group labels for effect picker
	LastUpdatedAt  time.Time
	LiveSourceNote string
}

type warCatalogFile struct {
	Effects []Effect `json:"effects"`
	Planets []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Native string `json:"native"`
	} `json:"planets"`
}

type starmapFile struct {
	Systems []struct {
		Name    string `json:"name"`
		NameEN  string `json:"name_en"`
		Planets []struct {
			EN string `json:"en"`
			CN string `json:"cn"`
		} `json:"planets"`
	} `json:"systems"`
	Planets map[string]struct {
		CN       string `json:"cn"`
		System   string `json:"system"`
		ID       *int   `json:"ID"`
		SectorEN string `json:"sector_en"`
		Position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
	} `json:"planets"`
}

type waypointsFile struct {
	Waypoints map[string][]int `json:"waypoints"`
}

type warInfoJSON struct {
	WarID         int `json:"warId"`
	LayoutVersion int `json:"layoutVersion"`
	PlanetInfos   []struct {
		Index        int     `json:"index"`
		Position     struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
		Waypoints    []int   `json:"waypoints"`
		Sector       int     `json:"sector"`
		MaxHealth    float64 `json:"maxHealth"`
		Disabled     bool    `json:"disabled"`
		InitialOwner int     `json:"initialOwner"`
	} `json:"planetInfos"`
}

type statusJSON struct {
	WarID        int     `json:"warId"`
	Time         float64 `json:"time"`
	PlanetStatus []struct {
		Index          int     `json:"index"`
		Owner          int     `json:"owner"`
		Health         float64 `json:"health"`
		RegenPerSecond float64 `json:"regenPerSecond"`
		Players        int     `json:"players"`
		Position       struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
	} `json:"planetStatus"`
	PlanetAttacks []struct {
		Source int `json:"source"`
		Target int `json:"target"`
	} `json:"planetAttacks"`
	Campaigns []struct {
		ID          int `json:"id"`
		PlanetIndex int `json:"planetIndex"`
		Type        int `json:"type"`
		Count       int `json:"count"`
		Race        int `json:"race"`
	} `json:"campaigns"`
	PlanetEvents []struct {
		ID          int     `json:"id"`
		PlanetIndex int     `json:"planetIndex"`
		EventType   int     `json:"eventType"`
		Race        int     `json:"race"`
		Health      float64 `json:"health"`
		MaxHealth   float64 `json:"maxHealth"`
	} `json:"planetEvents"`
	PlanetActiveEffects []struct {
		Index            int `json:"index"`
		GalacticEffectID int `json:"galacticEffectId"`
	} `json:"planetActiveEffects"`
	GlobalEvents []struct {
		EventID       int   `json:"eventId"`
		EffectIDs     []int `json:"effectIds"`
		PlanetIndices []int `json:"planetIndices"`
	} `json:"globalEvents"`
}

// LoadEmbedded builds the complete initial GalaxyData from bundled offline assets.
func LoadEmbedded() (*GalaxyData, error) {
	return buildGalaxyData(assets.WarInfoJSON, assets.StatusSampleJSON, "内置离线快照")
}

func buildGalaxyData(warInfoRaw, statusRaw []byte, sourceNote string) (*GalaxyData, error) {
	var wc warCatalogFile
	if err := json.Unmarshal(assets.WarCatalogJSON, &wc); err != nil {
		return nil, fmt.Errorf("解析 war_catalog.json 失败: %w", err)
	}
	var sm starmapFile
	if err := json.Unmarshal(assets.StarmapJSON, &sm); err != nil {
		return nil, fmt.Errorf("解析 starmap.json 失败: %w", err)
	}
	var wp waypointsFile
	if err := json.Unmarshal(assets.WaypointsJSON, &wp); err != nil {
		return nil, fmt.Errorf("解析 waypoints.json 失败: %w", err)
	}
	var wi warInfoJSON
	if err := json.Unmarshal(warInfoRaw, &wi); err != nil {
		return nil, fmt.Errorf("解析 WarInfo 失败: %w", err)
	}
	var st statusJSON
	if err := json.Unmarshal(statusRaw, &st); err != nil {
		return nil, fmt.Errorf("解析 Status 失败: %w", err)
	}

	g := &GalaxyData{
		WarID:          st.WarID,
		StatusTime:     st.Time,
		PlanetsByID:    make(map[int]*Planet),
		SectorsByName:  make(map[string]*SectorInfo),
		Effects:        wc.Effects,
		EffectsByID:    make(map[int]Effect, len(wc.Effects)),
		LastUpdatedAt:  time.Now(),
		LiveSourceNote: sourceNote,
	}
	if g.WarID == 0 {
		g.WarID = wi.WarID
	}

	groupSeen := make(map[string]bool)
	for _, eff := range wc.Effects {
		g.EffectsByID[eff.ID] = eff
		lbl := eff.GroupLabel
		if lbl == "" {
			lbl = "其他"
		}
		if !groupSeen[lbl] {
			groupSeen[lbl] = true
			g.GroupLabels = append(g.GroupLabels, lbl)
		}
	}

	// Build name & sector lookup maps
	cpvNames := make(map[int]struct{ zh, en string }, len(wc.Planets))
	for _, p := range wc.Planets {
		cpvNames[p.ID] = struct{ zh, en string }{p.Name, p.Native}
	}
	starByID := make(map[int]struct {
		cn, en, sysZH, sysEN string
	}, len(sm.Planets))
	for enKey, row := range sm.Planets {
		if row.ID == nil {
			continue
		}
		starByID[*row.ID] = struct {
			cn, en, sysZH, sysEN string
		}{row.CN, enKey, row.System, row.SectorEN}
	}

	// Seed planets from WarInfo
	for _, info := range wi.PlanetInfos {
		p := &Planet{
			ID:           info.Index,
			X:            info.Position.X,
			Y:            info.Position.Y,
			MaxHealth:    info.MaxHealth,
			Health:       info.MaxHealth,
			InitialOwner: info.InitialOwner,
			Owner:        info.InitialOwner,
			Waypoints:    append([]int(nil), info.Waypoints...),
		}
		if smRow, ok := starByID[p.ID]; ok {
			p.NameZH = smRow.cn
			p.NameEN = smRow.en
			p.SectorZH = smRow.sysZH
			p.SectorEN = smRow.sysEN
		}
		if cpv, ok := cpvNames[p.ID]; ok {
			if p.NameZH == "" {
				p.NameZH = cpv.zh
			}
			if p.NameEN == "" {
				p.NameEN = cpv.en
			}
		}
		if p.ID == 264 || p.ID == 265 || p.SectorZH == "" || strings.EqualFold(p.SectorZH, "TBD") {
			p.Unlisted = true
		}
		g.PlanetsByID[p.ID] = p
	}

	// Merge waypoints.json edges as supplement
	for kStr, targets := range wp.Waypoints {
		pid, err := strconv.Atoi(kStr)
		if err != nil {
			continue
		}
		if p, ok := g.PlanetsByID[pid]; ok {
			seen := make(map[int]bool, len(p.Waypoints))
			for _, dst := range p.Waypoints {
				seen[dst] = true
			}
			for _, dst := range targets {
				if !seen[dst] {
					p.Waypoints = append(p.Waypoints, dst)
					seen[dst] = true
				}
			}
		}
	}

	g.applyStatusStruct(&st, sourceNote)
	return g, nil
}

// ApplyWarData updates planet metadata from WarInfo (if provided) and live Status.
func (g *GalaxyData) ApplyWarData(warInfoRaw, statusRaw []byte, sourceNote string) error {
	if len(warInfoRaw) > 0 {
		var wi warInfoJSON
		if err := json.Unmarshal(warInfoRaw, &wi); err == nil && len(wi.PlanetInfos) > 0 {
			for _, info := range wi.PlanetInfos {
				if p, ok := g.PlanetsByID[info.Index]; ok {
					if info.MaxHealth > 0 {
						p.MaxHealth = info.MaxHealth
					}
					if len(info.Waypoints) > 0 {
						p.Waypoints = append([]int(nil), info.Waypoints...)
					}
					if info.Position.X != 0 || info.Position.Y != 0 || info.Index == 0 {
						p.X = info.Position.X
						p.Y = info.Position.Y
					}
				}
			}
		}
	}
	return g.ApplyStatusBytes(statusRaw, sourceNote)
}

// ApplyStatusBytes updates live planet statuses, campaigns, attacks, effects and sectors from raw Status JSON.
func (g *GalaxyData) ApplyStatusBytes(statusRaw []byte, sourceNote string) error {
	var st statusJSON
	if err := json.Unmarshal(statusRaw, &st); err != nil {
		return err
	}
	if len(st.PlanetStatus) == 0 {
		return fmt.Errorf("Status 缺少 planetStatus")
	}
	g.applyStatusStruct(&st, sourceNote)
	return nil
}

func (g *GalaxyData) applyStatusStruct(st *statusJSON, sourceNote string) {
	if st.WarID > 0 {
		g.WarID = st.WarID
	}
	if st.Time > 0 {
		g.StatusTime = st.Time
	}
	g.LastUpdatedAt = time.Now()
	if sourceNote != "" {
		g.LiveSourceNote = sourceNote
	}

	// Reset dynamic fields
	for _, p := range g.PlanetsByID {
		p.ActiveEffects = p.ActiveEffects[:0]
		p.HasCampaign = false
		p.CampaignType = 0
		p.CampaignRace = 0
		p.CampaignCount = 0
		p.IsDefense = false
		p.EventHealth = 0
		p.EventMaxHealth = 0
		p.Hidden = false
		p.InGloom = false
		p.InTCS = false
	}

	for _, ps := range st.PlanetStatus {
		p, ok := g.PlanetsByID[ps.Index]
		if !ok {
			p = &Planet{
				ID:       ps.Index,
				X:        ps.Position.X,
				Y:        ps.Position.Y,
				Unlisted: true,
			}
			g.PlanetsByID[ps.Index] = p
		}
		p.Owner = ps.Owner
		p.Health = ps.Health
		p.RegenPerSec = ps.RegenPerSecond
		p.Players = ps.Players
		if ps.Position.X != 0 || ps.Position.Y != 0 || ps.Index == 0 {
			p.X = ps.Position.X
			p.Y = ps.Position.Y
		}
	}

	addPlanetEffect := func(pid, eid int) {
		if p, ok := g.PlanetsByID[pid]; ok {
			if !slices.Contains(p.ActiveEffects, eid) {
				p.ActiveEffects = append(p.ActiveEffects, eid)
			}
			if eid == 1188 || eid == 1193 {
				p.InGloom = true
			}
			if eid == 1395 {
				p.InTCS = true
			}
			if eid == 1190 {
				p.Hidden = true
			}
		}
	}
	for _, ae := range st.PlanetActiveEffects {
		addPlanetEffect(ae.Index, ae.GalacticEffectID)
	}
	for _, ge := range st.GlobalEvents {
		if len(ge.PlanetIndices) == 0 || len(ge.EffectIDs) == 0 {
			continue
		}
		for _, pid := range ge.PlanetIndices {
			for _, eid := range ge.EffectIDs {
				addPlanetEffect(pid, eid)
			}
		}
	}
	for _, p := range g.PlanetsByID {
		slices.Sort(p.ActiveEffects)
		if p.InGloom {
			p.Hidden = false
		}
	}

	for _, c := range st.Campaigns {
		if p, ok := g.PlanetsByID[c.PlanetIndex]; ok {
			p.HasCampaign = true
			p.CampaignType = c.Type
			p.CampaignRace = c.Race
			p.CampaignCount = c.Count
			if c.Type == 4 {
				p.IsDefense = true
			}
		}
	}
	for _, ev := range st.PlanetEvents {
		if ev.EventType == 1 {
			if p, ok := g.PlanetsByID[ev.PlanetIndex]; ok {
				p.IsDefense = true
				p.EventHealth = ev.Health
				p.EventMaxHealth = ev.MaxHealth
				if p.CampaignRace == 0 {
					p.CampaignRace = ev.Race
				}
			}
		}
	}

	// Build sorted slice of listed planets
	g.Planets = make([]*Planet, 0, len(g.PlanetsByID))
	for _, p := range g.PlanetsByID {
		if !p.Unlisted && !math.IsNaN(p.X) && !math.IsNaN(p.Y) {
			g.Planets = append(g.Planets, p)
		}
	}
	slices.SortFunc(g.Planets, func(a, b *Planet) int {
		return a.ID - b.ID
	})

	// Build undirected unique edges from waypoints
	edgeSeen := make(map[uint64]bool)
	g.Edges = g.Edges[:0]
	for _, p := range g.Planets {
		for _, dst := range p.Waypoints {
			dp, ok := g.PlanetsByID[dst]
			if !ok || dp.Unlisted {
				continue
			}
			a, b := p.ID, dst
			if a > b {
				a, b = b, a
			}
			key := uint64(a)<<32 | uint64(uint32(b))
			if !edgeSeen[key] {
				edgeSeen[key] = true
				g.Edges = append(g.Edges, Edge{From: p.ID, To: dst})
			}
		}
	}

	// Build battle routes ONLY for planets with active defense campaigns (matching galaxy-map-v2.html)
	g.BattleRoutes = g.BattleRoutes[:0]
	for _, p := range g.Planets {
		if !p.IsDefense {
			continue
		}
		var neighbours []int
		for _, atk := range st.PlanetAttacks {
			if atk.Source == p.ID {
				neighbours = append(neighbours, atk.Target)
			} else if atk.Target == p.ID {
				neighbours = append(neighbours, atk.Source)
			}
		}
		if len(neighbours) == 0 {
			continue
		}
		origin := neighbours[0]
		for _, nb := range neighbours {
			if np, ok := g.PlanetsByID[nb]; ok && np.Owner == p.CampaignRace && p.CampaignRace != 0 {
				origin = nb
				break
			}
		}
		attacker := p.CampaignRace
		if attacker == 0 {
			if op, ok := g.PlanetsByID[origin]; ok {
				attacker = op.Owner
			}
		}
		g.BattleRoutes = append(g.BattleRoutes, BattleRoute{
			Source:   origin,
			Target:   p.ID,
			Attacker: attacker,
		})
	}

	// Compute polar grid sector boundaries and stats
	g.Sectors, g.SectorsByName = BuildSectors(g.Planets)
}

// RefreshFromLiveAPI fetches live WarInfo and Status from HD2's official API.
// If upstreamProxyURL is non-empty it routes through that proxy; otherwise it bypasses env proxies.
func RefreshFromLiveAPI(ctx context.Context, warID int, upstreamProxyURL ...string) (warInfoRaw, statusRaw, effectsRaw []byte, err error) {
	if warID <= 0 {
		warID = 801
	}
	transport := &http.Transport{
		Proxy: nil,
	}
	if len(upstreamProxyURL) > 0 && strings.TrimSpace(upstreamProxyURL[0]) != "" {
		if pu, pErr := url.Parse(strings.TrimSpace(upstreamProxyURL[0])); pErr == nil && pu.Host != "" {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	client := &http.Client{
		Timeout:   12 * time.Second,
		Transport: transport,
	}
	fetch := func(targetURL string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "HD2-MY-CPV/1.0")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Accept-Language", "en-US")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		if resp.StatusCode != http.StatusOK {
			msg := strings.TrimSpace(string(body))
			if len(msg) > 120 {
				msg = msg[:120]
			}
			if msg != "" {
				return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
			}
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if readErr != nil {
			return nil, readErr
		}
		return body, nil
	}

	base := fmt.Sprintf("https://api.live.prod.thehelldiversgame.com/api/WarSeason/%d", warID)
	statusRaw, err = fetch(base + "/Status")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("拉取实时战况失败: %w", err)
	}
	warInfoRaw, _ = fetch(base + "/WarInfo")
	effectsRaw, _ = fetch("https://api.live.prod.thehelldiversgame.com/api/WarSeason/GalacticWarEffects")
	return warInfoRaw, statusRaw, effectsRaw, nil
}
