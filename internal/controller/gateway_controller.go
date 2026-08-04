package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

const (
	// gatewayImage is the default hanzoai/gateway image (a KrakenD fork).
	gatewayImage = "ghcr.io/hanzoai/gateway:latest"
	// gatewayPort is the in-cluster HTTP port served by the gateway.
	gatewayPort = int32(8080)
	// gatewayConfigDir is where the rendered config is mounted in the container.
	gatewayConfigDir = "/etc/gateway"
	// gatewayConfigKey is both the ConfigMap key and the on-disk file name.
	gatewayConfigKey = "gateway.json"
	// gatewayBinary is the hanzoai/gateway entrypoint.
	gatewayBinary = "/usr/bin/gateway"

	// luxPrimarySubnet is the Lux primary network subnet ID. C/X/P live here and
	// are served by the base routes, so they are skipped during subnet discovery.
	luxPrimarySubnet = "11111111111111111111111111111111LpoYY"

	// defaultClusterRPCPort matches the NodeCluster Service default RPC port.
	defaultClusterRPCPort = int32(8545)

	// gatewayResyncInterval re-runs reconciliation to refresh status and
	// re-discover subnet chains.
	gatewayResyncInterval = 60 * time.Second
)

// gatewayHTTPClient is used for best-effort subnet discovery against a node's
// P-chain RPC. The timeout bounds each reconcile's discovery cost.
var gatewayHTTPClient = &http.Client{Timeout: 10 * time.Second}

// chainRoute is an auto-discovered subnet chain (name + blockchain ID).
type chainRoute struct {
	name string
	id   string
}

// GatewayReconciler reconciles a Gateway object.
type GatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=gateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=nodeclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)

	var gw v1alpha1.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve the referenced NodeCluster (best-effort) to learn its protocol and
	// RPC backend, then auto-discover subnet chains for lux clusters.
	protocol, rpcBackend := "", ""
	var subnetChains []chainRoute
	if gw.Spec.NodeClusterRef != "" {
		var cluster v1alpha1.NodeCluster
		key := client.ObjectKey{Namespace: gw.Namespace, Name: gw.Spec.NodeClusterRef}
		switch err := r.Get(ctx, key, &cluster); {
		case err == nil:
			protocol = cluster.Spec.Protocol
			rpcPort := clusterRPCPort(&cluster)
			rpcBackend = fmt.Sprintf("http://%s-headless.%s.svc:%d", cluster.Name, cluster.Namespace, rpcPort)
			if gw.Spec.AutoRoutes && protocol == "lux" {
				subnetChains = r.discoverSubnetChains(ctx, cluster.Namespace, cluster.Name, rpcPort)
			}
		case apierrors.IsNotFound(err):
			log.Info("Referenced NodeCluster not found; deploying gateway without auto-routes",
				"nodeClusterRef", gw.Spec.NodeClusterRef)
		default:
			return ctrl.Result{}, err
		}
	}

	labels := manifests.StandardLabels(gw.Name, "gateway", "", "")
	selectorLbls := manifests.SelectorLabels(gw.Name)

	// Render the hanzoai/gateway config and reconcile it as a ConfigMap.
	configJSON, routeCount := buildGatewayConfig(&gw, protocol, rpcBackend, subnetChains)
	desiredCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name + "-config",
			Namespace: gw.Namespace,
			Labels:    manifests.StandardLabels(gw.Name, "gateway-config", "", ""),
		},
		Data: map[string]string{gatewayConfigKey: configJSON},
	}
	if err := r.apply(ctx, &gw, desiredCM, &corev1.ConfigMap{}); err != nil {
		return ctrl.Result{}, err
	}

	// Deployment.
	desiredDep := buildGatewayDeployment(&gw, labels, selectorLbls)
	if err := r.apply(ctx, &gw, desiredDep, &appsv1.Deployment{}); err != nil {
		return ctrl.Result{}, err
	}

	// Service (ClusterIP). External access and TLS termination are handled by
	// hanzoai/ingress, never by the gateway itself.
	desiredSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLbls,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := r.apply(ctx, &gw, desiredSvc, &corev1.Service{}); err != nil {
		return ctrl.Result{}, err
	}

	// Ingress via hanzoai/ingress (ingressClassName) with cert-manager TLS.
	if gw.Spec.Ingress != nil && gw.Spec.Ingress.Enabled {
		desiredIng := manifests.BuildIngress(gw.Name, gw.Namespace, gw.Spec.Ingress, gw.Name, 80, labels)
		if err := r.apply(ctx, &gw, desiredIng, &networkingv1.Ingress{}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Status.
	var currentDep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredDep), &currentDep); err == nil {
		gw.Status.ReadyReplicas = currentDep.Status.ReadyReplicas
	}
	gw.Status.ActiveRoutes = int32(routeCount)
	gw.Status.ChainRoutes = chainRouteMap(subnetChains)
	gw.Status.ExternalEndpoint = externalEndpoint(&gw)
	gw.Status.ObservedGeneration = gw.Generation

	replicas := derefInt32(gw.Spec.Replicas, 2)
	if gw.Status.ReadyReplicas == replicas {
		gw.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&gw.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Gateway is ready")
	} else {
		gw.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&gw.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady",
			fmt.Sprintf("%d/%d replicas ready", gw.Status.ReadyReplicas, replicas))
	}

	if err := r.Status().Update(ctx, &gw); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Gateway", "phase", gw.Status.Phase,
		"routes", gw.Status.ActiveRoutes, "chains", len(gw.Status.ChainRoutes))
	return ctrl.Result{RequeueAfter: gatewayResyncInterval}, nil
}

