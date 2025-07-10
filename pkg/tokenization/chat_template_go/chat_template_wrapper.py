#!/usr/bin/env python3
"""
Standalone wrapper for render_jinja_template function from transformers.
This can be easily called from Go or other languages.
"""

import inspect
import json
import re
import types
from contextlib import contextmanager
from datetime import datetime
from functools import lru_cache
from inspect import isfunction
from typing import Any, Callable, Optional, Union, get_args, get_origin, get_type_hints
import logging

from packaging import version

# Check if jinja2 is available
try:
    import jinja2
    from jinja2.ext import Extension
    from jinja2.sandbox import ImmutableSandboxedEnvironment
    JINJA_AVAILABLE = True
except ImportError:
    jinja2 = None
    JINJA_AVAILABLE = False

# Check if torch is available
try:
    from torch import Tensor
    TORCH_AVAILABLE = True
except ImportError:
    TORCH_AVAILABLE = False

# Check if PIL is available
try:
    from PIL.Image import Image
    VISION_AVAILABLE = True
except ImportError:
    VISION_AVAILABLE = False

# Basic logging setup
logger = logging.getLogger(__name__)

BASIC_TYPES = (int, float, str, bool, Any, type(None), ...)
# Extracts the initial segment of the docstring, containing the function description
description_re = re.compile(r"^(.*?)[\n\s]*(Args:|Returns:|Raises:|\Z)", re.DOTALL)
# Extracts the Args: block from the docstring
args_re = re.compile(r"\n\s*Args:\n\s*(.*?)[\n\s]*(Returns:|Raises:|\Z)", re.DOTALL)
# Splits the Args: block into individual arguments
args_split_re = re.compile(
    r"""
(?:^|\n)  # Match the start of the args block, or a newline
\s*(\w+):\s*  # Capture the argument name and strip spacing
(.*?)\s*  # Capture the argument description, which can span multiple lines, and strip trailing spacing
(?=\n\s*\w+:|\Z)  # Stop when you hit the next argument or the end of the block
""",
    re.DOTALL | re.VERBOSE,
)
# Extracts the Returns: block from the docstring, if present. Note that most chat templates ignore the return type/doc!
returns_re = re.compile(r"\n\s*Returns:\n\s*(.*?)[\n\s]*(Raises:|\Z)", re.DOTALL)


class TypeHintParsingException(Exception):
    """Exception raised for errors in parsing type hints to generate JSON schemas"""
    pass


class DocstringParsingException(Exception):
    """Exception raised for errors in parsing docstrings to generate JSON schemas"""
    pass


def _get_json_schema_type(param_type: str) -> dict[str, str]:
    type_mapping = {
        int: {"type": "integer"},
        float: {"type": "number"},
        str: {"type": "string"},
        bool: {"type": "boolean"},
        type(None): {"type": "null"},
        Any: {},
    }
    if VISION_AVAILABLE:
        type_mapping[Image] = {"type": "image"}
    if TORCH_AVAILABLE:
        type_mapping[Tensor] = {"type": "audio"}
    return type_mapping.get(param_type, {"type": "object"})


