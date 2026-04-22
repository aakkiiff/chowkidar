package types

import "time"

type SystemMetrics struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	MemUsedGB   float64 `json:"mem_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
}

type ContainerMetrics struct {
	Name      string  `json:"name"`
	ID        string  `json:"id"`
	Image     string  `json:"image"`
	Status    string  `json:"status"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsedMB float64 `json:"mem_used_mb"`
	MemLimitMB float64 `json:"mem_limit_mb"`
}

type Report struct {
	ServerName   string             `json:"server_name"`
	Identity     string             `json:"identity"`
	Timestamp    time.Time           `json:"timestamp"`
	System       SystemMetrics        `json:"system"`
	Containers   []ContainerMetrics  `json:"containers"`
}
