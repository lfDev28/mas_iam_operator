#!/usr/bin/env bash

set -euo pipefail

CATALOG_IMG=${CATALOG_IMG:-quay.io/lee_forster/mas-iam-operator:catalog-0.0.9}
CATALOG_NAME=${CATALOG_NAME:-mas-iam-operator-dev}
CATALOG_NAMESPACE=${CATALOG_NAMESPACE:-openshift-marketplace}
CATALOG_DISPLAY_NAME=${CATALOG_DISPLAY_NAME:-"MAS IAM Operator (dev)"}

TARGET_NAMESPACE=${TARGET_NAMESPACE:-iam}
OPERATOR_GROUP_NAME=${OPERATOR_GROUP_NAME:-iam-operator-group}
SUBSCRIPTION_NAME=${SUBSCRIPTION_NAME:-mas-iam-operator}
PACKAGE_NAME=${PACKAGE_NAME:-mas-iam-operator}
CHANNEL=${CHANNEL:-alpha}
INSTALL_PLAN_APPROVAL=${INSTALL_PLAN_APPROVAL:-Automatic}

RETRY_MAX=${RETRY_MAX:-30}
SLEEP_SECONDS=${SLEEP_SECONDS:-10}

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required to run this script." >&2
  exit 1
fi

echo "Target catalog image: ${CATALOG_IMG}"
echo "Operator will be installed into namespace: ${TARGET_NAMESPACE}"

if ! oc get namespace "${TARGET_NAMESPACE}" >/dev/null 2>&1; then
  echo "Creating namespace ${TARGET_NAMESPACE}"
  oc create namespace "${TARGET_NAMESPACE}"
fi

echo "Cleaning up any existing CatalogSource/Subscription/CSV"
oc delete catalogsource "${CATALOG_NAME}" -n "${CATALOG_NAMESPACE}" --ignore-not-found
oc delete subscription "${SUBSCRIPTION_NAME}" -n "${TARGET_NAMESPACE}" --ignore-not-found

csv_selector="operators.coreos.com/${PACKAGE_NAME}.${TARGET_NAMESPACE}"
existing_csvs=$(oc get csv -n "${TARGET_NAMESPACE}" -l "${csv_selector}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
if [[ -n "${existing_csvs}" ]]; then
  echo "${existing_csvs}" | xargs -r -n1 oc delete csv -n "${TARGET_NAMESPACE}"
fi

echo "Applying CatalogSource ${CATALOG_NAME} in ${CATALOG_NAMESPACE}"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${CATALOG_NAME}
  namespace: ${CATALOG_NAMESPACE}
spec:
  displayName: ${CATALOG_DISPLAY_NAME}
  image: ${CATALOG_IMG}
  sourceType: grpc
EOF

echo "Waiting for CatalogSource to become READY..."
for i in $(seq 1 "${RETRY_MAX}"); do
  state=$(oc get catalogsource "${CATALOG_NAME}" -n "${CATALOG_NAMESPACE}" -o jsonpath='{.status.connectionState.lastObservedState}' 2>/dev/null || true)
  if [[ "${state}" == "READY" ]]; then
    echo "CatalogSource is READY"
    break
  fi
  if [[ "${i}" -eq "${RETRY_MAX}" ]]; then
    echo "error: CatalogSource ${CATALOG_NAME} did not become READY" >&2
    exit 1
  fi
  sleep "${SLEEP_SECONDS}"
done

echo "Applying/Updating OperatorGroup ${OPERATOR_GROUP_NAME} in ${TARGET_NAMESPACE}"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${OPERATOR_GROUP_NAME}
  namespace: ${TARGET_NAMESPACE}
spec:
  targetNamespaces:
  - ${TARGET_NAMESPACE}
EOF

echo "Creating Subscription ${SUBSCRIPTION_NAME}"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ${SUBSCRIPTION_NAME}
  namespace: ${TARGET_NAMESPACE}
spec:
  channel: ${CHANNEL}
  installPlanApproval: ${INSTALL_PLAN_APPROVAL}
  name: ${PACKAGE_NAME}
  source: ${CATALOG_NAME}
  sourceNamespace: ${CATALOG_NAMESPACE}
EOF

label_selector="${csv_selector}"

echo "Waiting for CSV to reach Succeeded state..."
for i in $(seq 1 "${RETRY_MAX}"); do
  csv=$(oc get csv -n "${TARGET_NAMESPACE}" -l "${label_selector}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | head -n1 || true)
  if [[ -n "${csv}" ]]; then
    phase=$(oc get csv "${csv}" -n "${TARGET_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [[ "${phase}" == "Succeeded" ]]; then
      echo "CSV ${csv} is Succeeded"
      break
    fi
  fi
  if [[ "${i}" -eq "${RETRY_MAX}" ]]; then
    echo "error: CSV did not reach Succeeded state in time" >&2
    exit 1
  fi
  sleep "${SLEEP_SECONDS}"
done

echo "Operator installed. Current CSVs:"
oc get csv -n "${TARGET_NAMESPACE}"

echo "Next steps:"
echo "  - Apply a MasIamStack custom resource:"
echo "      oc apply -f operators/mas-iam-operator/config/samples/iam_v1alpha1_masiamstack.yaml"
echo "  - Monitor pods and operator logs as needed."
