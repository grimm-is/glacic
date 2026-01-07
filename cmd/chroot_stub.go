//go:build !linux

package cmd


func setupChroot(jailPath string) error {
	Printer.Println("Warning: Chroot not supported on this OS")
	return nil
}

func enterChroot(jailPath string) error {
	return nil
}
