package app

import "github.com/egoist/mygo/ui"

var treeItemIcons = map[string]*ui.SVG{
	"database":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/></svg>`)),
	"schema":      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/><path d="M3 10h18"/></svg>`)),
	"table":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="1.5"/><path d="M3 10h18M9 4v16"/></svg>`)),
	"view":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></svg>`)),
	"index":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="15" r="5"/><path d="m11.5 11.5 8-8 2 2-2 2 1.5 1.5-2 2-1.5-1.5-4.5 4.5"/></svg>`)),
	"trigger":     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 4 14h7l-1 8 10-13h-7l1-7Z"/></svg>`)),
	"function":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 5 3 12l5 7m8-14 5 7-5 7"/></svg>`)),
	"sequence":    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6h12M9 12h12M9 18h12"/><path d="m3 6 1 1 2-2m-3 7 1 1 2-2m-3 7 1 1 2-2"/></svg>`)),
	"type":        ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 5 3 12l5 7m8-14 5 7-5 7"/><path d="M14 8h-4m2-3v14"/></svg>`)),
	"extension":   ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m12 2 9 5v10l-9 5-9-5V7l9-5Z"/><path d="m3.5 7.5 8.5 5 8.5-5M12 22V12.5"/></svg>`)),
	"query":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3h10l4 4v14H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z"/><path d="M15 3v5h4M8 13h8m-8 4h6"/></svg>`)),
	"users":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2m16 0v-2a4 4 0 0 0-3-3.87M14 3.13a4 4 0 0 1 0 7.75"/><circle cx="10" cy="7" r="4"/></svg>`)),
	"key":         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="15" r="5"/><path d="m11.5 11.5 8-8 2 2-2 2 1.5 1.5-2 2-1.5-1.5-4.5 4.5"/></svg>`)),
	"lease":       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>`)),
	"dashboard":   ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19a9 9 0 1 1 16 0"/><path d="m12 13 4-4"/><circle cx="12" cy="13" r="1"/><path d="M3 19h18"/></svg>`)),
	"maintenance": ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a5 5 0 0 0-6.6 6.6L3 18l3 3 5.1-5.1a5 5 0 0 0 6.6-6.6l-3.1 3.1-3-3 3.1-3.1Z"/></svg>`)),
}

var treeItemIconColors = map[string]ui.Color{
	"database":  ui.Hex("#eab308"),
	"schema":    ui.Hex("#f59e0b"),
	"table":     ui.Hex("#22c55e"),
	"view":      ui.Hex("#a855f7"),
	"index":     ui.Hex("#f59e0b"),
	"trigger":   ui.Hex("#f97316"),
	"function":  ui.Hex("#f59e0b"),
	"sequence":  ui.Hex("#10b981"),
	"type":      ui.Hex("#8b5cf6"),
	"extension": ui.Hex("#8b5cf6"),
	"query":     ui.Hex("#3b82f6"),
	"users":     ui.Hex("#14b8a6"),
	"key":       ui.Hex("#ef4444"),
	"lease":     ui.Hex("#f59e0b"),
	"dashboard": ui.Hex("#06b6d4"),
}
