{{/*
Expand the name of the chart.
*/}}
{{- define "ivaldi-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "ivaldi-agent.fullname" -}}
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
{{- define "ivaldi-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "ivaldi-agent.labels" -}}
helm.sh/chart: {{ include "ivaldi-agent.chart" . }}
{{ include "ivaldi-agent.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "ivaldi-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ivaldi-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The agent's ServiceAccount, Lease, and RBAC objects share the full name.
*/}}
{{- define "ivaldi-agent.serviceAccountName" -}}
{{ include "ivaldi-agent.fullname" . }}
{{- end }}

{{/*
Namespaces the agent reads through RoleBindings when observe.namespaces is
set: those, and the ones it deploys to, which it must also watch.
*/}}
{{- define "ivaldi-agent.observedNamespaces" -}}
{{- concat .Values.observe.namespaces .Values.execute.namespaces | uniq | toJson }}
{{- end }}
