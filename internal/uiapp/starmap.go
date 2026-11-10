package uiapp

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"hd2-mygo-cpv/internal/catalog"
	"hd2-mygo-cpv/internal/config"
)

type mapCacheKey struct {
	w, h        int
	camX, camY  float64
	zoom        float64
	tilt3D      bool
	showSector  bool
	showSupply  bool
	showFog     bool
	hoverSector string
	dataTime    int64
}

type StarMapState struct {
	CamX       float64
	CamY       float64
	Zoom       float64
	Tilt3D     bool
	ShowSector bool
	ShowSupply bool
	ShowFog    bool
	ShowLabels bool

	HoveredID     int // -1 if none
	PickingSource bool
	LastBoxX      float32
	LastBoxY      float32
	LastBoxW      float32
	LastBoxH      float32
	DragActive    bool
	DragMoved     bool
	DragButton    int
	LastPointerX  float32
	LastPointerY  float32
	PointerInMap  bool
	PointerX      float32
	PointerY      float32

	starCanvas *ui.Canvas
	starW      int
	starH      int

	layerCanvas *ui.Canvas
	layerKey    mapCacheKey
	layerValid  bool

	labelSizes map[int][2]float32
	screenXY   map[int][3]float32 // pid -> (sx, sy, depthScale) in window DIPs
}

func NewStarMapState() *StarMapState {
	return &StarMapState{
		CamX:       0,
		CamY:       0.02,
		Zoom:       1.0,
		Tilt3D:     true,
		ShowSector: true,
		ShowSupply: true,
		ShowFog:    true,
		ShowLabels: true,
		HoveredID:  -1,
		labelSizes: make(map[int][2]float32, 300),
		screenXY:   make(map[int][3]float32, 300),
	}
}

func (m *StarMapState) ResetView() {
	m.CamX = 0
	m.CamY = 0.02
	m.Zoom = 1.0
}

func (m *StarMapState) FocusPlanet(p *catalog.Planet) {
	if p == nil || p.Unlisted {
		return
	}
	m.CamX = p.X
	m.CamY = p.Y
	if m.Zoom < 2.2 {
		m.Zoom = 2.2
	}
}

func (m *StarMapState) baseScale(w, h float32) float64 {
	dim := math.Min(float64(w), float64(h))
	if dim < 200 {
		dim = 200
	}
	return dim * 0.43 * m.Zoom
}

// project maps galaxy coordinates (wx, wy) into screen coordinates inside rect r.
// In 2.5D tilted mode, the bottom of the viewport is closer to the camera (larger scale),
// and the top of the viewport is farther away (smaller scale).
func (m *StarMapState) project(wx, wy float64, r ui.Rect) (sx, sy, depthScale float32) {
	cx := float64(r.X + r.W*0.5)
	cy := float64(r.Y + r.H*0.52)
	scale := m.baseScale(r.W, r.H)
	px := (wx - m.CamX) * scale
	py := -(wy - m.CamY) * scale // wy > CamY (North/Top) -> py < 0; wy < CamY (South/Bottom) -> py > 0

	if !m.Tilt3D {
		return float32(cx + px), float32(cy + py), 1.0
	}

	const pitchRad = 42.0 * math.Pi / 180.0
	cosP := math.Cos(pitchRad)
	sinP := math.Sin(pitchRad)
	camDist := math.Max(float64(r.H)*1.35, 680.0)

	// Bottom (py > 0) has positive z toward viewer -> smaller denom -> larger k (bottom big, top small)
	z := py * sinP
	denom := camDist - z
	if denom < 140 {
		denom = 140
	}
	k := camDist / denom
	sx = float32(cx + px*k)
	sy = float32(cy + py*cosP*k)
	return sx, sy, float32(k)
}

func (m *StarMapState) unproject(sx, sy float32, r ui.Rect) (wx, wy float64) {
	cx := float64(r.X + r.W*0.5)
	cy := float64(r.Y + r.H*0.52)
	scale := m.baseScale(r.W, r.H)
	dx := float64(sx) - cx
	dy := float64(sy) - cy

	if !m.Tilt3D {
		return m.CamX + dx/scale, m.CamY - dy/scale
	}

	const pitchRad = 42.0 * math.Pi / 180.0
	cosP := math.Cos(pitchRad)
	sinP := math.Sin(pitchRad)
	camDist := math.Max(float64(r.H)*1.35, 680.0)

	// Inverse of dy = py * cosP * camDist / (camDist - py * sinP)
	denom := cosP*camDist + dy*sinP
	if denom < 80 {
		denom = 80
	}
	py := dy * camDist / denom
	z := py * sinP
	k := camDist / math.Max(camDist-z, 140)
	px := dx / k
	return m.CamX + px/scale, m.CamY - py/scale
}