// apply creates or updates desired, owning it via the gateway and mutating the
// live object toward desired. Name/namespace are set before the read so the
// CreateOrUpdate key is stable.
func (r *GatewayReconciler) apply(ctx context.Context, owner *v1alpha1.Gateway, desired, existing client.Object) error {
	existing.SetName(desired.GetName())
	existing.SetNamespace(desired.GetNamespace())
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, existing, func() error {
		if err := manifests.MutateFuncFor(existing, desired)(); err != nil {
			return err
		}
		return ctrl.SetControllerReference(owner, existing, r.Scheme)
	})
	return err
}

// discoverSubnetChains queries a node's P-chain (platform.getBlockchains) and
// returns non-primary subnet chains. It is best-effort: any failure yields nil.
func (r *GatewayReconciler) discoverSubnetChains(ctx context.Context, ns, clusterRef string, rpcPort int32) []chainRoute {
	url := fmt.Sprintf("http://%s-headless.%s.svc:%d/v1/bc/P", clusterRef, ns, rpcPort)
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"platform.getBlockchains","params":{}}`)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := gatewayHTTPClient.Do(httpReq)
	if err != nil {
		r.Log.V(1).Info("subnet discovery failed", "url", url, "error", err.Error())
		return nil
	}
	defer resp.Body.Close()

	var body struct {
		Result struct {
			Blockchains []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				NetID    string `json:"netID"`
				SubnetID string `json:"subnetID"`
			} `json:"blockchains"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}

	var chains []chainRoute
	for _, c := range body.Result.Blockchains {
		// Lux P-chain reports the subnet under netID; fall back to subnetID.
		subnet := c.NetID
		if subnet == "" {
			subnet = c.SubnetID
		}
		if subnet == luxPrimarySubnet || c.ID == "" {
			continue
		}
		name := c.Name
		if name == "" {
			name = "unknown"
		}
		chains = append(chains, chainRoute{name: name, id: c.ID})
	}

	// Deterministic order keeps the rendered config stable across reconciles.
	sort.Slice(chains, func(i, j int) bool {
		if chains[i].name != chains[j].name {
			return chains[i].name < chains[j].name
		}
		return chains[i].id < chains[j].id
	})
	return chains
}

