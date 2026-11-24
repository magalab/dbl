package app

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safefile"
	"github.com/magalab/dbl/internal/sqlstream"
)

const maxSQLCSVImportBytes = 16 << 20

type objectExportFormat uint8

const (
	objectExportCSV objectExportFormat = iota
	objectExportJSON
	objectExportSQL
)

func (app *workbench) exportSQLObject(object connection.Object, format objectExportFormat) {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil {
		return
	}
	store, connectionID, driver, openedPath := app.store, app.openedConnectionID, app.openedDriver, app.openedPath
	statement := objectDataQuery(app, object)
	var streamFormat sqlstream.ExportFormat
	extension := ".csv"
	if format == objectExportJSON {
		streamFormat, extension = sqlstream.JSON, ".json"
	} else if format == objectExportSQL {
		extension = ".sql"
	}
	defaultPath := safeExportName(object.Name) + extension
	filterName := strings.TrimPrefix(extension, ".")
	app.dialogOpen = true
	app.status = app.localized("请选择导出文件保存位置…")
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: app.window, Title: app.localized("导出") + " " + strings.ToUpper(filterName),
			ButtonLabel: app.localized("导出"), DefaultPath: defaultPath,
			Filters: []mygo.FileFilter{{Name: strings.ToUpper(filterName), Extensions: []string{filterName}}},
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("导出失败", err)
				return
			}
			if path == "" {
				app.status = app.localized("已取消导出。")
				return
			}
			if app.store != store || app.openedConnectionID != connectionID || app.openedDriver != driver || driver == connection.DriverSQLite && app.openedPath != openedPath {
				app.status = app.localized("导出来源已变化，请重新选择结果后导出。")
				return
			}
			if filepath.Ext(path) == "" {
				path += extension
			}
			if format == objectExportSQL {
				app.exportSQLInsertsToPath(store, driver, object, path)
				return
			}
			app.exportResultToPath(store, statement, path, streamFormat)
		})
	}()
}

func (app *workbench) exportSQLInsertsToPath(store connection.SQL, driver connection.Driver, object connection.Object, path string) {
	ctx, cancel := app.withTaskTimeout(exportTimeout)
	app.cancelExport = cancel
	app.exporting = true
	app.status = app.localized("正在导出表数据为 SQL…")
	go func() {
		file, err := safefile.CreateExport(path)
		var count int64
		if err == nil {
			count, err = writeSQLInsertExport(ctx, store, driver, object, file.File())
			if err == nil {
				err = file.Commit()
			}
			if err != nil {
				if cleanupErr := file.Abort(); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
				}
			}
		}
		canceled := err != nil && (errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled))
		cancel()
		app.window.Update(func() {
			app.cancelExport = nil
			app.exporting = false
			if canceled {
				app.status = app.localized("导出已取消，临时文件已清理。")
			} else if err != nil && safefile.IsPublished(err) {
				app.status = fmt.Sprintf(app.localized("导出文件已完整生成到 %s，但目录同步失败，断电后是否保留无法确认。"), path)
			} else if err != nil {
				app.reportError("导出失败", err)
			} else {
				app.status = fmt.Sprintf(app.localized("已导出 %d 行到 %s"), count, path)
			}
		})
	}()
}

func writeSQLInsertExport(ctx context.Context, store connection.SQL, driver connection.Driver, object connection.Object, output io.Writer) (int64, error) {
	return writeSQLInsertRows(ctx, store, driver, object, output, true)
}

func writeSQLInsertRows(ctx context.Context, store connection.SQL, driver connection.Driver, object connection.Object, output io.Writer, includeComment bool) (int64, error) {
	schema := object.Schema
	if driver == connection.DriverSQLite && schema == "main" {
		schema = ""
	}
	tableColumns, err := store.Columns(ctx, schema, object.Name)
	if err != nil {
		return 0, err
	}
	selected := make([]connection.Column, 0, len(tableColumns))
	identityAlways := false
	for _, column := range tableColumns {
		switch driver {
		case connection.DriverSQLite:
			if column.Hidden != 0 {
				continue
			}
		case connection.DriverMySQL:
			if strings.Contains(strings.ToLower(column.Extra), "generated") {
				continue
			}
		case connection.DriverPostgreSQL:
			if column.Extra == "generated" {
				continue
			}
			if column.Extra == "identity always" {
				identityAlways = true
			}
		}
		selected = append(selected, column)
	}
	table := quoteIdentifierForDriver(driver, object.Name)
	if object.Schema != "" {
		table = quoteIdentifierForDriver(driver, object.Schema) + "." + table
	}
	query := "SELECT 1 FROM " + table + ";\n"
	columnNames := make([]string, len(selected))
	if len(selected) > 0 {
		for index, column := range selected {
			columnNames[index] = quoteIdentifierForDriver(driver, column.Name)
		}
		query = "SELECT " + strings.Join(columnNames, ", ") + " FROM " + table + ";\n"
	}
	cursor, err := store.StartQuery(ctx, query)
	if err != nil {
		return 0, err
	}
	defer cursor.Close()
	resultColumns := cursor.Columns()
	if len(resultColumns) == 0 {
		return 0, sqlstream.ErrNoResultColumns
	}
	writer := bufio.NewWriter(output)
	if includeComment {
		if _, err := fmt.Fprintf(writer, "-- Data for %s\n", table); err != nil {
			return 0, err
		}
	}
	var count int64
	for {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		rows, done, readErr := cursor.NextBatch(100)
		if readErr != nil {
			return count, readErr
		}
		for _, row := range rows {
			if len(selected) == 0 {
				statement := "INSERT INTO " + table + " DEFAULT VALUES;\n"
				if driver == connection.DriverMySQL {
					statement = "INSERT INTO " + table + " () VALUES ();\n"
				}
				if _, err := io.WriteString(writer, statement); err != nil {
					return count, err
				}
				count++
				continue
			}
			values := make([]string, len(selected))
			for i, column := range selected {
				if i < len(row) {
					values[i] = sqlDumpLiteral(row[i], column.Type, driver)
				} else {
					values[i] = "NULL"
				}
			}
			identityClause := ""
			if identityAlways {
				identityClause = " OVERRIDING SYSTEM VALUE"
			}
			if _, err := fmt.Fprintf(writer, "INSERT INTO %s (%s)%s VALUES (%s);\n", table, strings.Join(columnNames, ", "), identityClause, strings.Join(values, ", ")); err != nil {
				return count, err
			}
			count++
		}
		if done {
			if err := writer.Flush(); err != nil {
				return count, err
			}
			return count, nil
		}
		if err := writer.Flush(); err != nil {
			return count, err
		}
	}
}

