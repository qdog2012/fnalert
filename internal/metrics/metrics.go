package metrics

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"fnmonitor/internal/config"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type Snapshot struct {
	CollectedAt  time.Time      `json:"collected_at"`
	Host         HostInfo       `json:"host"`
	CPU          CPUInfo        `json:"cpu"`
	Load         LoadInfo       `json:"load"`
	Memory       MemoryInfo     `json:"memory"`
	Swap         MemoryInfo     `json:"swap"`
	Filesystems  []Filesystem   `json:"filesystems"`
	DiskIO       []DiskIO       `json:"disk_io"`
	Network      NetworkInfo    `json:"network"`
	Temperatures []Temperature  `json:"temperatures"`
	Processes    ProcessSummary `json:"processes"`
	Warnings     []string       `json:"warnings,omitempty"`
}

type HostInfo struct {
	Hostname        string `json:"hostname"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	KernelVersion   string `json:"kernel_version"`
	OS              string `json:"os"`
	UptimeSeconds   uint64 `json:"uptime_seconds"`
	BootTime        uint64 `json:"boot_time"`
}

type CPUInfo struct {
	PhysicalCores int       `json:"physical_cores"`
	LogicalCores  int       `json:"logical_cores"`
	ModelName     string    `json:"model_name"`
	Percent       float64   `json:"percent"`
	PerCPUPercent []float64 `json:"per_cpu_percent"`
}

type LoadInfo struct {
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	Load1PerCPU float64 `json:"load1_per_cpu"`
}

type MemoryInfo struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsedPercent float64 `json:"used_percent"`
}

type Filesystem struct {
	Device            string  `json:"device"`
	Mountpoint        string  `json:"mountpoint"`
	FSType            string  `json:"fstype"`
	Total             uint64  `json:"total"`
	Used              uint64  `json:"used"`
	Free              uint64  `json:"free"`
	UsedPercent       float64 `json:"used_percent"`
	InodesTotal       uint64  `json:"inodes_total"`
	InodesUsed        uint64  `json:"inodes_used"`
	InodesFree        uint64  `json:"inodes_free"`
	InodesUsedPercent float64 `json:"inodes_used_percent"`
}

type DiskIO struct {
	Name        string `json:"name"`
	ReadBytes   uint64 `json:"read_bytes"`
	WriteBytes  uint64 `json:"write_bytes"`
	ReadCount   uint64 `json:"read_count"`
	WriteCount  uint64 `json:"write_count"`
	ReadTimeMS  uint64 `json:"read_time_ms"`
	WriteTimeMS uint64 `json:"write_time_ms"`
}

type NetworkInfo struct {
	Interfaces     []NetInterface `json:"interfaces"`
	TotalRxBytes   uint64         `json:"total_rx_bytes"`
	TotalTxBytes   uint64         `json:"total_tx_bytes"`
	RxBytesPerSec  float64        `json:"rx_bytes_per_sec"`
	TxBytesPerSec  float64        `json:"tx_bytes_per_sec"`
	RxMbps         float64        `json:"rx_mbps"`
	TxMbps         float64        `json:"tx_mbps"`
	RateWindowSecs float64        `json:"rate_window_secs"`
}

type NetInterface struct {
	Name        string `json:"name"`
	BytesRecv   uint64 `json:"bytes_recv"`
	BytesSent   uint64 `json:"bytes_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	ErrIn       uint64 `json:"err_in"`
	ErrOut      uint64 `json:"err_out"`
	DropIn      uint64 `json:"drop_in"`
	DropOut     uint64 `json:"drop_out"`
}

type Temperature struct {
	SensorKey   string  `json:"sensor_key"`
	Temperature float64 `json:"temperature_c"`
	High        float64 `json:"high,omitempty"`
	Critical    float64 `json:"critical,omitempty"`
}

type ProcessSummary struct {
	Count     int           `json:"count"`
	TopMemory []ProcessInfo `json:"top_memory"`
}

type ProcessInfo struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	RSS        uint64  `json:"rss"`
	VMS        uint64  `json:"vms"`
	CPUPercent float64 `json:"cpu_percent,omitempty"`
}

type Collector struct {
	cfg       config.CollectConfig
	mu        sync.Mutex
	prevNetAt time.Time
	prevRx    uint64
	prevTx    uint64
}

func New(cfg config.CollectConfig) *Collector {
	return &Collector{cfg: cfg}
}

