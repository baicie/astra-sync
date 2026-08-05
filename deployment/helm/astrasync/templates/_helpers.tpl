{{/*
Expand the name of the chart.
*/}}
{{- define "astrasync.name" }}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
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
