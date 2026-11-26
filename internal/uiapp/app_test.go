package uiapp

import (
	"math"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"hd2-strain-lab/internal/config"
)

func TestUIViewSelectPlanetAndDispatch(t *testing.T) {
	t.Setenv("HD2_MY_CPV_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)
	if !tt.HasText("HD2 Strain Lab · 星球变种工坊") {
		t.Fatalf("expected header title, got texts: %v", tt.Texts())
	}

	// Click planet #1 (克伦达斯II) in the left planet navigator
	if err := tt.Click("克伦达斯II"); err != nil {
		t.Fatalf("click planet #1 failed: %v", err)
	}
	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 1 {
		t.Fatalf("expected selected planet 1, got %v", app.selectedIDs)
	}

	// Toggle an effect in the effect picker
	if len(app.galaxy.Effects) == 0 {
		t.Fatalf("expected non-empty effect catalog")
	}
	firstEff := app.galaxy.Effects[0]
	if err := tt.Click(firstEff.Name); err != nil {
		t.Fatalf("click effect %q failed: %v", firstEff.Name, err)
	}
	if len(app.draft.Effects) != 1 || app.draft.Effects[0] != firstEff.ID {
		t.Fatalf("expected draft effect %d, got %v", firstEff.ID, app.draft.Effects)
	}

	// Dispatch config
	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("click dispatch failed: %v", err)
	}
	if len(app.plan.Targets) != 1 || app.plan.Targets[0].Planet != 1 {
		t.Fatalf("expected saved target for planet 1, got %+v", app.plan.Targets)
	}

	// Switch to Proxy tab and start/stop proxy
	if err := tt.Click("代理下发与连接"); err != nil {
		t.Fatalf("switch to proxy tab failed: %v", err)
	}
	if err := tt.Click("启动代理服务"); err != nil {
		t.Fatalf("start proxy failed: %v", err)
	}
	if !app.proxySrv.Stats().Running {
		t.Fatalf("expected proxy server running")
	}
	if err := tt.Click("停止代理"); err != nil {
		t.Fatalf("stop proxy failed: %v", err)
	}
	if app.proxySrv.Stats().Running {
		t.Fatalf("expected proxy server stopped")
	}
}