def _parse_type_hint(hint: str) -> dict:
    origin = get_origin(hint)
    args = get_args(hint)

    if origin is None:
        try:
            return _get_json_schema_type(hint)
        except KeyError:
            raise TypeHintParsingException(
                "Couldn't parse this type hint, likely due to a custom class or object: ", hint
            )

    elif origin is Union or (hasattr(types, "UnionType") and origin is types.UnionType):
        # Recurse into each of the subtypes in the Union, except None, which is handled separately at the end
        subtypes = [_parse_type_hint(t) for t in args if t is not type(None)]
        if len(subtypes) == 1:
            # A single non-null type can be expressed directly
            return_dict = subtypes[0]
        elif all(isinstance(subtype["type"], str) for subtype in subtypes):
            # A union of basic types can be expressed as a list in the schema
            return_dict = {"type": sorted([subtype["type"] for subtype in subtypes])}
        else:
            # A union of more complex types requires "anyOf"
            return_dict = {"anyOf": subtypes}
        if type(None) in args:
            return_dict["nullable"] = True
        return return_dict

    elif origin is list:
        if not args:
            return {"type": "array"}
        else:
            # Lists can only have a single type argument, so recurse into it
            return {"type": "array", "items": _parse_type_hint(args[0])}

    elif origin is tuple:
        if not args:
            return {"type": "array"}
        if len(args) == 1:
            raise TypeHintParsingException(
                f"The type hint {str(hint).replace('typing.', '')} is a Tuple with a single element, which "
                "we do not automatically convert to JSON schema as it is rarely necessary. If this input can contain "
                "more than one element, we recommend "
                "using a list[] type instead, or if it really is a single element, remove the tuple[] wrapper and just "
                "pass the element directly."
            )
        if ... in args:
            raise TypeHintParsingException(
                "Conversion of '...' is not supported in Tuple type hints. "
                "Use list[] types for variable-length"
                " inputs instead."
            )
        return {"type": "array", "prefixItems": [_parse_type_hint(t) for t in args]}

    elif origin is dict:
        # The JSON equivalent to a dict is 'object', which mandates that all keys are strings
        # However, we can specify the type of the dict values with "additionalProperties"
        out = {"type": "object"}
        if len(args) == 2:
            out["additionalProperties"] = _parse_type_hint(args[1])
        return out

    raise TypeHintParsingException("Couldn't parse this type hint, likely due to a custom class or object: ", hint)


def _convert_type_hints_to_json_schema(func: Callable) -> dict:
    type_hints = get_type_hints(func)
    signature = inspect.signature(func)
    required = []
    for param_name, param in signature.parameters.items():
        if param.annotation == inspect.Parameter.empty:
            raise TypeHintParsingException(f"Argument {param.name} is missing a type hint in function {func.__name__}")
        if param.default == inspect.Parameter.empty:
            required.append(param_name)

    properties = {}
    for param_name, param_type in type_hints.items():
        properties[param_name] = _parse_type_hint(param_type)

    schema = {"type": "object", "properties": properties}
    if required:
        schema["required"] = required

    return schema


def parse_google_format_docstring(docstring: str) -> tuple[Optional[str], Optional[dict], Optional[str]]:
    """
    Parses a Google-style docstring to extract the function description,
    argument descriptions, and return description.

    Args:
        docstring (str): The docstring to parse.

    Returns:
        The function description, arguments, and return description.
    """

    # Extract the sections
    description_match = description_re.search(docstring)
    args_match = args_re.search(docstring)
    returns_match = returns_re.search(docstring)

    # Clean and store the sections
    description = description_match.group(1).strip() if description_match else None
    docstring_args = args_match.group(1).strip() if args_match else None
    returns = returns_match.group(1).strip() if returns_match else None

    # Parse the arguments section
    args_dict = {}
    if docstring_args:
        # Split the args block into individual arguments
        args_matches = args_split_re.findall(docstring_args)
        for arg_name, arg_desc in args_matches:
            args_dict[arg_name] = arg_desc.strip()

    return description, args_dict, returns


def get_json_schema(func: Callable) -> dict:
    """
    Converts a function with type hints and a Google-style docstring into a JSON schema.

    Args:
        func (Callable): The function to convert.

    Returns:
        dict: The JSON schema representation of the function.
    """
    # Get the function's docstring
    docstring = func.__doc__
    if not docstring:
        raise DocstringParsingException(f"Function {func.__name__} has no docstring!")

    # Parse the docstring
    main_doc, param_descriptions, return_dict = parse_google_format_docstring(docstring)
    if not main_doc:
        raise DocstringParsingException(f"Function {func.__name__} has no description in its docstring!")

    # Convert type hints to JSON schema
    try:
        json_schema = _convert_type_hints_to_json_schema(func)
    except TypeHintParsingException as e:
        raise DocstringParsingException(
            f"Cannot generate JSON schema for {func.__name__} due to type hint parsing error: {e}"
        )

    # Add descriptions to the schema
    for arg, schema in json_schema["properties"].items():
        if arg not in param_descriptions:
            raise DocstringParsingException(
                f"Cannot generate JSON schema for {func.__name__} because the docstring has no description for the argument '{arg}'"
            )
        desc = param_descriptions[arg]
        enum_choices = re.search(r"\(choices:\s*(.*?)\)\s*$", desc, flags=re.IGNORECASE)
        if enum_choices:
            schema["enum"] = [c.strip() for c in json.loads(enum_choices.group(1))]
            desc = enum_choices.string[: enum_choices.start()].strip()
        schema["description"] = desc

    output = {"name": func.__name__, "description": main_doc, "parameters": json_schema}
    if return_dict is not None:
        output["return"] = return_dict
    return {"type": "function", "function": output}


