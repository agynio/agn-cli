{{- define "agn.configureEnv" -}}
{{- $env := list -}}

{{- $configPath := trimAll " \n\t" (default "" .Values.agn.configPath) -}}
{{- if $configPath -}}
{{- $env = append $env (dict "name" "AGN_CONFIG_PATH" "value" $configPath) -}}
{{- end -}}

{{- $userEnv := .Values.env | default (list) -}}
{{- $_ := set .Values "env" (concat $env $userEnv) -}}
{{- end -}}
