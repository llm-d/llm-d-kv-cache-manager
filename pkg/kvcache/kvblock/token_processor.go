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
	"crypto/sha256"
	"encoding/binary"

	"github.com/fxamacker/cbor/v2"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
)

// defaultBlockSize is the default number of tokens per block.
// 16 is the default value used by vLLM.
const defaultBlockSize = 16

// TokenProcessorConfig holds the configuration for the token processor.
type TokenProcessorConfig struct {
	BlockSize int `json:"blockSize"`
	// HashSeed is used to prefix initial hash chunks, similarly to vLLM's NONE_HASH.
	// This should be aligned with vLLM's `PYTHONHASHSEED` environment variable.
	// The system's deployer is responsible for aligning the vLLM deployments
	// with the same seed value.
	HashSeed string  `json:"hashSeed"`
	initHash *uint64 // cache once
}

// DefaultTokenProcessorConfig returns the default configuration for the token processor.
func DefaultTokenProcessorConfig() *TokenProcessorConfig {
	return &TokenProcessorConfig{
		BlockSize: defaultBlockSize,
		HashSeed:  "",
	}
}

// TokenProcessor defines the interface for converting tokens to
// KVBlockKeys.
type TokenProcessor interface {
	// TokensToKVBlockKeys converts tokens into kv_block.Keys.
	TokensToKVBlockKeys(tokens []uint32, modelName string) []Key
}

// ChunkedTokenDatabase is a concrete implementation of TokenDatabase.
// It mimics the ChunkedTokenDatabase in the Python code.
type ChunkedTokenDatabase struct {
	TokenProcessorConfig
}

var _ TokenProcessor = &ChunkedTokenDatabase{}

// NewChunkedTokenDatabase creates a new instance with the given config and metadata.
func NewChunkedTokenDatabase(config *TokenProcessorConfig) TokenProcessor {
	if config == nil {
		config = DefaultTokenProcessorConfig()
	} // TODO: validate?

	return &ChunkedTokenDatabase{
		TokenProcessorConfig: *config,
	}
}

// getInitHash returns the root parent hash.
func (db *ChunkedTokenDatabase) getInitHash() *uint64 {
	if db.initHash != nil {
		return db.initHash
	}

	encMode, err := cbor.CanonicalEncOptions().EncMode() // deterministic
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to create CBOR encoder")
		return nil
	}

	b, err := encMode.Marshal(db.HashSeed)
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to marshal payload to CBOR")
		return nil
	}

	sum := sha256.Sum256(b)
	hashVal := binary.BigEndian.Uint64(sum[24:])
	db.initHash = &hashVal
	return db.initHash
}

// hash computes a uint64 hash (lower 64 bits of SHA256).
// The format, serialization and hashing is aligned with that of vLLM.
func (db *ChunkedTokenDatabase) hash(parent uint64, tokens []uint32, extra interface{}) uint64 {
	payload := []interface{}{parent, tokens, extra}

	encMode, err := cbor.CanonicalEncOptions().EncMode() // deterministic
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to create CBOR encoder")
		return 0
	}

	b, err := encMode.Marshal(payload)
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to marshal payload to CBOR")
		return 0
	}

	sum := sha256.Sum256(b)
	return binary.BigEndian.Uint64(sum[24:])
}

// prefixHashes returns a slice of uint64 hashes.
func (db *ChunkedTokenDatabase) prefixHashes(parentHash uint64, tokenChunks [][]uint32) []uint64 {
	prefix := parentHash
	hashes := make([]uint64, len(tokenChunks))
	for i, chunk := range tokenChunks {
		prefix = db.hash(prefix, chunk, nil)
		hashes[i] = prefix
	}
	return hashes
}

// chunkTokens splits the input slice of tokens into chunks of size chunkSize.
func (db *ChunkedTokenDatabase) chunkTokens(tokens []uint32) [][]uint32 {
	var chunks [][]uint32
	for i := 0; i < len(tokens); i += db.BlockSize {
		end := i + db.BlockSize
		if end > len(tokens) {
			break // no partial blocks
		}

		chunks = append(chunks, tokens[i:end])
	}

	return chunks
}

// TokensToKVBlockKeys converts tokens into kv_block.Keys.
func (db *ChunkedTokenDatabase) TokensToKVBlockKeys(tokens []uint32, modelName string) []Key {
	parentPtr := db.getInitHash()
	if parentPtr == nil {
		return nil
	}

	chunks := db.chunkTokens(tokens)
	ph := db.prefixHashes(*parentPtr, chunks)
	return utils.SliceMap(ph, func(hashVal uint64) Key {
		return Key{
			ModelName: modelName,
			ChunkHash: hashVal,
		}
	})
}
