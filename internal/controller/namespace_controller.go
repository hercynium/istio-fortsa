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
	"strconv"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/util/workqueue"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Allow read-only access to Namespaces
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=get;list;watch

// Allow read-only access to MutatingWebhookConfigurations
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations/finalizers,verbs=get;list;watch

// Allow necessary access to Pods (we update labels)
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=get;list;watch;update;patch

// Allow read-only access to everything
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Namespace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)

	// name of this namespace
	var nsName = req.Name

	// istio rev pods in this namespace should use
	var nsDesiredRev, err = r.getNamespaceDesiredRev(ctx, nsName)
	if err != nil {
		log.Error(err, "Failed to get istio revision associated with this namespace", "ns", nsName)
		return ctrl.Result{}, err
	}

	// get pods in the namespace
	var pods = &corev1.PodList{}
	err = r.Client.List(ctx, pods, &client.ListOptions{
		Namespace: nsName,
	})
	if err != nil {
		log.Error(err, "Failed to get list of pods in this namespace", "ns", nsName)
		return ctrl.Result{}, err
	}

	// check each pod if it's using the desired revision of Istio
	for _, pod := range pods.Items {
		var podIstioRev = pod.Annotations[IstioRevLabel]
		if podIstioRev != "" && podIstioRev != nsDesiredRev {
			log.Info("Outdated pod found", "ns", nsName, "nsRev", nsDesiredRev, "pod", pod.Name, "podRev", podIstioRev)
			// label the pod as outdated so the pod controller can handle the rollout restart on its controller
			// doing all that here would be complex, and make rate-limiting more difficult.
			if pod.Labels == nil {
				pod.Labels = make(map[string]string)
			}
			pod.Labels[PodOutdatedLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)
			err := r.Update(ctx, &pod)
			if err != nil {
				log.Error(err, "Couldn't mark pod outdated", "ns", pod.Namespace, "pod", pod.Name)
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) getNamespaceDesiredRev(ctx context.Context, nsName string) (string, error) {
	var _ = log.FromContext(ctx)

	var ns = &corev1.Namespace{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: nsName}, ns, &client.GetOptions{})
	if err != nil {
		return "", err
	}

	// istio revision or tag this namespace is configured to use
	var nsIstioRevLabelValue = ns.Labels[IstioRevLabel]

	// get the webhooks that correspond to the label on the namespace
	var webhooks = &admissionregistrationv1.MutatingWebhookConfigurationList{}
	err = r.Client.List(ctx, webhooks, &client.ListOptions{
		LabelSelector: labels.Set{IstioTagLabel: nsIstioRevLabelValue}.AsSelector(),
	})
	if err != nil {
		return "", err
	}

	// map webhook tags to istio revisions for easy lookup. { tag => rev }
	var tagMap = make(map[string]string)
	for _, webhook := range webhooks.Items {
		tagMap[webhook.Labels[IstioTagLabel]] = webhook.Labels[IstioRevLabel]
	}

	// the revision that corresponds to the tag indicated by the label on the namespace
	var nsDesiredRev = tagMap[ns.Labels[IstioRevLabel]]

	return nsDesiredRev, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// also watch changes to MutatingWebhookConfigurations, because changing the istio
	// webhooks means we need to reconcile namespaces.
	src := source.Kind(
		mgr.GetCache(),
		&admissionregistrationv1.MutatingWebhookConfiguration{},
		r.webhookEventHandlers(),
		onlyReconcileIstioWebhooks(),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named("namespace").
		WithEventFilter(onlyReconcileIstioRevLabeled()).
		WatchesRawSource(src).
		Complete(r)
}

// filter namespace events we want to reconcile
func onlyReconcileIstioRevLabeled() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()[IstioRevLabel] != ""
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// only reconcile if the value of the label changed
			var oldLabels = e.ObjectOld.GetLabels()
			var newLabels = e.ObjectNew.GetLabels()
			return oldLabels[IstioRevLabel] != newLabels[IstioRevLabel]
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// no namespace means no label to think about. Skip the event.
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()[IstioRevLabel] != ""
		},
	}
}