func (m *StarMapState) hitTest(x, y float32) int {
	lx, ly := m.LastBoxX+x, m.LastBoxY+y
	bestID := -1
	bestD := float32(18.0)
	for pid, pt := range m.screenXY {
		dx := pt[0] - lx
		dy := pt[1] - ly
		d := float32(math.Hypot(float64(dx), float64(dy)))
		if d < bestD {
			bestD = d
			bestID = pid
		}
	}
	return bestID
}

func (m *StarMapState) ensureStarCanvas(p *ui.Painter, w, h int) {
	if w < 10 || h < 10 {
		return
	}
	if m.starCanvas != nil && m.starW == w && m.starH == h {
		return
	}
	m.starCanvas = ui.NewCanvas(w, h)
	m.starW, m.starH = w, h
	m.starCanvas.Draw(p, func(cp *ui.Painter) {
		cp.Fill(ui.Rect{W: float32(w), H: float32(h)}, BgDeep, 0)
		cp.FillRadialGradient(ui.Rect{W: float32(w), H: float32(h)}, ui.RadialGradient{
			Center: ui.Point{X: 0.5, Y: 0.52},
			Radius: float32(math.Max(float64(w), float64(h))) * 0.55,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(30, 58, 110, 0.22), At: 0},
				{Color: ui.RGBA(14, 24, 48, 0.10), At: 0.5},
				{Color: ui.RGBA(8, 11, 18, 0.0), At: 1},
			},
		}, 0)

		rng := rand.New(rand.NewPCG(72, 2026))
		count := max(140, (w*h)/3200)
		for range count {
			sx := rng.Float32() * float32(w)
			sy := rng.Float32() * float32(h)
			sz := float32(1.0)
			if rng.Float32() > 0.85 {
				sz = 1.8
			}
			alpha := float32(0.18 + rng.Float32()*0.48)
			col := ui.RGBA(200, 220, 255, alpha)
			if rng.Float32() > 0.88 {
				col = ui.RGBA(245, 220, 165, alpha)
			}
			cp.Fill(ui.Rect{X: sx, Y: sy, W: sz, H: sz}, col, sz/2)
		}
	})
}

// clipPolyToRect clips a 2D polygon against [xMin, xMax] x [yMin, yMax] using Sutherland-Hodgman.
// Keeping filled sector paths inside the viewport prevents multi-megapixel mask rasterization when zoomed in.
func clipPolyToRect(pts [][2]float32, xMin, yMin, xMax, yMax float32) [][2]float32 {
	if len(pts) < 3 {
		return nil
	}
	clipEdge := func(in [][2]float32, inside func(p [2]float32) bool, intersect func(a, b [2]float32) [2]float32) [][2]float32 {
		if len(in) == 0 {
			return nil
		}
		out := make([][2]float32, 0, len(in)+2)
		prev := in[len(in)-1]
		prevIn := inside(prev)
		for _, cur := range in {
			curIn := inside(cur)
			if curIn {
				if !prevIn {
					out = append(out, intersect(prev, cur))
				}
				out = append(out, cur)
			} else if prevIn {
				out = append(out, intersect(prev, cur))
			}
			prev = cur
			prevIn = curIn
		}
		return out
	}

	pts = clipEdge(pts, func(p [2]float32) bool { return p[0] >= xMin }, func(a, b [2]float32) [2]float32 {
		t := (xMin - a[0]) / (b[0] - a[0])
		return [2]float32{xMin, a[1] + t*(b[1]-a[1])}
	})
	pts = clipEdge(pts, func(p [2]float32) bool { return p[0] <= xMax }, func(a, b [2]float32) [2]float32 {
		t := (xMax - a[0]) / (b[0] - a[0])
		return [2]float32{xMax, a[1] + t*(b[1]-a[1])}
	})
	pts = clipEdge(pts, func(p [2]float32) bool { return p[1] >= yMin }, func(a, b [2]float32) [2]float32 {
		t := (yMin - a[1]) / (b[1] - a[1])
		return [2]float32{a[0] + t*(b[0]-a[0]), yMin}
	})
	pts = clipEdge(pts, func(p [2]float32) bool { return p[1] <= yMax }, func(a, b [2]float32) [2]float32 {
		t := (yMax - a[1]) / (b[1] - a[1])
		return [2]float32{a[0] + t*(b[0]-a[0]), yMax}
	})
	if len(pts) < 3 {
		return nil
	}
	return pts
}

func segIntersectsRect(ax, ay, bx, by, xMin, yMin, xMax, yMax float32) bool {
	if (ax < xMin && bx < xMin) || (ax > xMax && bx > xMax) ||
		(ay < yMin && by < yMin) || (ay > yMax && by > yMax) {
		return false
	}
	return true
}

