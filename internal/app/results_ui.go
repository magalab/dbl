package app

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/sqlstream"
)

type pinnedDataResultScroll struct {
	header ui.ScrollState
	footer ui.ScrollState
	rows   map[int]*ui.ScrollState
	synced float32
	page   int
}

func (app *workbench) resultTable(c *ui.Context) {
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		if !app.activeQuery.dataView {
			ui.Row(c).FillWidth().Padding(6, 10).Gap(8).AlignItems(ui.Center).Role(ui.RoleTabList).Label(app.localized("查询结果面板")).Children(func() {
				resultsButton := ui.Button(c, app.localized("结果")).Role(ui.RoleTab).Selected(!app.activeQuery.showHistory)
				if resultsButton.Clicked() {
					app.activeQuery.showHistory = false
				}
				historyButton := ui.Button(c, fmt.Sprintf(app.localized("历史 (%d)"), len(app.activeQuery.history))).Role(ui.RoleTab).Selected(app.activeQuery.showHistory)
				if historyButton.Clicked() {
					app.activeQuery.showHistory = true
				}
				if app.activeQuery.showHistory {
					ui.Spacer(c)
					clearButton := iconButton(c, app.localized("清空此工作区历史"), actionIcons["trash"])
					clearButton.Size(30, 30).Padding(4)
					clearButton.Disabled(app.busy || app.exporting || app.dialogOpen || app.cursor != nil || app.queryHistorySaving)
					if clearButton.Clicked() {
						app.activeQuery.history = nil
						app.activeQuery.historyList = ui.ListState{}
						app.persistQueryHistory()
						app.status = app.localized("当前工作区的查询历史已清空。")
					}
					clearAllButton := iconButton(c, app.localized("清空所有工作区及已保存历史"), actionIcons["trashAll"])
					clearAllButton.Size(30, 30).Padding(4)
					clearAllButton.Disabled(app.busy || app.exporting || app.dialogOpen || app.cursor != nil || app.queryHistorySaving)
					if clearAllButton.Clicked() {
						for _, workspace := range app.workspaces {
							workspace.history = nil
							workspace.historyList = ui.ListState{}
							workspace.activeHistoryID = 0
						}
						app.persistQueryHistory()
						app.status = app.localized("所有工作区和已保存的查询历史已清空。")
					}
				}
			})
		}
		if app.activeQuery.showHistory && !app.activeQuery.dataView {
			ui.Row(c).FillWidth().Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
				historyPersistence := ui.Checkbox(c, &app.queryHistoryEnabled, app.localized("在本机保存查询历史"))
				historyPersistence.Disabled(app.busy || app.exporting || app.dialogOpen || app.queryHistorySaving || app.cursor != nil || app.preferences == nil)
				if historyPersistence.Changed() {
					app.setQueryHistoryEnabled(app.queryHistoryEnabled)
				}
				ui.Text(c, app.localized("仅保存在本机；SQL 可能包含敏感值。")).TextColor(c.Theme().TextMuted).SingleLine()
				info := iconButton(c, app.localized("查询历史保存说明"), actionIcons["info"])
				info.Size(26, 26).Padding(3).Tooltip(app.localized("默认关闭。开启后会把 SQL 文本、时间、行数和状态写入本机设置文件；SQL 可能包含敏感值。关闭会删除已保存历史。"))
			})
			app.activeQuery.historyList.Key = func(i int) any { return app.activeQuery.history[i].ID }
			app.activeQuery.historyList.Label = func(i int) string { return app.activeQuery.history[i].SQL }
			ui.List(c, &app.activeQuery.historyList, len(app.activeQuery.history), func(i int) {
				record := app.activeQuery.history[i]
				ui.Row(c).FillWidth().Padding(4, 8).Gap(10).AlignItems(ui.Center).Children(func() {
					statement := ui.Button(c, shortSQL(record.SQL)).Background(ui.Color{}).BorderWidth(0).Padding(4, 6).Grow(1).Tooltip(app.localized("加载此 SQL 到编辑器"))
					if statement.Clicked() {
						app.activeQuery.query.Buffer.Set(record.SQL)
						app.activeQuery.showHistory = false
					}
					ui.Text(c, record.Started.Format("15:04:05")).TextColor(c.Theme().TextMuted).SingleLine()
					ui.Text(c, fmt.Sprintf(app.localized("%d 行 · %s · %s"), record.Rows, record.Duration.Round(time.Millisecond), app.localized(record.Outcome))).TextColor(c.Theme().TextMuted).SingleLine()
				})
			}).Grow(1).Label(app.localized("查询历史"))
			return
		}
		if len(app.activeQuery.columns) == 0 {
			placeholder := "执行查询后，结果会按页显示在这里。"
			if app.activeQuery.dataView {
				placeholder = "刷新数据后，表格内容会显示在这里。"
			}
			ui.Text(c, app.localized(placeholder)).TextColor(c.Theme().TextMuted).Padding(12)
			return
		}
		columns := make([]ui.TableColumn, 0, len(app.activeQuery.columns)+1)
		columns = append(columns, ui.TableColumn{ID: "row-number", Title: "#", Width: 48, Fixed: true, RowHeader: true})
		for i, column := range app.activeQuery.columns {
			headerWidth, _ := c.MeasureText(0, ui.Span{
				Text: column.Name, Font: c.Theme().Font, Size: c.Theme().FontSize, Weight: 600,
			})
			minWidth := max(float32(88), headerWidth+30) // Include cell padding and the sort indicator.
			width := float32(160)
			if app.activeQuery.dataView {
				width = 0 // Share the available table width, like a spreadsheet grid.
			}
			columns = append(columns, ui.TableColumn{ID: strconv.Itoa(i), Title: column.Name, Width: width, MinWidth: minWidth, Sortable: true})
		}
		app.updateResultOrder()
		rowCount := app.activeQuery.resultWindow.Len()
		pageSize := resultWorkspacePageSize(app.activeQuery)
		pageStart, pageEnd := 0, rowCount
		pageCount := 1
		if app.activeQuery.dataView {
			pageCount = resultPageCount(rowCount, pageSize)
			if app.activeQuery.dataInsertEditing {
				pageCount = resultPageCount(rowCount+1, pageSize)
			}
			if app.activeQuery.dataPage >= pageCount {
				app.activeQuery.dataPage = pageCount - 1
				app.activeQuery.dataSelectedRow = -1
			}
			if app.activeQuery.dataPage < 0 {
				app.activeQuery.dataPage = 0
			}
			pageStart = dataWorkspacePageStart(app.activeQuery)
			pageEnd = min(pageStart+pageSize, rowCount)
			if pageStart > rowCount {
				pageStart, pageEnd = rowCount, rowCount
			}
		} else {
			pageCount = resultPageCount(rowCount, pageSize)
			if app.activeQuery.resultPage >= pageCount {
				app.activeQuery.resultPage = pageCount - 1
			}
			if app.activeQuery.resultPage < 0 {
				app.activeQuery.resultPage = 0
			}
			pageStart = app.activeQuery.resultPage * pageSize
			pageEnd = min(pageStart+pageSize, rowCount)
		}
		columns[0].Width = app.resultRowNumberColumnWidth(c, pageStart, pageEnd)
		app.activeQuery.resultList.Sort = &app.activeQuery.resultSort
		if app.activeQuery.dataView {
			app.activeQuery.resultList.Selected = &app.activeQuery.dataSelectedRow
		} else {
			app.activeQuery.resultList.Selected = nil
		}
		queryID := app.activeQuery.activeHistoryID
		app.activeQuery.resultList.Key = func(i int) any {
			pageRowCount := pageEnd - pageStart
			insertRowVisible := app.activeQuery.dataView && app.activeQuery.dataInsertEditing && pageEnd == rowCount
			if insertRowVisible && i == pageRowCount {
				return fmt.Sprintf("insert-row-%d", app.activeQuery.id)
			}
			displayRow := i + pageStart
			return resultRowKey{QueryID: queryID, SourceRow: app.activeQuery.resultWindow.SourceRow(app.resultSourceIndex(displayRow))}
		}
		caption := fmt.Sprintf(app.localized("查询完成，共 %d 行。"), app.activeQuery.resultWindow.Total)
		if app.cursor != nil {
			caption = app.localized("结果还在读取；下一页可继续加载。")
		}
		if app.activeQuery.resultPath != "" {
			caption += " · " + app.localized("来源") + " " + app.activeQuery.resultPath
		}
		if app.activeQuery.resultWindow.OmittedRows > 0 {
			caption += fmt.Sprintf(app.localized(" · %d 行内容过大，未显示"), app.activeQuery.resultWindow.OmittedRows)
		}
		if app.activeQuery.resultWindow.FirstRow > 0 && !app.activeQuery.dataView {
			caption += " · " + app.localized("较早的结果已释放")
		}
		if !app.activeQuery.dataView && app.activeQuery.resultSort.Column != "" {
			caption += " · " + app.localized("排序仅作用于已读取的结果")
		}
		if !app.activeQuery.dataView {
			ui.Text(c, caption).Padding(6, 10)
		}
		tableRows := rowCount
		if app.activeQuery.dataView || rowCount > pageSize {
			tableRows = pageEnd - pageStart
		}
		insertRowVisible := app.activeQuery.dataView && app.activeQuery.dataInsertEditing && pageEnd == rowCount
		if insertRowVisible {
			tableRows++
		}
		renderCell := func(row, col int) {
			pageRowCount := pageEnd - pageStart
			if insertRowVisible && row == pageRowCount {
				app.dataInsertCell(c, app.activeQuery, col)
				return
			}
			displayRow := row + pageStart
			sourceRow := app.resultSourceIndex(displayRow)
			if col == 0 {
				ui.Text(c, strconv.FormatInt(app.activeQuery.resultWindow.SourceRow(sourceRow)+1, 10)).SingleLine().Padding(0, 8).Grow(1).TextAlign(ui.Center)
				return
			}
			dataColumn := col - 1
			values := make([]string, len(app.activeQuery.columns))
			for i, value := range app.activeQuery.resultWindow.Row(sourceRow) {
				if i < len(values) {
					values[i] = formatCell(value, resultColumnDatabaseType(app.activeQuery, i))
				}
			}
			cellText := values[dataColumn]
			rowText := sqlstream.FormatClipboardRow(values)
			var cell ui.Element
			if app.activeQuery.dataView {
				dataRowID := app.activeQuery.resultWindow.SourceRow(sourceRow)
				original := any(nil)
				if dataColumn >= 0 && dataColumn < len(app.activeQuery.resultWindow.Row(sourceRow)) {
					original = app.activeQuery.resultWindow.Row(sourceRow)[dataColumn]
				}
				shown := app.displayedDataCell(app.activeQuery, dataRowID, dataColumn, original)
				if change := findDataRowChange(app.activeQuery, dataRowID); change != nil && change.kind == connection.DeleteRow {
					cellText = "「" + cellText + "」 · " + app.localized("待删除")
				} else {
					cellText = formatCell(shown, resultColumnDatabaseType(app.activeQuery, dataColumn))
				}
				if app.activeQuery.dataCellEditing && app.activeQuery.dataEditSourceRow == dataRowID && app.activeQuery.dataEditColumn == dataColumn {
					workspace := app.activeQuery
					input := ui.TextInput(c, &workspace.dataEditDraft).FillWidth().Label(app.activeQuery.columns[dataColumn].Name)
					input.Disabled(!app.canEditDataWorkspace(app.activeQuery) || app.activeQuery.dataEditNull)
					cell = input.OnShortcut(0, ui.KeyEscape, func() {
						app.cancelDataCellEdit(workspace)
					})
				} else {
					columnInfo, hasColumnInfo := dataColumnInfoForResult(app.activeQuery, dataColumn)
					rowHasKey := dataRowHasPrimaryKeyValues(app.activeQuery, app.activeQuery.resultWindow.Row(sourceRow))
					editButton := ui.Button(c, cellText).FillWidth().SingleLine().Background(ui.Color{}).BorderWidth(0).Padding(4, 6).TextColor(c.Theme().Text)
					editButton.Disabled(!app.canEditDataWorkspace(app.activeQuery) || !rowHasKey || !hasColumnInfo || !dataColumnEditable(columnInfo))
					editButton.Tooltip(app.localized("点击单元格即可暂存修改"))
					if editButton.Clicked() {
						app.beginDataCellEdit(app.activeQuery, dataRowID, dataColumn, shown)
					}
					cell = editButton
				}
			} else {
				cell = ui.Text(c, cellText).SingleLine()
			}
			cell.ContextMenu(func(menu *ui.Menu) {
				if menu.Item(app.localized("复制单元格")).Chosen() {
					mygo.Clipboard.WriteText(cellText)
				}
				if menu.Item(app.localized("复制整行")).Chosen() {
					mygo.Clipboard.WriteText(rowText)
				}
				menu.Separator()
				app.addResultExportMenu(menu, app.activeQuery)
				if app.activeQuery.dataView {
					menu.Separator()
					rowHasKey := dataRowHasPrimaryKeyValues(app.activeQuery, app.activeQuery.resultWindow.Row(sourceRow))
					dataRowID := app.activeQuery.resultWindow.SourceRow(sourceRow)
					currentChange := findDataRowChange(app.activeQuery, dataRowID)
					if currentChange != nil && currentChange.kind == connection.DeleteRow {
						if menu.Item(app.localized("取消标记删除")).Chosen() {
							app.unstageDeleteDataRow(app.activeQuery, dataRowID)
						}
					} else if menu.Item(app.localized("标记为删除")).Disabled(!app.canEditDataWorkspace(app.activeQuery) || !rowHasKey || len(app.activeQuery.dataChanges) >= connection.MaxTableChangeBatch).Chosen() {
						app.stageDeleteDataRow(app.activeQuery, dataRowID)
					}
				}
				menu.Separator()
				menu.EditItems()
			})
		}
		app.pinnedResultTable(c, app.activeQuery, columns, tableRows, renderCell)
		if app.activeQuery.dataView {
			app.dataWorkspaceResultFooter(c, app.activeQuery, rowCount, pageStart, pageEnd, pageCount)
		} else {
			app.sqlResultFooter(c, app.activeQuery, rowCount, pageStart, pageEnd, pageCount)
		}
	})
}