func quoteIdentifierForDriver(driver connection.Driver, name string) string {
	quote := `"`
	if driver == connection.DriverMySQL {
		quote = "`"
	}
	return quote + strings.ReplaceAll(name, quote, quote+quote) + quote
}

func sqlDumpLiteral(value any, databaseType string, driver connection.Driver) string {
	switch value := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		typeName := strings.ToUpper(databaseType)
		binary := strings.Contains(typeName, "BLOB") || strings.Contains(typeName, "BINARY") || strings.Contains(typeName, "BYTEA") || !utf8.Valid(value)
		if binary {
			hexValue := hex.EncodeToString(value)
			if driver == connection.DriverPostgreSQL {
				return "decode('" + hexValue + "', 'hex')"
			}
			return "X'" + hexValue + "'"
		}
		return "'" + strings.ReplaceAll(string(value), "'", "''") + "'"
	case string:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case time.Time:
		return "'" + value.Format(time.RFC3339Nano) + "'"
	case bool:
		if driver == connection.DriverPostgreSQL {
			if value {
				return "TRUE"
			}
			return "FALSE"
		}
		if value {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(value)
	}
}

func (app *workbench) importCSVIntoTable(object connection.Object) {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || app.openedReadOnly || object.Type != "table" {
		return
	}
	store, connectionID, driver, openedPath := app.store, app.openedConnectionID, app.openedDriver, app.openedPath
	importer, ok := store.(connection.TableImporter)
	if !ok {
		app.status = app.localized("当前连接不支持导入数据。")
		return
	}
	app.sqlImportWizard = &sqlImportWizardState{
		Open: true, Store: store, Importer: importer, ConnectionID: connectionID,
		Driver: driver, OpenedPath: openedPath, Object: object,
		TargetChoice: object.Name, Format: "CSV", CustomDelimiter: ";", DelimiterChoice: app.localized("逗号 (,)"), HasHeader: true,
	}
	app.dialogOpen = true
}

type sqlCSVImportPlan struct {
	Changes []connection.TableChange
	Preview string
}

func (app *workbench) applySQLCSVImport(store connection.SQL, importer connection.TableImporter, connectionID string, object connection.Object, changes []connection.TableChange) {
	if app.busy || app.store != store || app.openedConnectionID != connectionID || app.openedReadOnly || len(changes) == 0 {
		return
	}
	app.busy = true
	app.status = app.localized("正在导入数据…")
	wizard := app.sqlImportWizard
	ctx, cancel := app.withTaskTimeout(3 * time.Minute)
	var cursor *sqlstream.Cursor
	var stopReading func()
	if app.cursor != nil && app.activeQuery != nil && app.activeQuery.queryConnectionID == connectionID {
		cursor, stopReading = app.cursor, app.cancelQuery
		app.activeQuery.streamStopped = true
		app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	}
	go func() {
		defer cancel()
		var err error
		if stopReading != nil {
			stopReading()
		}
		if cursor != nil {
			err = cursor.Close()
		}
		if err == nil {
			err = importer.ImportTableRows(ctx, object, changes)
		}
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != connectionID || app.store != store {
				app.status = app.localized("连接已变化，导入结果未关联当前连接。")
				if app.sqlImportWizard == wizard && wizard != nil {
					wizard.Executing = false
					wizard.Error = app.localized("连接已变化，导入未执行。")
				}
				return
			}
			if err != nil {
				if app.sqlImportWizard == wizard && wizard != nil {
					wizard.Executing = false
					wizard.Error = err.Error()
				}
				app.reportError("导入数据失败", err)
				return
			}
			app.status = fmt.Sprintf(app.localized("已向表 %s 导入 %d 行。"), qualifiedObjectName(object), len(changes))
			if app.sqlImportWizard == wizard && wizard != nil {
				wizard.Executing = false
				wizard.Completed = true
				wizard.Imported = len(changes)
			}
			if app.activeQuery != nil && app.activeQuery.dataView && app.activeQuery.dataConnectionID == connectionID && app.activeQuery.dataObject.Name == object.Name && app.activeQuery.dataObject.Schema == object.Schema {
				app.refreshDataWorkspace()
			}
		})
	}()
}
