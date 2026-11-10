package uiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"hd2-mygo-cpv/internal/catalog"
	"hd2-mygo-cpv/internal/certs"
	"hd2-mygo-cpv/internal/config"
	"hd2-mygo-cpv/internal/proxy"
)

type App struct {
	win        *mygo.Window
	theme      *ui.Theme
	profileDir string

	galaxy   *catalog.GalaxyData
	plan     config.Plan
	settings config.Settings
	certsMgr *certs.Manager
	proxySrv *proxy.Server

	starMap     *StarMapState
	selectedIDs []int

	// Left navigator state
	planetQuery     string
	planetFilter    int
	planetListState ui.ListState

	// Right console state
	rightTab        int // 0=Variants, 1=Proxy
	showCampaignCfg bool
	effectQuery     string
	effectGroup     string
	effectListState ui.ListState

	// Draft target being edited for selected planet(s)
	draft          config.PlanetTarget
	draftOwnerStr  string
	draftTypeStr   string
	draftRaceStr   string
	draftCountF    float64
	draftDefLevelF float64
	draftDefHoursF float64
	draftDefCountF float64

	refreshing bool
	toastMsg   string
}

func NewApp() (*App, error) {
	profileDir, err := config.ProfileDir()
	if err != nil {
		return nil, err
	}
	galaxy, err := catalog.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	plan, err := config.LoadPlan(profileDir)
	if err != nil {
		plan = config.Plan{Schema: 2, Targets: []config.PlanetTarget{}}
	}
	settings, _ := config.LoadSettings(profileDir)
	cm, err := certs.NewManager(profileDir)
	if err != nil {
		return nil, err
	}

	app := &App{
		theme:       NewHD2Theme(),
		profileDir:  profileDir,
		galaxy:      galaxy,
		plan:        plan,
		settings:    settings,
		certsMgr:    cm,
		starMap:     NewStarMapState(),
		effectGroup: "全部",
	}

	app.proxySrv = proxy.NewServer(cm, plan, settings, func() {
		if app.win != nil {
			app.win.Invalidate()
		}
	}, func(raw []byte) {
		// Copy raw bytes before hopping onto UI thread
		cp := append([]byte(nil), raw...)
		if app.win != nil {
			app.win.Update(func() {
				_ = app.galaxy.ApplyStatusBytes(cp, "游戏代理实时战况")
			})
		}
	})

	app.syncDraftFromSelection()
	return app, nil
}

func (a *App) AttachWindow(win *mygo.Window) {
	a.win = win
	win.OnClose(func(_ *mygo.CloseEvent) {
		a.stopProxy()
	})
	a.triggerLiveRefresh()
}

func (a *App) View(c *ui.Context) {
	c.SetTheme(a.theme)
	if a.toastMsg != "" {
		c.Toast(a.toastMsg)
		a.toastMsg = ""
	}

	ui.Column(c).Fill().Background(BgDeep).Children(func() {
		a.buildHeader(c)
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			a.buildLeftNavigator(c)
			a.buildStarMap(c)
			a.buildRightConsole(c)
		})
	})
}

