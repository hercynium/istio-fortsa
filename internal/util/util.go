package util

import (
	"context"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istiodata"
)

type ICUPReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	KubeClient *kubernetes.Clientset
	IstioData  *istiodata.IstioData
}

const (
	PodOutdatedLabel = "ipuc.cloudera.com/outdatedAt"
)

func LabelPodsOutdated(ctx context.Context, k *kubernetes.Clientset, oldPods []corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, pod := range oldPods {
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		if pod.Labels[PodOutdatedLabel] == "" {
			pod.Labels[PodOutdatedLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)
			_, err := k.CoreV1().Pods(pod.Namespace).Update(ctx, &pod, v1.UpdateOptions{})
			if err != nil {
				log.Error(err, "Couldn't update outdated pod label")
				return err
			}
		}
	}
	return nil
}

func UpdateDataAndCheckAndMarkPods(ctx context.Context, k *kubernetes.Clientset, i *istiodata.IstioData) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	err := i.RefreshIstioData(ctx, k)
	if err != nil {
		log.Error(err, "Couldn't refresh istio data")
		return ctrl.Result{}, nil
	}
	//r.IstioData.PrintProxyStatusData(ctx)

	oldPods, err := i.CheckProxiedPods(ctx, k)
	if err != nil {
		log.Error(err, "Error checking proxied pods")
		return ctrl.Result{}, err
	}

	// when pods are labeled as outdated, this should trigger the pod controller
	err = LabelPodsOutdated(ctx, k, oldPods)
	if err != nil {
		log.Error(err, "Error labelling outdated pods")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
