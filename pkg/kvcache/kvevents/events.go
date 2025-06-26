package kvevents

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

// EventBatch corresponds to the vLLM EventBatch class.
// It's a batch of events with a timestamp.
type EventBatch struct {
	Timestamp float64 `msgpack:"ts"`
	Events    []any   `msgpack:"events"`
	// DataParallelRank int      `msgpack:"data_parallel_rank"`
	_ struct{} `msgpack:",as_array"`
}

// KVCacheEvent represents the base for KV-cache events.
type KVCacheEvent interface {
	isKVCacheEvent()
}

// BlockStored corresponds to the vLLM BlockStored class.
type BlockStored struct {
	BlockHashes     []int64 `msgpack:"block_hashes"`
	ParentBlockHash *int64  `msgpack:"parent_block_hash"`
	// TokenIDs        []int    `msgpack:"token_ids"`
	// BlockSize       int      `msgpack:"block_size"`
	// LoraID          *int     `msgpack:"lora_id"`
	_ struct{} `msgpack:",as_array"`
}

func (BlockStored) isKVCacheEvent() {}

// BlockRemoved corresponds to the vLLM BlockRemoved class.
type BlockRemoved struct {
	BlockHashes []int64  `msgpack:"block_hashes"`
	_           struct{} `msgpack:",as_array"`
}

func (BlockRemoved) isKVCacheEvent() {}

// AllBlocksCleared corresponds to the vLLM AllBlocksCleared class.
type AllBlocksCleared struct {
	_ struct{} `msgpack:",as_array"`
}

func (AllBlocksCleared) isKVCacheEvent() {}

// KVEventBatch corresponds to the vLLM KVEventBatch class.
// It contains a list of KV-cache events.
type KVEventBatch struct {
	Timestamp float64        `msgpack:"ts"`
	Events    []KVCacheEvent `msgpack:"events"`
	// DataParallelRank int            `msgpack:"data_parallel_rank"`
	_ struct{} `msgpack:",as_array"`
}

// UnmarshalMsgpack decodes a msgpack array into a KVEventBatch struct.
func (b *KVEventBatch) UnmarshalMsgpack(dec *msgpack.Decoder) error {
	var err error
	var l int
	if l, err = dec.DecodeArrayLen(); err != nil {
		return err
	}
	if l < 2 {
		return fmt.Errorf("array too short for KVEventBatch")
	}

	b.Timestamp, err = dec.DecodeFloat64()
	if err != nil {
		return fmt.Errorf("failed to decode timestamp: %w", err)
	}

	// The second element is the list of events.
	eventCount, err := dec.DecodeArrayLen()
	if err != nil {
		return fmt.Errorf("failed to decode events array length: %w", err)
	}
	b.Events = make([]KVCacheEvent, eventCount)
	for idx := 0; idx < eventCount; idx++ {
		// Each event is a 2-element array: [tag, data]
		if _, err := dec.DecodeArrayLen(); err != nil {
			return fmt.Errorf("failed to decode event tuple array length: %w", err)
		}

		tag, err := dec.DecodeString()
		if err != nil {
			return fmt.Errorf("failed to decode event tag: %w", err)
		}

		// Decode based on the tag
		switch tag {
		case "BlockStored":
			var event BlockStored
			if err := dec.Decode(&event); err != nil {
				return err
			}
			b.Events[idx] = event
		case "BlockRemoved":
			var event BlockRemoved
			if err := dec.Decode(&event); err != nil {
				return err
			}
			b.Events[idx] = event
		case "AllBlocksCleared":
			var event AllBlocksCleared
			// AllBlocksCleared has no fields, so we just decode the empty array
			if _, err := dec.DecodeArrayLen(); err != nil {
				return err
			}
			b.Events[idx] = event
		default:
			// If unknown, we must still consume the data to not corrupt the stream
			if _, err := dec.DecodeInterface(); err != nil {
				return err
			}
		}
	}

	return nil
}