func (m *StarMapState) renderMapLayer(p *ui.Painter, w, h int, g *catalog.GalaxyData, hoveredSector *catalog.SectorInfo) {
	m.ensureStarCanvas(p, w, h)
	hoverName := ""
	if hoveredSector != nil {
		hoverName = hoveredSector.Name
	}
	key := mapCacheKey{
		w: w, h: h,
		camX: m.CamX, camY: m.CamY, zoom: m.Zoom,
		tilt3D: m.Tilt3D, showSector: m.ShowSector, showSupply: m.ShowSupply, showFog: m.ShowFog,
		hoverSector: hoverName, dataTime: g.LastUpdatedAt.UnixNano(),
	}
	if m.layerCanvas == nil || m.layerKey.w != w || m.layerKey.h != h {
		m.layerCanvas = ui.NewCanvas(w, h)
		m.layerValid = false
	}
	if m.layerValid && m.layerKey == key {
		return
	}
	m.layerKey = key
	m.layerValid = true

	lr := ui.Rect{W: float32(w), H: float32(h)}
	xMin, yMin, xMax, yMax := float32(-4), float32(-4), lr.W+4, lr.H+4

	m.layerCanvas.Draw(p, func(cp *ui.Painter) {
		if m.starCanvas != nil {
			cp.Image(m.starCanvas.Bitmap(), lr, ui.FillBox)
		}

		// Concentric radar reference rings via zero-mask 2-pt polylines
		for _, rad := range []float64{0.35, 0.70, 1.0} {
			const segs = 72
			var px, py float32
			for i := 0; i <= segs; i++ {
				th := float64(i) * 2.0 * math.Pi / float64(segs)
				sx, sy, _ := m.project(rad*math.Cos(th), rad*math.Sin(th), lr)
				if i > 0 && segIntersectsRect(px, py, sx, sy, xMin, yMin, xMax, yMax) {
					cp.StrokePolyline([][2]float32{{px, py}, {sx, sy}}, 1, ui.RGBA(55, 85, 130, 0.16))
				}
				px, py = sx, sy
			}
		}

		// 1. Polar grid sector polygons (viewport clipped for fill, zero-mask polylines for borders)
		if m.ShowSector {
			for _, sec := range g.Sectors {
				isHi := hoveredSector != nil && hoveredSector.Name == sec.Name
				fillCol := ui.Color{}
				lineCol := ui.RGBA(210, 225, 245, 0.18)
				lineW := float32(0.85)
				if sec.HasEnemy || isHi {
					if sec.HasEnemy {
						fillCol = FactionColor(sec.Dominant).Alpha(0.14)
					}
					lineCol = ui.RGBA(230, 240, 252, 0.34)
					lineW = 0.95
					if isHi {
						if sec.HasEnemy {
							fillCol = FactionColor(sec.Dominant).Alpha(0.24)
						} else {
							fillCol = ColorHumans.Alpha(0.13)
						}
						lineCol = ui.RGBA(255, 255, 255, 0.58)
						lineW = 1.4
					}
				}

				for _, ring := range sec.Rings {
					if len(ring) < 3 {
						continue
					}
					proj := make([][2]float32, len(ring))
					minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
					maxX, maxY := float32(-math.MaxFloat32), float32(-math.MaxFloat32)
					for i, pt := range ring {
						sx, sy, _ := m.project(pt[0], pt[1], lr)
						proj[i] = [2]float32{sx, sy}
						if sx < minX {
							minX = sx
						}
						if sx > maxX {
							maxX = sx
						}
						if sy < minY {
							minY = sy
						}
						if sy > maxY {
							maxY = sy
						}
					}
					if maxX < xMin || minX > xMax || maxY < yMin || minY > yMax {
						continue
					}
					if fillCol.A > 0 {
						if clipped := clipPolyToRect(proj, xMin, yMin, xMax, yMax); len(clipped) >= 3 {
							var path ui.Path
							for i, pt := range clipped {
								if i == 0 {
									path.MoveTo(pt[0], pt[1])
								} else {
									path.LineTo(pt[0], pt[1])
								}
							}
							path.Close()
							cp.FillPath(&path, fillCol)
						}
					}
					for i := 0; i < len(proj); i++ {
						a := proj[i]
						b := proj[(i+1)%len(proj)]
						if segIntersectsRect(a[0], a[1], b[0], b[1], xMin, yMin, xMax, yMax) {
							cp.StrokePolyline([][2]float32{a, b}, lineW, lineCol)
						}
					}
				}
			}
		}

		// Local screen coords for cached layer
		localXY := make(map[int][3]float32, len(g.Planets))
		for _, pl := range g.Planets {
			sx, sy, ds := m.project(pl.X, pl.Y, lr)
			localXY[pl.ID] = [3]float32{sx, sy, ds}
		}

		// 2. Supply lines
		if m.ShowSupply {
			baseW := float32(1.0 + math.Min(m.Zoom*0.42, 1.8))
			for _, edge := range g.Edges {
				pa := g.PlanetsByID[edge.From]
				pb := g.PlanetsByID[edge.To]
				if pa == nil || pb == nil {
					continue
				}
				xyA, okA := localXY[edge.From]
				xyB, okB := localXY[edge.To]
				if !okA || !okB {
					continue
				}
				if !segIntersectsRect(xyA[0], xyA[1], xyB[0], xyB[1], -80, -80, lr.W+80, lr.H+80) {
					continue
				}
				if pa.Owner == pb.Owner {
					col := FactionColor(pa.Owner).Alpha(0.32)
					cp.StrokePolyline([][2]float32{{xyA[0], xyA[1]}, {xyB[0], xyB[1]}}, baseW, col)
				} else if pa.Owner == catalog.FactionHumans || pb.Owner == catalog.FactionHumans {
					cA := FactionColor(pa.Owner).Alpha(0.78)
					cB := FactionColor(pb.Owner).Alpha(0.78)
					mx := (xyA[0] + xyB[0]) * 0.5
					my := (xyA[1] + xyB[1]) * 0.5
					mid := cA.Mix(cB, 0.5)
					cp.StrokePolyline([][2]float32{{xyA[0], xyA[1]}, {mx, my}}, baseW*1.45, cA)
					cp.StrokePolyline([][2]float32{{mx, my}, {xyB[0], xyB[1]}}, baseW*1.45, cB)
					cp.Fill(ui.Rect{X: mx - baseW*0.7, Y: my - baseW*0.7, W: baseW * 1.4, H: baseW * 1.4}, mid, baseW*0.7)
				} else {
					cp.StrokePolyline([][2]float32{{xyA[0], xyA[1]}, {xyB[0], xyB[1]}}, baseW, ui.RGBA(180, 195, 220, 0.28))
				}
			}
		}

		// 3. Invasion / Defense routes
		for _, br := range g.BattleRoutes {
			xyA, okA := localXY[br.Source]
			xyB, okB := localXY[br.Target]
			if !okA || !okB || !segIntersectsRect(xyA[0], xyA[1], xyB[0], xyB[1], -40, -40, lr.W+40, lr.H+40) {
				continue
			}
			col := FactionColor(br.Attacker)
			if br.Attacker == 0 {
				col = RedDanger
			}
			var dashPath ui.Path
			dashPath.MoveTo(xyA[0], xyA[1]).LineTo(xyB[0], xyB[1]).Dash(0, 7, 4)
			cp.StrokePath(&dashPath, 2.2, col.Alpha(0.9))

			dx := xyB[0] - xyA[0]
			dy := xyB[1] - xyA[1]
			dist := float32(math.Hypot(float64(dx), float64(dy)))
			if dist > 18 {
				ux, uy := dx/dist, dy/dist
				tipX, tipY := xyB[0]-ux*11, xyB[1]-uy*11
				perpX, perpY := -uy, ux
				var arrow ui.Path
				arrow.MoveTo(tipX, tipY).
					LineTo(tipX-ux*9+perpX*4.5, tipY-uy*9+perpY*4.5).
					LineTo(tipX-ux*9-perpX*4.5, tipY-uy*9-perpY*4.5).
					Close()
				cp.FillPath(&arrow, col)
			}
		}

		// 4. Gloom & TCS+ fog
		if m.ShowFog {
			fogScale := float32(m.Zoom)
			for _, pl := range g.Planets {
				if !pl.InGloom && !pl.InTCS {
					continue
				}
				xy, ok := localXY[pl.ID]
				if !ok {
					continue
				}
				sx, sy, ds := xy[0], xy[1], xy[2]
				if pl.InGloom {
					rad := min(float32(28)*fogScale*ds, 130)
					if sx+rad < 0 || sx-rad > lr.W || sy+rad < 0 || sy-rad > lr.H {
						continue
					}
					cp.FillRadialGradient(ui.Rect{X: sx - rad, Y: sy - rad, W: 2 * rad, H: 2 * rad}, ui.RadialGradient{
						Stops: []ui.GradientStop{
							{Color: ui.RGBA(244, 200, 48, 0.30), At: 0},
							{Color: ui.RGBA(218, 165, 24, 0.14), At: 0.52},
							{Color: ui.RGBA(206, 160, 24, 0.0), At: 1},
						},
					}, rad)
				}
				if pl.InTCS {
					rad := min(float32(22)*fogScale*ds, 100)
					if sx+rad < 0 || sx-rad > lr.W || sy+rad < 0 || sy-rad > lr.H {
						continue
					}
					cp.FillRadialGradient(ui.Rect{X: sx - rad, Y: sy - rad, W: 2 * rad, H: 2 * rad}, ui.RadialGradient{
						Stops: []ui.GradientStop{
							{Color: ui.RGBA(96, 165, 250, 0.28), At: 0},
							{Color: ui.RGBA(96, 165, 250, 0.0), At: 1},
						},
					}, rad)
				}
			}
		}

		// 5. Faction centroid labels
		factionSum := map[int][3]float64{}
		for _, pl := range g.Planets {
			if pl.Owner < 1 || pl.Owner > 4 {
				continue
			}
			cur := factionSum[pl.Owner]
			cur[0] += pl.X
			cur[1] += pl.Y
			cur[2] += 1
			factionSum[pl.Owner] = cur
		}
		for fac, sum := range factionSum {
			if sum[2] < 2 {
				continue
			}
			sx, sy, _ := m.project(sum[0]/sum[2], sum[1]/sum[2], lr)
			if sx < -60 || sx > lr.W+60 || sy < -30 || sy > lr.H+30 {
				continue
			}
			cp.Text(sx-24, sy-10, catalog.FactionNameZH(fac), 15, FactionColor(fac).Alpha(0.36))
		}
	})
}