func (app *workbench) resultRowNumberColumnWidth(c *ui.Context, pageStart, pageEnd int) float32 {
	theme := c.Theme()
	headerWidth, _ := c.MeasureText(0, ui.Span{
		Text: "#", Font: theme.Font, Size: theme.FontSize, Weight: 600,
	})
	maxOrdinal := int64(1)
	for row := pageStart; row < pageEnd; row++ {
		sourceRow := app.resultSourceIndex(row)
		maxOrdinal = max(maxOrdinal, app.activeQuery.resultWindow.SourceRow(sourceRow)+1)
	}
	digitCount := len(strconv.FormatInt(maxOrdinal, 10))
	numberWidth, _ := c.MeasureText(0, ui.Span{Text: strings.Repeat("8", digitCount), Font: theme.Font, Size: theme.FontSize})
	contentWidth := max(headerWidth, numberWidth)
	const horizontalPadding = float32(8)
	return max(float32(48), contentWidth+horizontalPadding*2)
}

func (app *workbench) pinnedResultTable(c *ui.Context, workspace *queryWorkspace, columns []ui.TableColumn, rowCount int, renderCell func(row, column int)) {
	if workspace == nil || len(columns) == 0 {
		return
	}
	syncDataResultHorizontalScroll(workspace)
	const rowHeight = float32(32)
	rowNumberWidth := columns[0].Width
	order := dataResultColumnOrder(workspace, columns)
	widths := make(map[int]float32, len(order))
	var contentWidth float32
	for _, index := range order {
		width := dataResultColumnWidth(workspace, columns[index])
		widths[index] = width
		contentWidth += width + 1
	}
	theme := c.Theme()
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Height(rowHeight).Gap(0).AlignItems(ui.Stretch).Background(theme.Surface).Children(func() {
			ui.Row(c).Width(rowNumberWidth).Shrink(0).AlignItems(ui.Center).Role(ui.RoleColumnHeader).Children(func() {
				ui.Text(c, "#").Padding(4, 8).Grow(1).SingleLine().TextAlign(ui.Center).Role(ui.RoleColumnHeader)
				ui.Box(c).Width(1).FillHeight().Background(theme.Border)
			})
			header := ui.ScrollHorizontal(c).Grow(1).MinWidth(0).TrackScroll(&workspace.resultGridScroll.header).Scrollbars(false)
			header.Children(func() {
				ui.Row(c).Height(rowHeight).Shrink(0).AlignItems(ui.Stretch).Children(func() {
					for _, index := range order {
						column := columns[index]
						label := column.Title
						if workspace.resultSort.Column == column.ID {
							if workspace.resultSort.Descending {
								label += " ↓"
							} else {
								label += " ↑"
							}
						}
						width := widths[index]
						ui.Row(c).Width(width).MinWidth(width).MaxWidth(width).Shrink(0).AlignItems(ui.Center).Role(ui.RoleColumnHeader).Children(func() {
							button := ui.Button(c, label).FillWidth().SingleLine().Background(ui.Color{}).BorderWidth(0).Padding(4, 6).
								TextColor(theme.TextMuted).Tooltip(column.Title)
							button.ContextMenu(func(menu *ui.Menu) {
								app.addResultExportMenu(menu, workspace)
							})
							if button.Clicked() {
								if workspace.resultSort.Column == column.ID {
									workspace.resultSort.Descending = !workspace.resultSort.Descending
								} else {
									workspace.resultSort = ui.SortOrder{Column: column.ID}
								}
								workspace.resultList.Sort = &workspace.resultSort
							}
						})
						ui.Box(c).Width(1).FillHeight().Background(theme.Border)
					}
				})
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(theme.Border)
		ui.List(c, &workspace.resultList, rowCount, func(row int) {
			ui.Row(c).FillWidth().MinHeight(rowHeight).Gap(0).AlignItems(ui.Stretch).Children(func() {
				ui.Row(c).Width(rowNumberWidth).MinWidth(rowNumberWidth).MaxWidth(rowNumberWidth).Shrink(0).AlignItems(ui.Center).Role(ui.RoleRowHeader).Children(func() {
					renderCell(row, 0)
					ui.Box(c).Width(1).FillHeight().Background(theme.Border)
				})
				rowScroll := workspace.resultGridScroll.rows[row]
				if rowScroll == nil {
					rowScroll = &ui.ScrollState{X: workspace.resultGridScroll.synced}
					workspace.resultGridScroll.rows[row] = rowScroll
				}
				rowContent := ui.ScrollHorizontal(c).Grow(1).MinWidth(0).TrackScroll(rowScroll).Scrollbars(false)
				rowContent.Children(func() {
					ui.Row(c).Height(rowHeight).Shrink(0).AlignItems(ui.Stretch).Children(func() {
						for _, index := range order {
							width := widths[index]
							ui.Row(c).Width(width).MinWidth(width).MaxWidth(width).Shrink(0).Clip().Padding(0, 2).AlignItems(ui.Center).Children(func() {
								renderCell(row, index)
							})
							ui.Box(c).Width(1).FillHeight().Background(theme.Border)
						}
					})
				})
			})
		}).Grow(1).Label(app.localized("查询结果"))
		ui.Row(c).FillWidth().Height(12).Gap(0).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(rowNumberWidth).Shrink(0).Background(theme.Surface)
			if contentWidth > 0 {
				bar := ui.ScrollHorizontal(c).Grow(1).TrackScroll(&workspace.resultGridScroll.footer).KeepScrollbars(true)
				bar.Children(func() {
					ui.Box(c).Width(contentWidth).Height(1).Shrink(0)
				})
			}
		})
	})
}

