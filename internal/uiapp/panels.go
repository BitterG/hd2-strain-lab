package uiapp

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"hd2-strain-lab/internal/bridge"
	"hd2-strain-lab/internal/catalog"
	"hd2-strain-lab/internal/config"
)

var (
	planetFilterLabels = []string{"全部", "超级地球", "终结族", "机器人", "光能者", "已配置", "战役中"}
	ownerOptions       = []string{"保持原样 (0)", "超级地球 (1)", "终结族 (2)", "机器人 (3)", "光能者 (4)"}
	campTypeOptions    = []string{"常规解放 (0)", "城市/特殊 (3)", "防守战役 (4)"}
	campRaceOptions    = []string{"自动推断 (0)", "终结族 (2)", "机器人 (3)", "光能者 (4)"}
)

func (a *App) buildLeftNavigator(c *ui.Context) {
	ui.Column(c).Width(258).FillHeight().Background(BgMid).BorderWidth(0, 1, 0, 0).BorderColor(BorderSub).Padding(10).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, "星球导航").FontSize(13).Bold().TextColor(HD2Gold)
			ui.Textf(c, "%d 颗", len(a.galaxy.Planets)).FontSize(11).TextColor(TextMute)
		})

		ui.SearchField(c.Key("planet-search"), &a.planetQuery).Placeholder("名称 / 英文 / ID / 星区").FillWidth()

		// Filter pills
		ui.Row(c).Wrap().Gap(4).Children(func() {
			for i, lbl := range planetFilterLabels {
				active := a.planetFilter == i
				btn := ui.Box(c.Key(fmt.Sprintf("pf-%d", i))).Padding(3, 8).Radius(999).Cursor(ui.CursorPointer)
				if active {
					btn.Background(HD2GoldDim).Border(1, HD2Gold)
				} else {
					btn.Background(BgCard).Border(1, BorderSub)
				}
				btn.Children(func() {
					col := TextDim
					if active {
						col = HD2Gold
					}
					ui.Text(c, lbl).FontSize(11).TextColor(col)
				})
				if btn.Clicked() {
					a.planetFilter = i
				}
			}
		})

		filtered := a.filteredPlanets()
		targetMap := a.targetMap()
		selSet := make(map[int]bool, len(a.selectedIDs))
		for _, id := range a.selectedIDs {
			selSet[id] = true
		}

		ui.List(c.Key("planet-list"), &a.planetListState, len(filtered)).Grow(1).FillWidth().
			ItemKey(func(i int) any { return filtered[i].ID }).
			Rows(func(row ui.ListRow) {
				pl := filtered[row.Index]
				isSel := selSet[pl.ID]
				tgt, hasTgt := targetMap[pl.ID]

				r := ui.Row(row.Context).FillWidth().Padding(6, 8).Gap(8).AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer)
				if isSel {
					r.Background(ui.RGBA(81, 217, 199, 0.16)).Border(1, CyanActive.Alpha(0.75))
				} else if row.Hovered() {
					r.Background(BgCardHover)
				}
				r.Children(func() {
					ui.Box(row.Context).Size(8, 8).Radius(4).Background(FactionColor(pl.Owner)).Shrink(0)
					ui.Column(row.Context).Grow(1).Gap(1).Children(func() {
						ui.Row(row.Context).AlignItems(ui.Center).Gap(4).Children(func() {
							ui.Text(row.Context, pl.DisplayName()).FontSize(12).Bold().TextColor(TextPrimary)
							ui.Textf(row.Context, "#%d", pl.ID).FontSize(10).TextColor(TextMute)
						})
						sub := pl.SectorZH
						if sub != "" {
							sub += "星区"
						}
						if pl.NameEN != "" {
							if sub != "" {
								sub += " · "
							}
							sub += pl.NameEN
						}
						ui.Text(row.Context, sub).FontSize(10).TextColor(TextMute)
					})
					if hasTgt {
						badgeCol := CyanActive
						txt := "已配"
						if !tgt.Enabled {
							badgeCol = TextMute
							txt = "暂停"
						}
						ui.Box(row.Context).Padding(2, 6).Radius(999).Background(badgeCol.Alpha(0.18)).Border(1, badgeCol.Alpha(0.65)).Children(func() {
							ui.Text(row.Context, txt).FontSize(10).TextColor(badgeCol)
						})
					} else if pl.IsDefense {
						ui.Box(row.Context).Padding(2, 5).Radius(999).Background(RedDanger.Alpha(0.2)).Children(func() {
							ui.Text(row.Context, "防").FontSize(10).TextColor(RedDanger)
						})
					} else if pl.HasCampaign {
						ui.Textf(row.Context, "%.0f%%", pl.LiberationPct()).FontSize(10).TextColor(HD2Gold)
					}
				})
				if r.Clicked() {
					a.handleMapSelect(pl.ID, c.Modifiers())
					a.starMap.FocusPlanet(pl)
				}
			})
	})
}

