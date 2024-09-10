package rollout

// code in this package modified from https://github.com/rickslick/autorollout-operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

// HandleRolloutRestart handles rollout restart of object by patching with annotation
func HandleRolloutRestart(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object, managedByValue string, restartTimeInRFC3339 string) error {
	log := log.FromContext(ctx)

	done, err := IsRolloutDone(ctx, client, obj)
	if err != nil {
		return err
	}

	if !done {
		log.Info("Deployment is currently in a rollout. Skipping.")
		return nil
	}

	if restartTimeInRFC3339 == "" {
		restartTimeInRFC3339 = time.Now().Format(time.RFC3339)
	}

	// TODO: refactor - make a set of functions that, for each type, extracts a pointer to the object's
	// t.Spec.Template. Then in here we can simply have one block of code to set the annotation and issue
	// the client.Patch on t... I think that should work...
	// see here for an example: https://github.com/stakater/Reloader/blob/master/internal/pkg/callbacks/rolling_upgrade.go#L211
	switch t := obj.(type) {
	case *appsv1.Deployment:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
		return client.Patch(ctx, t, patch)
	case *appsv1.DaemonSet:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
		return client.Patch(ctx, t, patch)
	case *appsv1.StatefulSet:
		patch := ctrlclient.StrategicMergeFrom(t.DeepCopy())
		if t.Spec.Template.ObjectMeta.Annotations == nil {
			t.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		t.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInRFC3339
		return client.Patch(ctx, t, patch)
	default:
		return fmt.Errorf(ErrorUnsupportedKind, t)
	}
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
