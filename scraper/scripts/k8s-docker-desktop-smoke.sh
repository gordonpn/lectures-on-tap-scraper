#!/usr/bin/env bash
set -euo pipefail

CTX=${CTX:-docker-desktop}

if [ -z "$CTX" ]; then
  echo "No kubectl context set. Provide CTX=<context> or enable Docker Desktop Kubernetes."
  exit 1
fi

if ! kubectl config get-contexts -o name | grep -qx "$CTX"; then
  echo "Context '$CTX' not found. Run 'kubectl config get-contexts' or set CTX=<context>."
  exit 1
fi

NAMESPACE=${NAMESPACE:-default}
IMAGE=${IMAGE:-lectures-notifier:main}

kubectl config use-context "$CTX"

# create secret from .env
if [ ! -f .env ]; then
  echo "Error: .env file not found (needed for secret)"
  exit 1
fi

kubectl create secret generic lectures-notifier-secrets \
  --from-env-file=.env \
  --namespace="$NAMESPACE" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -n "$NAMESPACE" -f k8s/

cronjobs=$(kubectl get cronjob -n "$NAMESPACE" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

if [ -z "$cronjobs" ]; then
  echo "No cronjobs found in namespace $NAMESPACE"
  exit 1
fi

echo "$cronjobs" | while read -r cj; do
  kubectl patch cronjob "$cj" -n "$NAMESPACE" --type merge -p '{"spec":{"schedule":"*/1 * * * *","successfulJobsHistoryLimit":1,"failedJobsHistoryLimit":1}}'
done

CJ_NAME=${CJ_NAME:-$(echo "$cronjobs" | head -n1)}
JOB_NAME=${JOB_NAME:-cj-smoke-$(date +%s)}

kubectl create job -n "$NAMESPACE" --from=cronjob/$CJ_NAME "$JOB_NAME"
kubectl get jobs -n "$NAMESPACE"

# wait for pod readiness to avoid early ContainerCreating errors
if ! kubectl wait -n "$NAMESPACE" --for=condition=Ready pod -l job-name="$JOB_NAME" --timeout=120s; then
  echo "Pods not ready; showing describe for debugging"
  kubectl describe pod -n "$NAMESPACE" -l job-name="$JOB_NAME"
  exit 1
fi

kubectl logs -n "$NAMESPACE" -l job-name="$JOB_NAME" -f --tail=-1
