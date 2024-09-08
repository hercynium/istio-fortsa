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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/k8s"
)

func main() {
	ctx := context.Background()
	config := ctrl.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		os.Exit(3)
	}

	namespace := "istio-system"
	podName := "kiali-7679bb98f6-x5qhx"
	pod, _ := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	fmt.Printf("Got the kiali pod: %v\n", pod.Name)

	obj, err := k8s.FindPodController(ctx, *kubeClient, *pod)
	if err != nil {
		os.Exit(4)
	}
	fmt.Printf("Got Object: [%+v]\n", obj.GetObjectKind())

	//foo := meta.RESTMapping{
	//	GroupVersionKind: obj.GetObjectKind().GroupVersionKind(),
	//	//Resource: obj.GetObjectKind().GroupVersionKind().GroupVersion().WithResource(),
	//}

	//_, err = defaultObjectRestarter(obj)
	err = k8s.RestartDeployment(namespace, kubeClient, obj)
	if err != nil {
		os.Exit(5)
	}

}
