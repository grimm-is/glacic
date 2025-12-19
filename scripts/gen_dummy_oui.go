package main

import (
	"grimm.is/glacic/internal/network"
	"fmt"
	"os"
)

func main() {
	db := &network.OUIDB{
		Entries: map[string]network.OUIEntry{
			"005056": {Manufacturer: "VMware, Inc.", Country: "US"},
			"525400": {Manufacturer: "QEMU Virtual NIC", Country: "US"},
		},
	}

	if err := db.Save("internal/network/assets/oui.db.gz"); err != nil {
		fmt.Printf("Failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated dummy OUI DB")
}
