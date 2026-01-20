# Launch a mongosh session into the MAS MongoDB replica set (namespace: mongoce).
# Requires: oc, jq (or Git for Windows jq), mongosh in PATH, and cluster access.

$ErrorActionPreference = 'Stop'

$Namespace = 'mongoce'
$Instance = 'mas-mongo-ce'

# Get the connection string from the admin secret
$connB64 = (& oc get secret "$Instance-admin-admin" -n $Namespace -o jsonpath='{.data.connectionString\.standard}')
$conn = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($connB64))

# Pick a mongod pod (label app=<instance>-svc)
$pod = (& oc get pod -n $Namespace -l "app=$Instance-svc" -o jsonpath='{.items[0].metadata.name}')
if (-not $pod) {
  # fallback to first StatefulSet pod name
  $fallback = "$Instance-0"
  $exists = (oc get pod -n $Namespace $fallback > $null 2>&1)
  if ($LASTEXITCODE -eq 0) {
    $pod = $fallback
  } else {
    Write-Error "No Mongo pods found with label app=$Instance-svc in namespace $Namespace, and $fallback missing";
    & oc get pods -n $Namespace;
    exit 1
  }
}

Write-Host "Using pod: $pod" -ForegroundColor Cyan
Write-Host "Connection: $conn" -ForegroundColor Cyan

# Open an interactive shell inside the mongod container
oc exec -it -n $Namespace $pod -c mongod `
  mongosh "$conn" --tls --tlsAllowInvalidCertificates
