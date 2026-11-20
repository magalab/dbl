package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/connection"
	mongoadapter "github.com/magalab/dbl/internal/mongodb"
	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) openMongo(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen || profile.Driver != preferences.DriverMongoDB {
		return
	}
	app.stageActiveDataWorkspaceDraft()
	app.busy = true
	if app.etcdCancel != nil {
		app.etcdCancel()
		app.etcdCancel = nil
		app.etcdWatching = false
	}
	app.status = app.localized("正在连接 MongoDB…")
	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已切换数据库，停止后续读取")
	}
	app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	if oldCancel != nil {
		oldCancel()
	}
	ctx, cancel := app.withTaskTimeout(25 * time.Second)
	app.mongoCancel = cancel
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		defer cancel()
		adapter, err := (mongoadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
		var docs connection.Documents
		if err == nil {
			var ok bool
			docs, ok = adapter.(connection.Documents)
			if !ok {
				err = errors.New("MongoDB adapter does not provide document capabilities")
			}
		}
		var databases []string
		if err == nil {
			databases, err = docs.Databases(ctx)
			if err != nil && profile.Database != "" {
				databases = []string{profile.Database}
				err = nil
			}
		}

		if err != nil && adapter != nil {
			_ = adapter.Close()
		}
		app.window.Update(func() {
			app.busy = false
			app.mongoCancel = nil
			app.finishWelcomeConnectionAttempt(profile.ID, err)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("MongoDB 连接已取消。")
					return
				}
				app.reportError("连接 MongoDB 失败", err)
				return
			}
			app.registerLiveConnection("mongodb:"+profile.ID, docs, profile)
			app.store, app.keyStore, app.documentStore, app.etcdStore = nil, nil, docs, nil
			app.mongoDatabases, app.mongoCollections = databases, nil
			app.mongoIndexes = nil
			app.mongoIndexError = ""
			app.mongoIndexesLoading = false
			app.mongoSelectedDB, app.mongoSelectedColl = "", ""
			app.mongoDocuments = nil
			app.mongoNextOffset = 0
			app.mongoHasMore = false
			app.mongoSelectedID = ""
			app.mongoOriginalJSON = ""
			app.mongoEditor = nil
			app.mongoDocumentTruncated = false
			app.mongoShowDiff = false
			app.mongoTreeExpanded = make(map[string]bool)
			app.openedDriver = connection.DriverMongoDB
			app.openedConnectionID = "mongodb:" + profile.ID
			app.openedLabel = profile.Name + " · " + profile.Host
			app.openedEnvironment = profile.Environment
			app.openedReadOnly = profile.ReadOnly
			app.status = app.localized("MongoDB 已连接。")
			app.activateConnectionWorkspace(app.openedConnectionID)
		})
	}()
}

func (app *workbench) selectMongoDatabase(database string) {
	if app.busy || app.documentStore == nil {
		return
	}
	docs := app.documentStore
	app.busy = true
	app.status = app.localized("正在读取 MongoDB 集合…")
	ctx, cancel := app.withTaskTimeout(15 * time.Second)
	app.mongoCancel = cancel
	go func() {
		defer cancel()
		collections, err := docs.Collections(ctx, database)
		app.window.Update(func() {
			app.busy = false
			app.mongoCancel = nil
			if app.documentStore != docs {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("MongoDB 集合读取已取消。")
					return
				}
				app.reportError("读取 MongoDB 集合失败", err)
				return
			}
			app.mongoSelectedDB = database
			if app.openedConnectionID != "" {
				if live, ok := app.liveConnections[app.openedConnectionID]; ok && live.profile.ID != "" {
					*app.connectionTreeState("profile:"+live.profile.ID, true) = true
				}
				*app.connectionTreeState("mongo-db:"+app.openedConnectionID+":"+database, false) = true
			}
			app.mongoSelectedColl = ""
			app.mongoCollections = collections
			app.mongoIndexes = nil
			app.mongoIndexError = ""
			app.mongoIndexesLoading = false
			app.mongoDocuments = nil
			app.mongoSelectedID = ""
			app.status = app.localized("MongoDB 数据库已切换。")
		})
	}()
}

