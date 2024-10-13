package tags

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
)

func webhook(name string, tag string, rev string) *admitv1.MutatingWebhookConfiguration {
	return &admitv1.MutatingWebhookConfiguration{
		Webhooks: []admitv1.MutatingWebhook{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "istio-system",
			Name:      name,
			Labels:    map[string]string{"istio.io/tag": tag, "istio.io/rev": rev, "app": "sidecar-injector"},
		},
	}
}

func invalidWebhook(name string, tag string) *admitv1.MutatingWebhookConfiguration {
	return &admitv1.MutatingWebhookConfiguration{
		Webhooks: []admitv1.MutatingWebhook{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "istio-system",
			Name:      name,
			Labels:    map[string]string{"istio.io/tag": tag, "app": "sidecar-injector"},
		},
	}
}

func namespace(name string, tag string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"istio.io/rev": tag},
		},
	}
}

func TestGetTags(t *testing.T) {
	var tests = []struct {
		description string
		namespace   string
		expected    []TagDescription
		objs        []runtime.Object
	}{
		{
			"no webhook configs",
			"",
			[]TagDescription{},
			[]runtime.Object{
				namespace("default", "canary"),
				namespace("test1", "canary"),
			},
		},
		{
			"invalid webhook config",
			"",
			[]TagDescription{},
			[]runtime.Object{
				invalidWebhook("invalid", "foo"),
				namespace("default", "canary"),
				namespace("test1", "stable"),
			},
		},
		{
			"multiple webhook configs",
			"",
			[]TagDescription{
				{Tag: "canary", Revision: "canary", Namespaces: []string{"default", "test1"}},
				{Tag: "stable", Revision: "stable", Namespaces: []string{}},
			},
			[]runtime.Object{
				webhook("canary", "canary", "canary"),
				webhook("stable", "stable", "stable"),
				namespace("default", "canary"),
				namespace("test1", "canary"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			client := fake.NewClientset(test.objs...)
			actual, err := GetTags(context.TODO(), client)
			if err != nil {
				t.Errorf("Unexpected error: %s", err)
				return
			}
			if diff := cmp.Diff(actual, test.expected); diff != "" {
				t.Errorf("%T differ (-got, +want): %s", test.expected, diff)
				return
			}
		})
	}
}
