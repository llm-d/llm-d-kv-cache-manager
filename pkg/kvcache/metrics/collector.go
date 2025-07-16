package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	Admissions = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "admissions_total",
		Help: "Total number of KV-block admissions",
	})
	Evictions = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "evictions_total",
		Help: "Total number of KV-block evictions",
	})

	// LookupRequests counts how many Lookup() calls have been made.
	LookupRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "lookup_requests_total",
		Help: "Total number of lookup calls",
	})
	// LookupHits counts how many keys were found in the cache on Lookup().
	LookupHits = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "lookup_hits_total",
		Help: "Number of keys found in the cache on Lookup()",
	})
	// LookupLatency logs latency of lookup calls.
	LookupLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "lookup_latency_seconds",
		Help:    "Latency of Lookup calls in seconds",
		Buckets: prometheus.DefBuckets,
	})
)

// Collectors returns a slice of all registered Prometheus collectors.
func Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		Admissions, Evictions,
		LookupRequests, LookupHits, LookupLatency,
	}
}

// Register registers all metrics with K8s registry.
func Register() {
	metrics.Registry.MustRegister(Collectors()...)
}

// StartMetricsLogging spawns a goroutine that logs current metric values every
// interval.
func StartMetricsLogging(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			logMetrics(ctx)
		}
	}()
}

func logMetrics(ctx context.Context) {
	var m dto.Metric

	err := Admissions.Write(&m)
	if err != nil {
		return
	}
	admissions := m.GetCounter().GetValue()

	err = Evictions.Write(&m)
	if err != nil {
		return
	}
	evictions := m.GetCounter().GetValue()

	err = LookupRequests.Write(&m)
	if err != nil {
		return
	}
	lookups := m.GetCounter().GetValue()

	var hitsMetric dto.Metric
	err = LookupHits.Write(&hitsMetric)
	if err != nil {
		return
	}
	hits := hitsMetric.GetCounter().GetValue()

	var latencyMetric dto.Metric
	err = LookupLatency.Write(&latencyMetric)
	if err != nil {
		return
	}
	latencyCount := latencyMetric.GetHistogram().GetSampleCount()
	latencySum := latencyMetric.GetHistogram().GetSampleSum()

	klog.FromContext(ctx).WithName("metrics").Info("metrics beat",
		"admissions", admissions,
		"evictions", evictions,
		"lookups", lookups,
		"hits", hits,
		"latency_count", latencyCount,
		"latency_sum", latencySum,
		"latency_avg", latencySum/float64(latencyCount),
	)
}
