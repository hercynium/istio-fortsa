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
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func mainX() {
	ctx := context.Background()
	config := ctrl.GetConfigOrDie()
	dynamic := dynamic.NewForConfigOrDie(config)
	kubeClient, err := kubernetes.NewForConfig(config)

	namespace := "istio-system"
	podName := "kiali-7679bb98f6-x5qhx"
	pod, _ := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	fmt.Printf("Got the kiali pod: %v\n", pod.Name)
	resourceId := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}
	fmt.Printf("[%+v]\n", resourceId)
	res, err := dynamic.Resource(resourceId).Namespace(pod.GetNamespace()).Get(ctx, pod.GetName(), metav1.GetOptions{})
	if err != nil {
		os.Exit(3)
	}
	item, err := GetPodControllerRecursively(dynamic, ctx, res)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Printf("%+v, %+v\n", item.GetName(), item.GetKind())
	}
}

func GetPodController(dynamic dynamic.Interface, ctx context.Context, pod *corev1.Pod) (*unstructured.Unstructured, error) {
	for _, o := range pod.GetOwnerReferences() {
		if *o.Controller {
			apiParts := strings.Split(o.APIVersion, "/")
			resourceId := schema.GroupVersionResource{
				Group:    apiParts[0],
				Version:  apiParts[1],
				Resource: strings.ToLower(o.Kind) + "s",
			}
			fmt.Printf("Looking for: [%v, %v]\n", o.APIVersion, o.Kind)
			c, err := dynamic.Resource(resourceId).Namespace(pod.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	}
	return nil, nil
}

func GetPodControllerRecursively(dynamic dynamic.Interface, ctx context.Context,
	obj *unstructured.Unstructured) (owner *unstructured.Unstructured, err error) {
	for _, oRef := range obj.GetOwnerReferences() {
		if *oRef.Controller {
			apiParts := strings.Split(oRef.APIVersion, "/")
			resourceId := schema.GroupVersionResource{
				Group:    apiParts[0],
				Version:  apiParts[1],
				Resource: strings.ToLower(oRef.Kind) + "s",
			}
			fmt.Printf("Looking for: [%v, %v]\n", oRef.APIVersion, oRef.Kind)
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
	return GetPodControllerRecursively(dynamic, ctx, owner)
}