func syncDataResultHorizontalScroll(workspace *queryWorkspace) {
	if workspace.resultGridScroll.page != workspace.dataPage {
		workspace.resultGridScroll.rows = nil
		workspace.resultGridScroll.page = workspace.dataPage
	}
	if workspace.resultGridScroll.rows == nil {
		workspace.resultGridScroll.rows = make(map[int]*ui.ScrollState)
	}
	states := make([]*ui.ScrollState, 0, len(workspace.resultGridScroll.rows)+2)
	states = append(states, &workspace.resultGridScroll.header, &workspace.resultGridScroll.footer)
	for _, state := range workspace.resultGridScroll.rows {
		if state != nil {
			states = append(states, state)
		}
	}
	x := workspace.resultGridScroll.synced
	for _, state := range states {
		if state.X != workspace.resultGridScroll.synced {
			x = state.X
			break
		}
	}
	for _, state := range states {
		state.X = x
	}
	workspace.resultGridScroll.synced = x
}

func dataResultColumnOrder(workspace *queryWorkspace, columns []ui.TableColumn) []int {
	indexes := make(map[string]int, len(columns))
	for index, column := range columns {
		if index > 0 {
			indexes[column.ID] = index
		}
	}
	order := make([]int, 0, len(indexes))
	seen := make(map[int]bool, len(indexes))
	for _, id := range workspace.resultList.Columns.Order {
		if index, ok := indexes[id]; ok && !seen[index] {
			order = append(order, index)
			seen[index] = true
		}
	}
	for index := 1; index < len(columns); index++ {
		if !seen[index] {
			order = append(order, index)
		}
	}
	return order
}

