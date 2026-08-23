// Package platform contém utilitários de infra (logger, metrics).
package platform

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics é um registrador Prometheus minimalista (só counters, gauges e
// histogramas simples). Evitamos importar client_golang para não inflar o
// binário; a saída é text/plain conforme a exposition spec 0.0.4.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*counter
	gauges   map[string]*gauge
	hists    map[string]*histogram
}

// NewMetrics cria um registrador vazio.
func NewMetrics() *Metrics {
	return &Metrics{
		counters: map[string]*counter{},
		gauges:   map[string]*gauge{},
		hists:    map[string]*histogram{},
	}
}

// Counter incrementa (thread-safe) o counter com labels dados.
func (m *Metrics) Counter(name, help string, labels map[string]string, delta float64) {
	key := seriesKey(name, labels)
	m.mu.RLock()
	c, ok := m.counters[key]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if c, ok = m.counters[key]; !ok {
			c = &counter{name: name, help: help, labels: cloneLabels(labels)}
			m.counters[key] = c
		}
		m.mu.Unlock()
	}
	atomic.AddUint64(&c.value, uint64(delta*1000))
}

// Gauge define o valor atual (thread-safe).
func (m *Metrics) Gauge(name, help string, labels map[string]string, value float64) {
	key := seriesKey(name, labels)
	m.mu.Lock()
	g, ok := m.gauges[key]
	if !ok {
		g = &gauge{name: name, help: help, labels: cloneLabels(labels)}
		m.gauges[key] = g
	}
	g.value = value
	m.mu.Unlock()
}

// Observe registra uma amostra em um histograma (buckets fixos em segundos).
func (m *Metrics) Observe(name, help string, labels map[string]string, value float64) {
	key := seriesKey(name, labels)
	m.mu.RLock()
	h, ok := m.hists[key]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if h, ok = m.hists[key]; !ok {
			h = &histogram{
				name:    name,
				help:    help,
				labels:  cloneLabels(labels),
				buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
				counts:  make([]uint64, 11),
			}
			m.hists[key] = h
		}
		m.mu.Unlock()
	}
	h.observe(value)
}

// Render emite todas as séries em text/plain conforme exposition format 0.0.4.
func (m *Metrics) Render(w io.Writer) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Ordena por nome para saída estável (mais fácil de testar/verificar).
	names := map[string]string{}
	for _, c := range m.counters {
		names[c.name] = c.help
	}
	for _, g := range m.gauges {
		names[g.name] = g.help
	}
	for _, h := range m.hists {
		names[h.name] = h.help
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	written := map[string]bool{}
	for _, n := range sorted {
		if err := m.writeFamily(w, n, names[n], written); err != nil {
			return err
		}
	}
	return nil
}

func (m *Metrics) writeFamily(w io.Writer, name, help string, written map[string]bool) error {
	if written[name] {
		return nil
	}
	written[name] = true

	// Detecta o tipo do family: counter, gauge, histogram
	kind := ""
	for _, c := range m.counters {
		if c.name == name {
			kind = "counter"
		}
	}
	for _, g := range m.gauges {
		if g.name == name {
			kind = "gauge"
		}
	}
	for _, h := range m.hists {
		if h.name == name {
			kind = "histogram"
		}
	}
	if kind == "" {
		return nil
	}

	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind); err != nil {
		return err
	}
	switch kind {
	case "counter":
		for _, c := range m.counters {
			if c.name != name {
				continue
			}
			v := float64(atomic.LoadUint64(&c.value)) / 1000
			if _, err := fmt.Fprintf(w, "%s%s %g\n", c.name, renderLabels(c.labels), v); err != nil {
				return err
			}
		}
	case "gauge":
		for _, g := range m.gauges {
			if g.name != name {
				continue
			}
			if _, err := fmt.Fprintf(w, "%s%s %g\n", g.name, renderLabels(g.labels), g.value); err != nil {
				return err
			}
		}
	case "histogram":
		for _, h := range m.hists {
			if h.name != name {
				continue
			}
			if err := h.writeTo(w); err != nil {
				return err
			}
		}
	}
	return nil
}

type counter struct {
	name   string
	help   string
	labels map[string]string
	value  uint64 // fixed-point *1000 para evitar float atômico
}

type gauge struct {
	name   string
	help   string
	labels map[string]string
	value  float64
}

type histogram struct {
	name    string
	help    string
	labels  map[string]string
	buckets []float64
	counts  []uint64
	sum     uint64 // *1000
	count   uint64
}

func (h *histogram) observe(v float64) {
	atomic.AddUint64(&h.count, 1)
	atomic.AddUint64(&h.sum, uint64(v*1000))
	for i, b := range h.buckets {
		if v <= b {
			atomic.AddUint64(&h.counts[i], 1)
		}
	}
}

func (h *histogram) writeTo(w io.Writer) error {
	base := h.name
	labels := renderLabels(h.labels)
	trimmed := labels
	if len(labels) >= 2 {
		trimmed = labels[:len(labels)-1] + ","
	} else {
		trimmed = "{"
	}
	var cumulative uint64
	for i, b := range h.buckets {
		cumulative = atomic.LoadUint64(&h.counts[i])
		if _, err := fmt.Fprintf(w, `%s_bucket%sle="%g"} %d`+"\n", base, trimmed, b, cumulative); err != nil {
			return err
		}
	}
	total := atomic.LoadUint64(&h.count)
	if _, err := fmt.Fprintf(w, `%s_bucket%sle="+Inf"} %d`+"\n", base, trimmed, total); err != nil {
		return err
	}
	sum := float64(atomic.LoadUint64(&h.sum)) / 1000
	if _, err := fmt.Fprintf(w, "%s_sum%s %g\n", base, labels, sum); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_count%s %d\n", base, labels, total); err != nil {
		return err
	}
	return nil
}

func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := name
	for _, k := range keys {
		s += "|" + k + "=" + labels[k]
	}
	return s
}

func cloneLabels(l map[string]string) map[string]string {
	if l == nil {
		return nil
	}
	out := make(map[string]string, len(l))
	for k, v := range l {
		out[k] = v
	}
	return out
}

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := "{"
	for i, k := range keys {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf(`%s="%s"`, k, escapeLabelValue(labels[k]))
	}
	s += "}"
	return s
}

func escapeLabelValue(v string) string {
	// Prometheus label values escapam \, " e \n
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, v[i])
		}
	}
	return string(out)
}

// ObserveSince é açúcar para medir latência.
func (m *Metrics) ObserveSince(name, help string, labels map[string]string, start time.Time) {
	m.Observe(name, help, labels, time.Since(start).Seconds())
}
