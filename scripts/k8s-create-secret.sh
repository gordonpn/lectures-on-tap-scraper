#!/usr/bin/env bash
set -euo pipefail

NAMESPACE=${NAMESPACE:-default}
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
ENV_FILE=${ENV_FILE:-"$PROJECT_ROOT/.env"}

if [ ! -f "$ENV_FILE" ]; then
  echo "Error: .env file not found at $ENV_FILE"
  exit 1
fi

kubectl create secret generic lectures-notifier-secrets \
  --from-env-file="$ENV_FILE" \
  --namespace="$NAMESPACE" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Secret created/updated in namespace $NAMESPACE"
kubectl get secret lectures-notifier-secrets -n "$NAMESPACE" -o jsonpath='{.data}' | jq 'keys'
