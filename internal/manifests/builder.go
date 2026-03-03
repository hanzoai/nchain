package manifests

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/protocol"
)

// BuildNodeClusterStatefulSet creates a StatefulSet for the blockchain node cluster.
func BuildNodeClusterStatefulSet(cluster *v1alpha1.NodeCluster, driver protocol.Driver) *appsv1.StatefulSet {
	labels := StandardLabels(cluster.Name, "node", cluster.Name, cluster.Spec.Image.Tag)
	labels = MergeLabels(labels, cluster.Spec.Labels)
	selectorLbls := SelectorLabels(cluster.Name)

	// Determine container image.
	image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repository, tagOrDefault(cluster.Spec.Image.Tag))

	// Build command/args from driver.
	command, args := driver.BuildCommand(&cluster.Spec)

	// Build environment.
	env := driver.BuildEnv(&cluster.Spec)
	env = append(env, cluster.Spec.Env...)

	// Build volume mounts.
	volumeMounts := driver.BuildVolumeMounts(&cluster.Spec)

	// Build health probes.
	healthPath, healthPort := driver.HealthEndpoint(&cluster.Spec)
	readyPath, readyPort := driver.ReadinessEndpoint(&cluster.Spec)

	container := corev1.Container{
		Name:         "node",
		Image:        image,
		Command:      command,
		Args:         args,
		Env:          env,
		EnvFrom:      cluster.Spec.EnvFrom,
		VolumeMounts: volumeMounts,
		Ports:        buildContainerPorts(cluster, driver),
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: healthPath,
					Port: intstr.FromInt32(healthPort),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       30,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: readyPath,
					Port: intstr.FromInt32(readyPort),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ImagePullPolicy: cluster.Spec.Image.PullPolicy,
	}

	// Apply resource requirements.
	if cluster.Spec.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: cluster.Spec.Resources.Requests,
			Limits:   cluster.Spec.Resources.Limits,
		}
	}

	// Pre-stop hook for graceful shutdown.
	container.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "sleep 5"},
			},
		},
	}

	// Build volumes.
	volumes := driver.BuildVolumes(&cluster.Spec)

	// Fix ConfigMap volume name reference.
	for i := range volumes {
		if volumes[i].Name == "config" && volumes[i].ConfigMap != nil {
			volumes[i].ConfigMap.Name = cluster.Name + "-config"
		}
	}

	// Init containers from driver.
	initContainers := driver.BuildInitContainers(&cluster.Spec)

	// PVC template for chain data.
	var pvcTemplates []corev1.PersistentVolumeClaim
	if cluster.Spec.Storage != nil {
		pvcTemplates = append(pvcTemplates, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "data",
				Labels: selectorLbls,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: cluster.Spec.Storage.Size,
					},
				},
				StorageClassName: &cluster.Spec.Storage.StorageClassName,
			},
		})
	}

	// Security context.
	var podSecCtx *corev1.PodSecurityContext
	if cluster.Spec.Security.RunAsUser != nil || cluster.Spec.Security.RunAsGroup != nil || cluster.Spec.Security.FSGroup != nil {
		podSecCtx = &corev1.PodSecurityContext{
			RunAsUser:  cluster.Spec.Security.RunAsUser,
			RunAsGroup: cluster.Spec.Security.RunAsGroup,
			FSGroup:    cluster.Spec.Security.FSGroup,
		}
	}

	// Image pull secrets.
	var pullSecrets []corev1.LocalObjectReference
	for _, s := range cluster.Spec.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: s})
	}

	terminationGrace := int64(60)

	// Update strategy.
	updateStrategy := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
	if cluster.Spec.UpgradeStrategy.Type == v1alpha1.UpgradeOnDelete {
		updateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cluster.Name,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: cluster.Spec.Annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:             cluster.Spec.Replicas,
			ServiceName:          cluster.Name + "-headless",
			MinReadySeconds:      10,
			UpdateStrategy:       updateStrategy,
			VolumeClaimTemplates: pvcTemplates,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLbls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MergeLabels(labels, selectorLbls),
					Annotations: cluster.Spec.Annotations,
				},
				Spec: corev1.PodSpec{
					InitContainers:                initContainers,
					Containers:                    []corev1.Container{container},
					Volumes:                       volumes,
					ImagePullSecrets:              pullSecrets,
					SecurityContext:               podSecCtx,
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}
}

