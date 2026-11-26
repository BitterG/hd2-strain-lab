package catalog

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadEmbeddedAndSectors(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded failed: %v", err)
	}
	if len(g.Planets) < 250 {
		t.Fatalf("expected >= 250 listed planets, got %d", len(g.Planets))
	}
	if len(g.Edges) < 200 {
		t.Fatalf("expected >= 200 supply edges, got %d", len(g.Edges))
	}
	if len(g.Effects) < 300 {
		t.Fatalf("expected >= 300 effects, got %d", len(g.Effects))
	}
	if len(g.Sectors) < 40 {
		t.Fatalf("expected >= 40 sectors, got %d", len(g.Sectors))
	}
	ringsCount := 0
	for _, s := range g.Sectors {
		ringsCount += len(s.Rings)
	}
	if ringsCount < 40 {
		t.Fatalf("expected sector polar grid rings, got %d", ringsCount)
	}
	se := g.PlanetsByID[0]
	if se == nil || se.NameZH != "超级地球" {
		t.Fatalf("expected planet 0 to be Super Earth (超级地球), got %+v", se)
	}
}

func TestApplyWarDataAndDefenseProgress(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded failed: %v", err)
	}
	wi := []byte(`{"warId":801,"planetInfos":[{"index":168,"position":{"x":0.42,"y":0.34},"waypoints":[124],"maxHealth":1500000,"initialOwner":1}]}`)
	st := []byte(`{"warId":801,"time":86383570,"planetStatus":[{"index":168,"owner":1,"health":1500000,"players":25851,"position":{"x":0.42,"y":0.34}}],"planetAttacks":[{"source":124,"target":168}],"campaigns":[{"id":51768,"planetIndex":168,"type":4,"count":38,"race":2}],"planetEvents":[{"id":5741,"planetIndex":168,"eventType":1,"race":2,"health":950000,"maxHealth":1900000}],"planetActiveEffects":[]}`)
	if err := g.ApplyWarData(wi, st, "test"); err != nil {
		t.Fatalf("ApplyWarData failed: %v", err)
	}
	p := g.PlanetsByID[168]
	if p == nil || !p.IsDefense {
		t.Fatalf("expected planet 168 to be in defense")
	}
	if math.Abs(p.LiberationPct()-50.0) > 0.1 {
		t.Fatalf("expected defense progress 50%%, got %.2f%%", p.LiberationPct())
	}
}

func TestGlobalEventsMergedIntoPlanetActiveEffects(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	st := []byte(`{
		"warId": 801,
		"time": 86383570,
		"planetStatus": [{"index": 124, "owner": 2, "health": 1000000, "players": 100, "position": {"x": 0.32, "y": 0.41}}],
		"planetActiveEffects": [{"index": 124, "galacticEffectId": 1430}, {"index": 124, "galacticEffectId": 1431}],
		"globalEvents": [{"eventId": 1501111, "effectIds": [1400, 1401], "planetIndices": [124]}]
	}`)
	if err := g.ApplyStatusBytes(st, "test"); err != nil {
		t.Fatal(err)
	}
	p := g.PlanetsByID[124]
	if len(p.ActiveEffects) != 4 {
		t.Fatalf("expected 4 active effects [1400 1401 1430 1431], got %v", p.ActiveEffects)
	}
}

