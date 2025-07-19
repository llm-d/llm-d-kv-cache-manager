#!/usr/bin/env python3
"""
Standalone wrapper for render_jinja_template function from transformers.
This can be easily called from Go or other languages.
"""

import json
import logging
import sys
from typing import Optional, Union

# Import core functions from transformers
try:
    from transformers.utils.chat_template_utils import render_jinja_template, get_json_schema
    TRANSFORMERS_AVAILABLE = True
except ImportError:
    TRANSFORMERS_AVAILABLE = False
    # Fallback: if transformers is not available, we'll provide a minimal implementation
    def render_jinja_template(*args, **kwargs):
        raise ImportError("transformers library is required but not available")
    
    def get_json_schema(*args, **kwargs):
        raise ImportError("transformers library is required but not available")

# Basic logging setup
logger = logging.getLogger(__name__)


def get_model_chat_template(model_name, chat_template=None, tools=None, revision=None, token=None):
    """
    Load a tokenizer from Hugging Face Hub and return its chat template string and required variables.
    Args:
        model_name (str): The model ID or path.
        chat_template (str, optional): The template name or string to use.
        tools (list[dict], optional): Tool schemas to pass.
        revision (str, optional): Model revision.
        token (str, optional): Hugging Face token for private models.
    Returns:
        dict: Dictionary containing 'template' and 'template_vars' keys.
    """
    if not TRANSFORMERS_AVAILABLE:
        raise ImportError("transformers library is required for get_model_chat_template")
    
    from transformers import AutoTokenizer
    tokenizer = AutoTokenizer.from_pretrained(model_name, revision=revision, token=token, trust_remote_code=True)
    template = tokenizer.chat_template if chat_template is None else chat_template
    # Collect special tokens
    template_vars = {}
    for k in ["bos_token", "eos_token", "eot_token", "pad_token", "unk_token", "sep_token", "additional_special_tokens"]:
        v = getattr(tokenizer, k, None)
        if v is not None:
            template_vars[k] = v
    return {"template": template, "template_vars": template_vars}


def main():
    """Example usage and testing function."""
    if not TRANSFORMERS_AVAILABLE:
        print("Error: transformers library is required but not available")
        print("Please install transformers: pip install transformers")
        return
    
    if len(sys.argv) < 2:
        print("Usage: python chat_template_wrapper.py <chat_template> [conversation_json]")
        print("Example:")
        print('python chat_template_wrapper.py "{% for message in messages %}{{ message.role }}: {{ message.content }}\\n{% endfor %}"')
        return
    
    chat_template = sys.argv[1]
    
    # Default conversation if none provided
    conversation = [
        {"role": "user", "content": "Hello!"},
        {"role": "assistant", "content": "Hi there! How can I help you today?"}
    ]
    
    if len(sys.argv) > 2:
        try:
            conversation = json.loads(sys.argv[2])
        except json.JSONDecodeError:
            print("Error: Invalid JSON for conversation")
            return
    
    try:
        rendered, generation_indices = render_jinja_template(
            conversations=[conversation],
            chat_template=chat_template
        )
        print("Rendered chat:")
        print(rendered[0])
        if generation_indices and len(generation_indices) > 0 and generation_indices[0]:
            print(f"Generation indices: {generation_indices[0]}")
    except Exception as e:
        print(f"Error: {e}")


if __name__ == "__main__":
    main() 