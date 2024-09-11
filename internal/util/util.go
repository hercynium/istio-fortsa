package util

import (
	"context"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istiodata"
)

type ICUPReconciler struct {
	client.Client
	IstioData  *istiodata.IstioData
	KubeClient *kubernetes.Clientset
}

func (r *ICUPReconciler) LabelPodsOutdated(ctx context.Context, k *kubernetes.Clientset, oldPods []corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, pod := range oldPods {
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		if pod.Labels["ipuc.cloudera.com/outdatedAt"] == "" {
			pod.Labels["ipuc.cloudera.com/outdatedAt"] = strconv.FormatInt(time.Now().UnixNano(), 10)
			_, err := k.CoreV1().Pods(pod.Namespace).Update(ctx, &pod, v1.UpdateOptions{})
			if err != nil {
				log.Error(err, "Couldn't update outdated pod label")
				return err
			}
		}
	}
	return nil
}
