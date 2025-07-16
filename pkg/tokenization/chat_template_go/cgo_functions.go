package chattemplatego

/*
// CGo build flags for Python 3.11
// These are platform-specific and may need adjustment for different systems
#cgo CFLAGS: -I/Library/Frameworks/Python.framework/Versions/3.11/include/python3.11
#cgo LDFLAGS: -L/Library/Frameworks/Python.framework/Versions/3.11/lib -lpython3.11
#include "cgo_functions.h"
*/
import "C"
import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unsafe"
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

// ChatTemplateCGoWrapper wraps the Python render_jinja_template function using CGo
type ChatTemplateCGoWrapper struct {
	initialized bool
}

// NewChatTemplateCGoWrapper creates a new CGo wrapper
func NewChatTemplateCGoWrapper() *ChatTemplateCGoWrapper {
	return &ChatTemplateCGoWrapper{
		initialized: false,
	}
}

// Initialize initializes the Python interpreter
func (w *ChatTemplateCGoWrapper) Initialize() error {
	if w.initialized {
		return nil
	}

	C.Py_InitializeGo()
	w.initialized = true
	return nil
}

// Finalize finalizes the Python interpreter
func (w *ChatTemplateCGoWrapper) Finalize() {
	if w.initialized {
		C.Py_FinalizeGo()
		w.initialized = false
	}
}

// RenderChatTemplateSimple is a simpler version using CGo
func (w *ChatTemplateCGoWrapper) RenderChatTemplateSimple(conversation []ChatMessage, chatTemplate string) (string, error) {
	if !w.initialized {
		if err := w.Initialize(); err != nil {
			return "", fmt.Errorf("failed to initialize Python: %w", err)
		}
	}

	// Convert conversation to JSON
	convJSON, err := json.Marshal(conversation)
	if err != nil {
		return "", fmt.Errorf("failed to marshal conversation: %w", err)
	}

	// Create Python code
	pythonCode := fmt.Sprintf(`
import sys
import os
import json

# Add current directory to Python path
sys.path.insert(0, os.getcwd())

from chat_template_wrapper import render_jinja_template

conversation = json.loads('''%s''')
chat_template = '''%s'''

rendered_chats, generation_indices = render_jinja_template(
    conversations=[conversation],
    chat_template=chat_template
)

result = rendered_chats[0]
`, string(convJSON), chatTemplate)

	// Execute Python code
	result, err := w.executePythonCode(pythonCode)
	if err != nil {
		return "", fmt.Errorf("failed to execute Python code: %w", err)
	}

	return result, nil
}

// executePythonCode executes Python code and returns the result
func (w *ChatTemplateCGoWrapper) executePythonCode(code string) (string, error) {
	// Convert Go string to C string
	cCode := C.CString(code)
	defer C.free(unsafe.Pointer(cCode))

	// Execute the Python code
	pyResult := C.Go_PyRun_SimpleString(cCode)
	if pyResult != 0 {
		return "", fmt.Errorf("failed to execute Python code")
	}

	// Get the main module's globals
	pyMain := C.Go_PyImport_AddModule(C.CString("__main__"))
	if pyMain == nil {
		return "", fmt.Errorf("failed to get main module")
	}

	pyGlobals := C.Go_PyModule_GetDict(pyMain)
	if pyGlobals == nil {
		return "", fmt.Errorf("failed to get Python globals")
	}

	// Get the 'result' variable from globals
	pyResultVar := C.Go_PyDict_GetItemString(pyGlobals, C.CString("result"))
	if pyResultVar == nil {
		return "", fmt.Errorf("failed to get result from Python")
	}

	// Convert Python string to Go string
	cResult := C.PyUnicode_AsGoString(pyResultVar)
	if cResult == nil {
		return "", fmt.Errorf("failed to convert Python result to string")
	}

	result := C.GoString(cResult)
	//fmt.Printf("DEBUG: Raw Python result: %q\n", result)
	return result, nil
}

