package types

// FreeIPInfo contains IP and its associated NodeIDs
type FreeIPInfo struct {
	IP      string   `json:"ip"`
	NodeIDs []string `json:"node_ids"`
}
