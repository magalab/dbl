package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) view(c *ui.Context) {
	app.applyInterfaceTheme(c)
	defer app.scheduleQueryDraftSave()
	if app.status != "" && app.status != app.announcedStatus {
		c.Announce(app.localized(app.status))
		app.announcedStatus = app.status
	}
	windowWidth, _ := c.Size()
	app.aiFullPage = app.showAI && windowWidth < aiDockBreakpoint
	ui.Column(c).Fill().Children(func() {
		app.toolbar(c)
		showWorkspaceMessage := app.activeQuery != nil && !app.activeQuery.dataView && !app.activeQuery.schemaView && !app.activeQuery.connectionView && !app.aiFullPage && app.documentStore == nil && app.keyStore == nil && app.etcdStore == nil
		mainArea := func() {
			if !app.hasActiveConnection() && !app.showAI {
				ui.Column(c).Fill().Children(func() {
					if app.queryWorkspaceRequested {
						app.workspace(c)
					} else {
						app.connectionWelcome(c)
					}
				})
				return
			}
			if windowWidth >= aiDockBreakpoint {
				if app.showAI {
					ui.Column(c).Fill().Direction(ui.RightToLeft).Children(func() {
						ui.Split(c, &app.aiPanelWidth, func() {
							ui.Column(c).Fill().Direction(ui.LeftToRight).MinWidth(320).Padding(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
								app.aiWorkspace(c)
							})
						}, func() {
							ui.Column(c).Fill().Direction(ui.LeftToRight).Children(func() {
								app.workspace(c)
							})
						}).Grow(1)
					})
				} else {
					ui.Column(c).Fill().Children(func() { app.workspace(c) })
				}
			} else {
				ui.Column(c).Fill().Children(func() {
					app.workspace(c)
				})
			}
		}
		rightPane := func() {
			ui.Column(c).Fill().Children(func() {
				ui.Box(c).Grow(1).MinHeight(0).Children(mainArea)
				ui.Row(c).Padding(8, 12).Children(func() {
					if !showWorkspaceMessage {
						ui.Text(c, app.localized(app.status)).Grow(1).SingleLine().Role(ui.RoleStatus)
					} else {
						ui.Spacer(c)
					}
					ui.Text(c, app.localized("DBL · 原生 SQL 工作区")).SingleLine()
				})
			})
		}
		if app.leftCollapsed {
			rightPane()
		} else {
			ui.Split(c, &app.leftWidth, func() {
				app.connections(c)
			}, rightPane).Grow(1)
		}
		app.scheduleLayoutSave()
	})
	connectionEditor := ui.DialogBase(c, &app.showConnectionEditor, func(backdrop, panel ui.Element) {
		theme := c.Theme()
		width, height := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.Width(620).Padding(theme.Space(5)).Gap(theme.Space(3)).Radius(theme.Space(2.5)).
			Background(theme.Background.Mix(theme.Surface, 0.45)).Border(1, theme.Border).
			MaxWidth(width-theme.Space(10)).MaxHeight(height-theme.Space(10)).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
		ui.Column(c).Width(580).MaxWidthPercent(100).Padding(16).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Cursor(ui.CursorGrab).Children(func() {
				title := "新建连接"
				if app.selectedProfileID != "" {
					title = "编辑连接"
				}
				ui.Text(c, app.localized(title)).Bold().Role(ui.RoleHeading).Level(1).Grow(1)
				if iconButton(c, app.localized("关闭"), actionIcons["close"]).Size(32, 32).Padding(5).Clicked() {
					app.showConnectionEditor = false
					app.discardConnectionEditorSecrets()
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(4, 20, 4, 4).Gap(8).Children(func() {
					app.connectionProfileForm(c)
				})
			})
			app.connectionProfileFormActions(c)
		})
	})
	if app.showConnectionEditor {
		width, height := c.Size()
		modalBounds := connectionEditor.Bounds()
		halfWidth := max(float32(0), (width-modalBounds.W)/2-5)
		halfHeight := max(float32(0), (height-modalBounds.H)/2-5)
		bounds := ui.Rect{X: -halfWidth, Y: -halfHeight, W: halfWidth * 2, H: halfHeight * 2}
		connectionEditor.Draggable(&app.connectionEditorOffset, ui.DraggableOptions{Bounds: &bounds, Rigid: true})
	} else {
		app.connectionEditorOffset = ui.Point{}
	}
	if connectionEditor.Dismissed() {
		app.discardConnectionEditorSecrets()
	}
	if ui.Modal(c, &app.showRedisAliasEditor, func() {
		ui.Column(c).Width(420).Padding(16).Gap(12).Children(func() {
			ui.Text(c, app.localized("设置数据库别名")).Bold().Role(ui.RoleHeading).Level(2)
			alias := ui.TextInput(c, &app.redisAliasDraft).Placeholder(app.localized("留空则显示默认 db 编号")).Label(app.localized("数据库别名"))
			alias.Disabled(app.busy || app.dialogOpen)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, app.localized("取消")).Clicked() {
					app.showRedisAliasEditor = false
				}
				save := ui.PrimaryButton(c, app.localized("保存"))
				save.Disabled(app.busy || app.dialogOpen)
				if save.Clicked() {
					app.saveRedisDatabaseAlias()
				}
			})
		})
	}).Dismissed() {
		app.showRedisAliasEditor = false
	}
	app.redisKeyEditorModal(c)
	app.etcdCreateKeyModal(c)
	app.etcdGrantLeaseModal(c)
	app.confirmationModal(c)
}