func (c *Collector) Snapshot(ctx context.Context) (Snapshot, error) {
	now := time.Now()
	s := Snapshot{CollectedAt: now}
	var warnings []string

	if hi, err := host.InfoWithContext(ctx); err != nil {
		warnings = append(warnings, "host: "+err.Error())
	} else {
		s.Host = HostInfo{
			Hostname:        hi.Hostname,
			Platform:        hi.Platform,
			PlatformVersion: hi.PlatformVersion,
			KernelVersion:   hi.KernelVersion,
			OS:              hi.OS,
			UptimeSeconds:   hi.Uptime,
			BootTime:        hi.BootTime,
		}
	}

	c.collectCPU(ctx, &s, &warnings)
	c.collectLoad(ctx, &s, &warnings)
	c.collectMemory(ctx, &s, &warnings)
	c.collectFilesystems(ctx, &s, &warnings)
	c.collectDiskIO(ctx, &s, &warnings)
	c.collectNetwork(ctx, &s, now, &warnings)
	c.collectTemperatures(ctx, &s, &warnings)
	c.collectProcesses(ctx, &s, &warnings)

	s.Warnings = warnings
	return s, nil
}

func (c *Collector) collectCPU(ctx context.Context, s *Snapshot, warnings *[]string) {
	logical, err := cpu.CountsWithContext(ctx, true)
	if err != nil {
		*warnings = append(*warnings, "cpu logical count: "+err.Error())
	}
	physical, err := cpu.CountsWithContext(ctx, false)
	if err != nil {
		*warnings = append(*warnings, "cpu physical count: "+err.Error())
	}
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "cpu info: "+err.Error())
	}
	percent, err := cpu.PercentWithContext(ctx, 300*time.Millisecond, false)
	if err != nil {
		*warnings = append(*warnings, "cpu percent: "+err.Error())
	}
	perCPU, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		*warnings = append(*warnings, "per-cpu percent: "+err.Error())
	}
	model := ""
	if len(infos) > 0 {
		model = infos[0].ModelName
	}
	used := 0.0
	if len(percent) > 0 {
		used = percent[0]
	}
	s.CPU = CPUInfo{
		PhysicalCores: physical,
		LogicalCores:  logical,
		ModelName:     model,
		Percent:       round2(used),
		PerCPUPercent: roundSlice2(perCPU),
	}
}

func (c *Collector) collectLoad(ctx context.Context, s *Snapshot, warnings *[]string) {
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "load: "+err.Error())
		return
	}
	perCPU := 0.0
	if s.CPU.LogicalCores > 0 {
		perCPU = avg.Load1 / float64(s.CPU.LogicalCores)
	}
	s.Load = LoadInfo{
		Load1:       round2(avg.Load1),
		Load5:       round2(avg.Load5),
		Load15:      round2(avg.Load15),
		Load1PerCPU: round2(perCPU),
	}
}

func (c *Collector) collectMemory(ctx context.Context, s *Snapshot, warnings *[]string) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "memory: "+err.Error())
	} else {
		s.Memory = MemoryInfo{
			Total:       vm.Total,
			Used:        vm.Used,
			Available:   vm.Available,
			UsedPercent: round2(vm.UsedPercent),
		}
	}
	sm, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "swap: "+err.Error())
	} else {
		available := sm.Free
		if sm.Total >= sm.Used {
			available = sm.Total - sm.Used
		}
		s.Swap = MemoryInfo{
			Total:       sm.Total,
			Used:        sm.Used,
			Available:   available,
			UsedPercent: round2(sm.UsedPercent),
		}
	}
}

func (c *Collector) collectFilesystems(ctx context.Context, s *Snapshot, warnings *[]string) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		*warnings = append(*warnings, "disk partitions: "+err.Error())
		return
	}
	ignoreFS := makeSet(c.cfg.IgnoreFilesystems)
	allowMounts := makeSet(c.cfg.DiskMounts)
	seen := map[string]bool{}
	for _, p := range parts {
		if p.Mountpoint == "" || seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		if len(allowMounts) > 0 && !allowMounts[p.Mountpoint] {
			continue
		}
		if ignoreFS[p.Fstype] {
			continue
		}
		if st, err := os.Stat(p.Mountpoint); err == nil && !st.IsDir() {
			continue
		}
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("disk usage %s: %v", p.Mountpoint, err))
			continue
		}
		s.Filesystems = append(s.Filesystems, Filesystem{
			Device:            p.Device,
			Mountpoint:        p.Mountpoint,
			FSType:            p.Fstype,
			Total:             usage.Total,
			Used:              usage.Used,
			Free:              usage.Free,
			UsedPercent:       round2(usage.UsedPercent),
			InodesTotal:       usage.InodesTotal,
			InodesUsed:        usage.InodesUsed,
			InodesFree:        usage.InodesFree,
			InodesUsedPercent: round2(usage.InodesUsedPercent),
		})
	}
	sort.Slice(s.Filesystems, func(i, j int) bool {
		return s.Filesystems[i].Mountpoint < s.Filesystems[j].Mountpoint
	})
}

