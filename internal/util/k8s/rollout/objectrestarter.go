/*
Copyright 2018 The Kubernetes Authors.

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
package rollout

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/cmd/rollout"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func DoRestartDeployment(namespace string, kubeClient *kubernetes.Clientset, obj runtime.Object) error {

	deploymentName := "deployment/nginx-deployment"
	tf := cmdtesting.NewTestFactory().WithNamespace("test")

	//cmdutil.NewMatchVersionFlags()
	//cf := cmdutil.NewFactory(restClientGetter)

	tf.Client = kubeClient.RESTClient()
	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	cmd := rollout.NewCmdRolloutRestart(tf, streams)

	cmd.Run(cmd, []string{deploymentName})

	return nil
}

const (
	ReasonRolloutRestartFailed      = "RolloutRestartFailed"
	ReasonRolloutRestartTriggered   = "RolloutRestartTriggered"
	ReasonRolloutRestartUnsupported = "RolloutRestartUnsupported"
	ReasonAnnotationSucceeded       = "AnnotationAdditionSucceeded"
	ReasonAnnotationFailed          = "AnnotationAdditionFailed"
)
const (
	DEFAULT_FLIPPER_INTERVAL      = time.Duration(10 * time.Minute)
	DEFAULT_PENDING_WAIT_INTERVAL = time.Duration(10 * time.Second)
)

const (
	AnnotationFlipperRestartedAt = "flipper.ricktech.io/restartedAt"
	RolloutRestartAnnotation     = "kubectl.kubernetes.io/restartedAt"
	RolloutManagedBy             = "flipper.ricktech.io/managedBy"
	rolloutIntervalGroupName     = "flipper.ricktech.io/IntervalGroup"
)

const (
	ErrorUnsupportedKind = "unsupported Kind %v"
)

// HandleRolloutRestart handles rollout restart of object by patching with annotation
func HandleRolloutRestart(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object, managedByValue string, restartTimeInRFC3339 string) error {
	// log := log.FromContext(ctx)

	switch t := obj.(type) {
	case *appsv1.Deployment:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}

		t.Annotations[RolloutManagedBy] = managedByValue
		if restartTimeInRFC3339 == "" {
			restartTimeInRFC3339 = time.Now().Format(time.RFC3339)
		}

		t.Annotations[AnnotationFlipperRestartedAt] = restartTimeInRFC3339
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339

		//TODO exponential backoff maybe use thirdparty lib ?
		//TODO wait for pods to be ready before proceeding and followed by annotation completedAt:time?
		return client.Patch(ctx, t, patch)
	case *appsv1.DaemonSet:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}

		t.Annotations[RolloutManagedBy] = managedByValue
		if restartTimeInRFC3339 == "" {
			restartTimeInRFC3339 = time.Now().Format(time.RFC3339)
		}

		t.Annotations[AnnotationFlipperRestartedAt] = restartTimeInRFC3339
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339

		//TODO exponential backoff maybe use thirdparty lib ?
		//TODO wait for pods to be ready before proceeding and followed by annotation completedAt:time?
		return client.Patch(ctx, t, patch)
	case *appsv1.StatefulSet:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}

		t.Annotations[RolloutManagedBy] = managedByValue
		if restartTimeInRFC3339 == "" {
			restartTimeInRFC3339 = time.Now().Format(time.RFC3339)
		}

		t.Annotations[AnnotationFlipperRestartedAt] = restartTimeInRFC3339
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339

		//TODO exponential backoff maybe use thirdparty lib ?
		//TODO wait for pods to be ready before proceeding and followed by annotation completedAt:time?
		return client.Patch(ctx, t, patch)
	default:
		return fmt.Errorf(ErrorUnsupportedKind, t)
	}
}
