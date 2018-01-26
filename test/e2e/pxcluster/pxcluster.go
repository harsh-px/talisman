package pxcluster

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"testing"

	g "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	apiv1alpha1 "github.com/portworx/talisman/pkg/apis/portworx.com/v1alpha1"
	"github.com/portworx/talisman/pkg/cluster/px"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	pxClusterProvider px.Cluster
	pxKvdbEndpoints   string
	pxDataInterface   string
	pxMgmtInterface   string
)

var (
	expect       = gomega.Expect
	haveOccurred = gomega.HaveOccurred
	beNil        = gomega.BeNil
	beEmpty      = gomega.BeEmpty
)

// RunE2ETests runs e2e tests for px cluster
func RunE2ETests(t *testing.T) {
	g.RunSpecs(t, "Talisman: PX cluster")
}

var _ = g.BeforeSuite(func() {
	var err error
	pxClusterProvider, err = px.NewPXClusterProvider(nil)
	expect(err).NotTo(haveOccurred())
	expect(pxClusterProvider).NotTo(beNil())

	pxKvdbEndpoints = os.Getenv("PX_KVDB_ENDPOINTS")
	expect(pxKvdbEndpoints).NotTo(beEmpty(), "Env variable PX_KVDB_ENDPOINTS must be set.")

	pxDataInterface = os.Getenv("PX_DATA_INTF")
	pxMgmtInterface = os.Getenv("PX_MGMT_INTF")
})

// Add ginkgo test for testing px cluster operations
var _ = g.Describe("install and teardown portworx", func() {
	g.It("has to install and teardown portworx cluster", func() {
		kvdbList, err := splitCsv(pxKvdbEndpoints)
		expect(err).NotTo(haveOccurred())
		expect(kvdbList).NotTo(beEmpty(), fmt.Sprintf("invalid kvdb list: %s", pxKvdbEndpoints))

		pxSpec := &apiv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "talisman-e2e-px-cluster",
				Namespace: metav1.NamespaceSystem,
			},
			Spec: apiv1alpha1.ClusterSpec{
				Kvdb: apiv1alpha1.KvdbSpec{
					Endpoints: kvdbList,
				},
				Storage: apiv1alpha1.StorageSpec{
					UseAll: true,
					Force:  true,
				},
				Network: apiv1alpha1.NodeNetwork{
					Data: pxDataInterface,
					Mgmt: pxMgmtInterface,
				},
			},
		}

		g.By("create portworx cluster", func() {
			err = pxClusterProvider.Create(pxSpec)
			expect(err).NotTo(haveOccurred())

		})

		/*g.By("destroy portworx cluster", func() {
			err := pxClusterProvider.Destroy(pxSpec)
			expect(err).NotTo(haveOccurred())
		})*/
	})
})

func splitCsv(in string) ([]string, error) {
	r := csv.NewReader(strings.NewReader(in))
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil || len(records) < 1 {
		return []string{}, err
	} else if len(records) > 1 {
		return []string{}, fmt.Errorf("Multiline CSV not supported")
	}
	return records[0], err
}

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(g.GinkgoWriter)
}
