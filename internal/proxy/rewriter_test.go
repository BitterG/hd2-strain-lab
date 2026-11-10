package proxy

import (
	"encoding/json"
	"testing"

	"hd2-mygo-cpv/assets"
	"hd2-mygo-cpv/internal/config"
)

func TestRewriteStatusEffectsAndCampaign(t *testing.T) {
	plan := config.Plan{
		Schema: 2,
		Targets: []config.PlanetTarget{
			{
				Planet:        78,
				Enabled:       true,
				Effects:       []int{1202, 1142},
				Owner:         2,
				Campaign:      true,
				ForceOpen:     true,
				CampaignType:  0,
				CampaignRace:  2,
				CampaignCount: 25,
			},
		},
	}

	ctx := &RewriteContext{
		AllowedEffects: map[int]bool{1202: true, 1203: true, 1142: true},
		EffectTypes:    map[int]int{1202: 40, 1203: 71, 1142: 32},
		CampaignIDs:    make(map[int]int),
		DefenseStates:  make(map[int]DefenseState),
	}

	out, summary, err := RewriteStatus(assets.StatusSampleJSON, plan, ctx)
	if err != nil {
		t.Fatalf("RewriteStatus failed: %v", err)
	}
	if summary.ChangedCount == 0 || summary.EffectsAdded < 2 {
		t.Fatalf("expected effects added and changes, got %+v", summary)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output JSON invalid: %v", err)
	}
	entries := doc["planetActiveEffects"].([]any)
	found1202, found1203, found1142 := false, false, false
	for _, row := range entries {
		m := row.(map[string]any)
		if int(m["index"].(float64)) == 78 {
			switch int(m["galacticEffectId"].(float64)) {
			case 1202:
				found1202 = true
			case 1203:
				found1203 = true
			case 1142:
				found1142 = true
			}
		}
	}
	if !found1202 || !found1203 || !found1142 {
		t.Fatalf("missing injected or auto-paired effects: 1202=%v 1203=%v 1142=%v", found1202, found1203, found1142)
	}
}

func TestRewriteStatusDefenseCampaign(t *testing.T) {
	plan := config.Plan{
		Schema: 2,
		Targets: []config.PlanetTarget{
			{
				Planet:        0,
				Enabled:       true,
				Effects:       []int{1142},
				Owner:         1,
				Campaign:      true,
				CampaignType:  4,
				CampaignRace:  2,
				CampaignCount: 20,
				DefenseSource: 78,
				DefenseLevel:  25,
				DefenseHours:  48,
				DefenseCount:  38,
			},
		},
	}

	ctx := &RewriteContext{
		AllowAllEffects: true,
		CampaignIDs:     make(map[int]int),
		DefenseStates:   make(map[int]DefenseState),
	}

	out, summary, err := RewriteStatus(assets.StatusSampleJSON, plan, ctx)
	if err != nil {
		t.Fatalf("RewriteStatus defense failed: %v", err)
	}
	if summary.ChangedCount == 0 {
		t.Fatalf("expected defense changes")
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output JSON invalid: %v", err)
	}
	events := doc["planetEvents"].([]any)
	hasDefenseEvent := false
	for _, row := range events {
		m := row.(map[string]any)
		if int(m["planetIndex"].(float64)) == 0 && int(m["eventType"].(float64)) == 1 {
			hasDefenseEvent = true
			break
		}
	}
	if !hasDefenseEvent {
		t.Fatalf("expected defense event for planet 0")
	}
}

func TestRewriteStatusDisableNativeEffects(t *testing.T) {
	// Planet 126 (Turing) natively has 1197, 1366, 1395 in Status.sample.json
	plan := config.Plan{
		Schema: 2,
		Targets: []config.PlanetTarget{
			{
				Planet:                126,
				Enabled:               true,
				Effects:               []int{1142},
				DisabledNativeEffects: []int{1395, 1366},
			},
		},
	}

	ctx := &RewriteContext{
		AllowAllEffects: true,
		CampaignIDs:     make(map[int]int),
		DefenseStates:   make(map[int]DefenseState),
	}

	out, summary, err := RewriteStatus(assets.StatusSampleJSON, plan, ctx)
	if err != nil {
		t.Fatalf("RewriteStatus failed: %v", err)
	}
	if summary.EffectsRemoved != 2 || summary.EffectsAdded != 1 {
		t.Fatalf("expected 2 removed and 1 added, got %+v", summary)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if hasPlanetEffect(doc, 126, 1395) || hasPlanetEffect(doc, 126, 1366) {
		t.Fatalf("expected native effects 1395 and 1366 to be removed from planet 126")
	}
	if !hasPlanetEffect(doc, 126, 1197) || !hasPlanetEffect(doc, 126, 1142) {
		t.Fatalf("expected native effect 1197 preserved and custom 1142 added on planet 126")
	}
}
