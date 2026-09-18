// Package i18n provides minimal UI-string translation (en / zh-CN),
// following $LANG (Medis language-settings equivalent). Strings not yet in
// the tables fall back to English.
package i18n

import (
	"os"
	"strings"
)

var lang = "en"

// Init picks the default language from the environment.
func Init() string {
	l := os.Getenv("LANG") + "," + os.Getenv("LC_ALL")
	if strings.Contains(l, "zh") {
		lang = "zh-CN"
	}
	return lang
}

// Set switches the language.
func Set(l string) { lang = l }

// Lang returns the active language.
func Lang() string { return lang }

var zh = map[string]string{
	"not connected": "未连接 — 按 c 打开连接",
	"conn":          "连接",
	"query":         "查询",
	"filter":        "过滤",
	"rescan":        "重扫",
	"alert":         "警报",
	"help":          "帮助",
	"quit":          "退出",
	"info":          "信息",
	"settings":      "设置",
	"delete key":    "删除键",
	"delete item":   "删除条目",
	"alert·write":   "警报 · 写命令",
	"deleted":       "已删除",
	"saved":         "已保存",
}

// T translates a UI string.
func T(s string) string {
	if lang == "zh-CN" {
		if v, ok := zh[s]; ok {
			return v
		}
	}
	return s
}