func TestUIViewStarMapInteraction(t *testing.T) {
	t.Setenv("HD2_MY_CPV_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)

	// Verify Super Earth (0) and Crimsica (78) projected coordinates are populated
	xy0, ok0 := app.starMap.screenXY[0]
	xy78, ok78 := app.starMap.screenXY[78]
	if !ok0 || !ok78 {
		t.Fatalf("expected projected planet coordinates for 0 and 78")
	}

	// Click directly on Super Earth on the star map canvas
	tt.ClickAt(xy0[0], xy0[1])
	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 0 {
		t.Fatalf("expected clicking map at (%.1f,%.1f) to select planet 0, got %v", xy0[0], xy0[1], app.selectedIDs)
	}

	// Toggle 2D flat mode via map toolbar button
	if err := tt.Click("2.5D 倾角"); err != nil {
		t.Fatalf("toggle 2D mode failed: %v", err)
	}
	if app.starMap.Tilt3D {
		t.Fatalf("expected Tilt3D=false after toggle")
	}

	// Verify mouse wheel zooms the star map
	prevZoom := app.starMap.Zoom
	tt.Wheel(xy78[0], xy78[1], 0, -120)
	if app.starMap.Zoom <= prevZoom {
		t.Fatalf("expected zoom to increase on wheel up, prev=%.2f now=%.2f", prevZoom, app.starMap.Zoom)
	}
}

func TestStarMapPerspectiveBottomLargerThanTop(t *testing.T) {
	m := NewStarMapState()
	m.Tilt3D = true
	r := ui.Rect{X: 0, Y: 0, W: 800, H: 700}

	// Bottom (y = -0.5) must be larger than Top (y = +0.5)
	bx1, _, dsBot := m.project(-0.3, -0.5, r)
	bx2, _, _ := m.project(0.3, -0.5, r)
	tx1, _, dsTop := m.project(-0.3, 0.5, r)
	tx2, _, _ := m.project(0.3, 0.5, r)

	if dsBot <= dsTop {
		t.Fatalf("expected bottom depthScale (%.4f) > top depthScale (%.4f)", dsBot, dsTop)
	}
	if (bx2 - bx1) <= (tx2 - tx1) {
		t.Fatalf("expected bottom width (%.2f) > top width (%.2f)", bx2-bx1, tx2-tx1)
	}

	// Round-trip project -> unproject
	sx, sy, _ := m.project(0.35, -0.42, r)
	wx, wy := m.unproject(sx, sy, r)
	if math.Abs(wx-0.35) > 1e-4 || math.Abs(wy-(-0.42)) > 1e-4 {
		t.Fatalf("unproject mismatch: got (%.5f, %.5f), want (0.35, -0.42)", wx, wy)
	}
}

func TestUIViewTuringNativeAndCustomEffects(t *testing.T) {
	t.Setenv("HD2_MY_CPV_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)

	// Click Turing (#126) directly on the star map canvas
	xy126, ok126 := app.starMap.screenXY[126]
	if !ok126 {
		t.Fatalf("expected projected coordinates for Turing (#126)")
	}
	tt.ClickAt(xy126[0], xy126[1])

	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 126 {
		t.Fatalf("expected planet 126 selected, got %v", app.selectedIDs)
	}

	// Should display Turing's native effects (3 planet-local + 2 active galaxy-wide in status_sample.json)
	if !tt.HasText("原版自带效果 (5)") {
		t.Fatalf("expected Turing native effects count 5 in UI, texts: %v", tt.Texts())
	}

	// Cancel all native effects
	if err := tt.Click("全部取消"); err != nil {
		t.Fatalf("click cancel all native failed: %v", err)
	}
	if len(app.draft.DisabledNativeEffects) != 5 {
		t.Fatalf("expected 5 disabled native effects, got %v", app.draft.DisabledNativeEffects)
	}

	// Add a custom effect and verify it appears in "已添加自定义效果 (1)"
	firstEff := app.galaxy.Effects[0]
	if err := tt.Click(firstEff.Name); err != nil {
		t.Fatalf("click effect failed: %v", err)
	}
	if !tt.HasText("已添加自定义效果 (1)") {
		t.Fatalf("expected added custom effects count 1")
	}

	// Dispatch and verify saved plan
	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if len(app.plan.Targets) != 1 || len(app.plan.Targets[0].DisabledNativeEffects) != 5 || len(app.plan.Targets[0].Effects) != 1 {
		t.Fatalf("unexpected saved target for Turing: %+v", app.plan.Targets)
	}
}

func TestUIViewDeleteConfiguredTargetsNoPanic(t *testing.T) {
	t.Setenv("HD2_MY_CPV_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)

	// Configure two planets (#1 and #126)
	if err := tt.Click("克伦达斯II"); err != nil {
		t.Fatalf("click planet #1 failed: %v", err)
	}
	firstEff := app.galaxy.Effects[0]
	if err := tt.Click(firstEff.Name); err != nil {
		t.Fatalf("click effect failed: %v", err)
	}
	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("save planet #1 failed: %v", err)
	}

	xy126, ok126 := app.starMap.screenXY[126]
	if !ok126 {
		t.Fatalf("expected projected coordinates for planet #126")
	}
	tt.ClickAt(xy126[0], xy126[1])
	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 126 {
		t.Fatalf("expected planet 126 selected, got %v", app.selectedIDs)
	}
	if err := tt.Click(firstEff.Name); err != nil {
		t.Fatalf("click effect for #126 failed: %v", err)
	}
	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("save planet #126 failed: %v", err)
	}
	if len(app.plan.Targets) != 2 {
		t.Fatalf("expected 2 configured targets, got %d", len(app.plan.Targets))
	}

	// Switch to configured targets list view and delete the first target (index 0 of 2), then the remaining target.
	if err := tt.Click("查看全部已配置"); err != nil {
		t.Fatalf("click view all configured failed: %v", err)
	}
	if !tt.HasText("已配置星球方案 (2)") {
		t.Fatalf("expected configured targets panel (2)")
	}
	if err := tt.Click("删"); err != nil {
		t.Fatalf("click delete first target failed: %v", err)
	}
	if len(app.plan.Targets) != 1 {
		t.Fatalf("expected 1 target left after first delete, got %d", len(app.plan.Targets))
	}
	if err := tt.Click("删"); err != nil {
		t.Fatalf("click delete last target failed: %v", err)
	}
	if len(app.plan.Targets) != 0 {
		t.Fatalf("expected 0 targets left after deleting last, got %d", len(app.plan.Targets))
	}
}

func TestUIViewCopyPlanetEffectsToAnotherPlanet(t *testing.T) {
	t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)

	// Select Turing (#126) which has native effects in offline snapshot
	xy126, ok126 := app.starMap.screenXY[126]
	if !ok126 {
		t.Fatalf("expected projected coordinates for Turing (#126)")
	}
	tt.ClickAt(xy126[0], xy126[1])
	native := app.selectedNativeEffects()
	if len(native) < 2 {
		t.Fatalf("expected >= 2 native effects on Turing, got %v", native)
	}

	// Cancel the first native effect and add one custom effect
	app.toggleDisableNativeEffect(native[0])
	firstEff := app.galaxy.Effects[0]
	if err := tt.Click(firstEff.Name); err != nil {
		t.Fatalf("click custom effect failed: %v", err)
	}
	destNative := app.galaxy.PlanetsByID[1].ActiveEffects
	expectedCount := 1 // firstEff
	for _, eid := range native[1:] {
		if !slices.Contains(destNative, eid) {
			expectedCount++
		}
	}

	// Click "拷贝效果到..." then select destination planet #1 (克伦达斯II)
	if err := tt.Click("拷贝效果到..."); err != nil {
		t.Fatalf("click copy effects button failed: %v", err)
	}
	if !app.starMap.PickingCopyTarget {
		t.Fatalf("expected PickingCopyTarget true")
	}
	if !tt.HasText("请点击目标星球") {
		t.Fatalf("expected copy banner on star map")
	}

	if err := tt.Click("克伦达斯II"); err != nil {
		t.Fatalf("click destination planet #1 failed: %v", err)
	}
	if app.starMap.PickingCopyTarget {
		t.Fatalf("expected PickingCopyTarget cleared after selecting destination")
	}
	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 1 {
		t.Fatalf("expected selection moved to destination planet 1, got %v", app.selectedIDs)
	}
	if len(app.plan.Targets) != 1 || app.plan.Targets[0].Planet != 1 {
		t.Fatalf("expected dispatched target for planet 1, got %+v", app.plan.Targets)
	}
	if got := len(app.plan.Targets[0].Effects); got != expectedCount {
		t.Fatalf("expected %d copied effects on planet 1, got %v", expectedCount, app.plan.Targets[0].Effects)
	}
}

