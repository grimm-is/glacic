package mdns

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// DeviceType enumerates supported device categories
type DeviceType string

const (
	DeviceTypeCast          DeviceType = "Google Cast"
	DeviceTypeHomeKit       DeviceType = "Apple HomeKit"
	DeviceTypePrinter       DeviceType = "Printer"
	DeviceTypeApple         DeviceType = "Apple Device"
	DeviceTypePlayService   DeviceType = "Android / Play Services"
	DeviceTypeMatter        DeviceType = "Matter"
	DeviceTypeSpotify       DeviceType = "Spotify Connect"
	DeviceTypeHue           DeviceType = "Philips Hue"
	DeviceTypeHomeAssistant DeviceType = "Home Assistant"
	DeviceTypeTimeMachine   DeviceType = "Time Machine"
	DeviceTypeRoku          DeviceType = "Roku"
	DeviceTypeADB           DeviceType = "ADB"
	DeviceTypeOctoPrint     DeviceType = "OctoPrint"
	DeviceTypeUnknown       DeviceType = "Unknown"
)

// DeviceProfile contains interpreted device information
type DeviceProfile struct {
	Type         DeviceType `json:"type"`
	Model        string     `json:"model"`
	FriendlyName string     `json:"friendly_name"`
	State        string     `json:"state,omitempty"`      // Human readable state
	ExtraInfo    string     `json:"extra_info,omitempty"` // BSSID, App, etc.

	// Specific Structs
	Cast          *CastInfo          `json:"cast,omitempty"`
	HomeKit       *HomeKitInfo       `json:"homekit,omitempty"`
	Printer       *PrinterInfo       `json:"printer,omitempty"`
	Apple         *AppleInfo         `json:"apple,omitempty"`
	PlayService   *PlayServiceInfo   `json:"play_service,omitempty"`
	Matter        *MatterInfo        `json:"matter,omitempty"`
	Spotify       *SpotifyInfo       `json:"spotify,omitempty"`
	Hue           *HueInfo           `json:"hue,omitempty"`
	HomeAssistant *HomeAssistantInfo `json:"home_assistant,omitempty"`
	TimeMachine   *TimeMachineInfo   `json:"time_machine,omitempty"`
	Roku          *RokuInfo          `json:"roku,omitempty"`
	ADB           *ADBInfo           `json:"adb,omitempty"`
	OctoPrint     *OctoPrintInfo     `json:"octoprint,omitempty"`
	Generic       *GenericInfo       `json:"generic,omitempty"`
}

// --- Specific Protocol Structs ---

type CastInfo struct {
	FriendlyName string   `json:"friendly_name"`
	Model        string   `json:"model"`
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	WiFiBSSID    string   `json:"wifi_bssid"`
	Capabilities []string `json:"capabilities"`
}

type HomeKitInfo struct {
	Model         string `json:"model"`
	Category      string `json:"category"`
	PairingStatus string `json:"pairing_status"`
	ConfigVer     string `json:"config_ver"`
}

type PrinterInfo struct {
	Model      string `json:"model"`
	State      string `json:"state"`
	AdminURL   string `json:"admin_url"`
	IsAirPrint bool   `json:"is_airprint"`
}

type AppleInfo struct {
	HardwareModel string `json:"hardware_model"`
	OSVersion     string `json:"os_version"`
	MacAddress    string `json:"mac_address"`
}

type PlayServiceInfo struct {
	EndpointID   string `json:"endpoint_id"`
	DecodedID    string `json:"decoded_id"`
	DirectIP     string `json:"direct_ip"`
	Capabilities int    `json:"capabilities"`
}

type MatterInfo struct {
	DeviceName string `json:"device_name"`
	VendorID   string `json:"vendor_id"`
	ProductID  string `json:"product_id"`
	IsSleepy   bool   `json:"is_sleepy"`
}

type SpotifyInfo struct {
	Version string `json:"version"`
	CPath   string `json:"cpath"`
	Stack   string `json:"stack"`
}

