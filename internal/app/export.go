package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safefile"
	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) exportResult(format sqlstream.ExportFormat) {
	workspace := app.activeQuery
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || workspace == nil {
		return
	}
	statement := strings.TrimSpace(workspace.resultSQL)
	if statement == "" {
		app.status = app.localized("当前结果没有可导出的 SQL。")
		return
	}
	if workspace.resultConnectionID == "" || workspace.resultConnectionID != app.openedConnectionID {
		app.status = app.localized("结果来自另一连接；请先重新连接来源数据库，再导出。")
		return
	}
	if app.openedDriver == connection.DriverSQLite && (workspace.resultPath == "" || workspace.resultPath != app.openedPath) {
		app.status = app.localized("结果来自另一 SQLite 文件；请先重新打开来源文件，再导出。")
		return
	}
	if len(workspace.columns) == 0 {
		app.status = app.localized("当前结果没有可导出的列。")
		return
	}
	store, connectionID, driver, resultPath, openedPath := app.store, app.openedConnectionID, app.openedDriver, workspace.resultPath, app.openedPath
	name := workspace.title
	if workspace.dataView && workspace.dataObject.Name != "" {
		name = workspace.dataObject.Name
	}
	defaultPath := safeExportName(name) + exportExtension(format)
	filterName := strings.TrimPrefix(exportExtension(format), ".")
	dialogTitle := app.localized("导出") + " " + strings.ToUpper(filterName)
	dialogButton := app.localized("导出")
	app.dialogOpen = true
	app.status = app.localized("请选择导出文件保存位置…")
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      app.window,
			Title:       dialogTitle,
			ButtonLabel: dialogButton,
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: strings.ToUpper(filterName), Extensions: []string{filterName}}},
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("选择导出文件位置失败", err)
				return
			}
			if path == "" {
				app.status = app.localized("已取消导出。")
				return
			}
			if app.activeQuery != workspace || app.store != store || app.openedConnectionID != connectionID || app.openedDriver != driver || driver == connection.DriverSQLite && (resultPath == "" || resultPath != openedPath || app.openedPath != openedPath) {
				app.status = app.localized("导出来源已变化，请重新选择结果后导出。")
				return
			}
			if filepath.Ext(path) == "" {
				path += exportExtension(format)
			}
			app.exportResultToPath(store, statement, path, format)
		})
	}()
}

func (app *workbench) exportResultToPath(store connection.SQL, statement, path string, format sqlstream.ExportFormat) {
	ctx, cancel := app.withTaskTimeout(exportTimeout)
	app.cancelExport = cancel
	app.exporting = true
	app.status = app.localized("正在只读重跑查询并导出完整结果…")
	go func() {
		file, err := safefile.CreateExport(path)
		var count int64
		var cleanupErr error
		if err == nil {
			count, err = store.Export(ctx, statement, format, file.File())
			if err == nil {
				err = file.Commit()
			}
			if err != nil {
				cleanupErr = file.Abort()
				if cleanupErr != nil {
					err = errors.Join(err, fmt.Errorf("清理临时导出文件失败: %w", cleanupErr))
				}
			}
		}
		canceled := err != nil && cleanupErr == nil && (errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled))
		timedOut := err != nil && cleanupErr == nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded))
		cancel()
		app.window.Update(func() {
			app.cancelExport = nil
			app.exporting = false
			switch {
			case canceled:
				app.status = app.localized("导出已取消，临时文件已清理。")
			case timedOut:
				app.status = fmt.Sprintf("导出超过 %s，临时文件已清理。", exportTimeout)
			case err != nil && safefile.IsPublished(err):
				app.status = fmt.Sprintf("导出文件已完整生成到 %s，但目录同步失败，断电后是否保留无法确认。", path)
			case err != nil:
				app.reportError("导出失败", err)
			default:
				app.status = fmt.Sprintf("已导出 %d 行到 %s", count, path)
			}
		})
	}()
}

func (app *workbench) addResultExportMenu(menu *ui.Menu, workspace *queryWorkspace) {
	menu.Submenu(app.localized("导出结果"), func(submenu *ui.Menu) {
		app.addResultExportFormats(submenu, workspace)
	})
}

func (app *workbench) addResultExportFormats(menu *ui.Menu, workspace *queryWorkspace) {
	disabled := app.resultExportDisabled(workspace)
	if menu.Item(app.localized("导出为 CSV…")).Disabled(disabled).Chosen() {
		app.exportResult(sqlstream.CSV)
	}
	if menu.Item(app.localized("导出为 JSON…")).Disabled(disabled).Chosen() {
		app.exportResult(sqlstream.JSON)
	}
}

func (app *workbench) resultExportDisabled(workspace *queryWorkspace) bool {
	disabled := app.busy || app.exporting || app.dialogOpen || app.store == nil || workspace == nil || len(workspace.columns) == 0 || strings.TrimSpace(workspace.resultSQL) == "" || workspace.resultConnectionID == "" || workspace.resultConnectionID != app.openedConnectionID
	if workspace != nil && app.openedDriver == connection.DriverSQLite && (workspace.resultPath == "" || workspace.resultPath != app.openedPath) {
		disabled = true
	}
	return disabled
}

func exportExtension(format sqlstream.ExportFormat) string {
	if format == sqlstream.JSON {
		return ".json"
	}
	return ".csv"
}

func safeExportName(name string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	name = strings.Trim(builder.String(), "-. ")
	if name == "" {
		return "query-results"
	}
	return name
}

func formatCell(value any, databaseType string) string {
	switch value := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		typ := strings.ToUpper(databaseType)
		if typ == "" || strings.Contains(typ, "BLOB") || strings.Contains(typ, "BINARY") || strings.Contains(typ, "BYTEA") || !utf8.Valid(value) {
			return "base64:" + base64.StdEncoding.EncodeToString(value)
		}
		return strings.ToValidUTF8(string(value), "�")
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}