// BuildNodeClusterHeadlessService creates a headless Service for StatefulSet DNS.
func BuildNodeClusterHeadlessService(cluster *v1alpha1.NodeCluster) *corev1.Service {
	labels := StandardLabels(cluster.Name, "node", cluster.Name, "")
	selectorLbls := SelectorLabels(cluster.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-headless",
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector:  selectorLbls,
			Ports:     buildServicePorts(cluster),
		},
	}
}

// BuildNodeClusterService creates a ClusterIP Service for external access.
func BuildNodeClusterService(cluster *v1alpha1.NodeCluster) *corev1.Service {
	labels := StandardLabels(cluster.Name, "node", cluster.Name, "")
	selectorLbls := SelectorLabels(cluster.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLbls,
			Ports:    buildServicePorts(cluster),
		},
	}
}

// BuildNodeClusterConfigMap creates a ConfigMap with protocol-specific configuration.
func BuildNodeClusterConfigMap(cluster *v1alpha1.NodeCluster, driver protocol.Driver) *corev1.ConfigMap {
	labels := StandardLabels(cluster.Name, "config", cluster.Name, "")

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-config",
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Data: driver.BuildConfigMap(&cluster.Spec),
	}
}

// BuildIndexerDeployment creates a Deployment for a blockchain data indexer.
func BuildIndexerDeployment(indexer *v1alpha1.Indexer) *appsv1.Deployment {
	labels := StandardLabels(indexer.Name, "indexer", "", indexer.Spec.Image.Tag)
	selectorLbls := SelectorLabels(indexer.Name)

	image := fmt.Sprintf("%s:%s", indexer.Spec.Image.Repository, tagOrDefault(indexer.Spec.Image.Tag))

	container := corev1.Container{
		Name:            "indexer",
		Image:           image,
		ImagePullPolicy: indexer.Spec.Image.PullPolicy,
		Env: []corev1.EnvVar{
			{Name: "CHAIN_REF", Value: indexer.Spec.ChainRef},
			{Name: "NODE_CLUSTER_REF", Value: indexer.Spec.NodeClusterRef},
		},
	}

	if indexer.Spec.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: indexer.Spec.Resources.Requests,
			Limits:   indexer.Spec.Resources.Limits,
		}
	}

	if indexer.Spec.Storage != nil {
		container.VolumeMounts = []corev1.VolumeMount{
			{Name: "data", MountPath: "/data"},
		}
	}

	replicas := int32(1)
	if indexer.Spec.Replicas != nil {
		replicas = *indexer.Spec.Replicas
	}

	terminationGrace := int64(30)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      indexer.Name,
			Namespace: indexer.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLbls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: MergeLabels(labels, selectorLbls),
				},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}

	return dep
}