func (app *workbench) currentLayout() preferences.Layout {
	return preferences.Layout{LeftWidth: app.leftWidth, InspectorWidth: app.aiPanelWidth, ResultHeight: app.resultHeight, QueryEditorHeight: app.queryEditorHeight, LeftCollapsed: app.leftCollapsed, RightCollapsed: app.rightCollapsed}
}

func (app *workbench) scheduleLayoutSave() {
	if app.preferences == nil || app.busy || app.exporting || app.dialogOpen {
		return
	}
	layout := app.currentLayout()
	if layout == app.observedLayout {
		return
	}
	app.observedLayout = layout
	if app.layoutSaveTimer != nil {
		app.layoutSaveTimer.Stop()
	}
	store := app.preferences
	app.layoutSaveTimer = time.AfterFunc(350*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			app.layoutSaveTimer = nil
			if app.preferences != store {
				return
			}
			if app.layoutSaving {
				app.observedLayout = app.savedLayout
				return
			}
			if app.currentLayout() != layout {
				app.observedLayout = app.savedLayout
				app.scheduleLayoutSave()
				return
			}
			if app.busy || app.exporting || app.dialogOpen {
				app.observedLayout = app.savedLayout
				return
			}
			app.layoutSaving = true
			app.preferenceRevision++
			revision := app.preferenceRevision
			go func() {
				err := store.SetLayout(layout, revision)
				if err != nil {
					app.window.Update(func() {
						app.layoutSaving = false
						if app.preferences != store {
							return
						}
						if app.currentLayout() == layout {
							app.observedLayout = layout
						} else {
							app.observedLayout = app.savedLayout
							app.scheduleLayoutSave()
						}
						app.reportError("保存窗口布局失败", err)
					})
					return
				}
				mygo.RunOnMain(func() {
					app.layoutSaving = false
					if app.preferences != store {
						return
					}
					app.savedLayout = layout
					app.observedLayout = layout
					if app.currentLayout() != layout {
						app.scheduleLayoutSave()
					}
				})
			}()
		})
	})
}

func (app *workbench) reportError(summary string, err error) {
	summary = app.localized(summary)
	app.status = summary
	app.errorSummary = summary
	app.errorDetails = ""
	if err != nil {
		app.errorDetails = safeerror.Display(err)
	}
	app.showErrorDetails = false
}

