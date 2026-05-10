package domain

type RegistryClient interface {
	RegisterTunnel(tunnelID, agentID string) error
	DeregisterTunnel(tunnelID string) error
	Heartbeat(tunnelID string) error
}
