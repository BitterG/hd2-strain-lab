package uiapp

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
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

	// Should display Turing's native effects
	if !tt.HasText("原版自带效果 (3)") {
		t.Fatalf("expected Turing native effects count 3 in UI, texts: %v", tt.Texts())
	}

	// Cancel all native effects
	if err := tt.Click("全部取消"); err != nil {
		t.Fatalf("click cancel all native failed: %v", err)
	}
	if len(app.draft.DisabledNativeEffects) != 3 {
		t.Fatalf("expected 3 disabled native effects, got %v", app.draft.DisabledNativeEffects)
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
	if len(app.plan.Targets) != 1 || len(app.plan.Targets[0].DisabledNativeEffects) != 3 || len(app.plan.Targets[0].Effects) != 1 {
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
	expectedCount := (len(native) - 1) + 1

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

