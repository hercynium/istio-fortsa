package rollout

// code in this package modified from https://github.com/rickslick/autorollout-operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ReasonRolloutRestartFailed      = "RolloutRestartFailed"
	ReasonRolloutRestartTriggered   = "RolloutRestartTriggered"
	ReasonRolloutRestartUnsupported = "RolloutRestartUnsupported"
	ReasonAnnotationSucceeded       = "AnnotationAdditionSucceeded"
	ReasonAnnotationFailed          = "AnnotationAdditionFailed"
)

const (
	RolloutRestartAnnotation = "ipuc.cloudera.com/restartedAt"
)

// DoRolloutRestart handles rollout restart of object by patching with annotation
func DoRolloutRestart(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object) error {
	log := log.FromContext(ctx)

	done, err := IsRolloutReady(ctx, client, obj)
	if err != nil {
		return err
	}
	if !done {
		log.Info("Deployment is currently in a rollout. Skipping.")
		return nil
	}

	restartTimeInNanos := time.Now().Format(time.RFC3339)

	// TODO: figure out how to properly handle different object types, not like this...
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "Deployment":
		objX := &appsv1.Deployment{}
		client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		return client.Patch(ctx, objX, patch)
	case "DaemonSet":
		objX := &appsv1.DaemonSet{}
		client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		return client.Patch(ctx, objX, patch)
	case "StatefulSet":
		objX := &appsv1.StatefulSet{}
		client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		return client.Patch(ctx, objX, patch)
	default:
		return fmt.Errorf("unsupported Kind %v for rollout restart", obj.GetObjectKind().GroupVersionKind().Kind)
	}
}

// we only want to issue a rollout when any previous rollout is done. any other status -
// error, in-progress, whatever, and we don't want to do another rollout automatically.
func IsRolloutReady(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object) (bool, error) {
	var revision int64
	statusViewer, err := polymorphichelpers.StatusViewerFor(obj.GetObjectKind().GroupVersionKind().GroupKind())
	if err != nil {
		return false, err
	}
	status, done, err := statusViewer.Status(obj.(runtime.Unstructured), revision)
	if err != nil {
		return false, err
	}
	fmt.Printf("Status: %s", status)
	return done, nil
}