func dataResultColumnWidth(workspace *queryWorkspace, column ui.TableColumn) float32 {
	width := max(float32(160), column.MinWidth)
	if column.Width > 0 {
		width = max(width, column.Width)
	}
	if saved, ok := workspace.resultList.Columns.Widths[column.ID]; ok {
		width = max(column.MinWidth, saved)
	}
	return width
}

func (app *workbench) sqlResultFooter(c *ui.Context, workspace *queryWorkspace, retainedRows, pageStart, pageEnd, pageCount int) {
	ui.Row(c).FillWidth().Shrink(0).Padding(5, 10).Gap(7).AlignItems(ui.Center).Children(func() {
		app.resultFooterCaption(c, workspace, pageEnd-pageStart)
		app.resultCountButton(c, workspace)
		app.resultFooterDuration(c, workspace)
		ui.Spacer(c)
		app.resultPaginationControls(c, workspace, &workspace.resultPage, retainedRows, pageCount, false)
		app.resultExportButton(c, workspace)
	})
}

func (app *workbench) resultFooterCaption(c *ui.Context, workspace *queryWorkspace, shownRows int) {
	shownRows = max(shownRows, 0)
	caption := app.localized("暂无结果")
	if workspace.resultTotalKnown {
		if workspace.resultTotalCounted {
			caption = fmt.Sprintf(app.localized("当前页显示 %d 行 · 统计总数 %d 行"), shownRows, workspace.resultTotal)
			ui.Text(c, caption).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(app.localized("总数由单独的只读 COUNT 查询得出；数据变化时可能与当前读取结果略有差异。"))
			return
		}
		caption = fmt.Sprintf(app.localized("当前页显示 %d 行 · 查询完成，共 %d 行"), shownRows, workspace.resultTotal)
	} else if shownRows > 0 {
		caption = fmt.Sprintf(app.localized("当前页显示 %d 行"), shownRows)
		if app.activeQuery == workspace && app.cursor != nil {
			caption = fmt.Sprintf(app.localized("当前页显示 %d 行 · 已读取 %d 行"), shownRows, workspace.resultWindow.Total)
		} else if workspace.streamStopped {
			caption = fmt.Sprintf(app.localized("当前页显示 %d 行 · 已读取 %d 行（查询未完成）"), shownRows, workspace.resultWindow.Total)
		} else {
			caption = fmt.Sprintf(app.localized("当前页显示 %d 行 · 查询完成，共 %d 行"), shownRows, workspace.resultWindow.Total)
		}
	}
	ui.Text(c, caption).TextColor(c.Theme().TextMuted).SingleLine()
}