var actionIcons = map[string]*ui.SVG{
	"add":          ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`)),
	"filePlus":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h6"/><path d="M14 2v6h6M14 2l6 6m-1 9h-6m3-3v6"/></svg>`)),
	"panelLeft":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/></svg>`)),
	"panelRight":   ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/></svg>`)),
	"query":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 3h7l5 5v13H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z"/><path d="M14 3v5h5M8 13h8m-8 4h8"/></svg>`)),
	"play":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M7 4.8c0-.8.9-1.3 1.6-.9l11 7.1a1.2 1.2 0 0 1 0 2L8.6 20.1c-.7.5-1.6 0-1.6-.9V4.8Z"/></svg>`)),
	"format":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 6h16M4 10h10M4 14h16M4 18h10"/></svg>`)),
	"stop":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1.5"/></svg>`)),
	"nextBatch":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12m-5-5 5 5 5-5M5 20h14"/></svg>`)),
	"sparkle":      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 1.9 5.8L20 11l-6.1 2.2L12 19l-1.9-5.8L4 11l6.1-2.2L12 3Z"/><path d="m19 14 .9 2.1L22 17l-2.1.9L19 20l-.9-2.1L16 17l2.1-.9L19 14Z"/></svg>`)),
	"folder":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/><path d="M3 10h18"/></svg>`)),
	"open":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3h7v7M10 14 21 3"/><path d="M19 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h6"/></svg>`)),
	"history":      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 2.6-6.4L3 8"/><path d="M3 3v5h5m4-1v5l3 2"/></svg>`)),
	"search":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="10.8" cy="10.8" r="6.8"/><path d="m16 16 5 5"/></svg>`)),
	"filter":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5h16l-6.5 7.5v5.2l-3 1.8v-7L4 5Z"/></svg>`)),
	"refresh":      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M5.6 9a7 7 0 0 1 11.6-2L20 12M4 12l2.8 5a7 7 0 0 0 11.6-2"/></svg>`)),
	"save":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3h12l4 4v14H3V3h2Z"/><path d="M7 3v6h10V3M7 21v-7h10v7"/></svg>`)),
	"saveConnect":  ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 3h13l4 4v14H3V4a1 1 0 0 1 1-1Z"/><path d="M7 3v6h10V3M7 21v-7h8"/><path d="M14 17h6m-2-2 2 2-2 2"/></svg>`)),
	"close":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="m6 6 12 12M18 6 6 18"/></svg>`)),
	"eye":          ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6S2 12 2 12Z"/><circle cx="12" cy="12" r="2.5"/></svg>`)),
	"eyeOff":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m3 3 18 18M10.6 6.2A10 10 0 0 1 12 6c6.4 0 10 6 10 6a15 15 0 0 1-3.1 3.7M6.2 6.2C3.5 7.8 2 12 2 12s3.6 6 10 6a10 10 0 0 0 3.5-.6"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/></svg>`)),
	"plug":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5m6-5v5M7 8h10v3a5 5 0 0 1-5 5v5m0-5a5 5 0 0 1-5-5V8Z"/></svg>`)),
	"edit":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m14 5 5 5M4 20l4-.8L19 8a2.1 2.1 0 0 0-3-3L5 16l-1 4Z"/></svg>`)),
	"warning":      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.3 3.9 1.9 18.1A2 2 0 0 0 3.6 21h16.8a2 2 0 0 0 1.7-2.9L13.7 3.9a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4m0 4h.01"/></svg>`)),
	"copy":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></svg>`)),
	"trash":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M8 6V4h8v2m3 0-1 14H6L5 6m4 4v6m6-6v6"/></svg>`)),
	"trashAll":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M2 7h9M4 7l.7 13h4.6L10 7M5 7V5h4v2m4 0h9m-7 0 .7 13h4.6L21 7m-6 0V5h4v2"/></svg>`)),
	"check":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 4 4L19 6"/></svg>`)),
	"undo":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 14 4 9l5-5"/><path d="M4 9h9a7 7 0 0 1 0 14h-2"/></svg>`)),
	"chevronLeft":  ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>`)),
	"chevronRight": ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>`)),
	"firstPage":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m11 18-6-6 6-6m8 12-6-6 6-6"/><path d="M4 5v14"/></svg>`)),
	"lastPage":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m13 18 6-6-6-6M5 18l6-6-6-6"/><path d="M20 5v14"/></svg>`)),
	"chevronDown":  ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>`)),
	"chevronUp":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m6 15 6-6 6 6"/></svg>`)),
	"download":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4"/></svg>`)),
	"upload":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16V4m-5 5 5-5 5 5M4 16v4h16v-4"/></svg>`)),
	"sync":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M5.5 9a7 7 0 0 1 11.7-2L20 12M4 12l2.8 5a7 7 0 0 0 11.7-2"/></svg>`)),
	"activity":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12h4l3-8 4 16 3-8h4"/></svg>`)),
	"info":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8h.01"/></svg>`)),
	"system":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8m-4-4v4"/><circle cx="12" cy="10.5" r="2.5"/></svg>`)),
	"sun":          ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42"/></svg>`)),
	"moon":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20.8 13A8.5 8.5 0 0 1 11 3.2 8.5 8.5 0 1 0 20.8 13Z"/></svg>`)),
	"languages":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m5 8 6 6m-7 0 6-6 2-3M2 5h12M7 2h1m14 20-5-10-5 10m3-4h6"/></svg>`)),
	"settings":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Z"/><path d="m19.4 15 .1.1 1.4 1.1-1.5 2.6-1.7-.7a8 8 0 0 1-1.8 1l-.3 1.9h-3l-.3-1.9a8 8 0 0 1-1.8-1l-1.7.7-1.5-2.6 1.4-1.1a7 7 0 0 1 0-2l-1.4-1.1 1.5-2.6 1.7.7a8 8 0 0 1 1.8-1l.3-1.9h3l.3 1.9a8 8 0 0 1 1.8 1l1.7-.7 1.5 2.6-1.4 1.1a7 7 0 0 1 0 2Z"/></svg>`)),
}

func iconButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	button := ui.Button(c, "").Label(label).Tooltip(label)
	button.Padding(c.Theme().Space(1.5))
	button.Children(func() { ui.Icon(c, icon).Size(16, 16) })
	return button
}

func iconPrimaryButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	button := ui.PrimaryButton(c, "").Label(label).Tooltip(label)
	button.Padding(c.Theme().Space(1.5))
	button.Children(func() { ui.Icon(c, icon).Size(16, 16) })
	return button
}

func (app *workbench) toolbar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(bar.Height, 32)).Shrink(0).Padding(0, bar.Right+8, 0, bar.Left+8).Gap(8).DragWindow().Children(func() {
		leftPanel := iconButton(c, app.localized("切换左侧边栏 · ⌘/Ctrl+B"), actionIcons["panelLeft"])
		leftPanel.Selected(!app.leftCollapsed)
		if leftPanel.Clicked() {
			app.toggleLeftPanel()
		}
		newConnection := iconPrimaryButton(c, app.localized("新建连接…"), actionIcons["add"])
		newConnection.Disabled(app.busy || app.exporting || app.dialogOpen)
		if newConnection.Clicked() {
			app.beginNewConnection()
		}
		newQuery := iconButton(c, app.localized("新建查询工作区"), actionIcons["query"])
		newQuery.Disabled(app.busy || app.exporting || app.dialogOpen || (app.hasActiveConnection() && app.store == nil))
		if newQuery.Clicked() {
			app.addWorkspace()
		}
		if app.aiRunning {
			cancelAI := ui.Button(c, app.localized("取消 AI 请求"))
			if cancelAI.Clicked() && app.aiCancel != nil {
				app.aiCancel()
			}
		}
		if app.exporting && app.cancelExport != nil {
			if ui.Button(c, app.localized("取消导出")).Clicked() {
				app.cancelExport()
			}
		} else if app.busy && app.cancelQuery != nil {
			if !app.sqlQueryTabActive() && ui.Button(c, app.localized("取消查询")).Clicked() && app.cancelQuery != nil {
				app.cancelQuery()
			}
		} else if app.busy && app.cancelRedis != nil {
			if ui.Button(c, app.localized("取消 Redis 操作")).Clicked() && app.cancelRedis != nil {
				app.cancelRedis()
			}
		} else if app.busy && app.mongoCancel != nil {
			if ui.Button(c, app.localized("取消 MongoDB 操作")).Clicked() && app.mongoCancel != nil {
				app.mongoCancel()
			}
		} else if app.busy || app.exporting {
			if !app.sqlQueryTabActive() {
				ui.Text(c, app.localized("处理中…"))
			}
		} else if app.keyStore != nil || app.documentStore != nil || app.etcdStore != nil {
			// Non-SQL data sources provide their actions in their own workspaces.
		} else if app.store != nil && app.aiDraftPending {
			ui.Text(c, app.localized("AI 草稿执行前审阅")).Bold()
			ui.Text(c, app.localized("目标：")+app.openedLabel+" · "+string(app.openedDriver)+" · "+app.localized(environmentLabel(app.openedEnvironment))).TextColor(c.Theme().TextMuted)
			ui.Text(c, app.localized("SQL 类型估计：")+app.localized(sqlRiskSummary(app.aiDraftStatement))+"。请确认目标、语句与影响范围。").TextColor(c.Theme().TextMuted)
			ui.Text(c, app.aiDraftStatement).Selectable()
			ui.Row(c).Gap(8).Children(func() {
				confirm := ui.PrimaryButton(c, app.localized("确认并执行此草稿"))
				confirm.Disabled(app.dialogOpen || app.busy || app.exporting)
				if confirm.Clicked() {
					app.aiDraftApproved = true
					app.executeSQL()
				}
				cancel := ui.Button(c, app.localized("取消审阅"))
				if cancel.Clicked() {
					app.aiDraftPending, app.aiDraftApproved = false, false
				}
			})
		}
		ui.Box(c).Grow(1)
		aiLabel := "打开 AI 助手 · ⌘/Ctrl+Shift+B"
		if app.showAI {
			aiLabel = "关闭 AI 助手 · ⌘/Ctrl+Shift+B"
		}
		aiButton := iconButton(c, app.localized(aiLabel), actionIcons["sparkle"])
		aiButton.Selected(app.showAI)
		aiButton.Disabled(app.busy || app.exporting || app.dialogOpen)
		if aiButton.Clicked() {
			app.toggleAIPanel()
		}
		settingsButton := iconButton(c, app.localized("打开设置…"), actionIcons["settings"])
		if settingsButton.Clicked() {
			app.openSettings()
		}
	})
}

