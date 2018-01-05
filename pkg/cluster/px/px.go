package px

import (
	"fmt"
	"strings"
	"time"

	apiv1alpha1 "github.com/portworx/talisman/pkg/apis/portworx.com/v1alpha1"
	clientset "github.com/portworx/talisman/pkg/client/clientset/versioned"
	informers "github.com/portworx/talisman/pkg/client/informers/externalversions"
	listers "github.com/portworx/talisman/pkg/client/listers/portworx.com/v1alpha1"
	"github.com/portworx/talisman/pkg/k8sutil"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
)

const (
	pxDefaultNamespace       = "kube-system"
	pxDefaultResourceName    = "portworx"
	pxClusterServiceName     = "portworx-service"
	pxRestEndpointPort       = 9001
	pxRestHealthEndpointPort = 9015
	pxEnableLabelKey         = "px/enabled"
	pxDefaultImageRepo       = "portworx/oci-monitor"
	pxDefaultImageTag        = "1.2.12.0"
)

var pxDefaultLabels = map[string]string{"name": pxDefaultResourceName}

type pxClusterOps struct {
	kubeClient     kubernetes.Interface
	operatorClient clientset.Interface
	recorder       record.EventRecorder
	clustersLister listers.ClusterLister
}

// Cluster an interface to manage a storage cluster
type Cluster interface {
	// Create creates the given cluster
	Create(c *apiv1alpha1.Cluster) error
	// Status returns the current status of the given cluster
	Status(c *apiv1alpha1.Cluster) (*apiv1alpha1.ClusterStatus, error)
	// Upgrade upgrades the given cluster
	Upgrade(c *apiv1alpha1.Cluster) error
	// Destory destroys all components of the given cluster
	Destroy(c *apiv1alpha1.Cluster) error
}

type pxCluster struct {
	spec        *apiv1alpha1.Cluster
	pxImageRepo string
	pxImageTag  string
}

func (ops *pxClusterOps) Create(spec *apiv1alpha1.Cluster) error {
	logrus.Infof("request to create new px cluster: %#v", spec)

	name := spec.Name
	namespace := spec.Namespace
	cls, err := ops.operatorClient.Portworx().Clusters("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(cls.Items) > 1 {
		policy := metav1.DeletePropagationForeground
		err = ops.operatorClient.Portworx().Clusters(namespace).Delete(name,
			&metav1.DeleteOptions{
				PropagationPolicy: &policy,
			})
		if err != nil {
			return err
		}

		return fmt.Errorf("failed creating new px cluster: %s. "+
			" only one portworx cluster is supported.", spec.Name)
	}

	logrus.Infof("creating a new portworx cluster: %#v", spec)

	c := &pxCluster{
		spec:        spec,
		pxImageRepo: pxDefaultImageRepo,
		pxImageTag:  pxDefaultImageTag,
	}
	// RBAC
	role, err := c.getPXClusterRole()
	if err != nil {
		return err
	}

	role, err = ops.kubeClient.RbacV1().ClusterRoles().Create(role)
	if err != nil {
		return err
	}

	sa, err := c.getServiceAccount()
	if err != nil {
		return err
	}

	sa, err = ops.kubeClient.CoreV1().ServiceAccounts(sa.Namespace).Create(sa)
	if err != nil {
		return err
	}

	binding, err := c.getPXClusterRoleBinding()
	if err != nil {
		return err
	}

	binding, err = ops.kubeClient.RbacV1().ClusterRoleBindings().Create(binding)
	if err != nil {
		return err
	}

	logrus.Infof("[debug] created clusterrolebinging: %#v", binding)

	// DaemonSet
	ds, err := c.getPXDaemonSet()
	if err != nil {
		return err
	}

	ds, err = ops.kubeClient.Extensions().DaemonSets(ds.Namespace).Create(ds)
	if err != nil {
		logrus.Errorf("failed creating daemonset: %s. Err: %s", ds.Name, err)
		return err
	}

	logrus.Infof("created daemonset: %#v", ds)

	// Service
	svc, err := c.getPXService()
	if err != nil {
		return err
	}

	svc, err = ops.kubeClient.CoreV1().Services(svc.Namespace).Create(svc)
	if err != nil {
		return err
	}

	// TODO Get stork spec
	// TODO Get PVC binder spec

	// TODO record event for this cluster
	return nil
}