func (app *workbench) selectMongoCollection(collection string) {
	if app.busy || app.documentStore == nil {
		return
	}
	app.mongoSelectedColl = collection
	app.mongoIndexes = nil
	app.mongoIndexError = ""
	app.mongoIndexesLoading = false
	app.mongoDocuments = nil
	app.mongoNextOffset = 0
	app.mongoHasMore = false
	app.mongoSelectedID = ""
	app.mongoEditor = nil
	app.queryMongoDocuments(true)
}

func (app *workbench) queryMongoDocuments(reset bool) {
	if app.busy || app.documentStore == nil || app.mongoSelectedDB == "" || app.mongoSelectedColl == "" {
		return
	}
	app.ensureMongoQueryEditors()
	docs := app.documentStore
	db, coll, filter, projection, pipeline, usePipeline := app.mongoSelectedDB, app.mongoSelectedColl, app.mongoFilterEditor.Buffer.String(), app.mongoProjectionEditor.Buffer.String(), app.mongoPipelineEditor.Buffer.String(), app.mongoUsePipeline
	offset := app.mongoNextOffset
	if reset {
		offset = 0
	}
	app.busy = true
	app.status = app.localized("正在读取 MongoDB 文档…")
	ctx, cancel := app.withTaskTimeout(2 * time.Minute)
	app.mongoCancel = cancel
	go func() {
		defer cancel()
		var page connection.DocumentPage
		var err error
		if usePipeline {
			page, err = docs.AggregateDocuments(ctx, db, coll, pipeline, offset, 50)
		} else {
			page, err = docs.FindDocuments(ctx, db, coll, filter, projection, offset, 50)
		}
		app.window.Update(func() {
			app.busy = false
			app.mongoCancel = nil
			if app.documentStore != docs {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("MongoDB 文档读取已取消。")
					return
				}
				app.reportError("MongoDB 查询失败", err)
				return
			}
			if reset {
				app.mongoDocuments = nil
			}
			app.mongoDocuments = append(app.mongoDocuments, page.Documents...)
			app.mongoNextOffset = page.NextOffset
			app.mongoHasMore = page.HasMore
			app.mongoSelectedID = ""
			app.mongoOriginalJSON = ""
			app.mongoEditor = nil
			app.mongoShowDiff = false
			app.status = fmt.Sprintf("已读取 %d 个 MongoDB 文档。", len(app.mongoDocuments))
		})
	}()
}

func (app *workbench) inspectMongoDocument(id string) {
	if app.busy || app.documentStore == nil {
		return
	}
	docs := app.documentStore
	db, coll := app.mongoSelectedDB, app.mongoSelectedColl
	app.busy = true
	app.mongoSelectedID = id
	app.status = app.localized("正在读取 MongoDB 文档详情…")
	ctx, cancel := app.withTaskTimeout(30 * time.Second)
	app.mongoCancel = cancel
	go func() {
		defer cancel()
		document, err := docs.GetDocument(ctx, db, coll, id)
		app.window.Update(func() {
			app.busy = false
			app.mongoCancel = nil
			if app.documentStore != docs {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("MongoDB 文档详情读取已取消。")
					return
				}
				app.reportError("读取 MongoDB 文档失败", err)
				return
			}
			app.mongoOriginalJSON = document.JSON
			buffer := editor.NewBuffer(document.JSON)
			buffer.SetLanguage(editor.LanguageFor("json"))
			app.mongoEditor = &editor.State{Buffer: buffer, Label: "MongoDB 文档 JSON"}
			app.mongoDocumentTruncated = document.Truncated
			app.mongoNewDocument = false
			app.mongoShowDiff = false
			app.mongoTreeExpanded = make(map[string]bool)
			app.status = app.localized("MongoDB 文档详情已加载。")
		})
	}()
}