func (app *workbench) resultFooterDuration(c *ui.Context, workspace *queryWorkspace) {
	for _, record := range workspace.history {
		if record.ID != workspace.activeHistoryID || record.Started.IsZero() {
			continue
		}
		duration := record.Duration
		if app.activeQuery == workspace && !workspace.resultPageReadStarted.IsZero() {
			duration += time.Since(workspace.resultPageReadStarted)
		}
		ui.Text(c, "·").TextColor(c.Theme().TextMuted).SingleLine()
		ui.Text(c, duration.Round(time.Millisecond).String()).TextColor(c.Theme().TextMuted).SingleLine()
		return
	}
}

func (app *workbench) resultExportButton(c *ui.Context, workspace *queryWorkspace) {
	export := ui.MenuButton(c, app.localized("导出"), func(menu *ui.Menu) {
		app.addResultExportFormats(menu, workspace)
	})
	export.Size(78, 30).Padding(4, 8).Tooltip(app.localized("导出结果"))
	export.Disabled(app.resultExportDisabled(workspace))
}

func (app *workbench) resultCountButton(c *ui.Context, workspace *queryWorkspace) {
	if workspace == nil || app.activeQuery != workspace || len(workspace.columns) == 0 || strings.TrimSpace(workspace.resultSQL) == "" {
		return
	}
	if app.resultCountWorkspace == workspace {
		if app.resultCountCancelRequested {
			if app.resultCountCancelError != "" {
				retry := ui.Button(c, app.localized("重试取消")).
					Padding(4, 8).
					TextColor(c.Theme().Danger).
					Background(c.Theme().Danger.Alpha(0.12)).
					Border(1, c.Theme().Danger.Alpha(0.5)).
					Tooltip(fmt.Sprintf(app.localized("数据库端取消请求失败：%s"), app.resultCountCancelError))
				if retry.Clicked() {
					app.resultCountCancelError = ""
					app.resultCountServerCancelSent = false
					app.cancelResultCountOnServer(workspace)
				}
				return
			}
			ui.Button(c, app.localized("正在停止…")).
				Padding(4, 8).
				TextColor(c.Theme().TextMuted).
				Background(c.Theme().Danger.Alpha(0.08)).
				Border(1, c.Theme().Danger.Alpha(0.25)).
				Disabled(true).
				Tooltip(app.localized("已发送取消请求，等待数据库查询停止。"))
			return
		}
		ui.Text(c, app.localized("统计中…")).TextColor(c.Theme().Accent).SingleLine()
		cancel := ui.Button(c, app.localized("取消统计")).
			Padding(4, 8).
			TextColor(c.Theme().Danger).
			Background(c.Theme().Danger.Alpha(0.12)).
			Border(1, c.Theme().Danger.Alpha(0.5)).
			Tooltip(app.localized("取消正在后台运行的总数查询。"))
		if cancel.Clicked() {
			app.cancelResultCountForWorkspace(workspace)
		}
		return
	}
	if workspace.resultCountCancelled {
		retry := ui.Button(c, app.localized("统计已取消 · 重试")).
			Padding(4, 8).
			TextColor(c.Theme().TextMuted).
			Tooltip(app.localized("重新执行总数统计。"))
		retry.Disabled(app.busy || app.exporting || app.dialogOpen || app.resultCountWorkspace != nil || app.store == nil || workspace.resultConnectionID != app.openedConnectionID)
		if retry.Clicked() {
			workspace.resultCountCancelled = false
			app.countRemainingResultRows()
		}
		return
	}
	if workspace.resultTotalKnown {
		return
	}
	if app.store == nil || workspace.resultConnectionID == "" || workspace.resultConnectionID != app.openedConnectionID {
		return
	}
	if _, ok := resultCountStatement(workspace.resultSQL); !ok {
		return
	}
	countTooltip := "在数据库后台执行只读 COUNT 查询，不会把全部行传到界面；数据变化时统计结果可能与当前查询流略有差异。"
	if app.resultCountWorkspace != nil {
		countTooltip = "另一个工作区正在统计总数。"
	}
	count := ui.Button(c, app.localized("统计总数")).
		Background(ui.Color{}).
		BorderWidth(0).
		Padding(2, 4).
		TextColor(c.Theme().Accent).
		Tooltip(app.localized(countTooltip))
	count.Disabled(app.busy || app.exporting || app.dialogOpen || app.resultCountWorkspace != nil || (workspace.dataView && (len(workspace.dataChanges) > 0 || workspace.dataInsertEditing || workspace.dataCellEditing)))
	if count.Clicked() {
		app.countRemainingResultRows()
	}
}