func TestUIViewFontScaleSettingAndPersistence(t *testing.T) {
	prof := t.TempDir()
	t.Setenv("HD2_STRAIN_LAB_PROFILE", prof)

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)
	if math.Abs(app.settings.FontScale-1.0) > 1e-6 || app.theme.FontSize != 13 {
		t.Fatalf("expected default font scale 1.0 (13pt), got scale=%v size=%v", app.settings.FontScale, app.theme.FontSize)
	}

	// Step up via header A+ button
	if err := tt.Click("A+"); err != nil {
		t.Fatalf("click header A+ failed: %v", err)
	}
	if math.Abs(app.settings.FontScale-1.05) > 1e-6 {
		t.Fatalf("expected font scale 1.05 after A+, got %v", app.settings.FontScale)
	}

	// Switch to proxy/settings tab and pick preset "特大 130%"
	if err := tt.Click("代理下发与连接"); err != nil {
		t.Fatalf("switch to proxy tab failed: %v", err)
	}
	if err := tt.Click("特大 130%"); err != nil {
		t.Fatalf("click preset 130%% failed: %v", err)
	}
	if math.Abs(app.settings.FontScale-1.30) > 1e-6 || app.theme.FontSize != 17 {
		t.Fatalf("expected font scale 1.30 (17pt), got scale=%v size=%v", app.settings.FontScale, app.theme.FontSize)
	}

	// Verify persistence on disk
	loaded, err := config.LoadSettings(prof)
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if math.Abs(loaded.FontScale-1.30) > 1e-6 {
		t.Fatalf("expected persisted FontScale 1.30, got %v", loaded.FontScale)
	}

	// Step down via header A- then reset via percentage button
	if err := tt.Click("A-"); err != nil {
		t.Fatalf("click header A- failed: %v", err)
	}
	if math.Abs(app.settings.FontScale-1.25) > 1e-6 {
		t.Fatalf("expected font scale 1.25 after A-, got %v", app.settings.FontScale)
	}
	if err := tt.Click("125%"); err != nil {
		t.Fatalf("click reset percentage button failed: %v", err)
	}
	if math.Abs(app.settings.FontScale-1.0) > 1e-6 {
		t.Fatalf("expected font scale reset to 1.0, got %v", app.settings.FontScale)
	}
}