func (r *NamespaceReconciler) webhookEventHandlers() handler.TypedFuncs[*admissionregistrationv1.MutatingWebhookConfiguration, reconcile.Request] {
	return handler.TypedFuncs[*admissionregistrationv1.MutatingWebhookConfiguration, reconcile.Request]{
		CreateFunc: func(ctx context.Context, e event.TypedCreateEvent[*admissionregistrationv1.MutatingWebhookConfiguration], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			var log = log.FromContext(ctx)
			log.Info("Reconciling Create for Webhook", "name", e.Object.Name)
			for _, nsRec := range r.reconcileWebhookConfig(ctx, e.Object) {
				q.Add(nsRec)
			}
		},
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[*admissionregistrationv1.MutatingWebhookConfiguration], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			var log = log.FromContext(ctx)
			log.Info("Reconciling Update for Webhook", "name", e.ObjectNew.Name)
			for _, nsRec := range r.reconcileWebhookConfig(ctx, e.ObjectNew) {
				q.Add(nsRec)
			}
		},
		/*
			DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[*admissionregistrationv1.MutatingWebhookConfiguration], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      e.Object.Name,
					Namespace: e.Object.Namespace,
				}})
			},
			GenericFunc: func(ctx context.Context, e event.TypedGenericEvent[*admissionregistrationv1.MutatingWebhookConfiguration], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      e.Object.Name,
					Namespace: e.Object.Namespace,
				}})
			},
		*/

	}
}

// all istio webhooks should have this "app" label value
const webhookAppLabelValue = "sidecar-injector"

// the webhooks we're interested in...
func isIstioTaggedWebhook(o client.Object) bool {
	labels := o.GetLabels()
	return labels["app"] == webhookAppLabelValue && labels[IstioTagLabel] != ""
}

func (r *NamespaceReconciler) reconcileWebhookConfig(ctx context.Context,
	webhook *admissionregistrationv1.MutatingWebhookConfiguration) []reconcile.Request {
	var log = log.FromContext(ctx)

	var tag = webhook.Labels[IstioTagLabel] // canary, stable, default, etc...
	var rev = webhook.Labels[IstioRevLabel] // istiod instance revision

	if !isIstioTaggedWebhook(webhook) {
		return []reconcile.Request{}
	}

	log.Info("Istio Webhook Found", "name", webhook.Name, "istioTag", tag, "istioRev", rev)

	// find namespaces that use this webhook's tag
	var nsList = &corev1.NamespaceList{}
	err := r.Client.List(ctx, nsList, &client.ListOptions{
		LabelSelector: labels.Set{IstioRevLabel: tag}.AsSelector(),
	})
	if err != nil {
		log.Error(err, "Failed to get list of namespaces labeled for istio revision", "tag", tag, "rev", rev)
		return []reconcile.Request{}
	}

	var nsRecs = []reconcile.Request{}
	for _, ns := range nsList.Items {
		log.Info("Enqueuing Namespace", "ns", ns.Name)
		rec := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: ns.Name, Namespace: ns.Namespace},
		}
		nsRecs = append(nsRecs, rec)
	}

	return nsRecs
}

func onlyReconcileIstioWebhooks() predicate.TypedPredicate[*admissionregistrationv1.MutatingWebhookConfiguration] {
	return predicate.TypedFuncs[*admissionregistrationv1.MutatingWebhookConfiguration]{
		CreateFunc: func(e event.TypedCreateEvent[*admissionregistrationv1.MutatingWebhookConfiguration]) bool {
			return isIstioTaggedWebhook(e.Object)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*admissionregistrationv1.MutatingWebhookConfiguration]) bool {
			return isIstioTaggedWebhook(e.ObjectNew)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*admissionregistrationv1.MutatingWebhookConfiguration]) bool {
			return isIstioTaggedWebhook(e.Object)
		},
		GenericFunc: func(e event.TypedGenericEvent[*admissionregistrationv1.MutatingWebhookConfiguration]) bool {
			return isIstioTaggedWebhook(e.Object)
		},
	}
}