func (app *workbench) resultPaginationControls(c *ui.Context, workspace *queryWorkspace, page *int, retainedRows, pageCount int, dataView bool) {
	editingData := dataView && (workspace.dataInsertEditing || workspace.dataCellEditing)
	streaming := app.activeQuery == workspace && app.cursor != nil
	ui.Text(c, app.localized("每页")).TextColor(c.Theme().TextMuted).SingleLine()
	pageSizeChoice := strconv.Itoa(resultWorkspacePageSize(workspace))
	pageSize := ui.Select(c, &pageSizeChoice, resultPageSizeOptions()).Width(72).Label(app.localized("每页行数"))
	pageSize.Disabled(app.busy || app.dialogOpen || editingData)
	if pageSize.Changed() {
		if size, parseErr := strconv.Atoi(pageSizeChoice); parseErr == nil {
			app.setResultPageSize(workspace, size, page, dataView)
		}
	}
	firstPage := iconButton(c, app.localized("第一页"), actionIcons["firstPage"])
	firstPage.Size(30, 30).Padding(4)
	firstPage.Disabled(*page <= 0 || app.busy || app.dialogOpen || editingData)
	if firstPage.Clicked() {
		*page = 0
		workspace.pageJumpInput = "1"
		if dataView {
			workspace.dataSelectedRow = -1
		}
	}
	pagePrevious := iconButton(c, app.localized("上一页"), actionIcons["chevronLeft"])
	pagePrevious.Size(30, 30).Padding(4)
	pagePrevious.Disabled(*page <= 0 || app.busy || app.dialogOpen || editingData)
	if pagePrevious.Clicked() {
		*page = *page - 1
		workspace.pageJumpInput = strconv.Itoa(*page + 1)
		if dataView {
			workspace.dataSelectedRow = -1
		}
	}
	pageInput := ui.TextInput(c, &workspace.pageJumpInput).
		Width(54).
		Height(30).
		Padding(4, 6).
		Placeholder(app.localized("页码")).
		Label(app.localized("跳转到页码")).
		Tooltip(app.localized("输入页码后按回车跳转；未读取的页会按顺序加载，最多保留当前结果窗口。"))
	target, err := strconv.Atoi(strings.TrimSpace(workspace.pageJumpInput))
	jumpPageLimit := pageCount
	if streaming && workspace.resultWindow.Len() < maxDisplayedRows {
		jumpPageLimit = max(jumpPageLimit, resultPageCount(maxDisplayedRows, resultWorkspacePageSize(workspace)))
	}
	valid := retainedRows > 0 && err == nil && target >= 1 && target <= jumpPageLimit
	pageInput.Disabled(app.busy || app.dialogOpen || editingData)
	pageInput.OnShortcut(0, ui.KeyEnter, func() {
		if valid && !app.busy && !app.dialogOpen && !editingData {
			targetPage := target - 1
			if targetPage < pageCount {
				*page = targetPage
				if dataView {
					workspace.dataSelectedRow = -1
				}
			} else if streaming {
				app.fetchResultPage(targetPage)
			}
		}
	})
	pageLabel := fmt.Sprintf("/ %d", pageCount)
	if streaming {
		pageLabel = fmt.Sprintf("/ %d+", pageCount)
	}
	pageCountLabel := ui.Text(c, pageLabel).TextColor(c.Theme().TextMuted).SingleLine()
	if streaming {
		pageCountLabel.Tooltip(app.localized("已加载页数；加号表示还有结果未读取。"))
	}
	pageNext := iconButton(c, app.localized("下一页"), actionIcons["chevronRight"])
	pageNext.Size(30, 30).Padding(4)
	pageNext.Disabled((*page+1 >= pageCount && !streaming) || app.busy || app.dialogOpen || editingData)
	if pageNext.Clicked() {
		if *page+1 < pageCount {
			*page = *page + 1
			workspace.pageJumpInput = strconv.Itoa(*page + 1)
			if dataView {
				workspace.dataSelectedRow = -1
			}
		} else if streaming {
			app.fetchNextResultPage()
		}
	}
	lastPage := iconButton(c, app.localized("最后一页"), actionIcons["lastPage"])
	lastPage.Size(30, 30).Padding(4).Tooltip(app.localized("跳到当前已加载结果的最后一页"))
	lastPage.Disabled(*page >= pageCount-1 || app.busy || app.dialogOpen || editingData)
	if lastPage.Clicked() {
		*page = pageCount - 1
		workspace.pageJumpInput = strconv.Itoa(*page + 1)
		if dataView {
			workspace.dataSelectedRow = -1
		}
	}
}