def _render_with_assistant_indices(
    compiled_template, messages, tools, documents, add_generation_prompt, **template_kwargs
):
    rendered_blocks = []
    generation_indices = []
    with compiled_template.environment.activate_tracker(rendered_blocks, generation_indices):
        for block in compiled_template.generate(
            messages=messages,
            tools=tools,
            documents=documents,
            add_generation_prompt=add_generation_prompt,
            **template_kwargs,
        ):
            rendered_blocks.append(block)
        rendered_chat = "".join(rendered_blocks)
    return rendered_chat, generation_indices


@lru_cache
def _compile_jinja_template(chat_template):
    if not JINJA_AVAILABLE:
        raise ImportError(
            "apply_chat_template requires jinja2 to be installed. Please install it using `pip install jinja2`."
        )

    class AssistantTracker(Extension):
        # This extension is used to track the indices of assistant-generated tokens in the rendered chat
        tags = {"generation"}

        def __init__(self, environment: ImmutableSandboxedEnvironment):
            # The class is only initiated by jinja.
            super().__init__(environment)
            environment.extend(activate_tracker=self.activate_tracker)
            self._rendered_blocks = None
            self._generation_indices = None

        def parse(self, parser: jinja2.parser.Parser) -> jinja2.nodes.CallBlock:
            lineno = next(parser.stream).lineno
            body = parser.parse_statements(["name:endgeneration"], drop_needle=True)
            return jinja2.nodes.CallBlock(self.call_method("_generation_support"), [], [], body).set_lineno(lineno)

        @jinja2.pass_eval_context
        def _generation_support(self, context: jinja2.nodes.EvalContext, caller: jinja2.runtime.Macro) -> str:
            rv = caller()
            if self.is_active():
                # Only track generation indices if the tracker is active
                start_index = len("".join(self._rendered_blocks))
                end_index = start_index + len(rv)
                self._generation_indices.append((start_index, end_index))
            return rv

        def is_active(self) -> bool:
            return self._rendered_blocks or self._generation_indices

        @contextmanager
        def activate_tracker(self, rendered_blocks: list[int], generation_indices: list[int]):
            try:
                if self.is_active():
                    raise ValueError("AssistantTracker should not be reused before closed")
                self._rendered_blocks = rendered_blocks
                self._generation_indices = generation_indices

                yield
            finally:
                self._rendered_blocks = None
                self._generation_indices = None

    if version.parse(jinja2.__version__) < version.parse("3.1.0"):
        raise ImportError(
            f"apply_chat_template requires jinja2>=3.1.0 to be installed. Your version is {jinja2.__version__}."
        )

    def raise_exception(message):
        raise jinja2.exceptions.TemplateError(message)

    def tojson(x, ensure_ascii=False, indent=None, separators=None, sort_keys=False):
        # We override the built-in tojson filter because Jinja's default filter escapes HTML characters
        # We also expose some options like custom indents and separators
        return json.dumps(x, ensure_ascii=ensure_ascii, indent=indent, separators=separators, sort_keys=sort_keys)

    def strftime_now(format):
        return datetime.now().strftime(format)

    jinja_env = ImmutableSandboxedEnvironment(
        trim_blocks=True, lstrip_blocks=True, extensions=[AssistantTracker, jinja2.ext.loopcontrols]
    )
    jinja_env.filters["tojson"] = tojson
    jinja_env.globals["raise_exception"] = raise_exception
    jinja_env.globals["strftime_now"] = strftime_now
    return jinja_env.from_string(chat_template)