func (p *pxClusterOps) Status(c *apiv1alpha1.Cluster) (*apiv1alpha1.ClusterStatus, error) {
	return nil, nil
}

func (ops *pxClusterOps) Upgrade(new *apiv1alpha1.Cluster) error {
	logrus.Infof("upgrading px cluster")
	// TODO check if anything needs to be changed in the ds spec
	// TODO check px target px version is supported
	c := &pxCluster{
		spec:        new,
		pxImageRepo: pxDefaultImageRepo,
		pxImageTag:  pxDefaultImageTag,
	}

	// DaemonSet
	ds, err := c.getPXDaemonSet()
	if err != nil {
		return err
	}

	ds, err = ops.kubeClient.Extensions().DaemonSets(ds.Namespace).Update(ds)
	if err != nil {
		logrus.Errorf("failed updating daemonset: %s. Err: %s", ds.Name, err)
		return err
	}
	return nil
}

func (ops *pxClusterOps) Destroy(c *apiv1alpha1.Cluster) error {
	logrus.Infof("destroying px cluster")
	return nil
}

func (p *pxCluster) getOwnerReference() []metav1.OwnerReference {
	trueVar := true
	return []metav1.OwnerReference{
		{
			APIVersion:         apiv1alpha1.SchemeGroupVersion.String(),
			Kind:               apiv1alpha1.PXClusterKind,
			Name:               p.spec.Name,
			UID:                p.spec.UID,
			Controller:         &trueVar,
			BlockOwnerDeletion: &trueVar,
		},
	}
}

func (p *pxCluster) getServiceAccount() (*corev1.ServiceAccount, error) {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxDefaultResourceName,
			Namespace:       pxDefaultNamespace,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
	}, nil
}

func (p *pxCluster) getPXClusterRole() (*rbacv1.ClusterRole, error) {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"watch", "get", "update", "list"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{"get", "list"},
		},
	}

	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxDefaultResourceName,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
		Rules: rules,
	}, nil
}

func (p *pxCluster) getPXClusterRoleBinding() (*rbacv1.ClusterRoleBinding, error) {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxDefaultResourceName,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      pxDefaultResourceName,
			Namespace: pxDefaultNamespace,
		}},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: pxDefaultResourceName,
		},
	}, nil
}

func (p *pxCluster) getPXService() (*corev1.Service, error) {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxClusterServiceName,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: pxDefaultLabels,
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.Protocol("TCP"),
					Port:     pxRestEndpointPort,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: pxRestEndpointPort,
					},
				},
			},
		},
	}, nil
}

