/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	"golang.org/x/time/rate"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hercynium/istio-fortsa/internal/common"
	"github.com/hercynium/istio-fortsa/internal/config"
	"github.com/hercynium/istio-fortsa/internal/k8s"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Config     config.FortsaConfig
	KubeClient *kubernetes.Clientset
}

// Allow necessary access to pods
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)

	log.Info("Reconciling Pod")

	// use the k8s client to get the pod
	podX, err := r.KubeClient.CoreV1().Pods(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		// it was probably deleted, so nothing to do...
		log.Info("Couldn't load pod", "err", err, "ns", req.Namespace, "pod", req.Name)
		return ctrl.Result{}, nil
	}

	// find the controller of the pod
	pc, err := k8s.FindPodController(ctx, *r.KubeClient, *podX)
	if err != nil {
		log.Info("Could not find controller for pod", "err", err, "ns", podX.Namespace, "pod", podX.Name)
		// not returning error, since it probably was deleted
		return ctrl.Result{}, nil
	}

	// make sure the controller is one we can restart
	switch pc.GetKind() {
	case "DaemonSet", "Deployment", "StatefulSet":
		break
	default:
		log.Info("Upsupported controller type for restart",
			"ns", podX.Namespace, "pod", podX.Name,
			"podController", pc.GetName(), "podControllerKind", pc.GetKind())
		return ctrl.Result{}, nil
	}

	// check if the controller is ready to be restarted
	done, err := k8s.IsRolloutReady(ctx, r.Client, pc)
	if err != nil {
		log.Info("Couldn't determine if rollout is ready, requeuing",
			"err", err, "ns", podX.Namespace, "pod", podX.Name,
			"podController", pc.GetName(), "podControllerKind", pc.GetKind())
		// try again if we couldn't determine status.
		return ctrl.Result{Requeue: true}, nil
	}
	if !done {
		log.Info("Deployment is currently in a rollout. Skipping.",
			"ns", podX.Namespace, "pod", podX.Name,
			"podController", pc.GetName(), "podControllerKind", pc.GetKind())
		// reinject?
		return ctrl.Result{}, nil
	}

	// do the thing
	dryRun := r.Config.DryRun
	err = k8s.DoRolloutRestart(ctx, r.Client, pc, dryRun)
	if err != nil {
		log.Error(err, "Error doing rollout restart on controller for pod",
			"ns", podX.Namespace, "pod", podX.Name,
			"podController", pc.GetName(), "podControllerKind", pc.GetKind())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Named("pod").
		WithEventFilter(onlyReconcileOutdatedPods()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
			RateLimiter:             r.podControllerRateLimiter(),
		}).
		Complete(r)
}

// reconcile if the label is present and non-empty
func onlyReconcileOutdatedPods() predicate.Predicate {
	outdatedPodLabel := common.PodOutdatedLabel
	return predicate.Funcs{
		// on controller start, we get create events, so reconcile only those with the outdated label set
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()[outdatedPodLabel] != ""
		},
		// reconcile when a pod is updated and the label has been added or changed
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()[outdatedPodLabel] != "" &&
				e.ObjectOld.GetLabels()[outdatedPodLabel] != e.ObjectNew.GetLabels()[outdatedPodLabel]
		},
		// if a pod with this label is deleted, there's nothing to do, right?
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// ignore these for now (what do they even do?)
			return false
		},
	}
}

func (r *PodReconciler) podControllerRateLimiter() workqueue.TypedRateLimiter[reconcile.Request] {
	// TODO: figure out how to implement desired rate-limiting semantics here...
	// for example: only perform 5 restarts every minute, and no more than 5 active restarts at a time
	var restartsPerMinute = r.Config.RestartsPerMinute
	var activeRestartLimit = r.Config.ActiveRestartLimit

	limit := rate.Limit(1.0 / (60.0 / restartsPerMinute))
	limiter := rate.NewLimiter(limit, activeRestartLimit)
	return workqueue.NewTypedMaxOfRateLimiter(
		//workqueue.NewTypedItemExponentialFailureRateLimiter[T](500*time.Millisecond, 1000*time.Second),
		// This is only for retry speed and its only the overall factor (not per item)
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: limiter},
	)
}