type HueInfo struct {
	BridgeID string `json:"bridge_id"`
	ModelID  string `json:"model_id"`
}

type HomeAssistantInfo struct {
	LocationName string `json:"location_name"`
	Version      string `json:"version"`
	UUID         string `json:"uuid"`
}

type TimeMachineInfo struct {
	VolumeName  string `json:"volume_name"`
	CapacityGB  int    `json:"capacity_gb"`
	SupportsMac bool   `json:"supports_mac"`
	Model       string `json:"model"`
}

type RokuInfo struct {
	DeviceType string `json:"device_type"`
	ECPPort    string `json:"ecp_port"`
}

type ADBInfo struct {
	Port    string `json:"port"`
	Version string `json:"version"`
}

type OctoPrintInfo struct {
	Version string `json:"version"`
	APIPath string `json:"api_path"`
}

type GenericInfo struct {
	GuessedVendor    string            `json:"guessed_vendor"`
	InterestingKeys  map[string]string `json:"interesting_keys"`
	ReadableServices []string          `json:"readable_services"`
}

// HomeKit Category IDs (ci)
var hapCategories = map[int]string{
	1: "Other", 2: "Bridge", 3: "Fan", 4: "Garage",
	5: "Lightbulb", 6: "Door Lock", 7: "Outlet", 8: "Switch",
	9: "Thermostat", 10: "Sensor", 11: "Security System", 12: "Door",
	13: "Window", 14: "Window Covering", 15: "Printer", 16: "Humidifier",
	17: "Dehumidifier", 19: "Air Purifier", 20: "Heater", 21: "AC",
	23: "Faucet", 24: "Sprinkler", 32: "Video Doorbell",
}

