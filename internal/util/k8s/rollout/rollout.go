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
	RolloutRestartAnnotation = "fortsa.example.com/restartedAt"
)

//+kubebuilder:rbac:groups=apps,resources=deployment;deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonset;daemonsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=replicaset;replicasets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulset;statefulsets,verbs=get;list;watch;update;patch

// DoRolloutRestart handles rollout restart of object by patching with annotation
// TODO: if annotation exists, check status. If rollout failed in some way, report it.
func DoRolloutRestart(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object, dryRun bool) error {
	log := log.FromContext(ctx)
	log.Info("Attempting rollout restart", "obj", obj.GetName())

	restartTimeInNanos := time.Now().Format(time.RFC3339)

	// TODO: figure out how to DRY this code
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "Deployment":
		objX := &appsv1.Deployment{}
		err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		if err != nil {
			return err
		}
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		// TODO: check the annotation and if it exists, check the timestamp. If it's been less than 1 hour,
		// skip updating it to trigger another restart attempt
		if !dryRun {
			objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		}
		return client.Patch(ctx, objX, patch)
	case "DaemonSet":
		objX := &appsv1.DaemonSet{}
		err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		if err != nil {
			return err
		}
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		if !dryRun {
			objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		}
		return client.Patch(ctx, objX, patch)
	case "StatefulSet":
		objX := &appsv1.StatefulSet{}
		err := client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, objX)
		if err != nil {
			return err
		}
		patch := ctrlclient.StrategicMergeFrom(objX.DeepCopy())
		if objX.Spec.Template.ObjectMeta.Annotations == nil {
			objX.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		if !dryRun {
			objX.Spec.Template.ObjectMeta.Annotations[RolloutRestartAnnotation] = restartTimeInNanos
		}
		return client.Patch(ctx, objX, patch)
	default:
		return fmt.Errorf("unsupported Kind %v for rollout restart", obj.GetObjectKind().GroupVersionKind().Kind)
	}
}

// we only want to issue a rollout when any previous rollout is done. any other status -
// error, in-progress, whatever, and we don't want to do another rollout automatically.
func IsRolloutReady(ctx context.Context, client ctrlclient.Client, obj ctrlclient.Object) (bool, error) {
	log := log.FromContext(ctx)
	var revision int64
	statusViewer, err := polymorphichelpers.StatusViewerFor(obj.GetObjectKind().GroupVersionKind().GroupKind())
	if err != nil {
		return false, err
	}
	status, done, err := statusViewer.Status(obj.(runtime.Unstructured), revision)
	if err != nil {
		return false, err
	}
	log.Info("Rollout readiness checked", "RolloutStatus", status)
	return done, nil
}
