//go:build (!darwin && !windows) || (darwin && !cgo)

package cloudsync

import "fmt"

func (SystemVault) Get(string) (string, error) {
	return "", fmt.Errorf("cloud sync requires macOS Keychain or Windows Credential Manager")
}
func (SystemVault) Set(string, string) error {
	return fmt.Errorf("cloud sync requires macOS Keychain or Windows Credential Manager")
}
func (SystemVault) Delete(string) error { return nil }
