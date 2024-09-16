/*
Copyright 2024.

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

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istiodata"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/k8s"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/k8s/rollout"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	KubeClient *kubernetes.Clientset
	IstioData  *istiodata.IstioData
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling pod", "pod-name", req.NamespacedName)

	pod, err := r.KubeClient.CoreV1().Pods(req.Namespace).Get(ctx, req.Name, v1.GetOptions{})
	if err != nil {
		// it was probably deleted, so nothing to do...
		log.Info("Couldn't load pod", "err", err)
		return ctrl.Result{}, nil
	}

	pc, err := k8s.FindPodController(ctx, *r.KubeClient, *pod)
	if err != nil {
		log.Info("Error finding controller for pod", "pod-name", pod.Name, "err", err)
		// not returning error, since it probably was deleted
		return ctrl.Result{}, nil
	}

	done, err := rollout.IsRolloutReady(ctx, r.Client, pc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !done {
		log.Info("Deployment is currentlyx in a rollout. Skipping.")
		// reinject?
		return ctrl.Result{}, err
	}

	dryRun := false
	err = rollout.DoRolloutRestart(ctx, r.Client, pc, dryRun)
	if err != nil {
		log.Error(err, "Error doing rollout restart on controller for pod", "pod-name", pod.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcile if the label is present and non-empty
func onlyReconcileOutdatedPods() predicate.Predicate {
	outdatedPodLabel := util.PodOutdatedLabel
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

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(onlyReconcileOutdatedPods()).
		Complete(r)
}
