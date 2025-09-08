// Copyright 2025 The llm-d Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	indexerpb "github.com/llm-d/llm-d-kv-cache-manager/api"
	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
)

func main() {
	addr := flag.String("listen", ":50051", "gRPC listen address")
	flag.Parse()

	ctx := context.Background()
	lc := &net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}

	indexerSvc, err := setupIndexerService(context.TODO(), defaultConfig())
	if err != nil {
		log.Fatalf("failed to create indexer service: %v", err)
	}

	grpcServer := grpc.NewServer()
	indexerpb.RegisterIndexerServiceServer(grpcServer, indexerSvc)

	log.Printf("gRPC server listening on %s", *addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC serve error: %v", err)
	}
}

func setupIndexerService(ctx context.Context, config *kvcache.Config) (*IndexerService, error) {
	indexer, err := setupKVCacheIndexer(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	indexerSvc := NewIndexerService(indexer)

	// Start the indexer
	go indexer.Run(ctx)

	// Add some sample data for testing
	log.Println("Adding sample KV cache data for testing...")
	err = indexerSvc.AddSampleDataToIndexer(ctx, testdata.ModelName)
	if err != nil {
		log.Printf("Warning: failed to add sample data: %v", err)
	} else {
		log.Println("Sample data added successfully")
	}

	return indexerSvc, nil
}

func setupKVCacheIndexer(ctx context.Context, config *kvcache.Config) (*kvcache.Indexer, error) {
	indexer, err := kvcache.NewKVCacheIndexer(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	// Start the indexer
	go indexer.Run(ctx)

	return indexer, nil
}

func defaultConfig() *kvcache.Config {
	cfg := kvcache.NewDefaultConfig()
	// Set the same block size as the kv_cache_index example for compatibility
	cfg.TokenProcessorConfig.BlockSize = 256
	return cfg
}
