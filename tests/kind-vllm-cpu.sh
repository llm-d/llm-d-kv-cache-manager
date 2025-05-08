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

#!/bin/bash
set -euo pipefail

# ----------------------------------------
# Variables
# ----------------------------------------
CLUSTER_NAME="ai"
KIND_CONFIG="kind-config.yaml"
VLLM_IMAGE="public.ecr.aws/q9t5s3a7/vllm-cpu-release-repo:v0.8.0"
KGATEWAY_IMAGE="cr.kgateway.dev/kgateway-dev/envoy-wrapper:v2.0.0"
METALLB_VERSION="v0.14.9"
INFERENCE_VERSION="v0.3.0"
KGTW_VERSION="v2.0.0"

# ----------------------------------------
# Step 1: Create Kind Cluster
# ----------------------------------------
echo "🛠️  Creating Kind cluster..."
kind delete cluster --name "$CLUSTER_NAME"
kind create cluster --name "$CLUSTER_NAME" --config "$KIND_CONFIG"

echo "📦  Loading vLLM Docker image..."
kind load docker-image "$VLLM_IMAGE" --name="$CLUSTER_NAME"

# ----------------------------------------
# Step 2: Install MetalLB
# ----------------------------------------
echo "🌐  Installing MetalLB..."
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/${METALLB_VERSION}/config/manifests/metallb-native.yaml
echo "⏳  Waiting for MetalLB pods to be ready..."
kubectl wait --namespace metallb-system \
  --for=condition=Ready pod \
  --selector=component=controller \
  --timeout=120s

kubectl wait --namespace metallb-system \
  --for=condition=Ready pod \
  --selector=component=speaker \
  --timeout=120s

echo "⚙️  Applying MetalLB config..."
kubectl apply -f metalb-config.yaml

# ----------------------------------------
# Step 3: Deploy vLLM on CPU
# ----------------------------------------
echo "🧠  Deploying vLLM CPU model..."
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/raw/main/config/manifests/vllm/cpu-deployment.yaml

# ----------------------------------------
# Step 4: Deploy Inference API Components
# ----------------------------------------
echo "📡  Installing Inference API..."
kubectl apply -f "https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/download/${INFERENCE_VERSION}/manifests.yaml"

kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/raw/main/config/manifests/inferencemodel.yaml
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/raw/main/config/manifests/inferencepool-resources.yaml

# ----------------------------------------
# Step 5: Install Kgateway
# ----------------------------------------
echo "🚪  Installing Kgateway..."
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
helm upgrade -i --create-namespace --namespace kgateway-system --version "$KGTW_VERSION" kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds
helm upgrade -i --namespace kgateway-system --version "$KGTW_VERSION" kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --set inferenceExtension.enabled=true

# ----------------------------------------
# Step 6: Apply Gateway and Routes
# ----------------------------------------
echo "📨  Applying Gateway and HTTPRoute..."
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/raw/main/config/manifests/gateway/kgateway/gateway.yaml
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/raw/main/config/manifests/gateway/kgateway/httproute.yaml

echo "📨  Wait Gateway to be ready..."
# sleep 30  # Give time for pod to create
# kubectl wait --for=condition=Ready pod --selector=app.kubernetes.io/instance=inference-gateway --timeout=240s
# Wait up to 2 minutes for the Gateway to get an IP
for i in {1..24}; do
  IP=$(kubectl get gateway inference-gateway -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || echo "")
  if [[ -n "$IP" ]]; then
    echo "✅  Gateway IP assigned: $IP"
    break
  fi
  echo "⏳  Still waiting for Gateway IP..."
  sleep 5
done

if [[ -z "$IP" ]]; then
  echo "❌  Timed out waiting for Gateway IP."
  exit 1
fi
# ----------------------------------------
# Step 7: Run Inference Request
# ----------------------------------------
echo "🔍  Fetching Gateway IP..."
sleep 5  # Give time for IP allocation
IP=$(kubectl get gateway/inference-gateway -o jsonpath='{.status.addresses[0].value}')
PORT=80

echo "📨  Sending test inference request to $IP:$PORT..."
curl -si "${IP}:${PORT}/v1/completions" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Qwen/Qwen2.5-1.5B-Instruct",
    "prompt": "Write as if you were a critic: San Francisco",
    "max_tokens": 100,
    "temperature": 0
  }'



