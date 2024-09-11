#!/usr/bin/env bash
set -euo pipefail
set -x

ISTIO_V1="1.22.4"
ISTIO_V2="1.23.1"

ISTIO_HELM_REPO="https://istio-release.storage.googleapis.com/charts"

### set up a new cluster

kind create cluster --config kind-config.yaml

sleep 5

kubectl create ns istio-system

sleep 2


### install first istio revision

helm install --wait --namespace istio-system istio-base "$ISTIO_HELM_REPO/base-$ISTIO_V1.tgz" \
  --values istio-base.values.yaml \
  --set revision="v${ISTIO_V1//./-}" \
  --set defaultRevision="v${ISTIO_V1//./-}"

helm install --wait --namespace istio-system istio-cni "$ISTIO_HELM_REPO/cni-$ISTIO_V1.tgz" \
  --values istio-cni.values.yaml \
  --set revision="v${ISTIO_V1//./-}"

helm install --wait --namespace istio-system "istio-istiod-v${ISTIO_V1//./-}" "$ISTIO_HELM_REPO/istiod-$ISTIO_V1.tgz" \
  --values istio-istiod.values.yaml \
  --set revision="v${ISTIO_V1//./-}"

# set the stable tag for this revision (tag name is arbitrarty)
istioctl tag generate stable --overwrite --revision "v${ISTIO_V1//./-}" | kubectl apply -f - 

# set the canary tag for this revision (tag name is arbitrarty)
istioctl tag generate canary --overwrite --revision "v${ISTIO_V1//./-}" | kubectl apply -f - 

###
### install bookinfo app and test...
###

# install k8s Gateway API
kubectl get crd gateways.gateway.networking.k8s.io &> /dev/null || \
  { kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml; }

# enable auto-injection on the default namespace
#kubectl label namespace default istio-injection=enabled
kubectl label namespace default istio.io/rev=stable

kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/bookinfo/platform/kube/bookinfo.yaml

sleep 5

### install new istiod

helm install --wait --namespace istio-system "istio-istiod-v${ISTIO_V2//./-}" "$ISTIO_HELM_REPO/istiod-$ISTIO_V2.tgz" \
  --values istio-istiod.values.yaml \
  --set revision="v${ISTIO_V2//./-}"


# set the canary tag for this revision (tag name is arbitrarty)
istioctl tag generate canary --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f - 

###
### Restart stuff in canary-tagged namespaces and verify it's working
###  (this restart of outdated pods is what the IPUC operator should do)
###



### promote the new istiod to stable

# set the stable tag for this revision
istioctl tag generate stable --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f -


### make the new istio revision the system default

helm upgrade --wait --namespace istio-system istio-base "$ISTIO_HELM_REPO/base-$ISTIO_V2.tgz" \
  --values istio-base.values.yaml \
  --set revision="v${ISTIO_V2//./-}" \
  --set defaultRevision="v${ISTIO_V2//./-}" \
  --skip-crds


### TODO: figure out when is best to upgrade the CNI

helm upgrade --wait --namespace istio-system istio-cni "$ISTIO_HELM_REPO/cni-$ISTIO_V2.tgz" \
  --values istio-cni.values.yaml \
  --set revision="v${ISTIO_V2//./-}"



echo DONE

