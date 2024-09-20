package util

import (
	"context"
	"strconv"
	"time"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/hercynium/istio-fortsa/internal/util/istiodata"
)

const (
	PodOutdatedLabel = "fortsa.example.com/outdatedAt"
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

// TODO: figure out how to implement desired rate-limiting semantics here...
// for example: only perform 5 restarts every minute, and no more than 5 active restarts at a time
const restartsPerMinute = 5.0 // TODO: compute from restartDelay config param
const activeRestartLimit = 5  // TODO: make this actually work

func PodControllerRateLimiter[T comparable]() workqueue.TypedRateLimiter[T] {
	limit := rate.Limit(1.0 / (60.0 / restartsPerMinute))
	limiter := rate.NewLimiter(limit, activeRestartLimit)
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[T](500*time.Millisecond, 1000*time.Second),
		// This is only for retry speed and its only the overall factor (not per item)
		&workqueue.TypedBucketRateLimiter[T]{Limiter: limiter},
	)
}
