package learning

import "strings"

// AppSignatures maps domain suffixes to Application names.
// Keys should be lowercased.
// We use a "longest match wins" logic implies you should check specific ones before generic ones
// if you iterate, but map iteration is random.
// Ideally, the matching logic should check for the longest matching suffix.
var AppSignatures = map[string]string{
	// --- Video Streaming: Netflix ---
	"netflix.com":    "Netflix",
	"netflix.net":    "Netflix",
	"nflxext.com":    "Netflix",
	"nflximg.com":    "Netflix",
	"nflxso.net":     "Netflix",
	"nflxvideo.net":  "Netflix",
	"nflxsearch.net": "Netflix",

	// --- Video Streaming: YouTube / Google ---
	"youtube.com":      "YouTube",
	"youtu.be":         "YouTube",
	"ytimg.com":        "YouTube",
	"googlevideo.com":  "YouTube",
	"ggpht.com":        "YouTube",
	"c.youtube.com":    "YouTube",
	"video.google.com": "YouTube",

	// --- Video Streaming: Amazon Prime Video ---
	"amazonvideo.com":        "Prime Video",
	"prime-video.amazon.dev": "Prime Video",
	"aiv-cdn.net":            "Prime Video",
	"aiv-delivery.net":       "Prime Video",
	"atv-ps.amazon.com":      "Prime Video",
	"pv-cdn.net":             "Prime Video",
	"media-amazon.com":       "Prime Video",
	"ssl-images-amazon.com":  "Prime Video",
	// "video.a2z.com" is often Prime Video assets

	// --- Video Streaming: Disney+ / Hulu / ESPN ---
	// Disney uses BAMTech (Bamgrid) infrastructure
	"disneyplus.com":    "Disney+",
	"disney-plus.net":   "Disney+",
	"disney.cqloud.com": "Disney+",
	"bamgrid.com":       "Disney/Hulu",
	"dssedge.com":       "Disney/Hulu",
	"dssott.com":        "Disney/Hulu",
	// Hulu
	"hulu.com":       "Hulu",
	"huluim.com":     "Hulu",
	"hulustream.com": "Hulu",
	// ESPN
	"espn.com":                 "ESPN",
	"espncdn.com":              "ESPN",
	"espn.net":                 "ESPN",
	"espncricinfo.com":         "ESPN",
	"media.video-cdn.espn.com": "ESPN",

	// --- Video Streaming: HBO / Warner / Discovery ---
	"hbo.com":           "HBO/Max",
	"hbomax.com":        "HBO/Max",
	"max.com":           "HBO/Max",
	"hbonow.com":        "HBO/Max",
	"hbogo.com":         "HBO/Max",
	"play.hbomax.com":   "HBO/Max",
	"discoveryplus.com": "Discovery+",
	"disco-api.com":     "Discovery+",
	"discomax.com":      "Discovery+",

	// --- Video Streaming: Paramount / CBS / Pluto ---
	"paramountplus.com": "Paramount+",
	"pplusstatic.com":   "Paramount+",
	"cbsi.com":          "Paramount/CBS",
	"cbsaavideo.com":    "Paramount/CBS",
	"cbsivideo.com":     "Paramount/CBS",
	"cbsistatic.com":    "Paramount/CBS",
	"saa.cbsi.com":      "Paramount/CBS",
	"pluto.tv":          "Pluto TV",
	"plutotv.net":       "Pluto TV",
	"plutopreprod.tv":   "Pluto TV",

	// --- Video Streaming: Apple TV+ ---
	"tv.apple.com":               "Apple TV+",
	"hls-svod.itunes.apple.com":  "Apple TV+",
	"play-edge.itunes.apple.com": "Apple TV+",
	// Note: "itunes.apple.com" is too generic (could be App Store)

	// --- Video Streaming: Peacock ---
	"peacocktv.com":     "Peacock",
	"cdn.peacocktv.com": "Peacock",
	// Peacock uses "peacocktv.com.c.footprint.net" - this suffix match handles it

	// --- Video Streaming: Others ---
	"crunchyroll.com":     "Crunchyroll",
	"crunchyrollcdn.com":  "Crunchyroll",
	"vrv.co":              "Crunchyroll",
	"tubi.tv":             "Tubi",
	"tubi.io":             "Tubi",
	"starz.com":           "Starz",
	"starzplay.com":       "Starz",
	"mgmplus.com":         "MGM+",
	"hallmarkchannel.com": "Hallmark",
	"hmnow.com":           "Hallmark",

	// --- Social Media ---
	"facebook.com":     "Facebook",
	"fbcdn.net":        "Facebook",
	"fbsbx.com":        "Facebook",
	"instagram.com":    "Instagram",
	"cdninstagram.com": "Instagram",
	"tiktok.com":       "TikTok",
	"tiktokcdn.com":    "TikTok",
	"twitch.tv":        "Twitch",
	"ttvnw.net":        "Twitch",
	"snapchat.com":     "Snapchat",
	"sc-cdn.net":       "Snapchat",

	// --- Gaming (Bonus) ---
	"steampowered.com": "Steam",
	"steamcontent.com": "Steam",
	"xboxlive.com":     "Xbox",
	"playstation.net":  "PlayStation",
	"nintendo.net":     "Nintendo",
}

// IdentifyApp returns the app name for a given domain, or "" if unknown.
// It implements a "longest suffix match" to ensure specificity.
func IdentifyApp(domain string) string {
	domain = strings.ToLower(domain)

	// 1. Exact match
	if app, ok := AppSignatures[domain]; ok {
		return app
	}

	// 2. Suffix match
	// We want the longest match (e.g. "video.google.com" > "google.com")
	// Since our map is small (~100 items), iterating is fast enough (nanoseconds).
	var bestMatch string
	var longestLen int

	for suffix, app := range AppSignatures {
		if strings.HasSuffix(domain, suffix) {
			// Check for dot boundary to prevent partial matches
			// e.g. "notnetflix.com" should not match "netflix.com"
			// Valid: "video.netflix.com" (len > suffix) && char before suffix is '.'

			suffixLen := len(suffix)
			domainLen := len(domain)

			// If exact match (already caught above, but safety check)
			if domainLen == suffixLen {
				if suffixLen > longestLen {
					bestMatch = app
					longestLen = suffixLen
				}
				continue
			}

			// If subdomain match
			if domainLen > suffixLen && domain[domainLen-suffixLen-1] == '.' {
				if suffixLen > longestLen {
					bestMatch = app
					longestLen = suffixLen
				}
			}
		}
	}
	return bestMatch
}