func TestGalaxyWideGlobalEventsMergedAndExpiredSkipped(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	st := []byte(`{
		"warId": 801,
		"time": 87746480,
		"planetStatus": [
			{"index": 0, "owner": 1, "health": 1000000, "players": 100, "position": {"x": 0, "y": 0}},
			{"index": 78, "owner": 2, "health": 500000, "players": 50, "position": {"x": 0.4, "y": 0.2}},
			{"index": 126, "owner": 1, "health": 1000000, "players": 20, "position": {"x": 0.3, "y": 0.3}}
		],
		"planetActiveEffects": [{"index": 126, "galacticEffectId": 1395}],
		"globalEvents": [
			{"eventId": 1501121, "effectIds": [1194], "planetIndices": [], "expireTime": 87885416},
			{"eventId": 1501120, "effectIds": [1393], "planetIndices": [], "expireTime": 86202504}
		]
	}`)
	if err := g.ApplyStatusBytes(st, "test"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.GlobalEffects, []int{1194}) {
		t.Fatalf("expected GlobalEffects [1194] (expired 1393 skipped), got %v", g.GlobalEffects)
	}
	if !g.IsGlobalEffect(1194) || g.IsGlobalEffect(1393) {
		t.Fatalf("IsGlobalEffect mismatch: 1194=%v 1393=%v", g.IsGlobalEffect(1194), g.IsGlobalEffect(1393))
	}
	for _, pid := range []int{0, 78, 126} {
		p := g.PlanetsByID[pid]
		if !slices.Contains(p.ActiveEffects, 1194) {
			t.Fatalf("expected planet #%d to include galaxy-wide effect 1194, got %v", pid, p.ActiveEffects)
		}
		if slices.Contains(p.ActiveEffects, 1393) {
			t.Fatalf("expected expired effect 1393 skipped on planet #%d, got %v", pid, p.ActiveEffects)
		}
	}
}

func TestSpaceStationsMergedIntoPlanetActiveEffects(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	st := []byte(`{
		"warId": 801,
		"time": 86387510,
		"planetStatus": [{"index": 168, "owner": 2, "health": 1000000, "players": 25000, "position": {"x": 0.42, "y": 0.34}}],
		"planetActiveEffects": [{"index": 168, "galacticEffectId": 1395}],
		"spaceStations": [{"id32": 749875195, "planetIndex": 168, "activeEffectIds": [1238, 1261, 1262, 1384, 1385], "flags": 1}]
	}`)
	if err := g.ApplyStatusBytes(st, "test"); err != nil {
		t.Fatal(err)
	}
	p := g.PlanetsByID[168]
	// Expect [1217 1238 1261 1262 1384 1385 1395]
	expected := []int{1217, 1238, 1261, 1262, 1384, 1385, 1395}
	if len(p.ActiveEffects) != len(expected) {
		t.Fatalf("expected active effects %v, got %v", expected, p.ActiveEffects)
	}
	for i, id := range expected {
		if p.ActiveEffects[i] != id {
			t.Fatalf("expected active effects %v, got %v", expected, p.ActiveEffects)
		}
	}
}