// RenderChatTemplate renders a chat template using the Python function (full-featured)
func (w *ChatTemplateCGoWrapper) RenderChatTemplate(req ChatTemplateRequest) (*ChatTemplateResponse, error) {
	if !w.initialized {
		if err := w.Initialize(); err != nil {
			return nil, fmt.Errorf("failed to initialize Python: %w", err)
		}
	}

	// Convert request to JSON and then base64 encode it
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	reqB64 := base64.StdEncoding.EncodeToString(reqJSON)

	// Create Python code (parse base64, call render_jinja_template, print JSON)
	pythonCode := fmt.Sprintf(`
import sys
import os
import json
import base64
import __main__
__main__.result = 'hello'
sys.path.insert(0, os.getcwd())
try:
    from chat_template_wrapper import render_jinja_template
    request = json.loads(base64.b64decode('''%s''').decode('utf-8'))
    rendered_chats, generation_indices = render_jinja_template(
        conversations=request['conversations'],
        tools=request.get('tools'),
        documents=request.get('documents'),
        chat_template=request['chat_template'],
        return_assistant_tokens_mask=request.get('return_assistant_tokens_mask', False),
        continue_final_message=request.get('continue_final_message', False),
        add_generation_prompt=request.get('add_generation_prompt', False),
        template_vars=request.get('template_vars')
    )
    response = {
        'rendered_chats': rendered_chats,
        'generation_indices': generation_indices
    }
    __main__.result = json.dumps(response)
except Exception as e:
    import traceback
    error_msg = 'PYTHON_EXCEPTION:' + str(e) + '\\n' + traceback.format_exc()
    __main__.result = error_msg
`, reqB64)

	// Execute Python code and get result (JSON string)
	resultJSON, err := w.executePythonCode(pythonCode)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Python code: %w", err)
	}

	// If Python returned an exception, print it and return an error
	if len(resultJSON) > 18 && resultJSON[:18] == "PYTHON_EXCEPTION:" {
		fmt.Println(resultJSON)
		return nil, fmt.Errorf("python exception: %s", resultJSON[18:])
	}

	// Parse the response
	var response ChatTemplateResponse
	if err := json.Unmarshal([]byte(resultJSON), &response); err != nil {
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

// GetModelChatTemplate fetches the chat template string for a model from Python
func (w *ChatTemplateCGoWrapper) GetModelChatTemplate(req GetChatTemplateRequest) (string, map[string]interface{}, error) {
	if !w.initialized {
		if err := w.Initialize(); err != nil {
			return "", nil, fmt.Errorf("failed to initialize Python: %w", err)
		}
	}

	// Marshal request to JSON and base64 encode
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	reqB64 := base64.StdEncoding.EncodeToString(reqJSON)

	// Python code to call get_model_chat_template
	pythonCode := fmt.Sprintf(`
import sys
import os
import json
import base64
import __main__
sys.path.insert(0, os.getcwd())
try:
    from chat_template_wrapper import get_model_chat_template
    request = json.loads(base64.b64decode('''%s''').decode('utf-8'))
    result_dict = get_model_chat_template(
        model_name=request['model_name'],
        chat_template=request.get('chat_template'),
        tools=request.get('tools'),
        revision=request.get('revision'),
        token=request.get('token')
    )
    __main__.result = json.dumps(result_dict)
except Exception as e:
    import traceback
    error_msg = 'PYTHON_EXCEPTION:' + str(e) + '\\n' + traceback.format_exc()
    __main__.result = error_msg
`, reqB64)

	result, err := w.executePythonCode(pythonCode)
	if err != nil {
		return "", nil, fmt.Errorf("failed to execute Python code: %w", err)
	}

	// Check if the result starts with our exception prefix
	if len(result) > 18 && result[:18] == "PYTHON_EXCEPTION:" {
		fmt.Println("DEBUG: Python exception detected:", result)
		return "", nil, fmt.Errorf("python exception: %s", result[18:])
	}

	// Also check for any error-like output that might not have the prefix
	if len(result) > 0 && (result[0] == 'P' || result[0] == 'E' || result[0] == 'T') {
		fmt.Println("DEBUG: Potential error output:", result)
		return "", nil, fmt.Errorf("python error: %s", result)
	}

	// Parse the response
	var resp GetModelChatTemplateResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Template, resp.TemplateVars, nil
}