func (a *App) buildHeader(c *ui.Context) {
	st := a.proxySrv.Stats()
	counts := map[int]int{}
	for _, p := range a.galaxy.Planets {
		counts[p.Owner]++
	}
	enabledCount := 0
	for _, t := range a.plan.Targets {
		if t.Enabled {
			enabledCount++
		}
	}

	ui.Row(c).FillWidth().Height(48).Padding(0, 14).Gap(10).AlignItems(ui.Center).
		Background(BgMid).BorderWidth(0, 0, 1, 0).BorderColor(BorderSub).Children(func() {
		ui.Text(c, "绝地潜兵 2 · 自定义星球变种指挥台").FontSize(15).Bold().TextColor(HD2Gold)

		ui.Box(c).Padding(2, 8).Radius(999).Background(BlueAccent.Alpha(0.2)).Border(1, BlueAccent.Alpha(0.6)).Children(func() {
			ui.Text(c, "MYGO GPU").FontSize(10).Bold().TextColor(ui.Hex("#93C5FD"))
		})

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "星球 %d", len(a.galaxy.Planets)).FontSize(11.5).TextColor(TextDim)
			ui.Textf(c, "超地 %d", counts[catalog.FactionHumans]).FontSize(11.5).TextColor(ColorHumans)
			ui.Textf(c, "虫族 %d", counts[catalog.FactionTerminids]).FontSize(11.5).TextColor(ColorTerminids)
			ui.Textf(c, "机器人 %d", counts[catalog.FactionAutomatons]).FontSize(11.5).TextColor(ColorAutomatons)
			ui.Textf(c, "光能 %d", counts[catalog.FactionIlluminate]).FontSize(11.5).TextColor(ColorIlluminate)
			ui.Textf(c, "已配置 %d", enabledCount).FontSize(11.5).Bold().TextColor(CyanActive)
		})

		ui.Spacer(c)

		ui.Text(c, a.galaxy.LiveSourceNote).FontSize(11).TextColor(TextMute)

		refreshLbl := "刷新实时战况"
		if a.refreshing {
			refreshLbl = "刷新中..."
		}
		if ui.Button(c.Key("hdr-refresh"), refreshLbl).Disabled(a.refreshing).Clicked() {
			a.triggerLiveRefresh()
		}

		proxyBtnLbl := "● 代理未启动"
		if st.Running {
			proxyBtnLbl = fmt.Sprintf("● 代理 :%d (改写 %d)", st.Port, st.Rewrites)
		}
		pxBtn := ui.Button(c.Key("hdr-proxy"), proxyBtnLbl)
		if st.Running {
			pxBtn.TextColor(GreenOK)
		}
		if pxBtn.Clicked() {
			a.rightTab = 1
		}

		if ui.PrimaryButton(c.Key("hdr-dispatch"), "下发配置").Clicked() {
			if len(a.selectedIDs) > 0 {
				if err := a.applyDraftToSelection(c); err != nil {
					c.Toast(err.Error())
				} else {
					c.Toast("已保存选中星球并热重载下发")
				}
			} else {
				if err := a.saveAndDispatch(); err != nil {
					c.Toast(err.Error())
				} else {
					c.Toast("已保存全部方案并热重载下发")
				}
			}
		}
	})
}

func (a *App) triggerLiveRefresh() {
	if a.refreshing {
		return
	}
	a.refreshing = true
	warID := a.galaxy.WarID
	upstream := ""
	if a.settings.UseUpstreamProxy {
		upstream = a.settings.UpstreamProxyURL
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		warInfoRaw, statusRaw, _, err := catalog.RefreshFromLiveAPI(ctx, warID, upstream)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			a.refreshing = false
			if err != nil {
				a.toastMsg = err.Error()
				return
			}
			if applyErr := a.galaxy.ApplyWarData(warInfoRaw, statusRaw, "官方实时战况 "+time.Now().Format("15:04:05")); applyErr != nil {
				a.toastMsg = applyErr.Error()
			} else {
				a.toastMsg = "已同步最新银河战况"
			}
		})
	}()
}

func (a *App) targetMap() map[int]config.PlanetTarget {
	m := make(map[int]config.PlanetTarget, len(a.plan.Targets))
	for _, t := range a.plan.Targets {
		m[t.Planet] = t
	}
	return m
}

func (a *App) anySelectedConfigured() bool {
	tm := a.targetMap()
	for _, id := range a.selectedIDs {
		if _, ok := tm[id]; ok {
			return true
		}
	}
	return false
}

func (a *App) syncDraftFromSelection() {
	if len(a.selectedIDs) == 0 {
		a.draft = config.DefaultTarget(0)
	} else {
		tm := a.targetMap()
		if existing, ok := tm[a.selectedIDs[0]]; ok {
			a.draft = existing
			a.draft.Effects = append([]int(nil), existing.Effects...)
			a.draft.DisabledNativeEffects = append([]int(nil), existing.DisabledNativeEffects...)
		} else {
			a.draft = config.DefaultTarget(a.selectedIDs[0])
		}
	}
	a.draftOwnerStr = findOptionByInt(ownerOptions, a.draft.Owner)
	a.draftTypeStr = findOptionByInt(campTypeOptions, a.draft.CampaignType)
	a.draftRaceStr = findOptionByInt(campRaceOptions, a.draft.CampaignRace)
	if a.draft.CampaignCount <= 0 {
		a.draft.CampaignCount = 20
	}
	a.draftCountF = float64(a.draft.CampaignCount)
	if a.draft.DefenseLevel <= 0 {
		a.draft.DefenseLevel = 20
	}
	a.draftDefLevelF = float64(a.draft.DefenseLevel)
	if a.draft.DefenseHours <= 0 {
		a.draft.DefenseHours = 48
	}
	a.draftDefHoursF = float64(a.draft.DefenseHours)
	if a.draft.DefenseCount <= 0 {
		a.draft.DefenseCount = 38
	}
	a.draftDefCountF = float64(a.draft.DefenseCount)
}

