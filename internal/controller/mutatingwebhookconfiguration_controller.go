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

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// MutatingWebhookConfigurationReconciler reconciles a MutatingWebhookConfiguration object
type MutatingWebhookConfigurationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	KubeClient *kubernetes.Clientset
	IstioData  *util.IstioData
}

//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=mutatingwebhookconfigurations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MutatingWebhookConfiguration object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *MutatingWebhookConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.FromContext(ctx)

	log.Info("Reconciling MutatingWebhookConfiguration")

	// if the istio tag on the namespace changed, we should restart the pods so the
	// sidecar proxies can be configured to whatever the new tag value indicates.
	//sd, _ := util.GetProxyStatusData(ctx, r.KubeClient)
	//r.IstioData.RefreshIstioData(ctx, req, r.KubeClient)
	util.PrintProxyStatusData(ctx, r.IstioData.ProxyStatuses)

	var webHook admissionv1.MutatingWebhookConfiguration
	if err := r.Get(ctx, req.NamespacedName, &webHook); err != nil {
		log.Error(err, "unable to fetch MutatingWebhookConfiguration")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

var webhookAppLabelValue = "sidecar-injector"

// filter webhooks we want to reconcile.
func onlyReconcileIstioWebhooks() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()["app"] == webhookAppLabelValue
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
	// kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	// if err != nil {
	// 	panic(err.Error())
	// }

	// rec := &MutatingWebhookConfigurationReconciler{
	// 	Client:     mgr.GetClient(),
	// 	Scheme:     mgr.GetScheme(),
	// 	KubeClient: kubeClient,
	// }

	return ctrl.NewControllerManagedBy(mgr).
		For(&admissionv1.MutatingWebhookConfiguration{}).
		WithEventFilter(onlyReconcileIstioWebhooks()).
		Complete(r)
}
