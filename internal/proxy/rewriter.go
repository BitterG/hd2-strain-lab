package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"hd2-mygo-cpv/internal/config"
)

// Verified type40 tag / type71 presence pairs (War 801).
var effectPairs = [][2]int{
	{1202, 1203},
	{1243, 1245},
	{1244, 1386},
	{1248, 1249},
	{1303, 1310},
	{1306, 1309},
	{1307, 1308},
	{1360, 1361},
	{1377, 1378},
	{1380, 1379},
	{1401, 1400},
	{1402, 1403},
	{1413, 1414},
	{1430, 1431},
	{1434, 1433},
}

var nativeEnemyEffects = map[int]int{
	1361: 3,
}

func decodeJSONObject(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("JSON 对象为空")
	}
	return doc, nil
}

func decodeJSONAny(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.Atoi(x.String()); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(x.String(), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f {
			return int(f), true
		}
		return 0, false
	case float64:
		if math.Trunc(x) == x {
			return int(x), true
		}
		return 0, false
	case int:
		return x, true
	case int64:
		return int(x), true
	default:
		return 0, false
	}
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(x.String(), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func deepCopyJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = deepCopyJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = deepCopyJSON(val)
		}
		return out
	default:
		return v
	}
}

type DefenseState struct {
	Signature string
	ID        int
	Start     float64
	Expire    float64
}

type RewriteContext struct {
	AllowedEffects  map[int]bool
	EffectTypes     map[int]int
	AllowAllEffects bool
	RegionReference map[string]any
	CampaignIDs     map[int]int
	DefenseStates   map[int]DefenseState
}

type RewriteSummary struct {
	ChangedCount   int
	EffectsAdded   int
	EffectsRemoved int
	PlanetsPatched int
	Notes          []string
}

