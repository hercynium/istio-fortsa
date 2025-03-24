package common

const (
	// k8s object label for istio revision
	IstioRevLabel = "istio.io/rev"

	// k8s object label for istio revision tag
	IstioTagLabel = "istio.io/tag"

	// add this label to pods found connected to an outdated istiod
	PodOutdatedLabel = "fortsa.scaffidi.net/outdatedAt"
)
