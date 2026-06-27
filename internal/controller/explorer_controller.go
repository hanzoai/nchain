package controller

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

// defaultExplorerIngressClass is the ingress class used when an Explorer's
// ingress does not specify one. The Hanzo/Lux stack routes through hanzoai/ingress.
const defaultExplorerIngressClass = "hanzo"

// ExplorerReconciler reconciles an Explorer object.
type ExplorerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=explorers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=explorers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=indexers,verbs=get;list;watch
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=chains,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *ExplorerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("explorer", req.NamespacedName)

	var explorer v1alpha1.Explorer
	if err := r.Get(ctx, req.NamespacedName, &explorer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Build backend and frontend deployments, then wire the explorer to its
	// indexers (graph/indexer API) and branding via environment variables.
	backendDep, frontendDep := manifests.BuildExplorerDeployments(&explorer)
	explorerEnv := r.buildExplorerEnv(ctx, &explorer)
	backendDep.Spec.Template.Spec.Containers[0].Env = append(
		backendDep.Spec.Template.Spec.Containers[0].Env, explorerEnv...)
	if frontendDep != nil {
		frontendDep.Spec.Template.Spec.Containers[0].Env = append(
			frontendDep.Spec.Template.Spec.Containers[0].Env, explorerEnv...)
	}

	// Reconcile backend Deployment.
	if err := ctrl.SetControllerReference(&explorer, backendDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingBackend := &appsv1.Deployment{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingBackend, func() error {
		existingBackend.Name = backendDep.Name
		existingBackend.Namespace = backendDep.Namespace
		return manifests.MutateFuncFor(existingBackend, backendDep)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Backend Service.
	backendLabels := manifests.StandardLabels(explorer.Name+"-backend", "explorer-backend", explorer.Name, "")
	backendSelector := manifests.SelectorLabels(explorer.Name + "-backend")
	backendSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      explorer.Name + "-backend",
			Namespace: explorer.Namespace,
			Labels:    backendLabels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: backendSelector,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 4000, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := ctrl.SetControllerReference(&explorer, backendSvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingBackendSvc := &corev1.Service{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingBackendSvc, func() error {
		existingBackendSvc.Name = backendSvc.Name
		existingBackendSvc.Namespace = backendSvc.Namespace
		return manifests.MutateFuncFor(existingBackendSvc, backendSvc)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile frontend if present.
	if frontendDep != nil {
		if err := ctrl.SetControllerReference(&explorer, frontendDep, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingFrontend := &appsv1.Deployment{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingFrontend, func() error {
			existingFrontend.Name = frontendDep.Name
			existingFrontend.Namespace = frontendDep.Namespace
			return manifests.MutateFuncFor(existingFrontend, frontendDep)()
		}); err != nil {
			return ctrl.Result{}, err
		}

		// Frontend Service.
		frontendLabels := manifests.StandardLabels(explorer.Name+"-frontend", "explorer-frontend", explorer.Name, "")
		frontendSelector := manifests.SelectorLabels(explorer.Name + "-frontend")
		frontendSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      explorer.Name + "-frontend",
				Namespace: explorer.Namespace,
				Labels:    frontendLabels,
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: frontendSelector,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 3000, Protocol: corev1.ProtocolTCP},
				},
			},
		}
		if err := ctrl.SetControllerReference(&explorer, frontendSvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingFrontendSvc := &corev1.Service{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingFrontendSvc, func() error {
			existingFrontendSvc.Name = frontendSvc.Name
			existingFrontendSvc.Namespace = frontendSvc.Namespace
			return manifests.MutateFuncFor(existingFrontendSvc, frontendSvc)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Build Ingress if enabled.
	if explorer.Spec.Ingress != nil && explorer.Spec.Ingress.Enabled {
		// Route ingress to frontend if available, else backend.
		ingressTarget := explorer.Name + "-backend"
		ingressPort := int32(4000)
		if frontendDep != nil {
			ingressTarget = explorer.Name + "-frontend"
			ingressPort = 3000
		}
		// Default the ingress class to the Hanzo/Lux stack ingress.
		ingSpec := *explorer.Spec.Ingress
		if ingSpec.IngressClassName == "" {
			ingSpec.IngressClassName = defaultExplorerIngressClass
		}
		labels := manifests.StandardLabels(explorer.Name, "explorer", "", "")
		desiredIng := manifests.BuildIngress(explorer.Name, explorer.Namespace, &ingSpec,
			ingressTarget, ingressPort, labels)
		if err := ctrl.SetControllerReference(&explorer, desiredIng, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingIng := &networkingv1.Ingress{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingIng, func() error {
			existingIng.Name = desiredIng.Name
			existingIng.Namespace = desiredIng.Namespace
			return manifests.MutateFuncFor(existingIng, desiredIng)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status. ReadyReplicas tracks the deployment the ingress targets
	// (frontend when present, else backend).
	var currentBackend appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(backendDep), &currentBackend); err == nil {
		explorer.Status.BackendReady = currentBackend.Status.ReadyReplicas > 0
	}
	explorer.Status.ReadyReplicas = currentBackend.Status.ReadyReplicas
	if frontendDep != nil {
		var currentFrontend appsv1.Deployment
		if err := r.Get(ctx, client.ObjectKeyFromObject(frontendDep), &currentFrontend); err == nil {
			explorer.Status.FrontendReady = currentFrontend.Status.ReadyReplicas > 0
			explorer.Status.ReadyReplicas = currentFrontend.Status.ReadyReplicas
		}
	} else {
		explorer.Status.FrontendReady = true
	}

	// URL is derived from the ingress host.
	if explorer.Spec.Ingress != nil && explorer.Spec.Ingress.Enabled && len(explorer.Spec.Ingress.Hosts) > 0 {
		explorer.Status.URL = "https://" + explorer.Spec.Ingress.Hosts[0]
	}

	explorer.Status.ObservedGeneration = explorer.Generation
	if explorer.Status.BackendReady && explorer.Status.FrontendReady {
		explorer.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&explorer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Explorer is ready")
	} else {
		explorer.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&explorer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady", "Explorer components are starting")
	}

	if err := r.Status().Update(ctx, &explorer); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Explorer", "phase", explorer.Status.Phase)
	return ctrl.Result{}, nil
}

// buildExplorerEnv ports the Rust operator's explorer wiring: each indexer ref
// becomes CHAIN_{i}_NAME / CHAIN_{i}_INDEXER, the default ref drives NETWORK_ID
// and the read API host, and branding/NEXT_PUBLIC_* configure the explorer UI.
func (r *ExplorerReconciler) buildExplorerEnv(ctx context.Context, explorer *v1alpha1.Explorer) []corev1.EnvVar {
	ns := explorer.Namespace
	refs := explorer.Spec.IndexerRefs

	// Default indexer ref: first marked default, else the first overall.
	var defaultRef *v1alpha1.ExplorerIndexerRef
	for i := range refs {
		if refs[i].Default {
			defaultRef = &refs[i]
			break
		}
	}
	if defaultRef == nil && len(refs) > 0 {
		defaultRef = &refs[0]
	}

	// NETWORK_ID comes from the default indexer's chain (fallback "1").
	networkID := "1"
	if defaultRef != nil {
		if id := r.indexerChainID(ctx, ns, defaultRef.IndexerName); id != "" {
			networkID = id
		}
	}

	// App host from the ingress, else the in-cluster service DNS.
	appHost := fmt.Sprintf("%s-explorer.%s.svc", explorer.Name, ns)
	if explorer.Spec.Ingress != nil && len(explorer.Spec.Ingress.Hosts) > 0 {
		appHost = explorer.Spec.Ingress.Hosts[0]
	}

	// API host is the default indexer's graph/indexer endpoint.
	apiHost := fmt.Sprintf("indexer-c-indexer.%s.svc:4000", ns)
	if defaultRef != nil {
		apiHost = indexerEndpoint(defaultRef.IndexerName, ns)
	}

	networkName := "Lux Network"
	if explorer.Spec.Branding != nil && explorer.Spec.Branding.NetworkName != "" {
		networkName = explorer.Spec.Branding.NetworkName
	}

	env := make([]corev1.EnvVar, 0, len(refs)*2+9)
	for i, ref := range refs {
		env = append(env,
			corev1.EnvVar{Name: fmt.Sprintf("CHAIN_%d_NAME", i), Value: ref.DisplayName},
			corev1.EnvVar{Name: fmt.Sprintf("CHAIN_%d_INDEXER", i), Value: "http://" + indexerEndpoint(ref.IndexerName, ns)},
		)
	}
	env = append(env,
		corev1.EnvVar{Name: "CHAIN_COUNT", Value: strconv.Itoa(len(refs))},
		corev1.EnvVar{Name: "NEXT_PUBLIC_APP_HOST", Value: appHost},
		corev1.EnvVar{Name: "NEXT_PUBLIC_NETWORK_NAME", Value: networkName},
		corev1.EnvVar{Name: "NEXT_PUBLIC_NETWORK_ID", Value: networkID},
		corev1.EnvVar{Name: "NEXT_PUBLIC_API_HOST", Value: "http://" + apiHost},
		corev1.EnvVar{Name: "NEXT_PUBLIC_APP_PROTOCOL", Value: "https"},
		corev1.EnvVar{Name: "NEXT_PUBLIC_API_PROTOCOL", Value: "http"},
	)
	if b := explorer.Spec.Branding; b != nil {
		env = append(env, corev1.EnvVar{Name: "NETWORK_NAME", Value: b.NetworkName})
		if b.LogoURL != "" {
			env = append(env, corev1.EnvVar{Name: "LOGO_URL", Value: b.LogoURL})
		}
	}
	return env
}

// indexerChainID resolves an Indexer's numeric chain ID via its referenced
// Chain. It is best-effort: any lookup failure yields "" so the caller falls
// back to a default.
func (r *ExplorerReconciler) indexerChainID(ctx context.Context, ns, indexerName string) string {
	var idx v1alpha1.Indexer
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: indexerName}, &idx); err != nil {
		return ""
	}
	if idx.Spec.ChainRef == "" {
		return ""
	}
	var chain v1alpha1.Chain
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: idx.Spec.ChainRef}, &chain); err != nil {
		return ""
	}
	return chain.Spec.ChainID
}

// indexerEndpoint returns the in-cluster host:port of an indexer's graph/indexer
// HTTP API, matching the operator's indexer service naming convention.
func indexerEndpoint(indexerName, ns string) string {
	return fmt.Sprintf("%s-indexer.%s.svc:4000", indexerName, ns)
}

func (r *ExplorerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Explorer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
