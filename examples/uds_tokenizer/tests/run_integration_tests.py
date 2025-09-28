#!/usr/bin/env python3
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

"""
Integration test runner that waits for the server to be ready before running tests.
"""

import asyncio
import aiohttp
import json
import os
import sys
import time


async def wait_for_server(max_wait_time=60):
    """Wait for the server to be ready by checking the health endpoint."""
    uds_path = "/tmp/tokenizer/tokenizer-uds.socket"
    
    # Wait for the socket file to be created
    start_time = time.time()
    while not os.path.exists(uds_path):
        if time.time() - start_time > max_wait_time:
            print(f"Server did not start within {max_wait_time} seconds")
            return False
        print("Waiting for server to start...")
        await asyncio.sleep(1)
    
    # Create connector using Unix Domain Socket
    connector = aiohttp.UnixConnector(path=uds_path)
    
    # Wait for the server to respond to health checks
    async with aiohttp.ClientSession(connector=connector) as session:
        while time.time() - start_time < max_wait_time:
            try:
                async with session.get("http://localhost/health") as response:
                    if response.status == 200:
                        data = await response.json()
                        if data.get("status") == "healthy":
                            print("Server is ready!")
                            return True
            except Exception as e:
                pass  # Server might not be ready yet
            print("Waiting for server to be ready...")
            await asyncio.sleep(1)
    
    print(f"Server did not become ready within {max_wait_time} seconds")
    return False


async def run_tests():
    """Run the integration tests."""
    # Unix Domain Socket path
    uds_path = "/tmp/tokenizer/tokenizer-uds.socket"
    
    # Create connector using Unix Domain Socket (for main functionality endpoints)
    connector = aiohttp.UnixConnector(path=uds_path)
    
    # Create session for UDS endpoints
    async with aiohttp.ClientSession(connector=connector) as uds_session:
        print("=== Testing All Endpoints ===")
        
        # 1. Test config retrieval endpoint
        print("\n1. Testing config retrieval...")
        try:
            async with uds_session.get("http://localhost/config") as response:
                config_data = await response.json()
                print(f"Current config: {config_data}")
                print(f"Status code: {response.status}")
        except Exception as e:
            print(f"Error getting config: {e}")
            return False
        
        # 2. Test chat template endpoint
        print("\n2. Testing chat template...")
        messages = [
            {
                "role": "system",
                "content": "You are a helpful assistant."
            },
            {
                "role": "user",
                "content": "Hello, how are you?"
            }
        ]
        try:
            async with uds_session.post(
                "http://localhost/chat-template",
                json=messages
            ) as response:
                template_result = await response.text()
                print(f"Chat template result: {template_result[:100]}...")
                print(f"Status code: {response.status}")
        except Exception as e:
            print(f"Error applying chat template: {e}")
            return False
        
        # 3. Test tokenize endpoint
        print("\n3. Testing tokenize...")
        prompt = "Hello, how are you?"
        try:
            async with uds_session.post(
                "http://localhost/tokenize",
                data=prompt
            ) as response:
                tokenize_result = await response.text()
                print(f"Tokenize result: {tokenize_result[:100]}...")
                print(f"Status code: {response.status}")
        except Exception as e:
            print(f"Error tokenizing: {e}")
            return False
        
        # 4. Test config update endpoint
        print("\n4. Testing config update...")
        new_config = {
            "model": "Qwen/Qwen3-8B",  # Using a valid model name
            "add_special_tokens": False
        }
        try:
            async with uds_session.post(
                "http://localhost/config",
                json=new_config
            ) as response:
                update_result = await response.json()
                print(f"Config update result: {update_result}")
                print(f"Status code: {response.status}")
        except Exception as e:
            print(f"Error updating config: {e}")
            return False
    
    # Create separate session for TCP probe endpoint
    async with aiohttp.ClientSession() as tcp_session:
        # 5. Test health check endpoint (via TCP port)
        print("\n5. Testing health check...")
        # Note: Using localhost and probe port (default 8080)
        try:
            async with tcp_session.get("http://localhost:8080/health") as response:
                health_data = await response.json()
                print(f"Health check result: {health_data}")
                print(f"Status code: {response.status}")
        except Exception as e:
            print(f"Error checking health: {e}")
            return False
    
    return True


async def main():
    """Main function."""
    # Wait for server to be ready
    if not await wait_for_server():
        print("Server failed to start or become ready")
        sys.exit(1)
    
    # Run tests
    success = await run_tests()
    
    if success:
        print("\nAll tests passed!")
        sys.exit(0)
    else:
        print("\nSome tests failed!")
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())