// serviceRegistry maps common mDNS service strings to human readable names
var serviceRegistry = map[string]string{
	// --- Standard Network Services ---
	"_http._tcp":        "Web Server",
	"_https._tcp":       "Secure Web Server",
	"_ssh._tcp":         "SSH Remote Login",
	"_sftp-ssh._tcp":    "SFTP File Transfer",
	"_ftp._tcp":         "FTP File Transfer",
	"_tftp._udp":        "TFTP Service",
	"_telnet._tcp":      "Telnet",
	"_workstation._tcp": "Workstation/PC",
	"_rfb._tcp":         "VNC Remote Desktop",
	"_rdp._tcp":         "Windows Remote Desktop",
	"_sleep-proxy._udp": "Bonjour Sleep Proxy",

	// --- File Sharing ---
	"_smb._tcp":        "SMB/CIFS File Sharing",
	"_adisk._tcp":      "Apple Time Machine Target",
	"_afpovertcp._tcp": "Apple Filing Protocol",
	"_nfs._tcp":        "NFS Network File System",
	"_webdav._tcp":     "WebDAV File Share",

	// --- Printers & Scanners ---
	"_ipp._tcp":            "Internet Printing Protocol",
	"_ipps._tcp":           "Secure Internet Printing",
	"_printer._tcp":        "LPR Printer",
	"_pdl-datastream._tcp": "PDL Printer Stream",
	"_scanner._tcp":        "Scanner",
	"_uscan._tcp":          "Universal Scanner",

	// --- Apple Ecosystem ---
	"_airplay._tcp":        "Apple AirPlay Video",
	"_raop._tcp":           "Apple AirPlay Audio",
	"_hap._tcp":            "HomeKit Accessory Protocol",
	"_companion-link._tcp": "Apple Companion Link",
	"_device-info._tcp":    "Device Info",
	"_airport._tcp":        "Apple AirPort Base Station",
	"_home-sharing._tcp":   "iTunes Home Sharing",
	"_touch-able._tcp":     "iPhone/iPod Touch Remote",
	"_apple-mobdev2._tcp":  "macOS Wi-Fi Sync",

	// --- Google / Android ---
	"_googlecast._tcp":       "Google Cast",
	"_adb._tcp":              "Android Debug Bridge",
	"_androidtvremote2._tcp": "Android TV Remote (v2)",
	"_googlezone._tcp":       "Google Nest Audio Group",

	// --- Amazon / Alexa ---
	"_amzn-wplay._tcp":        "Amazon Fire TV (WhisperPlay)",
	"_amazonecho-remote._tcp": "Amazon Echo Remote",

	// --- Media & Entertainment ---
	"_spotify-connect._tcp": "Spotify Connect",
	"_sonos._tcp":           "Sonos Speaker",
	"_soundtouch._tcp":      "Bose SoundTouch",
	"_bose._tcp":            "Bose Audio",
	"_heos._tcp":            "Denon HEOS",
	"_plexmediasvr._tcp":    "Plex Media Server",
	"_jellyfin._tcp":        "Jellyfin Media Server",
	"_emby._tcp":            "Emby Media Server",
	"_steam-streaming._udp": "Steam In-Home Streaming",
	"_nvstream._tcp":        "Nvidia GameStream",
	"_roku._tcp":            "Roku Control Protocol",
	"_rsp._tcp":             "Roku Server Protocol",
	"_tivo_videos._tcp":     "TiVo Media",
	"_mpd._tcp":             "Music Player Daemon",
	"_volumio._tcp":         "Volumio Audio",
	"_daap._tcp":            "iTunes Music Sharing",

	// --- Smart Home / IoT ---
	"_matter._tcp":                "Matter Device",
	"_matterc._udp":               "Matter Commissioning",
	"_home-assistant._tcp":        "Home Assistant",
	"_hue._tcp":                   "Philips Hue Bridge",
	"_philipshue._tcp":            "Philips Hue Bridge",
	"_bond._tcp":                  "Bond Home Bridge",
	"_lutron._tcp":                "Lutron Lighting",
	"_shelly._tcp":                "Shelly IoT Device",
	"_tplink._tcp":                "TP-Link Smart Device",
	"_aqara._tcp":                 "Aqara Hub",
	"_aqara-setup._tcp":           "Aqara Setup",
	"_wled._tcp":                  "WLED Light",
	"_esphomelib._tcp":            "ESPHome Device",
	"_nanoleafapi._tcp":           "Nanoleaf Light Panels",
	"_octoprint._tcp":             "OctoPrint Server",
	"_vizio-cast._tcp":            "Vizio SmartCast",
	"_logitech-reverse-host._tcp": "Harmony Hub",

	// --- Databases & Infrastructure ---
	"_postgresql._tcp":    "PostgreSQL Database",
	"_mysql._tcp":         "MySQL Database",
	"_mongo._tcp":         "MongoDB",
	"_elasticsearch._tcp": "Elasticsearch",
	"_prometheus._tcp":    "Prometheus Metrics",
	"_mqtt._tcp":          "MQTT Broker",
	"_influxdb._tcp":      "InfluxDB",
	"_coap._udp":          "CoAP Protocol",
	"_distcc._tcp":        "Distributed C Compiler",

	// --- Creative & Productivity ---
	"_sketchmirror._tcp":    "Sketch App Mirror",
	"_photoshopserver._tcp": "Adobe Photoshop Server",
	"_omnistate._tcp":       "OmniGroup App",
	"_keynotecontrol._tcp":  "Keynote Remote",
	"_teamviewer._tcp":      "TeamViewer",
	"_ndi._tcp":             "NDI (Network Device Interface)",
}

