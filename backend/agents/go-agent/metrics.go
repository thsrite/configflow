package main

import (
	"fmt"
	"log"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// SystemMetrics 系统监控数据
type SystemMetrics struct {
	CPU         CPUMetrics     `json:"cpu"`
	Memory      MemoryMetrics  `json:"memory"`
	Disk        DiskMetrics    `json:"disk"`
	Network     NetworkMetrics `json:"network"`
	CollectedAt string         `json:"collected_at"`
}

// CPUMetrics CPU 监控数据
type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"` // CPU 使用率百分比
	CoreCount    int     `json:"core_count"`    // CPU 核心数
}

// MemoryMetrics 内存监控数据
type MemoryMetrics struct {
	Total       uint64  `json:"total"`         // 总内存 (bytes)
	Used        uint64  `json:"used"`          // 已用内存 (bytes)
	Available   uint64  `json:"available"`     // 可用内存 (bytes)
	UsedPercent float64 `json:"used_percent"`  // 使用率百分比
}

// DiskMetrics 磁盘监控数据
type DiskMetrics struct {
	Total       uint64  `json:"total"`         // 总容量 (bytes)
	Used        uint64  `json:"used"`          // 已用容量 (bytes)
	Free        uint64  `json:"free"`          // 可用容量 (bytes)
	UsedPercent float64 `json:"used_percent"`  // 使用率百分比
}

// NetworkMetrics 网络监控数据
type NetworkMetrics struct {
	BytesSent uint64 `json:"bytes_sent"` // 累计发送字节数
	BytesRecv uint64 `json:"bytes_recv"` // 累计接收字节数
	SpeedSent uint64 `json:"speed_sent"` // 发送速度 (bytes/sec)
	SpeedRecv uint64 `json:"speed_recv"` // 接收速度 (bytes/sec)
}

// MetricsCollector 监控数据收集器
type MetricsCollector struct {
	lastNetStats map[string]net.IOCountersStat
	lastNetTime  time.Time
}

// NewMetricsCollector 创建新的监控数据收集器
func NewMetricsCollector() *MetricsCollector {
	// 初始调用 cpu.Percent(0, false) 来初始化 CPU 采样基准点
	// 后续非阻塞调用将基于此基准计算 CPU 使用率
	cpu.Percent(0, false)

	return &MetricsCollector{
		lastNetStats: make(map[string]net.IOCountersStat),
		lastNetTime:  time.Time{},
	}
}

// CollectSystemMetrics 收集所有系统监控数据
// 使用 defer+recover 确保单个指标收集失败不影响其他指标
func (m *MetricsCollector) CollectSystemMetrics() (*SystemMetrics, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Critical: Metrics collection panic: %v", r)
		}
	}()

	metrics := &SystemMetrics{
		CollectedAt: time.Now().Format(time.RFC3339),
	}

	var collectedCount int

	// 收集 CPU 数据
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: CPU collection panic: %v", r)
			}
		}()
		if cpuMetrics, err := m.collectCPU(); err == nil {
			metrics.CPU = cpuMetrics
			collectedCount++
		} else {
			log.Printf("Warning: Failed to collect CPU metrics: %v", err)
		}
	}()

	// 收集内存数据
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: Memory collection panic: %v", r)
			}
		}()
		if memMetrics, err := m.collectMemory(); err == nil {
			metrics.Memory = memMetrics
			collectedCount++
		} else {
			log.Printf("Warning: Failed to collect memory metrics: %v", err)
		}
	}()

	// 收集磁盘数据
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: Disk collection panic: %v", r)
			}
		}()
		if diskMetrics, err := m.collectDisk(); err == nil {
			metrics.Disk = diskMetrics
			collectedCount++
		} else {
			log.Printf("Warning: Failed to collect disk metrics: %v", err)
		}
	}()

	// 收集网络数据
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: Network collection panic: %v", r)
			}
		}()
		if netMetrics, err := m.collectNetwork(); err == nil {
			metrics.Network = netMetrics
			collectedCount++
		} else {
			log.Printf("Warning: Failed to collect network metrics: %v", err)
		}
	}()

	// 如果所有指标都收集失败，返回错误
	if collectedCount == 0 {
		return nil, fmt.Errorf("failed to collect any system metrics")
	}

	log.Printf("Successfully collected %d/4 system metrics", collectedCount)
	return metrics, nil
}

// collectCPU 收集 CPU 监控数据
func (m *MetricsCollector) collectCPU() (CPUMetrics, error) {
	// 非阻塞获取 CPU 使用率（基于上次调用到现在的时间段计算）
	// 避免阻塞 1 秒导致 RouterOS 等嵌入式系统 CPU 占用过高
	percent, err := cpu.Percent(0, false)
	if err != nil {
		return CPUMetrics{}, err
	}

	// 获取 CPU 核心数
	counts, err := cpu.Counts(true)
	if err != nil {
		counts = 0
	}

	cpuPercent := 0.0
	if len(percent) > 0 {
		cpuPercent = percent[0]
	}

	return CPUMetrics{
		UsagePercent: cpuPercent,
		CoreCount:    counts,
	}, nil
}

// collectMemory 收集内存监控数据
func (m *MetricsCollector) collectMemory() (MemoryMetrics, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return MemoryMetrics{}, err
	}

	return MemoryMetrics{
		Total:       v.Total,
		Used:        v.Used,
		Available:   v.Available,
		UsedPercent: v.UsedPercent,
	}, nil
}

// collectDisk 收集磁盘监控数据
func (m *MetricsCollector) collectDisk() (DiskMetrics, error) {
	// 获取根分区的磁盘使用情况
	usage, err := disk.Usage("/")
	if err != nil {
		return DiskMetrics{}, err
	}

	return DiskMetrics{
		Total:       usage.Total,
		Used:        usage.Used,
		Free:        usage.Free,
		UsedPercent: usage.UsedPercent,
	}, nil
}

// collectNetwork 收集网络监控数据
func (m *MetricsCollector) collectNetwork() (NetworkMetrics, error) {
	// 获取网络统计信息 (所有接口汇总)
	counters, err := net.IOCounters(false)
	if err != nil {
		return NetworkMetrics{}, err
	}

	if len(counters) == 0 {
		return NetworkMetrics{}, nil
	}

	current := counters[0]
	now := time.Now()

	// 计算网络速度 (如果有上一次的数据)
	var speedSent, speedRecv uint64
	if !m.lastNetTime.IsZero() {
		elapsed := now.Sub(m.lastNetTime).Seconds()
		if elapsed > 0 {
			if last, exists := m.lastNetStats["all"]; exists {
				// 计算速度 = (当前值 - 上次值) / 时间差
				bytesSentDiff := current.BytesSent - last.BytesSent
				bytesRecvDiff := current.BytesRecv - last.BytesRecv

				speedSent = uint64(float64(bytesSentDiff) / elapsed)
				speedRecv = uint64(float64(bytesRecvDiff) / elapsed)
			}
		}
	}

	// 保存当前数据供下次计算使用
	m.lastNetStats["all"] = current
	m.lastNetTime = now

	return NetworkMetrics{
		BytesSent: current.BytesSent,
		BytesRecv: current.BytesRecv,
		SpeedSent: speedSent,
		SpeedRecv: speedRecv,
	}, nil
}
