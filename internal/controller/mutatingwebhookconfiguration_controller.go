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
	"cmp"
	"context"
	"fmt"
	"strings"

	"istio.io/api/label"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logger "sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/maps"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/slices"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
)

// MutatingWebhookConfigurationReconciler reconciles a MutatingWebhookConfiguration object
type MutatingWebhookConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	log.Info("Reconcile OutdatedPodReconciler")

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

// SetupWithManager sets up the controller with the Manager.
func (r *MutatingWebhookConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&admissionv1.MutatingWebhookConfiguration{}).
		Complete(r)
}

type tagDescription struct {
	Tag        string   `json:"tag"`
	Revision   string   `json:"revision"`
	Namespaces []string `json:"namespaces"`
}

type uniqTag struct {
	revision, tag string
}

// listTags lists existing revision.
func getTags(ctx context.Context, kubeClient kubernetes.Interface) ([]tagDescription, error) {
	tagWebhooks, err := GetRevisionWebhooks(ctx, kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve revision tags: %v", err)
	}
	if len(tagWebhooks) == 0 {
		fmt.Printf("No Istio revision tag MutatingWebhookConfigurations to list\n")
		return nil, nil
	}
	rawTags := map[uniqTag]tagDescription{}
	for _, wh := range tagWebhooks {
		tagName := GetWebhookTagName(wh)
		tagRevision, err := GetWebhookRevision(wh)
		if err != nil {
			return nil, fmt.Errorf("error parsing revision from webhook %q: %v", wh.Name, err)
		}
		tagNamespaces, err := GetNamespacesWithTag(ctx, kubeClient, tagName)
		if err != nil {
			return nil, fmt.Errorf("error retrieving namespaces for tag %q: %v", tagName, err)
		}
		tagDesc := tagDescription{
			Tag:        tagName,
			Revision:   tagRevision,
			Namespaces: tagNamespaces,
		}
		key := uniqTag{
			revision: tagRevision,
			tag:      tagName,
		}
		rawTags[key] = tagDesc
	}
	for k := range rawTags {
		if k.tag != "" {
			delete(rawTags, uniqTag{revision: k.revision})
		}
	}

	tags := slices.SortFunc(maps.Values(rawTags), func(a, b tagDescription) int {
		if r := cmp.Compare(a.Revision, b.Revision); r != 0 {
			return r
		}
		return cmp.Compare(a.Tag, b.Tag)
	})

	for _, t := range tags {
		fmt.Printf("%s\t%s\t%s\n", t.Tag, t.Revision, strings.Join(t.Namespaces, ","))
	}

	return tags, nil
}

func GetRevisionWebhooks(ctx context.Context, client kubernetes.Interface) ([]admissionv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: label.IoIstioRev.Name,
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetWebhooksWithTag returns webhooks tagged with istio.io/tag=<tag>.
func GetWebhooksWithTag(ctx context.Context, client kubernetes.Interface, tag string) ([]admissionv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", tags.IstioTagLabel, tag),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetWebhooksWithRevision returns webhooks tagged with istio.io/rev=<rev> and NOT TAGGED with istio.io/tag.
// this retrieves the webhook created at revision installation rather than tag webhooks
func GetWebhooksWithRevision(ctx context.Context, client kubernetes.Interface, rev string) ([]admissionv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,!%s", label.IoIstioRev.Name, rev, tags.IstioTagLabel),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetNamespacesWithTag retrieves all namespaces pointed at the given tag.
func GetNamespacesWithTag(ctx context.Context, client kubernetes.Interface, tag string) ([]string, error) {
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", label.IoIstioRev.Name, tag),
	})
	if err != nil {
		return nil, err
	}

	nsNames := make([]string, len(namespaces.Items))
	for i, ns := range namespaces.Items {
		nsNames[i] = ns.Name
	}
	return nsNames, nil
}

// GetWebhookTagName extracts tag name from webhook object.
func GetWebhookTagName(wh admissionv1.MutatingWebhookConfiguration) string {
	return wh.ObjectMeta.Labels[tags.IstioTagLabel]
}

// GetWebhookRevision extracts tag target revision from webhook object.
func GetWebhookRevision(wh admissionv1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[label.IoIstioRev.Name]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag revision from webhook")
}

// PreviousInstallExists checks whether there is an existing Istio installation. Should be used in installer when deciding
// whether to make an installation the default.
func PreviousInstallExists(ctx context.Context, client kubernetes.Interface) bool {
	mwhs, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: "app=sidecar-injector",
	})
	if err != nil {
		return false
	}
	return len(mwhs.Items) > 0
}