// AnalyzeDevice interprets the parsed mDNS data to valid device profiles
func AnalyzeDevice(parsed *ParsedMDNS) *DeviceProfile {
	profile := &DeviceProfile{
		Type:  DeviceTypeUnknown,
		Model: "Unknown Device",
	}

	txt := parsed.TXTRecords
	services := parsed.Services

	// Classification candidate
	type candidate struct {
		Type         DeviceType
		Score        int
		Model        string
		FriendlyName string
		State        string
		ExtraInfo    string
	}
	var candidates []candidate

	// Helper to add candidate
	add := func(c candidate) {
		candidates = append(candidates, c)
	}

	// 1. Google Cast (Score: 20) - Protocol/Service
	if hasService(services, "_googlecast._tcp") || txt["fn"] != "" {
		profile.Cast = parseCast(txt)
		extra := ""
		if profile.Cast.WiFiBSSID != "" {
			extra = fmt.Sprintf("[WiFi AP: %s]", profile.Cast.WiFiBSSID)
		}
		if len(profile.Cast.Capabilities) > 0 {
			if extra != "" {
				extra += " "
			}
			extra += fmt.Sprintf("Caps: %s", strings.Join(profile.Cast.Capabilities, ", "))
		}
		add(candidate{
			Type:         DeviceTypeCast,
			Score:        20,
			Model:        profile.Cast.Model,
			FriendlyName: profile.Cast.FriendlyName,
			State:        profile.Cast.Status,
			ExtraInfo:    extra,
		})
	}

	// 2. HomeKit (Score: 30) - Often implies specific smart hardware
	if hasService(services, "_hap._tcp") || txt["sf"] != "" {
		profile.HomeKit = parseHomeKit(txt)
		add(candidate{
			Type:         DeviceTypeHomeKit,
			Score:        30,
			Model:        profile.HomeKit.Model,
			FriendlyName: fmt.Sprintf("%s (%s)", profile.HomeKit.Model, profile.HomeKit.Category),
			State:        profile.HomeKit.PairingStatus,
			ExtraInfo:    fmt.Sprintf("Config Ver: %s", profile.HomeKit.ConfigVer),
		})
	}

	// 3. Printer (Score: 40) - Hardware
	if hasService(services, "_ipp._tcp") || hasService(services, "_printer._tcp") || txt["pdl"] != "" {
		profile.Printer = parsePrinter(txt)
		extra := ""
		if profile.Printer.IsAirPrint {
			extra = "AirPrint Supported"
		}
		add(candidate{
			Type:      DeviceTypePrinter,
			Score:     40,
			Model:     profile.Printer.Model,
			State:     profile.Printer.State,
			ExtraInfo: extra,
		})
	}

	// 4. Apple Device (Score: 50) - Hardware Identity (Mac, iPhone, etc)
	if hasKey(txt, "model") && strings.Contains(txt["model"], "Mac") {
		profile.Apple = parseApple(txt)
		add(candidate{
			Type:         DeviceTypeApple,
			Score:        50,
			Model:        profile.Apple.HardwareModel,
			FriendlyName: profile.Apple.HardwareModel,
			ExtraInfo:    fmt.Sprintf("%s (ID: %s)", profile.Apple.OSVersion, profile.Apple.MacAddress),
		})
	}

	// 5. Android/Play Services (Score: 15) - Protocol
	if hasKey(txt, "n") && (hasKey(txt, "f") || hasService(services, "_FC9F5ED42C8A._tcp")) {
		profile.PlayService = parsePlayServices(txt)
		add(candidate{
			Type:      DeviceTypePlayService,
			Score:     15,
			Model:     "Android Device",
			State:     "Privacy Mode Active",
			ExtraInfo: fmt.Sprintf("Endpoint: %s", profile.PlayService.EndpointID),
		})
	}

	// 6. Matter (Score: 35) - Hardware
	if hasService(services, "_matter._tcp") || hasService(services, "_matterc._udp") {
		profile.Matter = parseMatter(txt)
		add(candidate{
			Type:         DeviceTypeMatter,
			Score:        35,
			FriendlyName: profile.Matter.DeviceName,
			ExtraInfo:    fmt.Sprintf("Sleepy: %v", profile.Matter.IsSleepy),
		})
	}

	// 7. Spotify (Score: 10) - App/Protocol
	if hasService(services, "_spotify-connect._tcp") {
		profile.Spotify = parseSpotify(txt)
		add(candidate{
			Type:      DeviceTypeSpotify,
			Score:     10,
			ExtraInfo: fmt.Sprintf("Connect v%s", profile.Spotify.Version),
		})
	}

	// 8. Hue (Score: 45) - Specific Hardware Bridge
	if hasService(services, "_hue._tcp") {
		profile.Hue = parseHue(txt)
		add(candidate{
			Type:  DeviceTypeHue,
			Score: 45,
			Model: fmt.Sprintf("Bridge %s", profile.Hue.ModelID),
		})
	}

	// 9. Home Assistant (Score: 45) - Specific Server
	if hasService(services, "_home-assistant._tcp") {
		profile.HomeAssistant = parseHA(txt)
		add(candidate{
			Type:         DeviceTypeHomeAssistant,
			Score:        45,
			FriendlyName: profile.HomeAssistant.LocationName,
			ExtraInfo:    fmt.Sprintf("v%s", profile.HomeAssistant.Version),
		})
	}

	// 10. Time Machine (Score: 40) - NAS Function
	if hasService(services, "_adisk._tcp") || (hasService(services, "_smb._tcp") && hasKey(txt, "dk0")) {
		profile.TimeMachine = parseTimeMachine(txt)
		add(candidate{
			Type:         DeviceTypeTimeMachine,
			Score:        40,
			Model:        profile.TimeMachine.Model,
			FriendlyName: profile.TimeMachine.VolumeName,
			ExtraInfo:    fmt.Sprintf("%d GB", profile.TimeMachine.CapacityGB),
		})
	}

	// 11. Roku (Score: 30) - Hardware
	if hasService(services, "_roku._tcp") {
		profile.Roku = parseRoku(txt)
		add(candidate{
			Type:  DeviceTypeRoku,
			Score: 30,
			Model: profile.Roku.DeviceType,
		})
	}

	// 12. ADB (Score: 15) - Protocol (Dev mode)
	if hasService(services, "_adb._tcp") || hasService(services, "_adb_secure._tcp") {
		profile.ADB = parseADB(txt)
		add(candidate{
			Type:      DeviceTypeADB,
			Score:     15,
			ExtraInfo: fmt.Sprintf("v%s Port: %s", profile.ADB.Version, profile.ADB.Port),
		})
	}

	// 13. OctoPrint (Score: 40) - Specific Server
	if hasService(services, "_octoprint._tcp") {
		profile.OctoPrint = parseOctoPrint(txt)
		add(candidate{
			Type:      DeviceTypeOctoPrint,
			Score:     40,
			Model:     "3D Printer Server",
			ExtraInfo: fmt.Sprintf("v%s", profile.OctoPrint.Version),
		})
	}

	// Generic & Fallback
	profile.Generic = parseGeneric(txt, services)
	if profile.Generic.GuessedVendor != "" {
		// If we already have candidates, validation check:
		// Does GuessedVendor match current best? Ideally yes.
	}

	// Select Winner
	var best candidate
	for _, c := range candidates {
		if c.Score > best.Score {
			best = c
		}
	}

	if best.Score > 0 {
		profile.Type = best.Type
		if best.Model != "" {
			profile.Model = best.Model
		}
		if best.FriendlyName != "" {
			profile.FriendlyName = best.FriendlyName
		}
		if best.State != "" {
			profile.State = best.State
		}
		if best.ExtraInfo != "" {
			profile.ExtraInfo = best.ExtraInfo
		}
	} else {
		// Fallback purely based on Generic info
		if profile.Generic.GuessedVendor != "" {
			profile.FriendlyName = fmt.Sprintf("%s Device", profile.Generic.GuessedVendor)
		}
		if len(parsed.Hostname) > 0 {
			if profile.FriendlyName == "" {
				profile.FriendlyName = parsed.Hostname
			}
		}
	}

	return profile
}

