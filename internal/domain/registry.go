package domain

type RegistryClient interface {
	RegisterTunnel(tunnelID, agentID, publicAddr, localAddr string) error
	DeregisterTunnel(tunnelID string) error
	Heartbeat(tunnelID string) error
}