func (c *Collector) collectDiskIO(ctx context.Context, s *Snapshot, warnings *[]string) {
	stats, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "disk io: "+err.Error())
		return
	}
	for name, io := range stats {
		s.DiskIO = append(s.DiskIO, DiskIO{
			Name:        name,
			ReadBytes:   io.ReadBytes,
			WriteBytes:  io.WriteBytes,
			ReadCount:   io.ReadCount,
			WriteCount:  io.WriteCount,
			ReadTimeMS:  io.ReadTime,
			WriteTimeMS: io.WriteTime,
		})
	}
	sort.Slice(s.DiskIO, func(i, j int) bool {
		return s.DiskIO[i].Name < s.DiskIO[j].Name
	})
}

func (c *Collector) collectNetwork(ctx context.Context, s *Snapshot, now time.Time, warnings *[]string) {
	stats, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		*warnings = append(*warnings, "network: "+err.Error())
		return
	}
	var rx uint64
	var tx uint64
	for _, st := range stats {
		if st.Name == "lo" {
			continue
		}
		rx += st.BytesRecv
		tx += st.BytesSent
		s.Network.Interfaces = append(s.Network.Interfaces, NetInterface{
			Name:        st.Name,
			BytesRecv:   st.BytesRecv,
			BytesSent:   st.BytesSent,
			PacketsRecv: st.PacketsRecv,
			PacketsSent: st.PacketsSent,
			ErrIn:       st.Errin,
			ErrOut:      st.Errout,
			DropIn:      st.Dropin,
			DropOut:     st.Dropout,
		})
	}
	sort.Slice(s.Network.Interfaces, func(i, j int) bool {
		return s.Network.Interfaces[i].Name < s.Network.Interfaces[j].Name
	})

	s.Network.TotalRxBytes = rx
	s.Network.TotalTxBytes = tx

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.prevNetAt.IsZero() {
		elapsed := now.Sub(c.prevNetAt).Seconds()
		if elapsed > 0 && rx >= c.prevRx && tx >= c.prevTx {
			s.Network.RateWindowSecs = round2(elapsed)
			s.Network.RxBytesPerSec = round2(float64(rx-c.prevRx) / elapsed)
			s.Network.TxBytesPerSec = round2(float64(tx-c.prevTx) / elapsed)
			s.Network.RxMbps = round2(s.Network.RxBytesPerSec * 8 / 1000 / 1000)
			s.Network.TxMbps = round2(s.Network.TxBytesPerSec * 8 / 1000 / 1000)
		}
	}
	c.prevNetAt = now
	c.prevRx = rx
	c.prevTx = tx
}

func (c *Collector) collectTemperatures(ctx context.Context, s *Snapshot, warnings *[]string) {
	stats, err := host.SensorsTemperaturesWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "temperature: "+err.Error())
		return
	}
	for _, t := range stats {
		s.Temperatures = append(s.Temperatures, Temperature{
			SensorKey:   t.SensorKey,
			Temperature: round2(t.Temperature),
			High:        round2(t.High),
			Critical:    round2(t.Critical),
		})
	}
	sort.Slice(s.Temperatures, func(i, j int) bool {
		return s.Temperatures[i].SensorKey < s.Temperatures[j].SensorKey
	})
}

func (c *Collector) collectProcesses(ctx context.Context, s *Snapshot, warnings *[]string) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "processes: "+err.Error())
		return
	}
	s.Processes.Count = len(procs)
	top := make([]ProcessInfo, 0, len(procs))
	for _, p := range procs {
		memInfo, err := p.MemoryInfoWithContext(ctx)
		if err != nil || memInfo == nil {
			continue
		}
		name, err := p.NameWithContext(ctx)
		if err != nil {
			name = ""
		}
		cpuPercent, _ := p.CPUPercentWithContext(ctx)
		top = append(top, ProcessInfo{
			PID:        p.Pid,
			Name:       name,
			RSS:        memInfo.RSS,
			VMS:        memInfo.VMS,
			CPUPercent: round2(cpuPercent),
		})
	}
	sort.Slice(top, func(i, j int) bool {
		return top[i].RSS > top[j].RSS
	})
	if len(top) > 10 {
		top = top[:10]
	}
	s.Processes.TopMemory = top
}

func makeSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	return float64(int(v*100+0.5)) / 100
}

func roundSlice2(values []float64) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = round2(v)
	}
	return out
}
