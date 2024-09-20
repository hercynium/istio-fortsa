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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/hercynium/istio-fortsa/internal/util"
	"github.com/hercynium/istio-fortsa/internal/util/istiodata"
)

// MutatingWebhookConfigurationReconciler reconciles a MutatingWebhookConfiguration object
type MutatingWebhookConfigurationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	KubeClient *kubernetes.Clientset
	IstioData  *istiodata.IstioData
}

//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MutatingWebhookConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.FromContext(ctx)

	log.Info("Reconciling MutatingWebhookConfiguration")

	// if the webhook config changed, we should check for istio proxy status
	// and revision tags and outdated pods because istio was likely updated
	return util.UpdateDataAndCheckAndMarkPods(ctx, r.KubeClient, r.IstioData)
}

// all istio webhooks should have this "app" label value
var webhookAppLabelValue = "sidecar-injector"

// filter webhooks we want to reconcile.
func onlyReconcileIstioWebhooks() predicate.Predicate {
	// only look at webhook configs created in the last 10 minutes on creation events
	duration, _ := time.ParseDuration("-10m")
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// only recently created and has the label
			return e.Object.GetCreationTimestamp().After(time.Now().Add(duration)) &&
				e.Object.GetLabels()["app"] == webhookAppLabelValue
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()["app"] == webhookAppLabelValue
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()["app"] == webhookAppLabelValue
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// ignore these for now
			return false
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MutatingWebhookConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&admissionv1.MutatingWebhookConfiguration{}).
		WithEventFilter(onlyReconcileIstioWebhooks()).
		Complete(r)
}
