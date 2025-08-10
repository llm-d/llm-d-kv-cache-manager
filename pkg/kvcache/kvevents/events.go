package kvevents

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

// event is a marker interface for KV-cache events.
type event interface {
	isEvent()
}

// EventBatch represents a batch of events.
// It is encoded as an array to match vLLM's format.
type EventBatch struct {
	_                struct{} `msgpack:",array"`
	TS               float64
	Events           []msgpack.RawMessage
	DataParallelRank *int `msgpack:",omitempty"`
}

// DecodeMsgpack allows 2- or 3-element array-encoded batches.
func (e *EventBatch) DecodeMsgpack(decoder *msgpack.Decoder) error {
	length, err := decoder.DecodeArrayLen()
	if err != nil {
		return err
	}
	if length < 2 {
		return fmt.Errorf("EventBatch: expected at least 2 fields, got %d", length)
	}
	if e.TS, err = decoder.DecodeFloat64(); err != nil {
		return err
	}
	if err := decoder.Decode(&e.Events); err != nil {
		return err
	}
	if length > 2 {
		var rank int
		if err := decoder.Decode(&rank); err != nil {
			return err
		}
		e.DataParallelRank = &rank
	}
	// skip any extra
	for i := 3; i < length; i++ {
		if err := decoder.Skip(); err != nil {
			return err
		}
	}
	return nil
}

// BlockStored event.
type BlockStored struct {
	_               struct{} `msgpack:",array"`
	BlockHashes     []uint64
	ParentBlockHash *uint64
	TokenIds        []uint32
	BlockSize       int
	LoraID          *int
}

func (BlockStored) isEvent() {}

type BlockStoredEvent struct {
	_         struct{} `msgpack:",array"`
	TypeField string
	*BlockStored
}

// BlockRemoved event.
type BlockRemoved struct {
	_           struct{} `msgpack:",array"`
	BlockHashes []uint64
}

func (BlockRemoved) isEvent() {}

type BlockRemovedEvent struct {
	_         struct{} `msgpack:",array"`
	TypeField string
	*BlockRemoved
}

// AllBlocksCleared event.
type AllBlocksCleared struct {
	_ struct{} `msgpack:",array"`
}

func (AllBlocksCleared) isEvent() {}