func (app *workbench) newMongoDocument() {
	if app.busy || app.documentStore == nil || app.openedReadOnly || app.mongoSelectedDB == "" || app.mongoSelectedColl == "" {
		return
	}
	buffer := editor.NewBuffer("{}\n")
	buffer.SetLanguage(editor.LanguageFor("json"))
	app.mongoEditor = &editor.State{Buffer: buffer, Label: "新 MongoDB 文档 JSON"}
	app.mongoSelectedID = ""
	app.mongoOriginalJSON = ""
	app.mongoDocumentTruncated = false
	app.mongoNewDocument = true
	app.mongoShowDiff = true
	app.mongoTreeExpanded = make(map[string]bool)
}

func (app *workbench) renderMongoJSONTree(c *ui.Context, source string) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		ui.Text(c, app.localized("JSON 树暂不可用：当前内容不是有效 JSON。 ")).TextColor(c.Theme().TextMuted)
		return
	}
	if app.mongoTreeExpanded == nil {
		app.mongoTreeExpanded = make(map[string]bool)
	}
	app.mongoTreeExpanded["root"] = true
	remaining := 500
	ui.Column(c).Padding(8).Gap(3).Children(func() {
		app.renderMongoJSONNode(c, "Document", "root", root, 0, &remaining)
		if remaining <= 0 {
			ui.Text(c, app.localized("字段过多；树形预览最多显示 500 个节点。")).TextColor(c.Theme().TextMuted)
		}
	})
}

func (app *workbench) renderMongoJSONNode(c *ui.Context, label, path string, value any, depth int, remaining *int) {
	if *remaining <= 0 {
		return
	}
	(*remaining)--
	if depth > 16 {
		ui.Text(c, strings.Repeat("  ", depth)+label+": …"+fmt.Sprintf(app.localized("（嵌套超过 %d 层）"), 16)).TextColor(c.Theme().TextMuted)
		return
	}
	indent := strings.Repeat("  ", depth)
	switch node := value.(type) {
	case map[string]any:
		expanded := app.mongoTreeExpanded[path]
		arrow := "▸"
		if expanded {
			arrow = "▾"
		}
		buttonLabel := fmt.Sprintf("%s%s %s · %d fields", indent, arrow, label, len(node))
		if ui.Button(c, buttonLabel).Clicked() {
			app.mongoTreeExpanded[path] = !expanded
		}
		if !expanded {
			return
		}
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			app.renderMongoJSONNode(c, key, path+"/"+key, node[key], depth+1, remaining)
		}
	case []any:
		expanded := app.mongoTreeExpanded[path]
		arrow := "▸"
		if expanded {
			arrow = "▾"
		}
		buttonLabel := fmt.Sprintf("%s%s %s · %d items", indent, arrow, label, len(node))
		if ui.Button(c, buttonLabel).Clicked() {
			app.mongoTreeExpanded[path] = !expanded
		}
		if !expanded {
			return
		}
		for i, item := range node {
			app.renderMongoJSONNode(c, fmt.Sprintf("[%d]", i), fmt.Sprintf("%s/%d", path, i), item, depth+1, remaining)
		}
	default:
		ui.Text(c, indent+label+": "+mongoTreeScalarLabel(value)).Selectable()
	}
}

func mongoTreeScalarLabel(value any) string {
	text := fmt.Sprintf("%v", value)
	if len([]rune(text)) > 1024 {
		text = string([]rune(text)[:1024]) + "…"
	}
	return text
}

