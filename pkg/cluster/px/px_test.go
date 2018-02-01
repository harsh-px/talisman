package px

import (
	"testing"

	"github.com/portworx/talisman/pkg/apis/portworx.com/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestUpgrade(t *testing.T) {
	pxProvider, err := newFakePXClusterProvider("")
	require.NoError(t, err, "failed to create px cluster provider instance")

	newSpec := &v1alpha1.Cluster{
		Spec: v1alpha1.ClusterSpec{
			PXVersion: "portworx/px-enterprise:1.3.0-rc3",
		},
	}
	err = pxProvider.Upgrade(newSpec)
	require.NoError(t, err, "failed to upgrade px cluster")
}