func (a *App) buildStarMap(c *ui.Context) {
	m := a.starMap
	g := a.galaxy

	targetMap := a.targetMap()
	selectedSet := make(map[int]bool, len(a.selectedIDs))
	for _, id := range a.selectedIDs {
		selectedSet[id] = true
	}

	var hoveredSector *catalog.SectorInfo
	if m.HoveredID >= 0 {
		if hp := g.PlanetsByID[m.HoveredID]; hp != nil {
			hoveredSector = g.SectorsByName[hp.SectorZH]
		}
	} else if len(a.selectedIDs) > 0 {
		if sp := g.PlanetsByID[a.selectedIDs[0]]; sp != nil {
			hoveredSector = g.SectorsByName[sp.SectorZH]
		}
	}

	outer := ui.Box(c).Grow(1).FillHeight().Clip().Background(BgDeep)
	outer.Children(func() {
		canvas := ui.Box(c.Key("starmap-canvas")).Absolute().Top(0).Left(0).Right(0).Bottom(0)
		canvas.HandleInput(func(ev ui.InputEvent) bool {
			r := ui.Rect{W: m.LastBoxW, H: m.LastBoxH}
			if r.W <= 0 || r.H <= 0 {
				return false
			}
			switch ev.Kind {
			case ui.InputPointerMove:
				m.PointerInMap = true
				m.PointerX, m.PointerY = ev.X, ev.Y
				if m.DragActive {
					dx := ev.X - m.LastPointerX
					dy := ev.Y - m.LastPointerY
					if math.Abs(float64(dx)) > 1 || math.Abs(float64(dy)) > 1 {
						m.DragMoved = true
					}
					wx0, wy0 := m.unproject(m.LastPointerX, m.LastPointerY, ui.Rect{W: m.LastBoxW, H: m.LastBoxH})
					wx1, wy1 := m.unproject(ev.X, ev.Y, ui.Rect{W: m.LastBoxW, H: m.LastBoxH})
					m.CamX -= (wx1 - wx0)
					m.CamY -= (wy1 - wy0)
					m.LastPointerX, m.LastPointerY = ev.X, ev.Y
					return true
				}
				hid := m.hitTest(ev.X, ev.Y)
				if hid != m.HoveredID {
					m.HoveredID = hid
					return true
				}
				return m.HoveredID >= 0

			case ui.InputPointerDown:
				m.PointerInMap = true
				m.PointerX, m.PointerY = ev.X, ev.Y
				if ev.Button == 0 || ev.Button == 1 || ev.Button == 2 {
					m.DragActive = true
					m.DragMoved = false
					m.DragButton = ev.Button
					m.LastPointerX, m.LastPointerY = ev.X, ev.Y
					return true
				}

			case ui.InputPointerUp:
				wasDragMoved := m.DragMoved
				btn := m.DragButton
				m.DragActive = false
				m.DragMoved = false
				if ev.Canceled {
					return true
				}
				if !wasDragMoved && btn == 0 {
					pid := m.hitTest(ev.X, ev.Y)
					if pid >= 0 {
						if m.PickingSource {
							a.assignDefenseSource(pid)
							m.PickingSource = false
							return true
						}
						if ev.Clicks >= 2 {
							if p := g.PlanetsByID[pid]; p != nil {
								m.FocusPlanet(p)
							}
						}
						a.handleMapSelect(pid, ev.Mods)
						return true
					} else if ev.Mods&(ui.Ctrl|ui.Shift) == 0 && !m.PickingSource {
						a.selectedIDs = nil
						return true
					}
				}
				return true

			case ui.InputScroll:
				factor := 1.16
				if ev.DY > 0 {
					factor = 1.0 / 1.16
				}
				newZoom := m.Zoom * factor
				if newZoom < 0.55 {
					newZoom = 0.55
				}
				if newZoom > 12.0 {
					newZoom = 12.0
				}
				if newZoom != m.Zoom {
					wxBefore, wyBefore := m.unproject(ev.X, ev.Y, ui.Rect{W: m.LastBoxW, H: m.LastBoxH})
					m.Zoom = newZoom
					wxAfter, wyAfter := m.unproject(ev.X, ev.Y, ui.Rect{W: m.LastBoxW, H: m.LastBoxH})
					m.CamX += (wxBefore - wxAfter)
					m.CamY += (wyBefore - wyAfter)
				}
				return true

			case ui.InputZoom:
				newZoom := m.Zoom * float64(ev.Scale)
				if newZoom < 0.55 {
					newZoom = 0.55
				}
				if newZoom > 12.0 {
					newZoom = 12.0
				}
				m.Zoom = newZoom
				return true
			}
			return false
		})

		canvas.Draw(func(p *ui.Painter, r ui.Rect) {
			m.LastBoxX, m.LastBoxY, m.LastBoxW, m.LastBoxH = r.X, r.Y, r.W, r.H
			w, h := int(r.W), int(r.H)
			if w < 10 || h < 10 {
				return
			}

			// Blit cached background + sectors + supply lines + fog layer
			m.renderMapLayer(p, w, h, g, hoveredSector)
			if m.layerCanvas != nil {
				p.Image(m.layerCanvas.Bitmap(), r, ui.FillBox)
			}

			// Pre-project listed planets into window DIPs for fast hitTest & foreground draw
			clear(m.screenXY)
			for _, pl := range g.Planets {
				sx, sy, ds := m.project(pl.X, pl.Y, r)
				m.screenXY[pl.ID] = [3]float32{sx, sy, ds}
			}

			// 6. Planet nodes & labels (lightweight GPU primitives)
			for _, pl := range g.Planets {
				xy := m.screenXY[pl.ID]
				sx, sy, ds := xy[0], xy[1], xy[2]
				if sx < r.X-40 || sx > r.X+r.W+40 || sy < r.Y-40 || sy > r.Y+r.H+40 {
					continue
				}

				col := FactionColor(pl.Owner)
				if pl.Hidden {
					col = col.Alpha(0.40)
				}

				ptRatio := float32(math.Min(1.0, math.Log1p(float64(pl.Players))/math.Log1p(12000.0)))
				baseR := float32(3.8 + math.Min(m.Zoom*1.6, 7.2))
				rad := (baseR + ptRatio*2.8) * (0.75 + 0.25*ds)
				if rad < 2.2 {
					rad = 2.2
				}
				special := pl.ID == 0 || pl.ID == 259 || pl.ID == 260
				if special {
					rad += 3.0
				}
				isSelected := selectedSet[pl.ID]
				isHovered := m.HoveredID == pl.ID
				tgt, hasTarget := targetMap[pl.ID]
				if isSelected {
					rad += 1.8
				}

				// Outer glow
				glowR := rad * 2.2
				p.FillRadialGradient(ui.Rect{X: sx - glowR, Y: sy - glowR, W: 2 * glowR, H: 2 * glowR}, ui.RadialGradient{
					Stops: []ui.GradientStop{
						{Color: col.Alpha(0.40), At: 0},
						{Color: col.Alpha(0), At: 1},
					},
				}, glowR)

				// Shadow
				p.Fill(ui.Rect{X: sx - rad, Y: sy - rad + 1.4, W: 2 * rad, H: 2 * rad}, ui.RGBA(0, 0, 0, 0.52), rad)

				if special {
					sr := rad + 4.2
					p.Stroke(ui.Rect{X: sx - sr, Y: sy - sr, W: 2 * sr, H: 2 * sr}, col.Alpha(0.85), sr, 1.8)
				}

				if pl.IsDefense {
					dr := rad + 3.8
					p.StrokeDashed(ui.Rect{X: sx - dr, Y: sy - dr, W: 2 * dr, H: 2 * dr}, RedDanger.Alpha(0.92), dr, 1.8)
				} else if pl.HasCampaign {
					cr := rad + 2.8
					p.Stroke(ui.Rect{X: sx - cr, Y: sy - cr, W: 2 * cr, H: 2 * cr}, HD2Gold.Alpha(0.82), cr, 1.2)
				}

				if hasTarget {
					tr := rad + 5.8
					ringCol := CyanActive
					if !tgt.Enabled {
						ringCol = CyanActive.Alpha(0.35)
					}
					p.StrokeDashed(ui.Rect{X: sx - tr, Y: sy - tr, W: 2 * tr, H: 2 * tr}, ringCol, tr, 1.7)
				}

				if isSelected {
					sr := rad + 7.2
					p.Stroke(ui.Rect{X: sx - sr, Y: sy - sr, W: 2 * sr, H: 2 * sr}, CyanBright, sr, 2.2)
				} else if isHovered {
					hr := rad + 5.2
					p.Stroke(ui.Rect{X: sx - hr, Y: sy - hr, W: 2 * hr, H: 2 * hr}, ui.RGBA(230, 242, 255, 0.9), hr, 1.4)
				}

				p.Fill(ui.Rect{X: sx - rad, Y: sy - rad, W: 2 * rad, H: 2 * rad}, col, rad)
				p.Stroke(ui.Rect{X: sx - rad, Y: sy - rad, W: 2 * rad, H: 2 * rad}, ui.RGBA(8, 12, 20, 0.72), rad, 1.0)

				if m.ShowLabels && (isSelected || isHovered || hasTarget || special || m.Zoom >= 1.85 || (pl.Players >= 2000 && m.Zoom >= 1.15)) {
					lbl := pl.DisplayName()
					fontSize := float32(10.5)
					lblCol := TextPrimary
					if isSelected {
						fontSize = 11.5
						lblCol = CyanBright
					} else if hasTarget {
						lblCol = CyanActive
					}
					sz, okSz := m.labelSizes[pl.ID]
					if !okSz || isSelected {
						tw, th := p.MeasureText(0, ui.Span{Text: lbl, Size: fontSize})
						sz = [2]float32{tw, th}
						if !isSelected {
							m.labelSizes[pl.ID] = sz
						}
					}
					tx := sx + rad + 5
					ty := sy - sz[1]/2
					p.Fill(ui.Rect{X: tx - 4, Y: ty - 1, W: sz[0] + 8, H: sz[1] + 2}, ui.RGBA(8, 11, 18, 0.78), 4)
					p.Text(tx, ty, lbl, fontSize, lblCol)
				}
			}

			// 7. Hover tooltip (clamped inside map rect)
			if m.HoveredID >= 0 && m.PointerInMap && !m.DragActive {
				if hp := g.PlanetsByID[m.HoveredID]; hp != nil {
					a.paintHoverTooltip(p, r, hp, targetMap)
				}
			}
		})

		// Top-left Sector Card (like galaxy-map-v2.html)
		if hoveredSector != nil {
			a.buildSectorCard(c, hoveredSector)
		}

		// Picking Defense Source banner
		if m.PickingSource {
			ui.Row(c).Absolute().Top(12).Left(260).Padding(8, 14).Gap(10).Radius(999).
				Background(ui.RGBA(239, 68, 68, 0.92)).Border(1, ui.Hex("#FCA5A5")).Children(func() {
				ui.Text(c, "请在星图上点击一颗敌方星球作为进攻来源").FontSize(12).Bold().TextColor(ui.Hex("#FFFFFF"))
				if ui.Button(c, "取消").Clicked() {
					m.PickingSource = false
				}
			})
		}

		// Top-right Map Controls
		ui.Row(c).Absolute().Top(12).Right(12).Gap(6).Padding(6, 8).Radius(10).
			Background(ui.RGBA(14, 20, 32, 0.88)).Border(1, BorderSub).Children(func() {
			tiltLbl := "2.5D 倾角"
			if !m.Tilt3D {
				tiltLbl = "2D 平面"
			}
			if ui.Button(c.Key("tilt"), tiltLbl).Clicked() {
				m.Tilt3D = !m.Tilt3D
			}
			if ui.Button(c.Key("sec"), toggleLabel("星区", m.ShowSector)).Clicked() {
				m.ShowSector = !m.ShowSector
			}
			if ui.Button(c.Key("sup"), toggleLabel("补给线", m.ShowSupply)).Clicked() {
				m.ShowSupply = !m.ShowSupply
			}
			if ui.Button(c.Key("fog"), toggleLabel("雾气", m.ShowFog)).Clicked() {
				m.ShowFog = !m.ShowFog
			}
			if ui.Button(c.Key("reset"), "重置视角").Clicked() {
				m.ResetView()
			}
			if len(a.selectedIDs) > 0 {
				if ui.Button(c.Key("focus"), "聚焦选中").Clicked() {
					if sp := g.PlanetsByID[a.selectedIDs[0]]; sp != nil {
						m.FocusPlanet(sp)
					}
				}
				if ui.Button(c.Key("clear"), fmt.Sprintf("清空(%d)", len(a.selectedIDs))).Clicked() {
					a.selectedIDs = nil
				}
			}
		})

		// Bottom-left Legend & Hint bar
		ui.Row(c).Absolute().Bottom(12).Left(12).Gap(12).Padding(6, 12).Radius(8).
			Background(ui.RGBA(10, 14, 24, 0.84)).Border(1, BorderSub).Children(func() {
			legendDot(c, ColorHumans, "超级地球")
			legendDot(c, ColorTerminids, "终结族")
			legendDot(c, ColorAutomatons, "机器人")
			legendDot(c, ColorIlluminate, "光能者")
			legendDot(c, CyanActive, "已配置变种")
			ui.Text(c, "｜ 滚轮缩放 · 拖动平移 · 单击选球 · Ctrl多选 · 双击聚焦").FontSize(11).TextColor(TextMute)
		})
	})
}

