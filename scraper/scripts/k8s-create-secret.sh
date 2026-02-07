#!/usr/bin/env bash
set -euo pipefail

NAMESPACE=${NAMESPACE:-default}

if [ ! -f .env ]; then
  echo "Error: .env file not found"
  exit 1
fi

kubectl create secret generic lectures-notifier-secrets \
  --from-env-file=.env \
  --namespace="$NAMESPACE" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Secret created/updated in namespace $NAMESPACE"
kubectl get secret lectures-notifier-secrets -n "$NAMESPACE" -o jsonpath='{.data}' | jq 'keys'
