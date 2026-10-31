package cloudsync

// Vault keeps tokens in the OS credential store. There is no plaintext fallback.
type Vault interface {
	Get(account string) (string, error)
	Set(account, token string) error
	Delete(account string) error
}
type SystemVault struct{}
