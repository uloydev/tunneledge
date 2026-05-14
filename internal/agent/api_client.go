package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"tunneledge/pkg/config"
)

type APIClient struct {
	baseURL    string
	agentToken string
	httpClient *http.Client
}

type apiTunnelResponse struct {
	ID        uint   `json:"id"`
	Label     string `json:"label"`
	LocalAddr string `json:"local_addr"`
}

func NewAPIClient(baseURL, agentToken string) *APIClient {
	return &APIClient{
		baseURL:    baseURL,
		agentToken: agentToken,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *APIClient) FetchTunnels(agentProfileID uint) ([]config.TunnelConfig, error) {
	url := fmt.Sprintf("%s/api/v1/agents/%d/tunnels", c.baseURL, agentProfileID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tunnels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var tunnels []apiTunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&tunnels); err != nil {
		return nil, fmt.Errorf("failed to decode tunnels: %w", err)
	}

	result := make([]config.TunnelConfig, 0, len(tunnels))
	for _, t := range tunnels {
		result = append(result, config.TunnelConfig{
			Label:     t.Label,
			LocalAddr: t.LocalAddr,
		})
	}
	return result, nil
}

func (c *APIClient) CreateTunnel(agentProfileID uint, label, localAddr string) error {
	url := fmt.Sprintf("%s/api/v1/agents/%d/tunnels", c.baseURL, agentProfileID)
	body, _ := json.Marshal(map[string]string{
		"label":      label,
		"local_addr": localAddr,
	})

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create tunnel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *APIClient) DeleteTunnel(agentProfileID, tunnelID uint) error {
	url := fmt.Sprintf("%s/api/v1/agents/%d/tunnels/%d", c.baseURL, agentProfileID, tunnelID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete tunnel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *APIClient) UpdateTunnel(agentProfileID, tunnelID uint, label, localAddr string) error {
	url := fmt.Sprintf("%s/api/v1/agents/%d/tunnels/%d", c.baseURL, agentProfileID, tunnelID)
	body, _ := json.Marshal(map[string]string{
		"label":      label,
		"local_addr": localAddr,
	})

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.agentToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update tunnel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
