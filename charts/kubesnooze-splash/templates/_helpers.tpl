{{- define "kubesnooze-splash.name" -}}
kubesnooze-splash
{{- end -}}

{{- define "kubesnooze-splash.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "kubesnooze-splash.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubesnooze-splash.labels" -}}
app.kubernetes.io/name: {{ include "kubesnooze-splash.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: kubesnooze
{{- end -}}

{{- define "kubesnooze-splash.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubesnooze-splash.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