// BuildExplorerDeployments creates backend and (optionally) frontend Deployments.
func BuildExplorerDeployments(explorer *v1alpha1.Explorer) (*appsv1.Deployment, *appsv1.Deployment) {
	backendLabels := StandardLabels(explorer.Name+"-backend", "explorer-backend", explorer.Name, explorer.Spec.BackendImage.Tag)
	backendSelector := SelectorLabels(explorer.Name + "-backend")

	backendImage := fmt.Sprintf("%s:%s", explorer.Spec.BackendImage.Repository, tagOrDefault(explorer.Spec.BackendImage.Tag))

	backendContainer := corev1.Container{
		Name:            "backend",
		Image:           backendImage,
		ImagePullPolicy: explorer.Spec.BackendImage.PullPolicy,
		Env: []corev1.EnvVar{
			{Name: "CHAIN_REF", Value: explorer.Spec.ChainRef},
		},
	}
	if explorer.Spec.Resources != nil {
		backendContainer.Resources = corev1.ResourceRequirements{
			Requests: explorer.Spec.Resources.Requests,
			Limits:   explorer.Spec.Resources.Limits,
		}
	}

	one := int32(1)
	terminationGrace := int64(30)

	backend := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      explorer.Name + "-backend",
			Namespace: explorer.Namespace,
			Labels:    backendLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: backendSelector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: MergeLabels(backendLabels, backendSelector)},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{backendContainer},
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}

	// Frontend deployment (optional).
	var frontend *appsv1.Deployment
	if explorer.Spec.FrontendImage != nil {
		frontendLabels := StandardLabels(explorer.Name+"-frontend", "explorer-frontend", explorer.Name, explorer.Spec.FrontendImage.Tag)
		frontendSelector := SelectorLabels(explorer.Name + "-frontend")
		frontendImage := fmt.Sprintf("%s:%s", explorer.Spec.FrontendImage.Repository, tagOrDefault(explorer.Spec.FrontendImage.Tag))

		frontendContainer := corev1.Container{
			Name:            "frontend",
			Image:           frontendImage,
			ImagePullPolicy: explorer.Spec.FrontendImage.PullPolicy,
		}

		frontend = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      explorer.Name + "-frontend",
				Namespace: explorer.Namespace,
				Labels:    frontendLabels,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{MatchLabels: frontendSelector},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: MergeLabels(frontendLabels, frontendSelector)},
					Spec: corev1.PodSpec{
						Containers:                    []corev1.Container{frontendContainer},
						TerminationGracePeriodSeconds: &terminationGrace,
					},
				},
			},
		}
	}

	return backend, frontend
}

// BuildGatewayDeployment creates a Deployment for the RPC/API gateway.
func BuildGatewayDeployment(gw *v1alpha1.Gateway) *appsv1.Deployment {
	image := "ghcr.io/hanzoai/gateway:latest"
	if gw.Spec.Image != nil {
		image = fmt.Sprintf("%s:%s", gw.Spec.Image.Repository, tagOrDefault(gw.Spec.Image.Tag))
	}

	labels := StandardLabels(gw.Name, "gateway", "", "")
	selectorLbls := SelectorLabels(gw.Name)

	container := corev1.Container{
		Name:  "gateway",
		Image: image,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: "/etc/krakend", ReadOnly: true},
		},
	}
	if gw.Spec.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: gw.Spec.Resources.Requests,
			Limits:   gw.Spec.Resources.Limits,
		}
	}
	if gw.Spec.Image != nil {
		container.ImagePullPolicy = gw.Spec.Image.PullPolicy
	}

	replicas := int32(2)
	if gw.Spec.Replicas != nil {
		replicas = *gw.Spec.Replicas
	}
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
					MaxSurge:       intStrPtr(1),
					MaxUnavailable: intStrPtr(0),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: MergeLabels(labels, selectorLbls)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: gw.Name + "-config"},
								},
							},
						},
					},
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}
}

// BuildGatewayConfigMap creates a ConfigMap with KrakenD gateway configuration.
func BuildGatewayConfigMap(gw *v1alpha1.Gateway) *corev1.ConfigMap {
	labels := StandardLabels(gw.Name, "gateway-config", "", "")

	// Build KrakenD-compatible JSON configuration from routes.
	config := buildKrakenDConfig(gw)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name + "-config",
			Namespace: gw.Namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			"krakend.json": config,
		},
	}
}

