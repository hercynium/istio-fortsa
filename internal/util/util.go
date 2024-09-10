package util

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istiodata"
)

type ICUPReconciler struct {
	client.Client
	IstioData *istiodata.IstioData
}

func (r *ICUPReconciler) LabelPodsOutdated(ctx context.Context, oldPods []*corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, pod := range oldPods {
		pod.Labels["ipuc.cloudera.com/pod-outdated"] = time.Now().String()
		err := r.Update(ctx, pod)
		if err != nil {
			log.Error(err, "Couldn't update outdated pod label")
			return err
		}
	}
	return nil
}
