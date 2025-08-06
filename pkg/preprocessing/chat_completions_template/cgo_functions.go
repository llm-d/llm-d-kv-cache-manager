//go:build exclude

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

package chat_completions_template

/*
#cgo CFLAGS: -I{{PYTHON_PATH}}/include/python{{PYTHON_VERSION}}
#cgo LDFLAGS: -L{{PYTHON_PATH}}/lib -lpython{{PYTHON_VERSION}}
#include "cgo_functions.h"
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

// Global initialization tracking
var (
	globalInitialized bool
	globalInitOnce    sync.Once
	globalInitMutex   sync.Mutex
)

// ChatMessage represents a single message in a conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatTemplateRequest represents the request to render a chat template
type ChatTemplateRequest struct {
	Conversations             [][]ChatMessage        `json:"conversations"`
	Tools                     []interface{}          `json:"tools,omitempty"`
	Documents                 []interface{}          `json:"documents,omitempty"`
	ChatTemplate              string                 `json:"chat_template,omitempty"`
	ReturnAssistantTokensMask bool                   `json:"return_assistant_tokens_mask,omitempty"`
	ContinueFinalMessage      bool                   `json:"continue_final_message,omitempty"`
	AddGenerationPrompt       bool                   `json:"add_generation_prompt,omitempty"`
	TemplateVars              map[string]interface{} `json:"template_vars,omitempty"`
}

// ChatTemplateResponse represents the response from the Python function
type ChatTemplateResponse struct {
	RenderedChats     []string  `json:"rendered_chats"`
	GenerationIndices [][][]int `json:"generation_indices"`
}

// ChatTemplateCGoWrapper wraps the CGo functions for chat template operations
type ChatTemplateCGoWrapper struct {
	initialized bool
	mu          sync.Mutex
	initOnce    sync.Once
}

// NewChatTemplateCGoWrapper creates a new CGo wrapper
func NewChatTemplateCGoWrapper() *ChatTemplateCGoWrapper {
	return &ChatTemplateCGoWrapper{
		initialized: false,
	}
}

// Initialize initializes the Python interpreter and caches the module
func (w *ChatTemplateCGoWrapper) Initialize() error {
	// Use global initialization to ensure Python is only initialized once
	globalInitOnce.Do(func() {
		// Initialize Python interpreter
		C.Py_InitializeGo()

		// Initialize chat template module
		result := C.Py_InitChatTemplateModule()
		if result != 0 {
			fmt.Printf("[Go] Global Initialize ERROR - Failed to initialize chat template module\n")
			return
		}

		globalInitialized = true
	})

	// Set this instance as initialized
	w.initialized = true

	if !globalInitialized {
		return fmt.Errorf("failed to initialize chat template module")
	}

	return nil
}

// Finalize finalizes the Python interpreter and cleans up the module
func (w *ChatTemplateCGoWrapper) Finalize() {
	if w.initialized {
		// Use defer with recover to handle any segmentation faults
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[Go] Finalize recovered from panic: %v\n", r)
			}
		}()

		// Clean up the module first
		C.Py_CleanupChatTemplateModule()

		// Then finalize Python interpreter
		C.Py_FinalizeGo()

		w.initialized = false
	}
}

// RenderChatTemplate renders a chat template using the cached Python function
func (w *ChatTemplateCGoWrapper) RenderChatTemplate(req ChatTemplateRequest) (*ChatTemplateResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Initialize if needed (sync.Once ensures this only happens once)
	if err := w.Initialize(); err != nil {
		fmt.Printf("[Go] RenderChatTemplate ERROR - Failed to initialize: %v\n", err)
		return nil, fmt.Errorf("failed to initialize Python: %w", err)
	}

	// Convert request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("[Go] RenderChatTemplate ERROR - Failed to marshal request: %v\n", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	// Call the cached Python function
	cResult := C.Py_CallRenderJinjaTemplate(C.CString(string(reqJSON)))
	if cResult == nil {
		fmt.Printf("[Go] RenderChatTemplate ERROR - C function returned nil\n")
		return nil, fmt.Errorf("python render_jinja_template failed")
	}
	defer C.free(unsafe.Pointer(cResult))
	resultJSON := C.GoString(cResult)

	// Parse the response
	var response ChatTemplateResponse
	if err := json.Unmarshal([]byte(resultJSON), &response); err != nil {
		fmt.Printf("[Go] RenderChatTemplate ERROR - Failed to unmarshal response: %v\n", err)
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// Struct for the chat template fetch request
type GetChatTemplateRequest struct {
	ModelName    string        `json:"model_name"`
	ChatTemplate string        `json:"chat_template,omitempty"`
	Tools        []interface{} `json:"tools,omitempty"`
	Revision     string        `json:"revision,omitempty"`
	Token        string        `json:"token,omitempty"`
}

// Struct for the response
// GetModelChatTemplateResponse holds the template and template variables
type GetModelChatTemplateResponse struct {
	Template     string                 `json:"template"`
	TemplateVars map[string]interface{} `json:"template_vars"`
}

// GetModelChatTemplate fetches the model chat template using the cached Python function
func (w *ChatTemplateCGoWrapper) GetModelChatTemplate(req GetChatTemplateRequest) (string, map[string]interface{}, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Initialize if needed (sync.Once ensures this only happens once)
	if err := w.Initialize(); err != nil {
		fmt.Printf("[Go] GetModelChatTemplate ERROR - Failed to initialize: %v\n", err)
		return "", nil, fmt.Errorf("failed to initialize Python: %w", err)
	}

	// Convert request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("[Go] GetModelChatTemplate ERROR - Failed to marshal request: %v\n", err)
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	// Call the cached Python function
	cResult := C.Py_CallGetModelChatTemplate(C.CString(string(reqJSON)))
	if cResult == nil {
		fmt.Printf("[Go] GetModelChatTemplate ERROR - C function returned nil\n")
		return "", nil, fmt.Errorf("python get_model_chat_template failed")
	}
	defer C.free(unsafe.Pointer(cResult))
	resultJSON := C.GoString(cResult)

	// Parse the response
	var response GetModelChatTemplateResponse
	if err := json.Unmarshal([]byte(resultJSON), &response); err != nil {
		fmt.Printf("[Go] GetModelChatTemplate ERROR - Failed to unmarshal response: %v\n", err)
		return "", nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Template, response.TemplateVars, nil
}