func (a *App) buildRightConsole(c *ui.Context) {
	ui.Column(c).Width(404).FillHeight().Background(BgMid).BorderWidth(0, 0, 0, 1).BorderColor(BorderSub).Padding(12).Gap(10).Children(func() {
		ui.Segmented(c.Key("right-tabs"), &a.rightTab, "星球变种配置", "代理下发与连接").FillWidth()

		if a.rightTab == 0 {
			a.buildVariantTab(c)
		} else {
			a.buildProxyTab(c)
		}
	})
}

func (a *App) buildVariantTab(c *ui.Context) {
	// 1. Selected planets header & actions
	if len(a.selectedIDs) == 0 {
		ui.Column(c).Padding(12).Gap(6).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
			ui.Text(c, "未选中星球").FontSize(13).Bold().TextColor(HD2Gold)
			ui.Text(c, "在左侧列表或中央星图上单击星球即可开始编辑变种与战役配置；按住 Ctrl 单击可多选并批量下发。").FontSize(11.5).TextColor(TextDim)
		})
		a.buildConfiguredList(c)
		return
	}

	// Summary header of selected planet(s)
	ui.Column(c).Padding(10).Gap(6).Radius(8).Background(BgCard).Border(1, CyanActive.Alpha(0.45)).Children(func() {
		if len(a.selectedIDs) == 1 {
			pid := a.selectedIDs[0]
			pl := a.galaxy.PlanetsByID[pid]
			name := fmt.Sprintf("星球 #%d", pid)
			sub := ""
			owner := 0
			lib := 0.0
			if pl != nil {
				name = fmt.Sprintf("%s (#%d)", pl.DisplayName(), pid)
				sub = fmt.Sprintf("%s · %s星区", pl.NameEN, pl.SectorZH)
				owner = pl.Owner
				lib = pl.LiberationPct()
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, name).FontSize(14).Bold().TextColor(CyanBright)
					if sub != "" {
						ui.Text(c, sub).FontSize(11).TextColor(TextDim)
					}
				})
				if owner > 0 {
					fc := FactionColor(owner)
					lbl := fmt.Sprintf("%s · %.1f%%", catalog.FactionNameZH(owner), lib)
					if pl != nil && pl.IsDefense {
						fc = RedDanger
						lbl = fmt.Sprintf("防守战 · %.1f%%", lib)
					}
					ui.Box(c).Padding(3, 8).Radius(999).Background(fc.Alpha(0.18)).Border(1, fc.Alpha(0.7)).Children(func() {
						ui.Text(c, lbl).FontSize(10.5).TextColor(fc)
					})
				}
			})
		} else {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Textf(c, "已批量选中 %d 颗星球", len(a.selectedIDs)).FontSize(14).Bold().TextColor(CyanBright)
				if ui.Button(c.Key("clr-sel"), "取消多选").Clicked() {
					a.selectedIDs = a.selectedIDs[:1]
					a.syncDraftFromSelection()
				}
			})
			var names []string
			for _, id := range a.selectedIDs {
				if pl := a.galaxy.PlanetsByID[id]; pl != nil {
					names = append(names, pl.DisplayName())
				} else {
					names = append(names, fmt.Sprintf("#%d", id))
				}
				if len(names) >= 5 {
					break
				}
			}
			more := ""
			if len(a.selectedIDs) > 5 {
				more = fmt.Sprintf(" 等 %d 颗", len(a.selectedIDs))
			}
			ui.Text(c, strings.Join(names, "、")+more).FontSize(11).TextColor(TextDim)
		}

		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("apply-sel"), "保存并下发到代理").Grow(1).Clicked() {
				if err := a.applyDraftToSelection(c); err != nil {
					c.Toast(err.Error())
				} else {
					c.Toast("已保存并热重载下发到代理")
				}
			}
			if a.anySelectedConfigured() {
				if ui.Button(c.Key("del-sel"), "移除方案").Clicked() {
					a.removeSelectionTargets()
					c.Toast("已移除选中星球的变种方案")
				}
			}
		})
	})

	// Native Effects & Added Custom Effects Inspector
	nativeEffects := a.selectedNativeEffects()
	disSet := make(map[int]bool, len(a.draft.DisabledNativeEffects))
	for _, eid := range a.draft.DisabledNativeEffects {
		disSet[eid] = true
	}

	ui.Column(c).Padding(9).Gap(6).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
		// 1) Native Effects on the planet
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Textf(c, "原版自带效果 (%d)", len(nativeEffects)).FontSize(12).Bold().TextColor(ColorHumans)
			if len(nativeEffects) > 0 {
				ui.Row(c).Gap(4).Children(func() {
					if len(a.draft.DisabledNativeEffects) < len(nativeEffects) {
						if ui.Button(c.Key("dis-all-nat"), "全部取消").Clicked() {
							a.disableAllNativeEffects(nativeEffects)
						}
					}
					if len(a.draft.DisabledNativeEffects) > 0 {
						if ui.Button(c.Key("rst-all-nat"), "恢复全部").Clicked() {
							a.restoreAllNativeEffects()
						}
					}
				})
			}
		})
		if len(nativeEffects) == 0 {
			ui.Text(c, "该星球在当前战况中没有原版自带效果").FontSize(11).TextColor(TextMute)
		} else {
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, eid := range nativeEffects {
					isDisabled := disSet[eid]
					chipCol := ColorHumans
					label := fmt.Sprintf("%s #%d", a.effectDisplayName(eid), eid)
					action := "取消"
					if isDisabled {
						chipCol = RedDanger
						label = "[已取消] " + label
						action = "恢复"
					}
					chip := ui.Row(c.Key(fmt.Sprintf("nat-%d", eid))).Padding(3, 7).Gap(5).AlignItems(ui.Center).
						Radius(999).Background(chipCol.Alpha(0.14)).Border(1, chipCol.Alpha(0.55)).Cursor(ui.CursorPointer)
					chip.Children(func() {
						ui.Text(c, label).FontSize(10.5).TextColor(chipCol)
						ui.Text(c, action).FontSize(10).Bold().TextColor(TextPrimary)
					})
					if chip.Clicked() {
						a.toggleDisableNativeEffect(eid)
					}
				}
			})
		}

		ui.Divider(c)

		// 2) Added Custom Effects on the planet
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Textf(c, "已添加自定义效果 (%d)", len(a.draft.Effects)).FontSize(12).Bold().TextColor(CyanActive)
			if len(a.draft.Effects) > 0 {
				if ui.Button(c.Key("clr-eff-top"), "清空已添加").Clicked() {
					a.draft.Effects = nil
				}
			}
		})
		if len(a.draft.Effects) == 0 {
			ui.Text(c, "尚未添加自定义效果，请在下方目录点击勾选").FontSize(11).TextColor(TextMute)
		} else {
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, eid := range a.draft.Effects {
					chip := ui.Row(c.Key(fmt.Sprintf("add-%d", eid))).Padding(3, 7).Gap(5).AlignItems(ui.Center).
						Radius(999).Background(CyanActive.Alpha(0.15)).Border(1, CyanActive.Alpha(0.70)).Cursor(ui.CursorPointer)
					chip.Children(func() {
						ui.Textf(c, "%s #%d", a.effectDisplayName(eid), eid).FontSize(10.5).TextColor(CyanBright)
						ui.Text(c, "×").FontSize(11).Bold().TextColor(TextPrimary)
					})
					if chip.Clicked() {
						a.toggleDraftEffect(eid)
					}
				}
			})
		}
	})

	// Campaign & Owner Settings (collapsible card)
	ui.Column(c).Padding(9).Gap(7).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			hdrLbl := "▶ 战役与归属参数（展开）"
			if a.showCampaignCfg {
				hdrLbl = "▼ 战役与归属参数"
			}
			hdrBtn := ui.Box(c.Key("toggle-camp-cfg")).Padding(2, 4).Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, hdrLbl).FontSize(12).Bold().TextColor(HD2Gold)
			})
			if hdrBtn.Clicked() {
				a.showCampaignCfg = !a.showCampaignCfg
			}
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, "启用").FontSize(11.5).TextColor(TextDim)
				ui.Switch(c.Key("sw-en"), &a.draft.Enabled)
			})
		})
		if !a.showCampaignCfg {
			return
		}

		ui.Row(c).Wrap().Gap(10).Children(func() {
			ui.Checkbox(c.Key("chk-camp"), &a.draft.Campaign, "开启战役")
			ui.Checkbox(c.Key("chk-force"), &a.draft.ForceOpen, "强制开放(0血量)")
			ui.Checkbox(c.Key("chk-openreg"), &a.draft.OpenRegions, "开放分区")
			ui.Checkbox(c.Key("chk-initreg"), &a.draft.InitializeRegions, "初始化分区")
		})

		ui.Row(c).Gap(6).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "星球归属").FontSize(10.5).TextColor(TextMute)
				if ui.Select(c.Key("sel-owner"), &a.draftOwnerStr, ownerOptions).FillWidth().Changed() {
					a.draft.Owner = parseLeadingOptionInt(a.draftOwnerStr, 0)
				}
			})
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "战役类型").FontSize(10.5).TextColor(TextMute)
				if ui.Select(c.Key("sel-ctype"), &a.draftTypeStr, campTypeOptions).FillWidth().Changed() {
					a.draft.CampaignType = parseLeadingOptionInt(a.draftTypeStr, 0)
					if a.draft.CampaignType == 4 {
						a.draft.Campaign = true
						a.draft.Owner = 1
						a.draftOwnerStr = ownerOptions[1]
						if a.draft.CampaignRace < 2 || a.draft.CampaignRace > 4 {
							a.draft.CampaignRace = 2
							a.draftRaceStr = campRaceOptions[1]
						}
					}
				}
			})
		})

		ui.Row(c).Gap(6).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "作战敌人").FontSize(10.5).TextColor(TextMute)
				if ui.Select(c.Key("sel-race"), &a.draftRaceStr, campRaceOptions).FillWidth().Changed() {
					a.draft.CampaignRace = parseLeadingOptionInt(a.draftRaceStr, 0)
				}
			})
			ui.Column(c).Width(115).Gap(2).Children(func() {
				ui.Text(c, "作战槽数量").FontSize(10.5).TextColor(TextMute)
				if ui.NumberInput(c.Key("num-cnt"), &a.draftCountF, 1, 200, 1).FillWidth().Changed() {
					a.draft.CampaignCount = int(a.draftCountF)
				}
			})
		})

		if a.draft.CampaignType == 4 {
			ui.Column(c).Padding(8).Gap(6).Radius(6).Background(ui.RGBA(239, 68, 68, 0.10)).Border(1, RedDanger.Alpha(0.4)).Children(func() {
				srcText := "未选择进攻来源"
				if a.draft.DefenseSource >= 0 {
					if sp := a.galaxy.PlanetsByID[a.draft.DefenseSource]; sp != nil {
						srcText = fmt.Sprintf("%s (#%d)", sp.DisplayName(), sp.ID)
					} else {
						srcText = fmt.Sprintf("星球 #%d", a.draft.DefenseSource)
					}
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "进攻来源: "+srcText).FontSize(11.5).TextColor(TextPrimary)
					lbl := "从星图拾取"
					if a.starMap.PickingSource {
						lbl = "拾取中..."
					}
					if ui.Button(c.Key("pick-src"), lbl).Clicked() {
						a.starMap.PickingSource = !a.starMap.PickingSource
					}
				})
				ui.Row(c).Gap(6).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "入侵等级").FontSize(10).TextColor(TextMute)
						if ui.NumberInput(c.Key("def-lvl"), &a.draftDefLevelF, 1, 100, 1).FillWidth().Changed() {
							a.draft.DefenseLevel = int(a.draftDefLevelF)
						}
					})
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "时长(小时)").FontSize(10).TextColor(TextMute)
						if ui.NumberInput(c.Key("def-hrs"), &a.draftDefHoursF, 1, 168, 1).FillWidth().Changed() {
							a.draft.DefenseHours = int(a.draftDefHoursF)
						}
					})
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "防守作战数").FontSize(10).TextColor(TextMute)
						if ui.NumberInput(c.Key("def-cnt"), &a.draftDefCountF, 1, 200, 1).FillWidth().Changed() {
							a.draft.DefenseCount = int(a.draftDefCountF)
						}
					})
				})
			})
		}
	})

	// Effect Catalog Picker
	ui.Column(c).Grow(1).Padding(10).Gap(6).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Textf(c, "星系效果与变种 (%d 已选)", len(a.draft.Effects)).FontSize(12.5).Bold().TextColor(HD2Gold)
			if len(a.draft.Effects) > 0 {
				if ui.Button(c.Key("clr-eff"), "清空效果").Clicked() {
					a.draft.Effects = nil
				}
			}
		})

		ui.SearchField(c.Key("eff-search"), &a.effectQuery).Placeholder("搜索效果中文 / 英文 / ID").FillWidth()

		// Group filter chips
		groups := []string{"全部", "仅已选", "敌情与变种", "作战增减益", "临时配发战备", "弹药与补给", "星球环境与通路", "空间站与战术行动", "设施与星球标记"}
		ui.Row(c).Wrap().Gap(4).Children(func() {
			for _, g := range groups {
				active := a.effectGroup == g
				chip := ui.Box(c.Key("eg-" + g)).Padding(2, 7).Radius(999).Cursor(ui.CursorPointer)
				if active {
					chip.Background(HD2GoldDim).Border(1, HD2Gold)
				} else {
					chip.Background(BgMid).Border(1, BorderSub)
				}
				chip.Children(func() {
					col := TextDim
					if active {
						col = HD2Gold
					}
					ui.Text(c, g).FontSize(10.5).TextColor(col)
				})
				if chip.Clicked() {
					a.effectGroup = g
				}
			}
		})

		effList := a.filteredEffects()
		selEff := make(map[int]bool, len(a.draft.Effects))
		for _, eid := range a.draft.Effects {
			selEff[eid] = true
		}

		ui.List(c.Key("eff-list"), &a.effectListState, len(effList)).Grow(1).FillWidth().
			ItemKey(func(i int) any { return effList[i].ID }).
			Rows(func(row ui.ListRow) {
				eff := effList[row.Index]
				checked := selEff[eff.ID]
				r := ui.Row(row.Context).FillWidth().Padding(5, 8).Gap(8).AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer)
				if checked {
					r.Background(ui.RGBA(81, 217, 199, 0.14)).Border(1, CyanActive.Alpha(0.65))
				} else if row.Hovered() {
					r.Background(BgCardHover)
				}
				r.Children(func() {
					box := ui.Box(row.Context).Size(15, 15).Radius(4).Center().Shrink(0)
					if checked {
						box.Background(CyanActive)
						box.Children(func() {
							ui.Text(row.Context, "✓").FontSize(10.5).Bold().TextColor(BgDeep)
						})
					} else {
						box.Border(1, TextMute)
					}
					ui.Column(row.Context).Grow(1).Gap(1).Children(func() {
						ui.Text(row.Context, eff.Name).FontSize(11.5).TextColor(TextPrimary)
						meta := fmt.Sprintf("#%d", eff.ID)
						if eff.GroupLabel != "" {
							meta += " · " + eff.GroupLabel
						}
						if eff.EnglishName != "" && !strings.HasPrefix(eff.EnglishName, "Unresolved") {
							meta += " · " + eff.EnglishName
						}
						ui.Text(row.Context, meta).FontSize(10).TextColor(TextMute)
					})
				})
				if r.Clicked() {
					a.toggleDraftEffect(eff.ID)
				}
			})
	})

	// Compact footer showing total configured planets
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(2, 4).Children(func() {
		ui.Textf(c, "总方案: %d 颗星球", len(a.plan.Targets)).FontSize(11).TextColor(TextDim)
		if len(a.plan.Targets) > 0 {
			if ui.Button(c.Key("view-all-cfg"), "查看全部已配置").Clicked() {
				a.selectedIDs = nil
			}
		}
	})
}

