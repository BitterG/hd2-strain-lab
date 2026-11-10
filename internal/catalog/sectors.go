package catalog

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

const (
	polarWedges = 24 // 15 deg each
	polarRings  = 10 // 0.1 radius each
)

// BuildSectors computes sector stats and the 24x10 polar grid union boundary rings,
// matching the Helldivers 2 / galaxy-map-v2.html sector layout.
func BuildSectors(planets []*Planet) ([]*SectorInfo, map[string]*SectorInfo) {
	if len(planets) == 0 {
		return nil, map[string]*SectorInfo{}
	}

	bySector := make(map[string][]*Planet)
	for _, p := range planets {
		sec := p.SectorZH
		if sec == "" {
			sec = p.SectorEN
		}
		if sec == "" {
			sec = "未知星区"
		}
		bySector[sec] = append(bySector[sec], p)
	}

	// Build sector stats
	sectorsByName := make(map[string]*SectorInfo, len(bySector))
	for name, list := range bySector {
		var humanHP, totalHP float64
		var sumX, sumY float64
		enemyTally := make(map[int]int)
		for _, p := range list {
			hp := p.MaxHealth
			if hp <= 0 {
				hp = 1000000
			}
			if p.Owner == FactionHumans {
				humanHP += hp
			} else {
				humanHP += math.Max(0, hp-p.Health)
				if p.Owner >= 2 && p.Owner <= 4 {
					enemyTally[p.Owner]++
				}
			}
			totalHP += hp
			sumX += p.X
			sumY += p.Y
		}
		libPct := 100.0
		if totalHP > 0 {
			libPct = humanHP / totalHP * 100.0
		}
		dominant := FactionHumans
		bestCnt := 0
		var races []int
		for r, cnt := range enemyTally {
			races = append(races, r)
			if cnt > bestCnt || (cnt == bestCnt && r < dominant) {
				bestCnt = cnt
				dominant = r
			}
		}
		sort.Ints(races)
		sectorsByName[name] = &SectorInfo{
			Name:        name,
			PlanetCount: len(list),
			LibPct:      libPct,
			EnemyRaces:  races,
			Dominant:    dominant,
			HasEnemy:    len(races) > 0,
			CentroidX:   sumX / float64(len(list)),
			CentroidY:   sumY / float64(len(list)),
		}
	}

	// 1. Assign every polar grid cell (w, r) to a sector
	var cellOwner [polarWedges][polarRings]string
	wedgeDeg := 360.0 / float64(polarWedges)

	cellOf := func(x, y float64) (int, int) {
		ang := math.Mod(math.Mod(math.Atan2(y, x)*180.0/math.Pi+90.0, 360.0)+360.0, 360.0)
		w := int(math.Floor(ang / wedgeDeg))
		if w >= polarWedges {
			w = polarWedges - 1
		}
		r := int(math.Floor(math.Hypot(x, y) * float64(polarRings)))
		if r >= polarRings {
			r = polarRings - 1
		}
		if r < 0 {
			r = 0
		}
		return w, r
	}

	claims := make(map[[2]int][]*Planet)
	for _, p := range planets {
		w, r := cellOf(p.X, p.Y)
		k := [2]int{w, r}
		claims[k] = append(claims[k], p)
	}

	for k, list := range claims {
		w, r := k[0], k[1]
		th := ((float64(w)+0.5)*wedgeDeg - 90.0) * math.Pi / 180.0
		rad := (float64(r) + 0.5) / float64(polarRings)
		cx, cy := rad*math.Cos(th), rad*math.Sin(th)

		type tally struct {
			n int
			d float64
		}
		counts := make(map[string]tally)
		for _, p := range list {
			sec := p.SectorZH
			if sec == "" {
				sec = p.SectorEN
			}
			d := math.Hypot(p.X-cx, p.Y-cy)
			t := counts[sec]
			if t.n == 0 || d < t.d {
				t.d = d
			}
			t.n++
			counts[sec] = t
		}
		bestSec := ""
		bestN := -1
		bestD := math.Inf(1)
		for sec, t := range counts {
			if t.n > bestN || (t.n == bestN && (t.d < bestD || (t.d == bestD && sec < bestSec))) {
				bestSec, bestN, bestD = sec, t.n, t.d
			}
		}
		cellOwner[w][r] = bestSec
	}

	// Fill unclaimed cells from nearest planet
	for w := 0; w < polarWedges; w++ {
		for r := 0; r < polarRings; r++ {
			if cellOwner[w][r] != "" {
				continue
			}
			th := ((float64(w)+0.5)*wedgeDeg - 90.0) * math.Pi / 180.0
			rad := (float64(r) + 0.5) / float64(polarRings)
			mx, my := rad*math.Cos(th), rad*math.Sin(th)
			bestD := math.Inf(1)
			var bestP *Planet
			for _, p := range planets {
				dx, dy := p.X-mx, p.Y-my
				d := dx*dx + dy*dy
				if d < bestD {
					bestD = d
					bestP = p
				}
			}
			if bestP != nil {
				sec := bestP.SectorZH
				if sec == "" {
					sec = bestP.SectorEN
				}
				cellOwner[w][r] = sec
			}
		}
	}

	ownerAt := func(w, r int) string {
		if r < 0 || r >= polarRings {
			return "__OUT__"
		}
		ww := ((w % polarWedges) + polarWedges) % polarWedges
		if cellOwner[ww][r] == "" {
			return "__OUT__"
		}
		return cellOwner[ww][r]
	}

	polarPt := func(w float64, rad float64) [2]float64 {
		th := (w*wedgeDeg - 90.0) * math.Pi / 180.0
		return [2]float64{rad * math.Cos(th), rad * math.Sin(th)}
	}

	type gridNode struct {
		w int
		r int
	}
	normNode := func(w, r int) gridNode {
		if r <= 0 {
			return gridNode{0, 0}
		}
		return gridNode{((w % polarWedges) + polarWedges) % polarWedges, r}
	}

	// 2. Collect directed boundary edges per sector
	type dirEdge struct {
		a, b gridNode
	}
	edgesOf := make(map[string][]dirEdge)
	addDir := func(sec string, a, b gridNode) {
		if sec == "__OUT__" || sec == "" {
			return
		}
		edgesOf[sec] = append(edgesOf[sec], dirEdge{a, b})
	}
	for w := 0; w < polarWedges; w++ {
		for r := 0; r < polarRings; r++ {
			s := ownerAt(w, r)
			if r > 0 && ownerAt(w, r-1) != s {
				addDir(s, normNode(w, r), normNode(w+1, r))
			}
			if r > 0 && ownerAt(w+1, r) != s {
				addDir(s, normNode(w+1, r), normNode(w+1, r+1))
			}
			if ownerAt(w, r+1) != s {
				addDir(s, normNode(w+1, r+1), normNode(w, r+1))
			}
			if r > 0 && ownerAt(w-1, r) != s {
				addDir(s, normNode(w, r+1), normNode(w, r))
			}
		}
	}

	// Center ring jump edges if r=0 is split among sectors
	centerOver := make(map[string]map[gridNode]gridNode)
	var r0 [polarWedges]string
	oneRing := true
	for w := 0; w < polarWedges; w++ {
		r0[w] = ownerAt(w, 0)
		if w > 0 && r0[w] != r0[0] {
			oneRing = false
		}
	}
	if !oneRing {
		for w0 := 0; w0 < polarWedges; w0++ {
			if r0[w0] == r0[(w0-1+polarWedges)%polarWedges] {
				continue
			}
			l := 0
			for l < polarWedges && r0[(w0+l)%polarWedges] == r0[w0] {
				l++
			}
			m := centerOver[r0[w0]]
			if m == nil {
				m = make(map[gridNode]gridNode)
				centerOver[r0[w0]] = m
			}
			m[normNode(w0, 1)] = normNode(w0+l, 1)
		}
	}

	sampleEdge := func(a, b gridNode) [][2]float64 {
		if a.r != b.r || a.w == b.w {
			return [][2]float64{
				polarPt(float64(a.w), float64(a.r)/float64(polarRings)),
				polarPt(float64(b.w), float64(b.r)/float64(polarRings)),
			}
		}
		dw := b.w - a.w
		if dw > 1 {
			dw = -1
		} else if dw < -1 {
			dw = 1
		}
		steps := 12 // 15 deg / 12 = 1.25 deg per segment
		pts := make([][2]float64, 0, steps+1)
		rad := float64(a.r) / float64(polarRings)
		for i := 0; i <= steps; i++ {
			pts = append(pts, polarPt(float64(a.w)+float64(dw)*float64(i)/float64(steps), rad))
		}
		return pts
	}

	stepDir := func(a, b gridNode) (int, int) {
		dw := b.w - a.w
		if dw > 1 {
			dw = -1
		} else if dw < -1 {
			dw = 1
		}
		return dw, b.r - a.r
	}

	// 3. Trace closed rings for each sector
	for sec, edgeList := range edgesOf {
		info, ok := sectorsByName[sec]
		if !ok {
			continue
		}
		outs := make(map[gridNode][]gridNode)
		for _, e := range edgeList {
			outs[e.a] = append(outs[e.a], e.b)
		}
		over := centerOver[sec]
		used := make(map[string]bool)
		edgeKey := func(a, b gridNode) string {
			return fmt.Sprintf("%d:%d->%d:%d", a.w, a.r, b.w, b.r)
		}

		nextAfter := func(din [2]int, hasDin bool, cur gridNode) (gridNode, bool) {
			if over != nil {
				if f, ok := over[cur]; ok {
					return f, true
				}
			}
			list := outs[cur]
			if len(list) == 0 {
				return gridNode{}, false
			}
			if len(list) == 1 || !hasDin || cur == (gridNode{0, 0}) {
				return list[0], true
			}
			bestAng := math.Inf(-1)
			best := list[0]
			for _, d := range list {
				dw, dr := stepDir(cur, d)
				if dw == -din[0] && dr == -din[1] {
					continue
				}
				ang := math.Atan2(float64(din[0]*dr-din[1]*dw), float64(din[0]*dw+din[1]*dr))
				if ang > bestAng {
					bestAng = ang
					best = d
				}
			}
			return best, true
		}

		for _, e0 := range edgeList {
			if used[edgeKey(e0.a, e0.b)] {
				continue
			}
			var pts [][2]float64
			cur, nxt := e0.a, e0.b
			var din [2]int
			hasDin := false
			for guard := 0; guard < 2000; guard++ {
				k := edgeKey(cur, nxt)
				if used[k] {
					break
				}
				used[k] = true
				seg := sampleEdge(cur, nxt)
				if over != nil {
					if target, ok := over[cur]; ok && target == nxt {
						pts = append(pts, [2]float64{0, 0})
					}
				}
				if len(pts) == 0 {
					pts = append(pts, seg...)
				} else {
					pts = append(pts, seg[1:]...)
				}
				dw, dr := stepDir(cur, nxt)
				din = [2]int{dw, dr}
				hasDin = true
				cur = nxt
				if cur == e0.a {
					adv, ok := nextAfter(din, hasDin, cur)
					if ok && adv == e0.b {
						break
					}
				}
				adv, ok := nextAfter(din, hasDin, cur)
				if !ok {
					break
				}
				nxt = adv
			}
			if len(pts) >= 4 {
				info.Rings = append(info.Rings, pts)
			}
		}
	}

	out := make([]*SectorInfo, 0, len(sectorsByName))
	for _, s := range sectorsByName {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *SectorInfo) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out, sectorsByName
}
