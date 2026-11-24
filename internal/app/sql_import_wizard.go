package app

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

type sqlImportWizardState struct {
	Open            bool
	Step            int
	Store           connection.SQL
	Importer        connection.TableImporter
	ConnectionID    string
	Driver          connection.Driver
	OpenedPath      string
	Object          connection.Object
	Namespace       string
	NamespaceMode   bool
	TargetObjects   []connection.Object
	TargetChoice    string
	Format          string
	FormatChosen    bool
	JSONObjects     bool
	DelimiterChoice string
	CustomDelimiter string
	HasHeader       bool
	Path            string
	Records         [][]sqlImportCell
	Headers         []string
	Rows            [][]sqlImportCell
	Columns         []connection.Column
	Mapping         []string
	Plan            sqlCSVImportPlan
	Loading         bool
	Executing       bool
	Completed       bool
	Imported        int
	Error           string
}

type sqlImportCell struct {
	value   string
	present bool
	isNull  bool
}

func (app *workbench) closeSQLImportWizard() {
	if app.sqlImportWizard != nil && app.sqlImportWizard.Executing {
		return
	}
	app.sqlImportWizard = nil
	app.dialogOpen = false
}

func (app *workbench) importCSVIntoNamespace(connectionID string, driver connection.Driver, namespace string) {
	if app.busy || app.exporting || app.dialogOpen || app.openedReadOnly {
		return
	}
	if connectionID != app.openedConnectionID && !app.activateLiveConnectionForWorkspace(connectionID) {
		return
	}
	if app.store == nil || app.openedDriver != driver {
		return
	}
	importer, ok := app.store.(connection.TableImporter)
	if !ok {
		app.status = app.localized("当前连接不支持导入数据。")
		return
	}
	state := &sqlImportWizardState{
		Open: true, Store: app.store, Importer: importer, ConnectionID: app.openedConnectionID,
		Driver: driver, OpenedPath: app.openedPath, Namespace: namespace, NamespaceMode: true,
		TargetChoice: app.localized("选择目标表"), Format: "CSV", CustomDelimiter: ";",
		DelimiterChoice: app.localized("逗号 (,)"), HasHeader: true, Loading: true,
	}
	app.sqlImportWizard = state
	app.dialogOpen = true
	store := state.Store
	ctx, cancel := app.withTaskTimeout(20 * time.Second)
	go func() {
		objects, err := readAllSQLObjects(ctx, store)
		cancel()
		app.window.Update(func() {
			if app.sqlImportWizard != state {
				return
			}
			state.Loading = false
			if connectionErr := app.validateSQLImportConnection(state); connectionErr != nil {
				state.Error = connectionErr.Error()
				return
			}
			if err != nil {
				state.Error = safeerror.Display(err)
				return
			}
			for _, object := range objects {
				if object.Type == "table" && sqlObjectMatchesNamespace(string(driver), namespace, object) {
					state.TargetObjects = append(state.TargetObjects, object)
				}
			}
			if len(state.TargetObjects) == 0 {
				state.Error = app.localized("此 Schema 没有可导入数据的表。")
			}
		})
	}()
}

func (app *workbench) selectSQLImportTarget(state *sqlImportWizardState, name string) {
	if state == nil || app.sqlImportWizard != state || state.Loading || state.Executing {
		return
	}
	var selected connection.Object
	for _, object := range state.TargetObjects {
		if object.Name == name {
			selected = object
			break
		}
	}
	state.TargetChoice = name
	state.Columns, state.Headers, state.Rows, state.Mapping = nil, nil, nil, nil
	state.Plan = sqlCSVImportPlan{}
	state.Error = ""
	if selected.Type != "table" {
		state.Object = connection.Object{Schema: state.Namespace}
		return
	}
	state.Object = selected
	state.Loading = true
	store, schema, table := state.Store, selected.Schema, selected.Name
	if state.Driver == connection.DriverSQLite && schema == "main" {
		schema = ""
	}
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		columns, err := store.Columns(ctx, schema, table)
		cancel()
		app.window.Update(func() {
			if app.sqlImportWizard != state {
				return
			}
			state.Loading = false
			if connectionErr := app.validateSQLImportConnection(state); connectionErr != nil {
				state.Error = connectionErr.Error()
				return
			}
			if err != nil {
				state.Error = safeerror.Display(err)
				return
			}
			state.Columns = columns
			if len(state.Records) > 0 {
				app.prepareSQLImportMapping(state)
			}
		})
	}()
}