func (app *workbench) commitMongoDocument() {
	if app.busy || app.documentStore == nil || app.mongoEditor == nil || app.openedReadOnly || app.mongoDocumentTruncated {
		return
	}
	docs := app.documentStore
	db, coll, id, body := app.mongoSelectedDB, app.mongoSelectedColl, app.mongoSelectedID, app.mongoEditor.Buffer.String()
	isNew := app.mongoNewDocument
	if strings.TrimSpace(body) == "" {
		app.reportError("保存 MongoDB 文档失败", errors.New("文档不能为空"))
		return
	}
	if err := docs.ValidateDocument(body); err != nil {
		app.reportError("保存 MongoDB 文档失败", err)
		return
	}
	if !isNew && body == app.mongoOriginalJSON {
		app.status = app.localized("文档没有变化。")
		return
	}
	connectionID := app.openedConnectionID
	change := app.localizedMongoDiffPreview(app.mongoOriginalJSON, body, isNew)
	detail := fmt.Sprintf(app.localized("环境：%s\n连接：%s\n数据库：%s\n集合：%s\n文档 ID：%s\n\n%s"), app.localized(environmentLabel(app.openedEnvironment)), app.openedLabel, db, coll, id, change)
	app.requestConfirmation("提交 MongoDB 文档", "确认将此变更写入数据库？", detail, []string{"取消", "提交"}, 1, func(choice int) {
		if choice != 1 || app.openedConnectionID != connectionID {
			return
		}
		app.busy = true
		go func() {
			ctx, cancel := app.withTaskTimeout(30 * time.Second)
			var err error
			if isNew {
				err = docs.InsertDocument(ctx, db, coll, body)
			} else {
				err = docs.ReplaceDocument(ctx, db, coll, id, body)
			}
			cancel()
			app.window.Update(func() {
				app.busy = false
				if app.openedConnectionID != connectionID {
					return
				}
				if err != nil {
					app.reportError("提交 MongoDB 文档失败", err)
					return
				}
				app.mongoShowDiff = false
				app.queryMongoDocuments(true)
				app.status = app.localized("MongoDB 文档已提交。")
			})
		}()
	})
}

func mongoDocumentConfirmationDetail(environment, connectionLabel, database, collection, documentID, change string) string {
	return fmt.Sprintf("环境：%s\n连接：%s\n数据库：%s\n集合：%s\n文档 ID：%s\n\n%s", environment, connectionLabel, database, collection, documentID, change)
}

func mongoDiffPreview(before, after string, isNew bool) string {
	if isNew {
		return "+ 新文档\n" + truncateText(after, 8192)
	}
	if before == after {
		return "没有检测到修改。"
	}
	return "- 原文\n" + truncateText(before, 4096) + "\n+ 新文档\n" + truncateText(after, 4096)
}

func (app *workbench) localizedMongoDiffPreview(before, after string, isNew bool) string {
	if app.language != "en" {
		return mongoDiffPreview(before, after, isNew)
	}
	if isNew {
		return app.localized("+ 新文档") + "\n" + truncateText(after, 8192)
	}
	if before == after {
		return app.localized("没有检测到修改。")
	}
	return app.localized("- 原文") + "\n" + truncateText(before, 4096) + "\n" + app.localized("+ 新文档") + "\n" + truncateText(after, 4096)
}

func (app *workbench) ensureMongoQueryEditors() {
	if app.mongoFilterEditor == nil {
		app.mongoFilterEditor = newMongoEditor(`{}`, "MongoDB filter JSON")
	}
	if app.mongoProjectionEditor == nil {
		app.mongoProjectionEditor = newMongoEditor(`{}`, "MongoDB projection JSON")
	}
	if app.mongoPipelineEditor == nil {
		app.mongoPipelineEditor = newMongoEditor(`[]`, "MongoDB aggregation pipeline JSON")
	}
}

func newMongoEditor(value, label string) *editor.State {
	buffer := editor.NewBuffer(value)
	buffer.SetLanguage(editor.LanguageFor("json"))
	return &editor.State{Buffer: buffer, Label: label, FitContent: true}
}

func truncateText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "\n…（预览已截断）"
}

func (app *workbench) readMongoOverview(id string, live liveConnection) {
	source, ok := live.connection.(connection.MongoOverviewSource)
	if !ok || live.mongoOverviewRequested || app.window == nil {
		return
	}
	live.mongoOverviewRequested = true
	app.liveConnections[id] = live
	ctx, cancel := app.withTaskTimeout(4 * time.Second)
	window := app.window
	go func() {
		defer cancel()
		info := source.MongoOverview(ctx)
		window.Update(func() {
			current, exists := app.liveConnections[id]
			if exists && current.connection == live.connection {
				current.mongoOverview = info
				app.liveConnections[id] = current
			}
		})
	}()
}