func (a *App) buildConfiguredList(c *ui.Context) {
	ui.Column(c).Grow(1).Padding(10).Gap(6).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Textf(c, "已配置星球方案 (%d)", len(a.plan.Targets)).FontSize(12.5).Bold().TextColor(HD2Gold)
			if len(a.plan.Targets) > 0 {
				if ui.Button(c.Key("save-all-cfg"), "全部下发").Clicked() {
					if err := a.saveAndDispatch(); err != nil {
						c.Toast(err.Error())
					} else {
						c.Toast("已热重载下发到代理")
					}
				}
			}
		})
		if len(a.plan.Targets) == 0 {
			ui.Text(c, "暂无已配置星球。请在星图上点击任意星球并勾选效果后点击「保存并下发到代理」。").FontSize(11.5).TextColor(TextMute)
			return
		}
		ui.Scroll(c.Key("cfg-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				targets := slices.Clone(a.plan.Targets)
				for _, t := range targets {
					pl := a.galaxy.PlanetsByID[t.Planet]
					name := fmt.Sprintf("星球 #%d", t.Planet)
					if pl != nil {
						name = fmt.Sprintf("%s (#%d)", pl.DisplayName(), t.Planet)
					}
					card := ui.Row(c.Key(fmt.Sprintf("tgt-%d", t.Planet))).Padding(8).Gap(8).AlignItems(ui.Center).Radius(6).
						Background(BgMid).Border(1, BorderSub).Cursor(ui.CursorPointer)
					card.Children(func() {
						en := t.Enabled
						if ui.Switch(c.Key(fmt.Sprintf("tsw-%d", t.Planet)), &en).Changed() {
							a.setTargetEnabled(t.Planet, en)
						}
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, name).FontSize(12).Bold().TextColor(TextPrimary)
							ui.Textf(c, "效果: +%d / -%d · 战役=%v · 归属=%d", len(t.Effects), len(t.DisabledNativeEffects), t.Campaign, t.Owner).FontSize(10.5).TextColor(TextMute)
						})
						if ui.Button(c.Key(fmt.Sprintf("tgo-%d", t.Planet)), "定位").Clicked() {
							a.selectedIDs = []int{t.Planet}
							a.syncDraftFromSelection()
							if pl != nil {
								a.starMap.FocusPlanet(pl)
							}
						}
						if ui.Button(c.Key(fmt.Sprintf("tdel-%d", t.Planet)), "删").Clicked() {
							a.deleteTarget(t.Planet)
						}
					})
				}
			})
		})
	})
}

