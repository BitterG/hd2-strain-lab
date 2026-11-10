package catalog

import (
	"context"
	"math"
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
