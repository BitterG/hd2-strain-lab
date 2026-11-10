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