// RewriteStatus mutates a decoded WarStatus response safely according to the active Plan.
func RewriteStatus(raw []byte, plan config.Plan, ctx *RewriteContext) ([]byte, RewriteSummary, error) {
	var summary RewriteSummary
	targets, err := config.NormalizeTargets(plan)
	if err != nil {
		return raw, summary, err
	}
	if len(targets) == 0 {
		return raw, summary, nil
	}

	doc, err := decodeJSONObject(raw)
	if err != nil {
		return raw, summary, fmt.Errorf("战况 JSON 解析失败: %w", err)
	}
	rawEntries, ok1 := asSlice(doc["planetActiveEffects"])
	rawPlanets, ok2 := asSlice(doc["planetStatus"])
	if !ok1 || !ok2 {
		return raw, summary, fmt.Errorf("战况结构不兼容，已保持原样")
	}

	knownPlanets := make(map[int]bool, len(rawPlanets))
	for _, row := range rawPlanets {
		m, ok := asMap(row)
		if !ok {
			return raw, summary, fmt.Errorf("星球条目结构不兼容")
		}
		idx, okIdx := asInt(m["index"])
		if !okIdx {
			return raw, summary, fmt.Errorf("星球索引缺失")
		}
		knownPlanets[idx] = true
	}

	type pairKey struct{ p, e int }
	existingPairs := make(map[pairKey]bool, len(rawEntries))
	for _, row := range rawEntries {
		m, ok := asMap(row)
		if !ok {
			continue
		}
		pIdx, okP := asInt(m["index"])
		eID, okE := asInt(m["galacticEffectId"])
		if okP && okE {
			existingPairs[pairKey{pIdx, eID}] = true
		}
	}

	// Filter targets to known planets & allowed effects
	activeTargets := make([]config.PlanetTarget, 0, len(targets))
	for _, t := range targets {
		if !knownPlanets[t.Planet] {
			summary.Notes = append(summary.Notes, fmt.Sprintf("战况缺少星球 #%d", t.Planet))
			continue
		}
		if !ctx.AllowAllEffects && len(ctx.AllowedEffects) > 0 {
			filtered := make([]int, 0, len(t.Effects))
			skipped := 0
			for _, eid := range t.Effects {
				if ctx.AllowedEffects[eid] {
					filtered = append(filtered, eid)
				} else {
					skipped++
				}
			}
			if skipped > 0 {
				summary.Notes = append(summary.Notes, fmt.Sprintf("星球 #%d 跳过 %d 个当前目录不支持的效果", t.Planet, skipped))
			}
			t.Effects = filtered
		}
		// Auto-pair type40/type71 effects
		for _, pair := range effectPairs {
			tag, pres := pair[0], pair[1]
			hasTag := containsInt(t.Effects, tag)
			hasPres := containsInt(t.Effects, pres)
			if !hasTag && !hasPres {
				continue
			}
			allowedPair := ctx.AllowAllEffects || (len(ctx.AllowedEffects) == 0) ||
				(ctx.AllowedEffects[tag] && ctx.AllowedEffects[pres] &&
					(len(ctx.EffectTypes) == 0 || (ctx.EffectTypes[tag] == 40 && ctx.EffectTypes[pres] == 71)))
			if allowedPair {
				if !hasTag {
					t.Effects = append(t.Effects, tag)
				}
				if !hasPres {
					t.Effects = append(t.Effects, pres)
				}
			}
		}
		activeTargets = append(activeTargets, t)
	}
	if len(activeTargets) == 0 {
		return raw, summary, nil
	}

	// Work on deep copy
	res := deepCopyJSON(doc).(map[string]any)
	entries, _ := asSlice(res["planetActiveEffects"])
	planets, _ := asSlice(res["planetStatus"])
	campaigns, _ := asSlice(res["campaigns"])
	if campaigns == nil {
		campaigns = []any{}
	}

	// Remove disabled native effects first
	disabledPairs := make(map[pairKey]bool)
	for _, t := range activeTargets {
		for _, eid := range t.DisabledNativeEffects {
			disabledPairs[pairKey{t.Planet, eid}] = true
		}
	}
	if len(disabledPairs) > 0 {
		keptEntries := make([]any, 0, len(entries))
		clear(existingPairs)
		for _, row := range entries {
			if m, ok := asMap(row); ok {
				pIdx, okP := asInt(m["index"])
				eID, okE := asInt(m["galacticEffectId"])
				if okP && okE {
					k := pairKey{pIdx, eID}
					if disabledPairs[k] {
						summary.EffectsRemoved++
						summary.ChangedCount++
						continue
					}
					existingPairs[k] = true
				}
			}
			keptEntries = append(keptEntries, row)
		}
		entries = keptEntries
		res["planetActiveEffects"] = entries

		// Also remove disabled native effects applied via globalEvents[].planetIndices
		if geList, okGE := asSlice(res["globalEvents"]); okGE {
			for _, geRaw := range geList {
				ge, okM := asMap(geRaw)
				if !okM {
					continue
				}
				rawEffs, okE := asSlice(ge["effectIds"])
				rawPlanetsGE, okP := asSlice(ge["planetIndices"])
				if !okE || !okP || len(rawEffs) == 0 {
					continue
				}
				geEffs := make([]int, 0, len(rawEffs))
				for _, r := range rawEffs {
					if id, ok := asInt(r); ok {
						geEffs = append(geEffs, id)
					}
				}
				if len(geEffs) == 0 {
					continue
				}
				if len(rawPlanetsGE) > 0 {
					keptPlanets := make([]any, 0, len(rawPlanetsGE))
					changedGE := false
					for _, rp := range rawPlanetsGE {
						pid, okID := asInt(rp)
						if !okID {
							keptPlanets = append(keptPlanets, rp)
							continue
						}
						anyDisabled := false
						for _, eid := range geEffs {
							if disabledPairs[pairKey{pid, eid}] {
								anyDisabled = true
								break
							}
						}
						if anyDisabled {
							changedGE = true
							summary.EffectsRemoved++
							summary.ChangedCount++
							// Preserve any non-disabled effect from the same globalEvent via planetActiveEffects
							for _, eid := range geEffs {
								k := pairKey{pid, eid}
								if !disabledPairs[k] && !existingPairs[k] {
									existingPairs[k] = true
									entries = append(entries, map[string]any{
										"index":            pid,
										"galacticEffectId": eid,
									})
								}
							}
							continue
						}
						keptPlanets = append(keptPlanets, rp)
					}
					if changedGE {
						ge["planetIndices"] = keptPlanets
						res["planetActiveEffects"] = entries
					}
				} else {
					// Galaxy-wide globalEvent (empty planetIndices): if any target disables an effect in it,
					// materialize planetIndices as all other known planets.
					disabledPlanetsForGE := make(map[int]bool)
					for _, t := range activeTargets {
						for _, eid := range geEffs {
							if disabledPairs[pairKey{t.Planet, eid}] {
								disabledPlanetsForGE[t.Planet] = true
								break
							}
						}
					}
					if len(disabledPlanetsForGE) > 0 {
						expanded := make([]any, 0, len(knownPlanets))
						for pid := range knownPlanets {
							if disabledPlanetsForGE[pid] {
								summary.EffectsRemoved++
								summary.ChangedCount++
								for _, eid := range geEffs {
									k := pairKey{pid, eid}
									if !disabledPairs[k] && !existingPairs[k] {
										existingPairs[k] = true
										entries = append(entries, map[string]any{
											"index":            pid,
											"galacticEffectId": eid,
										})
									}
								}
								continue
							}
							expanded = append(expanded, pid)
						}
						ge["planetIndices"] = expanded
						res["planetActiveEffects"] = entries
					}
				}
			}
		}

		// Also remove disabled DSS station (#1217) or its activeEffectIds from spaceStations
		if ssList, okSS := asSlice(res["spaceStations"]); okSS {
			keptSS := make([]any, 0, len(ssList))
			changedSS := false
			for _, ssRaw := range ssList {
				ss, okM := asMap(ssRaw)
				if !okM {
					keptSS = append(keptSS, ssRaw)
					continue
				}
				pid, okP := asInt(ss["planetIndex"])
				if !okP {
					keptSS = append(keptSS, ssRaw)
					continue
				}
				if disabledPairs[pairKey{pid, 1217}] {
					changedSS = true
					summary.EffectsRemoved++
					summary.ChangedCount++
					continue
				}
				if rawEffs, okE := asSlice(ss["activeEffectIds"]); okE && len(rawEffs) > 0 {
					keptEffs := make([]any, 0, len(rawEffs))
					for _, r := range rawEffs {
						if eid, okID := asInt(r); okID && disabledPairs[pairKey{pid, eid}] {
							changedSS = true
							summary.EffectsRemoved++
							summary.ChangedCount++
							continue
						}
						keptEffs = append(keptEffs, r)
					}
					ss["activeEffectIds"] = keptEffs
				}
				keptSS = append(keptSS, ss)
			}
			if changedSS {
				res["spaceStations"] = keptSS
			}
		}
	}

	// Add effects
	var addedEntries []any
	for _, t := range activeTargets {
		for _, eid := range t.Effects {
			k := pairKey{t.Planet, eid}
			if !existingPairs[k] {
				existingPairs[k] = true
				addedEntries = append(addedEntries, map[string]any{
					"index":            t.Planet,
					"galacticEffectId": eid,
				})
				summary.EffectsAdded++
				summary.ChangedCount++
			}
		}
	}
	if len(addedEntries) > 0 {
		entries = append(entries, addedEntries...)
		res["planetActiveEffects"] = entries
	}

	// Build lookup maps
	planetByID := make(map[int]map[string]any, len(planets))
	originalOwner := make(map[int]int, len(planets))
	for _, row := range planets {
		m := row.(map[string]any)
		idx, _ := asInt(m["index"])
		planetByID[idx] = m
		if ow, ok := asInt(m["owner"]); ok {
			originalOwner[idx] = ow
		}
	}

	// Region definitions from WarInfo if matching warId + layoutVersion
	var regionDefs []any
	worldMaxHealth := make(map[int]float64)
	nativeRegionPlanets := make(map[int]bool)
	if ctx.RegionReference != nil {
		w1, okW1 := asInt(res["warId"])
		w2, okW2 := asInt(ctx.RegionReference["warId"])
		l1, okL1 := asInt(res["layoutVersion"])
		l2, okL2 := asInt(ctx.RegionReference["layoutVersion"])
		if okW1 && okW2 && w1 == w2 && (!okL1 || !okL2 || l1 == l2) {
			if defs, ok := asSlice(ctx.RegionReference["planetRegions"]); ok {
				regionDefs = defs
				for _, d := range defs {
					if dm, ok := asMap(d); ok {
						if pid, ok := asInt(dm["planetIndex"]); ok {
							nativeRegionPlanets[pid] = true
						}
					}
				}
			}
			if infos, ok := asSlice(ctx.RegionReference["planetInfos"]); ok {
				for _, info := range infos {
					if im, ok := asMap(info); ok {
						if pid, ok := asInt(im["index"]); ok {
							if mh, ok := asFloat(im["maxHealth"]); ok {
								worldMaxHealth[pid] = mh
							}
						}
					}
				}
			}
		}
	}

	// Infer enemy helper
	inferEnemy := func(t config.PlanetTarget) int {
		if t.CampaignRace >= 2 && t.CampaignRace <= 4 {
			return t.CampaignRace
		}
		if t.Owner >= 2 && t.Owner <= 4 {
			return t.Owner
		}
		for _, c := range campaigns {
			if cm, ok := asMap(c); ok {
				if pi, _ := asInt(cm["planetIndex"]); pi == t.Planet {
					if r, _ := asInt(cm["race"]); r >= 2 && r <= 4 {
						return r
					}
				}
			}
		}
		if ow := originalOwner[t.Planet]; ow >= 2 && ow <= 4 {
			return ow
		}
		for _, row := range entries {
			if em, ok := asMap(row); ok {
				if pi, _ := asInt(em["index"]); pi == t.Planet {
					if gid, _ := asInt(em["galacticEffectId"]); nativeEnemyEffects[gid] != 0 {
						return nativeEnemyEffects[gid]
					}
				}
			}
		}
		return 2
	}

	// Owner & ForceOpen health patches + regions
	for i := range activeTargets {
		t := &activeTargets[i]
		pMap := planetByID[t.Planet]
		if t.ForceOpen && t.CampaignType != 4 {
			race := inferEnemy(*t)
			if t.CampaignRace == 0 {
				t.CampaignRace = race
			}
			if t.Owner == 0 {
				t.Owner = race
			}
			if mh, ok := worldMaxHealth[t.Planet]; ok && mh > 1 {
				if h, okH := asFloat(pMap["health"]); !okH || h <= 0 {
					pMap["health"] = int64(mh / 2)
					summary.ChangedCount++
				}
			} else if h, okH := asFloat(pMap["health"]); !okH || h <= 0 {
				pMap["health"] = 500000
				summary.ChangedCount++
			}
		}
		if t.Owner != 0 {
			curOwner, _ := asInt(pMap["owner"])
			if curOwner != t.Owner {
				pMap["owner"] = t.Owner
				summary.ChangedCount++
			}
		}

		if t.InitializeRegions || t.OpenRegions {
			regList, _ := asSlice(res["planetRegions"])
			if regList == nil && regionDefs != nil {
				regList = []any{}
			}
			if regList != nil {
				patched, newRegs := patchPlanetRegions(regList, regionDefs, *t, inferEnemy(*t))
				if patched > 0 {
					res["planetRegions"] = newRegs
					summary.ChangedCount += patched
				}
			}
		}
	}

	// Campaigns & Defense events
	usedCampaignIDs := make(map[int]bool, len(campaigns))
	maxCampaignID := 50000
	for _, c := range campaigns {
		if cm, ok := asMap(c); ok {
			if cid, ok := asInt(cm["id"]); ok {
				usedCampaignIDs[cid] = true
				if cid > maxCampaignID {
					maxCampaignID = cid
				}
			}
		}
	}

	for _, t := range activeTargets {
		requestedRace := t.CampaignRace
		if requestedRace == 0 && t.Owner >= 2 && t.Owner <= 4 {
			requestedRace = t.Owner
		}
		if requestedRace != 0 {
			for _, c := range campaigns {
				if cm, ok := asMap(c); ok {
					if pi, _ := asInt(cm["planetIndex"]); pi == t.Planet {
						if r, _ := asInt(cm["race"]); r != requestedRace {
							cm["race"] = requestedRace
							summary.ChangedCount++
						}
					}
				}
			}
		}
		if !t.Campaign {
			continue
		}
		var current map[string]any
		for _, c := range campaigns {
			if cm, ok := asMap(c); ok {
				if pi, _ := asInt(cm["planetIndex"]); pi == t.Planet {
					current = cm
					break
				}
			}
		}
		race := requestedRace
		if race == 0 {
			race = inferEnemy(t)
		}
		if current == nil {
			cid := ctx.CampaignIDs[t.Planet]
			if cid == 0 || usedCampaignIDs[cid] {
				maxCampaignID++
				cid = maxCampaignID
			}
			usedCampaignIDs[cid] = true
			ctx.CampaignIDs[t.Planet] = cid
			current = map[string]any{
				"id":          cid,
				"planetIndex": t.Planet,
				"type":        t.CampaignType,
				"count":       t.CampaignCount,
				"race":        race,
			}
			campaigns = append(campaigns, current)
			summary.ChangedCount++
		} else {
			if ct, _ := asInt(current["type"]); ct != t.CampaignType {
				current["type"] = t.CampaignType
				summary.ChangedCount++
			}
			if cnt, ok := asInt(current["count"]); !ok || cnt <= 0 || ((t.ForceOpen || t.InitializeRegions) && cnt < t.CampaignCount) {
				current["count"] = t.CampaignCount
				summary.ChangedCount++
			}
			if race != 0 {
				if cr, _ := asInt(current["race"]); cr != race {
					current["race"] = race
					summary.ChangedCount++
				}
			}
		}

		if t.CampaignType == 4 {
			if err := applyDefense(res, t, current, ctx.DefenseStates, planetByID); err != nil {
				summary.Notes = append(summary.Notes, fmt.Sprintf("星球 #%d 防守配置跳过: %v", t.Planet, err))
			} else {
				summary.ChangedCount++
			}
		}
	}
	res["campaigns"] = campaigns

	if summary.ChangedCount == 0 {
		return raw, summary, nil
	}
	summary.PlanetsPatched = len(activeTargets)

	outBytes, err := json.Marshal(res)
	if err != nil {
		return raw, summary, err
	}
	return outBytes, summary, nil
}