// BuildBridgeDeployments creates server and (optionally) UI Deployments.
func BuildBridgeDeployments(bridge *v1alpha1.Bridge) (*appsv1.Deployment, *appsv1.Deployment) {
	serverLabels := StandardLabels(bridge.Name+"-server", "bridge-server", bridge.Name, bridge.Spec.ServerImage.Tag)
	serverSelector := SelectorLabels(bridge.Name + "-server")
	serverImage := fmt.Sprintf("%s:%s", bridge.Spec.ServerImage.Repository, tagOrDefault(bridge.Spec.ServerImage.Tag))

	serverContainer := corev1.Container{
		Name:            "server",
		Image:           serverImage,
		ImagePullPolicy: bridge.Spec.ServerImage.PullPolicy,
		Env: []corev1.EnvVar{
			{Name: "SOURCE_CHAIN_REF", Value: bridge.Spec.SourceChainRef},
			{Name: "TARGET_CHAIN_REF", Value: bridge.Spec.TargetChainRef},
		},
	}
	if bridge.Spec.Resources != nil {
		serverContainer.Resources = corev1.ResourceRequirements{
			Requests: bridge.Spec.Resources.Requests,
			Limits:   bridge.Spec.Resources.Limits,
		}
	}

	one := int32(1)
	terminationGrace := int64(30)

	server := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bridge.Name + "-server",
			Namespace: bridge.Namespace,
			Labels:    serverLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: serverSelector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: MergeLabels(serverLabels, serverSelector)},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{serverContainer},
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}

	var ui *appsv1.Deployment
	if bridge.Spec.UIImage != nil {
		uiLabels := StandardLabels(bridge.Name+"-ui", "bridge-ui", bridge.Name, bridge.Spec.UIImage.Tag)
		uiSelector := SelectorLabels(bridge.Name + "-ui")
		uiImage := fmt.Sprintf("%s:%s", bridge.Spec.UIImage.Repository, tagOrDefault(bridge.Spec.UIImage.Tag))

		uiContainer := corev1.Container{
			Name:            "ui",
			Image:           uiImage,
			ImagePullPolicy: bridge.Spec.UIImage.PullPolicy,
		}

		ui = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridge.Name + "-ui",
				Namespace: bridge.Namespace,
				Labels:    uiLabels,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{MatchLabels: uiSelector},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: MergeLabels(uiLabels, uiSelector)},
					Spec: corev1.PodSpec{
						Containers:                    []corev1.Container{uiContainer},
						TerminationGracePeriodSeconds: &terminationGrace,
					},
				},
			},
		}
	}

	return server, ui
}

// BuildIngress creates an Ingress resource with cert-manager annotations.
func BuildIngress(name, ns string, spec *v1alpha1.IngressSpec, serviceName string, servicePort int32, labels map[string]string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	annotations := map[string]string{}

	if spec.TLS {
		annotations["cert-manager.io/cluster-issuer"] = spec.ClusterIssuer
	}
	for k, v := range spec.Annotations {
		annotations[k] = v
	}

	var rules []networkingv1.IngressRule
	for _, host := range spec.Hosts {
		rules = append(rules, networkingv1.IngressRule{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: serviceName,
									Port: networkingv1.ServiceBackendPort{Number: servicePort},
								},
							},
						},
					},
				},
			},
		})
	}

	var tls []networkingv1.IngressTLS
	if spec.TLS && len(spec.Hosts) > 0 {
		tls = append(tls, networkingv1.IngressTLS{
			Hosts:      spec.Hosts,
			SecretName: name + "-tls",
		})
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: rules,
			TLS:   tls,
		},
	}

	if spec.IngressClassName != "" {
		ing.Spec.IngressClassName = &spec.IngressClassName
	}

	return ing
}

// BuildPDB creates a PodDisruptionBudget with minAvailable=1.
func BuildPDB(name, ns string, selectorLabels map[string]string) *policyv1.PodDisruptionBudget {
	minAvail := intstr.FromInt32(1)
	labels := StandardLabels(name, "pdb", "", "")

	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pdb",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvail,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
		},
	}
}

// buildServicePorts creates ServicePort entries from a NodeCluster spec.
func buildServicePorts(cluster *v1alpha1.NodeCluster) []corev1.ServicePort {
	ports := []corev1.ServicePort{
		{Name: "rpc", Port: portOrDefault(cluster.Spec.Ports.RPC, 8545), TargetPort: intstr.FromString("rpc"), Protocol: corev1.ProtocolTCP},
		{Name: "p2p", Port: portOrDefault(cluster.Spec.Ports.P2P, 30303), TargetPort: intstr.FromString("p2p"), Protocol: corev1.ProtocolTCP},
		{Name: "metrics", Port: portOrDefault(cluster.Spec.Ports.Metrics, 9090), TargetPort: intstr.FromString("metrics"), Protocol: corev1.ProtocolTCP},
	}

	for _, p := range cluster.Spec.Ports.Additional {
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: intstr.FromString(p.Name),
			Protocol:   p.Protocol,
		})
	}

	return ports
}