func (app *workbench) setResultPageSize(workspace *queryWorkspace, newSize int, page *int, dataView bool) {
	if workspace == nil || newSize <= 0 {
		return
	}
	oldSize := resultWorkspacePageSize(workspace)
	if newSize == oldSize {
		return
	}
	firstVisibleRow := *page*oldSize + 1
	workspace.resultPageSize = newSize
	*page = (firstVisibleRow - 1) / newSize
	workspace.pageJumpInput = strconv.Itoa(*page + 1)
	if dataView {
		workspace.dataSelectedRow = -1
	}
}

func (app *workbench) dataWorkspaceResultFooter(c *ui.Context, workspace *queryWorkspace, retainedRows, pageStart, pageEnd, pageCount int) {
	ui.Row(c).FillWidth().Shrink(0).Padding(5, 10).Gap(7).AlignItems(ui.Center).Children(func() {
		if workspace.dataInsertEditing && pageStart >= pageEnd {
			ui.Text(c, fmt.Sprintf(app.localized("新增行 · 已读取 %d 行"), workspace.resultWindow.Total)).TextColor(c.Theme().TextMuted).SingleLine()
		} else {
			app.resultFooterCaption(c, workspace, pageEnd-pageStart)
		}
		app.resultFooterDuration(c, workspace)
		app.resultCountButton(c, workspace)
		ui.Spacer(c)
		app.resultPaginationControls(c, workspace, &workspace.dataPage, retainedRows, pageCount, true)
		app.resultExportButton(c, workspace)
	})
}

func (app *workbench) updateResultOrder() {
	if app.activeQuery.lastSortColumn != app.activeQuery.resultSort.Column || app.activeQuery.lastSortDesc != app.activeQuery.resultSort.Descending {
		app.activeQuery.resultOrderDirty = true
		if app.activeQuery.dataView {
			app.activeQuery.dataSelectedRow = -1
		}
	}
	column := -1
	if app.activeQuery.resultSort.Column != "" {
		if parsed, err := strconv.Atoi(app.activeQuery.resultSort.Column); err == nil && parsed >= 0 && parsed < len(app.activeQuery.columns) {
			column = parsed
		}
	}
	if column < 0 {
		app.activeQuery.resultOrder = nil
		app.activeQuery.resultOrderDirty = false
		app.activeQuery.lastSortColumn = app.activeQuery.resultSort.Column
		app.activeQuery.lastSortDesc = app.activeQuery.resultSort.Descending
		return
	}
	if !app.activeQuery.resultOrderDirty && len(app.activeQuery.resultOrder) == app.activeQuery.resultWindow.Len() {
		return
	}
	app.activeQuery.resultOrder = sqlstream.SortWindowRowIndices(&app.activeQuery.resultWindow, column, app.activeQuery.resultSort.Descending)
	app.activeQuery.resultOrderDirty = false
	app.activeQuery.lastSortColumn = app.activeQuery.resultSort.Column
	app.activeQuery.lastSortDesc = app.activeQuery.resultSort.Descending
}

func (app *workbench) resultSourceIndex(displayRow int) int {
	if len(app.activeQuery.resultOrder) == 0 {
		return displayRow
	}
	return app.activeQuery.resultOrder[displayRow]
}

