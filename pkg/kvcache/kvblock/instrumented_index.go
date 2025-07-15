package kvblock

import (
	"context"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
)

type instrumentedIndex struct {
	next Index
}

// NewInstrumentedIndex wraps an Index and emits metrics for Add, Evict, and
// Lookup.
func NewInstrumentedIndex(next Index) Index {
	return &instrumentedIndex{next: next}
}

func (m *instrumentedIndex) Add(ctx context.Context, keys []Key, entries []PodEntry) error {
	err := m.next.Add(ctx, keys, entries)
	metrics.Admissions.Add(float64(len(keys)))
	return err
}

func (m *instrumentedIndex) Evict(ctx context.Context, key Key, entries []PodEntry) error {
	err := m.next.Evict(ctx, key, entries)
	metrics.Evictions.Add(float64(len(entries)))
	return err
}

func (m *instrumentedIndex) Lookup(
	ctx context.Context,
	keys []Key,
	podIdentifierSet sets.Set[string],
) ([]Key, map[Key][]string, error) {
	timer := prometheus.NewTimer(metrics.LookupLatency)
	defer timer.ObserveDuration()

	metrics.LookupRequests.Inc()
	hitKeys, pods, err := m.next.Lookup(ctx, keys, podIdentifierSet)

	metrics.KeyLookupResults.WithLabelValues("hit").Add(float64(len(hitKeys)))
	metrics.KeyLookupResults.WithLabelValues("miss").Add(float64(len(keys) - len(hitKeys)))

	return hitKeys, pods, err
}
