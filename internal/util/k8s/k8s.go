/*
Add code here for finding an outdated pod's controller, issuing rollout-restart to it,
and checking progress of the rollout-restart.

We will likely want logic to compare the timestamp of the label we give the pod
when marking it as outdated with the status of the controller's rollout-restart.

This way we can figure out if the rollout restart failed for some reason and we should
emit a log message and/or metrics indicating the manual intervention might be needed for
the pod. We also don't want to issue multiple rollouts for rollouts that are currently
progressing.
*/
package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PodNotFoundError struct{ msg string }

func (e PodNotFoundError) Error() string { return e.msg }

type ControllerNotFoundError struct{ msg string }

func (e ControllerNotFoundError) Error() string { return e.msg }

// I hate this function
func FindPodController(ctx context.Context, kubeClient kubernetes.Clientset, pod corev1.Pod) (*unstructured.Unstructured, error) {
	log := log.FromContext(ctx)

	// take the k8s-client Pod object and convert it to a k8s-client dynamic object

	dynamic := dynamic.NewForConfigOrDie(ctrl.GetConfigOrDie())

	resourceId := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}

	res, err := dynamic.Resource(resourceId).Namespace(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return nil, &PodNotFoundError{fmt.Sprintf("Couldn't find pod %v.%v", pod.Name, pod.Namespace)}
	}

	// find the top-level controller

	controller, err := getPodController(ctx, dynamic, res)
	if err != nil {
		return nil, &ControllerNotFoundError{fmt.Sprintf("Couldn't find controller of pod %v.%v", pod.Name, pod.Namespace)}
	}

	log.Info("Found controller for outdated pod", "pod-name", pod.Name, "controller-name", controller.GetName())
	return controller, nil
}

func getPodController(ctx context.Context, dynamic dynamic.Interface, obj *unstructured.Unstructured) (owner *unstructured.Unstructured, err error) {
	for _, oRef := range obj.GetOwnerReferences() {
		if *oRef.Controller {
			apiParts := strings.Split(oRef.APIVersion, "/")
			resourceId := schema.GroupVersionResource{
				Group:    apiParts[0],
				Version:  apiParts[1],
				Resource: strings.ToLower(oRef.Kind) + "s",
			}
			owner, err = dynamic.Resource(resourceId).Namespace(obj.GetNamespace()).Get(ctx, oRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if owner == nil {
		return obj, nil
	}
	return getPodController(ctx, dynamic, owner)
}