func (app *workbench) inspectorView(c *ui.Context) {
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Text(c, app.localized("检查器")).Bold()
		if app.store == nil {
			if app.documentStore != nil {
				ui.Text(c, app.openedLabel+" · "+app.localized(environmentLabel(app.openedEnvironment))).SingleLine()
				if app.openedReadOnly {
					ui.Text(c, app.localized("只读连接")).TextColor(c.Theme().TextMuted)
				}
				if app.mongoSelectedDB != "" {
					ui.Text(c, app.localized("数据库 · ")+app.mongoSelectedDB)
				}
				if app.mongoSelectedColl != "" {
					ui.Text(c, app.localized("集合 · ")+app.mongoSelectedColl)
					ui.Text(c, app.localized("索引")).Role(ui.RoleHeading).Level(3)
					if app.mongoIndexesLoading {
						ui.Text(c, app.localized("正在读取索引…")).TextColor(c.Theme().TextMuted)
					} else if app.mongoIndexError != "" {
						ui.Text(c, app.localized("索引读取失败：")+app.mongoIndexError).TextColor(c.Theme().TextMuted)
					} else if len(app.mongoIndexes) == 0 {
						ui.Text(c, app.localized("未返回索引信息。 ")).TextColor(c.Theme().TextMuted)
					} else {
						for _, index := range app.mongoIndexes {
							flags := make([]string, 0, 3)
							if index.Unique {
								flags = append(flags, "唯一")
							}
							if index.Sparse {
								flags = append(flags, "稀疏")
							}
							if index.HasTTL {
								flags = append(flags, fmt.Sprintf("TTL %d 秒", index.TTLSecond))
							}
							label := index.Name + " · " + index.KeysJSON
							if len(flags) > 0 {
								label += " · " + strings.Join(flags, " · ")
							}
							ui.Text(c, label).Selectable()
						}
					}
				}
				if app.mongoSelectedID != "" {
					ui.Text(c, app.localized("文档 ID · ")+app.mongoSelectedID).Selectable()
				}
				return
			}
			ui.Text(c, app.localized("连接数据库后显示对象和列信息。")).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, app.localized("连接")).TextColor(c.Theme().TextMuted)
		if app.hasOpenedSQLProfile {
			profile := app.openedSQLProfile
			ui.Text(c, profile.Name).Bold().SingleLine()
			ui.Text(c, welcomeDriverLabel(profile.Driver)+" · "+app.localized(environmentLabel(profile.Environment))).TextColor(c.Theme().TextMuted).SingleLine()
			if profile.Host != "" {
				endpoint := profile.Host
				if profile.Port > 0 {
					endpoint = net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port))
				}
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, app.localized("主机")).TextColor(c.Theme().TextMuted).Width(56)
					ui.Text(c, endpoint).Grow(1).SingleLine().Selectable()
				})
			}
			if profile.Database != "" {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, app.localized("数据库")).TextColor(c.Theme().TextMuted).Width(56)
					ui.Text(c, profile.Database).Grow(1).SingleLine().Selectable()
				})
			}
		} else if app.openedPath != "" {
			ui.Text(c, filepath.Base(app.openedPath)).Bold().SingleLine()
			ui.Text(c, welcomeDriverLabel(string(app.openedDriver))+" · "+app.localized(environmentLabel(app.openedEnvironment))).TextColor(c.Theme().TextMuted).SingleLine()
		} else {
			ui.Text(c, app.openedLabel).Bold().SingleLine()
			ui.Text(c, welcomeDriverLabel(string(app.openedDriver))+" · "+app.localized(environmentLabel(app.openedEnvironment))).TextColor(c.Theme().TextMuted).SingleLine()
		}
		if app.openedReadOnly {
			ui.Text(c, app.localized("只读连接")).TextColor(c.Theme().TextMuted)
		}
		if app.selectedObject != "" {
			ui.Text(c, app.localized(app.selectedType)+" · "+app.selectedObject).TextColor(c.Theme().TextMuted)
			if app.objectColumnsLoading {
				ui.Text(c, app.localized("正在读取表结构…")).TextColor(c.Theme().TextMuted)
			} else if app.objectColumnsError != "" {
				ui.Text(c, app.localized("读取表结构失败：")+app.objectColumnsError).TextColor(c.Theme().Danger)
			} else if len(app.objectColumns) == 0 && isSQLTableOrView(app.selectedType) {
				ui.Text(c, app.localized("没有列信息。 ")).TextColor(c.Theme().TextMuted)
			}
			for _, column := range app.objectColumns {
				flags := ""
				if column.PrimaryKey {
					flags = " · PK"
				}
				if column.NotNull {
					flags += " · NOT NULL"
				}
				switch column.Hidden {
				case 1:
					flags += " · HIDDEN"
				case 2:
					flags += " · GENERATED"
				case 3:
					flags += " · STORED GENERATED"
				}
				ui.Text(c, fmt.Sprintf("%s · %s%s", column.Name, column.Type, flags)).SingleLine()
			}
			if app.selectedSQL != "" {
				ui.Text(c, app.localized("SQL 定义")).TextColor(c.Theme().TextMuted)
				ui.Text(c, app.selectedSQL)
			}
		} else {
			ui.Text(c, app.localized("查询列")).TextColor(c.Theme().TextMuted)
			for _, column := range app.activeQuery.columns {
				ui.Text(c, column.Name+" · "+column.DatabaseType).SingleLine()
			}
		}
	})
}
