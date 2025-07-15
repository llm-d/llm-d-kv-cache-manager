package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/klog/v2"
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
	// KeyLookupResults counts keys examined, labelled by result “hit” or
	// “miss”.
	KeyLookupResults = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "lookup_key_total",
		Help: "Number of keys looked up by result (hit/miss)",
	}, []string{"result"})
	// LookupLatency logs latency of lookup calls.
	LookupLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "kvcache", Subsystem: "index", Name: "lookup_latency_seconds",
		Help:    "Latency of Lookup calls in seconds",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(
		Admissions, Evictions,
		LookupRequests, KeyLookupResults, LookupLatency,
	)
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

	var hit, miss dto.Metric
	err = KeyLookupResults.WithLabelValues("hit").Write(&hit)
	if err != nil {
		return
	}
	err = KeyLookupResults.WithLabelValues("miss").Write(&miss)
	if err != nil {
		return
	}
	hits := hit.GetCounter().GetValue()
	misses := miss.GetCounter().GetValue()

	var h dto.Metric
	err = LookupLatency.Write(&h)
	if err != nil {
		return
	}
	latencyCount := h.GetHistogram().GetSampleCount()
	latencySum := h.GetHistogram().GetSampleSum()

	klog.FromContext(ctx).WithName("metrics").Info("metrics beat",
		"admissions", admissions,
		"evictions", evictions,
		"lookups", lookups,
		"hits", hits,
		"misses", misses,
		"latency_count", latencyCount,
		"latency_sum", latencySum,
		"latency_avg", latencySum/float64(latencyCount),
	)
}