func (app *workbench) chooseSQLImportSource(state *sqlImportWizardState) {
	if state == nil || app.sqlImportWizard != state || state.Loading || state.Executing {
		return
	}
	state.Loading = true
	state.Error = ""
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: app.window, Title: app.localized("选择导入文件"), ButtonLabel: app.localized("打开"), Multiple: false,
			Filters: []mygo.FileFilter{{Name: app.localized("导入文件"), Extensions: []string{"csv", "tsv", "txt", "json"}}},
		})
		path := ""
		if err == nil && len(paths) > 0 {
			path = paths[0]
		}
		if path == "" {
			app.window.Update(func() {
				if app.sqlImportWizard == state {
					state.Loading = false
					if err != nil {
						state.Error = safeerror.Display(err)
					}
				}
			})
			return
		}
		format, delimiterChoice := state.Format, state.DelimiterChoice
		if !state.FormatChosen {
			format, delimiterChoice = "CSV", app.localized("逗号 (,)")
			switch strings.ToLower(filepath.Ext(path)) {
			case ".tsv":
				format, delimiterChoice = "TSV", app.localized("制表符 (Tab)")
			case ".txt":
				format = "分隔文本"
			case ".json":
				format = "JSON"
			}
		}
		records, jsonObjects, readErr := readSQLImportRecords(path, format, delimiterChoice, state.CustomDelimiter, app.localized)
		columns := state.Columns
		if readErr == nil && len(columns) == 0 {
			ctx, cancel := app.withTaskTimeout(15 * time.Second)
			schema := state.Object.Schema
			if state.Driver == connection.DriverSQLite && schema == "main" {
				schema = ""
			}
			columns, readErr = state.Store.Columns(ctx, schema, state.Object.Name)
			cancel()
		}
		app.window.Update(func() {
			if app.sqlImportWizard != state {
				return
			}
			state.Loading = false
			if err := app.validateSQLImportConnection(state); err != nil {
				state.Error = err.Error()
				return
			}
			if readErr != nil {
				state.Error = safeImportError(readErr)
				return
			}
			state.Path = path
			state.Format, state.DelimiterChoice = format, delimiterChoice
			state.JSONObjects = jsonObjects
			if jsonObjects {
				state.HasHeader = true
			}
			state.Records = records
			state.Columns = columns
			app.prepareSQLImportMapping(state)
		})
	}()
}

func safeImportError(err error) string {
	if err == nil {
		return ""
	}
	return safeerror.Display(err)
}

func (app *workbench) validateSQLImportConnection(state *sqlImportWizardState) error {
	if state == nil || app.store != state.Store || app.openedConnectionID != state.ConnectionID || app.openedDriver != state.Driver || app.openedReadOnly || state.Driver == connection.DriverSQLite && app.openedPath != state.OpenedPath {
		return errors.New(app.localized("连接已变化，导入未执行。"))
	}
	return nil
}

