package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"grimm.is/glacic/internal/network"
)

func main() {
	useReal := flag.Bool("real", false, "Download real IEEE OUI data (slow, requires network)")
	flag.Parse()

	var db *network.OUIDB
	var err error

	if *useReal {
		fmt.Println("Downloading IEEE OUI database...")
		start := time.Now()
		db, err = network.BuildOUIDB()
		if err != nil {
			fmt.Printf("Failed to download OUI data: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Downloaded %d entries in %v\n", len(db.Entries), time.Since(start))
	} else {
		// Common vendor prefixes for testing
		db = &network.OUIDB{
			Entries: map[string]network.OUIEntry{
				// Common router/network equipment
				"005056": {Manufacturer: "VMware, Inc.", Country: "US"},
				"525400": {Manufacturer: "QEMU Virtual NIC", Country: "US"},
				"000C29": {Manufacturer: "VMware, Inc.", Country: "US"},
				"001C42": {Manufacturer: "Parallels, Inc.", Country: "US"},
				"080027": {Manufacturer: "Oracle VirtualBox", Country: "US"},
				// Apple
				"A4C361": {Manufacturer: "Apple, Inc.", Country: "US"},
				"A8667F": {Manufacturer: "Apple, Inc.", Country: "US"},
				"F0B479": {Manufacturer: "Apple, Inc.", Country: "US"},
				"14C213": {Manufacturer: "Apple, Inc.", Country: "US"},
				"38F9D3": {Manufacturer: "Apple, Inc.", Country: "US"},
				"60FACD": {Manufacturer: "Apple, Inc.", Country: "US"},
				"78CA39": {Manufacturer: "Apple, Inc.", Country: "US"},
				"88E87F": {Manufacturer: "Apple, Inc.", Country: "US"},
				"AC1F74": {Manufacturer: "Apple, Inc.", Country: "US"},
				"D4619D": {Manufacturer: "Apple, Inc.", Country: "US"},
				// TP-Link
				"10FE2B": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				"14EB08": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				"30B49E": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				"54A7D3": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				"98DA0C": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				// Ubiquiti
				"24A43C": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				"44D9E7": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				"788A20": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				"B4FBE4": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				"F09FC2": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				"FC6C3F": {Manufacturer: "Ubiquiti Inc", Country: "US"},
				// Netgear
				"000FB5": {Manufacturer: "Netgear", Country: "US"},
				"20E52A": {Manufacturer: "Netgear", Country: "US"},
				"4CED63": {Manufacturer: "Netgear", Country: "US"},
				"6CB0CE": {Manufacturer: "Netgear", Country: "US"},
				"84F3EB": {Manufacturer: "Netgear", Country: "US"},
				"A00460": {Manufacturer: "Netgear", Country: "US"},
				// Cisco/Linksys
				"000F66": {Manufacturer: "Cisco-Linksys", Country: "US"},
				"001217": {Manufacturer: "Cisco-Linksys", Country: "US"},
				"001310": {Manufacturer: "Cisco-Linksys", Country: "US"},
				"001E58": {Manufacturer: "Cisco-Linksys", Country: "US"},
				"00233F": {Manufacturer: "Cisco Systems", Country: "US"},
				// ASUS
				"048D38": {Manufacturer: "ASUS", Country: "TW"},
				"105A17": {Manufacturer: "ASUS", Country: "TW"},
				"2C4D54": {Manufacturer: "ASUS", Country: "TW"},
				"40B076": {Manufacturer: "ASUS", Country: "TW"},
				"90E6BA": {Manufacturer: "ASUS", Country: "TW"},
				// Intel
				"002500": {Manufacturer: "Intel Corporate", Country: "US"},
				"003067": {Manufacturer: "Intel Corporate", Country: "US"},
				"00D861": {Manufacturer: "Intel Corporate", Country: "US"},
				"18CC18": {Manufacturer: "Intel Corporate", Country: "US"},
				"48452B": {Manufacturer: "Intel Corporate", Country: "US"},
				"4C346B": {Manufacturer: "Intel Corporate", Country: "US"},
				"8C8D28": {Manufacturer: "Intel Corporate", Country: "US"},
				"D4F5C7": {Manufacturer: "Intel Corporate", Country: "US"},
				// Broadcom
				"0010A4": {Manufacturer: "Broadcom", Country: "US"},
				// Dell
				"002219": {Manufacturer: "Dell Inc.", Country: "US"},
				"B083FE": {Manufacturer: "Dell Inc.", Country: "US"},
				// HP
				"001E0B": {Manufacturer: "Hewlett Packard", Country: "US"},
				"0022B0": {Manufacturer: "Hewlett Packard", Country: "US"},
				"A0D3C1": {Manufacturer: "Hewlett Packard", Country: "US"},
				// Samsung
				"002162": {Manufacturer: "Samsung Electronics", Country: "KR"},
				"84250D": {Manufacturer: "Samsung Electronics", Country: "KR"},
				"D8578B": {Manufacturer: "Samsung Electronics", Country: "KR"},
				// Raspberry Pi
				"B827EB": {Manufacturer: "Raspberry Pi Foundation", Country: "GB"},
				"DCEEB9": {Manufacturer: "Raspberry Pi Foundation", Country: "GB"},
				"E45F01": {Manufacturer: "Raspberry Pi Foundation", Country: "GB"},
				// Amazon
				"38D4D4": {Manufacturer: "Amazon Technologies", Country: "US"},
				"68D691": {Manufacturer: "Amazon Technologies", Country: "US"},
				"849845": {Manufacturer: "Amazon Technologies", Country: "US"},
				// Google
				"3C5AB4": {Manufacturer: "Google, Inc.", Country: "US"},
				"548913": {Manufacturer: "Google, Inc.", Country: "US"},
				"F45C89": {Manufacturer: "Google, Inc.", Country: "US"},
				// Microsoft
				"303926": {Manufacturer: "Microsoft Corporation", Country: "US"},
				"38F23E": {Manufacturer: "Microsoft Corporation", Country: "US"},
				"28188A": {Manufacturer: "Microsoft Corporation", Country: "US"},
				// Sonos
				"78281C": {Manufacturer: "Sonos, Inc.", Country: "US"},
				"B8E937": {Manufacturer: "Sonos, Inc.", Country: "US"},
				// TP-Link (ec:38:73 - user's gateway)
				"EC3873": {Manufacturer: "TP-Link Technologies", Country: "CN"},
				// Realtek
				"00E04C": {Manufacturer: "Realtek Semiconductor", Country: "TW"},
				"525000": {Manufacturer: "Realtek Semiconductor", Country: "TW"},
				// ESP8266/ESP32 (smart home devices)
				"18FE34": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"24A16D": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"24B2DE": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"2C3AE8": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"30AEA4": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"40F520": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"680AE2": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"806F9A": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"98F4AB": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"A4CF12": {Manufacturer: "Espressif Inc.", Country: "CN"},
				"BC658E": {Manufacturer: "Espressif Inc.", Country: "CN"},
			},
		}
		fmt.Printf("Generated curated OUI database with %d entries\n", len(db.Entries))
		fmt.Println("Run with -real flag to download full IEEE database (~35k entries)")
	}

	if err := db.Save("internal/network/assets/oui.db.gz"); err != nil {
		fmt.Printf("Failed to save: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Saved to internal/network/assets/oui.db.gz")
}
