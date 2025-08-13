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

package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	otelTrace "go.opentelemetry.io/otel/trace"
)

const (
	ServiceName = "llm-d-kv-cache-manager"

	envOTELTracingEnabled   = "OTEL_TRACING_ENABLED"
	envOTELExporterEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envOTELSamplingRate     = "OTEL_SAMPLING_RATE"
)

type Config struct {
	Enabled          bool
	ExporterEndpoint string
	SamplingRate     float64
	ServiceName      string
}

func NewConfigFromEnv() *Config {
	config := &Config{
		Enabled:          false,
		ExporterEndpoint: "http://localhost:4317",
		SamplingRate:     0.1,
		ServiceName:      ServiceName,
	}

	if enabled := os.Getenv(envOTELTracingEnabled); enabled != "" {
		if enabledBool, err := strconv.ParseBool(enabled); err == nil {
			config.Enabled = enabledBool
		}
	}

	if endpoint := os.Getenv(envOTELExporterEndpoint); endpoint != "" {
		config.ExporterEndpoint = endpoint
	}

	if samplingRateStr := os.Getenv(envOTELSamplingRate); samplingRateStr != "" {
		if samplingRate, err := strconv.ParseFloat(samplingRateStr, 64); err == nil {
			config.SamplingRate = samplingRate
		}
	}

	return config
}

func Initialize(ctx context.Context, config *Config) (func(context.Context) error, error) {
	// Always set up context propagation, even when tracing is disabled
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// If tracing is disabled, return a no-op shutdown function
	if !config.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(config.ExporterEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(config.SamplingRate)),
	)

	otel.SetTracerProvider(tracerProvider)

	return tracerProvider.Shutdown, nil
}

func GetTracer() otelTrace.Tracer {
	return otel.Tracer(ServiceName)
}

const (
	// KV Cache specific attributes.
	AttrKVCacheHitRatio   = "llm_d.kv_cache.hit_ratio"
	AttrKVCacheBlockKeys  = "llm_d.kv_cache.block_keys"
	AttrKVCachePodCount   = "llm_d.kv_cache.pod_count"
	AttrKVCacheTokenCount = "llm_d.kv_cache.token_count" //nolint:gosec // false positive - not credentials
	AttrKVCachePromptHash = "llm_d.kv_cache.prompt_hash"

	// GenAI request attributes.
	AttrGenAIRequestModel         = "gen_ai.request.model"
	AttrGenAIResponseFinishReason = "gen_ai.response.finish_reason"

	// Operation attributes.
	AttrOperationType    = "operation.type"
	AttrOperationOutcome = "operation.outcome"
)

// Operation types for KV cache operations.
const (
	OperationGetPodScores = "get_pod_scores"
	OperationFindTokens   = "find_tokens"
	OperationScorePods    = "score_pods"
)

const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
	OutcomeTimeout = "timeout"
)

func StartSpan(ctx context.Context, operationName, operationType string) (context.Context, otelTrace.Span) {
	tracer := GetTracer()
	ctx, span := tracer.Start(ctx, operationName)

	span.SetAttributes(
		attribute.String(AttrOperationType, operationType),
	)

	return ctx, span
}

func SetSpanError(span otelTrace.Span, err error) {
	if err != nil {
		span.SetAttributes(
			attribute.String(AttrOperationOutcome, OutcomeError),
		)
		span.RecordError(err)
	} else {
		span.SetAttributes(
			attribute.String(AttrOperationOutcome, OutcomeSuccess),
		)
	}
}