func TestUIViewCyberstanAutoRegionSetup(t *testing.T) {
	t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1380, 860)

	// Select Cyberstan (#260) on the star map
	xy260, ok260 := app.starMap.screenXY[260]
	if !ok260 {
		t.Fatalf("expected projected coordinates for Cyberstan (#260)")
	}
	tt.ClickAt(xy260[0], xy260[1])
	if len(app.selectedIDs) != 1 || app.selectedIDs[0] != 260 {
		t.Fatalf("expected Cyberstan (#260) selected, got %v", app.selectedIDs)
	}
	if !app.showCampaignCfg {
		t.Fatalf("expected campaign config panel auto-expanded for Cyberstan (#260)")
	}

	// Clicking "开启战役" on Cyberstan (#260) should automatically enable ForceOpen, OpenRegions, InitializeRegions and infer Race=3 (Automatons)
	if err := tt.Click("开启战役"); err != nil {
		t.Fatalf("click 开启战役 failed: %v", err)
	}
	if !app.draft.Campaign || !app.draft.ForceOpen || !app.draft.OpenRegions || !app.draft.InitializeRegions || app.draft.CampaignRace != 3 {
		t.Fatalf("expected auto-linked Cyberstan campaign flags (Campaign/ForceOpen/OpenRegions/InitializeRegions/Race=3), got %+v", app.draft)
	}

	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("click dispatch failed: %v", err)
	}
	if len(app.plan.Targets) != 1 || app.plan.Targets[0].Planet != 260 || !app.plan.Targets[0].InitializeRegions {
		t.Fatalf("expected saved Cyberstan target with InitializeRegions=true, got %+v", app.plan.Targets)
	}
}

func TestUIViewEditedPlanetEffectPickerRemainsVisible(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		fontScale float64
		numEffs   int
		defense   bool
	}{
		{"standard_many_effects_defense", 1380, 860, 1.0, 12, true},
		{"1080p_125dpi_edited", 1280, 760, 1.0, 6, false},
		{"1080p_150dpi_min_window", 1080, 680, 1.0, 4, false},
		{"large_font_130pct", 1380, 860, 1.30, 6, false},
		{"1080p_150dpi_font_125pct", 1080, 680, 1.25, 5, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())
			app, err := NewApp()
			if err != nil {
				t.Fatalf("NewApp failed: %v", err)
			}
			defer app.stopProxy()
			app.setFontScale(tc.fontScale)

			var effs []int
			for i := 0; i < tc.numEffs && i < len(app.galaxy.Effects); i++ {
				effs = append(effs, app.galaxy.Effects[i].ID)
			}
			tgt := config.DefaultTarget(126) // Turing (#126) also has native effects
			tgt.Effects = effs
			tgt.Campaign = true
			tgt.ForceOpen = true
			if tc.defense {
				tgt.CampaignType = 4
				tgt.Owner = 1
				tgt.CampaignRace = 2
				tgt.DefenseSource = 1
			}
			app.plan.Targets = []config.PlanetTarget{tgt}

			tt := ui.NewTester(app.View, tc.w, tc.h)
			app.handleMapSelect(126, 0)
			tt.Frame()

			firstEff := app.galaxy.Effects[0]
			rEff, okEff := tt.Find(firstEff.Name)
			if !okEff || rEff.W <= 0 || rEff.H <= 0 {
				t.Fatalf("expected effect row %q visible in picker, got ok=%v rect=%+v", firstEff.Name, okEff, rEff)
			}
			rFooter, okFooter := tt.Find("查看全部已配置")
			if !okFooter {
				t.Fatalf("expected footer button visible")
			}
			if rEff.Y+rEff.H > float32(tc.h) {
				t.Fatalf("effect row (%+v) overflows window height (%d)", rEff, tc.h)
			}
			if rEff.X+rEff.W >= rFooter.X {
				t.Fatalf("expected effect row (%+v) in left sidebar before right footer (%+v)", rEff, rFooter)
			}
			// Verify clicking an effect row in the picker actually works
			if err := tt.Click(firstEff.Name); err != nil {
				t.Fatalf("clicking effect %q in picker failed: %v", firstEff.Name, err)
			}
			if len(app.draft.Effects) != tc.numEffs-1 {
				t.Fatalf("expected toggling effect to update draft effects to %d, got %d", tc.numEffs-1, len(app.draft.Effects))
			}
		})
	}

	// Verify selecting a variants-only configured planet auto-collapses campaign config
	t.Run("variants_only_target_collapses_campaign_cfg", func(t *testing.T) {
		t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())
		app, err := NewApp()
		if err != nil {
			t.Fatalf("NewApp failed: %v", err)
		}
		defer app.stopProxy()

		tgt := config.DefaultTarget(78)
		tgt.Effects = []int{app.galaxy.Effects[0].ID}
		app.plan.Targets = []config.PlanetTarget{tgt}

		_ = ui.NewTester(app.View, 1380, 860)
		app.handleMapSelect(1, 0) // unconfigured inactive planet -> expands
		if !app.showCampaignCfg {
			t.Fatalf("expected showCampaignCfg=true for unconfigured inactive planet")
		}
		app.handleMapSelect(78, 0) // configured variants-only planet -> collapses
		if app.showCampaignCfg {
			t.Fatalf("expected showCampaignCfg=false for variants-only configured planet")
		}
	})
}

