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

package kvevents

import (
	"github.com/vmihailenco/msgpack/v5"
)

const (
	// BlockStoredEventTag is the tag for BlockStored events.
	BlockStoredEventTag = "BlockStored"
	// BlockRemovedEventTag is the tag for BlockRemoved events.
	BlockRemovedEventTag = "BlockRemoved"
	// AllBlocksClearedEventTag is the tag for AllBlocksCleared events.
	AllBlocksClearedEventTag = "AllBlocksCleared"
)

// event is a marker interface for KV-cache events.
type event interface {
	isEvent()
	ToTaggedUnion() []any
}

// EventBatch represents a batch of events.
// It is encoded as an array to match vLLM's format.
type EventBatch struct {
	_                struct{} `msgpack:",array"`
	TS               float64
	Events           []msgpack.RawMessage
	DataParallelRank *int `msgpack:",omitempty"`
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

func (bs BlockStored) ToTaggedUnion() []any {
	return []any{
		BlockStoredEventTag,
		bs.BlockHashes,
		bs.ParentBlockHash,
		bs.TokenIds,
		bs.BlockSize,
		bs.LoraID,
	}
}

func (BlockStored) isEvent() {}

// BlockRemoved event.
type BlockRemoved struct {
	_           struct{} `msgpack:",array"`
	BlockHashes []uint64
}

func (br BlockRemoved) ToTaggedUnion() []any {
	return []any{
		BlockRemovedEventTag,
		br.BlockHashes,
	}
}

func (BlockRemoved) isEvent() {}

// AllBlocksCleared event.
type AllBlocksCleared struct {
	_ struct{} `msgpack:",array"`
}

func (ac AllBlocksCleared) ToTaggedUnion() []any {
	return []any{
		AllBlocksClearedEventTag,
	}
}

func (AllBlocksCleared) isEvent() {}