// buildContainerPorts creates ContainerPort entries from a NodeCluster spec and driver.
func buildContainerPorts(cluster *v1alpha1.NodeCluster, driver protocol.Driver) []corev1.ContainerPort {
	defaults := driver.DefaultPorts()
	ports := []corev1.ContainerPort{
		{Name: "rpc", ContainerPort: portOrDefault(cluster.Spec.Ports.RPC, defaults.RPC), Protocol: corev1.ProtocolTCP},
		{Name: "p2p", ContainerPort: portOrDefault(cluster.Spec.Ports.P2P, defaults.P2P), Protocol: corev1.ProtocolTCP},
		{Name: "metrics", ContainerPort: portOrDefault(cluster.Spec.Ports.Metrics, defaults.Metrics), Protocol: corev1.ProtocolTCP},
	}

	for _, p := range cluster.Spec.Ports.Additional {
		ports = append(ports, corev1.ContainerPort{
			Name:          p.Name,
			ContainerPort: p.Port,
			Protocol:      p.Protocol,
		})
	}

	// Add driver-default additional ports if not specified in cluster spec.
	for _, dp := range defaults.Additional {
		found := false
		for _, p := range cluster.Spec.Ports.Additional {
			if p.Name == dp.Name {
				found = true
				break
			}
		}
		if !found {
			ports = append(ports, corev1.ContainerPort{
				Name:          dp.Name,
				ContainerPort: dp.Port,
				Protocol:      dp.Protocol,
			})
		}
	}

	return ports
}

// buildKrakenDConfig generates a simplified KrakenD JSON configuration from gateway routes.
func buildKrakenDConfig(gw *v1alpha1.Gateway) string {
	// Build a minimal KrakenD configuration. In production this would be more
	// elaborate, but this provides the skeleton for route configuration.
	var endpoints string
	for i, route := range gw.Spec.Routes {
		if i > 0 {
			endpoints += ","
		}
		endpoints += fmt.Sprintf(`{"endpoint":"%s","backend":[{"url_pattern":"%s","host":["http://%s"]}]}`,
			route.Prefix, route.Prefix, route.Backend)
	}

	return fmt.Sprintf(`{"version":3,"name":"%s","port":8080,"endpoints":[%s]}`, gw.Name, endpoints)
}

func tagOrDefault(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}

func portOrDefault(port, def int32) int32 {
	if port > 0 {
		return port
	}
	return def
}

func intStrPtr(val int32) *intstr.IntOrString {
	v := intstr.FromInt32(val)
	return &v
}

// --- Cloud (bootnode) manifest builders ---