func TestUIViewGlobalEffectDisplayAndCancel(t *testing.T) {
	prof := t.TempDir()
	t.Setenv("HD2_STRAIN_LAB_PROFILE", prof)

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	liveStatus := []byte(`{
		"warId": 801,
		"time": 87746480,
		"planetStatus": [
			{"index": 0, "owner": 1, "health": 1000000, "players": 100, "position": {"x": 0, "y": 0}},
			{"index": 78, "owner": 2, "health": 500000, "players": 50, "position": {"x": 0.4, "y": 0.2}},
			{"index": 126, "owner": 1, "health": 1000000, "players": 20, "position": {"x": 0.3, "y": 0.3}}
		],
		"planetActiveEffects": [],
		"globalEvents": [
			{"eventId": 1501121, "effectIds": [1194], "planetIndices": [], "expireTime": 87885416}
		]
	}`)
	if err := app.galaxy.ApplyStatusBytes(liveStatus, "live"); err != nil {
		t.Fatalf("ApplyStatusBytes failed: %v", err)
	}

	tt := ui.NewTester(app.View, 1380, 860)

	// 1. Unselected view shows "官方全星系效果 (1)" and #1194
	if !tt.HasText("官方全星系效果 (1)") {
		t.Fatalf("expected global effects card header in unselected view, texts: %v", tt.Texts())
	}
	if !tt.HasText("临时配发：轨道凝固汽油弹火力网 #1194") {
		t.Fatalf("expected effect #1194 chip in global effects card, texts: %v", tt.Texts())
	}

	// Click to globally cancel #1194
	if err := tt.Click("临时配发：轨道凝固汽油弹火力网 #1194"); err != nil {
		t.Fatalf("click global effect #1194 failed: %v", err)
	}
	if !slices.Equal(app.plan.DisabledGlobalEffects, []int{1194}) {
		t.Fatalf("expected DisabledGlobalEffects [1194], got %v", app.plan.DisabledGlobalEffects)
	}

	// Verify persistence when configuring a planet via commitTargetMap
	app.handleMapSelect(78, 0)
	tt.Frame()
	if !tt.HasText("[已全局取消] [全星系] 临时配发：轨道凝固汽油弹火力网 #1194") {
		t.Fatalf("expected globally disabled badge on selected planet view, texts: %v", tt.Texts())
	}
	if err := tt.Click("保存并下发到代理"); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if !slices.Equal(app.plan.DisabledGlobalEffects, []int{1194}) {
		t.Fatalf("expected DisabledGlobalEffects [1194] preserved after commitTargetMap, got %v", app.plan.DisabledGlobalEffects)
	}

	loaded, err := config.LoadPlan(prof)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if !slices.Equal(loaded.DisabledGlobalEffects, []int{1194}) {
		t.Fatalf("expected persisted DisabledGlobalEffects [1194], got %v", loaded.DisabledGlobalEffects)
	}

	// Restore global effect via selected planet inspector button, then test per-planet cancel
	if err := tt.Click("恢复全局效果"); err != nil {
		t.Fatalf("click 恢复全局效果 failed: %v", err)
	}
	if len(app.plan.DisabledGlobalEffects) != 0 {
		t.Fatalf("expected DisabledGlobalEffects cleared, got %v", app.plan.DisabledGlobalEffects)
	}
	if !tt.HasText("[全星系] 临时配发：轨道凝固汽油弹火力网 #1194") {
		t.Fatalf("expected restored global effect chip on selected planet view, texts: %v", tt.Texts())
	}
	if err := tt.Click("[全星系] 临时配发：轨道凝固汽油弹火力网 #1194"); err != nil {
		t.Fatalf("click per-planet cancel failed: %v", err)
	}
	if !slices.Equal(app.draft.DisabledNativeEffects, []int{1194}) {
		t.Fatalf("expected per-planet DisabledNativeEffects [1194], got %v", app.draft.DisabledNativeEffects)
	}
}

