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

import (
	"context"
	"encoding/json"
	"fmt"
	"unsafe"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
	"k8s.io/klog/v2"
)

/*
#cgo CFLAGS: -I/Users/guygirmonsky/.pyenv/versions/3.12.11/include/python3.12
#cgo LDFLAGS: -L/Users/guygirmonsky/.pyenv/versions/3.12.11/lib -lpython3.12
#include "cgo_functions.h"
*/
import "C"

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
// SIMPLIFIED: Removed redundant tracking since C handles everything
type ChatTemplateCGoWrapper struct {
	// REMOVED: initialized bool - C tracks this
	// REMOVED: mu sync.Mutex - C handles thread safety
	// REMOVED: initOnce sync.Once - C ensures single initialization
}

// NewChatTemplateCGoWrapper creates a new CGo wrapper.
// SIMPLIFIED: No initialization tracking needed
func NewChatTemplateCGoWrapper() *ChatTemplateCGoWrapper {
	return &ChatTemplateCGoWrapper{}
}

// Initialize initializes the Python interpreter and caches the module
// SIMPLIFIED: Just call C functions - let C handle all tracking
func (w *ChatTemplateCGoWrapper) Initialize() error {
	// Initialize Python interpreter - C handles process-level tracking
	C.Py_InitializeGo()

	// Initialize chat template module - C handles module-level tracking
	result := C.Py_InitChatTemplateModule()
	if result != 0 {
		return fmt.Errorf("failed to initialize chat template module")
	}

	return nil
}

// Finalize finalizes the Python interpreter and cleans up the module
// SIMPLIFIED: Just call C functions - let C handle all cleanup
func (w *ChatTemplateCGoWrapper) Finalize() {
	// Clean up the module first
	C.Py_CleanupChatTemplateModule()

	// Then finalize Python interpreter
	C.Py_FinalizeGo()
}

// RenderChatTemplate renders a chat template using the cached Python function.
// REQUIRES: The wrapper must be initialized before calling this method.
func (w *ChatTemplateCGoWrapper) RenderChatTemplate(ctx context.Context, req ChatTemplateRequest) (*ChatTemplateResponse, error) {
	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("chat-template.RenderChatTemplate")

	// Convert request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		traceLogger.Error(err, "Failed to marshal request")
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	// Call the cached Python function
	cResult := C.Py_CallRenderJinjaTemplate(C.CString(string(reqJSON)))
	if cResult == nil {
		traceLogger.Error(nil, "C function returned nil")
		return nil, fmt.Errorf("python render_jinja_template failed")
	}
	defer C.free(unsafe.Pointer(cResult))
	resultJSON := C.GoString(cResult)

	// Parse the response
	var response ChatTemplateResponse
	if err := json.Unmarshal([]byte(resultJSON), &response); err != nil {
		traceLogger.Error(err, "Failed to unmarshal response")
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

// GetModelChatTemplate fetches the model chat template using the cached Python function.
// REQUIRES: The wrapper must be initialized before calling this method.
func (w *ChatTemplateCGoWrapper) GetModelChatTemplate(ctx context.Context, req GetChatTemplateRequest) (string, map[string]interface{}, error) {
	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("chat-template.GetModelChatTemplate")

	// Convert request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		traceLogger.Error(err, "Failed to marshal request")
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	// Call the cached Python function
	cResult := C.Py_CallGetModelChatTemplate(C.CString(string(reqJSON)))
	if cResult == nil {
		traceLogger.Error(nil, "C function returned nil")
		return "", nil, fmt.Errorf("python get_model_chat_template failed")
	}
	defer C.free(unsafe.Pointer(cResult))
	resultJSON := C.GoString(cResult)

	// Parse the response
	var response GetModelChatTemplateResponse
	if err := json.Unmarshal([]byte(resultJSON), &response); err != nil {
		traceLogger.Error(err, "Failed to unmarshal response")
		return "", nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Template, response.TemplateVars, nil
}

// ClearCaches clears all caches for testing purposes.
// REQUIRES: The wrapper must be initialized before calling this method.
func (w *ChatTemplateCGoWrapper) ClearCaches(ctx context.Context) error {
	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("chat-template.ClearCaches")

	// Call the C function
	cResult := C.Py_ClearCaches()
	if cResult == nil {
		traceLogger.Error(nil, "Failed to clear caches")
		return fmt.Errorf("failed to clear caches")
	}
	defer C.free(unsafe.Pointer(cResult))

	return nil
}
