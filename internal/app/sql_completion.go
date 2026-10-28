package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/connection"
)

const maxSQLCompletionItems = 240

type sqlCompletionColumnCacheEntry struct {
	columns   []connection.Column
	expiresAt time.Time
}

type sqlCompletionPendingRequest struct {
	request       editor.CompletionRequest
	connectionID  string
	inputRevision uint64
}

func (app *workbench) bindSQLCompletions(workspace *queryWorkspace) {
	if workspace == nil || workspace.query == nil || workspace.sqlCompletionBound {
		return
	}
	workspace.sqlCompletionBound = true
	workspace.query.OnCompletion = func(request editor.CompletionRequest) {
		app.requestSQLCompletions(workspace, request)
	}
	workspace.query.OnInput = func(event ui.InputEvent) bool {
		workspace.sqlCompletionInputRevision++
		if app.sqlCompletionPending != nil {
			delete(app.sqlCompletionPending, workspace.id)
		}
		if event.Kind == ui.InputText && event.Text == "." && app.window != nil {
			app.window.Update(func() {
				if !app.sqlCompletionWorkspaceActive(workspace) {
					return
				}
				request := editor.CompletionRequest{
					Offset:   workspace.query.Cursor(),
					Version:  workspace.query.Buffer.Version(),
					Explicit: false,
				}
				app.requestSQLCompletions(workspace, request)
			})
		}
		return false
	}
}

func (app *workbench) requestSQLCompletions(workspace *queryWorkspace, request editor.CompletionRequest) {
	if !app.sqlCompletionWorkspaceActive(workspace) {
		return
	}
	connectionID := workspace.queryConnectionID
	if connectionID == "" {
		connectionID = app.openedConnectionID
	}
	if connectionID == "" || connectionID != app.openedConnectionID || app.store == nil {
		workspace.query.ShowCompletions(request.Offset-len(request.Prefix), nil)
		return
	}
	if request.Version != workspace.query.Buffer.Version() || request.Offset != workspace.query.Cursor() {
		return
	}
	if app.sqlCompletionPending == nil {
		app.sqlCompletionPending = make(map[uint64]sqlCompletionPendingRequest)
	}
	pending := sqlCompletionPendingRequest{
		request:       request,
		connectionID:  connectionID,
		inputRevision: workspace.sqlCompletionInputRevision,
	}
	app.sqlCompletionPending[workspace.id] = pending
	app.presentSQLCompletions(workspace, pending, true)
}

func (app *workbench) sqlCompletionWorkspaceActive(workspace *queryWorkspace) bool {
	return workspace != nil && workspace == app.activeQuery && workspace.query != nil && workspace.query.Buffer != nil
}

func (app *workbench) sqlCompletionRequestCurrent(workspace *queryWorkspace, pending sqlCompletionPendingRequest) bool {
	return app.sqlCompletionWorkspaceActive(workspace) &&
		pending.connectionID != "" && pending.connectionID == app.openedConnectionID &&
		pending.request.Version == workspace.query.Buffer.Version() &&
		pending.request.Offset == workspace.query.Cursor() &&
		pending.inputRevision == workspace.sqlCompletionInputRevision
}

func (app *workbench) presentSQLCompletions(workspace *queryWorkspace, pending sqlCompletionPendingRequest, loadColumns bool) {
	if !app.sqlCompletionRequestCurrent(workspace, pending) || app.store == nil {
		return
	}
	text := workspace.query.Buffer.String()
	request := pending.request
	offset := max(0, min(request.Offset, len(text)))
	prefixStart := max(0, offset-len(request.Prefix))
	tokens, ranges := lexSQLCompletionTokens(text, app.store.Driver())
	if sqlCompletionInsideOpaqueRange(ranges, offset) {
		workspace.query.ShowCompletions(prefixStart, nil)
		return
	}
	statement := sqlCompletionStatement(tokens, offset)
	prior := make([]sqlCompletionToken, 0, len(statement))
	for _, token := range statement {
		if token.end <= prefixStart {
			prior = append(prior, token)
		}
	}
	contextKind, schemaFilter, qualifier := sqlCompletionContextAt(prior)
	if contextKind == sqlCompletionExpression && request.Prefix == "" && !request.Explicit {
		workspace.query.ShowCompletions(prefixStart, nil)
		return
	}

	relations := parseSQLCompletionRelations(statement)
	resolved := app.resolveSQLCompletionRelations(relations)
	items := make([]editor.Completion, 0, 64)
	seen := make(map[string]bool)
	add := func(item editor.Completion) {
		if len(items) >= maxSQLCompletionItems {
			return
		}
		key := strings.ToLower(item.Label)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		items = append(items, item)
	}

	switch contextKind {
	case sqlCompletionRelationName:
		schemaFilterForDriver := schemaFilter
		if app.store.Driver() == connection.DriverSQLite {
			schemaFilterForDriver = ""
		}
		for _, object := range app.objects {
			if !isSQLTableOrView(object.Type) || (schemaFilterForDriver != "" && object.Schema != schemaFilterForDriver) {
				continue
			}
			label := object.Name
			if schemaFilter == "" && object.Schema != "" && app.sqlNamespace != "" && object.Schema != app.sqlNamespace {
				label = object.Schema + "." + object.Name
			}
			add(editor.Completion{Label: label, Detail: app.localized("数据表 / 视图"), Kind: editor.CompletionType})
		}
	case sqlCompletionQualifiedColumn:
		for _, relation := range resolved {
			if qualifier != "" && !sqlCompletionQualifierMatches(qualifier, relation.relation) {
				continue
			}
			columns, ready := app.sqlCompletionColumns(pending.connectionID, relation.schema, relation.name, loadColumns)
			if !ready {
				continue
			}
			for _, column := range columns {
				add(editor.Completion{Label: column.Name, Detail: app.localized("字段") + " · " + relation.relation.name, Kind: editor.CompletionField})
			}
		}
	default:
		if len(resolved) > 0 {
			for _, relation := range resolved {
				columns, ready := app.sqlCompletionColumns(pending.connectionID, relation.schema, relation.name, loadColumns)
				if !ready {
					continue
				}
				for _, column := range columns {
					add(editor.Completion{Label: column.Name, Detail: app.localized("字段") + " · " + relation.relation.name, Kind: editor.CompletionField})
				}
			}
		}
		for _, keyword := range sqlCompletionKeywords(app.store.Driver()) {
			add(editor.Completion{Label: keyword, Detail: app.localized("SQL 关键字"), Kind: editor.CompletionKeyword})
		}
		for _, function := range sqlCompletionFunctions(app.store.Driver()) {
			add(editor.Completion{Label: function, Detail: app.localized("SQL 函数"), Kind: editor.CompletionFunction})
		}
	}
	workspace.query.ShowCompletions(prefixStart, items)
}