func (a *App) toggleDraftEffect(eid int) {
	idx := slices.Index(a.draft.Effects, eid)
	if idx >= 0 {
		a.draft.Effects = slices.Delete(a.draft.Effects, idx, idx+1)
	} else {
		a.draft.Effects = append(a.draft.Effects, eid)
		// If it was marked disabled as native, unmark it so it is explicitly added.
		if di := slices.Index(a.draft.DisabledNativeEffects, eid); di >= 0 {
			a.draft.DisabledNativeEffects = slices.Delete(a.draft.DisabledNativeEffects, di, di+1)
		}
	}
}

func (a *App) toggleDisableNativeEffect(eid int) {
	idx := slices.Index(a.draft.DisabledNativeEffects, eid)
	if idx >= 0 {
		a.draft.DisabledNativeEffects = slices.Delete(a.draft.DisabledNativeEffects, idx, idx+1)
	} else {
		a.draft.DisabledNativeEffects = append(a.draft.DisabledNativeEffects, eid)
		if ci := slices.Index(a.draft.Effects, eid); ci >= 0 {
			a.draft.Effects = slices.Delete(a.draft.Effects, ci, ci+1)
		}
	}
}

func (a *App) disableAllNativeEffects(ids []int) {
	for _, eid := range ids {
		if !slices.Contains(a.draft.DisabledNativeEffects, eid) {
			a.draft.DisabledNativeEffects = append(a.draft.DisabledNativeEffects, eid)
		}
		if ci := slices.Index(a.draft.Effects, eid); ci >= 0 {
			a.draft.Effects = slices.Delete(a.draft.Effects, ci, ci+1)
		}
	}
}

func (a *App) restoreAllNativeEffects() {
	a.draft.DisabledNativeEffects = nil
}

func (a *App) effectDisplayName(eid int) string {
	if eff, ok := a.galaxy.EffectsByID[eid]; ok && eff.Name != "" {
		return eff.Name
	}
	return fmt.Sprintf("未解效果 #%d", eid)
}

