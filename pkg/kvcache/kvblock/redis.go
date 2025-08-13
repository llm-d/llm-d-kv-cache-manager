/*
Copyright 2025 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kvblock

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

// RedisIndexConfig holds the configuration for the RedisIndex.
type RedisIndexConfig struct {
	Address string `json:"address,omitempty"` // Redis server address
}

func DefaultRedisIndexConfig() *RedisIndexConfig {
	return &RedisIndexConfig{
		Address: "redis://127.0.0.1:6379",
	}
}

// NewRedisIndex creates a new RedisIndex instance.
func NewRedisIndex(config *RedisIndexConfig) (Index, error) {
	if config == nil {
		config = DefaultRedisIndexConfig()
	}

	if !strings.HasPrefix(config.Address, "redis://") &&
		!strings.HasPrefix(config.Address, "rediss://") &&
		!strings.HasPrefix(config.Address, "unix://") {
		config.Address = "redis://" + config.Address
	}

	redisOpt, err := redis.ParseURL(config.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redisURL: %w", err)
	}

	redisClient := redis.NewClient(redisOpt)
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisIndex{
		RedisClient: redisClient,
	}, nil
}

// RedisIndex implements the Index interface
// using Redis as the backend for KV block indexing.
type RedisIndex struct {
	RedisClient *redis.Client
}

var _ Index = &RedisIndex{}

// Lookup receives a list of keys and a set of pod identifiers,
// and retrieves the filtered pods associated with those keys.
// The filtering is done based on the pod identifiers provided.
// If the podIdentifierSet is empty, all pods are returned.
//
// It returns:
// 1. A map where the keys are those in (1) and the values are pod-identifiers.
// 2. An error if any occurred during the operation.
func (r *RedisIndex) Lookup(ctx context.Context, keys []Key,
	podIdentifierSet sets.Set[string],
) (map[Key][]string, error) {
	if len(keys) == 0 {
		return make(map[Key][]string), nil
	}

	logger := klog.FromContext(ctx).WithName("kvblock.RedisIndex.Lookup")
	podsPerKey := make(map[Key][]string)

	// pipeline for single RTT
	pipe := r.RedisClient.Pipeline()
	results := make([]*redis.StringSliceCmd, len(keys))

	// queue an HKeys command for each key in the pipeline
	for i, key := range keys {
		// HKeys gets all field names
		results[i] = pipe.HKeys(ctx, key.String())
	}

	_, execErr := pipe.Exec(ctx)
	if execErr != nil {
		return nil, fmt.Errorf("redis pipeline execution failed: %w", execErr)
	}

	filterPods := len(podIdentifierSet) > 0 // predicate for filtering

	for idx, cmd := range results {
		key := keys[idx]

		// cmd.Result() returns the slice of strings (pod IDs) which is the first layer in the mapping
		pods, cmdErr := cmd.Result()
		if cmdErr != nil {
			if !errors.Is(cmdErr, redis.Nil) {
				logger.Error(cmdErr, "failed to get pods for key", "key", key)
			}

			return podsPerKey, nil // early stop since prefix-chain breaks here
		}

		var filteredPods []string
		for _, p := range pods {
			ip := strings.SplitN(p, "@", 2)[0]
			if !filterPods || podIdentifierSet.Has(ip) {
				filteredPods = append(filteredPods, ip)
			}
		}

		if len(filteredPods) == 0 {
			logger.Info("no pods found for key, cutting search", "key", key)
			return podsPerKey, nil // early stop since prefix-chain breaks here
		}

		podsPerKey[key] = filteredPods
	}

	return podsPerKey, nil
}

// Add adds a set of keys and their associated pod entries to the index backend.
func (r *RedisIndex) Add(ctx context.Context, keys []Key, entries []PodEntry) error {
	if len(keys) == 0 || len(entries) == 0 {
		return nil
	}

	pipe := r.RedisClient.Pipeline()
	for _, key := range keys {
		redisKey := key.String()
		for _, entry := range entries {
			// Use HSet to add the pod identifier as a field in the hash
			pipe.HSet(ctx, redisKey, entry.String(), time.Now().Format(time.RFC3339))
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to add entries to Redis: %w", err)
	}

	return nil
}

// Evict removes a key and its associated pod entries from the index backend.
func (r *RedisIndex) Evict(ctx context.Context, key Key, entries []PodEntry) error {
	redisKey := key.String()
	pipe := r.RedisClient.Pipeline()

	for _, entry := range entries {
		// Use HDel to remove the pod identifier field from the hash
		pipe.HDel(ctx, redisKey, entry.String())
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to evict entries from Redis: %w", err)
	}

	return nil
}
