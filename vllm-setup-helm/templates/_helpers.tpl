{{/* vim: set filetype=gotpl: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "chart.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "chart.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "chart.labels" -}}
helm.sh/chart: {{ include "chart.chart" . }}
{{ include "chart.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels for VLLM deployment
*/}}
{{- define "chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
# Add model label for specific selection if needed
app.kubernetes.io/model: {{ .Values.vllm.model.label | quote }}
{{- end }}

{{/*
Selector labels for Redis deployment
*/}}
{{- define "chart.redisSelectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}-redis
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: redis-lookup-server
{{- end }}

{{/*
Return the proper VLLM image name
*/}}
{{- define "chart.vllmImage" -}}
{{- $tag := default .Chart.AppVersion .Values.vllm.image.tag -}}
{{- printf "%s:%s" .Values.vllm.image.repository $tag -}}
{{- end -}}

{{/*
Return the proper Redis image name
*/}}
{{- define "chart.redisImage" -}}
{{- printf "%s:%s" .Values.lmcache.redis.image.repository .Values.lmcache.redis.image.tag -}}
{{- end -}}

{{/*
Generate secret key name
*/}}
{{- define "chart.secretKeyName" -}}
{{- printf "%s_%s" .Values.secret.keyPrefix .Values.vllm.model.label -}}
{{- end -}}

{{/*
Generate Redis Service URL (fully qualified)
*/}}
{{- define "chart.redisServiceUrl" -}}
{{- $serviceName := printf "%s-%s" .Release.Name .Values.lmcache.redis.service.nameSuffix -}}
{{- printf "%s.%s.svc.cluster.local:%v" $serviceName .Release.Namespace .Values.lmcache.redis.service.port -}}
{{- end -}}

{{/*
Generate KV Cache Manager Service URL (fully qualified)
*/}}
{{- define "chart.kvCacheManagerServiceUrl" -}}
{{- $serviceName := printf "%s-kv-cache-manager" .Release.Name -}}
{{- printf "tcp://%s.%s.svc.cluster.local:%v" $serviceName .Release.Namespace .Values.kvCacheManager.service.port -}}
{{- end -}}

{{/*
Generate PVC name
*/}}
{{- define "chart.pvcName" -}}
{{- printf "%s-%s-storage-claim" (include "chart.fullname" .) .Values.vllm.model.label | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Renders imagePullSecrets block
*/}}
{{- define "chart.imagePullSecrets" -}}
{{- $secrets := .componentSecrets | default .globalSecrets -}}
{{- if $secrets -}}
imagePullSecrets:
{{- toYaml $secrets | nindent 2 }}
{{- end -}}
{{- end -}}
