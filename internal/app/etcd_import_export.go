package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safefile"
)

const (
	maxEtcdTransferKeys = 100
	maxEtcdImportBytes  = 16 << 20
)

type etcdTransferEntry struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	LeaseID int64  `json:"lease_id,omitempty"`
}

func (app *workbench) etcdExportDisabled() bool {
	if app.busy || app.exporting || app.dialogOpen || app.etcdStore == nil || !app.etcdKeysLoaded || len(app.etcdEntries) == 0 {
		return true
	}
	for _, entry := range app.etcdEntries {
		if entry.ValueTruncated {
			return true
		}
	}
	return false
}

func (app *workbench) etcdImportDisabled() bool {
	_, canCreate := app.etcdStore.(connection.EtcdKeyCreator)
	return app.busy || app.exporting || app.dialogOpen || app.etcdStore == nil || !canCreate || app.openedReadOnly
}

func (app *workbench) etcdDeleteDisabled() bool {
	return app.busy || app.exporting || app.dialogOpen || app.etcdStore == nil || app.openedReadOnly || app.etcdSelectedKey == "" || (app.etcdSection != "keys" && app.etcdSection != "global-search")
}

func (app *workbench) exportEtcdKeys() {
	if app.etcdExportDisabled() {
		return
	}
	entries := make([]etcdTransferEntry, 0, len(app.etcdEntries))
	for _, entry := range app.etcdEntries {
		if entry.ValueTruncated {
			app.status = app.localized("存在超过 1 MiB 的值，无法完整导出。")
			return
		}
		entries = append(entries, etcdTransferEntry{Key: entry.Key, Value: entry.Value, LeaseID: entry.Lease})
	}
	app.dialogOpen = true
	app.status = app.localized("请选择 etcd 键导出文件的位置…")
	defaultName := safeExportName(app.openedLabel) + "-etcd-keys.json"
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: app.window, Title: app.localized("导出 etcd 键"), ButtonLabel: app.localized("导出"), DefaultPath: defaultName,
			Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}},
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("导出 etcd 键失败", err)
				return
			}
			if path == "" {
				app.status = app.localized("已取消导出。")
				return
			}
			if filepath.Ext(path) == "" {
				path += ".json"
			}
			app.writeEtcdExport(path, entries)
		})
	}()
}

func (app *workbench) writeEtcdExport(path string, entries []etcdTransferEntry) {
	connectionID := app.openedConnectionID
	app.exporting = true
	app.status = app.localized("正在导出已加载的 etcd 键…")
	go func() {
		staged, err := safefile.CreateExport(path)
		if err == nil {
			encoder := json.NewEncoder(staged.File())
			encoder.SetIndent("", "  ")
			err = encoder.Encode(entries)
			if err == nil {
				err = staged.Commit()
			}
			if err != nil {
				cleanupErr := staged.Abort()
				if cleanupErr != nil {
					err = errors.Join(err, fmt.Errorf("clean up temporary export: %w", cleanupErr))
				}
			}
		}
		app.window.Update(func() {
			app.exporting = false
			if app.openedConnectionID != connectionID {
				app.status = app.localized("导出来源已变化，文件未关联当前连接。")
				return
			}
			if err != nil && safefile.IsPublished(err) {
				app.status = fmt.Sprintf(app.localized("导出文件已完整生成到 %s，但目录同步失败。"), path)
				return
			}
			if err != nil {
				app.reportError("导出 etcd 键失败", err)
				return
			}
			app.status = fmt.Sprintf(app.localized("已导出 %d 个已加载的 Key 到 %s。"), len(entries), path)
		})
	}()
}

