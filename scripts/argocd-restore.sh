#!/usr/bin/env bash
set -euo pipefail

if kubectl get application agn -n argocd >/dev/null 2>&1; then
  echo "Re-enabling ArgoCD auto-sync for agn..."
  kubectl patch application agn -n argocd \
    --type merge \
    -p '{"spec":{"syncPolicy":{"automated":{"prune":true,"selfHeal":true}}}}'
  echo "ArgoCD auto-sync restored."
fi