func (app *workbench) reloadSQLImportPreview(state *sqlImportWizardState, advance bool) {
	if state == nil || app.sqlImportWizard != state || state.Loading || state.Path == "" {
		return
	}
	state.Loading, state.Error = true, ""
	path := state.Path
	go func() {
		records, jsonObjects, err := readSQLImportRecords(path, state.Format, state.DelimiterChoice, state.CustomDelimiter, app.localized)
		app.window.Update(func() {
			if app.sqlImportWizard != state {
				return
			}
			state.Loading = false
			if connectionErr := app.validateSQLImportConnection(state); connectionErr != nil {
				state.Error = connectionErr.Error()
				return
			}
			if err != nil {
				state.Error = safeImportError(err)
				return
			}
			state.Records = records
			state.JSONObjects = jsonObjects
			if jsonObjects {
				state.HasHeader = true
			}
			app.prepareSQLImportMapping(state)
			if advance && state.Error == "" {
				state.Step = 2
			}
		})
	}()
}

func (app *workbench) prepareSQLImportMapping(state *sqlImportWizardState) {
	state.Headers, state.Rows, state.Mapping, state.Plan = nil, nil, nil, sqlCSVImportPlan{}
	if len(state.Records) == 0 {
		state.Error = app.localized("导入文件中没有数据。")
		return
	}
	first := state.Records[0]
	if len(first) == 0 {
		state.Error = app.localized("导入文件中没有可用字段。")
		return
	}
	if state.HasHeader || state.JSONObjects {
		state.Headers = make([]string, len(first))
		for index, cell := range first {
			state.Headers[index] = cell.value
		}
		state.Headers[0] = strings.TrimPrefix(state.Headers[0], "\ufeff")
		state.Rows = state.Records[1:]
	} else {
		state.Headers = make([]string, len(first))
		for index := range first {
			state.Headers[index] = fmt.Sprintf(app.localized("列 %d"), index+1)
		}
		state.Rows = state.Records
	}
	state.Mapping = make([]string, len(state.Headers))
	used := make(map[string]bool)
	for sourceIndex, sourceName := range state.Headers {
		for _, target := range state.Columns {
			if !sqlImportColumnWritable(target) || used[target.Name] {
				continue
			}
			if strings.EqualFold(sourceName, target.Name) {
				state.Mapping[sourceIndex] = target.Name
				used[target.Name] = true
				break
			}
		}
		if state.Mapping[sourceIndex] == "" && sourceIndex < len(state.Columns) {
			for _, target := range state.Columns {
				if sqlImportColumnWritable(target) && !used[target.Name] {
					state.Mapping[sourceIndex] = target.Name
					used[target.Name] = true
					break
				}
			}
		}
	}
	state.Error = ""
}

func sqlImportColumnWritable(column connection.Column) bool {
	return column.Hidden == 0 && !strings.Contains(strings.ToLower(column.Extra), "generated")
}

