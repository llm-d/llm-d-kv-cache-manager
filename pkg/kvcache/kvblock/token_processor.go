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
	HashSeed string `json:"hashSeed"`
	initHash []byte // cache once
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

// getInitHash returns the root parent hash as a full byte slice.
func (db *ChunkedTokenDatabase) getInitHash() []byte {
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
	db.initHash = sum[:] // Return the full 32-byte hash
	return db.initHash
}

// hash computes the full 32-byte SHA256 hash of the given parent, tokens,
// and extra keys, mimicking the vLLM implementation.
func (db *ChunkedTokenDatabase) hash(parent []byte, tokens []uint32, extra interface{}) []byte {
	payload := []interface{}{parent, tokens, extra}

	encMode, err := cbor.CanonicalEncOptions().EncMode() // deterministic
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to create CBOR encoder")
		return nil
	}

	b, err := encMode.Marshal(payload)
	if err != nil {
		klog.FromContext(context.Background()).Error(err, "failed to marshal payload to CBOR")
		return nil
	}

	sum := sha256.Sum256(b)
	return sum[:] // Return the full 32-byte hash
}

// prefixHashes returns a slice of full 32-byte hashes.
func (db *ChunkedTokenDatabase) prefixHashes(parentHash []byte, tokenChunks [][]uint32) [][]byte {
	prefix := parentHash
	hashes := make([][]byte, len(tokenChunks))
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
	parentBytes := db.getInitHash()
	if parentBytes == nil {
		return nil
	}

	chunks := db.chunkTokens(tokens)
	ph := db.prefixHashes(parentBytes, chunks)

	// Convert the final byte hashes to uint64 for the Key struct
	return utils.SliceMap(ph, func(hashBytes []byte) Key {
		// Truncate to 64 bits at the very end by taking the last 8 bytes
		hashVal := binary.BigEndian.Uint64(hashBytes[24:])
		return Key{
			ModelName: modelName,
			ChunkHash: hashVal,
		}
	})
}