// hasService checks if a service substring exists in the list
func hasService(list []string, target string) bool {
	for _, s := range list {
		if strings.Contains(s, target) {
			return true
		}
	}
	return false
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

// --- Parser Implementations ---

func parseCast(txt map[string]string) *CastInfo {
	c := &CastInfo{FriendlyName: txt["fn"], Model: txt["model"], ID: txt["id"], WiFiBSSID: txt["bs"]}
	if c.Model == "" {
		c.Model = txt["md"]
	}
	if c.Model != "" && txt["manufacturer"] != "" {
		c.Model = fmt.Sprintf("%s %s", txt["manufacturer"], c.Model)
	}

	if txt["st"] == "0" {
		c.Status = "Idle"
		if txt["rs"] != "" {
			c.Status += " (App: " + txt["rs"] + ")"
		}
	} else if txt["st"] == "1" {
		c.Status = "Active / Playing"
	}

	if val, err := strconv.ParseInt(txt["ca"], 10, 64); err == nil {
		if val&1 != 0 {
			c.Capabilities = append(c.Capabilities, "Video")
		}
		if val&4 != 0 {
			c.Capabilities = append(c.Capabilities, "Audio")
		}
		if val&32 != 0 {
			c.Capabilities = append(c.Capabilities, "Group Audio")
		}
	}
	return c
}

func parseHomeKit(txt map[string]string) *HomeKitInfo {
	h := &HomeKitInfo{Model: txt["md"], ConfigVer: txt["c#"]}
	if id, err := strconv.Atoi(txt["ci"]); err == nil {
		h.Category = hapCategories[id]
		if h.Category == "" {
			h.Category = fmt.Sprintf("Unknown (%d)", id)
		}
	}
	if txt["sf"] == "1" {
		h.PairingStatus = "Unpaired"
	} else {
		h.PairingStatus = "Paired"
	}
	return h
}

func parsePrinter(txt map[string]string) *PrinterInfo {
	p := &PrinterInfo{AdminURL: txt["adminurl"]}
	if val, ok := txt["ty"]; ok {
		p.Model = val
	} else {
		p.Model = txt["product"]
	}
	if strings.Contains(txt["pdl"], "image/urf") {
		p.IsAirPrint = true
	}
	switch txt["printer-state"] {
	case "3":
		p.State = "Idle"
	case "4":
		p.State = "Processing"
	case "5":
		p.State = "Stopped"
	default:
		p.State = "Unknown"
	}
	return p
}

func parseApple(txt map[string]string) *AppleInfo {
	// e.g. MacBookPro18,1
	a := &AppleInfo{HardwareModel: txt["model"], MacAddress: txt["deviceid"]}
	switch txt["osxvers"] {
	case "25":
		a.OSVersion = "macOS 16 (Beta)"
	case "24":
		a.OSVersion = "macOS 15 (Sequoia)"
	case "23":
		a.OSVersion = "macOS 14 (Sonoma)"
	case "22":
		a.OSVersion = "macOS 13 (Ventura)"
	default:
		if txt["osxvers"] != "" {
			a.OSVersion = "Darwin Kernel " + txt["osxvers"]
		}
	}
	return a
}

func parsePlayServices(txt map[string]string) *PlayServiceInfo {
	ps := &PlayServiceInfo{EndpointID: txt["n"], DirectIP: txt["IPv4"]}
	if val, err := strconv.Atoi(txt["f"]); err == nil {
		ps.Capabilities = val
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(txt["n"]); err == nil {
		ps.DecodedID = fmt.Sprintf("%X", decoded)
	}
	return ps
}

func parseMatter(txt map[string]string) *MatterInfo {
	m := &MatterInfo{DeviceName: txt["DN"]}
	m.VendorID = txt["VP"]
	if _, ok := txt["SII"]; ok {
		m.IsSleepy = true
	}
	return m
}

func parseSpotify(txt map[string]string) *SpotifyInfo {
	return &SpotifyInfo{Version: txt["VERSION"], CPath: txt["CPath"], Stack: txt["Stack"]}
}

func parseHue(txt map[string]string) *HueInfo {
	return &HueInfo{BridgeID: txt["bridgeid"], ModelID: txt["modelid"]}
}

func parseHA(txt map[string]string) *HomeAssistantInfo {
	return &HomeAssistantInfo{Version: txt["version"], LocationName: txt["location_name"], UUID: txt["uuid"]}
}

func parseTimeMachine(txt map[string]string) *TimeMachineInfo {
	tm := &TimeMachineInfo{VolumeName: txt["adVN"], Model: "NAS Target"}
	if kbStr, ok := txt["dk0"]; ok {
		if kb, err := strconv.Atoi(kbStr); err == nil {
			tm.CapacityGB = kb / 1024 / 1024
		}
	}
	if strings.Contains(txt["sys"], "waMa") {
		tm.SupportsMac = true
	}
	if txt["vendor"] != "" {
		tm.Model = txt["vendor"] + " NAS"
	}
	return tm
}

func parseRoku(txt map[string]string) *RokuInfo {
	return &RokuInfo{DeviceType: txt["rt"], ECPPort: "8060"}
}

func parseADB(txt map[string]string) *ADBInfo {
	return &ADBInfo{Version: txt["v"], Port: "5555"}
}

func parseOctoPrint(txt map[string]string) *OctoPrintInfo {
	return &OctoPrintInfo{Version: txt["version"], APIPath: txt["path"]}
}

func parseGeneric(txt map[string]string, services []string) *GenericInfo {
	g := &GenericInfo{
		InterestingKeys:  make(map[string]string),
		ReadableServices: []string{},
	}

	// 1. Vendor Guessing
	knownVendors := []string{"Synology", "QNAP", "Ubiquiti", "Sonos", "Bose", "Denon", "Yamaha", "Brother", "Epson", "HP", "Canon", "Nest", "Amazon", "Apple", "Google"}

	for _, v := range txt {
		for _, vendor := range knownVendors {
			if strings.Contains(strings.ToLower(v), strings.ToLower(vendor)) {
				g.GuessedVendor = vendor
				break
			}
		}
		if g.GuessedVendor != "" {
			break
		}
	}

	// 2. Extract "Interesting" Keys
	for k, v := range txt {
		isInteresting := false
		keyLower := strings.ToLower(k)
		if strings.Contains(keyLower, "ver") ||
			strings.Contains(keyLower, "build") ||
			strings.Contains(keyLower, "model") ||
			strings.Contains(keyLower, "serial") ||
			strings.Contains(keyLower, "name") ||
			strings.Contains(keyLower, "distro") ||
			strings.Contains(keyLower, "arch") {
			isInteresting = true
		}

		if len(v) > 4 && strings.Contains(v, " ") {
			isInteresting = true
		}

		if isInteresting {
			g.InterestingKeys[k] = v
		}
	}

	// 3. Service Translation
	for _, s := range services {
		// handle _service._tcp vs _service._tcp.local.
		base := strings.TrimSuffix(s, ".local.")
		base = strings.TrimSuffix(base, ".")

		if name, ok := serviceRegistry[base]; ok {
			g.ReadableServices = append(g.ReadableServices, name)
		} else {
			// Try matching just the first part (e.g. _ipp._tcp from _ipp._tcp.local)
			found := false
			for regKey, regName := range serviceRegistry {
				if strings.Contains(s, regKey) {
					g.ReadableServices = append(g.ReadableServices, regName)
					found = true
					break
				}
			}
			if !found {
				g.ReadableServices = append(g.ReadableServices, s)
			}
		}
	}

	// Deduplicate
	g.ReadableServices = uniqueStrings(g.ReadableServices)

	return g
}

func uniqueStrings(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
