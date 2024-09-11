package rollout

// code in this package modified from https://github.com/rickslick/autorollout-operator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
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

const (
	ErrorUnsupportedKind = "unsupported Kind %v"
)

func newClient() (dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return dynClient, nil
}

// DoRolloutRestart handles rollout restart of object by patching with annotation
func DoRolloutRestart(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object, restartTimeInNanos string) error {
	log := log.FromContext(ctx)

	done, err := IsRolloutDone(ctx, client, obj)
	if err != nil {
		return err
	}
	if !done {
		log.Info("Deployment is currently in a rollout. Skipping.")
		return nil
	}

	// TODO: figure out how to properly handle different object types, like DaemonSets
	objX := &appsv1.Deployment{}
	client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
	patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
	if objX.Spec.Template.ObjectMeta.Annotations == nil {
		objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}
	objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
	return client.Patch(ctx, objX, patch)

	// TODO: refactor - make a set of functions that, for each type, extracts a pointer to the object's
	// t.Spec.Template. Then in here we can simply have one block of code to set the annotation and issue
	// the client.Patch on t... I think that should work...
	// see here for an example: https://github.com/stakater/Reloader/blob/master/internal/pkg/callbacks/rolling_upgrade.go#L211
	//k := obj.GetObjectKind().GroupVersionKind().Kind
	// switch t := *objX.(type) {
	// case *appsv1.Deployment:
	// 	//t := obj.(*appsv1.Deployment)
	// 	patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
	// 	if t.Spec.Template.ObjectMeta.Annotations == nil {
	// 		t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	// 	}
	// 	t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
	// 	return client.Patch(ctx, t, patch)
	// case *appsv1.DaemonSet:
	// 	patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
	// 	if t.Spec.Template.ObjectMeta.Annotations == nil {
	// 		t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	// 	}
	// 	t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
	// 	return client.Patch(ctx, t, patch)
	// case *appsv1.StatefulSet:
	// 	patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
	// 	if t.Spec.Template.ObjectMeta.Annotations == nil {
	// 		t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	// 	}
	// 	t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
	// 	return client.Patch(ctx, t, patch)
	// default:
	// 	return fmt.Errorf(ErrorUnsupportedKind, obj.GetObjectKind().GroupVersionKind().Kind)
	// }
}

// we only want to issue a rollout when any previous rollout is done. any other
// status - error, in-progress, and we don;t want to do another rollout automatically.
func IsRolloutDone(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object) (bool, error) {
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
