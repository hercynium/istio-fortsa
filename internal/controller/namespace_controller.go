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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istiodata"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	util.ICUPReconciler
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	KubeClient *kubernetes.Clientset
	IstioData  *istiodata.IstioData
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	//log.Info("Reconciling Namespace")

	// if the istio tag on the namespace changed, we should restart the pods so the
	// sidecar proxies can be configured to whatever the new tag value indicates.
	err := r.IstioData.RefreshIstioData(ctx, r.KubeClient)
	if err != nil {
		log.Error(err, "Couldn't refresh istio data")
		return ctrl.Result{}, nil
	}
	//r.IstioData.PrintProxyStatusData(ctx)

	oldPods, err := r.IstioData.CheckProxiedPods(ctx, r.KubeClient)
	if err != nil {
		log.Error(err, "Error checking proxied pods")
		return ctrl.Result{}, err
	}

	// when pods are labeled as outdated, this should trigger the pod controller
	err = r.LabelPodsOutdated(ctx, r.KubeClient, oldPods)
	if err != nil {
		log.Error(err, "Error labelling outdated pods")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// filter namespace events we want to reconcile
func onlyReconcileIstioLabelChange() predicate.Predicate {
	// only look at namespaces created in the last 10 minutes on creation events
	duration, _ := time.ParseDuration("-10m")
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// only reconcile if the label exists and is not empty
			return e.Object.GetCreationTimestamp().After(time.Now().Add(duration)) &&
				e.Object.GetLabels()[tags.IstioRevisionLabel] != ""
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// only reconcile if the value of the label changed
			oldLabels := e.ObjectOld.GetLabels()
			newLabels := e.ObjectNew.GetLabels()
			return oldLabels[tags.IstioRevisionLabel] != newLabels[tags.IstioRevisionLabel]
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// no namespace means no label to think about. Skip the event.
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// ignore these for now
			return false
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithEventFilter(onlyReconcileIstioLabelChange()).
		Complete(r)
}