// BuildCloudAPIDeployment creates a Deployment for the Cloud API server.
func BuildCloudAPIDeployment(cloud *v1alpha1.Cloud) *appsv1.Deployment {
	name := cloud.Name + "-api"
	labels := StandardLabels(name, "cloud-api", cloud.Name, "")
	labels = MergeLabels(labels, cloud.Spec.Labels)
	selectorLbls := SelectorLabels(name)

	image := "ghcr.io/hanzoai/bootnode:api-latest"
	pullPolicy := corev1.PullIfNotPresent
	if cloud.Spec.API.Image != nil {
		image = fmt.Sprintf("%s:%s", cloud.Spec.API.Image.Repository, tagOrDefault(cloud.Spec.API.Image.Tag))
		pullPolicy = cloud.Spec.API.Image.PullPolicy
	}

	replicas := derefInt32Ptr(cloud.Spec.API.Replicas, 3)

	// Build env vars: database credentials from secret + brand IAM vars + features.
	env := []corev1.EnvVar{
		{Name: "APP_ENV", Value: "production"},
		{Name: "FEATURE_WALLETS", Value: strconv.FormatBool(cloud.Spec.Features.Wallets)},
		{Name: "FEATURE_BUNDLER", Value: strconv.FormatBool(cloud.Spec.Features.Bundler)},
		{Name: "FEATURE_NFTS", Value: strconv.FormatBool(cloud.Spec.Features.NFTs)},
		{Name: "FEATURE_GAS", Value: strconv.FormatBool(cloud.Spec.Features.Gas)},
		{Name: "FEATURE_WEBHOOKS", Value: strconv.FormatBool(cloud.Spec.Features.Webhooks)},
		{Name: "DATABASE_URL", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: cloud.Spec.Database.CredentialsSecret},
				Key:                  "database-url",
			},
		}},
		{Name: "REDIS_URL", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: cloud.Spec.Database.CredentialsSecret},
				Key:                  "redis-url",
			},
		}},
	}

	// Add per-brand IAM env vars.
	for i, brand := range cloud.Spec.Brands {
		prefix := fmt.Sprintf("BRAND_%d_", i)
		env = append(env,
			corev1.EnvVar{Name: prefix + "NAME", Value: brand.Name},
			corev1.EnvVar{Name: prefix + "IAM_URL", Value: brand.IAM.URL},
			corev1.EnvVar{Name: prefix + "IAM_ORG", Value: brand.IAM.Org},
			corev1.EnvVar{Name: prefix + "IAM_CLIENT_ID", Value: brand.IAM.ClientID},
		)
	}
	env = append(env, cloud.Spec.Env...)

	container := corev1.Container{
		Name:            "api",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 8000, Protocol: corev1.ProtocolTCP},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt32(8000),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       20,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt32(8000),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
	}
	if cloud.Spec.API.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: cloud.Spec.API.Resources.Requests,
			Limits:   cloud.Spec.API.Resources.Limits,
		}
	}

	var pullSecrets []corev1.LocalObjectReference
	for _, s := range cloud.Spec.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: s})
	}

	terminationGrace := int64(30)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cloud.Namespace,
			Labels:      labels,
			Annotations: cloud.Spec.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLbls},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       intStrPtr(1),
					MaxUnavailable: intStrPtr(0),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MergeLabels(labels, selectorLbls),
					Annotations: cloud.Spec.Annotations,
				},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					ImagePullSecrets:              pullSecrets,
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}
}

// BuildCloudWebDeployment creates a Deployment for a single brand's web frontend.
func BuildCloudWebDeployment(cloud *v1alpha1.Cloud, brand v1alpha1.BrandConfig) *appsv1.Deployment {
	name := cloud.Name + "-web-" + brand.Name
	labels := StandardLabels(name, "cloud-web", cloud.Name, "")
	labels = MergeLabels(labels, cloud.Spec.Labels)
	labels["nchain.hanzo.ai/brand"] = brand.Name
	selectorLbls := SelectorLabels(name)

	image := "ghcr.io/hanzoai/bootnode:web-latest"
	pullPolicy := corev1.PullIfNotPresent
	if cloud.Spec.Web.Image != nil {
		image = fmt.Sprintf("%s:%s", cloud.Spec.Web.Image.Repository, tagOrDefault(cloud.Spec.Web.Image.Tag))
		pullPolicy = cloud.Spec.Web.Image.PullPolicy
	}

	replicas := derefInt32Ptr(cloud.Spec.Web.Replicas, 2)

	apiURL := fmt.Sprintf("https://api.%s", brand.Domain)

	env := []corev1.EnvVar{
		{Name: "NODE_ENV", Value: "production"},
		{Name: "NEXT_PUBLIC_BRAND", Value: brand.Name},
		{Name: "NEXT_PUBLIC_API_URL", Value: apiURL},
		{Name: "NEXT_PUBLIC_IAM_URL", Value: brand.IAM.URL},
		{Name: "NEXT_PUBLIC_IAM_CLIENT_ID", Value: brand.IAM.ClientID},
	}
	env = append(env, cloud.Spec.Env...)

	container := corev1.Container{
		Name:            "web",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 3001, Protocol: corev1.ProtocolTCP},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/",
					Port: intstr.FromInt32(3001),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/",
					Port: intstr.FromInt32(3001),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
	}
	if cloud.Spec.Web.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: cloud.Spec.Web.Resources.Requests,
			Limits:   cloud.Spec.Web.Resources.Limits,
		}
	}

	var pullSecrets []corev1.LocalObjectReference
	for _, s := range cloud.Spec.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: s})
	}

	terminationGrace := int64(30)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cloud.Namespace,
			Labels:      labels,
			Annotations: cloud.Spec.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLbls},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       intStrPtr(1),
					MaxUnavailable: intStrPtr(0),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MergeLabels(labels, selectorLbls),
					Annotations: cloud.Spec.Annotations,
				},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					ImagePullSecrets:              pullSecrets,
					TerminationGracePeriodSeconds: &terminationGrace,
				},
			},
		},
	}
}

