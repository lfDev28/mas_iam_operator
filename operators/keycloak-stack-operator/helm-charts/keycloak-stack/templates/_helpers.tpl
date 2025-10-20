{{- define "keycloak-stack.name" -}}
keycloak
{{- end }}

{{- define "keycloak-stack.fullname" -}}
{{ printf "%s-%s" .Release.Name (include "keycloak-stack.name" .) }}
{{- end }}

{{- define "keycloak-stack.labels" -}}
app.kubernetes.io/name: {{ include "keycloak-stack.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "keycloak-stack.selectorLabels" -}}
app.kubernetes.io/name: {{ include "keycloak-stack.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "keycloak-stack.routeHost" -}}
{{- $route := .Values.keycloak.route -}}
{{- if not $route.enabled -}}
{{- "" -}}
{{- else -}}
  {{- $host := default "" $route.host -}}
  {{- $autoHost := $route.autoHost | default dict -}}
  {{- $autoHostEnabled := false -}}
  {{- $autoHostDomain := "" -}}
  {{- if hasKey $autoHost "enabled" -}}
    {{- $autoHostEnabled = $autoHost.enabled -}}
  {{- end -}}
  {{- if hasKey $autoHost "appsDomain" -}}
    {{- $autoHostDomain = $autoHost.appsDomain -}}
  {{- end -}}
  {{- if and (eq $host "") $autoHostEnabled (ne $autoHostDomain "") -}}
    {{- $host = printf "%s-%s.%s" (include "keycloak-stack.fullname" .) .Release.Namespace $autoHostDomain -}}
  {{- end -}}
  {{- $host -}}
{{- end -}}
{{- end -}}

{{- define "keycloak-stack.ldapBaseDN" -}}
{{- $domain := . | default "" | trim -}}
{{- if $domain -}}
  {{- $parts := splitList "." $domain -}}
  {{- $dn := "" -}}
  {{- range $index, $part := $parts -}}
    {{- if eq $index 0 -}}
      {{- $dn = printf "dc=%s" $part -}}
    {{- else -}}
      {{- $dn = printf "%s,dc=%s" $dn $part -}}
    {{- end -}}
  {{- end -}}
  {{- $dn -}}
{{- end -}}
{{- end -}}

{{- define "keycloak-stack.bootstrapAdminSecretName" -}}
{{- $bootstrap := .Values.keycloak.bootstrapAdmin -}}
{{- if $bootstrap.secretName -}}
  {{- $bootstrap.secretName -}}
{{- else if $bootstrap.createSecret -}}
  {{- printf "%s-bootstrap-admin" (include "keycloak-stack.fullname" .) -}}
{{- else -}}
  {{- "" -}}
{{- end -}}
{{- end -}}

{{- define "keycloak-stack.openldapAdminSecretName" -}}
{{- $admin := .Values.openldap.admin -}}
{{- if $admin.secretName -}}
  {{- $admin.secretName -}}
{{- else if $admin.createSecret -}}
  {{- printf "%s-openldap-admin" (include "keycloak-stack.fullname" .) -}}
{{- else -}}
  {{- "" -}}
{{- end -}}
{{- end -}}
