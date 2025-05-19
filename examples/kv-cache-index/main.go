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

package main

import (
	"context"
	"os"
	"time"

	kvcache "github.com/llm-d/llm-d-kv-cache-manager/pkg/kv-cache"

	"k8s.io/klog/v2"
)

/*
Refer to docs/phase1-setup.md

In Redis:
1) "meta-llama/Llama-3.1-8B-Instruct@33c26f4ed679005e733e382beeb8df69d8362c07400bb07fec69712413cb4310"
2) "meta-llama/Llama-3.1-8B-Instruct@0a3fd4e370c8aa0fafea88040e14f08aace073029aeec81a2b3aa8be8b84d6ae"
2) "mistralai/Mistral-7B-Instruct-v0.2@923cdf5f667a7c3e059a1f7b8ed8b7e61d079a1bdceb47196575f4c327a674ae"
3) "mistralai/Mistral-7B-Instruct-v0.2@e59c0c9babc978ec7d1f22510c7c3cae345f49fe88497c49ae598b95ee948313"
*/

//nolint:lll // need prompt as-is, chunking to string concatenation is too much of a hassle
const prompt = `lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst.`
const modelName = "ibm-granite/granite-3.3-8b-instruct"

func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	// For sample running with mistral (tokenizer), a huggingface token is needed
	huggingFaceToken := os.Getenv("HF_TOKEN")
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	return config
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	logger := klog.FromContext(ctx)

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(getKVCacheIndexerConfig())
	if err != nil {
		logger.Error(err, "failed to init Indexer")
	}

	logger.Info("created Indexer")

	go kvCacheIndexer.Run(ctx)
	logger.Info("started Indexer")

	// Get pods for the prompt
	pods, err := kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		logger.Error(err, "failed to get pod scores")
		return
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("got pods", "pods", pods)

	// Sleep 3 secs
	time.Sleep(3 * time.Second)

	// Get pods for the prompt
	pods, err = kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		logger.Error(err, "failed to get pod scores")
		return
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("got pods", "pods", pods)

	// Cancel the context
	cancel()
}
