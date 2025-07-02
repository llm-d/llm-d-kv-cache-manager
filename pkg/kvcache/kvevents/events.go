package kvevents

import "github.com/vmihailenco/msgpack/v5"

// Event is a marker interface for KV cache events.
type Event interface {
	isEvent()
}

// EventBatch represents a batch of events.
// It is encoded as an array to match vLLM's format.
type EventBatch struct {
	_msgpack struct{} `msgpack:",array"`
	Ts       float64
	Events   []msgpack.RawMessage
}

// BlockStored event.
type BlockStored struct {
	_msgpack        struct{} `msgpack:",array"`
	BlockHashes     []int64
	ParentBlockHash *int64
	LoraId          *int
}

func (BlockStored) isEvent() {}

// BlockRemoved event.
type BlockRemoved struct {
	_msgpack    struct{} `msgpack:",array"`
	BlockHashes []int64
}

func (BlockRemoved) isEvent() {}

// AllBlocksCleared event.
type AllBlocksCleared struct {
	_msgpack struct{} `msgpack:",array"`
}

func (AllBlocksCleared) isEvent() {}
