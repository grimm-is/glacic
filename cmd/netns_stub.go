//go:build !linux

package cmd

func setupNetworkNamespace() error {
	return nil
}

func isIsolated() bool {
	return false
}

func configureHostFirewall(interfaces []string) error {
	return nil
}

func configureHostFirewallWithNetworks(networks []string) error {
	return nil
}
