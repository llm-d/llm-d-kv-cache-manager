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
	"fmt"
	"net"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/helper"
	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"

	indexerpb "github.com/llm-d/llm-d-kv-cache-manager/api"
)

const servicerAddr = ":50051"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting KV cache index service Example")

	lc := &net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", servicerAddr)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to listen: %v", servicerAddr))
	}

	// Setup ZMQ publisher to simulate vLLM engines
	publisher, err := helper.SetupPublisher(ctx)
	if err != nil {
		logger.Error(err, "failed to setup ZMQ publisher")
		return
	}
	defer publisher.Close()

	indexerSvc, err := setupIndexerService(ctx)
	if err != nil {
		logger.Error(err, "failed to create indexer service")
	}

	// Initial query - should be empty since no events have been published
	pods, err := indexerSvc.indexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
	if err != nil {
		logger.Error(err, "failed to get pod scores")
	}
	logger.Info("@@@ Initial pod scores (should be empty)", "pods", pods)

	// Simulate vLLM engine publishing some kvcache to storage
	err = helper.SimulateProduceEvent(ctx, publisher)
	if err != nil {
		logger.Error(err, "failed to simulate produce event")
	}
	grpcServer := grpc.NewServer()
	indexerpb.RegisterIndexerServiceServer(grpcServer, indexerSvc)

	logger.Info("gRPC server setup", "address", servicerAddr)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(err, "gRPC serve error")
	}
}

func setupIndexerService(ctx context.Context) (*IndexerService, error) {
	logger := klog.FromContext(ctx)
	indexer, err := helper.SetupKVCacheIndexer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	// Setup events pool with ZMQ subscriber
	eventsPool := helper.SetupEventsPool(ctx, indexer.KVBlockIndex())
	indexerSvc := NewIndexerService(eventsPool, indexer)

	// Start the indexer
	go indexer.Run(ctx)
	logger.Info("Created kvcache Indexer")

	// Start events pool
	eventsPool.Start(ctx)
	logger.Info("Events pool started and listening for ZMQ messages")

	return indexerSvc, nil
}