func TestUIEffectFilterGroupsCoverAllCatalogGroups(t *testing.T) {
	t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	for _, eff := range app.galaxy.Effects {
		if !slices.Contains(effectFilterGroups, eff.GroupLabel) {
			t.Fatalf("effect #%d (%q) groupLabel %q is not reachable in UI effectFilterGroups", eff.ID, eff.Name, eff.GroupLabel)
		}
	}
}

func TestUIViewCollapsiblePlanetNavAndLeftEffectPicker(t *testing.T) {
	t.Setenv("HD2_STRAIN_LAB_PROFILE", t.TempDir())

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	defer app.stopProxy()

	tt := ui.NewTester(app.View, 1280, 760)
	if !app.showPlanetNav {
		t.Fatalf("expected showPlanetNav=true initially")
	}
	if !tt.HasText("SUPER EARTH") || !tt.HasText("克伦达斯II") || !tt.HasText("星系效果与变种（请先选中星球）") {
		t.Fatalf("expected planet nav and left effect picker visible initially, texts=%v", tt.Texts())
	}

	// Select planet #1 so draft editing is active
	if err := tt.Click("克伦达斯II"); err != nil {
		t.Fatalf("click planet #1 failed: %v", err)
	}
	firstEff := app.galaxy.Effects[0]
	rExpanded, okExp := tt.Find(firstEff.Name)
	if !okExp {
		t.Fatalf("expected effect row %q visible when nav expanded", firstEff.Name)
	}

	// Collapse planet nav -> planet list hidden, effect picker expands upward
	if err := tt.Click("收起"); err != nil {
		t.Fatalf("click 收起 failed: %v", err)
	}
	if app.showPlanetNav {
		t.Fatalf("expected showPlanetNav=false after 收起")
	}
	if tt.HasText("SUPER EARTH") || !tt.HasText("▶ 星球导航") {
		t.Fatalf("expected planet list hidden when collapsed")
	}
	rCollapsed, okCol := tt.Find(firstEff.Name)
	if !okCol {
		t.Fatalf("expected effect row %q visible when nav collapsed", firstEff.Name)
	}
	if rCollapsed.Y >= rExpanded.Y {
		t.Fatalf("expected effect picker to expand upward after collapsing nav: collapsedY=%.1f expandedY=%.1f", rCollapsed.Y, rExpanded.Y)
	}

	// Expand planet nav again
	if err := tt.Click("展开"); err != nil {
		t.Fatalf("click 展开 failed: %v", err)
	}
	if !app.showPlanetNav || !tt.HasText("SUPER EARTH") || !tt.HasText("▼ 星球导航") {
		t.Fatalf("expected planet nav restored after 展开")
	}

	// Verify dedicated 兵种激增 chip vs 敌情与变种 chip
	if err := tt.Click("兵种激增"); err != nil {
		t.Fatalf("click 兵种激增 failed: %v", err)
	}
	if !tt.HasText("移动工厂激增 (+50% 生成)") {
		t.Fatalf("expected surge effect in 兵种激增 group")
	}
	if err := tt.Click("敌情与变种"); err != nil {
		t.Fatalf("click 敌情与变种 failed: %v", err)
	}
	if !tt.HasText("掠食变种") {
		t.Fatalf("expected strain effect in 敌情与变种 group")
	}
	if tt.HasText("移动工厂激增 (+50% 生成)") {
		t.Fatalf("did not expect surge effect in 敌情与变种 group")
	}
}