func (app *workbench) importEtcdKeys() {
	if app.etcdImportDisabled() {
		return
	}
	app.dialogOpen = true
	app.status = app.localized("请选择要导入的 etcd JSON 文件…")
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: app.window, Title: app.localized("导入 etcd 键"), ButtonLabel: app.localized("打开"), Multiple: false,
			Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}},
		})
		var entries []etcdTransferEntry
		if err == nil && len(paths) > 0 && paths[0] != "" {
			entries, err = readEtcdTransferFile(paths[0])
		}
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("读取 etcd 导入文件失败", err)
				return
			}
			if len(paths) == 0 || paths[0] == "" {
				app.status = app.localized("已取消导入。")
				return
			}
			if len(entries) == 0 {
				app.status = app.localized("导入文件中没有 Key。")
				return
			}
			detail := app.etcdImportPreview(entries)
			store, connectionID := app.etcdStore, app.openedConnectionID
			app.requestConfirmation("导入 etcd 键", "将以仅创建方式导入 Key；已存在的 Key 不会被覆盖。", detail, []string{"取消", "导入"}, 1, func(choice int) {
				if choice != 1 || app.etcdStore != store || app.openedConnectionID != connectionID || app.openedReadOnly {
					return
				}
				app.createImportedEtcdKeys(store, connectionID, entries)
			})
		})
	}()
}

func readEtcdTransferFile(path string) ([]etcdTransferEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxEtcdImportBytes {
		return nil, fmt.Errorf("导入文件超过 %d MiB。", maxEtcdImportBytes>>20)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxEtcdImportBytes+1))
	decoder.DisallowUnknownFields()
	var entries []etcdTransferEntry
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("无法解析 etcd JSON：%w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("导入文件只能包含一个 JSON 数组。")
		}
		return nil, fmt.Errorf("读取导入文件失败：%w", err)
	}
	if len(entries) > maxEtcdTransferKeys {
		return nil, fmt.Errorf("每个文件最多导入 %d 个 Key。", maxEtcdTransferKeys)
	}
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if strings.TrimSpace(entry.Key) == "" {
			return nil, fmt.Errorf("第 %d 项 Key 为空。", index+1)
		}
		if len(entry.Value) > 1<<20 {
			return nil, fmt.Errorf("第 %d 项的值超过 1 MiB。", index+1)
		}
		if entry.LeaseID < 0 {
			return nil, fmt.Errorf("第 %d 项的 lease_id 无效。", index+1)
		}
		if _, duplicate := seen[entry.Key]; duplicate {
			return nil, fmt.Errorf("导入文件中 Key %q 重复。", entry.Key)
		}
		seen[entry.Key] = struct{}{}
	}
	return entries, nil
}

func (app *workbench) etcdImportPreview(entries []etcdTransferEntry) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, app.localized("导入 Key 数量：%d\n\n"), len(entries))
	limit := min(len(entries), 30)
	for _, entry := range entries[:limit] {
		builder.WriteString(entry.Key)
		if entry.LeaseID > 0 {
			fmt.Fprintf(&builder, " (Lease ID %d)", entry.LeaseID)
		}
		builder.WriteByte('\n')
	}
	if len(entries) > limit {
		fmt.Fprintf(&builder, app.localized("… 另有 %d 个 Key"), len(entries)-limit)
	}
	return builder.String()
}

func (app *workbench) createImportedEtcdKeys(store connection.Configuration, connectionID string, entries []etcdTransferEntry) {
	creator, ok := store.(connection.EtcdKeyCreator)
	if !ok || app.busy || app.etcdStore != store || app.openedConnectionID != connectionID {
		return
	}
	app.busy = true
	app.status = app.localized("正在安全导入 etcd 键…")
	go func() {
		ctx, cancel := app.withTaskTimeout(2 * time.Minute)
		defer cancel()
		created := 0
		var err error
		for _, entry := range entries {
			if _, err = creator.CreateKey(ctx, entry.Key, entry.Value, entry.LeaseID); err != nil {
				break
			}
			created++
		}
		app.window.Update(func() {
			app.busy = false
			if app.etcdStore != store || app.openedConnectionID != connectionID {
				return
			}
			if err != nil {
				prefix := fmt.Sprintf(app.localized("已导入 %d/%d 个 Key 后遇到错误："), created, len(entries))
				app.reportError("导入 etcd 键中止", errors.New(prefix+err.Error()))
			} else {
				app.status = fmt.Sprintf(app.localized("已安全导入 %d 个 etcd Key。"), created)
			}
			app.refreshEtcd(true)
		})
	}()
}
