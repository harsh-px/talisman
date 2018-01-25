package pxcluster

import (
	"fmt"
	"testing"

	g "github.com/onsi/ginkgo"
)

// RunE2ETests runs e2e tests for px cluster
func RunE2ETests(t *testing.T) {
	g.RunSpecs(t, "Talisman: PX cluster")
}

// Add ginkgo test for testing px cluster operations
var _ = g.Describe("install and teardown portworx", func() {
	g.It("has to install and teardown portworx cluster", func() {
		g.By("install portworx", func() {
			fmt.Printf("installing portworx ...\n")
		})
	})
})