func toggleLabel(name string, on bool) string {
	if on {
		return "✓ " + name
	}
	return name
}

func legendDot(c *ui.Context, col ui.Color, label string) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(col)
		ui.Text(c, label).FontSize(11).TextColor(TextDim)
	})
}

func (a *App) buildSectorCard(c *ui.Context, sec *catalog.SectorInfo) {
	domCol := FactionColor(sec.Dominant)
	ui.Column(c).Absolute().Top(12).Left(12).Width(228).Padding(10, 12).Gap(5).Radius(10).
		Background(ui.RGBA(10, 14, 26, 0.90)).Border(1, ui.Hex("#334155")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c).Size(10, 10).Radius(5).Background(domCol)
			ui.Text(c, sec.Name+"星区").FontSize(14).Bold().TextColor(TextPrimary).Grow(1)
			ui.Textf(c, "%d 星", sec.PlanetCount).FontSize(11).TextColor(TextMute)
		})
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, "星区解放度").FontSize(11).TextColor(TextDim)
			ui.Textf(c, "%.1f%%", sec.LibPct).FontSize(12).Bold().TextColor(TextPrimary)
		})
		ui.Box(c).FillWidth().Height(4).Radius(2).Background(ui.RGBA(239, 68, 68, 0.55)).Clip().Children(func() {
			pct := float32(sec.LibPct)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			ui.Box(c).WidthPercent(pct).FillHeight().Background(ColorHumans)
		})
		if len(sec.EnemyRaces) > 0 {
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, r := range sec.EnemyRaces {
					rc := FactionColor(r)
					ui.Row(c).Gap(4).Padding(2, 7).Radius(999).
						Background(rc.Alpha(0.16)).Border(1, rc.Alpha(0.7)).Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(rc)
						ui.Text(c, catalog.FactionNameZH(r)).FontSize(10).TextColor(rc)
					})
				}
			})
		} else {
			ui.Text(c, "超级地球完全控制").FontSize(11).TextColor(ColorHumans)
		}
	})
}

