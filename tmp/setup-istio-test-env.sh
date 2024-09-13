#!/usr/bin/env bash
set -euo pipefail
set -x

ISTIO_V1="1.22.4"
ISTIO_V2="1.23.1"

ISTIO_HELM_REPO="https://istio-release.storage.googleapis.com/charts"

helm repo add istio "$ISTIO_HELM_REPO"
helm repo update

### set up a new cluster
minikube start --addons=metrics-server,storage-provisioner --cpus=max --memory=no-limit

sleep 5

kubectl create ns debug

helm install --wait --namespace kube-system --create-namespace kube-state-metrics bitnami/kube-state-metrics
helm install --wait --namespace prometheus --create-namespace kube-prometheus bitnami/kube-prometheus



### install first istio revision

kubectl create ns istio-system

helm install --wait --namespace istio-system istio-base istio/base \
  --version "$ISTIO_V1" \
  --values istio-base.values.yaml \
  --set revision="v${ISTIO_V1//./-}" \
  --ser revisionTags="{default,stable}" \
  --set defaultRevision="v${ISTIO_V1//./-}"

helm install --wait --namespace istio-system istio-cni istio/cni \
  --version "$ISTIO_V1" \
  --values istio-cni.values.yaml \
  --set revision="v${ISTIO_V1//./-}"

helm install --wait --namespace istio-system "istio-istiod-v${ISTIO_V1//./-}" istio/istiod  \
  --version "$ISTIO_V1" \
  --values istio-istiod.values.yaml \
  --set revision="v${ISTIO_V1//./-}"


# set the "default" tag for this revision. This tag is special, see:
#  https://istio.io/latest/docs/setup/upgrade/canary/#default-tag
istioctl tag generate default --overwrite --revision "v${ISTIO_V1//./-}" | kubectl apply -f - 

# set the stable tag for this revision (tag name is arbitrarty)
istioctl tag generate stable --overwrite --revision "v${ISTIO_V1//./-}" | kubectl apply -f - 

# set the canary tag for this revision (tag name is arbitrarty)
istioctl tag generate canary --overwrite --revision "v${ISTIO_V1//./-}" | kubectl apply -f - 

###
### install bookinfo app and test...
###

sleep 10

# install k8s Gateway API
kubectl get crd gateways.gateway.networking.k8s.io &> /dev/null || \
  { kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml; }

# enable auto-injection on the default namespace
kubectl label namespace default istio.io/rev=stable

kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.23/samples/bookinfo/platform/kube/bookinfo.yaml

sleep 5


### install new istiod

helm install --wait --namespace istio-system "istio-istiod-v${ISTIO_V2//./-}" istio/istiod \
  --version "$ISTIO_V2" \
  --values istio-istiod.values.yaml \
  --set revision="v${ISTIO_V2//./-}"


# set the canary tag for this revision
istioctl tag generate canary --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f - 


### if there were any canary-tagged namespaces, IPUC would restart the pods


### promote the new istiod to stable

# set the default tag for this revision
istioctl tag generate default --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f -


### make the new istio revision the system default

helm upgrade --wait --namespace istio-system istio-base istio/base \
  --version "$ISTIO_V2" \
  --values istio-base.values.yaml \
  --set revision="v${ISTIO_V2//./-}" \
  --set defaultRevision="v${ISTIO_V2//./-}" \
  --skip-crds


# upgrade the CNI
helm upgrade --wait --namespace istio-system istio-cni istio/cni \
  --version "$ISTIO_V2" \
  --values istio-cni.values.yaml \
  --set revision="v${ISTIO_V2//./-}"

# set the default tag for this revision
istioctl tag generate default --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f -

### by now the controller should be running, and the next command should cause it
### to trigger the rollout restart of pods connected to the old istio version,
### because we labeled the "default" namespace with the stable tag

# set the stable tag for this revision
istioctl tag generate stable --overwrite --revision "v${ISTIO_V2//./-}" | kubectl apply -f -

echo DONE

