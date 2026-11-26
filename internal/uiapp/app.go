package uiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"hd2-strain-lab/internal/catalog"
	"hd2-strain-lab/internal/certs"
	"hd2-strain-lab/internal/config"
	"hd2-strain-lab/internal/proxy"
)

type DockSide string

const (
	DockLeft  DockSide = "left"
	DockRight DockSide = "right"
)

type PanelID string

const (
	PanelPlanetNav    PanelID = "planet_nav"
	PanelEffectPicker PanelID = "effect_picker"
	PanelConsole      PanelID = "console"
)

type panelDragPayload struct {
	Panel PanelID
}

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

	// Docked layout state
	leftSidebarW       float32
	rightSidebarW      float32
	panelSides         map[PanelID]DockSide
	draggingPanel      PanelID
	frameDraggingPanel PanelID

	// Left navigator state
	showPlanetNav   bool
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

	copySourceID        int
	copyEffectIDs       []int
	valueOverrideInputs map[int]float64

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
	settings.FontScale = config.NormalizeFontScale(settings.FontScale)
	cm, err := certs.NewManager(profileDir)
	if err != nil {
		return nil, err
	}

	panelSides := make(map[PanelID]DockSide, 3)
	for k, v := range config.NormalizePanelSides(settings.PanelSides) {
		panelSides[PanelID(k)] = DockSide(v)
	}

	app := &App{
		theme:               NewHD2Theme(),
		profileDir:          profileDir,
		galaxy:              galaxy,
		plan:                plan,
		settings:            settings,
		certsMgr:            cm,
		starMap:             NewStarMapState(),
		leftSidebarW:        float32(config.NormalizeSidebarWidth(settings.LeftSidebarWidth, config.DefaultLeftSidebarWidth)),
		rightSidebarW:       float32(config.NormalizeSidebarWidth(settings.RightSidebarWidth, config.DefaultRightSidebarWidth)),
		panelSides:          panelSides,
		showPlanetNav:       true,
		effectGroup:         "全部",
		valueOverrideInputs: make(map[int]float64),
	}
	app.theme.FontSize = app.fs(13)

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
	app.proxySrv.SetOnLiveEffects(func(raw []byte) {
		cp := append([]byte(nil), raw...)
		if app.win != nil {
			app.win.Update(func() {
				_ = app.galaxy.ApplyEffectsBytes(cp)
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

func (a *App) fontScale() float32 {
	return float32(config.NormalizeFontScale(a.settings.FontScale))
}

func (a *App) fs(base float32) float32 {
	return float32(math.Round(float64(base)*float64(a.fontScale())*2.0) / 2.0)
}

func (a *App) panelScale(minScale, maxScale float32) float32 {
	s := a.fontScale()
	if s < minScale {
		return minScale
	}
	if s > maxScale {
		return maxScale
	}
	return s
}

func (a *App) setFontScale(next float64) {
	norm := config.NormalizeFontScale(next)
	a.settings.FontScale = norm
	a.theme.FontSize = a.fs(13)
	if a.starMap != nil {
		clear(a.starMap.labelSizes)
	}
	_ = config.SaveSettings(a.profileDir, a.settings)
	a.proxySrv.UpdateSettings(a.settings)
}

func (a *App) panelSide(id PanelID) DockSide {
	if a.panelSides != nil {
		if side, ok := a.panelSides[id]; ok && (side == DockLeft || side == DockRight) {
			return side
		}
	}
	if id == PanelConsole {
		return DockRight
	}
	return DockLeft
}

func (a *App) panelsOnSide(side DockSide) []PanelID {
	order := []PanelID{PanelPlanetNav, PanelConsole, PanelEffectPicker}
	out := make([]PanelID, 0, len(order))
	for _, id := range order {
		if a.panelSide(id) == side {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) dockPanel(id PanelID, side DockSide) {
	if a.panelSide(id) == side {
		return
	}
	if a.panelSides == nil {
		a.panelSides = map[PanelID]DockSide{}
	}
	a.panelSides[id] = side
	a.persistLayoutSettings()
	if side == DockLeft {
		a.toastMsg = "已吸附到左侧"
	} else {
		a.toastMsg = "已吸附到右侧"
	}
}

func (a *App) clampSidebarWidth(w, winW float32) float32 {
	if w < float32(config.MinSidebarWidth) {
		w = float32(config.MinSidebarWidth)
	}
	if w > float32(config.MaxSidebarWidth) {
		w = float32(config.MaxSidebarWidth)
	}
	if winW > 0 {
		maxAllowed := max(float32(config.MinSidebarWidth), winW-260)
		if w > maxAllowed {
			w = maxAllowed
		}
	}
	return float32(math.Round(float64(w)))
}

func (a *App) computeSidebarWidths(winW float32, showLeft, showRight bool) (leftW, rightW float32) {
	leftPanels := a.panelsOnSide(DockLeft)
	rightPanels := a.panelsOnSide(DockRight)
	if showLeft {
		if len(leftPanels) == 0 {
			leftW = 120
		} else {
			leftW = a.clampSidebarWidth(a.leftSidebarW, winW)
		}
	}
	if showRight {
		if len(rightPanels) == 0 {
			rightW = 120
		} else {
			rightW = a.clampSidebarWidth(a.rightSidebarW, winW)
		}
	}
	if showLeft && showRight && winW > 0 {
		handlesW := float32(0)
		if len(leftPanels) > 0 {
			handlesW += 6
		}
		if len(rightPanels) > 0 {
			handlesW += 6
		}
		avail := winW - 240 - handlesW
		if avail > 0 && leftW+rightW > avail {
			scale := avail / (leftW + rightW)
			leftW = float32(math.Floor(float64(leftW * scale)))
			rightW = float32(math.Floor(float64(rightW * scale)))
		}
	}
	return leftW, rightW
}

func (a *App) persistLayoutSettings() {
	a.settings.LeftSidebarWidth = config.NormalizeSidebarWidth(float64(a.leftSidebarW), config.DefaultLeftSidebarWidth)
	a.settings.RightSidebarWidth = config.NormalizeSidebarWidth(float64(a.rightSidebarW), config.DefaultRightSidebarWidth)
	rawSides := make(map[string]string, 3)
	for _, id := range []PanelID{PanelPlanetNav, PanelEffectPicker, PanelConsole} {
		rawSides[string(id)] = string(a.panelSide(id))
	}
	a.settings.PanelSides = config.NormalizePanelSides(rawSides)
	_ = config.SaveSettings(a.profileDir, a.settings)
}

func (a *App) resetLayoutSettings() {
	a.leftSidebarW = float32(config.DefaultLeftSidebarWidth)
	a.rightSidebarW = float32(config.DefaultRightSidebarWidth)
	a.panelSides = map[PanelID]DockSide{
		PanelPlanetNav:    DockLeft,
		PanelEffectPicker: DockLeft,
		PanelConsole:      DockRight,
	}
	a.showPlanetNav = true
	a.persistLayoutSettings()
}

func (a *App) View(c *ui.Context) {
	a.theme.FontSize = a.fs(13)
	c.SetTheme(a.theme)
	if a.toastMsg != "" {
		c.Toast(a.toastMsg)
		a.toastMsg = ""
	}

	winW, _ := c.Size()
	leftPanels := a.panelsOnSide(DockLeft)
	rightPanels := a.panelsOnSide(DockRight)
	showLeft := len(leftPanels) > 0 || a.draggingPanel != ""
	showRight := len(rightPanels) > 0 || a.draggingPanel != ""
	leftW, rightW := a.computeSidebarWidths(winW, showLeft, showRight)
	a.frameDraggingPanel = ""

	ui.Column(c).Fill().Background(BgDeep).Children(func() {
		a.buildHeader(c)
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			if showLeft {
				a.buildDockSidebar(c, DockLeft, leftW, leftPanels, winW)
			}
			if len(leftPanels) > 0 {
				a.buildResizeHandle(c, "resize-left", DockLeft, winW)
			}
			a.buildStarMap(c)
			if len(rightPanels) > 0 {
				a.buildResizeHandle(c, "resize-right", DockRight, winW)
			}
			if showRight {
				a.buildDockSidebar(c, DockRight, rightW, rightPanels, winW)
			}
		})
	})

	a.draggingPanel = a.frameDraggingPanel
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

	ui.Row(c).FillWidth().Height(48*a.panelScale(1.0, 1.25)).Padding(0, 14).Gap(10).AlignItems(ui.Center).
		Background(BgMid).BorderWidth(0, 0, 1, 0).BorderColor(BorderSub).Children(func() {
		ui.Text(c, "HD2 Strain Lab · 星球变种工坊").FontSize(a.fs(15)).Bold().TextColor(HD2Gold)

		ui.Box(c).Padding(2, 8).Radius(999).Background(BlueAccent.Alpha(0.2)).Border(1, BlueAccent.Alpha(0.6)).Children(func() {
			ui.Text(c, "MYGO GPU").FontSize(a.fs(10)).Bold().TextColor(ui.Hex("#93C5FD"))
		})

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "星球 %d", len(a.galaxy.Planets)).FontSize(a.fs(11.5)).TextColor(TextDim)
			ui.Textf(c, "超地 %d", counts[catalog.FactionHumans]).FontSize(a.fs(11.5)).TextColor(ColorHumans)
			ui.Textf(c, "虫族 %d", counts[catalog.FactionTerminids]).FontSize(a.fs(11.5)).TextColor(ColorTerminids)
			ui.Textf(c, "机器人 %d", counts[catalog.FactionAutomatons]).FontSize(a.fs(11.5)).TextColor(ColorAutomatons)
			ui.Textf(c, "光能 %d", counts[catalog.FactionIlluminate]).FontSize(a.fs(11.5)).TextColor(ColorIlluminate)
			ui.Textf(c, "已配置 %d", enabledCount).FontSize(a.fs(11.5)).Bold().TextColor(CyanActive)
		})

		ui.Spacer(c)

		ui.Text(c, a.galaxy.LiveSourceNote).FontSize(a.fs(11)).TextColor(TextMute)

		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("hdr-fs-down"), "A-").Disabled(a.settings.FontScale <= 0.801).Clicked() {
				a.setFontScale(float64(a.fontScale()) - 0.05)
			}
			pct := int(math.Round(float64(a.fontScale()) * 100))
			if ui.Button(c.Key("hdr-fs-rst"), fmt.Sprintf("%d%%", pct)).Clicked() {
				a.setFontScale(1.0)
			}
			if ui.Button(c.Key("hdr-fs-up"), "A+").Disabled(a.settings.FontScale >= 1.499).Clicked() {
				a.setFontScale(float64(a.fontScale()) + 0.05)
			}
		})

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
	upstream := a.settings.EffectiveUpstreamProxyURL()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		warInfoRaw, statusRaw, effectsRaw, err := catalog.RefreshFromLiveAPI(ctx, warID, upstream)
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
				if len(effectsRaw) > 0 {
					_ = a.galaxy.ApplyEffectsBytes(effectsRaw)
				}
				a.proxySrv.SeedWarData(a.galaxy.WarID, warInfoRaw, effectsRaw)
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
	isConfigured := false
	if len(a.selectedIDs) == 0 {
		a.draft = config.DefaultTarget(0)
	} else {
		tm := a.targetMap()
		if existing, ok := tm[a.selectedIDs[0]]; ok {
			isConfigured = true
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
	if len(a.selectedIDs) > 0 {
		pl := a.galaxy.PlanetsByID[a.selectedIDs[0]]
		if isConfigured {
			a.showCampaignCfg = a.draft.Campaign || a.draft.ForceOpen ||
				a.draft.OpenRegions || a.draft.InitializeRegions ||
				a.draft.Owner != 0 || a.draft.CampaignType != 0
		} else {
			a.showCampaignCfg = pl != nil && (!pl.HasCampaign || pl.HasNativeRegions)
		}
	}
}

func (a *App) selectedHasNativeRegions() bool {
	for _, pid := range a.selectedIDs {
		if pl := a.galaxy.PlanetsByID[pid]; pl != nil && pl.HasNativeRegions {
			return true
		}
	}
	return false
}

func (a *App) selectedInferredRace() int {
	for _, pid := range a.selectedIDs {
		if pl := a.galaxy.PlanetsByID[pid]; pl != nil {
			return pl.InferEnemyRace()
		}
	}
	return catalog.FactionTerminids
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

func (a *App) isGlobalEffectDisabled(eid int) bool {
	return slices.Contains(a.plan.DisabledGlobalEffects, eid)
}

func (a *App) toggleDisableGlobalEffect(eid int) error {
	if idx := slices.Index(a.plan.DisabledGlobalEffects, eid); idx >= 0 {
		a.plan.DisabledGlobalEffects = slices.Delete(a.plan.DisabledGlobalEffects, idx, idx+1)
	} else {
		a.plan.DisabledGlobalEffects = append(a.plan.DisabledGlobalEffects, eid)
	}
	slices.Sort(a.plan.DisabledGlobalEffects)
	return a.saveAndDispatch()
}

func (a *App) disableAllGlobalEffects(ids []int) error {
	for _, eid := range ids {
		if !slices.Contains(a.plan.DisabledGlobalEffects, eid) {
			a.plan.DisabledGlobalEffects = append(a.plan.DisabledGlobalEffects, eid)
		}
	}
	slices.Sort(a.plan.DisabledGlobalEffects)
	return a.saveAndDispatch()
}

func (a *App) restoreAllGlobalEffects() error {
	a.plan.DisabledGlobalEffects = nil
	return a.saveAndDispatch()
}

func (a *App) setEffectValueOverride(eid, val int) error {
	if a.plan.EffectValueOverrides == nil {
		a.plan.EffectValueOverrides = make(map[int]int)
	}
	a.plan.EffectValueOverrides[eid] = val
	return a.saveAndDispatch()
}

func (a *App) clearEffectValueOverride(eid int) error {
	if a.plan.EffectValueOverrides != nil {
		delete(a.plan.EffectValueOverrides, eid)
		if len(a.plan.EffectValueOverrides) == 0 {
			a.plan.EffectValueOverrides = nil
		}
	}
	if a.valueOverrideInputs != nil {
		delete(a.valueOverrideInputs, eid)
	}
	return a.saveAndDispatch()
}

func (a *App) clearAllEffectValueOverrides() error {
	a.plan.EffectValueOverrides = nil
	if a.valueOverrideInputs != nil {
		clear(a.valueOverrideInputs)
	}
	return a.saveAndDispatch()
}

func (a *App) editableNumericEffectsForSelection() []int {
	seen := make(map[int]bool)
	var out []int
	add := func(eid int) {
		if seen[eid] {
			return
		}
		if _, ok := a.galaxy.EffectValueSpec(eid); ok {
			seen[eid] = true
			out = append(out, eid)
		}
	}
	for _, eid := range a.draft.Effects {
		add(eid)
	}
	for _, eid := range a.selectedNativeEffects() {
		add(eid)
	}
	slices.Sort(out)
	return out
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

func (a *App) currentEffectiveEffects() []int {
	dis := make(map[int]bool, len(a.draft.DisabledNativeEffects)+len(a.plan.DisabledGlobalEffects))
	for _, eid := range a.draft.DisabledNativeEffects {
		dis[eid] = true
	}
	for _, eid := range a.plan.DisabledGlobalEffects {
		dis[eid] = true
	}
	seen := make(map[int]bool)
	var out []int
	for _, eid := range a.selectedNativeEffects() {
		if !dis[eid] && !seen[eid] {
			seen[eid] = true
			out = append(out, eid)
		}
	}
	for _, eid := range a.draft.Effects {
		if !seen[eid] {
			seen[eid] = true
			out = append(out, eid)
		}
	}
	slices.Sort(out)
	return out
}

func (a *App) startCopyEffects(c *ui.Context) {
	if a.starMap.PickingCopyTarget {
		a.starMap.PickingCopyTarget = false
		a.copyEffectIDs = nil
		return
	}
	if len(a.selectedIDs) == 0 {
		c.Toast("请先选中一颗源星球")
		return
	}
	effs := a.currentEffectiveEffects()
	if len(effs) == 0 {
		c.Toast("当前选中星球没有可拷贝的生效效果")
		return
	}
	a.starMap.PickingSource = false
	a.starMap.PickingCopyTarget = true
	a.copySourceID = a.selectedIDs[0]
	a.copyEffectIDs = effs
	c.Toast(fmt.Sprintf("请在星图或左侧列表点击目标星球，接收 %d 项效果", len(effs)))
}

func (a *App) applyCopyEffectsTo(pid int) error {
	a.starMap.PickingCopyTarget = false
	effs := append([]int(nil), a.copyEffectIDs...)
	a.copyEffectIDs = nil
	if len(effs) == 0 {
		return nil
	}
	if pid == a.copySourceID {
		a.toastMsg = "目标星球与源星球相同，已取消拷贝"
		return nil
	}

	tm := a.targetMap()
	tgt, ok := tm[pid]
	if !ok {
		tgt = config.DefaultTarget(pid)
	} else {
		tgt.Effects = append([]int(nil), tgt.Effects...)
		tgt.DisabledNativeEffects = append([]int(nil), tgt.DisabledNativeEffects...)
	}
	tgt.Enabled = true

	globalDis := make(map[int]bool, len(a.plan.DisabledGlobalEffects))
	for _, eid := range a.plan.DisabledGlobalEffects {
		globalDis[eid] = true
	}
	destNative := make(map[int]bool)
	if pl := a.galaxy.PlanetsByID[pid]; pl != nil {
		for _, eid := range pl.ActiveEffects {
			if !globalDis[eid] {
				destNative[eid] = true
			}
		}
	}
	copiedSet := make(map[int]bool, len(effs))
	nextCustom := make([]int, 0, len(effs))
	for _, eid := range effs {
		copiedSet[eid] = true
		if !destNative[eid] {
			nextCustom = append(nextCustom, eid)
		}
	}
	keptDisabled := tgt.DisabledNativeEffects[:0]
	for _, eid := range tgt.DisabledNativeEffects {
		if !copiedSet[eid] {
			keptDisabled = append(keptDisabled, eid)
		}
	}
	tgt.DisabledNativeEffects = keptDisabled
	tgt.Effects = nextCustom

	tm[pid] = tgt
	if err := a.commitTargetMap(tm); err != nil {
		a.toastMsg = "拷贝失败: " + err.Error()
		return err
	}
	a.selectedIDs = []int{pid}
	a.syncDraftFromSelection()

	destName := fmt.Sprintf("星球 #%d", pid)
	if pl := a.galaxy.PlanetsByID[pid]; pl != nil {
		destName = fmt.Sprintf("%s (#%d)", pl.DisplayName(), pid)
	}
	a.toastMsg = fmt.Sprintf("已将 %d 项效果拷贝并下发到 %s", len(effs), destName)
	return nil
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
	return a.commitTargetMap(tm)
}

func (a *App) commitTargetMap(tm map[int]config.PlanetTarget) error {
	nextTargets := make([]config.PlanetTarget, 0, len(tm))
	for _, t := range tm {
		nextTargets = append(nextTargets, t)
	}
	slices.SortFunc(nextTargets, func(x, y config.PlanetTarget) int {
		return x.Planet - y.Planet
	})
	var clonedOverrides map[int]int
	if len(a.plan.EffectValueOverrides) > 0 {
		clonedOverrides = make(map[int]int, len(a.plan.EffectValueOverrides))
		for k, v := range a.plan.EffectValueOverrides {
			clonedOverrides[k] = v
		}
	}
	candidate := config.Plan{
		Schema:                2,
		Targets:               nextTargets,
		DisabledGlobalEffects: append([]int(nil), a.plan.DisabledGlobalEffects...),
		EffectValueOverrides:  clonedOverrides,
	}
	if _, err := config.NormalizeTargets(candidate); err != nil {
		return err
	}
	if _, err := config.NormalizeDisabledGlobalEffects(candidate); err != nil {
		return err
	}
	if _, err := config.NormalizeEffectValueOverrides(candidate); err != nil {
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
	if _, err := config.NormalizeDisabledGlobalEffects(a.plan); err != nil {
		return err
	}
	if _, err := config.NormalizeEffectValueOverrides(a.plan); err != nil {
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
	if _, err := config.NormalizeDisabledGlobalEffects(p); err != nil {
		return err
	}
	if _, err := config.NormalizeEffectValueOverrides(p); err != nil {
		return err
	}
	a.plan = p
	if a.valueOverrideInputs != nil {
		clear(a.valueOverrideInputs)
	}
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
