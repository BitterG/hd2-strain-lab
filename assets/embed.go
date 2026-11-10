package assets

import _ "embed"

//go:embed war_catalog.json
var WarCatalogJSON []byte

//go:embed war_info.json
var WarInfoJSON []byte

//go:embed status_sample.json
var StatusSampleJSON []byte

//go:embed starmap.json
var StarmapJSON []byte

//go:embed waypoints.json
var WaypointsJSON []byte

//go:embed app_icon.png
var AppIconPNG []byte