func (a *App) buildProxyTab(c *ui.Context) {
	st := a.proxySrv.Stats()
	trusted := a.certsMgr.IsTrusted()

	ui.Scroll(c.Key("proxy-scroll")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			// 1. Proxy Start/Stop & Dispatch Card
			ui.Column(c).Padding(10).Gap(8).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "代理服务与配置下发").FontSize(13).Bold().TextColor(HD2Gold)
					if st.Running {
						ui.Box(c).Padding(2, 8).Radius(999).Background(GreenOK.Alpha(0.18)).Border(1, GreenOK).Children(func() {
							ui.Textf(c, "运行中 :%d", st.Port).FontSize(10.5).TextColor(GreenOK)
						})
					} else {
						ui.Box(c).Padding(2, 8).Radius(999).Background(TextMute.Alpha(0.18)).Children(func() {
							ui.Text(c, "已停止").FontSize(10.5).TextColor(TextDim)
						})
					}
				})

				ui.Text(c, st.LastMessage).FontSize(11.5).TextColor(TextPrimary)

				ui.Row(c).Gap(8).Children(func() {
					if st.Running {
						if ui.Button(c.Key("stop-px"), "停止代理").Grow(1).Clicked() {
							a.stopProxy()
							c.Toast("代理已停止")
						}
						if ui.PrimaryButton(c.Key("re-push"), "热重载下发配置").Grow(1).Clicked() {
							if err := a.saveAndDispatch(); err != nil {
								c.Toast(err.Error())
							} else {
								c.Toast("已热重载方案到运行中的代理")
							}
						}
					} else {
						if ui.PrimaryButton(c.Key("start-px"), "启动代理服务").Grow(1).Clicked() {
							if !trusted {
								c.Toast("提示：尚未信任本机 CA 证书，请先点击下方「安装证书信任」")
							}
							if err := a.startProxy(); err != nil {
								c.Toast("启动代理失败: " + err.Error())
							} else {
								c.Toast("代理已启动并写入 Steam 桥接配置")
							}
						}
					}
				})

				ui.Row(c).Gap(12).Children(func() {
					ui.Textf(c, "战况命中: %d", st.StatusHits).FontSize(11).TextColor(TextDim)
					ui.Textf(c, "改写下发: %d", st.Rewrites).FontSize(11).TextColor(CyanActive)
					ui.Textf(c, "目录数: %d", st.CatalogCount).FontSize(11).TextColor(TextDim)
				})
			})

			// 2. Certificate Card
			ui.Column(c).Padding(10).Gap(7).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "1. 本机 TLS 证书 (当前用户信任)").FontSize(12.5).Bold().TextColor(HD2Gold)
					if trusted {
						ui.Text(c, "已信任").FontSize(11).Bold().TextColor(GreenOK)
					} else {
						ui.Text(c, "未信任").FontSize(11).Bold().TextColor(ColorTerminids)
					}
				})
				ui.Textf(c, "仅解密 %s，写入 CurrentUser\\ROOT，不修改整机系统策略或 hosts。", "api.live.prod.thehelldiversgame.com").FontSize(11).TextColor(TextDim)
				thumb := a.certsMgr.Thumbprint()
				if len(thumb) > 16 {
					ui.Textf(c, "指纹: %s...%s", thumb[:10], thumb[len(thumb)-8:]).FontSize(10.5).TextColor(TextMute)
				}
				ui.Row(c).Gap(6).Children(func() {
					if !trusted {
						if ui.PrimaryButton(c.Key("inst-ca"), "安装证书信任").Grow(1).Clicked() {
							if err := a.certsMgr.InstallTrust(); err != nil {
								c.Toast("安装失败: " + err.Error())
							} else {
								c.Toast("已安装 CA 证书到当前用户受信任根证书库")
							}
						}
					} else {
						if ui.Button(c.Key("rem-ca"), "移除证书信任").Grow(1).Clicked() {
							if err := a.certsMgr.RemoveTrust(); err != nil {
								c.Toast("移除失败: " + err.Error())
							} else {
								c.Toast("已从当前用户证书库移除 CA")
							}
						}
					}
					if ui.Button(c.Key("regen-ca"), "重建 CA").Clicked() {
						if st.Running {
							c.Toast("请先停止代理再重建 CA")
						} else if err := a.certsMgr.GenerateCA(); err != nil {
							c.Toast(err.Error())
						} else {
							c.Toast("已生成新 CA（需重新安装信任）")
						}
					}
				})
			})

			// 3. Steam Launch Bridge Card
			ui.Column(c).Padding(10).Gap(7).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
				ui.Text(c, "2. Steam 启动选项桥接").FontSize(12.5).Bold().TextColor(HD2Gold)
				ui.Text(c, "将下方命令粘贴到 Steam → 绝地潜兵 2 属性 → 通用 → 启动选项。代理关闭时正常直连游戏。").FontSize(11).TextColor(TextDim)

				exePath, _ := os.Executable()
				launchOpt := bridge.SteamLaunchOption(exePath)
				ui.Box(c).Padding(7, 9).Radius(6).Background(BgDeep).Border(1, BorderSub).Children(func() {
					ui.Text(c, launchOpt).FontSize(10.5).TextColor(CyanBright)
				})
				if ui.Button(c.Key("copy-steam"), "复制 Steam 启动选项").FillWidth().Clicked() {
					c.WriteClipboardThen(launchOpt, func(err error) {
						if err != nil {
							c.Toast("复制失败: " + err.Error())
						} else {
							c.Toast("已复制 Steam 启动选项到剪贴板")
						}
					})
				}
			})

			// 4. Upstream Proxy & Policy Settings
			ui.Column(c).Padding(10).Gap(7).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
				ui.Text(c, "3. 加速器兼容与下发选项").FontSize(12.5).Bold().TextColor(HD2Gold)

				changed := false
				if ui.Checkbox(c.Key("chk-up"), &a.settings.UseUpstreamProxy, "经由上游 HTTP 代理访问游戏 API（配合加速器）").Changed() {
					changed = true
				}
				if a.settings.UseUpstreamProxy {
					if ui.TextInput(c.Key("in-up"), &a.settings.UpstreamProxyURL).Placeholder("例如 http://127.0.0.1:7890").FillWidth().Changed() {
						changed = true
					}
				}
				if ui.Checkbox(c.Key("chk-all-eff"), &a.settings.AllowAllEffects, "允许下发未出现在当前目录的效果（离线/实验模式）").Changed() {
					changed = true
				}
				if changed {
					_ = config.SaveSettings(a.profileDir, a.settings)
					a.proxySrv.UpdateSettings(a.settings)
				}

				ui.Row(c).Gap(6).Children(func() {
					if ui.Button(c.Key("imp-plan"), "导入方案 JSON").Grow(1).Clicked() {
						go a.importPlanDialog()
					}
					if ui.Button(c.Key("exp-plan"), "导出方案 JSON").Grow(1).Clicked() {
						go a.exportPlanDialog()
					}
				})
			})

			// 5. Audit Logs
			ui.Column(c).Padding(10).Gap(6).Radius(8).Background(BgCard).Border(1, BorderSub).Children(func() {
				ui.Text(c, "代理审计日志").FontSize(12).Bold().TextColor(TextDim)
				ui.Box(c).Height(130).FillWidth().Padding(8).Radius(6).Background(BgDeep).Border(1, BorderSub).Children(func() {
					ui.Scroll(c.Key("log-scroll")).Fill().Children(func() {
						ui.Column(c).Gap(2).Children(func() {
							if len(st.Logs) == 0 {
								ui.Text(c, "暂无日志").FontSize(10.5).TextColor(TextMute)
							} else {
								for i := len(st.Logs) - 1; i >= 0; i-- {
									ui.Text(c, st.Logs[i]).FontSize(10.5).TextColor(TextDim)
								}
							}
						})
					})
				})
			})
		})
	})
}

