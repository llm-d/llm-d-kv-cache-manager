#!/bin/bash
# Copyright 2025 The llm-d Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Startup script to run tokenizer service with Gunicorn

# Set environment variables
export LOG_LEVEL=${LOG_LEVEL:-INFO}
export WORKERS=${WORKERS:-4}

# Remove old socket file if it exists
if [ -S "/tmp/tokenizer/tokenizer-uds.socket" ]; then
    rm /tmp/tokenizer/tokenizer-uds.socket
fi

# Create directory
mkdir -p /tmp/tokenizer

# Start service with Gunicorn
exec gunicorn -c gunicorn.conf.py server:create_app_for_gunicorn