func (app *workbench) buildSQLImportPlan(state *sqlImportWizardState) (sqlCSVImportPlan, error) {
	if len(state.Headers) == 0 || len(state.Rows) == 0 {
		return sqlCSVImportPlan{}, errors.New(app.localized("导入文件中没有数据行。"))
	}
	if len(state.Mapping) != len(state.Headers) {
		return sqlCSVImportPlan{}, errors.New(app.localized("请为导入字段选择目标列。"))
	}
	allowed := make(map[string]bool, len(state.Columns))
	for _, column := range state.Columns {
		if sqlImportColumnWritable(column) {
			allowed[column.Name] = true
		}
	}
	used := make(map[string]bool)
	for index, target := range state.Mapping {
		if target == "" || target == app.localized("跳过字段") {
			continue
		}
		if !allowed[target] {
			return sqlCSVImportPlan{}, fmt.Errorf(app.localized("目标字段 %q 不存在或不能写入。"), target)
		}
		if used[target] {
			return sqlCSVImportPlan{}, fmt.Errorf(app.localized("目标字段 %q 被映射了多次。"), target)
		}
		if strings.TrimSpace(state.Headers[index]) == "" {
			return sqlCSVImportPlan{}, fmt.Errorf(app.localized("源文件第 %d 列的字段名为空。"), index+1)
		}
		used[target] = true
	}
	if len(used) == 0 {
		return sqlCSVImportPlan{}, errors.New(app.localized("至少映射一个导入字段。"))
	}
	if len(state.Rows) > connection.MaxTableImportRows {
		return sqlCSVImportPlan{}, fmt.Errorf(app.localized("单次最多导入 %d 行，请拆分文件后重试。"), connection.MaxTableImportRows)
	}
	if len(state.Headers) > connection.MaxTableImportColumns {
		return sqlCSVImportPlan{}, fmt.Errorf(app.localized("每次导入最多支持 %d 个字段。"), connection.MaxTableImportColumns)
	}
	changes := make([]connection.TableChange, 0, len(state.Rows))
	preview := strings.Builder{}
	preview.WriteString(fmt.Sprintf(app.localized("目标：%s\n"), qualifiedObjectName(state.Object)))
	for rowIndex, row := range state.Rows {
		if len(row) != len(state.Headers) {
			line := rowIndex + 1
			if state.HasHeader {
				line++
			}
			return sqlCSVImportPlan{}, fmt.Errorf(app.localized("第 %d 行有 %d 个字段，映射定义有 %d 个字段。"), line, len(row), len(state.Headers))
		}
		values := make(map[string]any, len(used))
		for index, target := range state.Mapping {
			if target == "" || target == app.localized("跳过字段") {
				continue
			}
			if !row[index].present {
				continue
			}
			if row[index].isNull {
				values[target] = nil
			} else {
				values[target] = row[index].value
			}
		}
		changes = append(changes, connection.TableChange{Kind: connection.InsertRow, Values: values})
		if rowIndex < 5 && preview.Len() < 3800 {
			fmt.Fprintf(&preview, "%d. ", rowIndex+1)
			first := true
			for index, target := range state.Mapping {
				if target == "" || target == app.localized("跳过字段") {
					continue
				}
				if !first {
					preview.WriteString(" · ")
				}
				first = false
				cell := row[index]
				value := cell.value
				switch {
				case !cell.present:
					value = app.localized("使用数据库默认值")
				case cell.isNull:
					value = "NULL"
				}
				fmt.Fprintf(&preview, "%s=%s", target, truncateText(value, 100))
			}
			preview.WriteByte('\n')
		}
	}
	if len(changes) > 5 {
		fmt.Fprintf(&preview, "… %d\n", len(changes)-5)
	}
	return sqlCSVImportPlan{Changes: changes, Preview: strings.TrimSpace(preview.String())}, nil
}

func (app *workbench) startSQLImportExecution(state *sqlImportWizardState) {
	if state == nil || app.sqlImportWizard != state || state.Executing || app.busy {
		return
	}
	if err := app.validateSQLImportConnection(state); err != nil {
		state.Error = err.Error()
		return
	}
	plan, err := app.buildSQLImportPlan(state)
	if err != nil {
		state.Error = err.Error()
		state.Step = 2
		return
	}
	state.Plan, state.Error, state.Executing, state.Step = plan, "", true, 4
	app.applySQLCSVImport(state.Store, state.Importer, state.ConnectionID, state.Object, plan.Changes)
}

func readSQLDelimitedRecords(path string, delimiter rune, localized func(string) string) ([][]sqlImportCell, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSQLCSVImportBytes {
		return nil, fmt.Errorf(localized("导入文件超过 %d MiB。"), maxSQLCSVImportBytes>>20)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	records := make([][]sqlImportCell, 0, 64)
	for {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf(localized("读取导入文件：%w"), readErr)
		}
		if len(record) > connection.MaxTableImportColumns {
			return nil, fmt.Errorf(localized("每次导入最多支持 %d 个字段。"), connection.MaxTableImportColumns)
		}
		if len(records) >= connection.MaxTableImportRows+1 {
			return nil, fmt.Errorf(localized("单次最多导入 %d 行，请拆分文件后重试。"), connection.MaxTableImportRows)
		}
		cells := make([]sqlImportCell, len(record))
		for index, value := range record {
			cells[index] = sqlImportCell{value: value, present: true}
		}
		records = append(records, cells)
	}
	if len(records) > 0 && len(records[0]) > 0 {
		records[0][0].value = strings.TrimPrefix(records[0][0].value, "\ufeff")
	}
	return records, nil
}

