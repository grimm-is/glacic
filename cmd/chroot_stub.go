//go:build !linux

package cmd

import "fmt"

func setupChroot(jailPath string) error {
	fmt.Println("Warning: Chroot not supported on this OS")
	return nil
}

func enterChroot(jailPath string) error {
	return nil
}