func patchPlanetRegions(regions []any, defs []any, t config.PlanetTarget, race int) (int, []any) {
	posByIdx := make(map[int]int)
	for i, row := range regions {
		m, ok := asMap(row)
		if !ok {
			continue
		}
		if pi, ok := asInt(m["planetIndex"]); ok && pi == t.Planet {
			if ri, ok := asInt(m["regionIndex"]); ok {
				posByIdx[ri] = i
			}
		}
	}
	changed := 0
	if t.InitializeRegions && defs != nil {
		for _, row := range defs {
			dm, ok := asMap(row)
			if !ok {
				continue
			}
			pi, okP := asInt(dm["planetIndex"])
			ri, okR := asInt(dm["regionIndex"])
			mh, okM := asInt(dm["maxHealth"])
			if !okP || !okR || !okM || pi != t.Planet || mh <= 1 {
				continue
			}
			if pos, exists := posByIdx[ri]; exists {
				rm := regions[pos].(map[string]any)
				rm["owner"] = race
				rm["isAvailable"] = true
				if f, okF := asFloat(rm["availabilityFactor"]); !okF || f <= 0 {
					rm["availabilityFactor"] = 1
				}
				if h, okH := asFloat(rm["health"]); !okH || h <= 0 || h > float64(mh) {
					rm["health"] = mh / 2
				}
				changed++
			} else {
				posByIdx[ri] = len(regions)
				regions = append(regions, map[string]any{
					"planetIndex":        t.Planet,
					"regionIndex":        ri,
					"owner":              race,
					"health":             mh / 2,
					"players":            0,
					"regerPerSecond":     0,
					"isAvailable":        true,
					"availabilityFactor": 1,
				})
				changed++
			}
		}
	}
	if t.OpenRegions {
		for _, pos := range posByIdx {
			rm := regions[pos].(map[string]any)
			if h, okH := asFloat(rm["health"]); okH && h <= 0 {
				continue
			}
			avail, _ := rm["isAvailable"].(bool)
			factor, okF := asFloat(rm["availabilityFactor"])
			if !avail || !okF || factor <= 0 {
				rm["isAvailable"] = true
				if !okF || factor <= 0 {
					rm["availabilityFactor"] = 1
				}
				changed++
			}
		}
	}
	return changed, regions
}