func readSQLImportRecords(path, format, delimiterChoice, customDelimiter string, localized func(string) string) ([][]sqlImportCell, bool, error) {
	if format == "JSON" {
		records, objectRows, err := readSQLJSONRecords(path, localized)
		return records, objectRows, err
	}
	delimiter, err := sqlImportDelimiter(delimiterChoice, format, customDelimiter, localized)
	if err != nil {
		return nil, false, err
	}
	records, err := readSQLDelimitedRecords(path, delimiter, localized)
	return records, false, err
}

func readSQLJSONRecords(path string, localized func(string) string) ([][]sqlImportCell, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > maxSQLCSVImportBytes {
		return nil, false, fmt.Errorf(localized("导入文件超过 %d MiB。"), maxSQLCSVImportBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var root json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, fmt.Errorf(localized("读取导入文件：%w"), err)
	}
	var values []json.RawMessage
	if len(root) > 0 && root[0] == '[' {
		if err := json.Unmarshal(root, &values); err != nil {
			return nil, false, fmt.Errorf(localized("读取导入文件：%w"), err)
		}
	} else if len(root) > 0 && root[0] == '{' {
		values = []json.RawMessage{root}
	} else {
		return nil, false, errors.New(localized("JSON 顶层必须是对象或数组。"))
	}
	if len(values) == 0 || len(values) > connection.MaxTableImportRows+1 {
		if len(values) == 0 {
			return nil, false, errors.New(localized("导入文件中没有数据。"))
		}
		return nil, false, fmt.Errorf(localized("单次最多导入 %d 行，请拆分文件后重试。"), connection.MaxTableImportRows)
	}
	objectRows := true
	objects := make([]map[string]json.RawMessage, len(values))
	keys := make(map[string]bool)
	for index, value := range values {
		if len(value) == 0 || value[0] != '{' {
			objectRows = false
			break
		}
		if err := json.Unmarshal(value, &objects[index]); err != nil {
			return nil, false, fmt.Errorf(localized("读取导入文件：%w"), err)
		}
		for key := range objects[index] {
			keys[key] = true
			if len(keys) > connection.MaxTableImportColumns {
				return nil, false, fmt.Errorf(localized("每次导入最多支持 %d 个字段。"), connection.MaxTableImportColumns)
			}
		}
	}
	if objectRows {
		headers := make([]string, 0, len(keys))
		for key := range keys {
			headers = append(headers, key)
		}
		sort.Strings(headers)
		records := make([][]sqlImportCell, 1, len(objects)+1)
		records[0] = make([]sqlImportCell, len(headers))
		for index, header := range headers {
			records[0][index] = sqlImportCell{value: header, present: true}
		}
		for _, object := range objects {
			row := make([]sqlImportCell, len(headers))
			for column, header := range headers {
				value, exists := object[header]
				if !exists {
					row[column] = sqlImportCell{}
					continue
				}
				if strings.TrimSpace(string(value)) == "null" {
					row[column] = sqlImportCell{present: true, isNull: true}
					continue
				}
				var text string
				if err := json.Unmarshal(value, &text); err == nil {
					row[column] = sqlImportCell{value: text, present: true}
				} else {
					row[column] = sqlImportCell{value: string(value), present: true}
				}
			}
			records = append(records, row)
		}
		return records, true, nil
	}
	records := make([][]sqlImportCell, 0, len(values))
	for _, value := range values {
		var row []json.RawMessage
		if err := json.Unmarshal(value, &row); err != nil {
			return nil, false, errors.New(localized("JSON 数组中的每一项都必须是对象或数组。"))
		}
		cells := make([]sqlImportCell, len(row))
		if len(cells) > connection.MaxTableImportColumns {
			return nil, false, fmt.Errorf(localized("每次导入最多支持 %d 个字段。"), connection.MaxTableImportColumns)
		}
		for index, cell := range row {
			if strings.TrimSpace(string(cell)) == "null" {
				cells[index] = sqlImportCell{present: true, isNull: true}
				continue
			}
			var text string
			if err := json.Unmarshal(cell, &text); err == nil {
				cells[index] = sqlImportCell{value: text, present: true}
			} else {
				cells[index] = sqlImportCell{value: string(cell), present: true}
			}
		}
		records = append(records, cells)
	}
	return records, false, nil
}

func sqlImportDelimiter(choice, format, custom string, localized func(string) string) (rune, error) {
	if format == "分隔文本" {
		delimiters := []rune(custom)
		if len(delimiters) != 1 || delimiters[0] == '\r' || delimiters[0] == '\n' || delimiters[0] == '"' {
			return 0, errors.New(localized("自定义分隔符必须是单个非引号字符。"))
		}
		return delimiters[0], nil
	}
	switch choice {
	case localized("制表符 (Tab)"):
		return '\t', nil
	case localized("分号 (;)"):
		return ';', nil
	case localized("竖线 (|)"):
		return '|', nil
	default:
		return ',', nil
	}
}

func (app *workbench) sqlImportWizardModal(c *ui.Context) {
	state := app.sqlImportWizard
	if state == nil {
		return
	}
	if ui.Modal(c, &state.Open, func() {
		ui.Column(c).Width(760).MaxHeightPercent(90).Padding(14).Gap(10).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, app.localized("导入数据")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
				close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Size(30, 30).Padding(4)
				close.Disabled(state.Executing)
				if close.Clicked() {
					app.closeSQLImportWizard()
				}
			})
			targetLabel := state.Namespace
			if state.Object.Type == "table" {
				targetLabel = qualifiedObjectName(state.Object)
			}
			ui.Row(c).FillWidth().Gap(8).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("目标表")).TextColor(c.Theme().TextMuted)
				ui.Text(c, app.openedLabel+" / "+targetLabel).Bold().SingleLine()
				ui.Spacer(c)
				if state.Path != "" {
					ui.Text(c, filepath.Base(state.Path)).TextColor(c.Theme().TextMuted).SingleLine()
				}
			})
			ui.Row(c).FillWidth().Gap(4).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				for index, label := range []string{"来源", "选项", "映射", "确认", "执行"} {
					step := index + 1
					text := fmt.Sprintf("%d  %s", step, app.localized(label))
					item := ui.Text(c, text).Padding(6, 8).Grow(1)
					if state.Step == index {
						item.Bold().TextColor(c.Theme().Accent)
					} else if state.Step < index {
						item.TextColor(c.Theme().TextMuted)
					}
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(280).Children(func() {
				ui.Column(c).FillWidth().Padding(12, 4).Gap(12).Children(func() {
					app.sqlImportWizardStep(c, state)
					if state.Error != "" {
						ui.Text(c, state.Error).TextColor(c.Theme().Danger).Selectable()
					}
				})
			})
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if state.Step > 0 && !state.Executing && !state.Completed {
					back := ui.Button(c, app.localized("上一步"))
					back.Disabled(state.Loading)
					if back.Clicked() {
						state.Step--
						state.Error = ""
					}
				}
				switch {
				case state.Completed:
					close := ui.PrimaryButton(c, app.localized("完成"))
					if close.Clicked() {
						app.closeSQLImportWizard()
					}
				case state.Step == 4 && !state.Executing:
					close := ui.Button(c, app.localized("关闭"))
					if close.Clicked() {
						app.closeSQLImportWizard()
					}
				case state.Step == 3:
					apply := ui.PrimaryButton(c, app.localized("确认并导入"))
					apply.Disabled(state.Loading || state.Executing || len(state.Plan.Changes) == 0 || app.busy)
					if apply.Clicked() {
						app.startSQLImportExecution(state)
					}
				case state.Step < 3:
					next := ui.PrimaryButton(c, app.localized("下一步"))
					next.Disabled(state.Loading || !app.sqlImportWizardCanAdvance(state))
					if next.Clicked() {
						app.advanceSQLImportWizard(state)
					}
				}
			})
		})
	}).Dismissed() {
		if state.Executing {
			state.Open = true
		} else {
			app.closeSQLImportWizard()
		}
	}
}

