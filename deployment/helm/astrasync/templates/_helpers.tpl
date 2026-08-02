{{/*
Expand the name of the chart.
*/}}
{{- define "astrasync.name" .Chart.Name }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "astrasync.fullname" .Chart.Name }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "astrasync.chart" .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}

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
{{- define "astrasync.serviceAccountName" . }}
{{- if .Values.serviceAccount.create }}
{{- default (include "astrasync.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create PostgreSQL URL
*/}}
{{- define "astrasync.postgresql.url" . }}
{{- if .Values.postgresql.enabled }}
postgresql://{{ .Values.postgresql.auth.username }}:{{ .Values.postgresql.auth.password }}@{{ .Release.Name }}-postgresql:{{ .Values.postgresql.primary.service.port }}/{{ .Values.postgresql.auth.database }}
{{- else }}
{{ .Values.apiServer.config.databaseUrl }}
{{- end }}
{{- end }}

{{/*
Create etcd endpoints
*/}}
{{- define "astrasync.etcd.endpoints" . }}
{{- if .Values.etcd.enabled }}
{{ .Release.Name }}-etcd:2379
{{- else }}
{{ .Values.apiServer.config.etcdEndpoints }}
{{- end }}
{{- end }}