func (a *App) paintHoverTooltip(p *ui.Painter, r ui.Rect, hp *catalog.Planet, targetMap map[int]config.PlanetTarget) {
	tx := r.X + mClamp(a.starMap.PointerX+14, 8, r.W-232)
	ty := r.Y + mClamp(a.starMap.PointerY-14, 8, r.H-150)
	card := ui.Rect{X: tx, Y: ty, W: 222, H: 120}
	if _, ok := targetMap[hp.ID]; ok {
		card.H = 138
	}
	p.Fill(card, ui.RGBA(10, 14, 26, 0.94), 8)
	p.Stroke(card, ui.Hex("#334155"), 8, 1)

	title := fmt.Sprintf("%s (#%d)", hp.DisplayName(), hp.ID)
	p.Text(card.X+10, card.Y+8, title, 12.5, HD2Gold)
	sub := hp.NameEN
	if hp.SectorZH != "" {
		sub += " · " + hp.SectorZH + "星区"
	}
	p.Text(card.X+10, card.Y+26, sub, 10.5, TextDim)
	p.Text(card.X+10, card.Y+45, fmt.Sprintf("归属: %s   解放: %.1f%%", catalog.FactionNameZH(hp.Owner), hp.LiberationPct()), 11, TextPrimary)
	p.Text(card.X+10, card.Y+63, fmt.Sprintf("在线: %d 人   原版效果: %d 项", hp.Players, len(hp.ActiveEffects)), 11, TextDim)

	barBg := ui.Rect{X: card.X + 10, Y: card.Y + 84, W: card.W - 20, H: 4}
	p.Fill(barBg, ui.RGBA(255, 255, 255, 0.12), 2)
	libFrac := float32(hp.LiberationPct() / 100.0)
	p.Fill(ui.Rect{X: barBg.X, Y: barBg.Y, W: barBg.W * libFrac, H: 4}, FactionColor(hp.Owner), 2)

	if tgt, ok := targetMap[hp.ID]; ok {
		state := "已配置（启用）"
		if !tgt.Enabled {
			state = "已配置（暂停）"
		}
		p.Text(card.X+10, card.Y+96, fmt.Sprintf("%s · +%d / -%d 效果", state, len(tgt.Effects), len(tgt.DisabledNativeEffects)), 11, CyanActive)
		p.Text(card.X+10, card.Y+115, "单击选中编辑 · Ctrl+单击多选", 10, TextMute)
	} else {
		p.Text(card.X+10, card.Y+96, "单击选中配置 · Ctrl+单击多选", 10, TextMute)
	}
}

func mClamp(v, lo, hi float32) float32 {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *App) handleMapSelect(pid int, mods ui.Modifiers) {
	if mods&ui.Ctrl != 0 {
		idx := slices.Index(a.selectedIDs, pid)
		if idx >= 0 {
			a.selectedIDs = slices.Delete(a.selectedIDs, idx, idx+1)
		} else {
			a.selectedIDs = append(a.selectedIDs, pid)
		}
	} else if mods&ui.Shift != 0 {
		if !slices.Contains(a.selectedIDs, pid) {
			a.selectedIDs = append(a.selectedIDs, pid)
		}
	} else {
		a.selectedIDs = []int{pid}
	}
	a.syncDraftFromSelection()
}

var _ = time.Now
