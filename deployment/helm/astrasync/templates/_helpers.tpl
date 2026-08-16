{{/*
Expand the name of the chart.
*/}}
{{- define "astrasync.name" }}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Namespace dedicated to bounded Connection test execution. */}}
{{- define "astrasync.connectionTestExecutorNamespace" -}}
{{- default (printf "%s-connection-tests" .Release.Namespace | trunc 63 | trimSuffix "-") .Values.connectionTestExecutor.namespace -}}
{{- end -}}

{{/* Dedicated tokenless identity for the public Console workload. */}}
{{- define "astrasync.consoleServiceAccountName" -}}
{{- if .Values.console.serviceAccount.create -}}
{{- default (include "astrasync.componentFullname" (dict "context" . "component" "console")) .Values.console.serviceAccount.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- required "console.serviceAccount.name is required when create=false" .Values.console.serviceAccount.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/* Least-privilege identity used only by the Connection test executor. */}}
{{- define "astrasync.connectionTestExecutorServiceAccountName" -}}
{{- if .Values.connectionTestExecutor.serviceAccount.create -}}
{{- default (include "astrasync.componentFullname" (dict "context" . "component" "connection-test")) .Values.connectionTestExecutor.serviceAccount.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- required "connectionTestExecutor.serviceAccount.name is required when create=false" .Values.connectionTestExecutor.serviceAccount.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/* Dedicated identity for the workload-only compiler service. */}}
{{- define "astrasync.compilerValidationServiceAccountName" }}
{{- if .Values.compilerValidation.serviceAccount.create }}
{{- default (include "astrasync.componentFullname" (dict "context" . "component" "compiler-validation")) .Values.compilerValidation.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- required "compilerValidation.serviceAccount.name is required when create=false" .Values.compilerValidation.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "astrasync.fullname" }}
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
Create a component name that preserves its suffix within the 63-character DNS label limit.
*/}}
{{- define "astrasync.componentFullname" }}
{{- $baseLength := sub 62 (len .component) | int }}
{{- $base := include "astrasync.fullname" .context | trunc $baseLength | trimSuffix "-" }}
{{- printf "%s-%s" $base .component }}
{{- end }}

{{/*
Worker name with room for the StatefulSet ordinal in a DNS label.
*/}}
{{- define "astrasync.workerFullname" }}
{{- $component := "worker" }}
{{- $baseLength := sub 57 (len $component) | int }}
{{- $base := include "astrasync.fullname" . | trunc $baseLength | trimSuffix "-" }}
{{- printf "%s-%s" $base $component }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "astrasync.chart" }}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "astrasync.labels" }}
app.kubernetes.io/name: {{ include "astrasync.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ include "astrasync.chart" . }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "astrasync.selectorLabels" }}
app.kubernetes.io/name: {{ include "astrasync.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "astrasync.serviceAccountName" }}
{{- if .Values.serviceAccount.create }}
{{- default (include "astrasync.fullname" .) .Values.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- default "default" .Values.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create PostgreSQL URL
*/}}
{{- define "astrasync.postgresql.url" -}}
{{- if .Values.postgresql.enabled -}}
{{- $port := dig "primary" "service" "ports" "postgresql" 5432 .Values.postgresql -}}
postgresql://{{ .Values.postgresql.auth.username }}:{{ .Values.postgresql.auth.password }}@{{ .Release.Name }}-postgresql:{{ $port }}/{{ .Values.postgresql.auth.database }}
{{- else -}}
{{- .Values.apiServer.config.databaseUrl -}}
{{- end -}}
{{- end -}}

{{/* PostgreSQL URL usable from a workload in another namespace. */}}
{{- define "astrasync.postgresql.crossNamespaceUrl" -}}
{{- if .Values.postgresql.enabled -}}
{{- $port := dig "primary" "service" "ports" "postgresql" 5432 .Values.postgresql -}}
postgresql://{{ .Values.postgresql.auth.username }}:{{ .Values.postgresql.auth.password }}@{{ .Release.Name }}-postgresql.{{ .Release.Namespace }}.svc.cluster.local:{{ $port }}/{{ .Values.postgresql.auth.database }}
{{- else -}}
{{- .Values.apiServer.config.databaseUrl -}}
{{- end -}}
{{- end -}}

{{/*
Persistent claim used by dynamically dispatched Coordinators.
*/}}
{{- define "astrasync.schedulerProgressClaim" -}}
{{- if .Values.scheduler.progress.existingClaim -}}
{{- .Values.scheduler.progress.existingClaim -}}
{{- else -}}
{{- include "astrasync.componentFullname" (dict "context" . "component" "scheduler-progress") -}}
{{- end -}}
{{- end -}}

{{/*
Create etcd endpoints
*/}}
{{- define "astrasync.etcd.endpoints" -}}
{{- if .Values.etcd.enabled -}}
{{- .Release.Name }}-etcd:2379
{{- else -}}
{{- .Values.apiServer.config.etcdEndpoints -}}
{{- end -}}
{{- end -}}

{{/*
Standard metrics containerPort stanza. Emits nothing when the
monitoring.prometheus toggle is off so the helper is fail-closed: a
deployment that does not explicitly opt in cannot accidentally expose
a /metrics endpoint that has not been configured.
*/}}
{{- define "astrasync.metricsContainerPort" -}}
{{- if .Values.monitoring.prometheus.enabled -}}
- name: metrics
  containerPort: {{ .Values.monitoring.prometheus.port }}
  protocol: TCP
{{- end -}}
{{- end -}}

{{/*
Returns the metrics listen address that the Go binaries should bind.
Returns empty when the toggle is off so the binary falls back to its
own default (an empty value, which the binary treats as "do not bind").
*/}}
{{- define "astrasync.metricsListenAddress" -}}
{{- if .Values.monitoring.prometheus.enabled -}}
:{{ .Values.monitoring.prometheus.port }}
{{- end -}}
{{- end -}}
