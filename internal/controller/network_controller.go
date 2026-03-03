package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/status"
)

// NetworkReconciler reconciles a Network object. It is the top-level composer
// that creates and manages all child CRDs (NodeClusters, Chains, Indexers,
// Explorers, Bridges, Gateways).
type NetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=networks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=networks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=nodeclusters;chains;indexers;explorers;bridges;gateways,verbs=get;list;watch;create;update;patch;delete

func (r *NetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("network", req.NamespacedName)

	var network v1alpha1.Network
	if err := r.Get(ctx, req.NamespacedName, &network); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile child NodeClusters.
	for _, named := range network.Spec.Clusters {
		child := &v1alpha1.NodeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "nodecluster"),
			},
			Spec: named.Spec,
		}
		// Inherit protocol and networkID if not set.
		if child.Spec.Protocol == "" {
			child.Spec.Protocol = network.Spec.Protocol
		}
		if child.Spec.NetworkID == "" {
			child.Spec.NetworkID = network.Spec.NetworkID
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile child Chains.
	for _, named := range network.Spec.Chains {
		child := &v1alpha1.Chain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "chain"),
			},
			Spec: named.Spec,
		}
		if child.Spec.Protocol == "" {
			child.Spec.Protocol = network.Spec.Protocol
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile child Indexers.
	for _, named := range network.Spec.Indexers {
		child := &v1alpha1.Indexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "indexer"),
			},
			Spec: named.Spec,
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile child Explorers.
	for _, named := range network.Spec.Explorers {
		child := &v1alpha1.Explorer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "explorer"),
			},
			Spec: named.Spec,
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile child Bridges.
	for _, named := range network.Spec.Bridges {
		child := &v1alpha1.Bridge{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "bridge"),
			},
			Spec: named.Spec,
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile child Gateways.
	for _, named := range network.Spec.Gateways {
		child := &v1alpha1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
				Namespace: network.Namespace,
				Labels:    mergeWithNetworkLabels(&network, named.Name, "gateway"),
			},
			Spec: named.Spec,
		}
		if err := r.reconcileChild(ctx, &network, child); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Aggregate status from children.
	network.Status.ObservedGeneration = network.Generation
	network.Status.ClusterCount = int32(len(network.Spec.Clusters))
	network.Status.ChainCount = int32(len(network.Spec.Chains))

	// Count ready clusters.
	var readyClusters int32
	for _, named := range network.Spec.Clusters {
		var child v1alpha1.NodeCluster
		key := client.ObjectKey{
			Name:      fmt.Sprintf("%s-%s", network.Name, named.Name),
			Namespace: network.Namespace,
		}
		if err := r.Get(ctx, key, &child); err == nil {
			if child.Status.Phase == v1alpha1.PhaseRunning {
				readyClusters++
			}
		}
	}
	network.Status.ReadyClusters = readyClusters

	// Determine overall phase.
	if readyClusters == network.Status.ClusterCount && network.Status.ClusterCount > 0 {
		network.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&network.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "AllClustersReady", "All node clusters are running")
	} else if readyClusters > 0 {
		network.Status.Phase = v1alpha1.PhaseBootstrapping
		status.SetCondition(&network.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "PartiallyReady",
			fmt.Sprintf("%d/%d clusters ready", readyClusters, network.Status.ClusterCount))
	} else {
		network.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&network.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "Creating", "Network resources are being created")
	}

	if err := r.Status().Update(ctx, &network); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Network", "phase", network.Status.Phase,
		"clusters", network.Status.ClusterCount, "ready", readyClusters)

	return ctrl.Result{}, nil
}

// reconcileChild creates or updates a child resource with owner reference.
func (r *NetworkReconciler) reconcileChild(ctx context.Context, network *v1alpha1.Network, child client.Object) error {
	if err := ctrl.SetControllerReference(network, child, r.Scheme); err != nil {
		return err
	}

	existing := child.DeepCopyObject().(client.Object)
	err := r.Get(ctx, client.ObjectKeyFromObject(child), existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, child)
	}
	if err != nil {
		return err
	}

	// Merge labels.
	existingLabels := existing.GetLabels()
	if existingLabels == nil {
		existingLabels = make(map[string]string)
	}
	for k, v := range child.GetLabels() {
		existingLabels[k] = v
	}
	existing.SetLabels(existingLabels)

	return r.Update(ctx, existing)
}

// mergeWithNetworkLabels builds labels for a network child resource.
func mergeWithNetworkLabels(network *v1alpha1.Network, name, component string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/part-of":    network.Name,
		"app.kubernetes.io/managed-by": "nchain-operator",
		"nchain.hanzo.ai/network":      network.Name,
	}
	for k, v := range network.Spec.Labels {
		labels[k] = v
	}
	return labels
}

func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Network{}).
		Owns(&v1alpha1.NodeCluster{}).
		Owns(&v1alpha1.Chain{}).
		Owns(&v1alpha1.Indexer{}).
		Owns(&v1alpha1.Explorer{}).
		Owns(&v1alpha1.Bridge{}).
		Owns(&v1alpha1.Gateway{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
