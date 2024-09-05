package tags

import (
	"cmp"
	"context"
	"fmt"

	"istio.io/api/label"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/maps"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/slices"
)

const (
	IstioTagLabel = "istio.io/tag"
)

type tagDescription struct {
	Tag        string   `json:"tag"`
	Revision   string   `json:"revision"`
	Namespaces []string `json:"namespaces"`
}

type uniqTag struct {
	revision, tag string
}

func GetTags(ctx context.Context, kubeClient kubernetes.Interface) ([]tagDescription, error) {
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
	return wh.ObjectMeta.Labels[IstioTagLabel]
}

// GetWebhookRevision extracts tag target revision from webhook object.
func GetWebhookRevision(wh admissionv1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[label.IoIstioRev.Name]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag revision from webhook")
}

////
//// unused below here
////

// GetWebhooksWithTag returns webhooks tagged with istio.io/tag=<tag>.
func GetWebhooksWithTag(ctx context.Context, client kubernetes.Interface, tag string) ([]admissionv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", IstioTagLabel, tag),
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
		LabelSelector: fmt.Sprintf("%s=%s,!%s", label.IoIstioRev.Name, rev, IstioTagLabel),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
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
