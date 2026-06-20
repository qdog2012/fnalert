package alert

import (
	"fmt"
	"strings"
	"time"

	"fnmonitor/internal/config"
	"fnmonitor/internal/logs"
	"fnmonitor/internal/metrics"
)

type Event struct {
	Fingerprint string            `json:"fingerprint"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Message     string            `json:"message"`
	Value       float64           `json:"value,omitempty"`
	Threshold   float64           `json:"threshold,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	StartedAt   time.Time         `json:"started_at"`
}

func Evaluate(th config.ThresholdConfig, s metrics.Snapshot, logResult logs.Result) []Event {
	now := s.CollectedAt
	if now.IsZero() {
		now = time.Now()
	}
	var events []Event

	addPercent := func(name, title string, value, threshold float64) {
		if threshold <= 0 || value <= threshold {
			return
		}
		events = append(events, Event{
			Fingerprint: "metric:" + name,
			Severity:    severity(value, threshold),
			Title:       title,
			Message:     fmt.Sprintf("%s %.2f%% 超过阈值 %.2f%%", title, value, threshold),
			Value:       value,
			Threshold:   threshold,
			StartedAt:   now,
		})
	}

	addPercent("cpu", "CPU 使用率异常", s.CPU.Percent, th.CPUPercent)
	if th.Load1PerCPU > 0 && s.Load.Load1PerCPU > th.Load1PerCPU {
		events = append(events, Event{
			Fingerprint: "metric:load1_per_cpu",
			Severity:    severity(s.Load.Load1PerCPU, th.Load1PerCPU),
			Title:       "系统负载异常",
			Message:     fmt.Sprintf("1 分钟负载/CPU %.2f 超过阈值 %.2f，当前 Load1 %.2f", s.Load.Load1PerCPU, th.Load1PerCPU, s.Load.Load1),
			Value:       s.Load.Load1PerCPU,
			Threshold:   th.Load1PerCPU,
			StartedAt:   now,
		})
	}
	addPercent("memory", "内存使用率异常", s.Memory.UsedPercent, th.MemoryPercent)
	if s.Swap.Total > 0 {
		addPercent("swap", "Swap 使用率异常", s.Swap.UsedPercent, th.SwapPercent)
	}

	for _, fs := range s.Filesystems {
		if th.DiskPercent > 0 && fs.UsedPercent > th.DiskPercent {
			events = append(events, Event{
				Fingerprint: "metric:disk:" + fs.Mountpoint,
				Severity:    severity(fs.UsedPercent, th.DiskPercent),
				Title:       "磁盘空间异常",
				Message:     fmt.Sprintf("%s 使用率 %.2f%% 超过阈值 %.2f%%", fs.Mountpoint, fs.UsedPercent, th.DiskPercent),
				Value:       fs.UsedPercent,
				Threshold:   th.DiskPercent,
				Labels: map[string]string{
					"mountpoint": fs.Mountpoint,
					"device":     fs.Device,
					"fstype":     fs.FSType,
				},
				StartedAt: now,
			})
		}
		if th.DiskInodePercent > 0 && fs.InodesUsedPercent > th.DiskInodePercent {
			events = append(events, Event{
				Fingerprint: "metric:inode:" + fs.Mountpoint,
				Severity:    severity(fs.InodesUsedPercent, th.DiskInodePercent),
				Title:       "磁盘 inode 异常",
				Message:     fmt.Sprintf("%s inode 使用率 %.2f%% 超过阈值 %.2f%%", fs.Mountpoint, fs.InodesUsedPercent, th.DiskInodePercent),
				Value:       fs.InodesUsedPercent,
				Threshold:   th.DiskInodePercent,
				Labels: map[string]string{
					"mountpoint": fs.Mountpoint,
					"device":     fs.Device,
				},
				StartedAt: now,
			})
		}
	}

	if th.TemperatureC > 0 {
		for _, temp := range s.Temperatures {
			if temp.Temperature <= th.TemperatureC {
				continue
			}
			events = append(events, Event{
				Fingerprint: "metric:temperature:" + temp.SensorKey,
				Severity:    severity(temp.Temperature, th.TemperatureC),
				Title:       "温度异常",
				Message:     fmt.Sprintf("%s %.2f°C 超过阈值 %.2f°C", temp.SensorKey, temp.Temperature, th.TemperatureC),
				Value:       temp.Temperature,
				Threshold:   th.TemperatureC,
				Labels:      map[string]string{"sensor": temp.SensorKey},
				StartedAt:   now,
			})
		}
	}

	if th.ProcessCount > 0 && s.Processes.Count > th.ProcessCount {
		events = append(events, Event{
			Fingerprint: "metric:process_count",
			Severity:    "warning",
			Title:       "进程数量异常",
			Message:     fmt.Sprintf("进程数量 %d 超过阈值 %d", s.Processes.Count, th.ProcessCount),
			Value:       float64(s.Processes.Count),
			Threshold:   float64(th.ProcessCount),
			StartedAt:   now,
		})
	}

	if th.NetworkRxMbps > 0 && s.Network.RxMbps > th.NetworkRxMbps {
		events = append(events, Event{
			Fingerprint: "metric:network_rx",
			Severity:    severity(s.Network.RxMbps, th.NetworkRxMbps),
			Title:       "入站网络流量异常",
			Message:     fmt.Sprintf("入站速率 %.2f Mbps 超过阈值 %.2f Mbps", s.Network.RxMbps, th.NetworkRxMbps),
			Value:       s.Network.RxMbps,
			Threshold:   th.NetworkRxMbps,
			StartedAt:   now,
		})
	}
	if th.NetworkTxMbps > 0 && s.Network.TxMbps > th.NetworkTxMbps {
		events = append(events, Event{
			Fingerprint: "metric:network_tx",
			Severity:    severity(s.Network.TxMbps, th.NetworkTxMbps),
			Title:       "出站网络流量异常",
			Message:     fmt.Sprintf("出站速率 %.2f Mbps 超过阈值 %.2f Mbps", s.Network.TxMbps, th.NetworkTxMbps),
			Value:       s.Network.TxMbps,
			Threshold:   th.NetworkTxMbps,
			StartedAt:   now,
		})
	}

	if th.LogMatchCount > 0 {
		matches := logs.SortedMatched(logResult.Entries)
		if len(matches) >= th.LogMatchCount {
			lines := make([]string, 0, min(5, len(matches)))
			for _, entry := range matches {
				if len(lines) >= 5 {
					break
				}
				lines = append(lines, fmt.Sprintf("[%s/%s] %s", entry.Source, entry.Level, entry.Message))
			}
			events = append(events, Event{
				Fingerprint: "logs:matched",
				Severity:    logSeverity(matches),
				Title:       "系统日志告警",
				Message:     fmt.Sprintf("最近日志匹配告警条件 %d 条，示例：%s", len(matches), strings.Join(lines, " | ")),
				Value:       float64(len(matches)),
				Threshold:   float64(th.LogMatchCount),
				StartedAt:   now,
			})
		}
	}

	return events
}

func severity(value, threshold float64) string {
	if threshold <= 0 {
		return "warning"
	}
	if value >= threshold*1.2 {
		return "critical"
	}
	return "warning"
}

func logSeverity(entries []logs.Entry) string {
	for _, entry := range entries {
		if entry.Level == "critical" || entry.Level == "error" {
			return "critical"
		}
	}
	return "warning"
}
