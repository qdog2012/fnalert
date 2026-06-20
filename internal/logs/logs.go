package logs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fnmonitor/internal/config"
)

type Entry struct {
	Time    string `json:"time,omitempty"`
	Source  string `json:"source"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Matched bool   `json:"matched"`
}

type Result struct {
	Entries  []Entry  `json:"entries"`
	Warnings []string `json:"warnings,omitempty"`
}

type Reader struct {
	cfg            config.LogsConfig
	patterns       []*regexp.Regexp
	notifyMinLevel string
}

func New(cfg config.LogsConfig) (*Reader, error) {
	r := &Reader{cfg: cfg, notifyMinLevel: normalizeNotifyMinLevel(cfg.NotifyMinLevel)}
	for _, p := range cfg.ErrorPatterns {
		compiled, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile log error pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, compiled)
	}
	return r, nil
}

func (r *Reader) Recent(ctx context.Context) Result {
	maxLines := r.cfg.MaxLines
	if maxLines <= 0 {
		maxLines = 300
	}
	result := Result{Entries: []Entry{}}

	for _, file := range r.cfg.Files {
		if !file.Enabled || file.Path == "" {
			continue
		}
		lines, err := tailFile(file.Path, maxLines)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", file.Path, err))
			}
			continue
		}
		source := file.Name
		if source == "" {
			source = file.Path
		}
		for _, line := range lines {
			result.Entries = append(result.Entries, r.entry(source, line))
		}
	}

	if r.cfg.IncludeJournal {
		entries, err := r.journalEntries(ctx, maxLines)
		if err != nil {
			result.Warnings = append(result.Warnings, "journalctl: "+err.Error())
		} else {
			result.Entries = append(result.Entries, entries...)
		}
	}

	if r.cfg.IncludeDmesg {
		lines, err := commandLines(ctx, 5*time.Second, "dmesg", "--ctime", "--color=never")
		if err != nil {
			result.Warnings = append(result.Warnings, "dmesg: "+err.Error())
		} else {
			lines = lastN(lines, maxLines)
			for _, line := range lines {
				result.Entries = append(result.Entries, r.entry("dmesg", line))
			}
		}
	}

	if len(result.Entries) > maxLines*max(1, len(r.cfg.Files)+2) {
		result.Entries = result.Entries[len(result.Entries)-maxLines*max(1, len(r.cfg.Files)+2):]
	}
	return result
}

func (r *Reader) MatchCount(entries []Entry) int {
	count := 0
	for _, e := range entries {
		if e.Matched {
			count++
		}
	}
	return count
}

func (r *Reader) entry(source, raw string) Entry {
	raw = strings.TrimSpace(raw)
	level := detectLevel(raw)
	ts, msg := splitTimestamp(raw)
	if msg == "" {
		msg = raw
	}
	return r.entryWithLevel(source, ts, level, msg)
}

func (r *Reader) entryWithLevel(source, ts, level, msg string) Entry {
	msg = strings.TrimSpace(msg)
	matched := r.shouldNotifyLevel(level)
	if !matched && r.shouldNotifyLevel("error") {
		for _, p := range r.patterns {
			if p.MatchString(msg) {
				matched = true
				break
			}
		}
	}
	return Entry{
		Time:    ts,
		Source:  source,
		Level:   level,
		Message: msg,
		Matched: matched,
	}
}

func (r *Reader) journalEntries(ctx context.Context, maxLines int) ([]Entry, error) {
	lines, err := commandLines(ctx, 5*time.Second, "journalctl", "-n", fmt.Sprint(maxLines), "--no-pager", "-o", "json")
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(lines))
	for _, line := range lines {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			entries = append(entries, r.entry("journal", line))
			continue
		}
		msg := journalField(raw, "MESSAGE")
		if strings.TrimSpace(msg) == "" {
			msg = line
		}
		source := journalField(raw, "SYSLOG_IDENTIFIER")
		if source == "" {
			source = journalField(raw, "_SYSTEMD_UNIT")
		}
		if source == "" {
			source = "journal"
		}
		ts := journalTime(journalField(raw, "__REALTIME_TIMESTAMP"))
		level := journalPriorityLevel(journalField(raw, "PRIORITY"))
		if level == "" {
			level = detectLevel(msg)
		}
		entries = append(entries, r.entryWithLevel(source, ts, level, msg))
	}
	return entries, nil
}

func tailFile(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lines := make([]string, 0, maxLines)
	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) < maxLines {
			lines = append(lines, line)
			continue
		}
		copy(lines, lines[1:])
		lines[len(lines)-1] = line
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func commandLines(ctx context.Context, timeout time.Duration, name string, args ...string) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	lines := strings.Split(stdout.String(), "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func lastN(lines []string, n int) []string {
	if n <= 0 || len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

func detectLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "panic") || strings.Contains(lower, "critical") || strings.Contains(lower, "fatal") || strings.Contains(lower, "emerg") || strings.Contains(lower, "alert") || strings.Contains(lower, "严重") || strings.Contains(lower, "紧急"):
		return "critical"
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "failure") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "错误") || strings.Contains(lower, "失败"):
		return "error"
	case strings.Contains(lower, "warning") || strings.Contains(lower, "warn") || strings.Contains(lower, "degrad") || strings.Contains(lower, "retry") || strings.Contains(lower, "警告") || strings.Contains(lower, "告警"):
		return "warning"
	default:
		return "info"
	}
}

func normalizeNotifyMinLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "", "warn":
		return "warning"
	case "info", "warning", "error", "critical", "none":
		return level
	default:
		return "warning"
	}
}

func (r *Reader) shouldNotifyLevel(level string) bool {
	minRank := levelRank(r.notifyMinLevel)
	if minRank < 0 {
		return false
	}
	return levelRank(level) >= minRank
}

func levelRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "info":
		return 0
	case "warning", "warn":
		return 1
	case "error":
		return 2
	case "critical":
		return 3
	case "none":
		return -1
	default:
		return 0
	}
}

func journalPriorityLevel(priority string) string {
	n, err := strconv.Atoi(strings.TrimSpace(priority))
	if err != nil {
		return ""
	}
	switch {
	case n <= 2:
		return "critical"
	case n == 3:
		return "error"
	case n == 4:
		return "warning"
	default:
		return "info"
	}
}

func journalField(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(v)
	}
}

func journalTime(raw string) string {
	micros, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || micros <= 0 {
		return ""
	}
	return time.UnixMicro(micros).Format(time.RFC3339)
}

func splitTimestamp(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	if len(line) >= 25 && line[4] == '-' && line[7] == '-' {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	months := map[string]bool{
		"Jan": true, "Feb": true, "Mar": true, "Apr": true, "May": true, "Jun": true,
		"Jul": true, "Aug": true, "Sep": true, "Oct": true, "Nov": true, "Dec": true,
	}
	fields := strings.Fields(line)
	if len(fields) >= 4 && months[fields[0]] {
		return strings.Join(fields[:3], " "), strings.Join(fields[3:], " ")
	}
	if strings.HasPrefix(line, "[") {
		end := strings.Index(line, "]")
		if end > 0 {
			return line[:end+1], strings.TrimSpace(line[end+1:])
		}
	}
	return "", line
}

func SortedMatched(entries []Entry) []Entry {
	matched := make([]Entry, 0)
	for _, e := range entries {
		if e.Matched {
			matched = append(matched, e)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Source < matched[j].Source
	})
	return matched
}