func TestWikiTranslationsPresent(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	checks := map[int]string{
		1177: "黑洞",
		1304: "阿尔法前哨站",
		1331: "唯一真旗",
		1349: "护卫犬",
		1351: "便携式地狱火炸弹",
		1390: "破门锤",
		1404: "利维坦封锁屏障",
		1406: "寂域侧边星图区域",
		1423: "漩涡重型坦克",
		1426: "飞鹰掠过",
		1427: "轨道彩纸秀",
		1428: "轨道炮攻击",
		1430: "超级地球武装部队重兵驻守",
		1431: "腐蚀变种",
		1432: "毒瘴变种",
		1433: "毒瘴变种",
	}
	for id, substr := range checks {
		eff, ok := g.EffectsByID[id]
		if !ok {
			t.Fatalf("missing effect #%d", id)
		}
		if !containsSubstring(eff.Name, substr) {
			t.Fatalf("effect #%d expected to contain %q, got %q", id, substr, eff.Name)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRefreshFromLiveAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	wi, st, _, err := RefreshFromLiveAPI(ctx, 801)
	if err != nil {
		t.Fatalf("RefreshFromLiveAPI failed: %v", err)
	}
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.ApplyWarData(wi, st, "live"); err != nil {
		t.Fatalf("ApplyWarData live failed: %v", err)
	}
	if len(g.Planets) < 250 {
		t.Fatalf("expected >= 250 planets after live sync, got %d", len(g.Planets))
	}
	t.Logf("Live WarID=%d Time=%.0f Planets=%d BattleRoutes=%d", g.WarID, g.StatusTime, len(g.Planets), len(g.BattleRoutes))
}

func TestCatalogCategoriesAndCorrectedLabels(t *testing.T) {
	g, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded failed: %v", err)
	}
	if len(g.Effects) != 315 {
		t.Fatalf("expected 315 effects, got %d", len(g.Effects))
	}

	allowedGroups := map[string]bool{
		"敌情与变种":     true,
		"兵种激增":      true,
		"临时配发战备":    true,
		"冷却与重装":     true,
		"作战增减益":     true,
		"弹药与补给":     true,
		"星球环境与通路":   true,
		"空间站与战术行动":  true,
		"设施与星球标记":   true,
		"配套星图词条":    true,
		"未解效果":      true,
	}
	expectedUnknown := map[int]bool{
		1: true, 13: true, 1126: true, 1131: true, 1132: true, 1136: true,
		1138: true, 1141: true, 1176: true, 1201: true, 1218: true, 1225: true,
		1270: true, 1271: true, 1327: true, 1336: true, 1341: true, 1368: true,
		1381: true, 1382: true, 1415: true, 1429: true,
	}

	actualUnknownCount := 0
	strainCount := 0
	surgeCount := 0
	for _, eff := range g.Effects {
		if !allowedGroups[eff.GroupLabel] {
			t.Fatalf("effect #%d has unexpected groupLabel %q", eff.ID, eff.GroupLabel)
		}
		switch eff.GroupLabel {
		case "未解效果":
			actualUnknownCount++
			if !expectedUnknown[eff.ID] {
				t.Fatalf("unexpected effect #%d (%q) in 未解效果", eff.ID, eff.Name)
			}
		case "敌情与变种":
			strainCount++
		case "兵种激增":
			surgeCount++
		}
		if eff.GroupLabel != "未解效果" && strings.HasPrefix(eff.Name, "未解效果 #") {
			t.Fatalf("effect #%d in %q still has unresolved name %q", eff.ID, eff.GroupLabel, eff.Name)
		}
	}
	if actualUnknownCount != len(expectedUnknown) {
		t.Fatalf("expected %d effects in 未解效果, got %d", len(expectedUnknown), actualUnknownCount)
	}
	if strainCount != 16 || surgeCount != 16 {
		t.Fatalf("expected 16 敌情与变种 and 16 兵种激增, got strain=%d surge=%d", strainCount, surgeCount)
	}

	checks := []struct {
		id    int
		group string
		name  string
	}{
		{1273, "冷却与重装", "+60秒 飞鹰重新武装时间"},
		{1370, "冷却与重装", "+15秒 飞鹰重新武装时间"},
		{1275, "冷却与重装", "轨道 冷却时间增加(+15秒)"},
		{1289, "冷却与重装", "轨道炮攻击冷却时间减少(-120秒)"},
		{1337, "冷却与重装", "轨道烟雾攻击冷却时间增加(+30秒)"},
		{1424, "冷却与重装", "TD-110 漩涡重型坦克冷却时间减少(-120秒)"},
		{1278, "弹药与补给", "弹药紧缺(-25% 弹匣)"},
		{1258, "作战增减益", "增援减少(-1)"},
		{1264, "弹药与补给", "治疗针减少(-1 治疗针)"},
		{1245, "敌情与变种", "掠食变种"},
		{1283, "兵种激增", "移动工厂激增 (+50% 生成)"},
		{1297, "兵种激增", "追猎虫激增 (+125% 生成)"},
		{1355, "兵种激增", "机器人重甲敌军激增 (图标)"},
		{1356, "兵种激增", "巨型者激增 (+1000% 生成)"},
		{1371, "兵种激增", "移动工厂激增 (+50% 生成 v2)"},
		{1188, "星球环境与通路", "浓密阴霾"},
		{1345, "临时配发战备", "装备实验：轨道120mm高爆/电磁弹火力网"},
		{1373, "星球环境与通路", "一级异界风暴"},
		{1311, "设施与星球标记", "巢穴星球"},
		{1217, "空间站与战术行动", "民主空间站"},
		{1336, "未解效果", "未解效果 #1336"},
		{1429, "未解效果", "未解效果 #1429"},
	}
	for _, tc := range checks {
		eff, ok := g.EffectsByID[tc.id]
		if !ok {
			t.Fatalf("missing effect #%d", tc.id)
		}
		if eff.GroupLabel != tc.group || eff.Name != tc.name {
			t.Fatalf("effect #%d mismatch: got (%q, %q), want (%q, %q)", tc.id, eff.GroupLabel, eff.Name, tc.group, tc.name)
		}
	}
}
