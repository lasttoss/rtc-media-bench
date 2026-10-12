{{- define "sfu.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sfu.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "sfu.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sfu.labels" -}}
app.kubernetes.io/name: {{ include "sfu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "sfu.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sfu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* The SFU's own flags, derived from the values. Kept in one place so the deployment, the lab and
     the runbook cannot disagree about what was configured. */}}
{{- define "sfu.args" -}}
- -addr=:{{ .Values.service.wsPort }}
{{- if eq .Values.ice.mode "mux" }}
- -udp-mux-port={{ .Values.ice.muxPort }}
{{- else }}
- -udp-port-min={{ .Values.ice.portMin }}
- -udp-port-max={{ .Values.ice.portMax }}
{{- end }}
{{- if .Values.ice.tcpFallback }}
- -ice-tcp-port={{ .Values.ice.tcpPort }}
{{- end }}
{{- if eq .Values.advertise.strategy "explicit" }}
- -advertise-ip={{ .Values.advertise.advertiseIP }}
{{- else if eq .Values.advertise.strategy "nodeIP" }}
- -advertise-ip=$(NODE_IP)
{{- end }}
{{- end -}}
