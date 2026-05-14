package tui

import (
	"io"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Time    time.Time
	Level   string
	Tunnel  string
	Message string
	Fields  map[string]string
}

const logRingSize = 1000

type LogRing struct {
	mu      sync.RWMutex
	entries []LogEntry
	head    int
	count   int
}

func NewLogRing() *LogRing {
	return &LogRing{
		entries: make([]LogEntry, logRingSize),
	}
}

func (r *LogRing) Push(entry LogEntry) {
	r.mu.Lock()
	r.entries[r.head] = entry
	r.head = (r.head + 1) % logRingSize
	if r.count < logRingSize {
		r.count++
	}
	r.mu.Unlock()
}

func (r *LogRing) All() []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]LogEntry, 0, r.count)
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + logRingSize) % logRingSize
		result = append(result, r.entries[idx])
	}
	return result
}

func (r *LogRing) Filter(tunnel, level string) []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]LogEntry, 0)
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + logRingSize) % logRingSize
		e := r.entries[idx]

		if tunnel != "" && tunnel != "all" && e.Tunnel != tunnel {
			continue
		}
		if level != "" && level != "all" && !levelMatch(e.Level, level) {
			continue
		}
		result = append(result, e)
	}
	return result
}

func levelMatch(entryLevel, filterLevel string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}
	el, ok1 := levels[strings.ToLower(entryLevel)]
	fl, ok2 := levels[strings.ToLower(filterLevel)]
	if !ok1 || !ok2 {
		return true
	}
	return el >= fl
}

type LogWriter struct {
	ch    chan LogEntry
	rings map[string]*LogRing
	mu    sync.RWMutex
	done  chan struct{}
}

func NewLogWriter(ch chan LogEntry) *LogWriter {
	w := &LogWriter{
		ch:    ch,
		rings: make(map[string]*LogRing),
		done:  make(chan struct{}),
	}

	w.rings["all"] = NewLogRing()

	go w.processEntries()
	return w
}

func (w *LogWriter) processEntries() {
	for {
		select {
		case entry, ok := <-w.ch:
			if !ok {
				return
			}
			w.mu.Lock()
			w.rings["all"].Push(entry)
			if entry.Tunnel != "" {
				if _, exists := w.rings[entry.Tunnel]; !exists {
					w.rings[entry.Tunnel] = NewLogRing()
				}
				w.rings[entry.Tunnel].Push(entry)
			}
			w.mu.Unlock()
		case <-w.done:
			return
		}
	}
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}

	entry := parseConsoleLine(line)

	w.mu.RLock()
	ch := w.ch
	w.mu.RUnlock()

	select {
	case ch <- entry:
	default:
	}
	return len(p), nil
}

func (w *LogWriter) Ring(tunnel string) *LogRing {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if r, ok := w.rings[tunnel]; ok {
		return r
	}
	return w.rings["all"]
}

func (w *LogWriter) Rings() map[string]*LogRing {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make(map[string]*LogRing, len(w.rings))
	for k, v := range w.rings {
		result[k] = v
	}
	return result
}

func (w *LogWriter) Close() {
	close(w.done)
}

func parseConsoleLine(line string) LogEntry {
	entry := LogEntry{
		Time:  time.Now(),
		Level: "info",
		Fields: make(map[string]string),
	}

	parts := strings.SplitN(line, " ", 5)

	idx := 0
	if idx < len(parts) {
		if t, err := time.Parse("15:04:05", parts[idx]); err == nil {
			entry.Time = t
			idx++
		}
	}

	if idx < len(parts) {
		lvl := strings.ToUpper(parts[idx])
		switch lvl {
		case "DBG", "DEBUG":
			entry.Level = "debug"
			idx++
		case "INF", "INFO":
			entry.Level = "info"
			idx++
		case "WRN", "WARN", "WARNING":
			entry.Level = "warn"
			idx++
		case "ERR", "ERROR":
			entry.Level = "error"
			idx++
		}
	}

	remaining := ""
	if idx < len(parts) {
		remaining = strings.Join(parts[idx:], " ")
	}

	tunnelID := extractField(remaining, "tunnel_id")
	if tunnelID != "" {
		entry.Tunnel = tunnelID
	}

	label := extractField(remaining, "label")
	if label != "" && entry.Tunnel == "" {
		entry.Tunnel = label
	}

	msgStart := strings.Index(remaining, " │ ")
	if msgStart >= 0 && msgStart < len(remaining)-3 {
		entry.Message = remaining[msgStart+3:]
	} else if msgStart < 0 {
		if pipeIdx := strings.Index(remaining, "|"); pipeIdx >= 0 && pipeIdx < len(remaining)-2 {
			entry.Message = remaining[pipeIdx+2:]
		} else {
			entry.Message = remaining
		}
	} else {
		entry.Message = remaining
	}

	entry.Message = strings.TrimSpace(entry.Message)
	return entry
}

func extractField(s, key string) string {
	pattern := key + "="
	idx := strings.Index(s, pattern)
	if idx < 0 {
		return ""
	}
	start := idx + len(pattern)
	if start >= len(s) {
		return ""
	}

	if s[start] == '"' {
		end := strings.IndexByte(s[start+1:], '"')
		if end < 0 {
			return s[start+1:]
		}
		return s[start+1 : start+1+end]
	}

	end := start
	for end < len(s) && s[end] != ' ' && s[end] != '|' {
		end++
	}
	return s[start:end]
}

var _ io.Writer = (*LogWriter)(nil)
