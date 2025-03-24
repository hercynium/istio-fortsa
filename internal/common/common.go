package common

const (
	// k8s object label for istio revision
	IstioRevLabel = "istio.io/rev"

	// k8s object label for istio revision tag
	IstioTagLabel = "istio.io/tag"

	// namespace istio runs in
	IstioNamespace = "istio-system"

	PodOutdatedLabel = "fortsa.scaffidi.net/pod-outdated"
)
