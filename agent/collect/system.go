package collect

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/technonext/chowkidar/agent/types"
)

type SystemCollector struct{}

func NewSystemCollector() *SystemCollector {
	return &SystemCollector{}
}

func (s *SystemCollector) Collect() (types.SystemMetrics, error) {
	// cpu.Percent with percpu=false returns a single overall average
	percent, err := cpu.Percent(0, false)
	if err != nil {
		return types.SystemMetrics{}, err
	}

	memStat, err := mem.VirtualMemory()
	if err != nil {
		return types.SystemMetrics{}, err
	}

	diskStat, err := disk.Usage("/")
	if err != nil {
		return types.SystemMetrics{}, err
	}

	cpuAvg := 0.0
	if len(percent) > 0 {
		cpuAvg = percent[0]
	}

	return types.SystemMetrics{
		CPUPercent:  cpuAvg,
		MemTotalGB:  toGB(memStat.Total),
		MemUsedGB:   toGB(memStat.Used),
		DiskTotalGB: toGB(diskStat.Total),
		DiskUsedGB:  toGB(diskStat.Used),
	}, nil
}

func toGB(bytes uint64) float64 {
	return float64(bytes) / 1024 / 1024 / 1024
}
