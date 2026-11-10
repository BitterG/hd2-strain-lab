package catalog

import "testing"

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
