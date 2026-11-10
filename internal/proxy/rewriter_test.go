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

func TestRewriteStatusDisableGlobalEventEffects(t *testing.T) {
	// Simulate live Status where globalEvents applies [1400, 1401] to [124, 126]
	raw := []byte(`{
		"warId": 801,
		"time": 86383570,
		"planetStatus": [{"index": 124, "owner": 2, "health": 1000000}, {"index": 126, "owner": 1, "health": 2000000}],
		"planetActiveEffects": [{"index": 124, "galacticEffectId": 1395}, {"index": 124, "galacticEffectId": 1430}, {"index": 124, "galacticEffectId": 1431}],
		"campaigns": [],
		"globalEvents": [
			{"eventId": 1501111, "effectIds": [1400, 1401], "planetIndices": [124, 126]}
		]
	}`)
	plan := config.Plan{
		Schema: 2,
		Targets: []config.PlanetTarget{
			{
				Planet:                124,
				Enabled:               true,
				DisabledNativeEffects: []int{1395, 1400, 1401, 1430, 1431},
			},
		},
	}
	ctx := &RewriteContext{
		AllowAllEffects: true,
		CampaignIDs:     make(map[int]int),
		DefenseStates:   make(map[int]DefenseState),
	}
	out, summary, err := RewriteStatus(raw, plan, ctx)
	if err != nil {
		t.Fatalf("RewriteStatus failed: %v", err)
	}
	if summary.EffectsRemoved != 4 {
		t.Fatalf("expected 4 removed (3 planetActiveEffects + 1 globalEvent planet removal), got %+v", summary)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	geList := doc["globalEvents"].([]any)
	ge0 := geList[0].(map[string]any)
	pIndices := ge0["planetIndices"].([]any)
	if len(pIndices) != 1 || int(pIndices[0].(float64)) != 126 {
		t.Fatalf("expected planet 124 removed from globalEvents[0].planetIndices leaving [126], got %v", pIndices)
	}
	if hasPlanetEffect(doc, 124, 1395) || hasPlanetEffect(doc, 124, 1400) || hasPlanetEffect(doc, 124, 1430) {
		t.Fatalf("expected all disabled effects gone on planet 124")
	}
}