func (a *App) selectedNativeEffects() []int {
	seen := make(map[int]bool)
	var out []int
	for _, pid := range a.selectedIDs {
		if pl := a.galaxy.PlanetsByID[pid]; pl != nil {
			for _, eid := range pl.ActiveEffects {
				if !seen[eid] {
					seen[eid] = true
					out = append(out, eid)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

func (a *App) applyDraftToSelection(c *ui.Context) error {
	if len(a.selectedIDs) == 0 {
		return fmt.Errorf("请先选择至少一颗星球")
	}
	tm := a.targetMap()
	for _, pid := range a.selectedIDs {
		t := a.draft
		t.Planet = pid
		t.Effects = append([]int(nil), a.draft.Effects...)
		t.DisabledNativeEffects = append([]int(nil), a.draft.DisabledNativeEffects...)
		tm[pid] = t
	}
	nextTargets := make([]config.PlanetTarget, 0, len(tm))
	for _, t := range tm {
		nextTargets = append(nextTargets, t)
	}
	slices.SortFunc(nextTargets, func(x, y config.PlanetTarget) int {
		return x.Planet - y.Planet
	})
	candidate := config.Plan{Schema: 2, Targets: nextTargets}
	if _, err := config.NormalizeTargets(candidate); err != nil {
		return err
	}
	a.plan = candidate
	return a.saveAndDispatch()
}

func (a *App) removeSelectionTargets() {
	if len(a.selectedIDs) == 0 {
		return
	}
	sel := make(map[int]bool, len(a.selectedIDs))
	for _, id := range a.selectedIDs {
		sel[id] = true
	}
	kept := make([]config.PlanetTarget, 0, len(a.plan.Targets))
	for _, t := range a.plan.Targets {
		if !sel[t.Planet] {
			kept = append(kept, t)
		}
	}
	a.plan.Targets = kept
	a.syncDraftFromSelection()
	_ = a.saveAndDispatch()
}

func (a *App) setTargetEnabled(pid int, enabled bool) {
	for i := range a.plan.Targets {
		if a.plan.Targets[i].Planet == pid {
			a.plan.Targets[i].Enabled = enabled
			break
		}
	}
	_ = a.saveAndDispatch()
}

func (a *App) deleteTarget(pid int) {
	kept := make([]config.PlanetTarget, 0, len(a.plan.Targets))
	for _, t := range a.plan.Targets {
		if t.Planet != pid {
			kept = append(kept, t)
		}
	}
	a.plan.Targets = kept
	_ = a.saveAndDispatch()
}

func (a *App) saveAndDispatch() error {
	if _, err := config.NormalizeTargets(a.plan); err != nil {
		return err
	}
	if err := config.SavePlan(a.profileDir, a.plan); err != nil {
		return err
	}
	a.proxySrv.UpdatePlan(a.plan)
	return nil
}

func (a *App) importPlanBytes(raw []byte) error {
	var p config.Plan
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var list []config.PlanetTarget
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		p = config.Plan{Schema: 2, Targets: list}
	} else {
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.Schema == 0 {
			p.Schema = 2
		}
	}
	if _, err := config.NormalizeTargets(p); err != nil {
		return err
	}
	a.plan = p
	a.syncDraftFromSelection()
	return a.saveAndDispatch()
}

func (a *App) exportPlanFile(path string) error {
	b, err := json.MarshalIndent(a.plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a *App) startProxy() error {
	port, err := a.proxySrv.Start()
	if err != nil {
		return err
	}
	if err := config.WriteSteamProxyINI(a.profileDir, port, os.Getpid()); err != nil {
		a.proxySrv.Stop()
		return err
	}
	return nil
}

func (a *App) stopProxy() {
	config.ClearSteamProxyINI(a.profileDir)
	a.proxySrv.Stop()
}

func (a *App) filteredPlanets() []*catalog.Planet {
	q := strings.TrimSpace(strings.ToLower(a.planetQuery))
	tm := a.targetMap()
	out := make([]*catalog.Planet, 0, len(a.galaxy.Planets))
	for _, pl := range a.galaxy.Planets {
		switch a.planetFilter {
		case 1:
			if pl.Owner != catalog.FactionHumans {
				continue
			}
		case 2:
			if pl.Owner != catalog.FactionTerminids {
				continue
			}
		case 3:
			if pl.Owner != catalog.FactionAutomatons {
				continue
			}
		case 4:
			if pl.Owner != catalog.FactionIlluminate {
				continue
			}
		case 5:
			if _, ok := tm[pl.ID]; !ok {
				continue
			}
		case 6:
			if !pl.HasCampaign {
				continue
			}
		}
		if q != "" {
			idStr := strconv.Itoa(pl.ID)
			if !strings.Contains(strings.ToLower(pl.NameZH), q) &&
				!strings.Contains(strings.ToLower(pl.NameEN), q) &&
				!strings.Contains(strings.ToLower(pl.SectorZH), q) &&
				!strings.Contains(idStr, q) {
				continue
			}
		}
		out = append(out, pl)
	}
	return out
}

func (a *App) filteredEffects() []catalog.Effect {
	q := strings.TrimSpace(strings.ToLower(a.effectQuery))
	sel := make(map[int]bool, len(a.draft.Effects))
	for _, eid := range a.draft.Effects {
		sel[eid] = true
	}

	out := make([]catalog.Effect, 0, len(a.galaxy.Effects))
	for _, eff := range a.galaxy.Effects {
		switch a.effectGroup {
		case "全部":
		case "仅已选":
			if !sel[eff.ID] {
				continue
			}
		default:
			if eff.GroupLabel != a.effectGroup {
				continue
			}
		}
		if q != "" {
			idStr := strconv.Itoa(eff.ID)
			matched := strings.Contains(strings.ToLower(eff.Name), q) ||
				strings.Contains(strings.ToLower(eff.EnglishName), q) ||
				strings.Contains(strings.ToLower(eff.Description), q) ||
				strings.Contains(strings.ToLower(eff.Native), q) ||
				strings.Contains(idStr, q)
			if !matched {
				for _, al := range eff.Aliases {
					if strings.Contains(strings.ToLower(al), q) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, eff)
	}
	return out
}