func (p *pxCluster) getPXDaemonSet() (*extensionsv1beta1.DaemonSet, error) {
	trueVar := true

	kvdbEndpoints := strings.Join(p.spec.Spec.Kvdb.Endpoints, ",")
	args := []string{"-k", kvdbEndpoints, "-c", p.spec.Name, "-x", "kubernetes"}

	if len(p.spec.Spec.PXVersion) > 0 {
		p.pxImageTag = p.spec.Spec.PXVersion
	}

	// storage
	if len(p.spec.Spec.Storage.Devices) > 0 {
		for _, d := range p.spec.Spec.Storage.Devices {
			args = append(args, "-s", d)
		}
	} else if p.spec.Spec.Storage.ZeroStorage {
		args = append(args, "-z")
	} else {
		logrus.Infof("defaulting to using all devices for cluster: %s", p.spec.Name)
		args = append(args, "-a")

		if p.spec.Spec.Storage.UseAllWithParitions {
			args = append(args, "-F")
		} else if p.spec.Spec.Storage.Force {
			args = append(args, "-f")
		}
	}

	// network
	if len(p.spec.Spec.Network.Data) > 0 {
		args = append(args, "-d", p.spec.Spec.Network.Data)
	}

	if len(p.spec.Spec.Network.Mgmt) > 0 {
		args = append(args, "-m", p.spec.Spec.Network.Mgmt)
	}

	return &extensionsv1beta1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxDefaultResourceName,
			Namespace:       pxDefaultNamespace,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
		Spec: extensionsv1beta1.DaemonSetSpec{
			MinReadySeconds: 0,
			UpdateStrategy: extensionsv1beta1.DaemonSetUpdateStrategy{
				Type: extensionsv1beta1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &extensionsv1beta1.RollingUpdateDaemonSet{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: pxDefaultLabels,
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      pxEnableLabelKey,
												Operator: corev1.NodeSelectorOpNotIn,
												Values:   []string{"false"},
											},
											{
												Key:      "node-role.kubernetes.io/master",
												Operator: corev1.NodeSelectorOpDoesNotExist,
											},
										},
									},
								},
							},
						},
					},
					HostNetwork: true,
					HostPID:     true,
					Containers: []corev1.Container{
						{
							Name:  pxDefaultResourceName,
							Image: fmt.Sprintf("%s:%s", p.pxImageRepo, p.pxImageTag),
							TerminationMessagePath: "/tmp/px-termination-log",
							ImagePullPolicy:        corev1.PullAlways,
							Args:                   args,
							Env:                    p.spec.Spec.Env,
							LivenessProbe: &corev1.Probe{
								PeriodSeconds:       30,
								InitialDelaySeconds: 840,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Host: "127.0.0.1",
										Path: "/status",
										Port: intstr.IntOrString{
											Type:   intstr.Int,
											IntVal: pxRestEndpointPort,
										},
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								PeriodSeconds: 10,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Host: "127.0.0.1",
										Path: "/health",
										Port: intstr.IntOrString{
											Type:   intstr.Int,
											IntVal: pxRestHealthEndpointPort,
										},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &trueVar,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dockersock",
									MountPath: "/var/run/docker.sock",
								},
								{
									Name:      "kubelet",
									MountPath: "/var/lib/kubelet:shared",
								},
								{
									Name:      "libosd",
									MountPath: "/var/lib/osd:shared",
								},
								{
									Name:      "etcpwx",
									MountPath: "/etc/pwx",
								},
								{
									Name:      "optpwx",
									MountPath: "/opt/pwx",
								},
								{
									Name:      "proc1nsmount",
									MountPath: "/host_proc/1/ns",
								},
								{
									Name:      "sysdmount",
									MountPath: "/etc/systemd/system",
								},
							},
						},
					},
					RestartPolicy:      "Always",
					ServiceAccountName: pxDefaultResourceName,
					// TODO: update volummes based on openshift
					Volumes: []corev1.Volume{
						{
							Name: "dockersock",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/run/docker.sock",
								},
							},
						},
						{
							Name: "kubelet",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet",
								},
							},
						},
						{
							Name: "libosd",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/osd",
								},
							},
						},
						{
							Name: "etcpwx",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/pwx",
								},
							},
						},
						{
							Name: "optpwx",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/opt/pwx",
								},
							},
						},
						{
							Name: "proc1nsmount",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/proc/1/ns",
								},
							},
						},
						{
							Name: "sysdmount",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/systemd/system",
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func (p *pxCluster) getPVCController() (*corev1.ServiceAccount,
	*rbacv1.ClusterRole,
	*rbacv1.ClusterRoleBinding,
	*appsv1.Deployment, error) {
	return nil, nil, nil, nil, nil
}

// NewPXClusterProvider creates a new PX cluster
func NewPXClusterProvider(conf map[string]interface{}) (Cluster, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	operatorClient := clientset.NewForConfigOrDie(cfg)
	operatorInformerFactory := informers.NewSharedInformerFactory(operatorClient, time.Second*30)
	pxInformer := operatorInformerFactory.Portworx().V1alpha1().Clusters()
	return &pxClusterOps{
		kubeClient:     kubeClient,
		operatorClient: operatorClient,
		recorder:       k8sutil.CreateRecorder(kubeClient, "talisman", ""),
		clustersLister: pxInformer.Lister(),
	}, nil
}