// buildGatewayConfig renders the hanzoai/gateway (KrakenD-schema) JSON config and
// returns it along with the number of configured endpoints.
func buildGatewayConfig(gw *v1alpha1.Gateway, protocol, rpcBackend string, subnetChains []chainRoute) (string, int) {
	endpoints := []map[string]any{}

	if gw.Spec.AutoRoutes && rpcBackend != "" {
		if protocol == "lux" {
			endpoints = append(endpoints,
				gatewayEndpoint("/v1/bc/C/rpc", "/v1/bc/C/rpc", rpcBackend, "POST", endpointRateLimit(gw.Spec.RateLimits)),
				gatewayEndpoint("/v1/bc/C/ws", "/v1/bc/C/ws", rpcBackend, "", nil),
				gatewayEndpoint("/v1/bc/X", "/v1/bc/X", rpcBackend, "POST", nil),
				gatewayEndpoint("/v1/bc/P", "/v1/bc/P", rpcBackend, "POST", nil),
				gatewayEndpoint("/v1/info", "/v1/info", rpcBackend, "POST", nil),
				gatewayEndpoint("/v1/health", "/v1/health", rpcBackend, "", nil),
			)
			for _, c := range subnetChains {
				rpcPath := fmt.Sprintf("/v1/bc/%s/rpc", c.id)
				wsPath := fmt.Sprintf("/v1/bc/%s/ws", c.id)
				endpoints = append(endpoints,
					gatewayEndpoint(rpcPath, rpcPath, rpcBackend, "POST", endpointRateLimit(gw.Spec.RateLimits)),
					gatewayEndpoint(wsPath, wsPath, rpcBackend, "", nil),
				)
			}
		} else {
			// Non-lux clusters: a single JSON-RPC passthrough at the root.
			endpoints = append(endpoints,
				gatewayEndpoint("/rpc", "/", rpcBackend, "POST", endpointRateLimit(gw.Spec.RateLimits)))
		}
	}

	// Indexer API routes (Indexer Services listen on port 8080).
	for _, ref := range gw.Spec.IndexerRefs {
		backend := fmt.Sprintf("http://%s.%s.svc:8080", ref, gw.Namespace)
		endpoints = append(endpoints,
			gatewayEndpoint(fmt.Sprintf("/api/indexer/%s", ref), "/", backend, "", nil))
	}

	// Custom routes from the spec.
	for _, route := range gw.Spec.Routes {
		method := "GET"
		if len(route.Methods) > 0 && route.Methods[0] != "" {
			method = route.Methods[0]
		}
		endpoints = append(endpoints,
			gatewayEndpoint(route.Prefix, route.Prefix, ensureScheme(route.Backend), method, endpointRateLimit(route.RateLimit)))
	}

	extra := map[string]any{
		"router":        map[string]any{"return_error_msg": false},
		"security/cors": corsConfig(gw.Spec.CORS),
	}
	if svc := serviceRateLimit(gw.Spec.RateLimits); svc != nil {
		extra["qos/ratelimit/service"] = svc
	}

	cfg := map[string]any{
		"$schema":         "https://www.krakend.io/schema/v3.json",
		"version":         3,
		"name":            fmt.Sprintf("nchain gateway - %s", gw.Name),
		"port":            gatewayPort,
		"timeout":         "30s",
		"cache_ttl":       "0s",
		"output_encoding": "no-op",
		"extra_config":    extra,
		"endpoints":       endpoints,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", 0
	}
	return string(b), len(endpoints)
}

// gatewayEndpoint builds a no-op passthrough endpoint, optionally with an HTTP
// method restriction and endpoint-level extra config (e.g. rate limiting).
func gatewayEndpoint(endpoint, urlPattern, host, method string, extra map[string]any) map[string]any {
	ep := map[string]any{
		"endpoint":        endpoint,
		"output_encoding": "no-op",
		"backend": []map[string]any{{
			"url_pattern": urlPattern,
			"host":        []string{host},
			"encoding":    "no-op",
		}},
	}
	if method != "" {
		ep["method"] = method
	}
	if len(extra) > 0 {
		ep["extra_config"] = extra
	}
	return ep
}

// corsConfig returns the security/cors block, applying defaults for empty fields.
func corsConfig(c *v1alpha1.GatewayCORS) map[string]any {
	origins := []string{"*"}
	methods := []string{"GET", "POST", "OPTIONS"}
	headers := []string{"Content-Type", "Authorization"}
	if c != nil {
		if len(c.AllowedOrigins) > 0 {
			origins = c.AllowedOrigins
		}
		if len(c.AllowedMethods) > 0 {
			methods = c.AllowedMethods
		}
		if len(c.AllowedHeaders) > 0 {
			headers = c.AllowedHeaders
		}
	}
	return map[string]any{
		"allow_origins":  origins,
		"allow_methods":  methods,
		"allow_headers":  headers,
		"expose_headers": []string{"Content-Length"},
		"max_age":        "12h",
	}
}

// effectiveRate converts a RateLimitConfig into requests per second.
func effectiveRate(rl *v1alpha1.RateLimitConfig) float64 {
	window := time.Second
	if rl.Every != "" {
		if d, err := time.ParseDuration(rl.Every); err == nil && d > 0 {
			window = d
		}
	}
	return float64(rl.MaxRate) / window.Seconds()
}

