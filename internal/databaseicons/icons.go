package databaseicons

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/*.svg
var assets embed.FS

var icons = map[string]*ui.SVG{
	"sqlite":     parse("sqlite"),
	"postgresql": parse("postgresql"),
	"mysql":      parse("mysql"),
	"redis":      parse("redis"),
	"mongodb":    parse("mongodb"),
	"etcd":       parse("etcd"),
}

var colors = map[string]ui.Color{
	"sqlite":     ui.Hex("#003B57"),
	"postgresql": ui.Hex("#4169E1"),
	"mysql":      ui.Hex("#4479A1"),
	"redis":      ui.Hex("#FF4438"),
	"mongodb":    ui.Hex("#00A35C"),
	"etcd":       ui.Hex("#419EDA"),
}

func parse(name string) *ui.SVG {
	data, err := assets.ReadFile("assets/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

// Icon returns the compact brand mark and its brand color for a database driver.
func Icon(driver string) (*ui.SVG, ui.Color, bool) {
	icon, ok := icons[driver]
	return icon, colors[driver], ok
}
