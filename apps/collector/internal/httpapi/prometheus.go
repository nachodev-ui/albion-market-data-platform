package httpapi

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-market-data/collector/internal/observability"
)

func writeCounterMap(output *strings.Builder, name, help, labelName string, values map[string]observability.CounterSnapshot) {
	writeMetricHeader(output, name, help, "counter")
	for _, key := range sortedCounterKeys(values) {
		writeMetric(output, name, map[string]string{labelName: key}, strconv.FormatUint(values[key].Total, 10))
	}
}

func writeUint64Map(output *strings.Builder, name, help, labelName string, values map[string]uint64) {
	writeMetricHeader(output, name, help, "counter")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeMetric(output, name, map[string]string{labelName: key}, strconv.FormatUint(values[key], 10))
	}
}

func writeLastTimestampMap(output *strings.Builder, name, help, labelName string, values map[string]observability.CounterSnapshot) {
	writeMetricHeader(output, name, help, "gauge")
	for _, key := range sortedCounterKeys(values) {
		writeTimestampMetric(output, name, map[string]string{labelName: key}, values[key].LastAt)
	}
}

func sortedCounterKeys(values map[string]observability.CounterSnapshot) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStorageKeys(values map[string]observability.StorageSnapshot) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeMetricHeader(output *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(output, "# HELP %s %s\n", name, help)
	fmt.Fprintf(output, "# TYPE %s %s\n", name, metricType)
}

func writeMetricHeaderOnce(output *strings.Builder, name, help, metricType, pipeline string) {
	if pipeline == "prices" {
		writeMetricHeader(output, name, help, metricType)
	}
}

func writeMetric(output *strings.Builder, name string, labels map[string]string, value string) {
	output.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			output.WriteString(key)
			output.WriteString("=\"")
			output.WriteString(escapePrometheusLabel(labels[key]))
			output.WriteByte('"')
		}
		output.WriteByte('}')
	}
	output.WriteByte(' ')
	output.WriteString(value)
	output.WriteByte('\n')
}

func writeTimestampMetric(output *strings.Builder, name string, labels map[string]string, value *time.Time) {
	seconds := int64(0)
	if value != nil && !value.IsZero() {
		seconds = value.UTC().Unix()
	}
	writeMetric(output, name, labels, strconv.FormatInt(seconds, 10))
}

func escapePrometheusLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func millisecondsToSeconds(value float64) string {
	if value < 0 {
		value = 0
	}
	return strconv.FormatFloat(value/1000, 'f', 6, 64)
}

func boolMetric(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func defaultMetricLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