func (app *workbench) sqlQueryTabActive() bool {
	workspace := app.activeQuery
	return app.store != nil && app.queryWorkspaceRequested && !app.aiFullPage && workspace != nil &&
		!workspace.connectionView && !workspace.dataView && !workspace.schemaView
}

func (app *workbench) toggleLeftPanel() {
	app.leftCollapsed = !app.leftCollapsed
	app.scheduleLayoutSave()
}

func (app *workbench) toggleAIPanel() {
	app.showAI = !app.showAI
}

func appearanceLabel(appearance string) string {
	switch appearance {
	case "light":
		return "浅色"
	case "dark":
		return "深色"
	default:
		return "跟随系统"
	}
}

func (app *workbench) setLanguage(next string) {
	if app.busy || app.exporting || app.dialogOpen || app.preferences == nil {
		return
	}
	if next == app.language {
		return
	}
	app.preferenceRevision++
	revision := app.preferenceRevision
	store := app.preferences
	app.busy = true
	go func() {
		err := store.SetLanguage(next, revision)
		app.window.Update(func() {
			app.busy = false
			app.invalidateSettingsWindow()
			if err != nil {
				app.reportError(app.localized("保存语言设置失败"), err)
				return
			}
			app.language = next
			mygo.App.SetMenu(mygo.NewMenu(appMenuItems(app, runtime.GOOS)))
			if app.settingsWindow != nil {
				app.settingsWindow.SetTitle(app.localized("设置"))
			}
			app.status = app.localized("语言设置已保存。")
		})
	}()
}

func (app *workbench) setAppearance(next string) {
	if app.busy || app.exporting || app.dialogOpen || app.preferences == nil {
		return
	}
	if next == app.appearance {
		return
	}
	app.preferenceRevision++
	revision := app.preferenceRevision
	store := app.preferences
	app.busy = true
	app.status = app.localized("正在保存外观设置…")
	go func() {
		err := store.SetAppearance(next, revision)
		app.window.Update(func() {
			app.busy = false
			app.invalidateSettingsWindow()
			if err != nil {
				app.reportError(app.localized("保存外观设置失败"), err)
				return
			}
			app.appearance = next
			setAppearanceSource(next)
			app.status = app.localized("外观设置已保存。")
		})
	}()
}

func (app *workbench) beginNewConnection() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.path = existingSQLitePath(app.path)
	app.resetConnectionProfileTest()
	app.selectedProfileID = ""
	app.profileDriver = preferences.DriverSQLite
	app.profileName, app.profileGroup = "", ""
	app.profileEnvironment = preferences.EnvironmentDevelopment
	app.profileHost, app.profilePort = "", ""
	app.profileTLSMode = tlsModeLabel("disable")
	app.profileDatabase, app.profileRedisDB, app.profileAuthSource = "", "0", ""
	app.profileTLSCAPath, app.profileTLSCertPath, app.profileTLSKeyPath = "", "", ""
	app.profileUsername, app.profileSecret, app.profileCredential = "", "", ""
	app.profileSecretVisible = false
	app.removeCredential, app.profileReadOnly, app.profileFavorite = false, false, false
	app.showConnectionEditor = true
}

func (app *workbench) discardConnectionEditorSecrets() {
	app.profileSecret = ""
	app.profileSecretVisible = false
	app.profileTLSCertPath, app.profileTLSKeyPath = "", ""
	app.removeCredential = false
}
