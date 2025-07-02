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
	"strconv"

	"github.com/fxamacker/cbor/v2"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
	"k8s.io/klog/v2"
)

const defaultChunkSize = 256

// TokenProcessorConfig holds the configuration for the token processor.
type TokenProcessorConfig struct {
	ChunkSize int
	// HashSeed is used to prefix initial hash chunks, similarly to vLLM's NONE_HASH.
	// This should be aligned with vLLM's `PYTHONHASHSEED` environment variable.
	HashSeed int64
}

// DefaultTokenProcessorConfig returns the default configuration for the token processor.
func DefaultTokenProcessorConfig() *TokenProcessorConfig {
	return &TokenProcessorConfig{
		ChunkSize: defaultChunkSize,
		HashSeed:  0,
	}
}

// TokenProcessor defines the interface for converting tokens to
// KVBlockKeys.
type TokenProcessor interface {
	// TokensToKVBlockKeys converts tokens into kv_block.Keys.
	TokensToKVBlockKeys(parentHash *int64, tokens []uint32, modelName string) []Key
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

// getInitHash returns the initial hash by computing SHA-256 of the HashSeed.
// This mimics Python's sha256(os.getenv("PYTHONHASHSEED")).
func (db *ChunkedTokenDatabase) getInitHash() *int64 {
	seedStr := strconv.FormatInt(db.HashSeed, 10)
	sum := sha256.Sum256([]byte(seedStr))
	// Convert to int64 using big-endian (matching Python's int.from_bytes with byteorder="big")
	hash := int64(binary.BigEndian.Uint64(sum[:8]))
	return &hash
}

// hash computes the SHA-256 hash using CBOR serialization, mimicking Python's sha256_cbor function.
// It serializes the input using canonical CBOR, computes SHA-256, and returns the first 8 bytes as int64.
func (db *ChunkedTokenDatabase) hash(parent int64, tokens []uint32, extra interface{}) int64 {
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
	// Convert to int64 using big-endian (matching Python's int.from_bytes with byteorder="big")
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// chunkTokens splits the input slice of tokens into chunks of size chunkSize.
func (db *ChunkedTokenDatabase) chunkTokens(tokens []uint32) [][]uint32 {
	var chunks [][]uint32
	for i := 0; i < len(tokens); i += db.ChunkSize {
		end := i + db.ChunkSize
		if end > len(tokens) {
			break // no partial blocks
		}

		chunks = append(chunks, tokens[i:end])
	}

	return chunks
}

// prefixHashes computes the rolling (prefix) hash for each chunk and
// returns a slice of hash values. It starts from the parentHash
// and then for each token chunk it computes the new hash.
func (db *ChunkedTokenDatabase) prefixHashes(parentHash int64, tokenChunks [][]uint32) []int64 {
	prefixHash := parentHash
	hashes := make([]int64, len(tokenChunks))
	for i, chunk := range tokenChunks {
		prefixHash = db.hash(prefixHash, chunk, nil)
		hashes[i] = prefixHash
	}
	return hashes
}

// TokensToKVBlockKeys converts tokens into kv_block.Keys.
func (db *ChunkedTokenDatabase) TokensToKVBlockKeys(parentHash *int64, tokens []uint32, modelName string) []Key {
	if parentHash == nil {
		// If no parent hash is provided, use the initial hash.
		parentHash = db.getInitHash()
	}

	tokenChunks := db.chunkTokens(tokens)
	prefixHashes := db.prefixHashes(*parentHash, tokenChunks)

	return utils.SliceMap(prefixHashes, func(hashVal int64) Key {
		return Key{
			ModelName: modelName,
			ChunkHash: hashVal,
		}
	})
}