func (app *workbench) sqlImportWizardCanAdvance(state *sqlImportWizardState) bool {
	if state == nil || state.Object.Type != "table" || state.Path == "" || len(state.Records) == 0 {
		return false
	}
	if state.Step == 2 {
		_, err := app.buildSQLImportPlan(state)
		return err == nil
	}
	return state.Step < 3
}

func (app *workbench) advanceSQLImportWizard(state *sqlImportWizardState) {
	if state == nil || app.sqlImportWizard != state || state.Loading {
		return
	}
	switch state.Step {
	case 0:
		if state.Path != "" {
			state.Step = 1
		}
	case 1:
		app.reloadSQLImportPreview(state, true)
	case 2:
		plan, err := app.buildSQLImportPlan(state)
		if err != nil {
			state.Error = safeerror.Display(err)
			return
		}
		state.Plan, state.Error, state.Step = plan, "", 3
	}
}

func (app *workbench) sqlImportWizardStep(c *ui.Context, state *sqlImportWizardState) {
	switch state.Step {
	case 0:
		ui.Text(c, app.localized("选择文件格式并指定要导入的文件。导入会先预览和映射，不会直接写入。 ")).TextColor(c.Theme().TextMuted)
		if state.NamespaceMode {
			options := []string{app.localized("选择目标表")}
			for _, object := range state.TargetObjects {
				options = append(options, object.Name)
			}
			if state.TargetChoice == "" {
				state.TargetChoice = options[0]
			}
			target := ui.Select(c, &state.TargetChoice, options).Label(app.localized("目标表")).FillWidth()
			target.Disabled(state.Loading || len(state.TargetObjects) == 0)
			if target.Changed() {
				app.selectSQLImportTarget(state, state.TargetChoice)
			}
		}
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			for _, format := range []struct{ label, description string }{
				{"CSV", "逗号分隔文本"}, {"TSV", "制表符分隔文本"}, {"分隔文本", "自定义分隔符"}, {"JSON", "JSON 对象或数组"},
			} {
				format := format
				card := ui.Button(c, "").Label(app.localized(format.label)).Tooltip(app.localized(format.description)).Grow(1).Padding(10).Selected(state.Format == format.label)
				card.Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Text(c, app.localized(format.label)).Bold()
						ui.Text(c, app.localized(format.description)).TextColor(c.Theme().TextMuted)
					})
				})
				if card.Clicked() {
					state.Format = format.label
					state.FormatChosen = true
					state.JSONObjects = false
					switch format.label {
					case "TSV":
						state.DelimiterChoice = app.localized("制表符 (Tab)")
					default:
						state.DelimiterChoice = app.localized("逗号 (,)")
					}
				}
			}
		})
		if state.Path != "" {
			ui.Text(c, app.localized("已选择文件：")+state.Path).Selectable()
		}
		choose := ui.PrimaryButton(c, app.localized("选择文件"))
		choose.Disabled(state.Loading || state.Executing || state.Object.Type != "table")
		if choose.Clicked() {
			app.chooseSQLImportSource(state)
		}
		if state.Loading {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, app.localized("正在读取导入文件…")).TextColor(c.Theme().TextMuted)
			})
		}
	case 1:
		ui.Text(c, app.localized("设置分隔符和首行规则，再读取预览。 ")).TextColor(c.Theme().TextMuted)
		if state.Format == "JSON" {
			ui.Text(c, app.localized("JSON 导入支持对象数组和二维数组；对象数组会按键名映射字段。 ")).TextColor(c.Theme().TextMuted)
		} else if state.Format == "分隔文本" {
			delimiter := ui.TextInput(c, &state.CustomDelimiter).Label(app.localized("自定义分隔符")).Placeholder(";").Width(220)
			delimiter.Disabled(state.Loading)
		} else {
			formats := []string{app.localized("逗号 (,)"), app.localized("制表符 (Tab)"), app.localized("分号 (;)"), app.localized("竖线 (|)")}
			choice := ui.Select(c, &state.DelimiterChoice, formats).Label(app.localized("字段分隔符")).Width(260)
			choice.Disabled(state.Loading)
		}
		if !state.JSONObjects {
			header := ui.Checkbox(c, &state.HasHeader, app.localized("第一行是字段名"))
			header.Disabled(state.Loading || state.Format == "JSON" && state.JSONObjects)
		}
		ui.Text(c, fmt.Sprintf(app.localized("文件最多 %d MiB，每次最多导入 %d 行。"), maxSQLCSVImportBytes>>20, connection.MaxTableImportRows)).TextColor(c.Theme().TextMuted)
		if state.Loading {
			ui.Text(c, app.localized("正在生成字段预览…")).TextColor(c.Theme().TextMuted)
		}
	case 2:
		ui.Text(c, app.localized("将源文件字段映射到目标表列；未映射的字段会跳过。 ")).TextColor(c.Theme().TextMuted)
		if _, err := app.buildSQLImportPlan(state); err != nil {
			ui.Text(c, err.Error()).TextColor(c.Theme().Warning)
		}
		ui.Column(c).Gap(6).Children(func() {
			options := []string{app.localized("跳过字段")}
			for _, column := range state.Columns {
				if sqlImportColumnWritable(column) {
					options = append(options, column.Name)
				}
			}
			for index := range state.Headers {
				index := index
				sourceName := state.Headers[index]
				if sourceName == "" {
					sourceName = app.localized("（空字段名）")
				}
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, sourceName).Width(260).SingleLine()
					ui.Text(c, "→").TextColor(c.Theme().TextMuted)
					picker := ui.Select(c, &state.Mapping[index], options).Label(app.localized("目标列")).Grow(1)
					picker.Disabled(state.Loading || state.Executing)
				})
			}
		})
	case 3:
		plan, err := app.buildSQLImportPlan(state)
		if err != nil {
			state.Error = err.Error()
			return
		}
		state.Plan = plan
		mappedCount := 0
		for _, target := range state.Mapping {
			if target != "" && target != app.localized("跳过字段") {
				mappedCount++
			}
		}
		ui.Text(c, fmt.Sprintf(app.localized("文件：%s · %d 行 · %d 个映射字段"), filepath.Base(state.Path), len(plan.Changes), mappedCount)).Bold()
		ui.Text(c, app.localized("确认后将在一个事务中写入目标表；如果执行失败，事务会回滚。 ")).TextColor(c.Theme().TextMuted)
		ui.Text(c, plan.Preview).Padding(10).FillWidth().Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Selectable()
	case 4:
		switch {
		case state.Executing:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(18, 18)
				ui.Text(c, app.localized("正在导入数据…"))
			})
		case state.Completed:
			ui.Text(c, fmt.Sprintf(app.localized("已向表 %s 导入 %d 行。"), qualifiedObjectName(state.Object), state.Imported)).TextColor(c.Theme().Success)
		default:
			ui.Text(c, app.localized("导入未执行。可以返回确认步骤调整字段映射。 ")).TextColor(c.Theme().TextMuted)
		}
	}
}
