package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

// ChainReconciler reconciles a Chain object.
type ChainReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=chains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=chains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *ChainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("chain", req.NamespacedName)

	var chain v1alpha1.Chain
	if err := r.Get(ctx, req.NamespacedName, &chain); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Create ConfigMap with genesis data if provided.
	if chain.Spec.Genesis != nil && chain.Spec.Genesis.Raw != nil {
		desiredCM := r.buildGenesisConfigMap(&chain)
		if err := ctrl.SetControllerReference(&chain, desiredCM, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingCM := &corev1.ConfigMap{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingCM, func() error {
			existingCM.Name = desiredCM.Name
			existingCM.Namespace = desiredCM.Namespace
			return manifests.MutateFuncFor(existingCM, desiredCM)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status.
	chain.Status.ObservedGeneration = chain.Generation
	if chain.Status.Phase == "" {
		chain.Status.Phase = v1alpha1.PhaseRunning
	}
	status.SetCondition(&chain.Status.Conditions, v1alpha1.ConditionTypeReady,
		metav1.ConditionTrue, "Configured", "Chain configuration applied")
	status.SetCondition(&chain.Status.Conditions, v1alpha1.ConditionTypeReconciled,
		metav1.ConditionTrue, "Reconciled", "Chain reconciled successfully")

	if err := r.Status().Update(ctx, &chain); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Chain", "chainID", chain.Spec.ChainID, "phase", chain.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *ChainReconciler) buildGenesisConfigMap(chain *v1alpha1.Chain) *corev1.ConfigMap {
	labels := manifests.StandardLabels(chain.Name, "chain-genesis", chain.Name, "")

	data := map[string]string{}
	if chain.Spec.Genesis != nil && chain.Spec.Genesis.Raw != nil {
		// Pretty-print the genesis JSON.
		var raw json.RawMessage
		if err := json.Unmarshal(chain.Spec.Genesis.Raw, &raw); err == nil {
			if pretty, err := json.MarshalIndent(raw, "", "  "); err == nil {
				data["genesis.json"] = string(pretty)
			} else {
				data["genesis.json"] = string(chain.Spec.Genesis.Raw)
			}
		} else {
			data["genesis.json"] = string(chain.Spec.Genesis.Raw)
		}
	}

	if chain.Spec.EVMConfig != nil && chain.Spec.EVMConfig.Raw != nil {
		data["evm-config.json"] = string(chain.Spec.EVMConfig.Raw)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-genesis", chain.Name),
			Namespace: chain.Namespace,
			Labels:    labels,
		},
		Data: data,
	}
}

func (r *ChainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Chain{}).
		Owns(&corev1.ConfigMap{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
