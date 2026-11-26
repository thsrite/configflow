package main

// All HTTP JSON response structs declare their exported fields in strict
// lexicographical order of their JSON tag names so encoding/json emits
// byte-for-byte identical output to the previous map[string]any payloads.

type agentAddressGetResponse struct {
	Addresses  []string `json:"addresses"`
	Configured string   `json:"configured"`
	Protocol   int      `json:"protocol"`
	Reported   string   `json:"reported"`
	Success    bool     `json:"success"`
	Writable   bool     `json:"writable"`
}

type agentAddressInvalidResponse struct {
	Addresses []string `json:"addresses"`
	Message   string   `json:"message"`
	Success   bool     `json:"success"`
}

type agentAddressPostResponse struct {
	Host    string `json:"host"`
	Success bool   `json:"success"`
}

type domainProbeResponse struct {
	Results []probeTargetResult `json:"results"`
	Success bool                `json:"success"`
}

type regionTargetsResponse struct {
	Success bool           `json:"success"`
	Targets []regionTarget `json:"targets"`
}

type regionCheckResponse struct {
	Results []regionResult `json:"results"`
	Success bool           `json:"success"`
	Target  string         `json:"target"`
}