func (app *workbench) resolveSQLCompletionRelations(relations []sqlCompletionRelation) []sqlCompletionResolvedRelation {
	if app.store == nil {
		return nil
	}
	resolved := make([]sqlCompletionResolvedRelation, 0, len(relations))
	seen := make(map[string]bool)
	for _, relation := range relations {
		if relation.name == "" {
			continue
		}
		schema := relation.schema
		if app.store.Driver() == connection.DriverSQLite {
			schema = ""
		} else if schema == "" {
			schema = app.sqlNamespace
		}
		if app.store.Driver() != connection.DriverSQLite && schema == "" {
			continue
		}
		key := schema + "\x00" + relation.name
		if seen[key] {
			continue
		}
		seen[key] = true
		resolved = append(resolved, sqlCompletionResolvedRelation{relation: relation, schema: schema, name: relation.name})
	}
	return resolved
}

func (app *workbench) sqlCompletionColumns(connectionID, schema, table string, startLoad bool) ([]connection.Column, bool) {
	if app.sqlCompletionColumnCache == nil {
		app.sqlCompletionColumnCache = make(map[string]sqlCompletionColumnCacheEntry)
	}
	if app.sqlCompletionColumnLoading == nil {
		app.sqlCompletionColumnLoading = make(map[string]bool)
	}
	key := sqlCompletionColumnCacheKey(connectionID, schema, table)
	if cached, ok := app.sqlCompletionColumnCache[key]; ok {
		if cached.expiresAt.After(time.Now()) {
			return cached.columns, true
		}
		delete(app.sqlCompletionColumnCache, key)
	}
	if !startLoad || app.sqlCompletionColumnLoading[key] || app.store == nil || app.openedConnectionID != connectionID {
		return nil, false
	}
	store := app.store
	app.sqlCompletionColumnLoading[key] = true
	go func() {
		ctx, cancel := app.withTaskTimeout(8 * time.Second)
		defer cancel()
		columns, err := store.Columns(ctx, schema, table)
		app.window.Update(func() {
			delete(app.sqlCompletionColumnLoading, key)
			if app.openedConnectionID == connectionID && app.store == store {
				cacheTTL := 30 * time.Second
				if err != nil {
					cacheTTL = 10 * time.Second
				}
				app.sqlCompletionColumnCache[key] = sqlCompletionColumnCacheEntry{columns: columns, expiresAt: time.Now().Add(cacheTTL)}
			}
			if pending, ok := app.sqlCompletionPendingForConnection(connectionID); ok {
				if workspace := app.queryWorkspaceByID(pending.workspaceID); workspace != nil {
					app.presentSQLCompletions(workspace, pending.request, false)
				}
			}
		})
	}()
	return nil, false
}

type sqlCompletionPendingWithWorkspace struct {
	workspaceID uint64
	request     sqlCompletionPendingRequest
}

func (app *workbench) sqlCompletionPendingForConnection(connectionID string) (sqlCompletionPendingWithWorkspace, bool) {
	if app.sqlCompletionPending == nil {
		return sqlCompletionPendingWithWorkspace{}, false
	}
	pending, ok := app.sqlCompletionPending[app.activeQueryID()]
	if !ok || pending.connectionID != connectionID {
		return sqlCompletionPendingWithWorkspace{}, false
	}
	return sqlCompletionPendingWithWorkspace{workspaceID: app.activeQueryID(), request: pending}, true
}

func (app *workbench) activeQueryID() uint64 {
	if app.activeQuery == nil {
		return 0
	}
	return app.activeQuery.id
}

func (app *workbench) queryWorkspaceByID(id uint64) *queryWorkspace {
	if app.activeQuery != nil && app.activeQuery.id == id {
		return app.activeQuery
	}
	for _, workspace := range app.workspaces {
		if workspace != nil && workspace.id == id {
			return workspace
		}
	}
	return nil
}

func sqlCompletionColumnCacheKey(connectionID, schema, table string) string {
	return connectionID + "\x00" + schema + "\x00" + table
}