// BuildCloudService creates a ClusterIP Service for a Cloud component.
func BuildCloudService(name, ns string, port int32, selectorLabels map[string]string) *corev1.Service {
	labels := StandardLabels(name, "cloud", "", "")
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: port, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// BuildCloudHPA creates an HPA for a Cloud component.
func BuildCloudHPA(name, ns string, targetRef string, spec *v1alpha1.AutoscalingSpec) *autoscalingv2.HorizontalPodAutoscaler {
	labels := StandardLabels(name+"-hpa", "cloud-hpa", "", "")
	minReplicas := derefInt32Ptr(spec.MinReplicas, 2)
	maxReplicas := derefInt32Ptr(spec.MaxReplicas, 20)

	var metrics []autoscalingv2.MetricSpec
	if spec.TargetCPU != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: spec.TargetCPU,
				},
			},
		})
	}
	if spec.TargetMemory != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: spec.TargetMemory,
				},
			},
		})
	}

	// Aggressive scale-up, conservative scale-down.
	scaleUp := int32(60)   // 60s stabilization for scale-up
	scaleDown := int32(300) // 5min stabilization for scale-down

	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-hpa",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetRef,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics:     metrics,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleUp,
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleDown,
				},
			},
		},
	}
}

// BuildCloudIngress creates an Ingress resource for a single brand.
// It routes web3.{domain} to web, api.web3.{domain} to API, ws.web3.{domain} to API.
func BuildCloudIngress(cloud *v1alpha1.Cloud, brand v1alpha1.BrandConfig) *networkingv1.Ingress {
	name := cloud.Name + "-" + brand.Name
	labels := StandardLabels(name, "cloud-ingress", cloud.Name, "")
	labels["nchain.hanzo.ai/brand"] = brand.Name

	pathType := networkingv1.PathTypePrefix

	webSvcName := cloud.Name + "-web-" + brand.Name
	apiSvcName := cloud.Name + "-api"

	annotations := map[string]string{}
	if cloud.Spec.Ingress != nil {
		if cloud.Spec.Ingress.TLS {
			clusterIssuer := cloud.Spec.Ingress.ClusterIssuer
			if clusterIssuer == "" {
				clusterIssuer = "letsencrypt-prod"
			}
			annotations["cert-manager.io/cluster-issuer"] = clusterIssuer
		}
		for k, v := range cloud.Spec.Ingress.Annotations {
			annotations[k] = v
		}
	}

	webHost := brand.Domain
	apiHost := "api." + brand.Domain
	wsHost := "ws." + brand.Domain

	rules := []networkingv1.IngressRule{
		{
			Host: webHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: webSvcName,
								Port: networkingv1.ServiceBackendPort{Number: 3001},
							},
						},
					}},
				},
			},
		},
		{
			Host: apiHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: apiSvcName,
								Port: networkingv1.ServiceBackendPort{Number: 8000},
							},
						},
					}},
				},
			},
		},
		{
			Host: wsHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: apiSvcName,
								Port: networkingv1.ServiceBackendPort{Number: 8000},
							},
						},
					}},
				},
			},
		},
	}

	hosts := []string{webHost, apiHost, wsHost}
	var tls []networkingv1.IngressTLS
	if cloud.Spec.Ingress != nil && cloud.Spec.Ingress.TLS {
		tls = append(tls, networkingv1.IngressTLS{
			Hosts:      hosts,
			SecretName: name + "-tls",
		})
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cloud.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: rules,
			TLS:   tls,
		},
	}

	if cloud.Spec.Ingress != nil && cloud.Spec.Ingress.IngressClassName != "" {
		ing.Spec.IngressClassName = &cloud.Spec.Ingress.IngressClassName
	}

	return ing
}

func derefInt32Ptr(p *int32, def int32) int32 {
	if p != nil {
		return *p
	}
	return def
}
