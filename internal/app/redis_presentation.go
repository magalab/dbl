package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
)

func redisMutationConfirmationDetail(mutation connection.KeyMutation) string {
	operation := "新增或更新数据"
	switch mutation.Type {
	case "string":
		operation = "替换整个字符串值"
	case "json":
		operation = "替换整个 JSON 值"
	case "hash":
		operation = "新增或更新 Hash 字段"
	case "list":
		operation = "在列表末尾追加元素"
	case "set":
		operation = "添加 Set 成员（重复成员不会增加）"
	case "zset":
		operation = "新增成员或更新成员分数"
	case "stream":
		operation = "向 Stream 追加一条记录"
	}

	ttlImpact := "写入后保留现有 TTL"
	if mutation.Type == "string" && !mutation.PreserveTTL {
		ttlImpact = "写入后永不过期（会清除现有 TTL）"
	}
	if mutation.TTL > 0 {
		ttlImpact = "写入后设置整个键的 TTL：" + mutation.TTL.Round(time.Second).String()
	}

	detail := fmt.Sprintf("键：%s\n操作：%s\n类型：%s", mutation.Key, operation, mutation.Type)
	if mutation.Field != "" {
		detail += fmt.Sprintf("\n字段（%d 字节，转义预览最多 4 KiB）：\n%s", len(mutation.Field), truncateText(strconv.Quote(mutation.Field), 4096))
	}
	if mutation.Type == "zset" {
		detail += fmt.Sprintf("\nScore：%g", mutation.Score)
	}
	detail += fmt.Sprintf("\n值（%d 字节，转义预览最多 4 KiB）：\n%s\nTTL：%s", len(mutation.Value), truncateText(strconv.Quote(mutation.Value), 4096), ttlImpact)
	return detail
}

func redisOperationConfirmationDetail(environment, connectionLabel, operation string) string {
	return fmt.Sprintf("环境：%s\n连接：%s\n%s", environment, connectionLabel, operation)
}

func (app *workbench) redisMutationConfirmationDetail(mutation connection.KeyMutation) string {
	if app.language != "en" {
		return redisMutationConfirmationDetail(mutation)
	}
	operation := "Add or update data"
	switch mutation.Type {
	case "string":
		operation = "Replace the entire string value"
	case "json":
		operation = "Replace the entire JSON value"
	case "hash":
		operation = "Add or update a hash field"
	case "list":
		operation = "Append elements to the list"
	case "set":
		operation = "Add set members (duplicates are ignored)"
	case "zset":
		operation = "Add members or update their scores"
	case "stream":
		operation = "Append a stream entry"
	}
	typeName := strings.ToUpper(mutation.Type)
	if mutation.Type == "zset" {
		typeName = "Sorted Set"
	}
	ttlImpact := "Preserve existing TTL"
	if mutation.Type == "string" && !mutation.PreserveTTL {
		ttlImpact = "Remove existing TTL; key will not expire"
	}
	if mutation.PreserveTTL {
		ttlImpact = "Preserve existing TTL"
	}
	if mutation.TTL > 0 {
		ttlImpact = "Set key TTL to " + mutation.TTL.Round(time.Second).String()
	}
	detail := fmt.Sprintf("Key: %s\nOperation: %s\nType: %s", mutation.Key, operation, typeName)
	if mutation.Field != "" {
		detail += fmt.Sprintf("\nField (%d bytes, escaped preview up to 4 KiB):\n%s", len(mutation.Field), truncateText(strconv.Quote(mutation.Field), 4096))
	}
	if mutation.Type == "zset" {
		detail += fmt.Sprintf("\nScore: %g", mutation.Score)
	}
	detail += fmt.Sprintf("\nValue (%d bytes, escaped preview up to 4 KiB):\n%s\nTTL: %s", len(mutation.Value), truncateText(strconv.Quote(mutation.Value), 4096), ttlImpact)
	return detail
}

func (app *workbench) redisOperationConfirmationDetail(environment, connectionLabel, operation string) string {
	if app.language != "en" {
		return redisOperationConfirmationDetail(environment, connectionLabel, operation)
	}
	return fmt.Sprintf("Environment: %s\nConnection: %s\n%s", app.localized(environment), connectionLabel, operation)
}

func redisMemoryLabel(size int64) string {
	if size < 0 {
		return "未知"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	for _, unit := range []string{"KiB", "MiB", "GiB"} {
		value := float64(size) / 1024
		if value < 1024 || unit == "GiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		size = int64(value)
	}
	return fmt.Sprintf("%d B", size)
}
