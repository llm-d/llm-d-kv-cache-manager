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

# Gunicorn configuration file
import os
import multiprocessing

# Server configuration
bind = "unix:/tmp/tokenizer/tokenizer-uds.socket"
workers = int(os.getenv("WORKERS", multiprocessing.cpu_count() * 2 + 1))
worker_class = "aiohttp.GunicornWebWorker"
worker_connections = 1000
max_requests = 1000
max_requests_jitter = 100

# Logging configuration
accesslog = "-"
errorlog = "-"
loglevel = os.getenv("LOG_LEVEL", "info").lower()
# Use aiohttp-style log format
access_log_format = '%a %t "%r" %s %b "%{Referer}i" "%{User-Agent}i" %Tf'

# Process naming
proc_name = "tokenizer-service"

# Timeout settings
timeout = 30
graceful_timeout = 30
keepalive = 5

preload_app = True