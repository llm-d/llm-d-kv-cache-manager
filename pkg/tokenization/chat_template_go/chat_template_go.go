package chattemplatego

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ChatTemplateWrapper wraps the Python render_jinja_template function
type ChatTemplateWrapper struct {
	PythonScript string
}

// NewChatTemplateWrapper creates a new wrapper
func NewChatTemplateWrapper(pythonScript string) *ChatTemplateWrapper {
	return &ChatTemplateWrapper{
		PythonScript: pythonScript,
	}
}

// RenderChatTemplate renders a chat template using the Python function
func (w *ChatTemplateWrapper) RenderChatTemplate(req ChatTemplateRequest) (*ChatTemplateResponse, error) {
	// Convert request to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create Python script that imports and calls the function
	pythonCode := fmt.Sprintf(`
import sys
import json
from chat_template_wrapper import render_jinja_template

# Parse the request from stdin
request = json.loads(sys.stdin.read())

# Call the function
rendered_chats, generation_indices = render_jinja_template(
    conversations=request['conversations'],
    tools=request.get('tools'),
    documents=request.get('documents'),
    chat_template=request['chat_template'],
    return_assistant_tokens_mask=request.get('return_assistant_tokens_mask', False),
    continue_final_message=request.get('continue_final_message', False),
    add_generation_prompt=request.get('add_generation_prompt', False)
)

# Return the result as JSON
response = {
    'rendered_chats': rendered_chats,
    'generation_indices': generation_indices
}
print(json.dumps(response))
`)

	// Execute Python script
	cmd := exec.Command("python3", "-c", pythonCode)
	cmd.Stdin = strings.NewReader(string(reqJSON))

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute Python script: %w", err)
	}

	// Parse the response
	var response ChatTemplateResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// RenderChatTemplateSimple is a simpler version that calls the Python script directly
func RenderChatTemplateSimple(conversation []ChatMessage, chatTemplate string) (string, error) {
	// Convert conversation to JSON
	convJSON, err := json.Marshal(conversation)
	if err != nil {
		return "", err
	}

	// Call the Python script
	cmd := exec.Command("python3", "chat_template_wrapper.py", chatTemplate, string(convJSON))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// The script prints the rendered chat to stdout
	// We need to extract just the rendered chat part, not the "Rendered chat:" label
	outputStr := strings.TrimSpace(string(output))

	// Split by lines and find the actual rendered content
	lines := strings.Split(outputStr, "\n")
	for i, line := range lines {
		if line == "Rendered chat:" && i+1 < len(lines) {
			// Return everything after "Rendered chat:" line
			return strings.Join(lines[i+1:], "\n"), nil
		}
	}

	// If we can't find the "Rendered chat:" line, return the whole output
	return outputStr, nil
}