// endpointRateLimit builds the endpoint-level qos/ratelimit/router config.
func endpointRateLimit(rl *v1alpha1.RateLimitConfig) map[string]any {
	if rl == nil || rl.MaxRate <= 0 {
		return nil
	}
	router := map[string]any{
		"max_rate": effectiveRate(rl),
		"capacity": rl.MaxRate,
	}
	if rl.ClientMaxRate > 0 {
		router["client_max_rate"] = rl.ClientMaxRate
		router["client_capacity"] = rl.ClientMaxRate
		router["strategy"] = "ip"
	}
	return map[string]any{"qos/ratelimit/router": router}
}

// serviceRateLimit builds the service-level qos/ratelimit/service config.
func serviceRateLimit(rl *v1alpha1.RateLimitConfig) map[string]any {
	if rl == nil || rl.MaxRate <= 0 {
		return nil
	}
	return map[string]any{
		"max_rate": effectiveRate(rl),
		"capacity": rl.MaxRate,
	}
}

// ensureScheme prefixes a bare host with http:// when no scheme is present.
func ensureScheme(backend string) string {
	if strings.HasPrefix(backend, "http://") || strings.HasPrefix(backend, "https://") {
		return backend
	}
	return "http://" + backend
}

// chainRouteMap converts discovered chains into a name->blockchainID map.
func chainRouteMap(chains []chainRoute) map[string]string {
	if len(chains) == 0 {
		return nil
	}
	m := make(map[string]string, len(chains))
	for _, c := range chains {
		m[c.name] = c.id
	}
	return m
}

// externalEndpoint derives the externally reachable URL from the ingress host.
func externalEndpoint(gw *v1alpha1.Gateway) string {
	if gw.Spec.Ingress != nil && gw.Spec.Ingress.Enabled && len(gw.Spec.Ingress.Hosts) > 0 {
		scheme := "http"
		if gw.Spec.Ingress.TLS {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s", scheme, gw.Spec.Ingress.Hosts[0])
	}
	return ""
}

// clusterRPCPort returns the NodeCluster Service RPC port.
func clusterRPCPort(cluster *v1alpha1.NodeCluster) int32 {
	if cluster.Spec.Ports.RPC > 0 {
		return cluster.Spec.Ports.RPC
	}
	return defaultClusterRPCPort
}

// buildGatewayDeployment builds the hanzoai/gateway Deployment. The config is
// mounted at /etc/gateway and consumed via `gateway run -c`.
func buildGatewayDeployment(gw *v1alpha1.Gateway, labels, selectorLbls map[string]string) *appsv1.Deployment {
	image := gatewayImage
	pullPolicy := corev1.PullIfNotPresent
	if gw.Spec.Image != nil {
		image = fmt.Sprintf("%s:%s", gw.Spec.Image.Repository, imageTag(gw.Spec.Image.Tag))
		if gw.Spec.Image.PullPolicy != "" {
			pullPolicy = gw.Spec.Image.PullPolicy
		}
	}

	container := corev1.Container{
		Name:            "gateway",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command:         []string{gatewayBinary},
		Args:            []string{"run", "-c", gatewayConfigDir + "/" + gatewayConfigKey, "-d"},
		Env: []corev1.EnvVar{
			{Name: "GATEWAY_PORT", Value: strconv.Itoa(int(gatewayPort))},
		},
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: gatewayPort, Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: gatewayConfigDir, ReadOnly: true},
		},
		ReadinessProbe: httpProbe("/__health", gatewayPort, 5, 5),
		LivenessProbe:  httpProbe("/__health", gatewayPort, 10, 15),
		Resources:      gatewayResources(gw.Spec.Resources),
	}

	replicas := derefInt32(gw.Spec.Replicas, 2)
	terminationGrace := int64(30)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLbls},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       ptrIntStr(1),
					MaxUnavailable: ptrIntStr(0),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: manifests.MergeLabels(labels, selectorLbls)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: gw.Name + "-config"},
							},
						},
					}},
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}
}

func httpProbe(path string, port, initialDelay, period int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromInt32(port)},
		},
		InitialDelaySeconds: initialDelay,
		PeriodSeconds:       period,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

func gatewayResources(r *v1alpha1.ResourceRequirements) corev1.ResourceRequirements {
	if r != nil {
		return corev1.ResourceRequirements{Requests: r.Requests, Limits: r.Limits}
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

func imageTag(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}

func ptrIntStr(v int32) *intstr.IntOrString {
	i := intstr.FromInt32(v)
	return &i
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