func applyDefense(res map[string]any, t config.PlanetTarget, campaign map[string]any, states map[int]DefenseState, planetByID map[int]map[string]any) error {
	nowTime, okNow := asFloat(res["time"])
	if !okNow || nowTime < 0 {
		return fmt.Errorf("缺少有效战况时间")
	}
	src, okSrc := planetByID[t.DefenseSource]
	if !okSrc {
		return fmt.Errorf("进攻来源星球 #%d 不存在", t.DefenseSource)
	}
	if ow, _ := asInt(src["owner"]); ow != t.CampaignRace {
		return fmt.Errorf("进攻来源星球归属与敌人阵营不一致")
	}

	events, _ := asSlice(res["planetEvents"])
	if events == nil {
		events = []any{}
	}
	joints, _ := asSlice(res["jointOperations"])
	if joints == nil {
		joints = []any{}
	}
	attacks, _ := asSlice(res["planetAttacks"])
	if attacks == nil {
		attacks = []any{}
	}

	cid, _ := asInt(campaign["id"])
	used := make(map[int]bool)
	maxID := 9100 + t.Planet
	var existingEvent map[string]any
	for _, row := range events {
		if em, ok := asMap(row); ok {
			if eid, okID := asInt(em["id"]); okID {
				used[eid] = true
				if eid > maxID {
					maxID = eid
				}
			}
			if pi, _ := asInt(em["planetIndex"]); pi == t.Planet && func() int { v, _ := asInt(em["eventType"]); return v }() == 1 {
				if ecid, _ := asInt(em["campaignId"]); ecid == cid {
					existingEvent = em
				}
			}
		}
	}
	for _, row := range joints {
		if jm, ok := asMap(row); ok {
			if jid, okID := asInt(jm["id"]); okID {
				used[jid] = true
				if jid > maxID {
					maxID = jid
				}
			}
		}
	}

	sig := fmt.Sprintf("%d:%d:%d:%d", cid, t.DefenseSource, t.CampaignRace, t.DefenseHours)
	var eid int
	var start, expire float64
	if existingEvent != nil {
		eid, _ = asInt(existingEvent["id"])
		start, _ = asFloat(existingEvent["startTime"])
		expire, _ = asFloat(existingEvent["expireTime"])
	} else if st, ok := states[t.Planet]; ok && st.Signature == sig && !used[st.ID] {
		eid, start, expire = st.ID, st.Start, st.Expire
	} else {
		eid = maxID + 1
		start = nowTime
		expire = nowTime + float64(t.DefenseHours*3600)
	}

	hasAttack := false
	for _, row := range attacks {
		if am, ok := asMap(row); ok {
			s, _ := asInt(am["source"])
			dst, _ := asInt(am["target"])
			if s == t.DefenseSource && dst == t.Planet {
				hasAttack = true
				break
			}
		}
	}
	if !hasAttack {
		attacks = append(attacks, map[string]any{
			"source": t.DefenseSource,
			"target": t.Planet,
		})
	}

	maxHP := int64(t.DefenseLevel) * 50000
	evFields := map[string]any{
		"id":                eid,
		"planetIndex":       t.Planet,
		"eventType":         1,
		"race":              t.CampaignRace,
		"health":            maxHP * 99 / 100,
		"maxHealth":         maxHP,
		"startTime":         start,
		"expireTime":        expire,
		"campaignId":        cid,
		"jointOperationIds": []any{eid},
		"potentialBuildUp":  0,
	}
	if existingEvent == nil {
		events = append(events, evFields)
	} else {
		for k, v := range evFields {
			existingEvent[k] = v
		}
	}

	hasJoint := false
	for _, row := range joints {
		if jm, ok := asMap(row); ok {
			if jid, _ := asInt(jm["id"]); jid == eid {
				hasJoint = true
				break
			}
		}
	}
	if !hasJoint {
		joints = append(joints, map[string]any{
			"id":          eid,
			"planetIndex": t.Planet,
			"hqNodeIndex": 2,
		})
	}

	campaign["count"] = t.DefenseCount
	states[t.Planet] = DefenseState{Signature: sig, ID: eid, Start: start, Expire: expire}
	res["planetEvents"] = events
	res["jointOperations"] = joints
	res["planetAttacks"] = attacks
	return nil
}

func containsInt(list []int, target int) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
