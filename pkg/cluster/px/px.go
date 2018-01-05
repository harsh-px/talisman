package px

import (
	"fmt"
	"strings"
	"time"

	portworx "github.com/portworx/talisman/pkg/apis/portworx.com"
	apiv1alpha1 "github.com/portworx/talisman/pkg/apis/portworx.com/v1alpha1"
	clientset "github.com/portworx/talisman/pkg/client/clientset/versioned"
	informers "github.com/portworx/talisman/pkg/client/informers/externalversions"
	listers "github.com/portworx/talisman/pkg/client/listers/portworx.com/v1alpha1"
	"github.com/portworx/talisman/pkg/cluster"
	"github.com/portworx/talisman/pkg/k8sutil"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
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

type pxCluster struct {
	spec        *apiv1alpha1.Cluster
	pxImageRepo string
	pxImageTag  string
}

func (p *pxClusterOps) Create(namespace, name string) error {
	logrus.Infof("[debug] px create call for %s:%s", namespace, name)
	spec, err := p.operatorClient.Portworx().Clusters(namespace).Get(name,
		metav1.GetOptions{})
	if err != nil {
		return err
	}

	logrus.Infof("request to create new px cluster: %#v", spec)

	// TODO add gatekeeper check to ensure only one cluster is running
	logrus.Infof("creating a new portworx cluster: %#v", spec)

	c := &pxCluster{
		spec:        spec,
		pxImageRepo: pxDefaultImageRepo,
		pxImageTag:  pxDefaultImageTag,
	}
	// Get RBAC specs
	_, err = c.getPXClusterRole()
	if err != nil {
		return err
	}

	// Get daemonset spec
	// Get stork spec
	// Get PVC binder spec
	err = p.updateClusterStatus(spec)
	if err != nil {
		return err
	}

	// TODO record event for this cluster
	// p.recorder.Event(cluster, v1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (p *pxClusterOps) Upgrade(namespace, name string) error {
	logrus.Infof("upgrading px cluster")
	return nil
}

func (p *pxClusterOps) Destroy(namespace, name string) error {
	logrus.Infof("destroying px cluster")
	// TODO:  Get cluster

	// TODO: Find all px compoents that have owner as this cluster and delete them
	return nil
}

func (p *pxCluster) getOwnerReference() []metav1.OwnerReference {
	trueVar := true
	return []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: apiv1alpha1.SchemeGroupVersion.String(),
			Kind:       apiv1alpha1.PXClusterKind,
			Name:       p.spec.Name,
			UID:        p.spec.UID,
			Controller: &trueVar,
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
		Subjects: []rbacv1.Subject{rbacv1.Subject{
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
				corev1.ServicePort{
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

func (p *pxCluster) getPXDaemonSet() (*appsv1.DaemonSet, error) {
	trueVar := true
	kvdbEndpoints := strings.Join(p.spec.Spec.Kvdb.Endpoints, ",")

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pxDefaultResourceName,
			Namespace:       pxDefaultNamespace,
			OwnerReferences: p.getOwnerReference(),
			Labels:          pxDefaultLabels,
		},
		Spec: appsv1.DaemonSetSpec{
			MinReadySeconds: 0,
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											corev1.NodeSelectorRequirement{
												Key:      pxEnableLabelKey,
												Operator: corev1.NodeSelectorOpNotIn,
												Values:   []string{"false"},
											},
											corev1.NodeSelectorRequirement{
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
						corev1.Container{
							Name:  pxDefaultResourceName,
							Image: fmt.Sprintf("%s:%s", p.pxImageRepo, p.pxImageTag),
							TerminationMessagePath: "/tmp/px-termination-log",
							ImagePullPolicy:        corev1.PullAlways,
							Args: []string{
								"-k", kvdbEndpoints,
								"-c", p.spec.Name,
								"-a", // TODO take in storage args
								"-f",
								"-x", "kubernetes",
							},
							Env: p.spec.Spec.Env,
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
							/*VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "dockersock",
									MountPath: "/var/run/docker.sock",
								},
								corev1.VolumeMount{
									Name:      "kubelet",
									MountPath: "/var/lib/kubelet:shared",
								},
								corev1.VolumeMount{
									Name:      "libosd",
									MountPath: "/var/lib/osd:shared",
								},
								corev1.VolumeMount{
									Name:      "etcpwx",
									MountPath: "/etc/pwx",
								},
								corev1.VolumeMount{
									Name:      "optpwx",
									MountPath: "/opt/pwx",
								},
								corev1.VolumeMount{
									Name:      "proc1nsmount",
									MountPath: "/host_proc/1/ns",
								},
								corev1.VolumeMount{
									Name:      "sysdmount",
									MountPath: "/etc/systemd/system",
								},
							},*/
						},
					},
					RestartPolicy:      "Always",
					ServiceAccountName: pxDefaultResourceName,
					// TODO: update volummes based on openshift
					Volumes: []corev1.Volume{
						corev1.Volume{
							Name: "dockersock",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/run/docker.sock",
								},
							},
						},
						corev1.Volume{
							Name: "kubelet",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet",
								},
							},
						},
						corev1.Volume{
							Name: "libosd",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/osd",
								},
							},
						},
						corev1.Volume{
							Name: "etcpwx",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/pwx",
								},
							},
						},
						corev1.Volume{
							Name: "optpwx",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/opt/pwx",
								},
							},
						},
						corev1.Volume{
							Name: "proc1nsmount",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/proc/1/ns",
								},
							},
						},
						corev1.Volume{
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

// TODO fix the signature based on px objects
func (p *pxClusterOps) updateClusterStatus(cluster *apiv1alpha1.Cluster) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	// clusterCopy := cluster.DeepCopy()
	// _, err := c.pxoperatorclientset.Portworx().Clusters(cluster.Namespace).Update(clusterCopy)

	// TODO perform additional operations to fetch status. Refer to sample, etcd and rook operators

	return nil
}

// NewPXClusterProvider creates a new PX cluster
func NewPXClusterProvider(conf map[string]interface{}) (cluster.Cluster, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	operatorClient := clientset.NewForConfigOrDie(cfg)
	_ = apiextensionsclient.NewForConfigOrDie(cfg)
	_ = kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	operatorInformerFactory := informers.NewSharedInformerFactory(operatorClient, time.Second*30)
	pxInformer := operatorInformerFactory.Portworx().V1alpha1().Clusters()
	return &pxClusterOps{
		kubeClient:     kubeClient,
		operatorClient: operatorClient,
		recorder:       k8sutil.CreateRecorder(kubeClient, "talisman", ""),
		clustersLister: pxInformer.Lister(),
	}, nil
}

func init() {
	cluster.Register(portworx.GroupName, NewPXClusterProvider)
}