func (a *App) importPlanDialog() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  a.win,
		Title:   "导入星球变种方案 JSON",
		Filters: []mygo.FileFilter{{Name: "JSON 文件", Extensions: []string{"json"}}},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		return
	}
	a.win.Update(func() {
		if err := a.importPlanBytes(b); err != nil {
			a.toastMsg = "导入失败: " + err.Error()
		} else {
			a.toastMsg = fmt.Sprintf("已导入 %d 颗星球方案并下发", len(a.plan.Targets))
		}
	})
}

func (a *App) exportPlanDialog() {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出星球变种方案 JSON",
		DefaultPath: "hd2-cpv-plan.json",
		Filters:     []mygo.FileFilter{{Name: "JSON 文件", Extensions: []string{"json"}}},
	})
	if err != nil || path == "" {
		return
	}
	_ = a.exportPlanFile(path)
}

func parseLeadingOptionInt(s string, fallback int) int {
	l := strings.LastIndex(s, "(")
	r := strings.LastIndex(s, ")")
	if l >= 0 && r > l+1 {
		if v, err := strconv.Atoi(s[l+1 : r]); err == nil {
			return v
		}
	}
	return fallback
}

func findOptionByInt(options []string, v int) string {
	suffix := fmt.Sprintf("(%d)", v)
	for _, opt := range options {
		if strings.HasSuffix(opt, suffix) {
			return opt
		}
	}
	return options[0]
}

func (a *App) assignDefenseSource(pid int) {
	if slices.Contains(a.selectedIDs, pid) {
		return
	}
	a.draft.DefenseSource = pid
	if pl := a.galaxy.PlanetsByID[pid]; pl != nil && pl.Owner >= 2 && pl.Owner <= 4 {
		a.draft.CampaignRace = pl.Owner
		a.draftRaceStr = findOptionByInt(campRaceOptions, pl.Owner)
	}
}