def render_jinja_template(
    conversations: list[list[dict[str, str]]],
    tools: Optional[list[Union[dict, Callable]]] = None,
    documents: Optional[list[dict[str, str]]] = None,
    chat_template: Optional[str] = None,
    return_assistant_tokens_mask: Optional[bool] = False,
    continue_final_message: Optional[bool] = False,
    add_generation_prompt: Optional[bool] = False,
    **kwargs,
) -> tuple[list[str], list[list[tuple[int, int]]]]:
    """
    Render chat conversations using a Jinja2 template.
    
    Args:
        conversations: List of conversations, where each conversation is a list of message dicts
        tools: Optional list of tool schemas or callable functions
        documents: Optional list of document dicts with 'title' and 'text' keys
        chat_template: The Jinja2 template string to use for rendering
        return_assistant_tokens_mask: Whether to return assistant token indices
        continue_final_message: Whether to continue from the final message
        add_generation_prompt: Whether to add a generation prompt
        **kwargs: Additional template variables
        
    Returns:
        Tuple of (rendered_chats, generation_indices)
    """
    if return_assistant_tokens_mask and not re.search(r"\{\%-?\s*generation\s*-?\%\}", chat_template):
        logger.warning(
            "return_assistant_tokens_mask==True but chat template does not contain `{% generation %}` keyword."
        )

    # Unpack template_vars if present
    template_vars = kwargs.pop("template_vars", None)
    if template_vars:
        kwargs.update(template_vars)

    # Compilation function uses a cache to avoid recompiling the same template
    compiled_template = _compile_jinja_template(chat_template)

    # We accept either JSON schemas or functions for tools. If we get functions, we convert them to schemas
    if tools is not None:
        tool_schemas = []
        for tool in tools:
            if isinstance(tool, dict):
                tool_schemas.append(tool)
            elif isfunction(tool):
                tool_schemas.append(get_json_schema(tool))
            else:
                raise ValueError(
                    "Tools should either be a JSON schema, or a callable function with type hints "
                    "and a docstring suitable for auto-conversion to a schema."
                )
    else:
        tool_schemas = None

    if documents is not None:
        for document in documents:
            if not isinstance(document, dict):
                raise TypeError("Documents should be a list of dicts with 'title' and 'text' keys!")

    rendered = []
    all_generation_indices = []
    for chat in conversations:
        if hasattr(chat, "messages"):
            # Indicates it's a Conversation object
            chat = chat.messages
        if return_assistant_tokens_mask:
            rendered_chat, generation_indices = _render_with_assistant_indices(
                compiled_template=compiled_template,
                messages=chat,
                tools=tool_schemas,
                documents=documents,
                add_generation_prompt=add_generation_prompt,
                **kwargs,
            )
            all_generation_indices.append(generation_indices)
        else:
            rendered_chat = compiled_template.render(
                messages=chat,
                tools=tool_schemas,
                documents=documents,
                add_generation_prompt=add_generation_prompt,
                **kwargs,
            )
        if continue_final_message:
            final_message = chat[-1]["content"]
            if isinstance(final_message, (list, tuple)):
                for content_block in reversed(final_message):
                    if "text" in content_block:
                        # Pick the last text block in the message (the first one we hit while iterating in reverse)
                        final_message = content_block["text"]
                        break
                else:
                    raise ValueError(
                        "continue_final_message is set but we could not find any text to continuein the final message!"
                    )
            if final_message.strip() not in rendered_chat:
                raise ValueError(
                    "continue_final_message is set but the final message does not appear in the chat after "
                    "applying the chat template! This can happen if the chat template deletes portions of "
                    "the final message. Please verify the chat template and final message in your chat to "
                    "ensure they are compatible."
                )
            final_msg_loc = rendered_chat.rindex(final_message.strip())
            if rendered_chat[final_msg_loc : final_msg_loc + len(final_message.lstrip())] == final_message:
                # The template preserves spacing or the message doesn't have trailing spacing, so things are simple
                rendered_chat = rendered_chat[: final_msg_loc + len(final_message.lstrip())]
            else:
                # The message has trailing spacing that was trimmed, so we must be more cautious
                rendered_chat = rendered_chat[: final_msg_loc + len(final_message.strip())]
        rendered.append(rendered_chat)

    return rendered, all_generation_indices


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
    import sys
    
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