package k8sutil

import (
	"github.com/portworx/talisman/pkg/client/clientset/versioned/scheme"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// CreateRecorder creates a event recorder
func CreateRecorder(kubecli kubernetes.Interface, name, namespace string) record.EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logrus.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: typedcorev1.New(kubecli.Core().RESTClient()).Events(namespace)})
	return eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: name})
}
