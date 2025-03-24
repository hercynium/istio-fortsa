package controller

const (
	// k8s object label for istio revision
	IstioRevLabel = "istio.io/rev"

	// k8s object label for istio revision tag
	IstioTagLabel = "istio.io/tag"

	// namespace istio runs in
	IstioNamespace = "istio-system"

	RolloutRestartAnnotation = "fortsa.scaffidi.net/restartedAt"

	PodOutdatedLabel = "fortsa.scaffidi.net/pod-outdated"
)
