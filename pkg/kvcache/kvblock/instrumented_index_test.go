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

package kvblock_test

import (
	"testing"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/stretchr/testify/assert"
)

func TestNewInstrumentedIndex(t *testing.T) {
	// Create base index
	baseIndex, err := kvblock.NewInMemoryIndex(nil)
	assert.NoError(t, err)

	// Wrap with instrumentation
	instrumented := kvblock.NewInstrumentedIndex(baseIndex)
	assert.NotNil(t, instrumented)

	// Verify it implements Index interface
	assert.Implements(t, (*kvblock.Index)(nil), instrumented)
}

func TestInstrumentedIndexBasicFunctionality(t *testing.T) {
	// Create instrumented index
	baseIndex, err := kvblock.NewInMemoryIndex(nil)
	assert.NoError(t, err)
	instrumented := kvblock.NewInstrumentedIndex(baseIndex)

	// Test that basic functionality still works through the wrapper
	testAddBasic(t, instrumented)
}
